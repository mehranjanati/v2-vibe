package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/pkg/llm"
	"backend/pkg/skills"
)

// plannerPlanFixture is a schema-faithful JSON-mode planner payload (same
// shape as the example in backend/skills/01_planner.md).
const plannerPlanFixture = `{"thought_process":"1) VFS is empty - greenfield build. 2) Requirements: add, complete and delete todos. 3) Approach: seed data, store, rendering, bootstrap, markup. 4) Risks: none.","subtasks":["Data & state","UI & wiring"],"goal":"a working todo app","steps":[{"action":"create","file_path":"public/js/data.js","description":"seed todo items","associated_subtask_index":0},{"action":"modify","file_path":"public/index.html","description":"wire the entry point","associated_subtask_index":1}]}`

// plannerBrokenFixture decodes as JSON but violates the ExecutionPlan
// contract: an unknown action and an out-of-range subtask index.
const plannerBrokenFixture = `{"thought_process":"x","subtasks":[],"goal":"g","steps":[{"action":"rename","file_path":"a.js","description":"d","associated_subtask_index":0}]}`

// plannerPromptFixture stands in for backend/skills/01_planner.md.
const plannerPromptFixture = "PLANNER PROMPT (skills/01_planner.md)"

const plannerModel = "@cf/meta/llama-3.3-70b-instruct-fp8-fast"

// testPrompt builds the planner skill as skills.LoadRole would produce it.
func testPrompt(t *testing.T) skills.Prompt {
	t.Helper()
	return skills.Prompt{
		Config: skills.RoleConfig{
			Role:        skills.RolePlanner,
			Model:       plannerModel,
			SkillFile:   "01_planner.md",
			Temperature: 0.2,
			MaxTokens:   8192,
		},
		Text: plannerPromptFixture,
	}
}

// testConfig points the planner at the fake gateway.
func testConfig(srvURL string) llm.Config {
	return llm.Config{GatewayURL: srvURL, APIKey: "test-key", Timeout: 15 * time.Second}
}

// planServer spins an httptest endpoint answering with the given status
// and body. Returns the server plus a snapshot func of the raw request
// bodies captured so far.
func planServer(t *testing.T, status int, body string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return bodies
	}
}

// completionEnvelope wraps a plan JSON string in an OpenAI-compatible
// non-streaming chat completion response.
func completionEnvelope(t *testing.T, content string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"message": map[string]any{
				"role":    "assistant",
				"content": content,
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal completion envelope: %v", err)
	}
	return string(body)
}

// requestProbe mirrors the JSON shape the planner must POST (the deep
// response_format nesting included) so tests can walk the captured body.
type requestProbe struct {
	Model          string              `json:"model"`
	Messages       []llm.ChatMessage   `json:"messages"`
	Stream         bool                `json:"stream"`
	Temperature    float64             `json:"temperature,omitempty"`
	MaxTokens      int                 `json:"max_tokens,omitempty"`
	ResponseFormat responseFormatProbe `json:"response_format"`
}

// responseFormatProbe walks the json_schema of the request.
type responseFormatProbe struct {
	Type       string               `json:"type"`
	JSONSchema executionSchemaProbe `json:"json_schema"`
}

// executionSchemaProbe walks the critical ExecutionPlan schema nesting.
type executionSchemaProbe struct {
	Type       string   `json:"type"`
	Required   []string `json:"required"`
	Properties struct {
		ThoughtProcess struct {
			Type string `json:"type"`
		} `json:"thought_process"`
		Subtasks struct {
			Type string `json:"type"`
		} `json:"subtasks"`
		Steps struct {
			Type  string `json:"type"`
			Items struct {
				Type       string   `json:"type"`
				Required   []string `json:"required"`
				Properties struct {
					Action struct {
						Enum []string `json:"enum"`
					} `json:"action"`
				} `json:"properties"`
			} `json:"items"`
		} `json:"steps"`
	} `json:"properties"`
}

// decodeRequest unparses one captured request body into requestProbe.
func decodeRequest(t *testing.T, body string) requestProbe {
	t.Helper()
	var req requestProbe
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("request body is not the expected planner payload: %v\n%s", err, body)
	}
	return req
}

