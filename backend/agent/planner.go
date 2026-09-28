package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"backend/pkg/llm"
	"backend/pkg/skills"
)

// GeneratePlan calls the planner model through the Cloudflare AI Gateway
// (Workers AI, OpenAI-compatible /chat/completions) with JSON mode
// enabled: response_format is pinned to {"type":"json_schema", ...} with
// the ExecutionPlan schema, so the model cannot wrap its answer in
// markdown fences or prose — the previous source of Unmarshal/EOF errors.
// The reply decodes straight into ExecutionPlan, is normalized and
// validated, and any API or JSON failure is logged with full detail.
func GeneratePlan(ctx context.Context, userPrompt, vfsState string) (ExecutionPlan, error) {
	reg := skills.NewRegistry()
	prompt, err := reg.LoadRole(skills.RolePlanner)
	if err != nil {
		// The skill prompt (backend/skills/01_planner.md) is the contract:
		// fail loudly, never call the model without it.
		return ExecutionPlan{}, fmt.Errorf("agent: planner: %w", err)
	}
	return generatePlanWith(ctx, prompt, llm.NewConfigFromEnv(), userPrompt, vfsState)
}

// generatePlanWith is GeneratePlan's injectable core: it takes the already
// loaded planner skill (verbatim system prompt + per-role model config)
// and the provider config, so tests can point the call at a fake gateway.
// The planner model comes from the role config (default
// @cf/meta/llama-3.3-70b-instruct-fp8-fast, PLANNER_MODEL override win).
func generatePlanWith(ctx context.Context, prompt skills.Prompt, cfg llm.Config, userPrompt, vfsState string) (ExecutionPlan, error) {
	if cfg.GatewayURL == "" {
		return ExecutionPlan{}, errors.New("agent: planner: no LLM provider configured (set AI_GATEWAY_URL or CLOUDFLARE_ACCOUNT_ID)")
	}

	messages := []llm.ChatMessage{
		{Role: "system", Content: prompt.Text},
		{Role: "user", Content: userContent(userPrompt, vfsState)},
	}
	body, err := json.Marshal(planRequest{
		Model:          prompt.Config.Model,
		Messages:       messages,
		Stream:         false, // Workers AI JSON mode does not support streaming
		Temperature:    prompt.Config.Temperature,
		MaxTokens:      prompt.Config.MaxTokens,
		ResponseFormat: jsonModeResponseFormat(),
	})
	if err != nil {
		return ExecutionPlan{}, fmt.Errorf("agent: planner: marshal request: %w", err)
	}

	url := strings.TrimRight(cfg.GatewayURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ExecutionPlan{}, fmt.Errorf("agent: planner: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: cfg.Timeout}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return ExecutionPlan{}, fmt.Errorf("agent: planner: request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ExecutionPlan{}, fmt.Errorf("agent: planner: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Non-2xx: surface the provider's error body verbatim (capped).
		detail := snippet(string(raw))
		log.Printf("[planner] API error: status %d: %s", resp.StatusCode, detail)
		return ExecutionPlan{}, fmt.Errorf("agent: planner: API error: status %d: %s", resp.StatusCode, detail)
	}

	var completion planCompletion
	if err := json.Unmarshal(raw, &completion); err != nil {
		log.Printf("[planner] response is not a chat completion envelope: %v; body: %s", err, snippet(string(raw)))
		return ExecutionPlan{}, fmt.Errorf("agent: planner: response envelope: %w", err)
	}
	if completion.Error != nil {
		log.Printf("[planner] provider error: %s", completion.Error.Message)
		return ExecutionPlan{}, fmt.Errorf("agent: planner: provider error: %s", completion.Error.Message)
	}
	if len(completion.Choices) == 0 {
		return ExecutionPlan{}, errors.New("agent: planner: response has no choices")
	}

	// JSON mode guarantees the content is a bare JSON object, so a direct
	// unmarshal into ExecutionPlan normally succeeds without any of the
	// tolerant outer-`{...}` retries in ParsePlan.
	plan, perr := ParsePlan(completion.Choices[0].Message.Content)
	if perr != nil {
		log.Printf("[planner] model output does not decode into ExecutionPlan: %v; content: %s",
			perr, snippet(completion.Choices[0].Message.Content))
		return ExecutionPlan{}, perr
	}
	if verr := plan.Validate(); verr != nil {
		log.Printf("[planner] model output violates the ExecutionPlan contract: %v", verr)
		return ExecutionPlan{}, verr
	}
	log.Printf("[planner] plan generated (model %s): %d subtasks, %d steps",
		prompt.Config.Model, len(plan.Subtasks), len(plan.Steps))
	return plan, nil
}

// userContent frames the human turn: the user's request plus the current
// VFS snapshot the planner must reason about.
func userContent(userPrompt, vfsState string) string {
	return "User request:\n" + userPrompt + "\n\nCurrent VFS snapshot:\n" + vfsState
}

// planRequest is the OpenAI-compatible chat completion body for the
// structured planner call. stream is always false: Workers AI JSON mode
// does not support streaming.
type planRequest struct {
	Model          string                 `json:"model"`
	Messages       []llm.ChatMessage      `json:"messages"`
	Stream         bool                   `json:"stream"`
	Temperature    float64                `json:"temperature,omitempty"`
	MaxTokens      int                    `json:"max_tokens,omitempty"`
	ResponseFormat map[string]interface{} `json:"response_format"`
}

// planCompletion is the subset of the chat completion response the planner
// consumes: the assistant message content (a JSON string) plus the optional
// error object Workers AI returns when JSON mode cannot be met.
type planCompletion struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// jsonModeResponseFormat returns the response_format object that turns on
// Workers AI JSON mode (OpenAI structured-outputs compatible):
// {"type": "json_schema", "json_schema": <ExecutionPlan schema>}.
func jsonModeResponseFormat() map[string]interface{} {
	return map[string]interface{}{
		"type":        "json_schema",
		"json_schema": executionPlanSchema(),
	}
}

// executionPlanSchema returns the JSON Schema for ExecutionPlan, matching
// backend/agent/types.go field-for-field: the four snake_case fields, the
// string arrays for subtasks/steps and the closed enum of step actions.
func executionPlanSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"thought_process": map[string]interface{}{
				"type": "string",
			},
			"subtasks": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"goal": map[string]interface{}{
				"type": "string",
			},
			"steps": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"action": map[string]interface{}{
							"type": "string",
							"enum": []interface{}{ActionCreate, ActionModify, ActionDelete},
						},
						"file_path": map[string]interface{}{
							"type": "string",
						},
						"description": map[string]interface{}{
							"type": "string",
						},
						"associated_subtask_index": map[string]interface{}{
							"type": "integer",
						},
					},
					"required": []interface{}{
						"action", "file_path", "description", "associated_subtask_index",
					},
				},
			},
		},
		"required": []interface{}{
			"thought_process", "subtasks", "goal", "steps",
		},
	}
}

// snippet caps a payload for logs/errors so a huge or binary body never
// floods the log, while still keeping enough detail to debug a failure.
func snippet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[:500] + "…(truncated)"
	}
	return s
}
