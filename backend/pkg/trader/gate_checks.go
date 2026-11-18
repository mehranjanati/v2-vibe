package trader

import (
	"fmt"
	"strings"
	"time"
)

// ---------- the checks ----------

// CheckOrder runs every pre-trade control and returns the first failure.
//
// It is deliberately a pure function of (policy, state, intent, quote age): the
// same inputs always produce the same verdict, which is what makes a rejection
// reviewable after the fact.
func (p RiskPolicy) CheckOrder(state TradeState, in OrderIntent, quoteAge time.Duration, now time.Time) RiskDecision {
	d := &RiskDecision{Notional: in.Quantity * in.Price}
	ran := func(name string) { d.Checks = append(d.Checks, name) }

	// 1. The policy itself must be trustworthy.
	ran("policy-valid")
	if err := p.Validate(); err != nil {
		return d.reject("policy-invalid:" + err.Error())
	}
	// 2. A stale configuration must reject, never be upgraded silently.
	ran("config-version")
	if in.ConfigVersion != p.Version {
		return d.reject("config-version-mismatch:" + in.ConfigVersion + "!=" + p.Version)
	}
	// 3. Identity for retry safety.
	ran("client-order-id")
	if strings.TrimSpace(in.ClientOrderID) == "" {
		return d.reject("client-order-id-required")
	}
	// 4. Instrument allowlist: an unknown symbol is a mapping bug, not a trade.
	ran("symbol-allowlist")
	if len(p.AllowedSymbols) > 0 && !containsFold(p.AllowedSymbols, in.Symbol) {
		return d.reject("symbol-not-allowlisted:" + in.Symbol)
	}
	// 5. Shape: a non-positive quantity or price cannot be sized or priced.
	ran("intent-shape")
	if in.Quantity <= 0 || in.Price <= 0 {
		return d.reject("intent-invalid-size")
	}
	if in.Side != "buy" && in.Side != "sell" {
		return d.reject("intent-invalid-side:" + in.Side)
	}
	if in.Side == "buy" && in.StopLoss <= 0 {
		return d.reject("stop-loss-required")
	}
	// 6. Kill-switch: a tripped switch blocks new entries until a human clears it.
	ran("kill-switch")
	if state.KillSwitch && in.Side == "buy" {
		return d.reject("kill-switch-active")
	}
	// 7. Losing streak.
	ran("loss-streak")
	if in.Side == "buy" && state.ConsecutiveLosses >= p.MaxConsecutiveLosses {
		return d.reject("loss-streak-halt")
	}
	// 8. Daily loss stop against the day's opening equity.
	ran("daily-loss-stop")
	if in.Side == "buy" && state.DayStartEquity > 0 && state.Equity > 0 {
		loss := (state.Equity - state.DayStartEquity) / state.DayStartEquity
		if loss <= p.DailyLossStop {
			return d.reject(fmt.Sprintf("daily-loss-stop:%.4f", loss))
		}
	}
	// 9. Stale quote: priced off data older than the policy allows.
	ran("stale-quote")
	if p.StaleQuoteMaxAge > 0 && quoteAge > p.StaleQuoteMaxAge {
		return d.reject(fmt.Sprintf("stale-quote:%s", quoteAge.Round(time.Second)))
	}

	// Exposure checks only constrain OPENING; a sell reduces exposure.
	if in.Side == "buy" {
		grossNotional, count, sameSymbol := positionStats(state.Positions, in.Symbol)
		// 10. One position per symbol.
		ran("one-per-symbol")
		if sameSymbol {
			return d.reject("position-exists:" + in.Symbol)
		}
		// 11. Open-position count.
		ran("max-open-positions")
		if count >= p.MaxOpenPositions {
			return d.reject("max-open-positions")
		}
		// 12. Per-position notional cap.
		ran("position-cap")
		if state.Equity > 0 && d.Notional > p.MaxPositionPct*state.Equity {
			return d.reject(fmt.Sprintf("position-cap:%.2f>%.2f", d.Notional, p.MaxPositionPct*state.Equity))
		}
		// 13. Gross exposure after this order.
		ran("gross-exposure")
		if state.Equity > 0 && grossNotional+d.Notional > p.MaxGrossExposure*state.Equity {
			return d.reject(fmt.Sprintf("gross-exposure:%.2f>%.2f", grossNotional+d.Notional, p.MaxGrossExposure*state.Equity))
		}
		// 14. Risk-to-stop: the only sizing rule that ties size to the thesis.
		ran("risk-per-trade")
		riskPerUnit := in.Price - in.StopLoss
		if riskPerUnit <= 0 {
			return d.reject("stop-loss-above-entry")
		}
		if state.Equity > 0 {
			riskAmount := in.Quantity * riskPerUnit
			if riskAmount > p.MaxRiskPerTrade*state.Equity {
				return d.reject(fmt.Sprintf("risk-per-trade-exceeded:%.2f>%.2f", riskAmount, p.MaxRiskPerTrade*state.Equity))
			}
		}
	}

	d.Approved = true
	d.Reason = "approved:" + p.Version
	d.Token = issueToken(p.Version, in, now)
	return *d
}

func (d *RiskDecision) reject(reason string) RiskDecision {
	d.Approved = false
	d.Reason = reason
	d.Token = ""
	return *d
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}

// positionStats returns open notional (valued at entry — conservative, never at
// a hoped-for mark), the open position count, and whether `symbol` is already
// open.
func positionStats(positions []TradePosition, symbol string) (notional float64, count int, sameSymbol bool) {
	for _, pos := range positions {
		price := pos.Entry
		if price <= 0 {
			price = pos.TakeProfit
		}
		notional += pos.Quantity * price
		count++
		if strings.EqualFold(pos.Symbol, symbol) {
			sameSymbol = true
		}
	}
	return notional, count, sameSymbol
}
