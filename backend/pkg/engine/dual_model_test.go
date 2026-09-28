package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// dualPlanFixture is a schema-faithful ExecutionPlan the fake planner
// serves (JSON-mode reply); the two steps exercise create + modify.
const dualPlanFixture = `{"thought_process":"1) VFS is empty - greenfield build. 2) Requirements: shell app. 3) Approach: app script, entry markup. 4) Risks: none.","subtasks":["Script","Markup"],"goal":"a working app","steps":[{"action":"create","file_path":"public/js/app.js","description":"app bootstrap script","associated_subtask_index":0},{"action":"modify","file_path":"public/index.html","description":"entry markup","associated_subtask_index":1}]}`

// dualDeletePlanFixture plans a single delete step (no coder call).
const dualDeletePlanFixture = `{"thought_process":"x","subtasks":["Cleanup"],"goal":"g","steps":[{"action":"delete","file_path":"public/js/old.js","description":"remove the stale script","associated_subtask_index":0}]}`

// dualPipelineServer fakes the AI Gateway for the whole pipeline: non-
// streaming requests get the plan JSON-mode envelope (planner), streaming
// requests get the next coder response as one SSE delta (split in half).
func dualPipelineServer(t *testing.T, planFixture string, coderResponses ...string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		call := len(bodies)
		bodies = append(bodies, string(raw))
		mu.Unlock()

		var probe struct {
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(raw, &probe)

		if !probe.Stream {
			// Planner: non-streaming JSON-mode completion envelope.
			w.Header().Set("Content-Type", "application/json")
			env, _ := json.Marshal(map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]any{"role": "assistant", "content": planFixture},
				}},
			})
			_, _ = w.Write(env)
			return
		}

		// Coder: SSE stream of one response split in two deltas.
		mu.Lock()
		idx := call - 1
		mu.Unlock()
		text := ""
		if idx >= 0 && idx < len(coderResponses) {
			text = coderResponses[idx]
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for _, half := range []string{text[:len(text)/2], text[len(text)/2:]} {
			payload, _ := json.Marshal(map[string]any{
				"choices": []any{map[string]any{"delta": map[string]any{"content": half}}},
			})
			fmt.Fprintf(w, "data: %s\n\n", payload)
			fl.Flush()
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
}

// dualPipelineEnv points the skills registry at backend/skills (the
// pkg/engine working dir probes "../skills" first, which hits pkg/skills)
// and the pipeline at the fake gateway.
func dualPipelineEnv(t *testing.T, srvURL string) {
	t.Helper()
	t.Setenv("SKILLS_DIR", "../../skills")
	t.Setenv("AI_GATEWAY_URL", srvURL)
	t.Setenv("AI_GATEWAY_API_KEY", "test-key")
}

// approveLater resolves the B7 gate shortly after the run starts.
func approveLater(room *ProjectRoom) {
	go func() {
		time.Sleep(50 * time.Millisecond)
		room.ApprovePlan()
	}()
}

// dualTruncateServer fakes the AI Gateway for the truncation-retry test:
// non-streaming requests (planner) get the plan fixture; the FIRST
// streaming coder call is served truncated (finish_reason=length), every
// later streaming call serves `full` with finish_reason=stop.
func dualTruncateServer(t *testing.T, planFixture string, partial, full string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	var streamCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()

		var probe struct {
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(raw, &probe)

		if !probe.Stream {
			// Planner: non-streaming JSON-mode completion envelope.
			w.Header().Set("Content-Type", "application/json")
			env, _ := json.Marshal(map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]any{"role": "assistant", "content": planFixture},
				}},
			})
			_, _ = w.Write(env)
			return
		}

		// Coder: streaming SSE. First call truncates, later calls complete.
		n := int(streamCalls.Add(1))
		text := full
		finish := "stop"
		if n == 1 {
			text = partial
			finish = "length"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		if text != "" {
			payload, _ := json.Marshal(map[string]any{
				"choices": []any{map[string]any{"delta": map[string]any{"content": text}}},
			})
			fmt.Fprintf(w, "data: %s\n\n", payload)
			fl.Flush()
		}
		fin, _ := json.Marshal(map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": finish}},
		})
		fmt.Fprintf(w, "data: %s\n\n", fin)
		fmt.Fprint(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
}

