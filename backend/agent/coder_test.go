package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/pkg/llm"
	"backend/pkg/skills"
)

// coderModelFixture mirrors the registry default for the coder role.
const coderModel = "@cf/qwen/qwen2.5-coder-32b-instruct"

// coderCodeFixture is a representative raw coder reply (no fences, no
// prose — the 02_coder.md output contract).
const coderCodeFixture = "window.App = { name: 'todo' };\n"

// coderPromptFixture stands in for backend/skills/02_coder.md.
const coderPromptFixture = "CODER PROMPT (skills/02_coder.md)"

// coderPrompt builds the coder skill as skills.LoadRole would produce it.
func coderPrompt(t *testing.T) skills.Prompt {
	t.Helper()
	return skills.Prompt{
		Config: skills.RoleConfig{
			Role:        skills.RoleCoder,
			Model:       coderModel,
			SkillFile:   "02_coder.md",
			Temperature: 0.1,
			MaxTokens:   8192,
		},
		Text: coderPromptFixture,
	}
}

// testCoderConfig points the coder at the fake gateway.
func testCoderConfig(srvURL string) llm.Config {
	return llm.Config{GatewayURL: srvURL, APIKey: "test-key", Timeout: 15 * time.Second}
}

// sseCoderServer spins an OpenAI-compatible /chat/completions SSE endpoint
// streaming each response as ONE delta chunk (plus finish_reason and
// [DONE]). Returns a snapshot func of the raw request bodies.
func sseCoderServer(t *testing.T, chunks ...string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for _, c := range chunks {
			payload, _ := json.Marshal(map[string]any{
				"choices": []any{map[string]any{"delta": map[string]any{"content": c}}},
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

// coderRequestProbe mirrors the JSON shape executeStepWith must POST.
type coderRequestProbe struct {
	Model       string            `json:"model"`
	Messages    []llm.ChatMessage `json:"messages"`
	Stream      bool              `json:"stream"`
	Temperature float64           `json:"temperature"`
	MaxTokens   int               `json:"max_tokens"`
}

// drain reads everything left on an output channel and joins it.
func drain(ch <-chan string) string {
	var b strings.Builder
	for c := range ch {
		b.WriteString(c)
	}
	return b.String()
}

// TestExecuteStepStreamsDeltas: every model delta reaches outputChan in
// order and a clean stream returns nil.
func TestExecuteStepStreamsDeltas(t *testing.T) {
	srv, _ := sseCoderServer(t, "window.App", " = { name: 'todo' };\n")
	ch := make(chan string, 4)
	_, err := executeStepWith(context.Background(), coderPrompt(t), testCoderConfig(srv.URL),
		PlanStep{Action: ActionCreate, FilePath: "public/js/app.js", Description: "app shell"},
		"", ch)
	if err != nil {
		t.Fatalf("executeStepWith: %v", err)
	}
	close(ch)
	if got := drain(ch); got != "window.App = { name: 'todo' };\n" {
		t.Fatalf("streamed content = %q", got)
	}
}

// TestExecuteStepWritesCoderRequest: the HTTP body carries the registry
// coder model, the skill verbatim as the system message, the task JSON as
// the user turn (with existing_content for modify), streaming enabled and
// the role generation parameters.
func TestExecuteStepWritesCoderRequest(t *testing.T) {
	srv, getBodies := sseCoderServer(t, coderCodeFixture)
	ch := make(chan string, 4)
	_, err := executeStepWith(context.Background(), coderPrompt(t), testCoderConfig(srv.URL),
		PlanStep{Action: ActionModify, FilePath: "public/index.html", Description: "wire the entry point"},
		"<html>old</html>", ch)
	if err != nil {
		t.Fatalf("executeStepWith: %v", err)
	}
	close(ch)

	bodies := getBodies()
	if len(bodies) != 1 {
		t.Fatalf("requests = %d, want 1", len(bodies))
	}
	var req coderRequestProbe
	if err := json.Unmarshal([]byte(bodies[0]), &req); err != nil {
		t.Fatalf("request body is not the expected coder payload: %v\n%s", err, bodies[0])
	}
	if req.Model != coderModel {
		t.Errorf("model = %q, want %q", req.Model, coderModel)
	}
	if !req.Stream {
		t.Errorf("stream must be true for the coder SSE call")
	}
	if req.Temperature != 0.1 || req.MaxTokens != 8192 {
		t.Errorf("generation params = temp %v / maxTokens %d, want 0.1 / 8192", req.Temperature, req.MaxTokens)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || req.Messages[0].Content != coderPromptFixture {
		t.Errorf("system message does not carry the coder skill verbatim: %+v", req.Messages[0])
	}
	var task struct {
		Path            string `json:"path"`
		Action          string `json:"action"`
		Requirements    string `json:"requirements"`
		ExistingContent string `json:"existing_content"`
	}
	if err := json.Unmarshal([]byte(req.Messages[1].Content), &task); err != nil {
		t.Fatalf("user turn is not the task JSON: %v\n%s", err, req.Messages[1].Content)
	}
	if task.Path != "public/index.html" || task.Action != ActionModify ||
		task.Requirements != "wire the entry point" || task.ExistingContent != "<html>old</html>" {
		t.Errorf("task JSON = %+v", task)
	}
}

// TestExecuteStepCreateOmitsExistingContent: a create task carries no
// existing_content field at all.
func TestExecuteStepCreateOmitsExistingContent(t *testing.T) {
	srv, getBodies := sseCoderServer(t, coderCodeFixture)
	ch := make(chan string, 2)
	_, err := executeStepWith(context.Background(), coderPrompt(t), testCoderConfig(srv.URL),
		PlanStep{Action: ActionCreate, FilePath: "public/js/app.js", Description: "seed"}, "", ch)
	if err != nil {
		t.Fatalf("executeStepWith: %v", err)
	}
	close(ch)
	bodies := getBodies()
	var req coderRequestProbe
	if err := json.Unmarshal([]byte(bodies[0]), &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if strings.Contains(req.Messages[1].Content, "existing_content") {
		t.Errorf("create task must not carry existing_content: %s", req.Messages[1].Content)
	}
}

// TestExecuteStepNoProvider: without a gateway URL the coder fails fast
// with a clear configuration error before any HTTP is attempted.
func TestExecuteStepNoProvider(t *testing.T) {
	ch := make(chan string, 1)
	_, err := executeStepWith(context.Background(), coderPrompt(t),
		llm.Config{GatewayURL: "", APIKey: "", Timeout: time.Second},
		PlanStep{Action: ActionCreate, FilePath: "a.js"}, "", ch)
	if err == nil {
		t.Fatal("expected a provider error")
	}
	if !strings.Contains(err.Error(), "AI_GATEWAY_URL") {
		t.Fatalf("error does not mention the config: %v", err)
	}
}

// TestExecuteStepStreamError: a provider error frame mid-stream surfaces
// as the function's error, never as silent partial content.
func TestExecuteStepStreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"error\":{\"message\":\"boom\"}}\n\n")
	}))
	t.Cleanup(srv.Close)

	ch := make(chan string, 4)
	_, err := executeStepWith(context.Background(), coderPrompt(t), testCoderConfig(srv.URL),
		PlanStep{Action: ActionCreate, FilePath: "a.js"}, "", ch)
	close(ch)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected a stream error carrying the provider detail, got %v", err)
	}
}

