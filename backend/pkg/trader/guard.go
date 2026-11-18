package trader

import (
	"fmt"
	"strings"
	"time"
)

// ---------- Market-data guard (Phase A.2) ----------
//
// The survey's "look-ahead bias" and the injection papers' "fact poisoning"
// share one root cause: evidence with no timestamp discipline and no source
// allowlist. An agent that reads a fabricated candle or a stale news item
// makes a confident, wrong trade.
//
// This guard is the runner-side rejection (not a prompt instruction): it
// classifies one piece of market evidence and returns whether it may enter a
// decision. Stale or unsourced evidence never becomes a signal.

// MarketEvidence is one observation before it is trusted.
type MarketEvidence struct {
	// Source must be on the market-source allowlist below.
	Source string
	// Symbol is the instrument the evidence describes.
	Symbol string
	// ObservedAt is when the data was actually produced by the venue
	// (candle close time / news publication time) — NOT when we fetched it.
	ObservedAt time.Time
	// FetchedAt is when our tool read it. The gap is the latency budget.
	FetchedAt time.Time
}

// marketSourceAllowlist is the closed set of venues/feeds that may produce
// market evidence. A model cannot introduce a new source by naming one.
var marketSourceAllowlist = map[string]bool{
	"binance-spot":  true,
	"coinbase-spot": true,
	"paper-sim":     true,
}

// GuardConfig pins the freshness policy for one run. It is versioned so every
// decision can record which policy accepted the evidence.
type GuardConfig struct {
	// MaxAge rejects evidence older than this (stale quote threshold).
	MaxAge time.Duration
	// MaxLatency rejects evidence whose fetch lag exceeds this (embargo).
	MaxLatency time.Duration
	// AllowedSymbols is the instrument allowlist; empty means "all".
	AllowedSymbols []string
	// Version identifies this policy in the journal (e.g. "guard@1.0.0").
	Version string
}

// DefaultGuardConfig is the paper-trading policy for 1h candles: two candles
// of age and one candle of fetch lag. Live execution must tighten both to
// seconds — the manifest pins the version, so the change is reviewable.
func DefaultGuardConfig() GuardConfig {
	return GuardConfig{
		MaxAge:         2 * time.Hour,
		MaxLatency:     1 * time.Hour,
		AllowedSymbols: []string{"BTCUSDT", "ETHUSDT"},
		Version:        "guard-1h@1.0.0",
	}
}

// GuardVerdict is the guard's decision plus a machine-readable reason. A
// rejection is always explicit: the caller turns it into `flat` with a reason,
// never into a silently ignored signal.
type GuardVerdict struct {
	Allowed bool
	Reason  string
}

func (g GuardConfig) reject(reason string) GuardVerdict {
	return GuardVerdict{Allowed: false, Reason: reason}
}

// Check classifies one piece of evidence against the pinned policy.
func (g GuardConfig) Check(ev MarketEvidence, now time.Time) GuardVerdict {
	if ev.Source == "" || !marketSourceAllowlist[ev.Source] {
		return g.reject("source-not-allowlisted")
	}
	if ev.Symbol == "" {
		return g.reject("symbol-missing")
	}
	if len(g.AllowedSymbols) > 0 {
		allowed := false
		for _, s := range g.AllowedSymbols {
			if strings.EqualFold(s, ev.Symbol) {
				allowed = true
				break
			}
		}
		if !allowed {
			return g.reject("symbol-not-allowlisted")
		}
	}
	if ev.ObservedAt.IsZero() {
		return g.reject("timestamp-missing") // look-ahead guard: undated evidence is unusable
	}
	if ev.FetchedAt.IsZero() {
		ev.FetchedAt = now
	}
	// Latency: fetch lag is the embargo window. Evidence fetched long after it
	// was produced describes a market state that may already be gone.
	if g.MaxLatency > 0 {
		if lag := ev.FetchedAt.Sub(ev.ObservedAt); lag > g.MaxLatency {
			return g.reject(fmt.Sprintf("latency-exceeded:%s", lag.Round(time.Second)))
		}
	}
	// Age: evidence older than MaxAge is stale regardless of source.
	if g.MaxAge > 0 {
		if age := now.Sub(ev.ObservedAt); age > g.MaxAge {
			return g.reject(fmt.Sprintf("stale-data:%s", age.Round(time.Second)))
		}
	}
	return GuardVerdict{Allowed: true, Reason: "ok:" + g.Version}
}

// CheckCandles guards a whole candle batch. Semantics differ from a single
// observation on purpose:
//
//   - the LATENCY budget applies to the NEWEST candle only — older candles in
//     a series are naturally older, so judging them by fetch lag would reject
//     every valid series;
//   - the AGE budget applies to the newest candle (is the series still live?);
//   - every candle must be timestamped and strictly ascending (a gap or a
//     reordering means the series is not the one the venue produced);
//   - a candle from the FUTURE is rejected outright (look-ahead bias).
//
// Any single violation rejects the batch whole: a partially trusted series
// silently shifts indicators — one fabricated candle moves RSI.
func (g GuardConfig) CheckCandles(source, symbol string, observedAt []time.Time, fetchedAt time.Time, now time.Time) GuardVerdict {
	if len(observedAt) == 0 {
		return g.reject("no-candles")
	}
	// Source/symbol allowlists first: cheapest rejection, and they do not
	// depend on the timestamps.
	head := MarketEvidence{Source: source, Symbol: symbol, ObservedAt: observedAt[len(observedAt)-1], FetchedAt: fetchedAt}
	if !marketSourceAllowlist[head.Source] {
		return g.reject("source-not-allowlisted")
	}
	if head.Symbol == "" {
		return g.reject("symbol-missing")
	}
	if len(g.AllowedSymbols) > 0 {
		allowed := false
		for _, s := range g.AllowedSymbols {
			if strings.EqualFold(s, head.Symbol) {
				allowed = true
				break
			}
		}
		if !allowed {
			return g.reject("symbol-not-allowlisted")
		}
	}

	prev := time.Time{}
	for i, ts := range observedAt {
		if ts.IsZero() {
			return g.reject("timestamp-missing")
		}
		if ts.After(now) {
			return g.reject("timestamp-in-future") // look-ahead bias
		}
		if i > 0 && !ts.After(prev) {
			return g.reject("timestamps-not-ascending")
		}
		prev = ts
	}

	// Freshness of the series = freshness of its newest candle.
	return g.Check(head, now)
}
