package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	agentplan "backend/agent"
	"backend/pkg/llm"
)

// structuredPlanFixture is a valid ExecutionPlan payload the fake planner
// streams (same schema as the example in skills/01_planner.md).
const structuredPlanFixture = `{"thought_process":"1) VFS is empty - greenfield build. 2) Requirements: add, complete and delete todos. 3) Approach: seed data, store, rendering, bootstrap, markup. 4) Risks: none.","subtasks":["Data & state","UI & wiring"],"goal":"a working todo app","steps":[{"action":"create","file_path":"public/js/data.js","description":"seed todo items","associated_subtask_index":0},{"action":"modify","file_path":"public/index.html","description":"wire the entry point","associated_subtask_index":1}]}`

// brokenPlanFixture decodes as JSON but violates the plan contract: an
// unknown action and an out-of-range subtask index.
const brokenPlanFixture = `{"thought_process":"x","subtasks":[],"goal":"g","steps":[{"action":"rename","file_path":"a.js","description":"d","associated_subtask_index":0}]}`

// ssePlanServer spins an OpenAI-compatible /chat/completions SSE endpoint
// answering successive chat requests with the given texts (the last one
// repeats). Returns a snapshot func of the raw request bodies.
func ssePlanServer(t *testing.T, responses ...string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		text := responses[min(calls, len(responses)-1)]
		calls++
		mu.Unlock()

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

// newPlanTestRoom builds a room wired to the fake SSE endpoint.
func newPlanTestRoom(t *testing.T, srvURL, plannerPrompt string) *ProjectRoom {
	t.Helper()
	client := llm.NewClient(llm.Config{
		GatewayURL: srvURL,
		APIKey:     "test-key",
		Model:      "test-model",
		Timeout:    30 * time.Second,
	})
	room := NewProjectRoom("plan-itest", nil, client, nil)
	if plannerPrompt != "" {
		room.SetPlannerPrompt(plannerPrompt)
	}
	return room
}

// planRequest decodes a captured request body into its chat messages.
func planRequest(t *testing.T, body string) []llm.ChatMessage {
	t.Helper()
	var req struct {
		Messages []llm.ChatMessage `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal request body: %v\n%s", err, body)
	}
	return req.Messages
}

func planTestCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestProposePlanStructured covers the happy path: the planner's raw JSON
// streams in silently, is validated, and comes back as (machine JSON,
// rendered markdown) with NO raw-JSON deltas broadcast.
func TestProposePlanStructured(t *testing.T) {
	srv, getBodies := ssePlanServer(t, structuredPlanFixture)
	room := newPlanTestRoom(t, srv.URL, "PLANNER PROMPT (skills/01_planner.md)")

	machine, display := room.proposePlan(planTestCtx(t), "build a todo app")

	parsed, err := agentplan.ParsePlan(machine)
	if err != nil {
		t.Fatalf("machine plan does not parse: %v\n%s", err, machine)
	}
	if err := parsed.Validate(); err != nil {
		t.Fatalf("machine plan invalid: %v", err)
	}
	if parsed.Steps[0].Action != agentplan.ActionCreate || parsed.Steps[1].Action != agentplan.ActionModify {
		t.Errorf("actions not normalized: %+v", parsed.Steps)
	}

	if strings.Contains(display, `"thought_process"`) {
		t.Errorf("display must be rendered markdown, not raw JSON:\n%s", display)
	}
	for _, want := range []string{
		"**Goal:** a working todo app",
		"**Steps:**",
		"`create` `public/js/data.js`",
		"`modify` `public/index.html`",
	} {
		if !strings.Contains(display, want) {
			t.Errorf("display misses %q:\n%s", want, display)
		}
	}

	// Structured mode suppresses delta streaming: exactly one broadcast
	// (the turn-closing ConversationResponse) must be queued.
	if got := len(room.broadcast); got != 1 {
		t.Errorf("broadcast messages = %d, want 1 (final only)", got)
	}

	bodies := getBodies()
	if len(bodies) != 1 {
		t.Fatalf("LLM calls = %d, want 1", len(bodies))
	}
	msgs := planRequest(t, bodies[0])
	if len(msgs) == 0 || msgs[0].Role != "system" || !strings.Contains(msgs[0].Content, "PLANNER PROMPT") {
		t.Errorf("request did not carry the planner skill as system prompt: %+v", msgs)
	}
}

// TestProposePlanStructuredRepair covers the contract repair pass: an
// invalid first plan triggers exactly one repair round-trip whose request
// reports the violations, and the corrected plan is accepted.
func TestProposePlanStructuredRepair(t *testing.T) {
	srv, getBodies := ssePlanServer(t, brokenPlanFixture, structuredPlanFixture)
	room := newPlanTestRoom(t, srv.URL, "PLANNER PROMPT")

	machine, display := room.proposePlan(planTestCtx(t), "build a todo app")

	parsed, err := agentplan.ParsePlan(machine)
	if err != nil {
		t.Fatalf("repaired plan does not parse: %v\n%s", err, machine)
	}
	if parsed.Steps[0].Action != agentplan.ActionCreate {
		t.Errorf("repair did not normalize the action: %+v", parsed.Steps[0])
	}
	if !strings.Contains(display, "**Goal:** a working todo app") {
		t.Errorf("display = %q", display)
	}

	bodies := getBodies()
	if len(bodies) != 2 {
		t.Fatalf("LLM calls = %d, want 2 (plan + repair)", len(bodies))
	}
	// streamLLMRaw prepends the system message: [system, user, assistant,
	// user] — the rejected plan and the violation report trail it.
	msgs := planRequest(t, bodies[1])
	if len(msgs) != 4 {
		t.Fatalf("repair conversation = %d messages, want 4", len(msgs))
	}
	if msgs[0].Role != "system" || !strings.Contains(msgs[0].Content, "PLANNER PROMPT") {
		t.Errorf("repair call lost the planner system prompt: %+v", msgs[0])
	}
	if msgs[2].Role != "assistant" || msgs[2].Content != brokenPlanFixture {
		t.Errorf("repair conversation misses the rejected plan: %+v", msgs[2])
	}
	if msgs[3].Role != "user" || !strings.Contains(msgs[3].Content, "violates the output contract") {
		t.Errorf("repair request misses the violation report: %+v", msgs[3])
	}
}

// TestProposePlanUnrecoverable covers two consecutive contract violations:
// proposePlan gives up with ("", "") so the caller skips the plan gate.
func TestProposePlanUnrecoverable(t *testing.T) {
	srv, getBodies := ssePlanServer(t, brokenPlanFixture, brokenPlanFixture)
	room := newPlanTestRoom(t, srv.URL, "PLANNER PROMPT")

	machine, display := room.proposePlan(planTestCtx(t), "build a todo app")
	if machine != "" || display != "" {
		t.Fatalf("want empty machine/display, got %q / %q", machine, display)
	}
	if bs := getBodies(); len(bs) != 2 {
		t.Fatalf("LLM calls = %d, want 2 (plan + one repair)", len(bs))
	}
}

// TestProposePlanLegacyFallback covers the no-skills fallback: the prose
// plan passes through verbatim as both machine plan and display.
func TestProposePlanLegacyFallback(t *testing.T) {
	prose := "**Proposed Plan & Assumptions**\n1. **Screens & Components:** one screen.\n2. **Mock Data:** todos.\n3. **Interactions:** add/complete/delete.\n4. **Visual Style:** minimal."
	srv, _ := ssePlanServer(t, prose)
	room := newPlanTestRoom(t, srv.URL, "")

	machine, display := room.proposePlan(planTestCtx(t), "build a todo app")
	if machine != prose || display != prose {
		t.Fatalf("legacy mode must pass prose through verbatim:\n%q\n%q", machine, display)
	}
}

// TestPlanRepairMessages pins the repair conversation shape.
func TestPlanRepairMessages(t *testing.T) {
	msgs := planRepairMessages("build a todo app", "raw-broken-output",
		errors.New(`step 0: action "rename" is not one of "create", "modify", "delete"`))
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "build a todo app" {
		t.Errorf("msgs[0] = %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "raw-broken-output" {
		t.Errorf("msgs[1] = %+v", msgs[1])
	}
	if msgs[2].Role != "user" ||
		!strings.Contains(msgs[2].Content, "violates the output contract") ||
		!strings.Contains(msgs[2].Content, `"rename"`) {
		t.Errorf("msgs[2] = %+v", msgs[2])
	}
}

// TestHubPlannerPromptPlumbing verifies SetPlannerPrompt reaches new rooms.
func TestHubPlannerPromptPlumbing(t *testing.T) {
	h := NewEngineHub(nil, nil, nil)
	h.SetPlannerPrompt("PLANNER PROMPT")
	room := h.GetOrCreateRoom("hub-planner-prompt")
	t.Cleanup(func() {
		room.Stop()
		h.RemoveRoom("hub-planner-prompt")
	})
	if room.plannerPrompt != "PLANNER PROMPT" {
		t.Fatalf("room plannerPrompt = %q, want the hub prompt", room.plannerPrompt)
	}
}
