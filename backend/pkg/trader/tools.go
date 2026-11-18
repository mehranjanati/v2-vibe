package trader

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/redis/go-redis/v9"

	"backend/pkg/cloudflare"
)

// Deps are the only platform handles this package needs. The engine supplies
// them; nothing here knows about a room, a chat or a generation.
type Deps struct {
	// D1 is the journal/state store. Nil disables persistence (dev/CI).
	D1 *cloudflare.D1Client
	// RDB is the fast-resume mirror. Nil is supported.
	RDB *redis.Client
	// Anchor pins a verified chain head outside D1 (the control plane passes
	// its internal Git repo). Nil disables anchoring; verification still runs.
	Anchor Anchor
}

// Tools returns the agent-facing trade memory tools: resume (read state +
// recent journal) and checkpoint (write state + one provenanced event).
//
// Identity (tenant/agent/run) is bound per call by the engine through
// WithTradeContext — the model never supplies it.
func Tools(d Deps) ([]tool.BaseTool, error) {
	resume, err := newTradeResumeTool(d)
	if err != nil {
		return nil, err
	}
	checkpoint, err := newTradeCheckpointTool(d)
	if err != nil {
		return nil, err
	}
	riskCheck, err := newTradeRiskCheckTool(d)
	if err != nil {
		return nil, err
	}
	order, err := newTradeOrderTool(d)
	if err != nil {
		return nil, err
	}
	return []tool.BaseTool{resume, checkpoint, riskCheck, order}, nil
}

// tradeResumeInput caps how many recent journal events to replay.
type tradeResumeInput struct {
	Lookback int `json:"lookback,omitempty"`
}

// tradeResumeOutput is the resumable snapshot handed to the agent.
type tradeResumeOutput struct {
	Found   bool                     `json:"found"`
	State   TradeState               `json:"state"`
	Journal []map[string]interface{} `json:"journal"`
	RunID   string                   `json:"run_id"`
	// ChainOK reports whether the journal's hash chain verified. A tampered
	// chain makes the tool FAIL, so this is true whenever a result is returned.
	ChainOK bool `json:"chain_ok"`
}

// tradeCheckpointInput persists the run's end state + one journal event.
//
// Provenance fields are REQUIRED: a checkpoint without a pinned source and
// config version is rejected, because an unattributed row cannot be trusted
// as evidence if the memory is later attacked or audited.
type tradeCheckpointInput struct {
	StateJSON string `json:"state_json"`
	Event     string `json:"event"`
	Detail    string `json:"detail_json,omitempty"`
	// Source must be on the journal source allowlist.
	Source string `json:"source"`
	// ConfigVersion pins the risk/strategy config used by this run.
	ConfigVersion string `json:"config_version"`
	// SkillRef/ToolRef are optional but must be pinned refs when supplied.
	SkillRef string `json:"skill_ref,omitempty"`
	ToolRef  string `json:"tool_ref,omitempty"`
}

// tradeCheckpointOutput confirms the write.
type tradeCheckpointOutput struct {
	Written bool   `json:"written"`
	Event   string `json:"event"`
}

// tradeCtxKey is the context key for the engine-bound identity. The model
// must NOT control it (same pattern as the VFS tool context).
type tradeCtxKey struct{}

// WithTradeContext tags the tool-call context with the run's identity.
func WithTradeContext(ctx context.Context, tenant, agent, runID string) context.Context {
	return context.WithValue(ctx, tradeCtxKey{}, Identity{tenant: tenant, agent: agent, runID: runID})
}

func tradeID(ctx context.Context) (Identity, error) {
	id, ok := ctx.Value(tradeCtxKey{}).(Identity)
	if !ok || id.tenant == "" || id.agent == "" {
		return Identity{}, fmt.Errorf("trader: no identity bound to tool call")
	}
	return id, nil
}

// newTradeResumeTool builds the "trade_resume" tool.
//
// It verifies the journal's hash chain BEFORE handing any history to the agent:
// resuming from a tampered journal is exactly the memory-injection failure mode,
// so a broken chain fails the tool call instead of silently returning rows.
func newTradeResumeTool(d Deps) (tool.InvokableTool, error) {
	return utils.InferTool("trade_resume",
		"Resume the trader's cross-run memory: current positions, equity, kill-switch flag, plus recent journal events. Verifies the journal hash chain first; a tampered chain fails the call. Call once at run start.",
		func(ctx context.Context, in tradeResumeInput) (*tradeResumeOutput, error) {
			id, err := tradeID(ctx)
			if err != nil {
				return nil, err
			}
			st, found := LoadTradeState(ctx, d.D1, d.RDB, id.tenant, id.agent)
			chain, cerr := VerifyChain(ctx, d.D1, id.tenant, id.agent, in.Lookback)
			if cerr != nil {
				return nil, fmt.Errorf("trader: trade_resume: chain verify: %w", cerr)
			}
			if !chain.OK {
				return nil, fmt.Errorf("trader: trade_resume: memory-tampered (%s at row %d)", chain.Reason, chain.BrokenAt)
			}
			journal := LoadTradeJournal(ctx, d.D1, id.tenant, id.agent, in.Lookback)
			return &tradeResumeOutput{Found: found, State: st, Journal: journal, RunID: id.runID, ChainOK: chain.OK}, nil
		})
}

// newTradeCheckpointTool builds the "trade_checkpoint" tool. Persists the
// end-of-run state and appends one provenanced journal event.
func newTradeCheckpointTool(d Deps) (tool.InvokableTool, error) {
	return utils.InferTool("trade_checkpoint",
		"Persist the trader's end-of-run state and append one journal event. Requires source and config_version (provenance); call once at run end with the final positions, equity, losses and kill-switch flag.",
		func(ctx context.Context, in tradeCheckpointInput) (*tradeCheckpointOutput, error) {
			id, err := tradeID(ctx)
			if err != nil {
				return nil, err
			}
			if in.Event == "" {
				return nil, fmt.Errorf("trader: trade_checkpoint: event is required")
			}
			var st TradeState
			if err := json.Unmarshal([]byte(in.StateJSON), &st); err != nil {
				return nil, fmt.Errorf("trader: trade_checkpoint: state_json: %w", err)
			}
			var detail map[string]interface{}
			if in.Detail != "" {
				_ = json.Unmarshal([]byte(in.Detail), &detail)
			}
			prov := TradeProvenance{
				AgentRef:      "agent/" + id.agent + "@" + AgentVersion(id.agent),
				SkillRef:      in.SkillRef,
				ToolRef:       in.ToolRef,
				Source:        in.Source,
				ConfigVersion: in.ConfigVersion,
			}
			SaveTradeState(ctx, d.D1, d.RDB, id.tenant, id.agent, id.runID, st)
			if err := AppendTradeEventWithProvenance(ctx, d.D1, id.tenant, id.agent, id.runID, in.Event, prov, detail); err != nil {
				return nil, fmt.Errorf("trader: trade_checkpoint: %w", err)
			}
			// Seal the chain head outside D1 so a later table rewrite cannot
			// reproduce the history. Best-effort: a failed anchor never loses
			// the row itself.
			AnchorHead(ctx, d.D1, d.Anchor, id.tenant, id.agent)
			return &tradeCheckpointOutput{Written: true, Event: in.Event}, nil
		})
}

// AgentVersion resolves the pinned agent version for a known agent name.
// Unknown agents default to 1.0.0 so the ref stays structurally valid; the
// real pin comes from the manifest registry once P7.3 lands.
func AgentVersion(agentName string) string {
	switch agentName {
	case "spot-trader":
		return "1.0.0"
	default:
		return "1.0.0"
	}
}