// TestRunDualModelPipelineGeneratesFiles covers the R3 orchestrator end
// to end: GeneratePlan (planner, JSON mode) -> plan broadcast + gate ->
// per-step ExecuteStep (coder, SSE) -> files in the VFS.
func TestRunDualModelPipelineGeneratesFiles(t *testing.T) {
	srv, getBodies := dualPipelineServer(t, dualPlanFixture,
		"window.App = { name: 'todo' };\n",
		"<html><body>app</body></html>\n")
	dualPipelineEnv(t, srv.URL)

	room := NewProjectRoom("dual-itest", nil, nil, nil)
	go room.Run()
	t.Cleanup(room.Stop)
	room.UpsertFile("public/index.html", "<html>old</html>\n")
	approveLater(room)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := room.runDualModelPipeline(ctx, "build a todo app"); err != nil {
		t.Fatalf("runDualModelPipeline: %v", err)
	}

	files := room.GetVFS()
	if entry := files["public/js/app.js"]; entry == nil || entry.FileContents != "window.App = { name: 'todo' };\n" {
		t.Errorf("app.js not generated as streamed: %+v", entry)
	}
	if entry := files["public/index.html"]; entry == nil || entry.FileContents != "<html><body>app</body></html>\n" {
		t.Errorf("index.html not modified as streamed: %+v", entry)
	}
	if n := room.filesWritten.Load(); n != 2 {
		t.Errorf("filesWritten = %d, want 2", n)
	}

	bodies := getBodies()
	if len(bodies) != 3 {
		t.Fatalf("LLM calls = %d, want 3 (plan + 2 coder steps)", len(bodies))
	}
	// The planner request carried the VFS snapshot of the current files.
	if !strings.Contains(bodies[0], "public/index.html") {
		t.Errorf("planner request misses the VFS snapshot:\n%.400s", bodies[0])
	}
	// The modify step carried the current file content as existing_content
	// inside the task JSON.
	var modifyReq struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(bodies[2]), &modifyReq); err != nil {
		t.Fatalf("unmarshal modify request: %v", err)
	}
	if len(modifyReq.Messages) != 2 {
		t.Fatalf("modify request messages = %d, want 2", len(modifyReq.Messages))
	}
	var task struct {
		Path            string `json:"path"`
		Action          string `json:"action"`
		ExistingContent string `json:"existing_content"`
	}
	if err := json.Unmarshal([]byte(modifyReq.Messages[1].Content), &task); err != nil {
		t.Fatalf("modify user turn is not task JSON: %v\n%s", err, modifyReq.Messages[1].Content)
	}
	if task.Path != "public/index.html" || task.Action != "modify" || task.ExistingContent != "<html>old</html>\n" {
		t.Errorf("modify task JSON = %+v", task)
	}
}

// dualCoffeePlanFixture mirrors the real planner output for a landing-page
// request: every step description is pure mechanics ("Basic HTML5 doctype…")
// and none of them mentions the product domain.
const dualCoffeePlanFixture = `{"thought_process":"1) VFS empty - greenfield. 2) Requirements: coffee landing. 3) Approach: markup, tokens, behaviour. 4) Risks: none.","subtasks":["Structure","Styling","Interaction"],"goal":"A simple coffee landing page with a hero section, description, and call-to-action.","steps":[{"action":"create","file_path":"public/index.html","description":"Basic HTML5 doctype, lang, charset, and responsive viewport meta tag.","associated_subtask_index":0},{"action":"create","file_path":"public/styles.css","description":"Define the :root design tokens for colors and typography.","associated_subtask_index":1}]}`

// coderTaskProbe is the task JSON the coder call carries in its user turn.
type coderTaskProbe struct {
	Path         string `json:"path"`
	Action       string `json:"action"`
	Requirements string `json:"requirements"`
	PlanContext  *struct {
		UserRequest string   `json:"user_request"`
		Goal        string   `json:"goal"`
		Subtasks    []string `json:"subtasks"`
		Steps       []struct {
			Action      string `json:"action"`
			FilePath    string `json:"file_path"`
			Description string `json:"description"`
		} `json:"steps"`
		RelatedFiles map[string]string `json:"related_files"`
		Design       *struct {
			Style     string   `json:"style"`
			Pattern   string   `json:"pattern"`
			Sections  []string `json:"sections"`
			TokensCSS string   `json:"css_tokens"`
			Product   string   `json:"product"`
			Motion    []string `json:"motion"`
			A11y      []string `json:"a11y"`
		} `json:"design"`
	} `json:"plan_context"`
}

