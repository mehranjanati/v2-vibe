// Package trader contains the spot-trader use case: cross-run memory
// (journal + state), provenanced writes, a market-evidence guard, retention,
// and the agent-facing eino tools.
//
// It is deliberately a SEPARATE package from backend/pkg/engine so the use
// case never mixes into the platform core. The engine depends on this package
// (it wires Tools into the plan-execute tool set); this package never imports
// the engine. Dependencies are narrowed to cloudflare.D1Client and
// *redis.Client.
//
// Hybrid memory: `trade_journal` (D1, append-only event log) + `trade_state`
// (D1 upsert + Redis hash mirror for fast resume). Mirrors the generation
// lineage pattern: best-effort writes, tenant from the caller, secrets never
// in payloads.
//
// Tenant isolation: tenantID is an EXPLICIT argument — never inferred from
// the payload (same rule as backend/pkg/mcp: identity comes from the caller).
package trader

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"

	"backend/pkg/cloudflare"
)

// Trade event names written to trade_journal.
const (
	TradeEventSignal     = "signal"
	TradeEventApproval   = "approval"
	TradeEventFill       = "fill"
	TradeEventKillSwitch = "kill-switch"
	TradeEventResume     = "resume"
	TradeEventClose      = "close"
)

// tradeStateKey is the Redis fast-resume mirror of the D1 trade_state row.
func tradeStateKey(tenantID, agentName string) string {
	return "trade:state:" + tenantID + ":" + agentName
}

// TradeState is the resumable per-(tenant, agent) snapshot. Positions only —
// never candles, never secrets (P7.6 rule).
type TradeState struct {
	Equity float64 `json:"equity"`
	// DayStartEquity is the equity at the start of the trading day. The daily
	// loss stop is measured against it, so it must survive restarts (it is
	// omitted from older rows, which simply disables that one check).
	DayStartEquity    float64           `json:"day_start_equity,omitempty"`
	Positions         []TradePosition   `json:"positions"`
	ConsecutiveLosses int               `json:"consecutive_losses"`
	KillSwitch        bool              `json:"kill_switch"`
	UpdatedRunID      string            `json:"updated_run_id,omitempty"`
	Extra             map[string]string `json:"extra,omitempty"`
}

// TradePosition is one open paper/spot position.
type TradePosition struct {
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"`
	Quantity   float64 `json:"quantity"`
	Entry      float64 `json:"entry"`
	StopLoss   float64 `json:"stop_loss"`
	TakeProfit float64 `json:"take_profit"`
}

// D1 statements (bind params only, CF_LIMITS §8).
const (
	tradeStateUpsertSQL = "INSERT INTO trade_state (tenant_id, agent_name, state_json, updated_run_id, updated_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP) " +
		"ON CONFLICT(tenant_id, agent_name) DO UPDATE SET state_json = excluded.state_json, updated_run_id = excluded.updated_run_id, updated_at = CURRENT_TIMESTAMP"
	tradeStateSelectSQL   = "SELECT state_json, updated_run_id FROM trade_state WHERE tenant_id = ? AND agent_name = ? LIMIT 1"
	tradeJournalSelectSQL = "SELECT run_id, event, detail_json, created_at FROM trade_journal WHERE tenant_id = ? AND agent_name = ? ORDER BY created_at DESC, rowid DESC LIMIT ?"
	tradeMemoryTimeout    = 15 * time.Second
)

// AppendTradeEvent writes one event to the D1 journal through the hash chain.
// Best-effort for low-stakes bookkeeping: a failure is logged, never fatal to
// the run (same contract as lineage). Decision-grade writes use
// AppendTradeEventWithProvenance, which propagates the error.
func AppendTradeEvent(ctx context.Context, d1 *cloudflare.D1Client, tenantID, agentName, runID, event string, detail map[string]interface{}) {
	if d1 == nil {
		return
	}
	if tenantID == "" || agentName == "" || runID == "" || event == "" {
		log.Printf("[trade] append: tenant/agent/run/event required")
		return
	}
	payload := "{}"
	if detail != nil {
		if raw, err := json.Marshal(detail); err == nil {
			payload = string(raw)
		}
	}
	tctx, cancel := context.WithTimeout(ctx, tradeMemoryTimeout)
	defer cancel()
	if err := appendChained(tctx, d1, uuid.NewString(), tenantID, agentName, runID, event, payload); err != nil {
		log.Printf("[trade] journal append %q failed: %v", event, err)
	}
}
