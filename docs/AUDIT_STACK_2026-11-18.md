# Stack Audit — 2026-11-18

> **Scope:** the whole P0–P7 backlog against live code — not one use case.
> This is a read-only audit: no runtime behavior was changed by it.
> Truth hierarchy applied as documented in [`DEV_STATUS.md`](DEV_STATUS.md):
> live code > tests/CI > schema/migrations > config > active docs > archive.
> Status vocabulary is the checklist's: `planned` | `in_progress` |
> `implemented_unverified` | `verified` | `blocked` | `obsolete`.
>
> **Bundle status (2026-11-18):** the `docs/use-cases/spot-trader` bundle and its
> runtime left `main` for `feat/spot-trader-use-case` (`c59cac8`) — `backend/pkg/trader`,
> the engine trade anchor, and the `trade_journal`/`trade_state` schema with
> migrations `0016`/`0017`. Findings **F1** and **F2** therefore describe that
> branch, not the `main` baseline; references to parked code carry no line number
> because those paths no longer exist here.

## Contents

| Section | Anchor |
|---|---|
| Verification run for this audit | [`#verification-run`](#verification-run) |
| Verdict by phase | [`#verdict-by-phase`](#verdict-by-phase) |
| Findings | [`#findings`](#findings) |
| Item-by-item audit | [`#item-by-item-audit`](#item-by-item-audit) |
| Recommended order | [`#recommended-order`](#recommended-order) |
| Decisions needed | [`#decisions-needed`](#decisions-needed) |

<a id="verification-run"></a>
## Verification run for this audit

All four commands were executed on 2026-11-18 against this working tree:

```bash
bun run docs:check                       # exit 0
cd backend && go vet ./...               # exit 0
cd backend && go test ./...              # exit 0
git diff --stat && git status --short    # baseline drift captured
```

`docs:check` detail: 340 refs in 50 files, Postman contract 23 worker + 26
control routes, bindings 5/5 in parity, 10 manifests across 4 schemas,
System IR 4/4 cases + 6 rejected probes. `go test ./...` covers
`backend/pkg/{agent,api,cloudflare,design,engine,gateway,llm,mcp,metering,
modelrouter,models,skills,trader}`.

Baseline drift at audit time (uncommitted):
`backend/pkg/engine/plan_execute.go`, `docs/architecture/manifest-schemas.md`,
`migrations/meta/_journal.json`, `scripts/validate-manifests.mjs`,
`worker/database/schema.ts` modified; `backend/pkg/engine/trade_anchor.go`,
`backend/pkg/trader/`, `docs/use-cases/`, `migrations/0016_*.sql`,
`migrations/0017_*.sql` untracked.


<a id="verdict-by-phase"></a>
## Verdict by phase

| Phase | Checklist says | Audit says | Delta |
|---|---|---|---|
| **P0 Foundation** | P0.3–P0.8 `implemented_unverified`, rollout pending | Code + tests present (`backend/pkg/api/auth_test.go`, `backend/pkg/engine/hub_lifecycle_test.go`, `worker/light/tokenVault.ts`); boundary is **off in the deployed config** | Rollout is the gap, not the code |
| **P1 Agent Primitive** | P1.3.4 `verified`; P1.3.5–P1.11 open | `generation.go` has no Git commit call → P1.3.5 genuinely absent; `backend/pkg/engine/room.go:562` drops the budget on the engine path → P1.11 confirmed | Matches docs |
| **P2 Compiler** | all `planned` | Unchanged; only seed slices (P1.6/P1.8/P1.9) exist as IDs, none implemented | Matches docs |
| **P3 Durable Runtime** | all `planned` | Unchanged; live surface is still the 7-type workflow validator only | Matches docs |
| **P4 Workbench** | all `planned` | Unchanged | Matches docs |
| **P5 Systems** | all `planned` | Unchanged; internal Git (P1.3.4) is the only substrate that landed | Matches docs |
| **P6 Factory** | all `planned` | Unchanged | Matches docs |
| **P7.1–P7.4** | `implemented_unverified` | Confirmed: **no runtime consumer** — the only cross-package import is `modelrouter → gateway`, and nothing imports `modelrouter` or `mcp` outside their own tests | Matches docs |
| **P7.5–P7.7** | `planned` | Unchanged | Matches docs |
| **M1 metering** | recorder landed (`57ffd43`) | `backend/pkg/metering` has **no consumer in `backend/pkg/api`** — no route reads or writes `outcome_runs` | Not recorded in the checklist |

<a id="findings"></a>
## Findings

### F1 — Use-case tools are wired unconditionally into a platform generation path

`planExecuteTools` (`backend/pkg/engine/plan_execute.go:143`) appends
`trader.Tools(...)` to the executor tool set with **no feature flag, no app
classification and no manifest check** (`backend/pkg/engine/plan_execute.go:166`).
The plan-execute-replan path is reached from
`backend/pkg/engine/room.go:1064` whenever `canPlanExecute()` is true and a plan
was approved — so an ordinary app generation can be handed `trade_resume`,
`trade_checkpoint`, `trade_risk_check` and `trade_order`
(`backend/pkg/trader/tools.go`, on branch `feat/spot-trader-use-case`).

Consequences: four unrelated tool schemas inflate every executor prompt on that
path, and the platform core now knows about a product use case. The use case's
own README claims the boundary in the other direction ("the dependency direction
is engine → use case, never the reverse") — true for imports, silent about
conditional activation.

**Recommended:** gate the append on an explicit enablement signal (env flag
and/or "this app's System pins `paper-spot-trader`") and add a test asserting a
non-trader app gets zero `trade_*` tools.

### F2 — The trade tools cannot succeed: identity is never bound

`trader.WithTradeContext` (`backend/pkg/trader/tools.go`, branch
`feat/spot-trader-use-case`) is the only writer of the context identity and it is
**called nowhere in the repository** — `grep -rn 'WithTradeContext(' backend/`
returns the definition only. Every tool call therefore fails at its `tradeID`
accessor with `trader: no identity bound to tool call`.

The comment at `backend/pkg/engine/plan_execute.go:166` asserts the opposite
("identity is bound per-call by the engine via trader.WithTradeContext"). This is
the exact defect class the docs warn about: a comment claiming a binding no
caller performs. No engine test exercises the wiring — the engine's trade tests
cover only the Git anchor adapter.

**Recommended:** bind identity where the run is opened, and add one engine-level
test that drives a real tool call through the wired context (not a unit test of
the tool in isolation).

### F3 — Multi-tenant enforcement is off in the deployed configuration

`CONTROL_PLANE_REQUIRE_SESSION` is `"false"` in `render.yaml` (line 82). The
boundary itself is implemented and proven
(`backend/e2e/p03_session_boundary_test.go`,
`go test -tags e2e ./e2e/ -run P03`), but the shipped config keeps it disabled,
so P0.3–P0.7 remain advisory in production. This is the highest-impact gap in
P0: everything multi-tenant depends on it.

**Recommended:** turn it on in staging with `VIBECODER_STORE_KV_ID`, run the
real-KV variant of the e2e proof, then promote to production.


### F4 — Migrations 0012–0015 are not confirmed applied remotely

`main` carries `migrations/0012_github_tokens_seal.sql` through
`migrations/0015_skinny_tomorrow_man.sql` (`0016`/`0017` were the trade-memory
tables and left with the parked bundle). `DEV_STATUS.md` records 0012/0013 as
pending remote apply, and 0014–0015 (outcome runs, internal Git tables) are newer
than that note, so the remote state of the whole set is unconfirmed.

**Recommended:** one `db:migrate:remote` pass, then read the table list back with
the read-only D1 `SELECT` equivalent and record the applied set in
`DEV_STATUS.md`.

### F5 — P7.1–P7.4 are contracts without consumers (inversion risk)

Confirmed by import graph: `mcp` has zero importers, `gateway` is imported only
by `modelrouter`, and `modelrouter` has zero importers. Meanwhile the live
generation path still builds per-role models directly
(`NewRoleModel` at `backend/pkg/agent/eino_engine.go:154`, called from
`backend/pkg/engine/team.go:308`). The gateway/failover stack is real code with
real tests that changes no runtime behavior.

That sequencing is healthy only while it is labelled as such. The risk is drift:
manifests (P7.1) and IR (P7.2) describe policies nothing enforces, while the
engine keeps its own ad-hoc model selection.

**Recommended:** one narrow wiring slice — set the chat model and failover config
from `modelrouter` on the live generation path, with a test proving fallback and
budget refusal there. That single change gives three `implemented_unverified`

**Status (2026-11-18):** the slice landed for the **coder role**
(`coderRouting`, `backend/pkg/engine/team_router.go:79`; failover config attached
at `backend/pkg/engine/team.go:140`), opt-in via `MODEL_ROUTER_*` and fail-safe.
`modelrouter` now has a real importer, so the "zero consumers" part of F5 is no
longer true for P7.4. Still unproven on a live two-provider run, and MCP (P7.3)
still has no transport.
items runtime evidence.

### F6 — Outcome metering is landed but unread

`backend/pkg/metering` writes `outcome_runs` and step logs, and
`migrations/0014_outcome_runs.sql` exists, but no handler under
`backend/pkg/api` references `OutcomeRun`/`outcome_runs`. The M1 metering slice
is write-only from the product's point of view: nothing serves it.

**Recommended:** either wire one read route (the smallest honest slice) or mark
the slice explicitly as "recorder only, no surface" in the checklist so it is not
mistaken for a shipped capability.

### F7 — Internal Git is a substrate with no commit hook

`backend/pkg/engine/gitrepo.go` is `verified`, and
`backend/pkg/engine/trade_anchor.go` (now parked) was its only runtime consumer,
so `backend/pkg/engine/generation.go` still contains no commit call — P1.3.5 (one
batch commit per generation) is genuinely unimplemented. The consequence is odd:
with the bundle parked, **nothing** on `main` produces a Git commit at all — the
substrate has tests and no live producer.

**Recommended:** implement P1.3.5 before any P5 versioning surface, otherwise the
History API (P1.4) has nothing to read for ordinary generations.

**Status (2026-11-18, later the same day):** landed — `commitGeneration`
(`backend/pkg/engine/generation_git.go:52`) writes one commit per generation from
`finalizeGeneration` and stamps `generations.commit_sha`/`branch`. It is
`implemented_unverified`: the tests run on the emulated D1 only, and the
plan-execute path still opens no lineage row, so it produces no revision.
P1.3.6's pre-commit gate (credential scan that names `path:line` only, a 1 MiB
per-file skip, a mandatory author) landed the same day at
`backend/pkg/engine/generation_git.go:116`.

### F8 — Audit note on the use case itself

Positive: the trader bundle is genuinely isolated and documented —
`docs/use-cases/spot-trader/README.md`, `SECURITY.md`, `RUNBOOK.md`, six pinned
manifests, a closed-bundle rule enforced by `scripts/validate-manifests.mjs`, and
a 40-case unit suite (`go test ./pkg/trader/` runs 26 test functions / 40
subtests). The risk gate rejects rather than clamps
(`backend/pkg/trader/gate.go`, branch `feat/spot-trader-use-case`) and the
approval token binds the exact intent — both correct shapes.

The defect is not design, it is **integration** (F1 + F2) and **activation**
(no route, no cron calling `SweepTradeMemory`, no live feed).


<a id="item-by-item-audit"></a>
## Item-by-item audit

| ID | Checklist status | Code evidence | Gap to `verified` |
|---|---|---|---|
| *Snapshot note* | — | The table below records the status **at audit time (2026-11-18, morning)**. Later that day P1.3.5, P1.3.6, P1.11, P1.8, P1.9, P1.4/P1.5 and P1.6 landed as `implemented_unverified`; [`DEV_STATUS.md`](DEV_STATUS.md) is the live source of truth. | — |
| P0.3 control-plane boundary | `implemented_unverified` | `backend/pkg/api/auth.go` + 11-case test | Opt-in and off in `render.yaml` (F3) |
| P0.4 ownership | `implemented_unverified` | `RequireOwner` primitive | Per-resource wiring not exhaustively proven |
| P0.5 WS gate | `implemented_unverified` | `GET /ws/:id` gated | Same rollout gate |
| P0.6 credential vault | `implemented_unverified` | `worker/light/tokenVault.ts` (`enc:v1:`) | 0012 seals not confirmed applied (F4) |
| P0.7 workflow tenant scope | `implemented_unverified` | `workflow_dags.user_id` + guard | 0013 not confirmed applied (F4) |
| P0.8 room lifecycle/quota | `implemented_unverified` | `backend/pkg/engine/hub_lifecycle.go:242`, `hub_lifecycle_test.go` | `GET /ws/:id` still creates rooms outside the cap |
| P1.3.4 internal Git | `verified` | `backend/pkg/engine/gitrepo.go` + tests | None (emulated D1 only) |
| P1.3.5 commit hook | `planned` | absent in `generation.go` | Whole task (F7) |
| P1.3.6 secret scan | `planned` | absent | Whole task |
| P1.4 / P1.5 History | `implemented_unverified` | API + History tab shipped 2026-11-18 | GitHub mirror (P1.4.5–P1.4.7) and the mirror banner open; no browser E2E |
| P1.6–P1.10 | `planned` | absent | — |
| P1.11 maxTokens | `planned` | `backend/pkg/engine/room.go:562` drops the budget | Whole task (confirmed) |
| P2.1–P2.8 | `planned` | absent | — |
| P3.x | `planned` | except live 7-type validation | — |
| P4.x / P5.x / P6.x | `planned` | absent | — |
| P7.1 manifests | `implemented_unverified` | 10 manifests validate; closed-bundle rule added | No schema-loading consumer (F5) |
| P7.2 System IR | `implemented_unverified` | 4 cases + 6 negative probes green | No compiler enforcement (F5) |
| P7.3 MCP | `implemented_unverified` | 12 tests green | No transport, no route, no persistence (F5) |
| P7.4 gateway | `implemented_unverified` | 12 gateway + 9 router tests green | Not wired on the generation path (F5) |
| P7.5 runtime adapter | `planned` | absent | — |
| P7.6 scoped memory | `planned` | trader memory is a seed, not the abstraction | Whole task; the binding bug blocks the seed (F2) |
| P7.7 run trace | `planned` | absent | — |
| M1.1–M1.3 metering | not in checklist | `backend/pkg/metering` + 0014 | No read surface (F6) |

<a id="recommended-order"></a>
## Recommended order

1. **F3 — turn the P0 boundary on** (staging first, real-KV e2e proof). Nothing
   multi-tenant is honest until this is on.
2. **F4 — apply migrations 0012–0015** in one pass and record the applied set.
3. **F2 — bind the trade identity** and **F1 — gate the trade tools** — *after the
   park these live on `feat/spot-trader-use-case`, not `main`*: do them when the
   bundle returns, behind the activation model of decision 1. On `main` the
   substrate item that replaces this slot is **P1.3.5** (step 4), because F7 shows
   no live producer of Git commits is left.
4. **P1.3.5 + P1.3.6** — one commit per generation plus the pre-commit scan; the
   substrate every versioning task assumes.
5. **P1.11 + P1.8** — budget plumbing and the difficulty gate; both feed the
   router. *(Both landed 2026-11-18, `implemented_unverified`.)*
6. **F5 — one gateway wiring slice** on the live generation path.
7. **P1.4 / P1.5** — History API and UI, now that commits exist. *(API and UI
   landed 2026-11-18; the GitHub mirror is the remaining P1.4 work.)*
8. Then P2 catalog, P7.5 / P7.7, and the P5 reference-system work.

<a id="decisions-needed"></a>
## Decisions needed

1. **Use-case activation model (F1):** env flag, per-app manifest pin, or both?
   The answer decides whether `docs/use-cases/` is a product surface or a
   reference bundle only.
2. **P0 rollout target (F3):** staging-only for now, or straight to production
   once the real-KV e2e is green?
3. **P7 depth for `verified` (F5):** is one wired slice (model selection +
   failover + budget refusal on the generation path) enough, or must MCP/gateway
   also persist registry/policy/grants in D1 first?
4. **Metering surface (F6):** ship one read route now, or keep the recorder and
   label it explicitly as unwired in `DEV_CHECKLIST.md`?

_Last reviewed: 2026-11-18._

