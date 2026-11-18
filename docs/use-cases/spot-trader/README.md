# Use Case — Spot Trader (paper)

> **What this is:** a self-contained trading use case built on the platform:
> one agent, two skills, two tools, one System, plus a separate Go package that
> implements its cross-run memory.
>
> **Everything for this use case lives in this folder and in
> `backend/pkg/trader/`** — it is deliberately kept out of the platform's own
> structure (`docs/architecture/manifest-schemas/examples/`,
> `backend/pkg/engine/`) so a use case never mixes into the stack.
>
> **Status:** contracts and the memory package are `implemented_unverified`.
> Paper only: live execution is structurally rejected by the manifests.
> **Last reviewed:** 2026-11-18.

## Map of the use case

| Part | File | Role |
|---|---|---|
| Strategy skill | [`skills/rsi-ema-spot/SKILL.md`](skills/rsi-ema-spot/SKILL.md) | RSI(14) + EMA(50/200) long-only rules, 1h candles. Claude-compatible `SKILL.md` format. |
| Risk skill | [`skills/risk-manager/SKILL.md`](skills/risk-manager/SKILL.md) | Sizing (1% risk), stops, portfolio caps, kill-switch. |
| Skill manifest | [`manifests/rsi-ema-spot.skill.json`](manifests/rsi-ema-spot.skill.json) | `vibesdk/v1` pin of the strategy skill |
| Skill manifest | [`manifests/risk-manager.skill.json`](manifests/risk-manager.skill.json) | `vibesdk/v1` pin of the risk skill |
| Tool manifest | [`manifests/market-data.tool.json`](manifests/market-data.tool.json) | Read-only OHLCV (`sideEffect: safe`, no secrets) |
| Tool manifest | [`manifests/paper-order.tool.json`](manifests/paper-order.tool.json) | Paper execution, `clientOrderId` idempotency, approvals required |
| Agent manifest | [`manifests/spot-trader.agent.json`](manifests/spot-trader.agent.json) | Composes skills + tools, `memory.scope: agent`, 90-day retention |
| System manifest | [`manifests/paper-spot-trader.system.json`](manifests/paper-spot-trader.system.json) | Single agent + topology on `ingress.candle-close` |
| Security spec | [`SECURITY.md`](SECURITY.md) | Provenance, market guard, hash chain + Git anchor, **risk gate + approval token**, retention |
| Operator runbook | [`RUNBOOK.md`](RUNBOOK.md) | Pre-flight, control table, incident responses, known gaps |
| Go package | `backend/pkg/trader/` | Journal + state (D1/Redis), provenance, guard, chain, **risk gate, paper order**, retention, eino tools |
| Anchor adapter | `backend/pkg/engine/trade_anchor.go` | Seals chain heads as commits in the internal Git repo (engine → trader direction only) |

## Boundaries with the stack

The use case **uses** the platform and does not edit it:

| Platform surface | How the use case touches it |
|---|---|
| `docs/architecture/manifest-schemas/*.schema.json` | Read-only: the manifests here validate against these schemas |
| `scripts/validate-manifests.mjs` | Scans `docs/use-cases/*/manifests/` as a **closed bundle**: a manifest here may only reference manifests in the same bundle |
| `worker/database/schema.ts` | Adds two tables (`trade_journal`, `trade_state`) — the single D1 schema source stays central by design |
| `backend/pkg/engine/plan_execute.go` | Imports `backend/pkg/trader` and appends its tools; the dependency direction is engine → use case, never the reverse |
| `worker/light/lightApp.ts`, Go routes | Untouched — no route serves this use case yet |

## Verification

```bash
node scripts/validate-manifests.mjs          # schema + closed-bundle refs + canonical round-trip
cd backend && go vet ./pkg/trader/ && go test ./pkg/trader/
bun run docs:check
```

## Adding another use case

1. `mkdir -p docs/use-cases/<name>/{skills,manifests}`.
2. Pin manifests there (`kind/name@x.y.z`, canonical JSON: sorted keys, 2-space
   indent, one trailing newline). Cross-references stay inside the bundle —
   reuse a platform manifest by copying it in, not by referencing out, so the
   bundle stays exportable as one unit.
3. If it needs runtime code, create `backend/pkg/<name>/` — never add files to
   `backend/pkg/engine/` for a use case.
4. Run the verification commands above; the manifest gate picks the bundle up
   automatically.
