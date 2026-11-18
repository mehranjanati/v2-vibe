# DEV STATUS — Current-State Dashboard (2026-11-14)

> Single concise current-state dashboard. This file describes **current reality**
> and points elsewhere; it does not duplicate the backlog
> ([`DEV_CHECKLIST.md`](DEV_CHECKLIST.md)) or the specs
> ([`DEV_TASKS_SPEC.md`](DEV_TASKS_SPEC.md)). Product strategy lives in
> [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md).
> Status vocabulary: `planned` | `in_progress` | `implemented_unverified` |
> `verified` | `blocked` | `obsolete`.
> Truth hierarchy: live code > tests/CI > schema/migrations > config >
> active docs > archive.
> Last reviewed: 2026-11-18.

## Contents

| Section | Anchor |
|---|---|
| Current Architecture | [`#current-architecture`](#current-architecture) |
| Current Product Thesis | [`#current-product-thesis`](#current-product-thesis) |
| Current Implementation State | [`#current-implementation-state`](#current-implementation-state) |
| Verified Work | [`#verified-work`](#verified-work) |
| Implemented But Unverified | [`#implemented-but-unverified`](#implemented-but-unverified) |
| In Progress | [`#in-progress`](#in-progress) |
| Blocked Work | [`#blocked-work`](#blocked-work) |
| Obsolete Work | [`#obsolete-work`](#obsolete-work) |
| Immediate Next Tasks | [`#immediate-next-tasks`](#immediate-next-tasks) |
| Dependency Graph | [`#dependency-graph`](#dependency-graph) |
| Known Architecture Risks | [`#known-architecture-risks`](#known-architecture-risks) |
| Known Documentation Drift | [`#known-documentation-drift`](#known-documentation-drift) |
| Verification Commands | [`#verification-commands`](#verification-commands) |
| How to Read the Roadmap | [`#how-to-read-the-roadmap`](#how-to-read-the-roadmap) |

<a id="current-architecture"></a>

## Current Architecture

Dual-plane, nothing else is live. Narrative: [`llm.md`](llm.md); diagrams
and Worker bindings:
[`architecture-diagrams.md`](architecture-diagrams.md).

- Edge/auth plane: light Worker `vibesdk-v2` (`worker/light-index.ts` →
  `worker/light/lightApp.ts`), Hono, D1 `v2-vibe`, KV `VibecoderStore`,
  Workflows binding `vibesdk-v2-workflows`. Serves SPA via `ASSETS`; owns
  `/api/auth/*`, GitHub OAuth/export, `/api/apps`, `/api/status`,
  `/api/capabilities`, `/api/limits/usage`. Unknown `/api/*` → JSON 404;
  chat/project/WS → JSON 503.
- Control plane: Go Fiber (`backend/pkg/api/routes.go`) — sessions,
  rooms/VFS, LLM streaming, Pages deploys, workflow runs, plus the opt-in
  session boundary (`backend/pkg/api/auth.go`, `CONTROL_PLANE_REQUIRE_SESSION`).
  Redis
  (`vfs:{chatId}`, `vfs:snap:{genID}` 7-day TTL); lineage D1 tables
  (`generations`, `generation_files`, `generation_audits`).
- Execution plane: Cloudflare Workflow `VibeWorkflow` (schema v2, 7 node
  types: trigger/http/db/ai/email/condition/sleep).
- AI stack: Eino `v0.9.21` (`backend/go.mod`), DeepAgent coordinator +
  coder/reviewer, per-role models via `NewRoleModel`, plan-execute adjacent path.
- Retired (never live): ThinkAgent/SpaceDO/Artifacts, CodeGen DO,
  `worker/api/routes`, `worker/agents/**`, `worker/database/services`,
  sandbox/containers/dispatch. See `docs/llm.md` Historical content.
<a id="current-product-thesis"></a>

## Current Product Thesis

**Revised 2026-11-15 (D7):** the product direction is now the **VibeSDK Open
Agentic Platform** — Agent Builder first (M0), multi-agent systems (M1), runtime (M2),
ecosystem (M3), operations (M4), marketplace (M5), Autonomous Software Factory (M6).
The 2026-10-06 AI-Native Business OS / AI Sales wedge history is preserved in the
decision log; the current implementation below is explicitly the **AI App Builder
seed/reference workload** that the platform generalizes, not the platform itself.
Product vision: [`00-product-vision.md`](00-product-vision.md); builder MVP:
[`02-mvp.md`](02-mvp.md); decision log:
[`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md); canonical strategy:
[`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md). No implementation status
below changes: `implemented_unverified` stays unverified and all tenancy/security
warnings stay in force.

The platform thesis underneath is unchanged in its engineering shape: the
user requests an **outcome**; the system synthesizes the agentic system to
achieve it, executes it durably, evaluates the result, and improves or
repairs it. Graph/node editing remains an inspection/debugging
representation, not the product. App generation ("App Builder") is a
first-class **capability and reference workload** — and the seed of the
Autonomous Software Factory ([`09-software-factory.md`](09-software-factory.md)).
Cloudflare/serverless remains the **execution substrate**; VibeSDK owns the
agentic control plane above it. Canonical definitions, market entry,
reference systems, competitive landscape, and moat hypotheses:
[`PRODUCT_THESIS.md`](PRODUCT_THESIS.md).

<a id="current-implementation-state"></a>

## Current Implementation State

What exists today is a working **AI App Builder on a dual-plane runtime**
with real multi-agent generation, lineage, and deploy paths — i.e. the
strongest possible starting point for the platform thesis, but not the
platform yet. Per-task evidence in the checklist; specs in `DEV_SPEC_*.md`.

- Verified history: P0.1 skeleton, P0.2 DeepAgent + Eino bump, P1.0.0 D1
  tooling, P1.0.1–P1.0.3 cleanup, P1.1 tables + migration, P1.2 recorder,
  P1.3.0 D1-CAS decision, P1.3.1/P1.3.2 run hooks.
- Implemented but unverified: the P0.3–P0.8 guards (session boundary,
  ownership, WebSocket, credential vault, workflow tenant scope, room
  lifecycle/quotas — all opt-in with the P0.3 boundary), the P0.9 decision
  record, team E2E approve path (P0.2 scope).
- Still open: the end-to-end rollout that switches the P0.3–P0.8 guards on, internal Git's pre-commit gate
  (P1.3.6), History API/UI (P1.4/P1.5), reviewer/gate/claim/identity
  (P1.6–P1.10), maxTokens fix (P1.11), and everything in P2–P6 (specs only,
  except live 7-type validation in `worker/workflow/VibeWorkflow.ts` +
  `backend/pkg/engine/workflowschema.go`).

<a id="verified-work"></a>

## Verified Work

| Task | Evidence |
|---|---|
| P0.1 team skeleton | `backend/pkg/engine/team.go`, `TestCanRunTeamGating`, `TestParseReviewVerdict`; `go vet/test` green |
| P0.2 DeepAgent + Eino v0.9.21 | `backend/go.mod` pins v0.9.21; `deep.New` coordinator, `roleModel`, `noFormatInstruction`, `teamToolContract` in `backend/pkg/engine/team.go`; `NewRoleModel` in `backend/pkg/agent/eino_engine.go` |
| P1.0.0 D1 tooling | `package.json` `db:*` scripts with `--config wrangler.v2.jsonc`; `db:generate` no-drift |
| P1.1 tables + migration | `worker/database/schema.ts` generations trio; `migrations/0011_jittery_the_liberteens.sql` |
| P1.2 recorder | `backend/pkg/engine/generation.go` + `generation_test.go` (7 tests), `generation_hook_test.go` (4 tests) |
| P1.3.0 D1-CAS decision | `docs/DEV_SPEC_P1A.md` P1.3.0 + `docs/CF_LIMITS.md` section 8 `[S7]` |
| P1.3.1/P1.3.2 hooks | `runDualModelPipeline` open/finish + `runTeam` verdict return; 4 hook tests |
| P1.3.3 write attribution + fork | `backend/pkg/engine/write_authors_test.go` (per-path `author_agent` through one real Start→Finish: coder / gapfill / delete-row / NULL-manual, plus the agent-only negative); fork flag in `backend/pkg/engine/generation.go` (`generations.fork` + `generation_forked` audit); `go test -race ./pkg/engine/ -run 'WriteAuthor\|Generation\|DualModel'` green |
| P1.3.4 internal Git (D1-CAS) | `backend/pkg/engine/gitrepo.go` (`d1Storer` go-git Storer over `git_objects`/`git_refs`; `Init/Commit/Log/Diff/Revert` with real Git SHAs) + `worker/database/schema.ts` tables + `migrations/0015_skinny_tomorrow_man.sql`; `backend/pkg/engine/gitrepo_test.go` (4 cases: chain, storage contract + dedupe, fresh-handle reload, error modes) over the emulated D1 REST envelope |

<a id="implemented-but-unverified"></a>

## Implemented But Unverified

- **P0.3–P0.5 control-plane guards (2026-10-04; checklist P0 block).** `RequireSession`/`RequireOwner` (`backend/pkg/api/auth.go`) guard the seven mutating/project routes with an Edge-KV-backed verifier, failing closed (401/503) and attaching the identity the P0.4 ownership check and the P0.7 workflow tenant guard consume; `wsGuard` gates the `/ws/:id` upgrade (ticket/session, P0.5). Verified by unit tests (`backend/pkg/api/auth_test.go`, `backend/pkg/api/ws_auth_test.go`), `go vet ./... && go test ./...` and the `e2e` rollout harness (`go test -tags e2e ./e2e/ -run 'P03|P05'` green in emulator mode), plus `bun run typecheck && bun run lint && bun run build` green — but NOT verified end-to-end: enforcement is opt-in (`CONTROL_PLANE_REQUIRE_SESSION`), no staging deployment has run with it on, and the live-KV pass (`VIBE_P03_KV=real`) still needs a KV-scoped Cloudflare token (a Go-login session mints no Edge token, so sign-in through the control plane cannot satisfy the boundary).
- **P0.6 credential hardening (2026-10-07).** `worker/light/tokenVault.ts` seals every provider token as an `enc:v1:<iv>:<ct>` AES-256-GCM envelope (key derived from `JWT_SECRET`, additional data binds the row to `github_token:<userId>`), re-seals legacy plaintext on first read, and `POST /api/github-app/revoke` revokes at GitHub then deletes the row. Unit-tested by `worker/light/tokenVault.test.ts` (workerd — CI only, this box is macOS < 13.5); NOT verified end-to-end: `migrations/0012_github_tokens_seal.sql` has no remote apply yet and no deployed Edge has run the seal path.
- **P0.7 workflow tenant scope (2026-10-08).** `workflowGuard` (`backend/pkg/api/routes.go`) scopes trigger/get to `workflow_dags.user_id` — 403 + `workflow.access_denied` audit on cross-tenant access, 404 on unknown/unowned, 503 on store outage — and the runtime refuses a tenant-mismatched run. Verified by `backend/pkg/api/workflows_auth_test.go` + `backend/pkg/engine/workflows_owner_test.go` green; NOT verified end-to-end: `migrations/0013_first_mesmero.sql` awaits a remote apply and the workerd tenant cases run in CI only.
- **P0.8 room lifecycle + quotas (2026-11-14).** `RoomLifecyclePolicy` (`backend/pkg/engine/hub_lifecycle.go`) sets the documented policy — `ROOM_IDLE_TTL` (default 30m; `0` disables), `ROOM_MAX_PER_USER` (default 10; `0` disables) and `ROOM_SWEEP_INTERVAL` (default 1m) — wired at startup and swept by the janitor; idle rooms are stopped, and a user's N+1th NEW session answers 429 `ROOM_QUOTA_EXCEEDED` (`backend/pkg/api/routes.go`). Verified by `backend/pkg/engine/hub_lifecycle_test.go` (cap, cap-disabled, anonymous path, eviction guards + Run-loop exit, ownership survival, env parsing, attach-after-stop) and `backend/pkg/api/session_quota_test.go` green, plus `go test -race ./pkg/engine/` green; NOT verified end-to-end: no deployed control plane has run a full 30m idle cycle, and `GET /ws/:id` still creates a room outside the quota.
- **P0.9 identity decision (2026-10-07).** `docs/architecture/adr-identity-source-of-truth.md` records the contract — the Edge light Worker stays the sole identity authority (KV `session:token:*`) and Go only verifies with `EdgeKVVerifier`; no unification invented, no runtime change. Acceptance evidence is `bun run docs:check` green with one documented owner; it stays `implemented_unverified` until the P0.3 rollout proves a login path that mints the Edge token the Go boundary requires.
- **P1.3.5 per-generation Git commit (2026-11-18).** `commitGeneration` (`backend/pkg/engine/generation_git.go:52`) turns the finished VFS into exactly one commit per generation on `refs/heads/main` (+ the `refs/gen/{genID}` tag) and links the lineage row via `generationCommitSQL` (`backend/pkg/engine/generation.go:72`); the hook is the single funnel `finalizeGeneration` (`backend/pkg/engine/room.go:1248`), so the dual-model, team and fence-streaming paths all produce a revision, and the author is the single attributed role (P1.3.3 intake) or `engine`. Verified by `backend/pkg/engine/generation_git_test.go` (4 cases incl. the no-double-commit retry, the ref/row link, and content dedupe across generations) plus `go vet ./... && go test ./...` green; NOT verified end-to-end: no live D1 has run the statements (remote apply pending, same class as P1.3.4) and the plan-execute path opens no lineage row, so it commits nothing yet.
- P0.2 team E2E approve path — build green, full run manual-only (no live engine in tests, `canRunTeam` false). See `docs/MULTI_AGENT.md` Known gaps 1.

<a id="in-progress"></a>

## In Progress

Nothing is actively `in_progress` as of 2026-11-14. The last merged work was
the P0.3–P0.7 guard set (session boundary, ownership, WebSocket, credential
vault, workflow tenant scope — all `implemented_unverified`), the P0.9
identity decision record, the Edge `GET /api/auth/session` route, the
DeepAgent migration (P0.2) and the generation lineage recorder (P1.2); all
open checklist items are `implemented_unverified`, `planned` or `blocked`.
Claim `in_progress`
only while code is being written, and move the item back to `planned`
(or forward to `implemented_unverified` with its test) when the session ends.

<a id="blocked-work"></a>

## Blocked Work

- P1.1.3 local migrate — `blocked` on this machine class only: workerd refuses macOS < 13.5. Unblock: `bun run db:migrate:local` on Linux/CI/DevContainer, or read-only remote `d1 execute --remote`. Constraint tests pass via SQLite harness; the live-migrate step is blocked.
- R2-remote Git backend (rejected P1.3.0 alternative) is parked until R2 is enabled — deferred revisit, not a blocker on D1-CAS.

<a id="obsolete-work"></a>

## Obsolete Work

Nothing active is `obsolete`. The `obsolete` state is reserved for backlog
items superseded by the platform thesis. Retired system components
(ThinkAgent/SpaceDO/Artifacts, CodeGen DO, sandbox preview) are documented
as removed system parts under
[`llm.md`](llm.md) Historical content and
[`architecture-diagrams.md`](architecture-diagrams.md) — not as backlog
items. If a future decision kills a `planned` item, mark it `obsolete` in
[`DEV_CHECKLIST.md`](DEV_CHECKLIST.md) with its replacement, never by silent
deletion.

<a id="immediate-next-tasks"></a>

## Immediate Next Tasks

Ordered, dependency-driven. Open `docs/DEV_CHECKLIST.md`, take the first unblocked item, open its one spec.
Whole-stack evidence (2026-11-18, P0–P7 against live code, including the
uncommitted use-case bundle): [`AUDIT_STACK_2026-11-18.md`](AUDIT_STACK_2026-11-18.md)
— its findings F1–F3 are the correctness items below that the checklist does not
yet carry.

0. P0.9 follow-through — the decision is recorded (2026-10-07: Edge mints, Go verifies); what remains is making every login path present an Edge token to Go so the boundary can be switched on; checklist P0 block.
1. P0.3 rollout verification — enable `CONTROL_PLANE_REQUIRE_SESSION` against a real KV namespace and prove 401/503/403 on the seven guarded routes; checklist P0 block. Harness in place: `backend/e2e/p03_session_boundary_test.go` (build tag `e2e`) is green in emulator mode (`go test -tags e2e ./e2e/ -run P03 -v`) and runs the identical proof against the live Edge namespace via `VIBE_P03_KV=real` — that live run is still pending a Workers-KV-scoped Cloudflare token in this environment.
2. P0.4 ownership verification — `ownerGuard` is wired on rooms/VFS/deploys; prove cross-tenant addressing answers 404/403 on a deployed control plane with the boundary on.
3. P0.5 WebSocket authorization — wired (`wsGuard` on `/ws/:id`, ticket/session gate; `go test -tags e2e ./e2e/ -run P05`), part of the opt-in P0.3 rollout.
4. P0.6 rollout — apply `migrations/0012_github_tokens_seal.sql` remotely and prove the seal/read/revoke path on a deployed Edge (code landed 2026-10-07).
5. P0.7 workflow tenant isolation — wired (`workflowGuard` on the trigger/get routes, `workflow_dags.user_id` tenant, `workflow.access_denied` audits; `VibeWorkflow` tenant check; unit tests green, workerd suites + E2E rollout pending), part of the opt-in P0.3 rollout.
6. P0.8 rollout verification — defaults are live at startup (`ROOM_IDLE_TTL=30m`, `ROOM_MAX_PER_USER=10`, `ROOM_SWEEP_INTERVAL=1m`); prove on a deployed control plane that a quiet room is evicted and the 11th new session answers 429, then decide whether `GET /ws/:id` should share the cap.
7. ~~P1.3.5 Git commit hook~~ — **done 2026-11-18** (`implemented_unverified`): `finalizeGeneration` now writes one batch commit per generation (`backend/pkg/engine/generation_git.go:52`, hook at `backend/pkg/engine/room.go:1248`) and stamps `generations.commit_sha`/`branch`; remaining in this substrate is P1.3.6 (the pre-commit secret/size/author gate, `docs/DEV_SPEC_P1A.md`) plus a live-D1 pass.
8. P1.11 maxTokens via `model.WithMaxTokens` + `adk.WithChatModelOptions` — checklist P1.11, background in `docs/MULTI_AGENT.md` gap 6.
9. Use-case integration defects (audit F1/F2, 2026-11-18) — **parked on `feat/spot-trader-use-case` (`c59cac8`)**: `main` no longer imports `backend/pkg/trader`, so the unconditional append in `planExecuteTools` (`backend/pkg/engine/plan_execute.go:143`) is off the baseline (F1) and the unbound `trader.WithTradeContext` identity (F2) has no call site left to miss. Re-open both behind the decision-1 activation model; until then the bundle is a reference, not a product surface.
10. Migrations 0012–0015 — one remote apply pass in order, then record the applied set here; audit finding F4. (`0016`/`0017` are the trade-memory tables and travel with `feat/spot-trader-use-case`, so this branch's journal ends at `0015_skinny_tomorrow_man`.)

<a id="dependency-graph"></a>

## Dependency Graph

```text
P0.3-P0.9 (security/ownership/identity/lifecycle)
  └─> P1.3.5-P1.3.6 (Git commit hook + pre-commit, on the P1.3.4 D1-CAS repo) ─> P1.4 (History API) ─> P1.5 (frontend History)
  └─> P1.6 reviewer diffs ─> P1.8 gate ─> P1.9 claims (all need lineage P1.2 done)
  └─> P1.10 identity pipeline (needs P1.3.5 author + P3.1 suspense)
P1.11 maxTokens independent (streamLLM + eino engine only)
P1 done ─> P5 wave 1 (needs identity/auth foundation)
P1-git + P1.10 ─> P2 catalog ─> P6 waves 2+ (P6 wave 1 runs on current 7 types without P2)
P2 ─> P3/P4; P5 + P6.7 ─> P4 verifier thresholds
```

<a id="known-architecture-risks"></a>

## Known Architecture Risks

1. Split-brain identity — decided 2026-10-07 (P0.9): the Edge light Worker is the sole identity authority (KV `session:token:*` + D1 `users`); Go only verifies Edge tokens via `EdgeKVVerifier`. Go PG register/login are legacy compat stubs (no Edge token minted), Go D1 handlers stay dead (removal, never wiring). Record: `docs/architecture/adr-identity-source-of-truth.md`.
2. Control plane session boundary is provisional: `CONTROL_PLANE_REQUIRE_SESSION` is opt-in and resolves tokens against the Edge KV store, so a deployment that logs in through the Go handlers (no Edge token) cannot satisfy it until the identity contract settles; `GET /ws/:id` is now gated by the same boundary (P0.5, ticket/session) and the workflow routes are tenant-scoped with audited denials (P0.7). Per-resource ownership for rooms/VFS/deploys is wired (P0.4). Task P0.9 (P0.5/P0.7 wired, in the opt-in rollout).
3. Plaintext provider tokens — addressed (P0.6): `github_tokens.access_token` now holds `enc:v1:` AES-256-GCM envelopes sealed in `worker/light/tokenVault.ts`; revoke is `POST /api/github-app/revoke`. Rows written before P0.6 are purged by `migrations/0012_github_tokens_seal.sql` (pending remote apply — see the P0.6 verification note) or re-sealed lazily on first read.
4. ~~Workflow tenant gap~~ — closed (P0.7, wired 2026-10-08): `workflow_dags.user_id` records the owner at upsert time (first-write-wins), trigger/get run `workflowGuard` (403 + `workflow.access_denied` audit on cross-tenant access, 404 on unknown/unowned, 503 on store outage), and the runtime refuses a tenant-mismatched run. `migrations/0013_first_mesmero.sql` still awaits a remote D1 apply.
5. ~~Unbounded rooms~~ — addressed (P0.8, 2026-11-14): rooms idle for `ROOM_IDLE_TTL` (default 30m) with no clients attached and no generation running are stopped and evicted by the hub janitor, and `ROOM_MAX_PER_USER` (default 10) caps a user's live rooms with 429 `ROOM_QUOTA_EXCEEDED` on the next new session. Remaining caveats: `GET /ws/:id` still creates a room outside the cap, and only `generating` (not an in-flight conversational turn) blocks eviction — safe because a conversation is triggered by an attached client and finishes in minutes against a 30m TTL, but worth knowing.
6. One-by-one GitHub push (`pushFile` per path via Contents API) — non-atomic, rate-limit prone. Atomic Git Data API migration (tree→commit→ref) preserved in P1.4.5/P1.7.4.
7. `maxTokens` no-op on engine path (P1.11) — per-role overrides + truncation doubling dropped when `r.eng != nil`.
8. Coordinator parallelization is prompt-overridden, not code-enforced (`teamToolContract`); verdict parsing is substring matching. See `docs/MULTI_AGENT.md` gaps.

<a id="known-documentation-drift"></a>

## Known Documentation Drift

Drift the audit found but deliberately left open — each names the gap and the
owner document, so the next docs pass can close it without archaeology:

1. `space/` directory still exists in the working tree (`dist/`,
   `node_modules/` only) while the source was deleted and nothing imports it
   ([`llm.md`](llm.md) Historical content). Owner: repo cleanup (P1.0 scope
   in [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md)) — remove the leftover
   directories or ignore them; do not add code there.
2. `docs/llm.md` still ends with `_Last reviewed: 2026-09-25_`; its scope
   header is accurate but the review stamp predates the DeepAgent + P0.3–P0.9
   changes. Owner: next `llm.md` review.
3. Historical task-ID namespaces collide with the new P0–P6 phase model
   (e.g. legacy P1.x vs target P1): [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md)
   carries the explicit legacy→target map — always read the map, never infer
   phase from a legacy prefix.
4. Pre-2026-10 prose in `DEV_SPEC_*.md` still uses the "AI App Builder /
   n8n-like editor" framing; the canonical framing is now
   [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md). Specs are implementation
   contracts first — reframe them only when the task is touched, and never
   renumber historical IDs to "look cleaner".
5. D6 (AaaS-only + Web3 as next plan) landed 2026-11-05 as strategy only.
   The cross-document integration pass added direction call-outs to
   [`01-problem-and-icp.md`](01-problem-and-icp.md),
   [`02-mvp.md`](02-mvp.md),
   [`04-system-architecture.md`](04-system-architecture.md),
   [`06-functions-and-tools.md`](06-functions-and-tools.md),
   [`07-multitenancy-security.md`](07-multitenancy-security.md),
   [`08-marketplace.md`](08-marketplace.md),
   [`09-software-factory.md`](09-software-factory.md),
   [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md) and
   [`usage-limits-ui.md`](usage-limits-ui.md). Still open: no runtime support
   for outcome/run metering (the live `UsageSummary` surface stays
   chat/credits-based), no Web3 code of any kind, and no decision on O7/O8.
   Owner: the M0 metering slice and the M5 Web3 ADRs, in that order.

<a id="verification-commands"></a>

## Verification Commands

```bash
bun run docs:check
bun run typecheck && bun run lint && bun run build
cd backend && go vet ./... && go test ./...
```

Focused: `bunx vitest run path/to/file.test.ts`; `go test -race ./pkg/engine/ -run 'TestRunDualModelPipeline|TestGeneration'`.
macOS < 13.5: workerd suites cannot start locally — use CI evidence, never claim local workerd success.

<a id="how-to-read-the-roadmap"></a>

## How to Read the Roadmap

1. Start here for reality (what exists, what is verified, what is blocked).
2. Strategy and category definition:
   [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md) — read it once, in under
   10 minutes, before touching the backlog.
3. Ordered implementation backlog:
   [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md) — every task has an ID, a status,
   dependencies, affected files, acceptance criteria, and verification. The
   legacy→target phase map at its top is normative: historical IDs keep their
   meaning only through that map.
4. Task quality contract:
   [`DEV_TASKS_SPEC.md`](DEV_TASKS_SPEC.md) — what a task spec must contain
   before implementation starts.
5. Implementation contracts per workstream: `docs/DEV_SPEC_*.md`.
6. Architecture, routes, limits: [`llm.md`](llm.md),
   [`architecture-diagrams.md`](architecture-diagrams.md),
   [`POSTMAN_COLLECTION_README.md`](POSTMAN_COLLECTION_README.md) §7,
   [`CF_LIMITS.md`](CF_LIMITS.md).
7. Verify with [Verification Commands](#verification-commands) and update
   the status in the checklist — Discover → Choose next unblocked task →
   Implement → Verify → Update status → Continue.

<a id="documentation-rules"></a>

## Documentation Rules

- Live code > tests/CI > schema/config > active docs > archive. Never treat archive, comments, declared-but-unread env vars, or types-without-handlers as live.
- One backlog (`docs/DEV_CHECKLIST.md`), one spec per task (`docs/DEV_SPEC_*.md` via `docs/DEV_TASKS_SPEC.md`), numbers from `docs/CF_LIMITS.md` only, routes from `docs/POSTMAN_COLLECTION_README.md` section 7.
- No invented `file:line`: every reference must resolve under `bun run docs:check`. No future dates. No new `any`. No runtime edits in a docs pass.
- States stay explicit: `implemented_unverified` is not `verified`; `blocked` carries its unblock; `obsolete` names its replacement.

