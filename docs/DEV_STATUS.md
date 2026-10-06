# DEV STATUS — Current-State Dashboard (2026-10-04)

> Single concise current-state dashboard. This file describes **current reality**
> and points elsewhere; it does not duplicate the backlog
> ([`DEV_CHECKLIST.md`](DEV_CHECKLIST.md)) or the specs
> ([`DEV_TASKS_SPEC.md`](DEV_TASKS_SPEC.md)). Product strategy lives in
> [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md).
> Status vocabulary: `planned` | `in_progress` | `implemented_unverified` |
> `verified` | `blocked` | `obsolete`.
> Truth hierarchy: live code > tests/CI > schema/migrations > config >
> active docs > archive.
> Last reviewed: 2026-10-04.

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

**Revised 2026-10-06:** the product direction is now an **AI-Native Business
OS / Agent-as-a-Service** platform with one explicit MVP wedge — the AI Sales
outcome (lead qualification + follow-up + CRM update + reporting). Product
vision: [`00-product-vision.md`](00-product-vision.md); wedge definition:
[`02-mvp.md`](02-mvp.md); decision log:
[`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md).

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
- Implemented but unverified: control-plane auth boundary (P0.3 scope,
  opt-in), author threading (P1.3.3 scope), team E2E approve path (P0.2 scope).
- Still open: P0.4–P0.9 security/tenancy/identity foundation (P0.3 landed as
  an opt-in boundary), internal Git
  (P1.3.4–P1.3.6), History API/UI (P1.4/P1.5), reviewer/gate/claim/identity
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

<a id="implemented-but-unverified"></a>

## Implemented But Unverified

- **P0.3 control-plane auth boundary (new, 2026-10-04; checklist P0 block).** `RequireSession`/`RequireOwner` (`backend/pkg/api/auth.go`) now guard the seven mutating/project routes with an Edge-KV-backed verifier, failing closed (401/503) and attaching the identity for P0.4/P0.7. Verified by unit tests + `go vet/test` + `typecheck/lint/build`, but NOT verified end-to-end: enforcement is opt-in (`CONTROL_PLANE_REQUIRE_SESSION`), no staging deployment has run with it on, and the browser→Go Edge-token path still depends on the P0.9 identity decision (a Go-login session mints no Edge token, so sign-in via the control plane cannot satisfy the boundary yet).
- P1.3.3 author threading — code present, missing: (a) test proving every path records its expected non-empty author into `generation_files.author_agent`, (b) fork flag on concurrent manual write, (c) `author` from editor/import channels (today only agent paths pass it). Not `verified` until that test lands.
- P0.2 team E2E approve path — build green, full run manual-only (no live engine in tests, `canRunTeam` false). See `docs/MULTI_AGENT.md` Known gaps 1.

<a id="in-progress"></a>

## In Progress

Nothing is actively `in_progress` as of 2026-10-04. The last merged work was
the control-plane auth boundary (P0.3, `implemented_unverified`), the Edge
`GET /api/auth/session` route, the DeepAgent migration (P0.2) and the
generation lineage recorder (P1.2); all open checklist items are `planned` or
`blocked`. Claim `in_progress`
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

Ordered, dependency-driven. Open `docs/DEV_CHECKLIST.md`, take the first unblocked item, open its one spec:

0. P0.9 identity source-of-truth decision (now on the critical path) — the P0.3 boundary can only be switched on once the SPA/Worker can present the Edge token to Go; checklist P0 block.
1. P0.3 rollout verification — enable `CONTROL_PLANE_REQUIRE_SESSION` against a real KV namespace and prove 401/503/403 on the seven guarded routes; checklist P0 block.
2. P0.4 resource ownership model (new) — prerequisite for P2.7/P5.
3. P0.5 WebSocket authorization (new).
4. P0.6 credential hardening incl. GitHub token off plaintext D1 (new).
5. P0.7 workflow tenant isolation (new).
6. P0.8 room lifecycle/eviction/quotas (new).
7. P1.3.3 verify (author E2E test + fork flag) — `docs/DEV_SPEC_P1A.md` P1.3.3.
8. P1.3.4 internal Git D1-CAS (`backend/pkg/engine/gitrepo.go`) — `docs/DEV_SPEC_P1A.md` P1.3.4.
9. P1.11 maxTokens via `model.WithMaxTokens` + `adk.WithChatModelOptions` — checklist P1.11, background in `docs/MULTI_AGENT.md` gap 6.

<a id="dependency-graph"></a>

## Dependency Graph

```text
P0.3-P0.9 (security/ownership/identity/lifecycle)
  └─> P1.3.4-P1.3.6 (internal Git + pre-commit) ─> P1.4 (History API) ─> P1.5 (frontend History)
  └─> P1.6 reviewer diffs ─> P1.8 gate ─> P1.9 claims (all need lineage P1.2 done)
  └─> P1.10 identity pipeline (needs P1.3.5 author + P3.1 suspense)
P1.11 maxTokens independent (streamLLM + eino engine only)
P1 done ─> P5 wave 1 (needs identity/auth foundation)
P1-git + P1.10 ─> P2 catalog ─> P6 waves 2+ (P6 wave 1 runs on current 7 types without P2)
P2 ─> P3/P4; P5 + P6.7 ─> P4 verifier thresholds
```

<a id="known-architecture-risks"></a>

## Known Architecture Risks

1. Split-brain identity: Edge KV sessions (`session:token:*`) vs Go PG register/login returning bare `sessionId` (live) plus unregistered Go D1 handlers (`handleRegisterD1`/`handleLoginD1` dead code). No Go handler reads the Edge cookie. Task P0.9 decides; do not invent unification.
2. Control plane session boundary is provisional: `CONTROL_PLANE_REQUIRE_SESSION` is opt-in and resolves tokens against the Edge KV store, so a deployment that logs in through the Go handlers (no Edge token) cannot satisfy it until P0.9 decides the identity contract; `GET /ws/:id` is still ungated (P0.5) and per-resource ownership is not wired yet (P0.4/P0.7). Tasks P0.4-P0.5, P0.9.
3. Plaintext provider tokens: `github_tokens.access_token` in D1 (Edge `storeGitHubToken`/`getGitHubToken`). Task P0.6 moves to encrypted storage / vault-reference model.
4. Workflow tenant gap: `workflow_dags.workflow_id` is chat/project id, no `user_id`; trigger/get skip ownership. Task P0.7 gates multi-tenant exposure.
5. Unbounded rooms: `EngineHub` never evicts; no idle TTL, no quota. Task P0.8.
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

