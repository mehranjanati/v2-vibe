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
> Last reviewed: 2026-11-21.

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
- Still open: the end-to-end rollout that switches the P0.3–P0.8 guards on, the GitHub
  mirror (P1.4.5–P1.4.7), the identity pipeline (P1.10),
  and everything in P2–P6 (specs only,
  except live 7-type validation in `worker/workflow/VibeWorkflow.ts` +
  `backend/pkg/engine/workflowschema.go`). The P1 exit gate (P1.7) itself is
  recorded as `implemented_unverified` — its only blocked item is the push E2E,
  which needs the mirror.

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
| Agent reconnect contract (2026-11-21) | `GET /api/agent/:id/connect` answers `{success, data:{agentId, websocketUrl}}` — the shape `src/lib/api-client.ts#connectToAgent` and `sdk/src/client.ts#connect` read (a flat body made every chat reopen fail); pinned by `backend/pkg/api/agent_connect_contract_test.go`; reopening a chat, its VFS and the History tab verified in a browser against production |

<a id="implemented-but-unverified"></a>

## Implemented But Unverified

- **P0.3–P0.5 control-plane guards (2026-10-04; checklist P0 block).** `RequireSession`/`RequireOwner` (`backend/pkg/api/auth.go`) guard the seven mutating/project routes with an Edge-KV-backed verifier, failing closed (401/503) and attaching the identity the P0.4 ownership check and the P0.7 workflow tenant guard consume; `wsGuard` gates the `/ws/:id` upgrade (ticket/session, P0.5). Verified by unit tests (`backend/pkg/api/auth_test.go`, `backend/pkg/api/ws_auth_test.go`), `go vet ./... && go test ./...` and the `e2e` rollout harness (`go test -tags e2e ./e2e/ -run 'P03|P05'` green in emulator mode), plus `bun run typecheck && bun run lint && bun run build` green — but NOT verified end-to-end: enforcement is opt-in (`CONTROL_PLANE_REQUIRE_SESSION`), no staging deployment has run with it on, and the live-KV pass (`VIBE_P03_KV=real`) still needs a KV-scoped Cloudflare token (a Go-login session mints no Edge token, so sign-in through the control plane cannot satisfy the boundary).
- **P0.6 credential hardening (2026-10-07).** `worker/light/tokenVault.ts` seals every provider token as an `enc:v1:<iv>:<ct>` AES-256-GCM envelope (key derived from `JWT_SECRET`, additional data binds the row to `github_token:<userId>`), re-seals legacy plaintext on first read, and `POST /api/github-app/revoke` revokes at GitHub then deletes the row. Unit-tested by `worker/light/tokenVault.test.ts` (workerd — CI only, this box is macOS < 13.5); NOT verified end-to-end: no deployed Edge has run the seal path yet (`migrations/0012_github_tokens_seal.sql` was applied remotely 2026-11-18).
- **P0.7 workflow tenant scope (2026-10-08).** `workflowGuard` (`backend/pkg/api/routes.go`) scopes trigger/get to `workflow_dags.user_id` — 403 + `workflow.access_denied` audit on cross-tenant access, 404 on unknown/unowned, 503 on store outage — and the runtime refuses a tenant-mismatched run. Verified by `backend/pkg/api/workflows_auth_test.go` + `backend/pkg/engine/workflows_owner_test.go` green; NOT verified end-to-end: the workerd tenant cases run in CI only (`migrations/0013_first_mesmero.sql` was applied remotely 2026-11-18).
- **P0.8 room lifecycle + quotas (2026-11-14).** `RoomLifecyclePolicy` (`backend/pkg/engine/hub_lifecycle.go`) sets the documented policy — `ROOM_IDLE_TTL` (default 30m; `0` disables), `ROOM_MAX_PER_USER` (default 10; `0` disables) and `ROOM_SWEEP_INTERVAL` (default 1m) — wired at startup and swept by the janitor; idle rooms are stopped, and a user's N+1th NEW session answers 429 `ROOM_QUOTA_EXCEEDED` (`backend/pkg/api/routes.go`). Verified by `backend/pkg/engine/hub_lifecycle_test.go` (cap, cap-disabled, anonymous path, eviction guards + Run-loop exit, ownership survival, env parsing, attach-after-stop) and `backend/pkg/api/session_quota_test.go` green, plus `go test -race ./pkg/engine/` green; NOT verified end-to-end: no deployed control plane has run a full 30m idle cycle, and `GET /ws/:id` still creates a room outside the quota.
- **P0.9 identity decision (2026-10-07).** `docs/architecture/adr-identity-source-of-truth.md` records the contract — the Edge light Worker stays the sole identity authority (KV `session:token:*`) and Go only verifies with `EdgeKVVerifier`; no unification invented, no runtime change. Acceptance evidence is `bun run docs:check` green with one documented owner; it stays `implemented_unverified` until the P0.3 rollout proves a login path that mints the Edge token the Go boundary requires.
- **P1.3.5 per-generation Git commit (2026-11-18).** `commitGeneration` (`backend/pkg/engine/generation_git.go:52`) turns the finished VFS into exactly one commit per generation on `refs/heads/main` (+ the `refs/gen/{genID}` tag) and links the lineage row via `generationCommitSQL` (`backend/pkg/engine/generation.go:72`); the hook is the single funnel `finalizeGeneration` (`backend/pkg/engine/room.go:1248`), so the dual-model, team and fence-streaming paths all produce a revision, and the author is the single attributed role (P1.3.3 intake) or `engine`. Verified by `backend/pkg/engine/generation_git_test.go` (4 cases incl. the no-double-commit retry, the ref/row link, and content dedupe across generations) plus `go vet ./... && go test ./...` green; NOT verified end-to-end: no live D1 has run the statements (remote apply pending, same class as P1.3.4) and the plan-execute path opens no lineage row, so it commits nothing yet.
- **P1.3.6 pre-commit gate (2026-11-18).** At the top of `commitGeneration` (`backend/pkg/engine/generation_git.go:116`): the credential rule (`generationSecretPattern`, `backend/pkg/engine/generation_git.go:67` + `generationValueIsLiteral`, `backend/pkg/engine/generation_git.go:282`) refuses a revision whose file hardcodes a key — naming `path:line` and never the value — while property access, env lookups, empty values and documented placeholders pass so ordinary login forms still commit; the 1 MiB per-file cap (`generationCommitMaxFileBytes`, `backend/pkg/engine/generation_git.go:54`) skips (never truncates) an oversize file with a `commit_file_skipped` audit row and commits the rest; an empty author is an error. Audits via `writeGenerationCommitAudits` (`backend/pkg/engine/generation_git.go:340`). Verified by four cases in `backend/pkg/engine/generation_git_test.go` (block + no revision + audited, ordinary form code allowed, oversize skipped and absent from the tree, and the credential rule table) plus `go vet ./... && go test ./...` green; NOT verified end-to-end for the same reasons as P1.3.5 (no live D1, plan-execute opens no lineage row).
- **P1.11 token budget on the engine path (2026-11-18).** `Room.streamLLM` now forwards its budget through `Engine.RunStepWithBudget` (`backend/pkg/agent/eino_engine.go:229`), which applies it per call as an ADK run option — `runStepOptions` (`backend/pkg/agent/eino_engine.go:191`) → `adk.WithChatModelOptions([]model.Option{model.WithMaxTokens(n)})` — while `RunStepWith` keeps its old signature and behaviour (budget 0 = provider default). Verified by `backend/pkg/agent/budget_test.go` (a recording model sees 12288 then 24576, and none for the plain call) plus `go vet ./... && go test ./...` green. Inventory correction recorded in the checklist: only the summarizer (512) and the chat turn (1024) were affected; the code-generation and truncation-retry paths call the raw client and never lost their budget. NOT verified end-to-end: no live provider call in tests.
- **P1.8 difficulty gate (2026-11-18).** `teamGate` (`backend/pkg/engine/team_gate.go:59`) decides team-vs-single-coder before the approved plan executes: ≤2 steps with no coupling stay on the single-coder loop, larger or coupled plans get the team, and a room without engine/tool-model/prompts never gets the team. Coupling is derived from the two signals `agentplan.PlanStep` actually carries — a `modify`/`delete` action or a shared `AssociatedSubtaskIndex` (`planDependencyCount`, `backend/pkg/engine/team_gate.go:92`) — and every decision is written to `generation_audits` (`writeGateDecisionAudit`, `backend/pkg/engine/team_gate.go:117`, action `gate_decision`, payload `{decision, steps, dependencies, reason}`). Verified by 7 cases in `backend/pkg/engine/team_gate_test.go` plus `go vet ./... && go test ./...` green; NOT verified end-to-end — `canRunTeam` is false in tests, so nothing exercises the team path this gate steers towards.
- **P7 model routing on the coder role (2026-11-18).** First live consumer of the P7.4 gateway/modelrouter contracts: `coderRouting` (`backend/pkg/engine/team_router.go:79`) builds a pinned-policy router from `MODEL_ROUTER_POLICY` / `MODEL_ROUTER_CANDIDATES` / `MODEL_ROUTER_PROVIDER` / `MODEL_ROUTER_CONTEXT_WINDOW` and gives the coder agent both the starting model and `adk.ModelFailoverConfig` (`backend/pkg/engine/team.go:140`). Opt-in and fail-safe — no policy means exactly the pre-P7 role model, a bad configuration means the role model plus a logged reason. Verified by 7 cases in `backend/pkg/engine/team_router_test.go` (no policy, pinned policy with dedupe and `MaxRetries=1`, malformed ref, unusable context window, missing engine, plus the candidate/ref parser tables) and `go vet ./... && go test ./...` green; NOT verified end-to-end: no two-provider live run, so the actual failover switch and any cost guard remain unproven.
- **P1.4 History API (2026-11-18).** `GET /api/generations/:chatId`, `GET /api/generations/:chatId/:genId/diff` and `POST /api/generations/:chatId/:genId/rollback` (`backend/pkg/api/generations.go`) sit behind the session boundary plus `ownerGuardFor` (`backend/pkg/api/routes.go:404`). List/diff read the lineage rows — `generations` with the P1.3.5 `commit_sha`, and `generation_files` for op + hashes + sizes only — and rollback creates a NEW revert commit (`GitRepo.Revert`), materialises it with the new `GitRepo.FilesAt` (`backend/pkg/engine/gitrepo.go:720`) and restores a live room's working set (`applyWorkingSet`, author `rollback`), recording a `rollback` audit row. Verified by `backend/pkg/api/generations_test.go` (5 cases), `backend/pkg/engine/gitrepo_files_test.go` and the two-plane contract (29 control routes) with `go vet/test` and `bun run typecheck` green; NOT verified end-to-end: no live D1 has served these statements. Open: the History UI (P1.5), the GitHub mirror (P1.4.5–P1.4.6) and the import endpoint (P1.4.7).
- **P1.9 path ownership (`vfs_claim`) (2026-11-18).** A per-room `claimStore` (`backend/pkg/engine/claims.go:74`) with an injectable clock and a 10-minute TTL provides the ownership signal; `claimWritePath` (`backend/pkg/engine/claims.go:155`) makes EVERY tool write honour it, so a path held by one agent is refused with the owner's name even if the model never claims explicitly. The coder gets `vfs_claim`/`vfs_release`/`vfs_status` (`backend/pkg/engine/claims.go:207`), the reviewer only `vfs_status` (`backend/pkg/engine/team.go:88`), and `finalizeGeneration` releases all claims through a `defer` (`backend/pkg/engine/room.go:1221`) so no run leaves an orphan. Scope note: both `roomVFSStore` call sites use the `coder` author, so claim-on-write refuses a *different* holder — reachable today through the explicit tools and pinned by tests — while gap-fill writes directly through `UpsertFile` (`backend/pkg/engine/gapfill.go:242`) and is therefore never blocked by a coder claim. Verified by 6 cases in `backend/pkg/engine/claims_test.go` (deny with owner, refresh, owner-only release, TTL reclaim with a fake clock, refused gapfill write, tool JSON contract, finalize cleanup) plus `go vet ./... && go test ./...` green; NOT verified end-to-end — no real team run has two agents racing for one path.
- **P1.5 History UI (2026-11-18).** A `History` view in the chat (`src/routes/chat/components/history-panel.tsx`, reached from the view switch and rendered through the same header/container as Docs) consumes the P1.4 API through TanStack Query hooks (`src/hooks/use-generation-history.ts`) with keys under `queryKeys.generations`: the list shows the verdict badge, short `commit_sha`/branch/timestamp/fork/parent, selecting a generation shows its recorded diff (op + short hashes + sizes + author, never contents), and rollback goes through a Kumo `Dialog` that states the truth — a NEW revert commit, no history rewrite — then invalidates `generations.all` and the project file snapshot so list and preview refresh. Verified by `bun run typecheck`, `bun run lint` (0 errors) and `bun run build` (chunk 2,685 kB vs 2,671 kB, no new dependency); NOT verified in a browser — the two-generation → diff → rollback scenario still needs a live control plane. The mirror-pending banner (P1.5.4) stays open until the GitHub mirror (P1.4.5–P1.4.7) exists.
- **P1.6 diff-aware reviewer (2026-11-18).** The reviewer's instruction is built per run (`backend/pkg/engine/team.go:170`) from this run's diff and the app's hub files: `generationChangedFiles` (`backend/pkg/engine/team_hub.go:42`) diffs the open generation's baseline against the live VFS with the recorder's own `diffGenerationFiles` (path + op only, capped at 40, never contents), and `HubFiles` (`backend/pkg/engine/team_hub.go`) ranks the files other files depend on (≥2 inbound references or a plan mention, top 3, deterministic). The instruction ends with a RELATIVE-review rule, and `backend/skills/03_reviewer.md` now tells the reviewer that a `delete` others still reference is a `REQUEST_CHANGES` and that `HUB:` means blast-radius. Verified by 6 cases in `backend/pkg/engine/team_hub_test.go` plus `go vet ./... && go test ./...` green; NOT verified end-to-end — `canRunTeam` is false in tests, so no real verdict has referenced the hub line yet.
- **P1.7 exit gate (2026-11-18).** Three of four items are green and the fourth is blocked by missing features, not by this repo's code: `go vet ./... && go test ./...` (14 packages) and `typecheck`/`lint`/`build`/`docs:check` pass, and the two-generation → parent → diff → rollback chain now has a **route-level E2E** (`backend/e2e/p17_history_test.go`, build tag `e2e`, run `go test -tags e2e ./e2e/ -run P17 -v`) that drives the real `RegisterRoutes` + `EngineHub` + D1 client over an emulated D1 and asserts the revert commit's tree, HEAD movement (through a fresh handle, since refs are primed at `Init`) and the audit row. P1.7.4 (push + PR E2E) stays blocked because the GitHub mirror (P1.4.5–P1.4.7) does not exist; the browser E2E of the History UI and the live-D1 pass are environmental.
- P0.2 team E2E approve path — build green, full run manual-only (no live engine in tests, `canRunTeam` false). See `docs/MULTI_AGENT.md` Known gaps 1.
- **M0 slice v0 (D9) — executed 2026-11-21 on PRODUCTION** (Render `vibesdk-backend.onrender.com` + Edge `vibesdk-v2.apjkala25.workers.dev`, `CONTROL_PLANE_REQUIRE_SESSION=true`, `VIBECODER_STORE_KV_ID=56d249930a4447ee92493a7ff061a3ae` = the store the Edge mints into: 4 live `session:token:*` keys at test time, staging namespace 0). All seven acceptance criteria are proven against the live stack — criterion 2 with a throwaway second identity (removed after the test), criterion 4 in a real browser. **Criterion 2 (governance, all four codes):** anonymous `POST /api/agent/session` and `GET /api/generations/<id>` → 401; `GET /ws/<id>` without a ticket → 401 and with the session ticket → 101; owner `GET /api/generations/:chat` → 200; a second identity → **403** on all five cross-tenant routes (`generations` list/diff/rollback, `agent/:id/connect`, `projects/:id/files`); unknown project → 404. The second identity was a throwaway account registered through the Edge and removed after the test (its `users` row deleted, its session logged out; the store was back to the owner's 4 sessions). **Criterion 1 (loop):** chat `55817c46-e1e9-4c04-936c-2063c6d50bf6` → generation `2dc6aa64-ca54-41be-910d-a4239e45eb2e` `succeeded` / `verdict=done` in ~60 s with `commit_sha=eaf402b53916b7fd4b6f337e19a2e0f5ba06f73d` (3 files: `public/index.html`, `public/styles.css`, `public/js/main.js`; `git_objects` 0→8, `git_refs` 2). **Criterion 3 (attribution):** 3/3 `generation_files` rows carry `author_agent` (`coder`, `gapfill`, `coder`); `generation_audits` = 3× `file_written`, 1× `gate_decision`, 1× `generation_finished`, 1× `rollback`. **Criterion 4 (visibility, real browser):** in the deployed SPA (headless Chrome, the owner's Edge session), the History tab lists `eaf402b5` with `done` / `main` / `11/21/2026, 2:34:25 PM`; selecting it renders `eaf402b5 · 3 file(s) changed` as a PATH/OP/BEFORE/AFTER/AUTHOR table (3 `create` rows with hashes, sizes and `coder`/`gapfill`/`coder`), and "Roll back to this" opens the truthful dialog ("creates a NEW revert commit … History is never rewritten"), dismissed without executing. **Criterion 5 (rollback safety):** `POST …/rollback` → 200 `applied:true`, `restoredFiles:3`, NEW revert commit `4904bc2d28dcbfb4893ad4ad332e482c3d0f882e`, and the list still returns 1 row (history not rewritten). **Criterion 6 (loud failure):** a run cancelled via `stop_generation` emitted `generation_cancelled` to the client and left row `972856df-0ac1-410a-b581-2281a830aa52` with `status=cancelled`, `verdict=null`, `commit_sha=null`. **Root cause found by this run:** before it, production recorded *nothing* — every lineage/Git/D1 write failed silently because the control plane's Cloudflare token had no D1 write; after granting `D1 Edit` the write paths land. That failure mode is still log-only (`lineage: open generation record:`), so a future permission regression looks like a successful run with empty history. **Not proven:** the mirror-push item (P1.4.5–P1.4.7) only. **Bug found and fixed by this run:** reopening an existing chat failed for every user — `GET /api/agent/:id/connect` answered a flat body while the SPA (`src/lib/api-client.ts#connectToAgent`) and the SDK (`sdk/src/client.ts#connect`) read `data`, so the browser logged `Failed to connect to agent`, never opened the WebSocket, and left preview/VFS/History empty. Fixed by returning the standard envelope (`backend/pkg/api/routes.go`) and pinned by `backend/pkg/api/agent_connect_contract_test.go`; the criterion-4 browser pass above is against the redeployed plane.

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
1. ~~P0.3 rollout verification~~ — **verified 2026-11-18 against a real KV namespace.** `VIBE_P03_KV=real … go test -tags e2e ./e2e/ -run P03 -v` passed against the account's live namespaces (staging `VibecoderStore-staging` = `4748f35d77274731a17fa4f32c8c3e00`, created for this): anonymous → 401, unknown token → 401, store outage → 503, valid token → handler, three token transports (header / bearer / cookie), and owner scoping `own=200 · cross-user=403 · unknown=404 · anonymous=401` across the seven guarded routes. Emulator mode (`VIBE_P03_KV=emu`) stays the default for CI. Remaining for production: the same proof on a deployed control plane with `CONTROL_PLANE_REQUIRE_SESSION=true` (staging service, P0.3 rollout item) — the deployed-plane leg ran on PRODUCTION 2026-11-21 (see the M0 slice entry under *Implemented But Unverified*); the staging service itself stays unexercised.
2. P0.4 ownership verification — `ownerGuard` is wired on rooms/VFS/deploys; prove cross-tenant addressing answers 404/403 on a deployed control plane with the boundary on.
3. P0.5 WebSocket authorization — wired (`wsGuard` on `/ws/:id`, ticket/session gate; `go test -tags e2e ./e2e/ -run P05`), part of the opt-in P0.3 rollout.
4. P0.6 rollout — `migrations/0012_github_tokens_seal.sql` is **applied remotely (2026-11-18)**; what remains is proving the seal/read/revoke path on a deployed Edge (code landed 2026-10-07).
5. P0.7 workflow tenant isolation — wired (`workflowGuard` on the trigger/get routes, `workflow_dags.user_id` tenant, `workflow.access_denied` audits; `VibeWorkflow` tenant check; unit tests green, workerd suites + E2E rollout pending), part of the opt-in P0.3 rollout.
6. P0.8 rollout verification — defaults are live at startup (`ROOM_IDLE_TTL=30m`, `ROOM_MAX_PER_USER=10`, `ROOM_SWEEP_INTERVAL=1m`); prove on a deployed control plane that a quiet room is evicted and the 11th new session answers 429, then decide whether `GET /ws/:id` should share the cap.
7. ~~P1.3.5 Git commit hook~~ — **done 2026-11-18** (`implemented_unverified`): `finalizeGeneration` now writes one batch commit per generation (`backend/pkg/engine/generation_git.go:52`, hook at `backend/pkg/engine/room.go:1248`) and stamps `generations.commit_sha`/`branch`; P1.3.6 (the pre-commit credential/size/author gate) landed on top of it the same day, so the only thing left in this substrate is a live-D1 pass.
8. ~~P1.11 maxTokens~~ — **done 2026-11-18** (`implemented_unverified`): `streamLLM` now forwards the budget through `Engine.RunStepWithBudget` → `adk.WithChatModelOptions([]model.Option{model.WithMaxTokens(n)})` (`backend/pkg/agent/eino_engine.go:229`); the two affected call sites were the summarizer (512) and the conversational chat turn (1024), not the code-generation paths (those already called the raw client).
9. Use-case integration defects (audit F1/F2, 2026-11-18) — **parked on `feat/spot-trader-use-case` (`c59cac8`)**: `main` no longer imports `backend/pkg/trader`, so the unconditional append in `planExecuteTools` (`backend/pkg/engine/plan_execute.go:143`) is off the baseline (F1) and the unbound `trader.WithTradeContext` identity (F2) has no call site left to miss. Re-open both behind the decision-1 activation model; until then the bundle is a reference, not a product surface.
10. ~~Migrations 0012–0015~~ — **applied remotely 2026-11-18.** `bun run db:migrate:remote` (run via Node 22 on this box, see the deploy runbook) reported `✅` for `0011_jittery_the_liberteens` … `0015_skinny_tomorrow_man`; the read-back `SELECT name FROM d1_migrations ORDER BY id` returns the full set `0000 … 0015_skinny_tomorrow_man`, and the object-level check confirms the tables exist (`git_objects`=1, `outcome_runs`=1, `trade_journal`=0 — the last one belongs to the parked branch). Audit finding F4 is closed for `main`.

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
3. Plaintext provider tokens — addressed (P0.6): `github_tokens.access_token` now holds `enc:v1:` AES-256-GCM envelopes sealed in `worker/light/tokenVault.ts`; revoke is `POST /api/github-app/revoke`. Rows written before P0.6 are purged by `migrations/0012_github_tokens_seal.sql` (applied remotely 2026-11-18 — see the P0.6 verification note) or re-sealed lazily on first read.
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
cd backend && go test -tags e2e ./e2e/ -run 'P03|P05|P17'   # emulator mode; no credentials
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