// coderTaskFromBody extracts the task JSON from one recorded coder request.
func coderTaskFromBody(t *testing.T, body string) coderTaskProbe {
	t.Helper()
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal coder request: %v", err)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("coder request messages = %d, want 2", len(req.Messages))
	}
	var task coderTaskProbe
	if err := json.Unmarshal([]byte(req.Messages[1].Content), &task); err != nil {
		t.Fatalf("coder user turn is not the task JSON: %v\n%s", err, req.Messages[1].Content)
	}
	return task
}

// TestRunDualModelPipelinePassesPlanContext pins the fix for the
// "generated code has nothing to do with the prompt" failure: every coder
// call carries the user's ORIGINAL request plus the plan's goal, subtasks
// and full sibling-step list as `plan_context`.
//
// Without it the coder's only view of the task is one step's description —
// here "Basic HTML5 doctype, lang, charset, and responsive viewport meta
// tag." — which says nothing about coffee, so the model emits generic
// boilerplate and the preview shows an unrelated page.
func TestRunDualModelPipelinePassesPlanContext(t *testing.T) {
	srv, getBodies := dualPipelineServer(t, dualCoffeePlanFixture,
		"<html><body>coffee</body></html>\n",
		":root { --color-primary: #6f4e37; }\n")
	dualPipelineEnv(t, srv.URL)

	room := NewProjectRoom("dual-coffee", nil, nil, nil)
	go room.Run()
	t.Cleanup(room.Stop)
	approveLater(room)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const userPrompt = "build simple coffee landing"
	if err := room.runDualModelPipeline(ctx, userPrompt); err != nil {
		t.Fatalf("runDualModelPipeline: %v", err)
	}

	bodies := getBodies()
	if len(bodies) != 3 {
		t.Fatalf("LLM calls = %d, want 3 (plan + 2 coder steps)", len(bodies))
	}

	for i, body := range bodies[1:] {
		task := coderTaskFromBody(t, body)
		if task.PlanContext == nil {
			t.Fatalf("coder step %d carries no plan_context; the user request would never reach the model", i+1)
		}
		if task.PlanContext.UserRequest != userPrompt {
			t.Errorf("step %d user_request = %q, want %q", i+1, task.PlanContext.UserRequest, userPrompt)
		}
		if !strings.Contains(strings.ToLower(task.PlanContext.Goal), "coffee") {
			t.Errorf("step %d goal = %q, want the plan goal", i+1, task.PlanContext.Goal)
		}
		if len(task.PlanContext.Subtasks) != 3 {
			t.Errorf("step %d subtasks = %v, want the plan's 3 subtasks", i+1, task.PlanContext.Subtasks)
		}
		// The sibling list is what lets index.html link styles.css and
		// vice versa: both planned files must be visible from every step.
		if len(task.PlanContext.Steps) != 2 {
			t.Fatalf("step %d plan_context.steps = %d, want both planned files", i+1, len(task.PlanContext.Steps))
		}
		want := map[string]bool{"public/index.html": false, "public/styles.css": false}
		for _, st := range task.PlanContext.Steps {
			if _, ok := want[st.FilePath]; ok {
				want[st.FilePath] = true
			}
			if st.Action != "create" {
				t.Errorf("step %d sibling %s action = %q, want create", i+1, st.FilePath, st.Action)
			}
		}
		for path, seen := range want {
			if !seen {
				t.Errorf("step %d plan_context.steps is missing sibling %q", i+1, path)
			}
		}
		// The step's own mechanics description is still carried alongside.
		if task.Requirements == "" {
			t.Errorf("step %d lost its own requirements", i+1)
		}
	}

	// Sanity: the two steps still target their own files.
	if got := coderTaskFromBody(t, bodies[1]).Path; got != "public/index.html" {
		t.Errorf("step 1 path = %q, want public/index.html", got)
	}
	if got := coderTaskFromBody(t, bodies[2]).Path; got != "public/styles.css" {
		t.Errorf("step 2 path = %q, want public/styles.css", got)
	}
}

