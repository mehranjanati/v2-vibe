---
name: rsi-ema-spot
version: 1.0.0
description: Rule-based RSI(14) + EMA(50/200) long-only spot strategy on 1h candles. Paper-only.
---

# RSI + EMA Spot Skill v1.0.0

Rule-based long-only spot strategy. Deterministic rules — the LLM applies them, never invents new ones.

## When to use

Evaluate once per **closed 1h candle** for `BTCUSDT`, `ETHUSDT`.
Universe changes only via a new skill version (never ad-hoc).

## Strategy rules

**ENTRY — all must hold:**

1. Uptrend: `close > EMA50 > EMA200`.
2. Trigger: `RSI(14)` crossed **up through 30** from below within the last 3 closed candles.
3. Volume: last candle volume > 20-period average volume.

**EXIT — any triggers:**

1. `RSI(14) >= 70` (take profit), OR
2. `close < EMA50` (trend break), OR
3. Stop-loss from the `risk-manager` skill hit.

**Hard constraints:**

- Spot only. Never short, never leverage.
- One position per symbol.
- Intraday ticks are noise — ignore; decide on closed candles only.
- If candles are missing/stale (>2 candles late): output `flat` with reason `stale-data`.
- Never invent candles. No data = no trade.

## Output contract (per symbol)

```json
{ "signal": "long|flat", "confidence": 0.0, "reasons": ["..."], "invalidation": "..." }
```

`confidence` is 0..1. `flat` must always carry a reason (`no-setup`, `stale-data`, `risk-blocked`).

## Requires

- `tool/market-data@1.0.0` (1h klines)
- Must be paired with `skill/risk-manager@1.0.0` — entry without a sized stop is rejected.
- Every decision must pass the **market evidence guard** before it becomes a
  signal. Rejected evidence (`source-not-allowlisted`, `timestamp-missing`,
  `timestamp-in-future`, `latency-exceeded`, `stale-data`) means `flat` with
  that reason — never a guess.
- Every journal write needs provenance: `source`, `config_version`, and the
  pinned `skill/rsi-ema-spot@1.0.0` ref. A row without provenance is not
  evidence.

## Version history

- `1.0.0` — initial paper-only release. Live trading explicitly out of scope:
  live requires skill `>= 2.0.0` + `vault:` secrets + `payment` approval (see risk-manager).