// TestGeneratePlanHappyPath: a well-formed JSON-mode reply decodes straight
// into a validated ExecutionPlan.
func TestGeneratePlanHappyPath(t *testing.T) {
	srv, _ := planServer(t, http.StatusOK, completionEnvelope(t, plannerPlanFixture))
	plan, err := generatePlanWith(context.Background(), testPrompt(t), testConfig(srv.URL),
		"build a todo app", "index.html\nstyles.css")
	if err != nil {
		t.Fatalf("generatePlanWith: %v", err)
	}
	if plan.Goal != "a working todo app" || len(plan.Steps) != 2 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if plan.Steps[0].Action != ActionCreate || plan.Steps[0].FilePath != "public/js/data.js" {
		t.Fatalf("step 0 = %+v", plan.Steps[0])
	}
	if plan.ThoughtProcess == "" || len(plan.Subtasks) != 2 {
		t.Fatalf("plan lost fields: %+v", plan)
	}
}

// TestGeneratePlanWritesJSONModeRequest: the HTTP body must pin JSON mode
// (response_format json_schema with the ExecutionPlan schema), carry the
// skill verbatim as the system prompt and the request + VFS as the user
// turn, disable streaming and use the planner model.
func TestGeneratePlanWritesJSONModeRequest(t *testing.T) {
	srv, getBodies := planServer(t, http.StatusOK, completionEnvelope(t, plannerPlanFixture))
	_, err := generatePlanWith(context.Background(), testPrompt(t), testConfig(srv.URL),
		"build a todo app", "index.html\nstyles.css")
	if err != nil {
		t.Fatalf("generatePlanWith: %v", err)
	}
	reqs := getBodies()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	req := decodeRequest(t, reqs[0])

	if req.Model != plannerModel {
		t.Fatalf("model = %q, want %q", req.Model, plannerModel)
	}
	if req.Stream {
		t.Fatalf("stream must be false for JSON mode, got true")
	}
	if req.ResponseFormat.Type != "json_schema" {
		t.Fatalf("response_format.type = %q, want json_schema", req.ResponseFormat.Type)
	}
	schema := req.ResponseFormat.JSONSchema
	if schema.Type != "object" {
		t.Fatalf("json_schema.type = %q, want object", schema.Type)
	}
	for _, field := range []string{"thought_process", "subtasks", "goal", "steps"} {
		if !strings.Contains(strings.Join(schema.Required, ","), field) {
			t.Fatalf("json_schema.required misses %q: %+v", field, schema.Required)
		}
	}
	if strings.Join(schema.Properties.Steps.Items.Properties.Action.Enum, ",") != "create,modify,delete" {
		t.Fatalf("action enum = %+v, want create,modify,delete", schema.Properties.Steps.Items.Properties.Action.Enum)
	}

	if len(req.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || req.Messages[0].Content != plannerPromptFixture {
		t.Fatalf("system message does not carry the skill verbatim: %+v", req.Messages[0])
	}
	user := req.Messages[1]
	if user.Role != "user" {
		t.Fatalf("second message role = %q, want user", user.Role)
	}
	if !strings.Contains(user.Content, "build a todo app") || !strings.Contains(user.Content, "Current VFS snapshot") || !strings.Contains(user.Content, "index.html") {
		t.Fatalf("user message does not carry request + VFS: %q", user.Content)
	}
}

// TestGeneratePlanHttpError: a non-2xx provider response is logged as a
// distinct API error that keeps both status and body detail.
func TestGeneratePlanHttpError(t *testing.T) {
	srv, _ := planServer(t, http.StatusInternalServerError, `{"error":{"message":"boom"}}`)
	_, err := generatePlanWith(context.Background(), testPrompt(t), testConfig(srv.URL), "p", "v")
	if err == nil {
		t.Fatal("expected an API error")
	}
	if !strings.Contains(err.Error(), "agent: planner: API error") || !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error lacks detail: %v", err)
	}
}

