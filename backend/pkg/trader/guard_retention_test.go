package trader

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ---------- Phase A.2: market guard ----------

// TestGuardRejectsStaleAndUnsourcedEvidence covers the two look-ahead/
// injection failure modes: undated evidence and stale evidence.
func TestGuardRejectsStaleAndUnsourcedEvidence(t *testing.T) {
	g := DefaultGuardConfig()
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

	if v := g.Check(MarketEvidence{Source: "", Symbol: "BTCUSDT", ObservedAt: now}, now); v.Allowed || v.Reason != "source-not-allowlisted" {
		t.Fatalf("empty source: %+v", v)
	}
	if v := g.Check(MarketEvidence{Source: "random-blog", Symbol: "BTCUSDT", ObservedAt: now}, now); v.Allowed {
		t.Fatal("non-allowlisted source must be rejected")
	}
	if v := g.Check(MarketEvidence{Source: "binance-spot", Symbol: "DOGEUSDT", ObservedAt: now}, now); v.Allowed || v.Reason != "symbol-not-allowlisted" {
		t.Fatalf("symbol allowlist: %+v", v)
	}
	if v := g.Check(MarketEvidence{Source: "binance-spot", Symbol: "BTCUSDT"}, now); v.Allowed || v.Reason != "timestamp-missing" {
		t.Fatalf("undated evidence: %+v", v)
	}
	stale := now.Add(-5 * time.Hour)
	if v := g.Check(MarketEvidence{Source: "binance-spot", Symbol: "BTCUSDT", ObservedAt: stale, FetchedAt: stale}, now); v.Allowed || !strings.HasPrefix(v.Reason, "stale-data") {
		t.Fatalf("stale evidence: %+v", v)
	}
}

// TestGuardEnforcesLatencyBudget: evidence fetched long after it was produced
// is an embargo violation even when it is not old in absolute terms.
func TestGuardEnforcesLatencyBudget(t *testing.T) {
	g := DefaultGuardConfig()
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	observed := now.Add(-90 * time.Minute)
	fetched := observed.Add(2 * time.Hour) // lag > MaxLatency (1h)
	if v := g.Check(MarketEvidence{Source: "binance-spot", Symbol: "BTCUSDT", ObservedAt: observed, FetchedAt: fetched}, now); v.Allowed || !strings.HasPrefix(v.Reason, "latency-exceeded") {
		t.Fatalf("latency budget: %+v", v)
	}
	okFetched := observed.Add(10 * time.Minute)
	if v := g.Check(MarketEvidence{Source: "binance-spot", Symbol: "BTCUSDT", ObservedAt: observed, FetchedAt: okFetched}, now); !v.Allowed {
		t.Fatalf("fresh evidence must pass: %+v", v)
	}
}

// TestGuardCandlesAllOrNothing: the series rules are all-or-nothing — a gap,
// a reordered series, an undated candle or a future candle rejects the batch.
func TestGuardCandlesAllOrNothing(t *testing.T) {
	g := DefaultGuardConfig()
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	newest := now.Add(-30 * time.Minute)
	fetched := now.Add(-time.Minute)
	good := []time.Time{newest.Add(-2 * time.Hour), newest.Add(-time.Hour), newest}
	if v := g.CheckCandles("binance-spot", "BTCUSDT", good, fetched, now); !v.Allowed {
		t.Fatalf("fresh ascending series must pass: %+v", v)
	}
	// A series whose NEWEST candle is stale (the feed stopped living). The
	// fetch is stamped at the candle itself so the latency budget is not the
	// trigger — age must be what rejects it.
	stale := []time.Time{now.Add(-8 * time.Hour), now.Add(-7 * time.Hour)}
	staleFetched := now.Add(-7 * time.Hour)
	if v := g.CheckCandles("binance-spot", "BTCUSDT", stale, staleFetched, now); v.Allowed || !strings.HasPrefix(v.Reason, "stale-data") {
		t.Fatalf("stale series must be rejected: %+v", v)
	}
	// Reordered series: the venue never produced this order.
	reordered := []time.Time{newest, newest.Add(-time.Hour)}
	if v := g.CheckCandles("binance-spot", "BTCUSDT", reordered, fetched, now); v.Allowed || v.Reason != "timestamps-not-ascending" {
		t.Fatalf("reordered series: %+v", v)
	}
	// Future candle: look-ahead bias.
	future := []time.Time{newest, now.Add(time.Hour)}
	if v := g.CheckCandles("binance-spot", "BTCUSDT", future, fetched, now); v.Allowed || v.Reason != "timestamp-in-future" {
		t.Fatalf("future candle: %+v", v)
	}
	// Undated candle.
	undated := []time.Time{newest, {}}
	if v := g.CheckCandles("binance-spot", "BTCUSDT", undated, fetched, now); v.Allowed || v.Reason != "timestamp-missing" {
		t.Fatalf("undated candle: %+v", v)
	}
	if v := g.CheckCandles("binance-spot", "BTCUSDT", nil, now, now); v.Allowed || v.Reason != "no-candles" {
		t.Fatalf("empty batch: %+v", v)
	}
}

// ---------- Phase A.3: retention ----------

// TestSweepTradeMemoryIssuesTenantScopedDelete: the sweep always pins
// tenant+agent and fails closed without identity.
func TestSweepTradeMemoryIssuesTenantScopedDelete(t *testing.T) {
	_, d1 := newFakeTradeD1(t)
	if n, err := SweepTradeMemory(context.Background(), d1, "tenant-a", "spot-trader", 90, 1000); err != nil || n == 0 {
		t.Fatalf("sweep: n=%d err=%v", n, err)
	}
	// Empty identity fails closed: no statement is issued.
	if n, err := SweepTradeMemory(context.Background(), d1, "", "spot-trader", 90, 1000); err != nil || n != 0 {
		t.Fatalf("empty tenant must be a no-op: n=%d err=%v", n, err)
	}
	// Retention disabled is an explicit opt-out, not a silent unlock.
	if n, err := SweepTradeMemory(context.Background(), d1, "tenant-a", "spot-trader", 0, 1000); err != nil || n != 0 {
		t.Fatalf("retention 0 must be a no-op: n=%d err=%v", n, err)
	}
}

// TestSweepSQLIsParameterized: the cutoff and identity never reach SQL as
// caller-built literal text (CF_LIMITS §8).
func TestSweepSQLIsParameterized(t *testing.T) {
	for _, sql := range []string{tradeJournalSweepSQL, tradeStateSweepSQL} {
		if !strings.Contains(sql, "tenant_id = ?") || !strings.Contains(sql, "agent_name = ?") {
			t.Fatalf("sweep statement must pin identity as bind params: %s", sql)
		}
	}
	if strings.Contains(tradeJournalSweepSQL, "LIMIT 1000") {
		t.Fatal("limit must be a bind param, not a literal")
	}
}
