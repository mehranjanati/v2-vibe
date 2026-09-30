// Package agent assembles the stateless Eino agent engine: an ADK
// ReAct chat-model agent wired to the Cloudflare AI Gateway
// (OpenAI-compatible endpoint) with dynamic tools and Redis-backed
// checkpointing. The engine itself holds NO per-session state — all
// conversation state is passed in and out by the caller (loaded from and
// saved to Redis by the handler layer).
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// EngineConfig configures the agent engine.
type EngineConfig struct {
	// BaseURL is the OpenAI-compatible endpoint base URL, e.g.
	// https://gateway.ai.cloudflare.com/v1/<account>/<gateway> or the
	// Workers AI endpoint https://api.cloudflare.com/client/v4/accounts/<id>/ai/v1.
	BaseURL string
	// APIKey is the bearer token (AI Gateway token or Cloudflare API token).
	APIKey string
	// Model is the chat model identifier.
	Model string
	// Instruction is the system prompt for the agent.
	Instruction string
	// MaxIterations bounds the ReAct loop. Defaults to 20.
	MaxIterations int
	// Tools are the dynamic tools (rest_api, vfs_*) available to the agent.
	Tools []tool.BaseTool
}

// StreamEvent is a single chunk forwarded to the WebSocket layer while the
// agent step executes.
type StreamEvent struct {
	// Type is one of: "token" (LLM text delta), "tool_call" (the model
	// requested a tool), "tool_result" (a tool finished), "done".
	Type string
	// Content carries token text or a human-readable summary.
	Content string
	// ToolName / ToolCallID / ToolArgs identify tool activity.
	ToolName   string
	ToolCallID string
	ToolArgs   string
	// ToolResult is the raw tool output fed back to the LLM.
	ToolResult string
	// IsFinal marks the last assistant text message of the step.
	IsFinal bool
}

// Engine is the stateless agent runner. It is safe for concurrent use
// across sessions: sessions are distinguished purely by the message
// history the caller loads/saves via the checkpoint store.
type Engine struct {
	chatModel model.ChatModel
	cfg       EngineConfig
}

// NewEngine creates the engine, configuring the eino-ext OpenAI chat model
// to talk to the Cloudflare AI Gateway (or Workers AI) endpoint.
func NewEngine(ctx context.Context, cfg EngineConfig) (*Engine, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("agent: no LLM provider configured (set AI_GATEWAY_URL or CLOUDFLARE_ACCOUNT_ID)")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("agent: no model configured (set DEFAULT_MODEL)")
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 20
	}

	cm, err := einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Model:   cfg.Model,
	})
	if err != nil {
		return nil, fmt.Errorf("agent: create chat model: %w", err)
	}
	// Wrap with malformed-chunk tolerance so a single bad stream frame
	// (e.g. numeric "content" from Workers AI) ends the stream gracefully
	// instead of surfacing as a NodeRunError through the ADK graph.
	return &Engine{chatModel: newTolerantStreamModel(cm), cfg: cfg}, nil
}

// ChatModel exposes the underlying tool-calling chat model for advanced
// compositions (e.g. the planexecute prebuilt needs a
// model.ToolCallingChatModel for its planner/replanner). Returns nil when
// the chat model does not support tool calling.
func (e *Engine) ChatModel() model.ToolCallingChatModel {
	if tcm, ok := e.chatModel.(model.ToolCallingChatModel); ok {
		return tcm
	}
	return nil
}

// SupportsPlanExecute reports whether the underlying chat model supports
// the tool-calling interface required by the planexecute prebuilt.
func (e *Engine) SupportsPlanExecute() bool {
	_, ok := e.chatModel.(model.ToolCallingChatModel)
	return ok
}

// RoleModelConfig configures an additional per-role chat model built on the
// SAME provider endpoint as the engine (BaseURL/APIKey). The agent
// WebSocket handler keeps using the engine model; the multi-agent team
// builds one of these per role so the per-role model / token budget /
// temperature resolved by pkg/skills actually reach the provider instead of
// being shadowed by the single engine model.
//
// A zero MaxTokens (or negative Temperature) leaves that provider default
// untouched, so an unset override never changes the request.
type RoleModelConfig struct {
	// Model is the provider model id, e.g.
	// @cf/qwen/qwen2.5-coder-32b-instruct. Empty falls back to the engine
	// model, which makes the call a no-op.
	Model string
	// MaxTokens caps generated tokens for this role. Zero = provider default.
	MaxTokens int
	// Temperature samples the model. Negative = provider default.
	Temperature float64
}

// NewRoleModel builds a tool-calling chat model for one generation role.
//
// It returns the engine's own model unchanged when the role resolves to the
// same model id with no per-call budget — the common case — so the default
// configuration keeps sharing one provider client. Otherwise the returned
// model is wrapped with the same malformed-chunk tolerance as the engine
// model (see tolerant_model.go), so every consumer keeps the graceful
// end-of-stream behavior for bad Workers AI frames.
//
// max_tokens rather than max_completion_tokens is sent deliberately: this
// client talks to Workers AI / AI Gateway (OpenAI-compatible), where
// max_tokens is the supported parameter and the o1-series incompatibility
// noted by eino-ext does not apply.
func (e *Engine) NewRoleModel(ctx context.Context, cfg RoleModelConfig) (model.ChatModel, error) {
	modelID := strings.TrimSpace(cfg.Model)
	if modelID == "" {
		return e.chatModel, nil
	}
	if modelID == e.cfg.Model && cfg.MaxTokens <= 0 && cfg.Temperature < 0 {
		return e.chatModel, nil
	}

	cc := &einoopenai.ChatModelConfig{
		BaseURL: e.cfg.BaseURL,
		APIKey:  e.cfg.APIKey,
		Model:   modelID,
	}
	if cfg.MaxTokens > 0 {
		mt := cfg.MaxTokens
		cc.MaxTokens = &mt
	}
	if cfg.Temperature >= 0 {
		t := float32(cfg.Temperature)
		cc.Temperature = &t
	}

	cm, err := einoopenai.NewChatModel(ctx, cc)
	if err != nil {
		return nil, fmt.Errorf("agent: create role chat model %q: %w", modelID, err)
	}
	return newTolerantStreamModel(cm), nil
}

