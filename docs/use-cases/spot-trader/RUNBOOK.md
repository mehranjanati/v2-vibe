# RUNBOOK — spot-trader (paper)

> One page an operator can follow without reading the code. Every control below
> names the component that enforces it and the check that proves it fires.
> **Paper only:** nothing here touches real funds.
> **Status:** risk gate + order path implemented and unit-tested; no live feed
> and no venue reconciliation yet. Last reviewed: 2026-11-18.

## 1. Pre-flight (before every session)

| Step | Command / action | Pass condition |
|---|---|---|
| Manifest closure valid | `node scripts/validate-manifests.mjs` | `manifests: OK` |
| Gate + order tests green | `cd backend && go test ./pkg/trader/ -count=1` | `ok` |
| Journal chain intact | `trade_resume` call at run start | `chain_ok: true` |
| No kill-switch latched | `trade_resume` → `state.kill_switch` | `false` |
| Day baseline set | `state.day_start_equity` present | non-zero |

If `trade_resume` fails with `memory-tampered`, the run **must not proceed**.
Go to §4.

## 2. Live controls and where they live

| Control | Value | Enforced by | Proof |
|---|---|---|---|
| Config version pinned | `risk@1.0.0` | `RiskPolicy.CheckOrder` → `config-version-mismatch` | `gate_test.go` |
| Instrument allowlist | BTCUSDT, ETHUSDT | `symbol-allowlist` | `gate_test.go` |
| Kill-switch blocks entries | latched boolean | `kill-switch-active` | `gate_test.go` |
| Losing streak halt | 3 losses | `loss-streak-halt` | `gate_test.go` |
| Daily loss stop | −3% vs day start | `daily-loss-stop` | `gate_test.go` |
| Stale quote | > 2h | `stale-quote` | `gate_test.go` |
| One position per symbol | — | `position-exists` | `gate_test.go` |
| Max open positions | 3 | `max-open-positions` | `gate_test.go` |
| Per-position cap | 20% equity | `position-cap` | `gate_test.go` |
| Gross exposure | 60% equity | `gross-exposure` | `gate_test.go` |
| Risk per trade | 1% equity to stop | `risk-per-trade-exceeded` | `gate_test.go` |
| Stop required on entry | > 0 and below entry | `stop-loss-required`, `stop-loss-above-entry` | `gate_test.go` |
| Order needs approval | HMAC token over the exact intent | `VerifyToken` in `ExecutePaperOrder` | `order_test.go` |
| Journal immutable afterwards | hash chain + Git anchor | `VerifyChain`, `AnchorHead` | `chain_test.go` |

## 3. Daily/weekly operating rhythm

1. **Run start:** `trade_resume` (verifies chain, returns state + journal).
2. **Per signal:** `market-data` → guard (`source`/`timestamp`/`latency`) →
   `trade_risk_check` → only then `trade_order` with the token.
3. **Run end:** `trade_checkpoint` with `source`, `config_version`, final state.
   This appends the provenanced journal row and seals the chain head.
4. **Weekly:** review the journal for rejected orders. A high rejection rate is
   information — it means the strategy is proposing what the risk policy forbids,
   and one of the two is wrong.

## 4. Incident responses

| Symptom | Meaning | Action |
|---|---|---|
| `memory-tampered (row-hash-mismatch at row N)` | A journal row was edited after the fact | **Stop trading.** Export rows ≥ N, compare with the anchored commit (`trade-<tenant>-spot-trader` repo), restore from the anchor, then investigate write access to D1 |
| `link-broken` | A row was deleted | Same as above; the deleted row is recoverable from the anchor snapshot |
| `route-hash-missing` on an old row | Row predates the chain | Expected only for pre-A2 data; do not trade on it until re-anchored |
| `capability-not-approved` from an order | The model tried to trade without the gate | Expected behaviour; log it, do not "fix" by relaxing the gate |
| `kill-switch-active` | Drawdown or loss streak tripped | Human review required (`human-review` approval). The agent cannot self-resume |
| Anchor commit fails repeatedly | D1/Git write path degraded | Rows still land; trading may continue **only** if the operator accepts an expanding unanchored window. Record the decision |

## 5. What this runbook does NOT cover

| Gap | Why it matters |
|---|---|
| Venue reconciliation | Restart safety is claimed by `client_order_id` idempotency and proven by unit tests, but no drill against a venue has run |
| Real slippage / liquidity | Paper fills at the reference price; the simulator cannot show either |
| Scheduled retention sweep | `SweepTradeMemory` exists, nothing calls it yet |
| Monitoring / alerting | No heartbeat, no alert path; today the operator is the monitor |

Do not run live capital until the first three have an owner and a proof.