// TestRunDualModelPipelinePassesRelatedFiles pins the selector-mismatch
// fix: because steps run sequentially, the SECOND coder call must receive
// the real content the FIRST step wrote as plan_context.related_files.
//
// Without it the stylesheet author only knows the plan DESCRIPTION of
// index.html and guesses selectors — the live failure was markup with
// class="about-section" / id="order-now" vs CSS ".about" and JS
// ".order-now": the section rendered unstyled and the CTA was dead.
func TestRunDualModelPipelinePassesRelatedFiles(t *testing.T) {
	const markup = "<html><body><section class=\"about-section\"><button id=\"order-now\">Order</button></section></body></html>\n"
	srv, getBodies := dualPipelineServer(t, dualCoffeePlanFixture,
		markup,
		":root { --color-primary: #6f4e37; }\n")
	dualPipelineEnv(t, srv.URL)

	room := NewProjectRoom("dual-related", nil, nil, nil)
	go room.Run()
	t.Cleanup(room.Stop)
	approveLater(room)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := room.runDualModelPipeline(ctx, "build simple coffee landing"); err != nil {
		t.Fatalf("runDualModelPipeline: %v", err)
	}

	bodies := getBodies()
	if len(bodies) != 3 {
		t.Fatalf("LLM calls = %d, want 3 (plan + 2 coder steps)", len(bodies))
	}

	// Step 1 (index.html): nothing written yet, so no related_files.
	first := coderTaskFromBody(t, bodies[1])
	if first.PlanContext == nil {
		t.Fatal("step 1 lost plan_context")
	}
	if len(first.PlanContext.RelatedFiles) != 0 {
		t.Errorf("step 1 related_files = %v, want none (greenfield)", first.PlanContext.RelatedFiles)
	}

	// Step 2 (styles.css): must see step 1's REAL output, byte for byte.
	second := coderTaskFromBody(t, bodies[2])
	if second.PlanContext == nil {
		t.Fatal("step 2 lost plan_context")
	}
	related := second.PlanContext.RelatedFiles
	if len(related) == 0 {
		t.Fatal("step 2 received no related_files; it would guess selectors blind")
	}
	if got := related["public/index.html"]; got != markup {
		t.Errorf("step 2 related_files[public/index.html] = %q, want the exact file step 1 wrote", got)
	}
	if _, self := related["public/styles.css"]; self {
		t.Error("the file being written must not appear in its own related_files")
	}
}

// TestRunDualModelPipelineCarriesDesignBrief is the end-to-end guarantee for
// the design layer: a coffee-landing request must reach BOTH models with the
// on-brand design direction — the planner via its user turn (so it plans the
// catalog's section order) and every coder step via plan_context.design (so
// each file uses the same token block instead of inventing colors).
func TestRunDualModelPipelineCarriesDesignBrief(t *testing.T) {
	srv, getBodies := dualPipelineServer(t, dualCoffeePlanFixture,
		"<html><body>coffee</body></html>\n",
		":root{}\n")
	dualPipelineEnv(t, srv.URL)

	room := NewProjectRoom("dual-design", nil, nil, nil)
	go room.Run()
	t.Cleanup(room.Stop)
	approveLater(room)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := room.runDualModelPipeline(ctx, "build simple coffee landing"); err != nil {
		t.Fatalf("runDualModelPipeline: %v", err)
	}

	bodies := getBodies()
	if len(bodies) != 3 {
		t.Fatalf("LLM calls = %d, want 3 (plan + 2 coder steps)", len(bodies))
	}

	// 1. The PLANNER user turn must carry the rendered design direction.
	planner := bodyUserContent(t, bodies[0])
	for _, want := range []string{"Design direction", "--primary", "Section order"} {
		if !strings.Contains(planner, want) {
			t.Errorf("planner prompt missing %q:\n%s", want, clipForTest(planner, 900))
		}
	}

	// 2. EVERY coder step must carry the distilled design brief, and the
	// token block must be identical across steps (one design system).
	var firstTokens string
	for i, body := range bodies[1:] {
		task := coderTaskFromBody(t, body)
		if task.PlanContext == nil || task.PlanContext.Design == nil {
			t.Fatalf("coder step %d carries no plan_context.design", i+1)
		}
		d := task.PlanContext.Design
		if d.Style == "" || d.Pattern == "" {
			t.Errorf("step %d design missing style/pattern: %+v", i+1, d)
		}
		if !strings.Contains(d.TokensCSS, "--primary") {
			t.Errorf("step %d token block lacks --primary", i+1)
		}
		if len(d.Sections) == 0 {
			t.Errorf("step %d design has no section order", i+1)
		}
		if i == 0 {
			firstTokens = d.TokensCSS
		} else if d.TokensCSS != firstTokens {
			t.Error("token block differs between coder steps — files would drift apart")
		}
	}
}

