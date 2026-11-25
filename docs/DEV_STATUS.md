# DEV STATUS — Current-State Dashboard (2026-11-24)

> Single concise current-state dashboard. This file describes **current reality**
> and points elsewhere; it does not duplicate the backlog
> ([`DEV_CHECKLIST.md`](DEV_CHECKLIST.md)) or the specs
> ([`DEV_TASKS_SPEC.md`](DEV_TASKS_SPEC.md)). Product strategy lives in
> [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md).
> Status vocabulary: `planned` | `in_progress` | `implemented_unverified` |
> `verified` | `blocked` | `obsolete`.
> Truth hierarchy: live code > tests/CI > schema/migrations > config >
> active docs > archive.
> Last reviewed: 2026-11-24. Ecosystem backlog mapping: [`tasks/DEPENDENCY_GRAPH.md`](tasks/DEPENDENCY_GRAPH.md) (historical IDs preserved; ecosystem IDs track rollout on top). Next execution: [`tasks/NEXT_EXECUTION.md`](tasks/NEXT_EXECUTION.md).

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

Web3 note (PROPOSED 2026-11-25, architecture only, no runtime code): owner
decision **D22** accepted the versioned, canonical, target-independent Smart
Contract IR with a generic core plus explicit target extensions; it ships as
`architecture/web3-smart-contract-ir.md` plus schema and fixtures under
`architecture/contract-ir/` plus the sixth docs gate
(`scripts/validate-contract-ir.mjs`, eight negative probes). Chain
abstraction, LayerZero interop, toolchain pipeline and wallet-first signing
remain PROPOSED/OPEN; the tree still contains no wallet, RPC client,
compiler, generator, bridge, or deployment code. Commercial entry point and
MVP scope: `ecosystem/11-web3-network-economy.md` and
`ecosystem/14-product-and-business-model.md`.

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
| Credential vault proof (2026-11-24) | `backend/pkg/vault` (AES-256-GCM envelopes, AAD `vault:<tenant>:<name>`, migration `0020`) + four session-guarded `/api/vault` routes; 5 store tests + 5 route tests; resolution is call-time only and lands as the MCP transport's `Authorization: Bearer` header |
| Activation trace proof (2026-11-25) | `backend/pkg/activation/trace.go` (`TraceStore`, `ValidateRecord`) + `backend/pkg/activation/d1store` (append-only `Persist`/`Load`), table `activation_decision_traces` in `worker/database/schema.ts` + additive migration `0021_mature_nekra.sql`; replay is a no-op, a different record for the same run is `ErrTraceImmutable`, another tenant is `ErrTraceTenant`, a corrupt row is `ErrTraceCorrupt`, an unattributable record is `ErrTraceInvalid` before D1; 4 trace tests + T1-T11 store tests green incl. `-race`. No caller (no assembler, no policy source), so no row is produced yet |
| Activation core proof (2026-11-25) | `backend/pkg/activation` — pure `Decide()` (deny-overrides, default-deny, `E_ACTIVATION_ESCALATION` + audit, budget-before-bound, ranking intersection, canonical record with `decidedAt` excluded); 12 test functions incl. -race; go/parser import scan proves no `backend/pkg/mcp`/exec/SQL in non-test code |
| MCP route surface proof (2026-11-24) | `backend/pkg/api/mcp.go` (six session-guarded routes over `mcp.Gate`; grants only against a reviewed policy, `vault:` policies 503, `stdio://` never dialed) with 7 route tests in `backend/pkg/api/mcp_test.go`, the SSE binding in `backend/pkg/mcp/sse.go` (+7 tests), and `scripts/postman-route-contract.json` carrying 40 control routes |
| P7.12 run control proof (2026-11-24) | `backend/pkg/api/workflows_control_test.go` (8 route-test functions + 8 subtests green locally over the emulated D1/Workflows API) with the fake-D1 control surface in `backend/pkg/api/workflows_auth_test.go`; `worker/workflow/VibeWorkflow.test.ts` run-control cases (cancel finality, terminated run never rewritten as failed, capability refusal) — CI-run because workerd cannot start on macOS 12.7.6 |

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
- **P1.4.5 atomic GitHub mirror (2026-11-21).** `worker/light/githubMirror.ts` replaces the Contents-API push loop with the Git Data API — blobs → tree (`base_tree` = head tree) → commit (`parents=[head]`, `[]` when the branch is new) → ref (`PATCH` existing, `POST` new) — so one generation lands as exactly ONE commit and the ref move is the commit point (an interrupted mirror leaves orphan objects, never a rewritten history). `github_mirrors` (`migrations/0016_ambitious_human_torch.sql`) stores `last_pushed_sha` + `last_generation_id`, so re-exporting an unchanged generation is a no-op; all three D1 helpers are best-effort, so an un-migrated deployment still mirrors. The token export path answers 502 naming the failed step (`ref`/`head-tree`/`blob`/`tree`/`commit`/`write-ref`) instead of a bare 500. Verified by 8 cases in `worker/light/githubMirror.test.ts` over an injected fake transport, plus the same assertions run against the module in a Node/`tsx` harness (workerd cannot start on macOS 12.7.6, so the committed suite is CI-only); `bun run typecheck`, `bun run lint` (0 errors) and `bun run build` green. NOT verified against real GitHub credentials — that is P1.7.4's push E2E — and the migration (`0016_ambitious_human_torch.sql`) was applied remotely by 2026-11-25 (verified via the `d1_migrations` read-back during the 0021 apply).
- **P1.4.6 create-PR action (2026-11-21).** `POST /api/github-app/push-pr` (Edge) mirrors the newest succeeded generation onto `gen/<first 8 of the generation id>` — head derived from lineage server-side, never from the browser — with the P1.4.5 atomic push, then opens a pull request against the app's mirrored base branch with an auto body (app · generation · verdict · internal commit · branch, plus a changed-files table from `generation_files`: path · op · size · author, capped at 40). A 422 "already exists" lists the open PR for that head and returns it with `reused: true` (double click safe); a 422 without an open PR reports `no changes to pull`. An app that was never exported answers `409 NOT_EXPORTED` instead of silently creating a repository. Frontend: `apiClient.pushPullRequest` + `useCreatePullRequest` + a "Create pull request" action in the History panel that opens the PR URL and reports the outcome. Verified by 5 new cases in `worker/light/githubMirror.test.ts` and the same assertions in a `tsx` harness (`ALL PR HARNESS CHECKS PASSED`, which caught an absolute-URL double-prefix defect); typecheck/lint/build/docs:check green. The post-run banner (`src/routes/chat/components/github-pr-banner.tsx`) offers the same action above the deployment controls when a run completes (not for `think`, not for a cancelled/interrupted run, not for a failed team verdict); `generation_started` clears it and it is dismissible. NOT verified against real GitHub credentials (P1.7.4), and not exercised in a browser — the dev server needs workerd, which cannot start on macOS 12.6.
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
1. ~~P0.3 rollout verification~~ — **verified 2026-11-18 against a real KV namespace.** `VIBE_P03_KV=real … go test -tags e2e ./e2e/ -run P03 -v` passed against the account's live namespaces (staging `VibecoderStore-staging` = `4748f35d77274731a17fa4f32c8c3e00`, created for this): anonymous → 401, unknown token → 401, store outage → 503, valid token → handler, three token transports (header / bearer / cookie), and owner scoping `own=200 · cross-user=403 · unknown=404 · anonymous=401` across the seven guarded routes. Emulator mode (`VIBE_P03_KV=emu`) stays the default for CI. Remaining for production: the same proof on a deployed control plane with `CONTROL_PLANE_REQUIRE_SESSION=true` (staging service, P0.3 rollout item) — the deployed-plane leg ran on PRODUCTION 2026-11-21 (see the M0 slice entry under *Implemented But Unverified*); there is no staging plane by decision D17 (2026-11-22) — the deployed-plane leg is proven locally instead (2026-11-22 entry below).
   - **Export-path fix (2026-11-22, P0-SEC-001).** Turning the boundary on silently broke every GitHub export path: the Worker's server-side `GET /api/projects/:id/files` (`worker/light/lightApp.ts`) sent no session token, so the control plane answered 401 and the Worker reported `404 No generated files`. `fetchProjectFiles` now forwards the caller's Edge token as `X-Session-Token` from all four call sites (export, push-pr, check-out-of-date/check-remote, OAuth callback), pinned by `worker/light/lightApp.test.ts`. Typecheck green; the Workers-pool test needs CI (macOS < 13.5 blocks workerd locally).
   - **Deployed-plane proof, boundary ON (2026-11-22).** The staging leg is now proven without a paid staging environment: the real control-plane binary (`go build ./cmd`, local Redis, `CONTROL_PLANE_REQUIRE_SESSION=true`, `VIBECODER_STORE_KV_ID=56d249930a4447ee92493a7ff061a3ae`) served HTTP and logged `control-plane session boundary enabled (Edge KV namespace …)`. Matrix over real sockets: `GET /health` → **200**; anonymous `GET /api/projects/:id/files`, `GET /api/agent/:id/connect`, `GET /api/generations/:id` → **401** `{"error":"authentication required"}`; `GET /ws/:id` without a ticket → **401**; and with the Edge store unreachable from this box, an unknown token on both HTTP and WS (`?ticket=…`) → **503** `{"error":"session store unavailable"}` — i.e. **fail-closed, never an allow**. `docker-compose.yml` now forwards both variables so the same proof is one command (`docs/DEPLOY_RENDER_CLOUDFLARE.md` Step 3). Valid-token pass-through and the cross-tenant 403 keep their earlier evidence (real-KV harness 2026-11-18; production matrix 2026-11-21).
