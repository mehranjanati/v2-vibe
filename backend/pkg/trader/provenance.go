package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"

	"github.com/google/uuid"

	"backend/pkg/cloudflare"
)

// ---------- Provenance (Phase A.1) ----------
//
// Every journal event carries WHO produced it and UNDER WHICH PINNED
// CONTRACTS. This is the defense the research literature calls for: when an
// agent's memory is the trusted surface (memory-injection is measurably more
// effective than prompt-injection), a row without provenance is not
// evidence — it is an assertion.
//
// The fields mirror the manifest schema exactly, so a journal row can be
// cross-checked against the pinned Agent/Skill/Tool manifests:
//
//	agent@1.0.0 · skill/rsi-ema-spot@1.0.0 · tool/market-data@1.0.0
//
// `Source` is the trust boundary: only values on the allowlist may write.
type TradeProvenance struct {
	// AgentRef is the pinned agent manifest ref (agent/spot-trader@1.0.0).
	AgentRef string
	// SkillRef is the pinned skill that produced the decision.
	SkillRef string
	// ToolRef is the pinned tool that produced the data/action.
	ToolRef string
	// Source names the origin of the evidence (market-data, paper-order,
	// risk-manager, operator). Anything else is rejected.
	Source string
	// ConfigVersion pins the risk/strategy configuration the decision used.
	// A missing or stale config must reject, never fall back permissive.
	ConfigVersion string
}

// tradeSourceAllowlist is the closed set of journal writers. An agent cannot
// invent a source: unknown values are rejected before the row is written.
var tradeSourceAllowlist = map[string]bool{
	"market-data":  true, // pinned market-data.tool
	"paper-order":  true, // pinned paper-order.tool
	"risk-manager": true, // pinned risk-manager.skill gate
	"strategy":     true, // pinned strategy skill decision
	"operator":     true, // human-initiated (approval, resume)
}

// manifestRefPattern is the pinned-ref grammar from manifest-schemas
// (kind/name@MAJOR.MINOR.PATCH). Floating refs are structurally rejected.
var manifestRefPattern = regexp.MustCompile(`^(agent|skill|tool)/[a-z0-9]([-a-z0-9]*[a-z0-9])?@(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// Validate rejects a provenance that cannot be trusted as evidence.
func (p TradeProvenance) Validate() error {
	if p.Source == "" {
		return fmt.Errorf("trade provenance: source is required")
	}
	if !tradeSourceAllowlist[p.Source] {
		return fmt.Errorf("trade provenance: source %q is not allowlisted", p.Source)
	}
	if p.ConfigVersion == "" {
		return fmt.Errorf("trade provenance: config_version is required (stale config must reject, never default)")
	}
	for _, ref := range []struct{ name, value string }{
		{"agent_ref", p.AgentRef},
		{"skill_ref", p.SkillRef},
		{"tool_ref", p.ToolRef},
	} {
		if ref.value == "" {
			continue // optional per event kind, but if present it must be pinned
		}
		if !manifestRefPattern.MatchString(ref.value) {
			return fmt.Errorf("trade provenance: %s %q is not a pinned kind/name@x.y.z ref", ref.name, ref.value)
		}
	}
	return nil
}

// AsDetail flattens provenance into the journal's detail_json under the
// reserved "provenance" key. Reserved so a model-supplied detail can never
// overwrite it (see AppendTradeEvent).
func (p TradeProvenance) AsDetail() map[string]interface{} {
	out := map[string]interface{}{
		"source":         p.Source,
		"config_version": p.ConfigVersion,
	}
	if p.AgentRef != "" {
		out["agent_ref"] = p.AgentRef
	}
	if p.SkillRef != "" {
		out["skill_ref"] = p.SkillRef
	}
	if p.ToolRef != "" {
		out["tool_ref"] = p.ToolRef
	}
	return out
}

// AppendTradeEventWithProvenance writes a journal event that carries the
// pinned provenance, through the hash chain. It is the ONLY append path that
// may be used for decisions and fills.
//
// Two guarantees:
//   - the provenance block always wins over a same-named model-supplied detail
//     (an agent cannot forge its own source);
//   - the row is hash-chained, so a later edit of this row or any row before it
//     is detectable by VerifyChain, and the head can be anchored in Git.
//
// A chain failure is returned: a decision that cannot be recorded immutably
// must not be treated as recorded.
func AppendTradeEventWithProvenance(ctx context.Context, d1 *cloudflare.D1Client, tenantID, agentName, runID, event string, prov TradeProvenance, detail map[string]interface{}) error {
	if err := prov.Validate(); err != nil {
		log.Printf("[trade] provenance rejected for %q: %v", event, err)
		return err
	}
	if d1 == nil {
		return nil
	}
	if tenantID == "" || agentName == "" || runID == "" || event == "" {
		return fmt.Errorf("trader: journal append: tenant/agent/run/event required")
	}
	merged := make(map[string]interface{}, len(detail)+1)
	for k, v := range detail {
		if k == "provenance" {
			continue // caller-supplied provenance is never trusted
		}
		merged[k] = v
	}
	merged["provenance"] = prov.AsDetail()
	payload := "{}"
	if raw, err := json.Marshal(merged); err == nil {
		payload = string(raw)
	}

	tctx, cancel := context.WithTimeout(ctx, tradeMemoryTimeout)
	defer cancel()
	return appendChained(tctx, d1, uuid.NewString(), tenantID, agentName, runID, event, payload)
}
