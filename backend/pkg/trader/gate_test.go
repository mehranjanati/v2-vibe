package trader

import (
	"strings"
	"testing"
	"time"
)

func baseIntent() OrderIntent {
	return OrderIntent{
		Symbol: "BTCUSDT", Side: "buy", Quantity: 0.01, Price: 100000,
		StopLoss: 97000, ClientOrderID: "c-1", ConfigVersion: "risk@1.0.0",
	}
}

func baseState() TradeState {
	return TradeState{Equity: 10000, DayStartEquity: 10000}
}

// TestGateApprovesSaneOrder: the happy path, with every check name recorded.
func TestGateApprovesSaneOrder(t *testing.T) {
	p := DefaultRiskPolicy()
	now := time.Now().UTC()
	d := p.CheckOrder(baseState(), baseIntent(), time.Minute, now)
	if !d.Approved {
		t.Fatalf("expected approval, got %s", d.Reason)
	}
	if d.Token == "" {
		t.Fatal("approval must carry a token")
	}
	for _, want := range []string{"policy-valid", "config-version", "kill-switch", "risk-per-trade", "gross-exposure"} {
		found := false
		for _, c := range d.Checks {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("check %q did not run (ran: %v)", want, d.Checks)
		}
	}
	if err := VerifyToken(d.Token, p.Version, baseIntent(), now); err != nil {
		t.Fatalf("issued token must verify: %v", err)
	}
}

// TestGateRejections walks the control list. Each case must reject with its own
// reason: a generic "denied" hides which control fired.
func TestGateRejections(t *testing.T) {
	now := time.Now().UTC()
	p := DefaultRiskPolicy()
	cases := []struct {
		name   string
		mutate func(*TradeState, *OrderIntent, *time.Duration, *RiskPolicy)
		want   string
	}{
		{"stale config version", func(_ *TradeState, in *OrderIntent, _ *time.Duration, _ *RiskPolicy) {
			in.ConfigVersion = "risk@0.9.0"
		}, "config-version-mismatch"},
		{"missing client order id", func(_ *TradeState, in *OrderIntent, _ *time.Duration, _ *RiskPolicy) {
			in.ClientOrderID = ""
		}, "client-order-id-required"},
		{"symbol not allowlisted", func(_ *TradeState, in *OrderIntent, _ *time.Duration, _ *RiskPolicy) {
			in.Symbol = "DOGEUSDT"
		}, "symbol-not-allowlisted"},
		{"buy without stop", func(_ *TradeState, in *OrderIntent, _ *time.Duration, _ *RiskPolicy) {
			in.StopLoss = 0
		}, "stop-loss-required"},
		{"kill switch", func(st *TradeState, _ *OrderIntent, _ *time.Duration, _ *RiskPolicy) {
			st.KillSwitch = true
		}, "kill-switch-active"},
		{"loss streak", func(st *TradeState, _ *OrderIntent, _ *time.Duration, _ *RiskPolicy) {
			st.ConsecutiveLosses = 3
		}, "loss-streak-halt"},
		{"daily loss stop", func(st *TradeState, _ *OrderIntent, _ *time.Duration, _ *RiskPolicy) {
			st.Equity = 9600 // -4% vs 10000
		}, "daily-loss-stop"},
		{"stale quote", func(_ *TradeState, _ *OrderIntent, age *time.Duration, _ *RiskPolicy) {
			*age = 3 * time.Hour
		}, "stale-quote"},
		{"position exists", func(st *TradeState, _ *OrderIntent, _ *time.Duration, _ *RiskPolicy) {
			st.Positions = []TradePosition{{Symbol: "BTCUSDT", Quantity: 0.01, Entry: 100000}}
		}, "position-exists"},
		{"max open positions", func(st *TradeState, _ *OrderIntent, _ *time.Duration, _ *RiskPolicy) {
			st.Positions = []TradePosition{
				{Symbol: "ETHUSDT", Quantity: 0.001, Entry: 3000},
				{Symbol: "SOLUSDT", Quantity: 0.001, Entry: 100},
				{Symbol: "XRPUSDT", Quantity: 0.001, Entry: 1},
			}
		}, "max-open-positions"},
		{"position cap", func(_ *TradeState, in *OrderIntent, _ *time.Duration, _ *RiskPolicy) {
			in.Quantity = 0.05 // 5000 notional > 20% of 10000
		}, "position-cap"},
		{"risk per trade", func(_ *TradeState, in *OrderIntent, _ *time.Duration, _ *RiskPolicy) {
			// 2000 notional (exactly the 20% cap, so it passes) but a 6% stop
			// distance puts 120 at risk against a 100 cap.
			in.Quantity = 0.02
			in.StopLoss = 94000
		}, "risk-per-trade-exceeded"},
		{"stop above entry", func(_ *TradeState, in *OrderIntent, _ *time.Duration, _ *RiskPolicy) {
			in.StopLoss = 101000
		}, "stop-loss-above-entry"},
		{"invalid policy", func(_ *TradeState, _ *OrderIntent, _ *time.Duration, pol *RiskPolicy) {
			pol.Version = ""
		}, "policy-invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, in, age := baseState(), baseIntent(), time.Minute
			pol := p
			tc.mutate(&st, &in, &age, &pol)
			d := pol.CheckOrder(st, in, age, now)
			if d.Approved {
				t.Fatalf("expected rejection (%s), got approval", tc.want)
			}
			if !strings.HasPrefix(d.Reason, tc.want) {
				t.Fatalf("expected reason %q, got %q", tc.want, d.Reason)
			}
			if d.Token != "" {
				t.Fatal("a rejected decision must never carry a token")
			}
		})
	}
}

// TestTokenIsBoundToIntent: an approval cannot be widened after the fact.
func TestTokenIsBoundToIntent(t *testing.T) {
	p := DefaultRiskPolicy()
	now := time.Now().UTC()
	d := p.CheckOrder(baseState(), baseIntent(), time.Minute, now)

	wider := baseIntent()
	wider.Quantity = 0.5 // same token, 50x the size
	if err := VerifyToken(d.Token, p.Version, wider, now); err == nil {
		t.Fatal("token must not authorize a different quantity")
	}
	otherSymbol := baseIntent()
	otherSymbol.Symbol = "ETHUSDT"
	if err := VerifyToken(d.Token, p.Version, otherSymbol, now); err == nil {
		t.Fatal("token must not authorize a different symbol")
	}
	if err := VerifyToken(d.Token, "risk@2.0.0", baseIntent(), now); err == nil {
		t.Fatal("token must not authorize under a different policy version")
	}
	// Expired tokens are refused.
	if err := VerifyToken(d.Token, p.Version, baseIntent(), now.Add(tokenTTL+time.Minute)); err == nil {
		t.Fatal("expired token must be refused")
	}
	// A forged MAC is refused.
	forged := d.Token[:strings.Index(d.Token, ".")+1] + strings.Repeat("a", 64)
	if err := VerifyToken(forged, p.Version, baseIntent(), now); err == nil {
		t.Fatal("forged MAC must be refused")
	}
}