2. P0.4 ownership verification — `ownerGuard` is wired on rooms/VFS/deploys; prove cross-tenant addressing answers 404/403 on a deployed control plane with the boundary on.
3. P0.5 WebSocket authorization — wired (`wsGuard` on `/ws/:id`, ticket/session gate; `go test -tags e2e ./e2e/ -run P05`), part of the opt-in P0.3 rollout. **Per-room authorization closed 2026-11-22** (`ownerGuardFor` now also runs on the upgrade: cross-tenant → 403, unowned → 404). **P0-SEC-008 closed 2026-11-22**: the upgrade URL no longer carries the session token — `POST /api/ws-ticket` mints a 15s single-use ticket bound to (user, room), and `wsOriginGuard` refuses a foreign browser Origin with 403. **P0-SEC-009 closed 2026-11-22**: the cookie transport now reads the Edge's real `access_token` cookie (the old `session` name was dead), and a cookie-authenticated request must present a trusted Origin. The P0-SEC-003 audit is fully closed.
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

## ADI Consolidation (2026-11-23, documentation-only)

Mission VIBE-ARCH-CONSOLIDATION-ADI-001: five PROPOSED contracts (`architecture/adaptive-intelligence-architecture.md`, `architecture/composition-lock.md`, `architecture/context-activation-and-memory.md`, `architecture/evaluation-and-evolution.md`, `architecture/adr-adaptive-intelligence-boundaries.md`); ecosystem docs carry short ADI pointers; backlog grows by 23 design-only ADI tasks (all TODO/BLOCKED; none in NEXT_EXECUTION). No code, schema, migration, route, or pricing change. **ADI-FOUND-001 is DONE (2026-11-23, audit only):** `architecture/ir-parity-audit.md` proves with executed probes that the Go reader is the lenient side in 8 of 10 cases, that cycles/reachability are unenforced at the IR boundary on both sides, and that `Canonicalize` accepts trailing content; **ADI-FOUND-001a is DONE (2026-11-23):** both readers now refuse the same thirteen probes, cycles and unreachable nodes are errors at the IR boundary (not only at runtime), `Canonicalize` rejects trailing content and duplicate keys, and a shared positive control guards against over-rejection. Evidence: `go test ./pkg/ir/` (13 named probes) + 17 gate probes + `docs:check`; no schema bump, no runtime change. **ADI-FOUND-002 is DONE (2026-11-23, design only):** `architecture/composition-lock.md` is a normative contract (artifact shape, digest/canonical rules, three resolution modes, eight-step algorithm, eight failure codes, snapshot semantics, migration table, nine acceptance tests) — no resolver, store or route exists yet. **ADI-FOUND-003 is DONE (2026-11-23, design only):** `architecture/composition-resolver.md` specifies the AssetSource contract, the eight-step deterministic algorithm, the `E_INCOMPATIBLE_VERSION` rule, eleven failure codes with precedence, and a 21-case test specification; **ADI-FOUND-003a is DONE (2026-11-23, code):** `backend/pkg/composition` — bundled-mode resolver with 24 green tests (P1–P6, N1–N12, D1–D3, G1) including `-race`, a golden 9-entry M1 lock with real sha256 digests, and no network/D1/route dependency. **ADI-FOUND-004 is DONE (2026-11-23, design only):** `architecture/run-snapshot.md` fixes the run↔snapshot relation (one snapshot per run, digest identity, append-only, content-deduplicated), the ten-field stored record, the five admission checks with codes, retention/GDPR rules, and migration-by-transformer. It records plainly that no snapshot column exists today, so **no run is snapshot-bound yet**. **ADI-FOUND-004a is DONE (2026-11-23, storage + admission logic):** migration `0017_nebulous_shatterstar.sql` (`composition_snapshots` + nullable `snapshot_id`, purely additive, no backfill), `composition.Snapshot`/`SnapshotStore`/`Verify`/`AdmitRun`, and a D1 adapter with `ON CONFLICT DO NOTHING` and no update path. 13 tests green; typecheck/lint clean. **ADI-FOUND-004b is DONE (2026-11-23, live wiring, opt-in):** the workflow trigger accepts an optional `snapshotId`, `hub.AdmitRunSnapshot` refuses before any instance row exists, the binding is recorded as `NULLIF(?, '')`, and `CONTROL_PLANE_REQUIRE_SNAPSHOT` makes binding mandatory (7 route tests prove a refusal writes no row and starts no runtime instance). **No producer writes snapshots yet** — the run path has no System to resolve — so the switch stays off by default and enabling it today refuses every run. **ADI-CTX-001 is DONE (2026-11-23, design only):** `architecture/context-assembly.md` defines the context envelope (closed `kind`/`scope` vocabularies, provenance on every segment, knowledge vs memory vs profile structurally distinguishable), the 8-candidate / 600-token / policy-budget bounds with a deterministic drop order that refuses rather than truncates mandatory segments, and six failure codes; no provider or store exists yet, and the umbrella doc now carries only a summary plus pointer. **ADI-CTX-002 is DONE (2026-11-23, design only):** `architecture/activation-policy.md` separates deterministic eligibility (narrowing only) from probabilistic ranking (ordering only) with `mcp.Gate` as call-time authority, states `activated ⊆ ranked ⊆ eligible ⊆ candidates ⊆ closure` with a code per violated link, mandates deny-overrides plus default-deny, and pins a per-candidate decision record where every decision is attributable; selectors reuse `backend/pkg/mcp` vocabulary. No engine exists. **ADI-MEM-001 is DONE (2026-11-23, design only):** `architecture/memory-provider.md` defines the replaceable memory interface (Remember/Recall/Forget, no Update), the scope containment ladder with an authority matrix (tenant is a wall, scopes only narrow, no cross-subject reads), provenance with explicit-vs-inferred rules, the secret denylist on write, hard bounded recall with total ordering, retention where `Forget` must return the real count, and a `Capabilities` advertisement so a lexical provider must say `SemanticRecall: false`. **No memory store exists** — the Redis index covers VFS files with a hash embedding, and there is no memory table; the adapter choice is deferred. **ADI-MEM-002 and ADI-MEM-003 are DONE (2026-11-23, design only):** scope enforcement was delivered inside `architecture/memory-provider.md` §3-§7 rather than as a duplicate document, and `architecture/personalization-profile.md` defines a profile that is explicitly **not authorization**, with three namespaces replacing the umbrella single ladder, evidence-required inference, and an explicit-never-overridden rule; it recommends shipping explicit-only personalization first. **ADI-EVT-001 is DONE (2026-11-23, design only):** `architecture/event-envelope.md` defines the event envelope (producer-assigned `eventId`, materialised `dedupeKey`, closed category/type rules with no PII, required tenant, `source` vs `subject`, **both** `occurredAt` and `receivedAt`, versions decoupled from the payload schema ref) plus at-least-once duplication semantics where a duplicate is a success and ordering is per-correlation best-effort. No broker, queue or table exists, and none is introduced. **ADI-EVAL-001 is DONE (2026-11-23, design only):** `architecture/evaluation-contract.md` judges a **snapshot** (an asset update is a new subject), closes the five layers with an honest automation matrix (L1/L2 automated, L4 partly, L3 and end-to-end L5 with no pipeline), forbids composite scores, and states that `not_evaluated` never counts as `pass`; an LLM judge may inform L3 only. **ADI-EVAL-002 is DONE (2026-11-23, design only):** `architecture/benchmark-runner.md` designs the batch runner and records the finding that drives it — a snapshot carries `systemDigest` and per-asset ref+digest but **no bytes**, so re-evaluation needs the System document plus an `AssetSource` and must verify both; L2 verifies the stored closure rather than re-resolving. Four exit codes separate pass / fail / incomplete / contract-violation. No binary, dataset or store exists. **ADI-EVAL-003 is DONE (2026-11-23, design only):** `architecture/candidate-lifecycle.md` makes a candidate a snapshot and a stable composition a pointer, so candidates cannot mutate stable compositions by construction; promotion is a compare-and-set with an expected current snapshot id, evidence is valid only for `(snapshotId, dataset digest, evaluatorVersion)`, and un-promoted candidates expire. **ADI-EVAL-004 is DONE (2026-11-23, design only):** `architecture/promotion-and-rollback.md` makes promotion a four-condition conjunction (gate for the target environment + approvals bound to the evaluation artifact + unexpired candidate + compare-and-set), defines rollout on environment pointers because no traffic-splitting layer exists, splits rollback (previously-stable target, no new evaluation, refused if the closure holds a retired asset) from emergency pin (any verified snapshot, named decision, marked pointer), and requires exactly one `audit_logs` row per pointer movement — no movement without its audit row. **ADI-EVT-002 is DONE (2026-11-23, design only):** `architecture/durable-event-delivery.md` — transactional outbox plus durable inbox on D1 with no broker; the conditional-`UPDATE` claim is safe because D1 is single-threaded, backoff and dead letters are row states, inbox retention must outlive the retry window (otherwise a duplicate becomes a second execution), and the capacity arithmetic from the repository own limits makes sweeping a correctness rule. **ADI-EVT-003 is DONE (2026-11-23, design only):** `architecture/event-to-activation.md` keeps subscriptions in the data layer (no IR change, D7 untouched), resolves an event-triggered run through the stable pointer, makes the eligibility filter deterministic and model-free, de-duplicates **per binding** with a database-unique key (the inbox marks received, never started), and states in a table what an event may and may not influence - it never grants, selects capabilities or bypasses the snapshot rule. **ADI-MEM-004 is DONE (2026-11-23, design only):** `architecture/context-retrieval.md` orders retrieval as profile -> memory -> knowledge (the reverse of the assembly drop order), builds one bounded query with no LLM expansion, records `providerKind` so today's lexical `HashEmbedding` knowledge provider is labelled lexical rather than implying semantic recall, enforces bounds twice, and refuses out-of-scope items instead of filtering them. **This closes every ADI design task that can be closed without an open decision (37/45).** The remainder is blocked by decision or by D8, and the next honest step is code, not more contract. **ADI-MEM-001a is DONE (2026-11-23, first memory code):** `backend/pkg/memory` + `backend/pkg/memory/d1store` implement the three-operation provider (Remember/Recall/Forget) with the tenant wall, the scope ladder, the inferred-vs-explicit rule, the secret scan, total ordering and honest capabilities; migration `0018` adds `memory_records` additively. 11 tests green, backend suite green, vet/typecheck/eslint clean. **The provider is lexical** (`SemanticRecall: false`) and has no route or agent caller, so D8 is untouched. **ADI-EVAL-002a is DONE (2026-11-23, second code task):** `backend/pkg/evaluation` runs L1 (re-validate the System document) and L2 (verify the stored closure) for real, reports L3/L4/L5 as `not_evaluated` with precise reasons, and maps outcomes to four CI exit codes; the artifact digest excludes the clock so two runs compare equal. 13 tests green, full backend suite green, vet and docs gate clean. A document that is not the snapshot's refuses the run as a **subject** error rather than a layer failure. **ADI-FOUND-005 is DONE (2026-11-23, design only):** `architecture/ir-to-run-binding.md` closes the last named gap — the **procedural** shape compiles 1:1 onto the existing DAG (same seven node kinds, `next[]` to edges), while the **agentic** shape is refused with `E_BIND_SHAPE` because no runtime node type runs an agent. It fixes the compile contract, the mapping table, deterministic ordering, three additive run fields and five refusal codes; **ADI-FOUND-005a is DONE (2026-11-23, code):** `backend/pkg/plan` compiles a verified snapshot plus its System document into a plan (Steps/DependsOn and Edges/When from one mapping, deterministic Kahn ordering, five refusal codes), and refuses agentic Systems by design. 10 tests green, vet and the full backend suite clean. The tests forced a real correction: the IR `when` belongs to the **target** node. **ADI-FOUND-004c is DONE (2026-11-24, the producer):** the resolve → persist → compile → start flow is wired on the live trigger path — `POST /api/workflows/trigger` accepts an inline `system` document, resolves it against the embedded bundled catalog (`backend/pkg/composition/bundle`, parity-tested against the docs examples), persists the snapshot and compiles the plan before anything durable is written, then admits it through the same `AdmitRunSnapshot` guard. Migration `0019` adds the three plan columns (`plan_kind`, `plan_digest`, `compiler_version` on `workflow_instances`), written only when a plan was compiled. 12 route tests + 2 bundle tests green (`-race` clean). `CONTROL_PLANE_REQUIRE_SNAPSHOT` is now satisfiable and stays off by default; the bundled catalog is the example set, so refs outside it refuse with `E_MISSING` until the registry (ADI-ECO-002, O27). The next honest step is `ADI-CTX-003` (activation engine) or the agentic-execution ADR. D7–D18 preserved; D19 proposed; O26–O28 opened (O1–O10, O14–O16, O18–O20, O22–O25 stay open; O21 resolved by D17). **Added 2026-11-25:** `ADI-CTX-003` is DONE (`backend/pkg/activation`, the eligibility core as a pure library) and `ADI-CTX-004` is DONE as a store (`activation.TraceStore` + `ValidateRecord`, `backend/pkg/activation/d1store`, additive migration `0021` `activation_decision_traces`; replay is a no-op, a different record for the same run is refused, a corrupt row is refused on read, an unattributable decision never reaches D1). No caller exists — no assembler (ADI-CTX-001) and no policy source — so no decision row is produced on the live path; the next owner gate is a caller, and agentic execution remains a PROPOSED ADR. Migration `0021` was applied remotely on 2026-11-25 (the same run also landed the then-pending `0017`–`0020`), after `bun run d1:token` refreshed the expired D1 token.

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

