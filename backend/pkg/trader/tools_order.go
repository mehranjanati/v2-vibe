package trader

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// ---------- order-path tools (Phase B enforcement) ----------
//
// Two tools, one contract: the agent must pass the gate before it may trade,
// and the order tool refuses anything the gate did not authorize. The token is
// the seam — a plain function call cannot fake it because it is an HMAC over
// the exact intent with a key that never leaves the process.

// tradeRiskCheckInput is the model-facing schema for the pre-trade gate.
type tradeRiskCheckInput struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Quantity      float64 `json:"quantity"`
	Price         float64 `json:"price"`
	StopLoss      float64 `json:"stop_loss,omitempty"`
	ClientOrderID string  `json:"client_order_id"`
	ConfigVersion string  `json:"config_version"`
	// QuoteAgeSeconds is how old the price reference is. Required: the model
	// must state it rather than let the gate assume "just now".
	QuoteAgeSeconds int `json:"quote_age_seconds"`
}

// tradeRiskCheckOutput returns the verdict and, when approved, the token the
// order tool will demand.
type tradeRiskCheckOutput struct {
	Approved bool     `json:"approved"`
	Reason   string   `json:"reason"`
	Notional float64  `json:"notional"`
	Token    string   `json:"token,omitempty"`
	Checks   []string `json:"checks"`
}

// newTradeRiskCheckTool builds "trade_risk_check": the gate the agent must pass
// BEFORE proposing an order.
func newTradeRiskCheckTool(d Deps) (tool.InvokableTool, error) {
	return utils.InferTool("trade_risk_check",
		"Run the pre-trade risk gate for a proposed order. Returns approved=true plus an approval token, or approved=false with the blocking reason. Pass quote_age_seconds honestly: a stale price is rejected.",
		func(ctx context.Context, in tradeRiskCheckInput) (*tradeRiskCheckOutput, error) {
			id, err := tradeID(ctx)
			if err != nil {
				return nil, err
			}
			state, _ := LoadTradeState(ctx, d.D1, d.RDB, id.tenant, id.agent)
			policy := DefaultRiskPolicy()
			intent := OrderIntent{
				Symbol: in.Symbol, Side: in.Side, Quantity: in.Quantity,
				Price: in.Price, StopLoss: in.StopLoss,
				ClientOrderID: in.ClientOrderID, ConfigVersion: in.ConfigVersion,
			}
			decision := policy.CheckOrder(state, intent, time.Duration(in.QuoteAgeSeconds)*time.Second, time.Now().UTC())
			return &tradeRiskCheckOutput{
				Approved: decision.Approved,
				Reason:   decision.Reason,
				Notional: decision.Notional,
				Token:    decision.Token,
				Checks:   decision.Checks,
			}, nil
		})
}

// tradeOrderInput is the model-facing schema for order execution.
type tradeOrderInput struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Quantity      float64 `json:"quantity"`
	Price         float64 `json:"price"`
	StopLoss      float64 `json:"stop_loss,omitempty"`
	ClientOrderID string  `json:"client_order_id"`
	ConfigVersion string  `json:"config_version"`
	// Token is the value returned by trade_risk_check for THIS intent.
	Token string `json:"token"`
}

// tradeOrderOutput mirrors PaperOrderResult for the model.
type tradeOrderOutput struct {
	Status      string  `json:"status"`
	Reason      string  `json:"reason,omitempty"`
	OrderID     string  `json:"order_id,omitempty"`
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"`
	Quantity    float64 `json:"quantity"`
	Price       float64 `json:"price"`
	Notional    float64 `json:"notional"`
	EquityAfter float64 `json:"equity_after"`
}

// newTradeOrderTool builds "trade_order": the ONLY execution path. It refuses
// to fill without a token the gate issued for exactly this intent.
func newTradeOrderTool(d Deps) (tool.InvokableTool, error) {
	return utils.InferTool("trade_order",
		"Execute a PAPER order. Requires the token returned by trade_risk_check for exactly these arguments; without a valid token the order is rejected and nothing changes. Never call this before trade_risk_check.",
		func(ctx context.Context, in tradeOrderInput) (*tradeOrderOutput, error) {
			id, err := tradeID(ctx)
			if err != nil {
				return nil, err
			}
			intent := OrderIntent{
				Symbol: in.Symbol, Side: in.Side, Quantity: in.Quantity,
				Price: in.Price, StopLoss: in.StopLoss,
				ClientOrderID: in.ClientOrderID, ConfigVersion: in.ConfigVersion,
			}
			res, oerr := ExecutePaperOrder(ctx, d, DefaultRiskPolicy(), id, intent, in.Token, time.Now().UTC())
			out := &tradeOrderOutput{
				Status: res.Status, Reason: res.Reason, OrderID: res.OrderID,
				Symbol: res.Symbol, Side: res.Side, Quantity: res.Quantity,
				Price: res.Price, Notional: res.Notional, EquityAfter: res.EquityAfter,
			}
			if oerr != nil {
				return out, fmt.Errorf("trader: trade_order: %w", oerr)
			}
			return out, nil
		})
}
