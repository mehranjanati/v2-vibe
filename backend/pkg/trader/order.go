package trader

import (
	"context"
	"fmt"
	"time"
)

// ---------- paper order path (Phase B) ----------
//
// This is the enforcement point: an order executes ONLY with a token the gate
// issued for exactly this intent. A model that "decides" to trade without
// asking the gate gets an error, not a fill.
//
// It is paper by construction — it fills at the intent's reference price and
// never touches a venue. Real execution needs a live OrderExecutor with a real
// idempotency key and vault-held credentials; the gate and the token contract
// stay identical, which is the point of splitting them.

// PaperOrderResult is the outcome of an approved paper order.
type PaperOrderResult struct {
	Status      string  `json:"status"` // filled | rejected
	Reason      string  `json:"reason,omitempty"`
	OrderID     string  `json:"order_id,omitempty"`
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"`
	Quantity    float64 `json:"quantity"`
	Price       float64 `json:"price"`
	Notional    float64 `json:"notional"`
	EquityAfter float64 `json:"equity_after"`
}

// ExecutePaperOrder runs one order through the full path:
//
//	verify token → (paper) fill → update state → append provenanced journal row
//
// The token check happens BEFORE any state change, so a rejected order cannot
// leave a half-applied position behind.
func ExecutePaperOrder(ctx context.Context, d Deps, policy RiskPolicy, id Identity, in OrderIntent, token string, now time.Time) (PaperOrderResult, error) {
	res := PaperOrderResult{Symbol: in.Symbol, Side: in.Side, Quantity: in.Quantity, Price: in.Price, Notional: in.Quantity * in.Price}
	if err := VerifyToken(token, policy.Version, in, now); err != nil {
		res.Status = "rejected"
		res.Reason = err.Error()
		return res, err
	}

	state, _ := LoadTradeState(ctx, d.D1, d.RDB, id.tenant, id.agent)
	res.EquityAfter = state.Equity

	switch in.Side {
	case "buy":
		cost := in.Quantity * in.Price
		if state.Equity > 0 && cost > state.Equity {
			res.Status = "rejected"
			res.Reason = "insufficient-equity"
			return res, fmt.Errorf("trader: paper order: %s", res.Reason)
		}
		state.Positions = append(state.Positions, TradePosition{
			Symbol:     in.Symbol,
			Side:       "buy",
			Quantity:   in.Quantity,
			Entry:      in.Price,
			StopLoss:   in.StopLoss,
			TakeProfit: 0,
		})
		state.Equity -= cost
	case "sell":
		idx := -1
		for i, p := range state.Positions {
			if p.Symbol == in.Symbol {
				idx = i
				break
			}
		}
		if idx < 0 {
			res.Status = "rejected"
			res.Reason = "no-open-position"
			return res, fmt.Errorf("trader: paper order: %s", res.Reason)
		}
		pos := state.Positions[idx]
		// Cash accounting: the entry cost was already debited when the position
		// opened, so closing returns only the sale proceeds. P&L is derived for
		// the loss streak, never added on top of the proceeds (that would double
		// count the loss).
		pnl := (in.Price - pos.Entry) * in.Quantity
		state.Equity += in.Quantity * in.Price
		if pnl < 0 {
			state.ConsecutiveLosses++
		} else {
			state.ConsecutiveLosses = 0
		}
		state.Positions = append(state.Positions[:idx], state.Positions[idx+1:]...)
	}

	res.Status = "filled"
	res.OrderID = "paper-" + in.ClientOrderID
	res.EquityAfter = state.Equity

	SaveTradeState(ctx, d.D1, d.RDB, id.tenant, id.agent, id.runID, state)
	prov := TradeProvenance{
		AgentRef:      "agent/" + id.agent + "@" + AgentVersion(id.agent),
		ToolRef:       "tool/paper-order@1.0.0",
		Source:        "paper-order",
		ConfigVersion: policy.Version,
	}
	if err := AppendTradeEventWithProvenance(ctx, d.D1, id.tenant, id.agent, id.runID, TradeEventFill, prov, map[string]interface{}{
		"symbol":          in.Symbol,
		"side":            in.Side,
		"quantity":        in.Quantity,
		"price":           in.Price,
		"client_order_id": in.ClientOrderID,
		"equity_after":    state.Equity,
	}); err != nil {
		// The fill happened in memory but could not be sealed. Surfacing the
		// error is the only honest option: the caller must not believe the
		// trade is durably recorded.
		return res, fmt.Errorf("trader: paper order recorded but journal append failed: %w", err)
	}
	AnchorHead(ctx, d.D1, d.Anchor, id.tenant, id.agent)
	return res, nil
}

// Identity is the caller identity bound to a run by the engine. Fields are
// unexported on purpose: only the engine can construct it (WithTradeContext),
// never the model.
type Identity struct {
	tenant string
	agent  string
	runID  string
}

// Tenant/Agent/RunID expose the bound identity to callers that need to report
// or audit it. They are read-only views, not constructors.
func (i Identity) Tenant() string { return i.tenant }
func (i Identity) Agent() string  { return i.agent }
func (i Identity) RunID() string  { return i.runID }
