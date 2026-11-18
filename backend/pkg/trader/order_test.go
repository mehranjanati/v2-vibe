package trader

import (
	"context"
	"strings"
	"testing"
	"time"
)

func testIdentity() Identity {
	return Identity{tenant: "t1", agent: "spot-trader", runID: "run-1"}
}

// TestPaperOrderRequiresToken: without a gate-issued token nothing executes and
// the state is untouched — the enforcement point, not advice.
func TestPaperOrderRequiresToken(t *testing.T) {
	_, d1 := newFakeTradeD1(t)
	d := Deps{D1: d1}
	ctx := context.Background()
	id := testIdentity()
	now := time.Now().UTC()
	SaveTradeState(ctx, d1, nil, id.tenant, id.agent, id.runID, baseState())

	res, err := ExecutePaperOrder(ctx, d, DefaultRiskPolicy(), id, baseIntent(), "", now)
	if err == nil {
		t.Fatal("an order without a token must fail")
	}
	if res.Status != "rejected" || !strings.Contains(res.Reason, "not risk-approved") {
		t.Fatalf("unexpected rejection: %+v", res)
	}
	state, _ := LoadTradeState(ctx, d1, nil, id.tenant, id.agent)
	if len(state.Positions) != 0 || state.Equity != 10000 {
		t.Fatalf("rejected order must not change state: %+v", state)
	}
}

// TestPaperOrderFillsWithValidToken: approved intent → position, equity and a
// provenanced, chained fill event.
func TestPaperOrderFillsWithValidToken(t *testing.T) {
	f, d1 := newFakeTradeD1(t)
	d := Deps{D1: d1}
	ctx := context.Background()
	id := testIdentity()
	now := time.Now().UTC()
	SaveTradeState(ctx, d1, nil, id.tenant, id.agent, id.runID, baseState())

	in := baseIntent()
	pol := DefaultRiskPolicy()
	decision := pol.CheckOrder(baseState(), in, time.Minute, now)
	if !decision.Approved {
		t.Fatalf("setup: %s", decision.Reason)
	}

	res, err := ExecutePaperOrder(ctx, d, pol, id, in, decision.Token, now)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Status != "filled" || res.OrderID != "paper-c-1" {
		t.Fatalf("unexpected result: %+v", res)
	}
	state, _ := LoadTradeState(ctx, d1, nil, id.tenant, id.agent)
	if len(state.Positions) != 1 || state.Positions[0].Symbol != "BTCUSDT" {
		t.Fatalf("position not recorded: %+v", state.Positions)
	}
	if state.Equity != 9000 { // 10000 - 0.01*100000
		t.Fatalf("equity not debited: %.2f", state.Equity)
	}
	// The fill is in the journal, provenanced and chained.
	f.mu.Lock()
	rows := append([]fakeTradeJournalRow(nil), f.journal...)
	f.mu.Unlock()
	if len(rows) != 1 || rows[0].event != TradeEventFill {
		t.Fatalf("expected one fill event, got %+v", rows)
	}
	if !strings.Contains(rows[0].detail, `"source":"paper-order"`) {
		t.Fatalf("fill must carry provenance: %s", rows[0].detail)
	}
	if rows[0].hash == "" {
		t.Fatal("fill must be hash-chained")
	}
	if st, _ := VerifyChain(ctx, d1, id.tenant, id.agent, 100); !st.OK {
		t.Fatalf("chain broken after fill: %+v", st)
	}
}

// TestPaperSellClosesPositionAndTracksLoss: a losing close increments the losing
// streak, which is what the next gate check reads.
func TestPaperSellClosesPositionAndTracksLoss(t *testing.T) {
	_, d1 := newFakeTradeD1(t)
	d := Deps{D1: d1}
	ctx := context.Background()
	id := testIdentity()
	now := time.Now().UTC()

	SaveTradeState(ctx, d1, nil, id.tenant, id.agent, id.runID, TradeState{
		Equity: 9000, DayStartEquity: 10000,
		Positions: []TradePosition{{Symbol: "BTCUSDT", Side: "buy", Quantity: 0.01, Entry: 100000}},
	})
	sell := OrderIntent{Symbol: "BTCUSDT", Side: "sell", Quantity: 0.01, Price: 97000, ClientOrderID: "c-2", ConfigVersion: "risk@1.0.0"}
	pol := DefaultRiskPolicy()
	decision := pol.CheckOrder(TradeState{Equity: 9000, DayStartEquity: 10000}, sell, time.Minute, now)
	if !decision.Approved {
		t.Fatalf("setup: %s", decision.Reason)
	}
	if _, err := ExecutePaperOrder(ctx, d, pol, id, sell, decision.Token, now); err != nil {
		t.Fatalf("execute: %v", err)
	}
	state, _ := LoadTradeState(ctx, d1, nil, id.tenant, id.agent)
	if len(state.Positions) != 0 {
		t.Fatalf("position must be closed: %+v", state.Positions)
	}
	if state.ConsecutiveLosses != 1 {
		t.Fatalf("losing close must count, got %d", state.ConsecutiveLosses)
	}
	// Entry cost was 1000 (debited when opened); the close returns 970.
	// 10000 - 1000 + 970 = 9970 — the loss appears once, not twice.
	if state.Equity != 9970 {
		t.Fatalf("cash accounting wrong, equity=%.2f want 9970", state.Equity)
	}
}

// TestPaperOrderRejectsWidenedIntent: a token approved for 0.01 cannot execute
// a 0.5 order — the exact forgery the MAC exists to stop.
func TestPaperOrderRejectsWidenedIntent(t *testing.T) {
	_, d1 := newFakeTradeD1(t)
	d := Deps{D1: d1}
	ctx := context.Background()
	id := testIdentity()
	now := time.Now().UTC()
	SaveTradeState(ctx, d1, nil, id.tenant, id.agent, id.runID, baseState())

	pol := DefaultRiskPolicy()
	approved := baseIntent()
	decision := pol.CheckOrder(baseState(), approved, time.Minute, now)
	if !decision.Approved {
		t.Fatalf("setup: %s", decision.Reason)
	}
	widened := approved
	widened.Quantity = 0.5
	if _, err := ExecutePaperOrder(ctx, d, pol, id, widened, decision.Token, now); err == nil {
		t.Fatal("widened intent must be refused")
	}
	state, _ := LoadTradeState(ctx, d1, nil, id.tenant, id.agent)
	if len(state.Positions) != 0 {
		t.Fatal("refused order must not open a position")
	}
}