// TestGeneratePlanProviderError: Workers AI reports a structured error
// object (e.g. JSON mode could not be met) inside a 200 response.
func TestGeneratePlanProviderError(t *testing.T) {
	body := `{"error":{"message":"JSON Mode couldn't be met: invalid schema"}}`
	srv, _ := planServer(t, http.StatusOK, body)
	_, err := generatePlanWith(context.Background(), testPrompt(t), testConfig(srv.URL), "p", "v")
	if err == nil {
		t.Fatal("expected a provider error")
	}
	if !strings.Contains(err.Error(), "provider error") || !strings.Contains(err.Error(), "JSON Mode") {
		t.Fatalf("error lacks detail: %v", err)
	}
}

// TestGeneratePlanTruncatedContent: the provider returns a response whose
// content is cut off mid-JSON (e.g. max_tokens was hit). This must fail
// with the log-worthy ParsePlan detail, never silently produce an empty
// or partial plan.
func TestGeneratePlanTruncatedContent(t *testing.T) {
	truncated := `{"thought_process": "a very long "`
	srv, _ := planServer(t, http.StatusOK, completionEnvelope(t, truncated))
	_, err := generatePlanWith(context.Background(), testPrompt(t), testConfig(srv.URL), "p", "v")
	if err == nil {
		t.Fatal("expected a decode error for truncated JSON")
	}
	if !strings.Contains(err.Error(), "agent:") || !strings.Contains(err.Error(), "plan") {
		t.Fatalf("error does not mention the plan contract: %v", err)
	}
}

// TestGeneratePlanContractViolation: content that decodes as JSON but
// violates the ExecutionPlan contract (unknown action, out-of-range
// subtask) must be rejected with the validation detail.
func TestGeneratePlanContractViolation(t *testing.T) {
	srv, _ := planServer(t, http.StatusOK, completionEnvelope(t, plannerBrokenFixture))
	_, err := generatePlanWith(context.Background(), testPrompt(t), testConfig(srv.URL), "p", "v")
	if err == nil {
		t.Fatal("expected a validation error")
	}
	if !strings.Contains(err.Error(), "rename") || !strings.Contains(err.Error(), "action") {
		t.Fatalf("error does not carry the violation detail: %v", err)
	}
}

// TestGeneratePlanNoProvider: without a gateway URL the planner fails fast
// with a clear configuration error before any HTTP is attempted.
func TestGeneratePlanNoProvider(t *testing.T) {
	_, err := generatePlanWith(context.Background(), testPrompt(t),
		llm.Config{GatewayURL: "", APIKey: "", Timeout: 5 * time.Second}, "p", "v")
	if err == nil {
		t.Fatal("expected a provider error")
	}
	if !strings.Contains(err.Error(), "AI_GATEWAY_URL") {
		t.Fatalf("error does not mention the config: %v", err)
	}
}

// TestGeneratePlanIntegration exercises the public GeneratePlan end to
// end: the real skills registry loads backend/skills/01_planner.md, and
// llm.NewConfigFromEnv reads the (temporarily overridden) environment
// pointing at the fake gateway.
func TestGeneratePlanIntegration(t *testing.T) {
	srv, getBodies := planServer(t, http.StatusOK, completionEnvelope(t, plannerPlanFixture))

	origURL := os.Getenv("AI_GATEWAY_URL")
	origKey := os.Getenv("AI_GATEWAY_API_KEY")
	_ = os.Setenv("AI_GATEWAY_URL", srv.URL)
	_ = os.Setenv("AI_GATEWAY_API_KEY", "test-key")
	defer func() {
		_ = os.Setenv("AI_GATEWAY_URL", origURL)
		_ = os.Setenv("AI_GATEWAY_API_KEY", origKey)
	}()

	plan, err := GeneratePlan(context.Background(), "build a todo app", "index.html\nstyles.css")
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if plan.Goal != "a working todo app" || len(plan.Steps) != 2 {
		t.Fatalf("unexpected plan: %+v", plan)
	}

	reqs := getBodies()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	req := decodeRequest(t, reqs[0])
	if req.Model != plannerModel {
		t.Fatalf("model = %q, want the registry planner model %q", req.Model, plannerModel)
	}
	// Provenance: the system prompt really comes from skills/01_planner.md.
	if !strings.Contains(req.Messages[0].Content, "Planner") {
		t.Fatalf("system prompt does not look like 01_planner.md: %.200s", req.Messages[0].Content)
	}
}
