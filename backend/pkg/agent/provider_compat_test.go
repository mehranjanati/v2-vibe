package agent

import (
	"context"
	"encoding/json"
	"testing"
)

// workersAIArrayBody mirrors what the eino-ext OpenAI adapter serializes:
// multi-part text content as an array, a null assistant content and a
// string system message.
const workersAIArrayBody = `{
  "model": "@cf/qwen/qwen2.5-coder-32b-instruct",
  "messages": [
    {"role": "system", "content": [{"type": "text", "text": "you are a planner"}]},
    {"role": "user", "content": [{"type": "text", "text": "build a todo app"}]},
    {"role": "assistant", "content": null, "tool_calls": [{"id": "call_1"}]},
    {"role": "tool", "tool_call_id": "call_1", "content": [{"type": "text", "text": "file written"}]},
    {"role": "user", "content": "plain string stays"}
  ]
}`

func TestWorkersAIPayloadModifierFlattensContent(t *testing.T) {
	out, err := workersAIPayloadModifier(context.Background(), nil, []byte(workersAIArrayBody))
	if err != nil {
		t.Fatalf("modifier returned error: %v", err)
	}

	var body struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("rewritten body is not JSON: %v\n%s", err, out)
	}
	if body.Model != "@cf/qwen/qwen2.5-coder-32b-instruct" {
		t.Errorf("model = %q, want the original model", body.Model)
	}
	if len(body.Messages) != 5 {
		t.Fatalf("messages = %d, want 5 (order and count must be preserved)", len(body.Messages))
	}

	for i, want := range []string{"you are a planner", "build a todo app", "", "file written", "plain string stays"} {
		var got string
		if err := json.Unmarshal(body.Messages[i].Content, &got); err != nil {
			t.Fatalf("messages[%d].content is not a string: %s (%v)", i, body.Messages[i].Content, err)
		}
		if got != want {
			t.Errorf("messages[%d].content = %q, want %q", i, got, want)
		}
	}
}

func TestWorkersAIPayloadModifierKeepsStringBodiesUntouched(t *testing.T) {
	raw := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	out, err := workersAIPayloadModifier(context.Background(), nil, raw)
	if err != nil {
		t.Fatalf("modifier returned error: %v", err)
	}
	if string(out) != string(raw) {
		t.Errorf("body changed although it was already compatible:\n got: %s\nwant: %s", out, raw)
	}
}

func TestWorkersAIPayloadModifierPassesThroughUnknownBodies(t *testing.T) {
	for _, raw := range []string{`not json`, `{"prompt":"legacy"}`, `{"messages":"not-an-array"}`} {
		out, err := workersAIPayloadModifier(context.Background(), nil, []byte(raw))
		if err != nil {
			t.Fatalf("modifier(%q) returned error: %v", raw, err)
		}
		if string(out) != raw {
			t.Errorf("modifier(%q) = %q, want it passed through unchanged", raw, out)
		}
	}
}

func TestNeedsWorkersAIPayloadCompat(t *testing.T) {
	cases := map[string]bool{
		"https://api.cloudflare.com/client/v4/accounts/acct/ai/v1": true,
		"https://API.Cloudflare.com/whatever":                      true,
		"https://gateway.ai.cloudflare.com/v1/acct/gw":             false,
		"https://api.openai.com/v1":                                false,
		"":                                                         false,
	}
	for baseURL, want := range cases {
		if got := needsWorkersAIPayloadCompat(baseURL); got != want {
			t.Errorf("needsWorkersAIPayloadCompat(%q) = %v, want %v", baseURL, got, want)
		}
	}
}