// TestExecuteStepContextCancel: a cancelled context stops the call and
// surfaces the cancellation instead of blocking on channel sends.
func TestExecuteStepContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	ch := make(chan string)
	if _, err := executeStepWith(ctx, coderPrompt(t), testCoderConfig(srv.URL),
		PlanStep{Action: ActionCreate, FilePath: "a.js"}, "", ch); err == nil {
		t.Fatal("expected a context error")
	}
}

// TestExecuteStepIntegration exercises the public ExecuteStep end to end:
// the real skills registry loads backend/skills/02_coder.md and
// llm.NewConfigFromEnv reads the (overridden) environment pointing at the
// fake gateway.
func TestExecuteStepIntegration(t *testing.T) {
	srv, getBodies := sseCoderServer(t, coderCodeFixture)
	t.Setenv("AI_GATEWAY_URL", srv.URL)
	t.Setenv("AI_GATEWAY_API_KEY", "test-key")

	ch := make(chan string, 4)
	_, err := ExecuteStep(context.Background(),
		PlanStep{Action: ActionModify, FilePath: "public/index.html", Description: "wire entry"},
		"<html>old</html>", ch)
	if err != nil {
		t.Fatalf("ExecuteStep: %v", err)
	}
	close(ch)
	if got := drain(ch); got != coderCodeFixture {
		t.Fatalf("content = %q", got)
	}

	bodies := getBodies()
	var req coderRequestProbe
	if err := json.Unmarshal([]byte(bodies[0]), &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if req.Model != coderModel {
		t.Errorf("model = %q, want the registry coder model %q", req.Model, coderModel)
	}
	// Provenance: the system prompt really comes from skills/02_coder.md.
	if !strings.Contains(req.Messages[0].Content, "Coder") {
		t.Errorf("system prompt does not look like 02_coder.md: %.200s", req.Messages[0].Content)
	}
}