// RunStep executes one full ReAct step for the session using the engine's
// default instruction and tool set. See RunStepWith for details.
func (e *Engine) RunStep(
	ctx context.Context,
	userMessage string,
	history []*schema.Message,
	emit func(StreamEvent),
) ([]*schema.Message, error) {
	return e.RunStepWith(ctx, e.cfg.Instruction, e.cfg.Tools, userMessage, history, emit)
}

// RunStepWith executes one full ADK ReAct step with a per-call system
// instruction and tool set. This is the single LLM entry point shared by
// the agent WebSocket handler AND the project-room generation pipeline
// (P3 unification): one chat model, one streaming event path, one stack.
//
//   - history (loaded by the caller) + the new user message is fed to the
//     ADK ChatModelAgent,
//   - the agent streams tokens and executes tool calls in its loop,
//     emitting StreamEvents via emit,
//   - the full updated message history is returned for the caller.
//
// A nil/empty tools list yields a plain streaming completion agent.
func (e *Engine) RunStepWith(
	ctx context.Context,
	system string,
	tools []tool.BaseTool,
	userMessage string,
	history []*schema.Message,
	emit func(StreamEvent),
) ([]*schema.Message, error) {
	if emit == nil {
		emit = func(StreamEvent) {}
	}

	a, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "vibe_agent",
		Description:   "Stateless ReAct agent with REST and VFS tools",
		Instruction:   system,
		Model:         e.chatModel,
		MaxIterations: e.cfg.MaxIterations,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("agent: build chat model agent: %w", err)
	}

	userMsg := schema.UserMessage(userMessage)
	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           a,
		EnableStreaming: true,
	})
	iter := runner.Run(ctx, []*schema.Message{userMsg})

	updated := make([]*schema.Message, 0, len(history)+8)
	updated = append(updated, history...)
	updated = append(updated, userMsg)

	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			return nil, fmt.Errorf("agent: run step: %w", ev.Err)
		}
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		mv := ev.Output.MessageOutput

		var msg *schema.Message
		if mv.IsStreaming {
			msg, err = drainStream(mv.MessageStream, emit)
			if err != nil {
				return nil, err
			}
		} else {
			msg = mv.Message
		}
		if msg == nil {
			continue
		}
		updated = append(updated, msg)

		switch {
		case len(msg.ToolCalls) > 0:
			for _, tc := range msg.ToolCalls {
				emit(StreamEvent{
					Type: "tool_call", ToolName: tc.Function.Name,
					ToolCallID: tc.ID, ToolArgs: tc.Function.Arguments,
				})
			}
		case msg.Role == schema.Tool:
			emit(StreamEvent{
				Type: "tool_result", ToolName: msg.ToolName,
				ToolCallID: msg.ToolCallID, ToolResult: msg.Content,
			})
		case msg.Role == schema.Assistant:
			emit(StreamEvent{Type: "token", Content: msg.Content, IsFinal: true})
		}
	}

	emit(StreamEvent{Type: "done"})
	return updated, nil
}

// drainStream consumes an LLM token stream, forwarding each delta via
// emit, and reconstructs the complete message (content plus the tool
// calls carried by the final chunk) so it can be appended to history.
func drainStream(sr *schema.StreamReader[*schema.Message], emit func(StreamEvent)) (*schema.Message, error) {
	defer sr.Close() //nolint:errcheck // always close, even on io.EOF

	var (
		content   string
		lastCalls []schema.ToolCall
	)
	for {
		chunk, err := sr.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Tolerate malformed chunks: some Workers AI / gateway model
			// deltas carry a non-string "content" (e.g. a number) which
			// the upstream client cannot unmarshal. That single bad frame
			// kills the whole stream at the acl layer — but the content
			// accumulated so far is still valid. Treat it like an early
			// end-of-stream instead of failing the entire generation; the
			// room's truncation/retry path then re-emits the unfinished
			// files with a larger budget.
			if isMalformedChunkError(err) {
				log.Printf("agent: ignoring malformed stream chunk (%v); using partial output", err)
				break
			}
			return nil, fmt.Errorf("agent: read token stream: %w", err)
		}
		if chunk == nil {
			continue
		}
		if chunk.Content != "" {
			content += chunk.Content
			emit(StreamEvent{Type: "token", Content: chunk.Content})
		}
		if len(chunk.ToolCalls) > 0 {
			// Streaming tool-call args accumulate across chunks; keep the
			// latest snapshot, which carries the complete arguments.
			lastCalls = append(lastCalls[:0], chunk.ToolCalls...)
		}
	}
	return schema.AssistantMessage(content, lastCalls), nil
}

// isMalformedChunkError reports whether err is a JSON shape mismatch that
// an upstream OpenAI-compatible client hit while decoding one stream
// frame — i.e. a single bad chunk, not a transport/API failure. We
// deliberately tolerate these.
func isMalformedChunkError(err error) bool {
	if err == nil {
		return false
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "cannot unmarshal") && strings.Contains(msg, "of type")
}
