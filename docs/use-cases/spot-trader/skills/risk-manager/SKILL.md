---
name: risk-manager
version: 1.0.0
description: Position sizing, stop-loss, portfolio limits and kill-switch. Paper-only.
---

# Risk Manager Skill v1.0.0

Every entry needs a sized stop before it may reach any order tool.
This skill is the gate — no bypass path exists.

## Position sizing

- Risk **<= 1% of equity** per trade.
- `size = riskAmount / (entry - stopLoss)`.
- Stop: `max(2 x ATR(14), 3% below entry)`.
- Take-profit: minimum **2R**. Worse R:R = no trade.

## Portfolio limits

- Max **3 concurrent** positions.
- Max **60%** of equity invested.
- One position per symbol (matches `rsi-ema-spot`).

## Kill-switch (halt all new entries)

- Daily equity drawdown `<= -3%`, OR
- 3 consecutive losses.
- Resume requires `human-review` approval. The agent cannot self-resume.

## Order hygiene

- Every order carries a unique `clientOrderId` (idempotency key) — never reuse one.
- Paper mode default. Live is **structurally impossible** on v1 manifests:
  going live requires skill `>= 2.0.0`, secrets via `vault:` refs only
  (a literal credential anywhere is rejected), and the `payment` approval.

## Output contract

```json
{ "approved": true, "size": 0.0, "stopLoss": 0.0, "takeProfit": 0.0, "reasons": ["..."] }
```

`approved: false` must carry the blocking reason (`risk-limit`, `kill-switch`, `bad-rr`).

## Requires

- `tool/paper-order@1.0.0` (paper execution with `clientOrderId` idempotency).

## Enforcement (not advice)

The limits above are ALSO enforced in code by `RiskPolicy.CheckOrder`
(`backend/pkg/trader/gate_checks.go`) under version `risk@1.0.0`, and an order
executes only with the approval token that gate issues for the exact intent.
If this text and the code ever disagree, **the code wins** — the skill is the
explanation, the gate is the control.

Never call `trade_order` before `trade_risk_check`. A rejected check means
`flat` with that reason; do not re-phrase the same order to slip past it.

## Version history

- `1.0.0` — initial paper-only release. Live gate arrives in `2.0.0`.
