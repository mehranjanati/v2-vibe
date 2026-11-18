package trader

import (
	"fmt"
	"strings"
	"time"
)

// ---------- Risk gate (Phase B) ----------
//
// The research consensus is blunt: "a prompt is a suggestion; a runner-side
// check is a rejection." `risk-manager.skill` tells the model what the limits
// are; this file is what makes them true.
//
// The gate REJECTS — it never silently clamps. A clamped order still trades a
// size the model did not reason about, which is how "1% risk" becomes "whatever
// the gate allowed". Every rejection carries a machine-readable reason that is
// recorded, so a blocked trade is evidence rather than a silent no-op.

// RiskPolicy is the versioned, reviewable limit set. It is code, not prose:
// changing a limit is a code change with a version bump, which is what makes a
// decision reproducible ("this trade was allowed under risk@1.0.0").
type RiskPolicy struct {
	// Version is required. An order carrying a different version is rejected:
	// a stale config must never be silently upgraded to the current one.
	Version string
	// MaxRiskPerTrade is the fraction of equity risked to the stop (e.g. 0.01).
	MaxRiskPerTrade float64
	// MaxPositionPct caps one position's notional as a fraction of equity.
	MaxPositionPct float64
	// MaxGrossExposure caps the sum of open notional as a fraction of equity.
	MaxGrossExposure float64
	// MaxOpenPositions caps how many symbols may be open at once.
	MaxOpenPositions int
	// DailyLossStop is a negative fraction (e.g. -0.03) measured against the
	// day's starting equity.
	DailyLossStop float64
	// MaxConsecutiveLosses halts new entries after a losing streak.
	MaxConsecutiveLosses int
	// StaleQuoteMaxAge rejects an order priced off a stale reference.
	StaleQuoteMaxAge time.Duration
	// AllowedSymbols is the instrument allowlist; empty means "all".
	AllowedSymbols []string
}

// DefaultRiskPolicy mirrors risk-manager.skill@1.0.0 exactly, so the prose and
// the enforcement cannot drift apart unnoticed.
func DefaultRiskPolicy() RiskPolicy {
	return RiskPolicy{
		Version:              "risk@1.0.0",
		MaxRiskPerTrade:      0.01,
		MaxPositionPct:       0.20,
		MaxGrossExposure:     0.60,
		MaxOpenPositions:     3,
		DailyLossStop:        -0.03,
		MaxConsecutiveLosses: 3,
		StaleQuoteMaxAge:     2 * time.Hour,
		AllowedSymbols:       []string{"BTCUSDT", "ETHUSDT"},
	}
}

// Validate rejects a policy that would silently disable a control: an empty
// version, a non-positive cap or an unreadable loss stop.
func (p RiskPolicy) Validate() error {
	if strings.TrimSpace(p.Version) == "" {
		return fmt.Errorf("risk policy: version is required")
	}
	if p.MaxRiskPerTrade <= 0 || p.MaxRiskPerTrade > 0.05 {
		return fmt.Errorf("risk policy: MaxRiskPerTrade must be in (0, 0.05]")
	}
	if p.MaxPositionPct <= 0 || p.MaxPositionPct > 1 {
		return fmt.Errorf("risk policy: MaxPositionPct must be in (0, 1]")
	}
	if p.MaxGrossExposure <= 0 || p.MaxGrossExposure > 3 {
		return fmt.Errorf("risk policy: MaxGrossExposure must be in (0, 3]")
	}
	if p.MaxOpenPositions <= 0 {
		return fmt.Errorf("risk policy: MaxOpenPositions must be positive")
	}
	if p.DailyLossStop >= 0 || p.DailyLossStop < -1 {
		return fmt.Errorf("risk policy: DailyLossStop must be in [-1, 0)")
	}
	if p.MaxConsecutiveLosses <= 0 {
		return fmt.Errorf("risk policy: MaxConsecutiveLosses must be positive")
	}
	if p.StaleQuoteMaxAge <= 0 {
		return fmt.Errorf("risk policy: StaleQuoteMaxAge must be positive")
	}
	return nil
}

// OrderIntent is a proposed order before execution.
type OrderIntent struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"` // buy | sell
	Quantity      float64 `json:"quantity"`
	Price         float64 `json:"price"`           // reference price for notional
	StopLoss      float64 `json:"stop_loss"`       // required for a buy
	ClientOrderID string  `json:"client_order_id"` // idempotency key
	ConfigVersion string  `json:"config_version"`  // policy the caller reasoned under
}

// RiskDecision is the gate's verdict plus the checks that ran, so an audit can
// show what was proven rather than just what was refused.
type RiskDecision struct {
	Approved bool     `json:"approved"`
	Reason   string   `json:"reason"`
	Notional float64  `json:"notional"`
	Checks   []string `json:"checks"`
	// Token authorizes exactly this intent under this policy version. It is
	// empty when Approved is false.
	Token string `json:"token,omitempty"`
}
