package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// errNoBindTools is reported when the inner model cannot bind tool schemas.
var errNoBindTools = errors.New("agent: inner model does not support BindTools")

// ---------- Workers AI request compatibility ----------
//
// The eino-ext OpenAI adapter serializes a message's multi-part content as a
// JSON *array* and omits `content` entirely when it is empty. Workers AI's
// OpenAI-compatible endpoint (`/accounts/<id>/ai/v1`) validates a stricter
// schema and rejects that body with:
//
//	AiError: Bad input: oneOf at '/' not met … Type mismatch of
//	'/messages/0/content', 'array' not in 'string'
//
// (code 5006). The failing call is the first graph model node, so the whole
// generation dies before any text is produced.
//
// workersAIPayloadModifier rewrites the serialized body into the shape Workers
// AI accepts, using the adapter's own documented hook
// (einoopenai.WithRequestPayloadModifier), so neither the ADK graph nor the
// per-role team pipeline has to know about the quirk. It is applied only when
// the engine talks to Workers AI directly; an AI Gateway URL
// (gateway.ai.cloudflare.com) may proxy to providers where multi-part content
// is valid and is left untouched.

// workersAIHost marks the direct Workers AI API host.
const workersAIHost = "api.cloudflare.com"

// needsWorkersAIPayloadCompat reports whether baseURL targets Workers AI
// directly and therefore needs the payload rewrite.
func needsWorkersAIPayloadCompat(baseURL string) bool {
	return strings.Contains(strings.ToLower(baseURL), workersAIHost)
}

// workersAIPayloadModifier flattens every message's `content` into a plain
// string and guarantees the property exists:
//
//   - `"content": [{type:"text",text:"…"}, …]` → the concatenated text,
//   - `"content": null` / missing → `""` (Workers AI requires the property),
//   - `"content": "…"` → unchanged.
//
// Non-text parts (images, audio) are dropped: Workers AI's chat schema has no
// representation for them and the agent pipeline is text-only. A body that is
// not recognisable JSON is returned unchanged rather than blocking the call.
func workersAIPayloadModifier(_ context.Context, _ []*schema.Message, rawBody []byte) ([]byte, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rawBody, &body); err != nil {
		return rawBody, nil
	}
	rawMessages, ok := body["messages"]
	if !ok {
		return rawBody, nil
	}
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(rawMessages, &messages); err != nil {
		return rawBody, nil
	}

	changed := false
	for _, message := range messages {
		content, hasContent := message["content"]
		if !hasContent || bytes.Equal(bytes.TrimSpace(content), []byte("null")) {
			message["content"] = json.RawMessage(`""`)
			changed = true
			continue
		}
		var text string
		if err := json.Unmarshal(content, &text); err == nil {
			continue // already the shape Workers AI wants
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(content, &parts); err != nil {
			message["content"] = json.RawMessage(`""`)
			changed = true
			continue
		}
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
		joined, err := json.Marshal(strings.Join(texts, "\n"))
		if err != nil {
			continue
		}
		message["content"] = joined
		changed = true
	}
	if !changed {
		return rawBody, nil
	}

	encodedMessages, err := json.Marshal(messages)
	if err != nil {
		return rawBody, nil
	}
	body["messages"] = encodedMessages
	out, err := json.Marshal(body)
	if err != nil {
		return rawBody, nil
	}
	return out, nil
}

// workersAISafeModel injects the payload modifier into every Generate/Stream
// call, including the tool-bound models the ADK and plan-execute graphs derive
// via WithTools. Graph nodes pass their own options, so the modifier is
// appended last and wins for every call.
type workersAISafeModel struct {
	model.ToolCallingChatModel
}

// newWorkersAISafeModel wraps m with the Workers AI payload rewrite. A model
// that is not a tool-calling model is returned unchanged (it cannot reach the
// agent pipeline).
func newWorkersAISafeModel(m model.ChatModel) model.ChatModel {
	tcm, ok := m.(model.ToolCallingChatModel)
	if !ok {
		return m
	}
	return &workersAISafeModel{ToolCallingChatModel: tcm}
}

// modelOpts appends the payload modifier to a call's options.
func modelOpts(opts []model.Option) []model.Option {
	return append(opts, einoopenai.WithRequestPayloadModifier(workersAIPayloadModifier))
}

// Generate delegates with the payload modifier injected.
func (w *workersAISafeModel) Generate(
	ctx context.Context,
	in []*schema.Message,
	opts ...model.Option,
) (*schema.Message, error) {
	return w.ToolCallingChatModel.Generate(ctx, in, modelOpts(opts)...)
}

// Stream delegates with the payload modifier injected.
func (w *workersAISafeModel) Stream(
	ctx context.Context,
	in []*schema.Message,
	opts ...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	return w.ToolCallingChatModel.Stream(ctx, in, modelOpts(opts)...)
}

// WithTools keeps the compatibility wrapper on derived tool-bound models.
func (w *workersAISafeModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	inner, err := w.ToolCallingChatModel.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &workersAISafeModel{ToolCallingChatModel: inner}, nil
}

// BindTools delegates to the inner model (deprecated binding style).
func (w *workersAISafeModel) BindTools(tools []*schema.ToolInfo) error {
	type binder interface {
		BindTools([]*schema.ToolInfo) error
	}
	if b, ok := w.ToolCallingChatModel.(binder); ok {
		return b.BindTools(tools)
	}
	return errNoBindTools
}
