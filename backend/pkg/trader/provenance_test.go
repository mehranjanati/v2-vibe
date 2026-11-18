package trader

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// ---------- Phase A.1: provenance ----------

// TestProvenanceRejectsUnknownSource: an agent cannot invent a journal source.
func TestProvenanceRejectsUnknownSource(t *testing.T) {
	p := TradeProvenance{Source: "twitter-alpha-leak", ConfigVersion: "risk@1.0.0"}
	if err := p.Validate(); err == nil {
		t.Fatal("unknown source must be rejected")
	}
	if !strings.Contains(p.Validate().Error(), "not allowlisted") {
		t.Fatalf("unexpected error: %v", p.Validate())
	}
}

// TestProvenanceRequiresConfigVersion: stale/missing config rejects rather
// than silently defaulting to a permissive policy.
func TestProvenanceRequiresConfigVersion(t *testing.T) {
	p := TradeProvenance{Source: "market-data"}
	if err := p.Validate(); err == nil {
		t.Fatal("missing config_version must be rejected")
	}
}

// TestProvenanceRejectsFloatingRef: @latest is not a pin (same rule as the
// manifest gate's negative probes).
func TestProvenanceRejectsFloatingRef(t *testing.T) {
	p := TradeProvenance{
		Source:        "strategy",
		ConfigVersion: "risk@1.0.0",
		SkillRef:      "skill/rsi-ema-spot@latest",
	}
	if err := p.Validate(); err == nil {
		t.Fatal("floating skill ref must be rejected")
	}
	p.SkillRef = "skill/rsi-ema-spot@1.0.0"
	if err := p.Validate(); err != nil {
		t.Fatalf("pinned ref must pass: %v", err)
	}
}

// TestAppendWithProvenanceCannotBeForged: a model-supplied "provenance" key in
// detail_json is overwritten by the engine's provenance, never merged.
func TestAppendWithProvenanceCannotBeForged(t *testing.T) {
	f, d1 := newFakeTradeD1(t)
	prov := TradeProvenance{
		AgentRef:      "agent/spot-trader@1.0.0",
		SkillRef:      "skill/rsi-ema-spot@1.0.0",
		Source:        "strategy",
		ConfigVersion: "risk@1.0.0",
	}
	detail := map[string]interface{}{
		"signal":     "long",
		"provenance": map[string]interface{}{"source": "binance-official-news"},
	}
	if err := AppendTradeEventWithProvenance(context.Background(), d1, "t1", "spot-trader", "run-1", TradeEventSignal, prov, detail); err != nil {
		t.Fatalf("append: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.journal) != 1 {
		t.Fatalf("expected 1 row, got %d", len(f.journal))
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(f.journal[0].detail), &payload); err != nil {
		t.Fatalf("detail not JSON: %v", err)
	}
	got, ok := payload["provenance"].(map[string]interface{})
	if !ok {
		t.Fatalf("provenance missing: %+v", payload)
	}
	if got["source"] != "strategy" {
		t.Fatalf("forged provenance survived: %v", got["source"])
	}
	if payload["signal"] != "long" {
		t.Fatalf("legit detail lost: %+v", payload)
	}
}

// TestAppendWithProvenanceRejectsBadSource writes nothing on a bad source.
func TestAppendWithProvenanceRejectsBadSource(t *testing.T) {
	f, d1 := newFakeTradeD1(t)
	prov := TradeProvenance{Source: "telegram-signal", ConfigVersion: "risk@1.0.0"}
	if err := AppendTradeEventWithProvenance(context.Background(), d1, "t1", "spot-trader", "run-1", TradeEventSignal, prov, nil); err == nil {
		t.Fatal("expected rejection")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.journal) != 0 {
		t.Fatalf("rejected event must not be written, got %d", len(f.journal))
	}
}