// bodyUserContent returns the first user turn of a recorded chat request.
func bodyUserContent(t *testing.T, body string) string {
	t.Helper()
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	for _, m := range req.Messages {
		if m.Role == "user" {
			return m.Content
		}
	}
	return ""
}

// clipForTest bounds a string for test failure output.
func clipForTest(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

const dualEmptyPlanFixture = `{"thought_process":"analysis with no steps","subtasks":[],"goal":"g","steps":[]}`

// TestRunDualModelPipelineEmptyPlanGreenfieldFails: an empty-steps plan on
// an empty VFS is treated as a planner failure (error return) so
// runGeneration falls back to the legacy generation paths, instead of
// silently completing with zero files and a blank preview.
func TestRunDualModelPipelineEmptyPlanGreenfieldFails(t *testing.T) {
	srv, _ := dualPipelineServer(t, dualEmptyPlanFixture)
	dualPipelineEnv(t, srv.URL)

	room := NewProjectRoom("dual-empty", nil, nil, nil)
	go room.Run()
	t.Cleanup(room.Stop)
	approveLater(room)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := room.runDualModelPipeline(ctx, "build something"); err == nil {
		t.Fatal("an empty greenfield plan must fail the pipeline, not complete silently")
	}
	if len(room.GetVFS()) != 0 {
		t.Errorf("VFS should stay empty on planner failure, got %d files", len(room.GetVFS()))
	}
}

// TestRunDualModelPipelineDeletesFiles: a delete step removes the file
// from the VFS without any coder call. The room also ships an index.html
// that references no assets, so the A1 gap-fill pass (now part of the
// pipeline) finds nothing missing and makes no coder calls either.
func TestRunDualModelPipelineDeletesFiles(t *testing.T) {
	srv, getBodies := dualPipelineServer(t, dualDeletePlanFixture)
	dualPipelineEnv(t, srv.URL)

	room := NewProjectRoom("dual-delete", nil, nil, nil)
	go room.Run()
	t.Cleanup(room.Stop)
	room.UpsertFile("public/js/old.js", "legacy\n")
	room.UpsertFile("public/index.html", "<html><body>hi</body></html>\n")
	approveLater(room)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := room.runDualModelPipeline(ctx, "remove the old script"); err != nil {
		t.Fatalf("runDualModelPipeline: %v", err)
	}
	if _, ok := room.GetVFS()["public/js/old.js"]; ok {
		t.Fatal("delete step did not remove the file from the VFS")
	}
	if bs := getBodies(); len(bs) != 1 {
		t.Fatalf("LLM calls = %d, want 1 (planner only; delete makes no coder call)", len(bs))
	}
}

// TestCoderFileContent pins the normalization applied to coder replies:
// a single wrapped fence is stripped, JS gets the shared syntax repair,
// and the file ends with exactly one trailing newline.
func TestCoderFileContent(t *testing.T) {
	got := coderFileContent("public/js/app.js", "```js\nwindow.App = { name: 'x', rating: 4. };\n```\n")
	if want := "window.App = { name: 'x', rating: 4 };\n"; got != want {
		t.Errorf("fenced JS = %q, want %q", got, want)
	}
	if got := coderFileContent("public/index.html", "<html></html>"); got != "<html></html>\n" {
		t.Errorf("html = %q", got)
	}
	if got := coderFileContent("public/js/empty.js", ""); got != "" {
		t.Errorf("empty = %q", got)
	}
}

// TestVfsSnapshot: the planner context lists sorted "path (N bytes)" lines
// and renders "(empty)" for a fresh project.
func TestVfsSnapshot(t *testing.T) {
	room := NewProjectRoom("snapshot", nil, nil, nil)
	if got := room.vfsSnapshot(); got != "(empty)" {
		t.Errorf("empty snapshot = %q", got)
	}
	room.UpsertFile("public/index.html", "<html></html>\n")
	room.UpsertFile("public/js/app.js", "x")
	got := room.vfsSnapshot()
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("snapshot = %q", got)
	}
	if !strings.HasPrefix(lines[0], "public/index.html") || !strings.Contains(lines[0], "14 bytes") {
		t.Errorf("snapshot line 0 = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "public/js/app.js") || !strings.Contains(lines[1], "1 bytes") {
		t.Errorf("snapshot line 1 = %q", lines[1])
	}
}

// TestRunDualModelPipelineTruncatedCoderRetries (A1): the coder is cut off
// mid-file (finish_reason=length) on the first pass. The pipeline must
// regenerate the file with a doubled budget so the VFS never keeps the
// partial file, and the final content is the complete regenerated one.
func TestRunDualModelPipelineTruncatedCoderRetries(t *testing.T) {
	plan := `{"thought_process":"x","subtasks":["Entry"],"goal":"g","steps":[{"action":"create","file_path":"public/index.html","description":"entry markup","associated_subtask_index":0}]}`
	partial := "<html><body><h1>partial"
	full := "<html><body><h1>complete</h1></body></html>\n"
	srv, getBodies := dualTruncateServer(t, plan, partial, full)
	dualPipelineEnv(t, srv.URL)

	room := NewProjectRoom("dual-trunc", nil, nil, nil)
	go room.Run()
	t.Cleanup(room.Stop)
	approveLater(room)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := room.runDualModelPipeline(ctx, "build a page"); err != nil {
		t.Fatalf("runDualModelPipeline: %v", err)
	}
	entry, ok := room.GetVFS()["public/index.html"]
	if !ok {
		t.Fatal("index.html missing from VFS")
	}
	if got := entry.FileContents; got != full {
		t.Fatalf("index.html = %q, want the complete regenerated file %q", got, full)
	}
	// planner + truncated coder pass + the doubling-budget retry. No gap-fill
	// calls: the page references no scripts/styles.
	if bs := getBodies(); len(bs) != 3 {
		t.Fatalf("LLM calls = %d, want 3 (planner + truncated coder + retry)", len(bs))
	}
}

// TestStripSingleFenceUnclosed (A5): an opener fence with no closer, the
// classic truncated-stream signature, must still be broken so ```html never
// leaks into the final file, while the partial body is preserved.
func TestStripSingleFenceUnclosed(t *testing.T) {
	got, ok := stripSingleFence("```html\n<div>partial")
	if !ok {
		t.Fatal("an unclosed opener fence must still be stripped (A5)")
	}
	if got != "<div>partial" {
		t.Errorf("unclosed fence body = %q, want %q", got, "<div>partial")
	}
	if c := coderFileContent("public/index.html", "```html\n<div>partial"); c != "<div>partial\n" {
		t.Errorf("coderFileContent = %q, want %q", c, "<div>partial\n")
	}
}

// TestPlannerMaxTokensFromRoleConfig (B1): the room's structured planner
// budget is read from the planner role config (skills registry), so the
// legacy structured path and the dual-model GeneratePlan path always run
// with the same budget.
func TestPlannerMaxTokensFromRoleConfig(t *testing.T) {
	if got := plannerMaxTokens(); got != 8192 {
		t.Errorf("plannerMaxTokens default = %d, want the planner role default 8192", got)
	}
	t.Setenv("PLANNER_MAX_TOKENS", "6000")
	if got := plannerMaxTokens(); got != 6000 {
		t.Errorf("plannerMaxTokens = %d, want the PLANNER_MAX_TOKENS override 6000", got)
	}
}

// TestPlannerVFSContextNoRedis (A3): without a Redis/vector index the
// planner context is exactly the VFS snapshot (no RAG section), and it
// carries the file list for the planner to reason about.
func TestPlannerVFSContextNoRedis(t *testing.T) {
	room := NewProjectRoom("rag-none", nil, nil, nil)
	room.UpsertFile("public/index.html", "<html></html>\n")
	got := room.plannerVFSContext(context.Background(), "build a page")
	if !strings.Contains(got, "public/index.html") {
		t.Errorf("planner context missing the snapshot: %q", got)
	}
	if strings.Contains(got, "Relevant existing code context") {
		t.Errorf("planner context must omit RAG without Redis: %q", got)
	}
}
