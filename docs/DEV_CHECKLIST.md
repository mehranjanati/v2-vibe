# DEV CHECKLIST — Canonical Ordered Implementation Backlog

> **Single canonical backlog.** Every task has an explicit ID and status.
> Historical task IDs are preserved verbatim and mapped into the target P0–P6
> phase model below — never renumbered. Strategy:
> [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md). Current reality:
> [`DEV_STATUS.md`](DEV_STATUS.md). Task-spec quality contract:
> [`DEV_TASKS_SPEC.md`](DEV_TASKS_SPEC.md).
> Specs: [`DEV_SPEC_P1A.md`](DEV_SPEC_P1A.md) ·
> [`DEV_SPEC_P1B.md`](DEV_SPEC_P1B.md) · [`DEV_SPEC_P2.md`](DEV_SPEC_P2.md) ·
> [`DEV_SPEC_P3P4.md`](DEV_SPEC_P3P4.md) · [`DEV_SPEC_P5.md`](DEV_SPEC_P5.md) ·
> [`DEV_SPEC_P6.md`](DEV_SPEC_P6.md).
> Docs-maintenance history (not the product backlog):
> [`DOCS_AUDIT_BACKLOG.md`](DOCS_AUDIT_BACKLOG.md) — all 16 tasks closed.
>
> Status vocabulary: `planned` | `in_progress` | `implemented_unverified` |
> `verified` | `blocked` | `obsolete`. Tick rule: only when the task's
> tests/build are green (rule 1). Never mark planned work implemented because
> the architecture "could support it".

## Historical ID → Target Phase Map (normative)

Historical IDs keep their meaning only through this map. Read the map; never
infer phase from a legacy prefix.

| Legacy ID(s) | Legacy meaning | Target phase | Notes |
|---|---|---|---|
| P0.1, P0.2 (+ sub-IDs) | Team skeleton; DeepAgent migration | **P1** Agent Primitive | The team is a canonical building block; the migration is done (`verified`). |
| P0.3–P0.9 | Security/tenancy/identity foundation | **P0** Foundation | 1:1 — the target P0 exists because of these. |
| P1.0 (+ P1.0.0–P1.0.5) | Repo cleanup + D1 tooling | **P0** Foundation | Hygiene that unblocks everything; mostly `verified`. |
| P1.1 (tables/migration) | Generation lineage schema | **P1** Agent Primitive | Lineage is a primitive: `generations`, `generation_files`, `generation_audits`. |
| P1.1.3 local migrate | Blocked migrate step | **P0** Foundation | Environment/tooling blocker, stays `blocked` here. |
| P1.2 (recorder) | Generation lineage recorder | **P1** Agent Primitive | `verified`. |
| P1.3.0 (D1-CAS decision) | Storage design decision | **P1** Agent Primitive | Decision record; implementation is P1.3.4–P1.3.6. |
| P1.3.1/P1.3.2 (run hooks) | Lineage run hooks | **P1** Agent Primitive | `verified`. |
| P1.3.3 (author threading) | Write attribution | **P1** Agent Primitive | `implemented_unverified`. |
| P1.3.4–P1.3.6 (internal Git) | Versioned system state | **P5** Systems & Marketplace | Git is the system-versioning substrate. |
| P1.4 (History API) | Lineage read API + mirror | **P5** Systems & Marketplace | System version/diff/rollback surface. |
| P1.5 (frontend History) | Lineage UI | **P4** Workbench | Inspection UI for system state. |
| P1.6 (reviewer diffs) | Relative review | **P2** Compiler | Diff-aware review feeds plan validation + repair. |
| P1.7 (P1 validation) | Phase gate | **P1** Agent Primitive | Kept as the P1 exit gate. |
| P1.8 (difficulty gate) | Team-vs-coder routing | **P2** Compiler | First slice of model/team routing. |
| P1.9 (`vfs_claim`) | Write coordination | **P2** Compiler | Concurrency control for composed teams. |
| P1.10 (identity pipeline) | Identified generation | **P4** Workbench | Artifact identity for the workbench/preview loop. |
| P1.11 (maxTokens) | Token-budget fix | **P1** Agent Primitive | Budget plumbing the router will drive. |
| P2.1–P2.6 (node catalog) | Capability packaging | **P1** Agent Primitive | Node packages = first registry content. |
| P2.7 (isolation/security/quota) | Runtime policy | **P0** + **P3** | Ownership halves → P0.3–P0.7; execution halves → P3. |
| P2.8 (E2E/exit path) | Validation + export | **P3** + **P5** | Bundle gate → P3; open export → P5. |
| P3.1–P3.4 (suspense/RFT/circuit) | Output quality + repair | **P6** Factory | Repair-loop building blocks. |
| P3.5 (postmortems) | Deferred research notes | **P6** Factory | Decision-note tasks, not code. |
| P4.1/P4.2 (sandbox) | Execution environment | **P4** Workbench | Agent tooling environment. |
| P5.1–P5.6 (App Runtime) | Per-app backend | **P5** Systems | Reframed as the first **reference system**, not a vertical. |
| P6.1–P6.8 (workflow canvas) | Visual workflow work | **P4** + **P3** | Inspection view (P4) + triggers/observability (P3). |

## Task template (every item below carries these fields)

`id` · target `phase` · `title` · `status` · `goal` · `why` (platform
reading) · `dependencies` · `affected files` (when known) · `implementation
notes` · `acceptance criteria` · `verification` · `unblock condition` (if
`blocked`). A developer must be able to pick a task and implement it without
prior chat history.

---

## P0 — Foundation (target)

Purpose: identity, tenancy, security, credentials, authorization, agent
identity, quotas, isolation, audit. Everything multi-tenant depends on this
phase; nothing in P2–P6 ships to other users' data without it.

### P0.3 — Control-plane auth boundary

- **id:** P0.3 · **phase:** P0 · **title:** Control-plane auth boundary · **status:** `implemented_unverified`.
- **Goal:** every mutating control-plane route enforces an explicit session/ownership check.
- **Why:** Edge auth does not protect Go; without this, multi-tenancy is fiction.
- **Dependencies:** P0.9 decision informs the final mechanism; the provisional check landed first (see notes).
- **Affected files:** `backend/pkg/api/auth.go`, `backend/pkg/api/routes.go`, `backend/pkg/api/auth_test.go`, `backend/pkg/cloudflare/kv.go`, `backend/pkg/engine/hub.go`, `backend/cmd/main.go`, `src/services/controlPlaneClient.ts`, `src/lib/control-plane-session.ts`, `src/lib/api-client.ts`, `src/contexts/auth-context.tsx`, `worker/types/auth-types.ts`.
- **Implementation notes:** `sessionGuard` (`backend/pkg/api/routes.go`) now runs `RequireSession` (`backend/pkg/api/auth.go`) in front of the seven routes at `backend/pkg/api/routes.go:302`, `backend/pkg/api/routes.go:305`, `backend/pkg/api/routes.go:308`, `backend/pkg/api/routes.go:312`, `backend/pkg/api/routes.go:315`, `backend/pkg/api/routes.go:320`, `backend/pkg/api/routes.go:321` (`POST /api/agent/session`, `GET /api/agent/:id/connect`, `POST /api/projects/:id/deploy`, `GET /api/projects/:id/files`, `POST /api/projects/:id/github-export`, `POST /api/workflows/trigger`, `GET /api/workflows/:workflowId`). Token sources: `Authorization: Bearer`, `X-Session-Token`, `session` cookie. Provisional verifier = `EdgeKVVerifier` reading `session:token:<token>` from the Edge `VibecoderStore` KV namespace (`backend/pkg/cloudflare/kv.go`), i.e. the split is **contracted** (Edge stays the identity authority), not unified — the P0.9 record still owns the final choice. Enforcement is opt-in: `CONTROL_PLANE_REQUIRE_SESSION` (`backend/cmd/main.go`), and it fails closed (503) when the KV namespace/credentials are missing. `RequireOwner` is the reusable ownership primitive for P0.4/P0.7.
- **Acceptance criteria:** passed for the guarded surface when the boundary is installed — anonymous mutate → 401, unknown token → 401, cross-user resource → 403 (owner middleware), store outage/misconfiguration → 503, valid token → handler runs. NOT yet closed: the boundary is opt-in (a deployment that does not set `CONTROL_PLANE_REQUIRE_SESSION` keeps the old behaviour), `GET /ws/:id` is still ungated (P0.5), and per-resource ownership wiring + the Edge-token propagation for a Go-login deployment are open (P0.4/P0.9).
- **Verification:** `backend/pkg/api/auth_test.go` (11 cases: the 7 guarded routes anonymous, unknown token, valid token, three token transports, fail-closed 503 twice, owner 401/403/404/200, identity locals) + `backend/pkg/cloudflare/kv_test.go` (4 cases) green; `cd backend && go vet ./... && go test ./...` green; `bun run typecheck && bun run lint && bun run build` green.


### P0.4 — Resource ownership model

- **id:** P0.4 · **phase:** P0 · **title:** Resource ownership model · **status:** `planned`.
- **Goal:** rooms/VFS/deploys scoped by `user_id`, not bare chat/project id.
- **Why:** rooms/VFS/deploys are keyed by chat/project id with no ownership — user B can address user A's project.
- **Dependencies:** P0.3.
- **Affected files:** `backend/pkg/engine/hub.go`, `backend/pkg/api/routes.go`, schema.
- **Acceptance criteria:** ownership column + per-query scoping; user B cannot read/mutate user A project.
- **Verification:** cross-user Go tests + `go vet ./... && go test ./...`.

### P0.5 — WebSocket authorization

- **id:** P0.5 · **phase:** P0 · **title:** WebSocket authorization · **status:** `planned`.
- **Goal:** ticket or session gate on WS upgrade.
- **Why:** `GET /ws/:id` (`backend/pkg/api/routes.go:318`) upgrades without any ticket/session check.
- **Dependencies:** P0.3.
- **Affected files:** `backend/pkg/api/routes.go`.
- **Acceptance criteria:** no-ticket connect rejected.
- **Verification:** WS auth tests + `go vet ./... && go test ./...`.

### P0.6 — Credential/secret hardening

- **id:** P0.6 · **phase:** P0 · **title:** Credential/secret hardening · **status:** `planned`.
- **Goal:** no plaintext provider tokens in D1; encrypted storage or vault/reference model + revoke.
- **Why:** Edge `storeGitHubToken`/`getGitHubToken` (`worker/light/lightApp.ts:564`, `worker/light/lightApp.ts:575`) persist `access_token` plaintext in D1 `github_tokens`.
- **Dependencies:** none (prerequisite for P2.7/P6.4 credential vault — merge, don't duplicate).
- **Affected files:** `worker/light/lightApp.ts`, `migrations/0008_github_tokens.sql`.
- **Acceptance criteria:** D1 holds ciphertext/reference only; resolve + revoke work.
- **Verification:** D1-content test (no plaintext) + `bun run typecheck && bun run build`.

### P0.7 — Workflow authorization + tenant isolation

- **id:** P0.7 · **phase:** P0 · **title:** Workflow authorization + tenant isolation · **status:** `planned`.
- **Goal:** workflow resources explicitly scoped to user/tenant ownership.
- **Why:** `workflow_dags.workflow_id` is chat/project id with no `user_id` (`worker/database/schema.ts:634`); `handleWorkflowTrigger`/`handleWorkflowGet` (`backend/pkg/api/workflows.go:42`, `backend/pkg/api/workflows.go:107`) skip ownership.
- **Dependencies:** P0.3, P0.4.
- **Affected files:** `backend/pkg/api/workflows.go`, `worker/database/schema.ts`, `worker/workflow/VibeWorkflow.ts`.
- **Acceptance criteria:** user B trigger/get on user A workflow → 403 + audit. Gates multi-tenant workflow exposure.
- **Verification:** cross-user workflow tests + `go vet ./... && go test ./...`.

### P0.8 — Room lifecycle + quotas

- **id:** P0.8 · **phase:** P0 · **title:** Room lifecycle + quotas · **status:** `planned`.
- **Goal:** documented TTL/heartbeat/cap policy + enforcement for rooms.
- **Why:** `EngineHub` (`backend/pkg/engine/hub.go:126`, `backend/pkg/engine/hub.go:203`, `backend/pkg/engine/hub.go:210`) never evicts; no idle TTL, no per-user cap.
- **Dependencies:** none.
- **Affected files:** `backend/pkg/engine/hub.go`, `backend/pkg/engine/room.go`.
- **Acceptance criteria:** idle room evicted; quota cap returns explicit error.
- **Verification:** lifecycle/quota tests + `go vet ./... && go test ./...`.

### P0.9 — Identity source-of-truth decision

- **id:** P0.9 · **phase:** P0 · **title:** Identity source-of-truth decision · **status:** `planned`.
- **Goal:** one recorded, documented identity/session architecture decision.
- **Why:** split-brain — Edge KV sessions (`session:token:*` in `worker/light/lightApp.ts:482`) vs live Go PG handlers (`handleRegisterPG`/`handleLoginPG` in `backend/pkg/api/auth_pg.go:98`, `backend/pkg/api/auth_pg.go:158`) vs dead Go D1 handlers (`handleRegisterD1`/`handleLoginD1` in `backend/pkg/api/auth_d1.go:96`, `backend/pkg/api/auth_d1.go:152`, unregistered in `backend/pkg/api/routes.go`). No Go handler reads the Edge cookie.
- **Dependencies:** none (decision first; implementation follows).
- **Affected files:** decision record + `docs/llm.md`, `docs/setup.md`, `docs/POSTMAN_COLLECTION_README.md`.
- **Acceptance criteria:** recorded decision (unify vs contract the split) and docs updated. Do not invent unification.
- **Verification:** `bun run docs:check` green; one documented owner.
- **Unblock condition:** P0.4/P0.5 mechanism choice waits on this; provisional checks may land first.

### P0 history (verified, IDs preserved) — maps to target P0 (hygiene) and P1 (team primitive)

- [x] P0.1 — Multi-agent team skeleton (Coordinator/Coder/Reviewer) — STATUS: `verified` → target P1.
  - [x] P0.1.1 — `backend/skills/00_coordinator.md` + `03_reviewer.md`
  - [x] P0.1.2 — `backend/pkg/engine/team.go`
  - [x] P0.1.3 — skills registry + env override
  - [x] P0.1.4 — hub/room/cmd wiring + single-coder fallback
  - [x] P0.1.5 — WS events (team_started/activity/completed)
  - [x] P0.1.6 — frontend handler + typecheck/lint green
  - [x] P0.1.7 — tests green + `go vet/build` green

- [x] P0.2 — DeepAgent migration + Eino bump (2026-09-30) — STATUS: `verified` → target P1.
  - [x] P0.2.1 — `github.com/cloudwego/eino v0.9.19 → v0.9.21` (`backend/go.mod`/`go.sum`) + new indirect dep `github.com/bmatcuk/doublestar/v4 v4.10.0` (from `adk/filesystem`, imported by `adk/prebuilt/deep`)
  - [x] P0.2.2 — `Engine.NewRoleModel` + `RoleModelConfig` (`backend/pkg/agent/eino_engine.go:115-176`): builds a separate per-role model on the same endpoint with the role's `MaxTokens`/`Temperature`; the `RoleModelConfig` key ignores an empty budget and falls back to the engine model
  - [x] P0.2.3 — coordinator migrated from `adk.NewChatModelAgent` + two `adk.NewAgentTool` to `deep.New` (`backend/pkg/engine/team.go:180-191`): `SubAgents: [coder, reviewer]`, `WithoutGeneralSubAgent: true`, `MaxIteration: 30` (was 24). DeepAgent is a deliberate choice — `adk/prebuilt/supervisor`, the workflow agents and `deterministic_transfer` are explicitly **NOT RECOMMENDED** in the pinned version's source, which recommends DeepAgent in the same breath.
  - [x] P0.2.4 — `roleModel` (`backend/pkg/engine/team.go:280-308`) carries per-role budgets from the registry to the engine (previously all three roles shared `r.eng.ChatModel()` and the registry `Temperature`/`MaxTokens` were no-ops on the team path)
  - [x] P0.2.5 — `noFormatInstruction` (`backend/pkg/engine/team.go:310-334`) on coder and reviewer: bypasses the ADK default FString pass. **Required, not cosmetic** — DeepAgent's built-in `write_todos` sets a session value and the task tool forwards it to sub-agents via `withSharedParentSession()`, so `defaultGenModelInput` would break on the braces in `02_coder.md`/`01_planner.md`
  - [x] P0.2.6 — `teamToolContract` (`backend/pkg/engine/team.go:350-366`) on the coordinator prompt: the mechanical `task`-tool contract + `write_todos` + **an explicit anti-parallelize override** (DeepAgent's built-in prompt advises parallelizing, but steps have dependency order)
  - [x] P0.2.7 — `docs/MULTI_AGENT.md` aligned (new DeepAgent runtime contract section + 26 fixed line refs + gap #6). Reference rule: library files are cited **without** line numbers because `scripts/validate-spec-refs.mjs` resolves only in-repo paths
  - ⚠️ no test coverage: `go build ./...` green and `bun run docs:check` green, but `pkg/engine` does not compile its tests due to P1.0.4/generation_test.go; the team E2E (approve scenario) is manual

## P1 — Agent Primitive (target): lineage, attribution, budgets, capability registry

Purpose: the canonical building blocks agentic systems compose from —
AgentDefinition, ModelDefinition, ToolDefinition, SkillDefinition,
MemoryDefinition, PolicyDefinition, capability registry, versioned manifests.
Platform reading: today's lineage/author/budget/catalog work *is* primitive
construction; P1.7 gates the phase.

### P1.0 — Platform repo cleanup + D1 tooling (IDs preserved; maps to target P0)

- **id:** P1.0 · **phase:** P0 · **status:** `planned` overall (sub-items below carry their own states).
- **Goal:** clean tree, working D1 tooling, no secret leaks into git.
- **Why:** hygiene that unblocks everything; legacy order kept for traceability.
- **Dependencies:** none.
- **Acceptance criteria:** all sub-items `verified`; `docs:check` + full gates green.
- **Verification:** `bun run docs:check && bun run typecheck && bun run lint && bun run build`; `cd backend && go vet ./... && go test ./...
  - [x] P1.0.0 — D1 tooling fix ✅ (2026-09-23): scripts now use `bun --bun wrangler d1 migrations apply v2-vibe --{local,remote} --config wrangler.v2.jsonc`; `db:generate` no drift; production D1 has all 11 migrations + 29 tables. ⚠️ the `--local` path needs workerd (macOS < 13.5 fails) → run in CI/DevContainer. Details: `docs/DEV_SPEC_P1A.md`
  - [x] P1.0.1 — ✅ (2026-09-28) on branch `chore/p1.0-repo-cleanup` in 3 commits: (1) `feat(phase-1): commit multi-agent engine, workflow API and dual-plane wiring`, (2) `chore: remove retired worker/agents, space and container surfaces` = 372 net deletions (worker/agents 145 + worker/services 73 + worker/api 58 + worker/utils 25 + worker/database 12 + worker/middleware 4 + worker/logger 3 + worker/config 2 + worker/types 1 + worker/observability 1 + `worker/app.ts` + `space/` 34 + `container/` 11 + `SandboxDockerfile` + `scripts/deploy.ts`) plus 10 type modules renamed into `worker/types/`, (3) `docs: track the spec set, CF limits, audit backlog and archive` = all of `docs/**`
  - [x] P1.0.1b — ✅ (2026-09-28) `scripts/validate-spec-refs.mjs` + `scripts/validate-postman.mjs` + `scripts/postman-route-contract.json` + `scripts/run-tests.mjs` were committed in the first commit of the same PR, and `docs/DOCS_AUDIT_BACKLOG.md` + `docs/archive/**` in the docs commit (fresh clones/CI get `docs:check` and `test`)
  - [x] P1.0.2 — ✅ (2026-09-28) PR #1 to `github/main` (https://github.com/mehranjanati/v2-vibe/pull/1) merged clean after both jobs (`ci` + `go-test`) went green — merge commit `22a5cb3`
  - [x] P1.0.3 — ✅ (2026-09-28) CI on `main` green post-merge (`ci` + `go-test` jobs on `22a5cb3`); CI + build verified after merge (`go vet ./... && go test ./...` + `typecheck/lint/build`) — note: the gating jobs `lint`/`typecheck`/`test-build`/`go-test` from T15 exist (in `ci.yml` and in both deploy workflows whose `deploy` depends on them); (`docs/DOCS_AUDIT_BACKLOG.md` ← T15) + local gates 2026-09-28: `typecheck`/`lint` (0 errors, 3 warnings)/`build`/`go vet ./...`+`go test ./...`/`docs:check` green; `bun run test` cannot run on macOS 12.7.6 (workerd ≥ 13.5) ⇒ verify in CI only
  - [ ] P1.0.4 — never commit `.dev.vars*`/`.prod.vars`/`.env*`/`.wrangler/`/`dist/`
  - [x] P1.0.5 — `CLOUDFLARE_API_TOKEN` ✅ (2026-09-23): via `bun run d1:token` (new script `scripts/sync-d1-token.ts`) a fresh OAuth token is read from wrangler, validated with `SELECT 1` against D1, and written to root `.env`; then the `vibesdk-backend` container was rebuilt and its env verified (`len=93` — previously `len=0`). Also `backend/.env`/`.env.example` were cleaned of placeholders and `backend/cmd/main.go` now also reads root `.env`. For a durable token: create a Custom token with `Account → D1 → Edit` once and substitute it for OAuth manually.

- [ ] P1.1 — Generation lineage schema (+ Git columns) — STATUS: `planned` overall → target P1 (Memory/Lineage). Spec: `docs/DEV_SPEC_P1A.md` P1.1. Goal: lineage tables + Git-ready columns live in D1.
  - [x] P1.1.1 — ✅ (2026-09-29) three tables `generations`/`generation_files`/`generation_audits` in `worker/database/schema.ts` (plural names matching the file convention; `generation_files` natural key `(generation_id, path)`; indexes `(chat_id, created_at)` + on `parent`/`status`/`action`; `parent` self-FK with `ON DELETE set null`; child FKs with `cascade`) + `Generation*` types at file end — the new section was added **after the last table** so earlier line numbers stay put (`worker/database/schema.ts:475` is still `audit_logs`) ⇒ `docs:check` green
  - [x] P1.1.1b — ✅ (2026-09-29) columns `commit_sha` + `branch` + `fork` (boolean, default `false`) on the same `generations` table ⇒ arrived in the same P1.1.2 migration (no second migration needed, per the spec's "don't split" rule)
  - [x] P1.1.2 — ✅ (2026-09-29) actual name from `bun run db:generate`: **`migrations/0011_jittery_the_liberteens.sql`** (+ `migrations/meta/0011_snapshot.json` + `idx: 11` entry in `_journal.json`); SQL contains only `CREATE TABLE`×3 + `CREATE INDEX`×6, no `DROP`/`ALTER` (no drift); re-running `db:generate` = "No schema changes"
  - [ ] P1.1.3 — `bun run db:migrate:local` + green tests — STATUS: `blocked` (machine-class only).
### P1.2 — Generation lineage recorder — STATUS: `verified` (2026-09-29) → target P1 (Memory/Lineage). Spec: `docs/DEV_SPEC_P1A.md` P1.2. Goal: record open/finish + diff + audit for every generation.
  - [x] P1.2.1 — ✅ `StartGenerationRecord(ctx, chatID)` (`backend/pkg/engine/generation.go:318`): full VFS snapshot (hash+size+content in one JSON) on `vfs:snap:{genID}` + in-memory mirror (no-Redis path) + insert of a `running` row in D1; `parent` = latest `succeeded` of the same chat (`generationParentSQL`; tie-break on `rowid` because `CURRENT_TIMESTAMP` has second precision and two generations in one second must chain in insertion order)
  - [x] P1.2.2 — ✅ `FinishGenerationRecord(ctx, genID, verdict)` (`backend/pkg/engine/generation.go:396`): diff snapshot↔current VFS (`create/modify/delete`, sorted by path, untouched file = no row) → `INSERT OR REPLACE` into `generation_files` + closing the row (`UPDATE generations`) + audit in a separate goroutine (`generation_finished` with actor=system and one `file_written` per file carrying an author); verdict→status mapping: `request_changes` = `succeeded` (files landed), `error` = `failed`, empty = `cancelled`; all errors advisory (logged, never fail the run)
  - [x] P1.2.3 — ✅ sha256 hash via the standard library (`hashVFSFile`); nil-Redis guard (interface `generationKV` + `generationKVOrNil` + one mirror slot in `backend/pkg/engine/room.go:105-120` enabling diff without Redis; single slot so a cancelled run that never gets `Finish` cannot leak a VFS version); 7-day TTL on `vfs:snap:*` (`generationSnapshotTTL`, `backend/pkg/engine/generation.go:50`)
  - [x] P1.2.4 — ✅ `backend/pkg/engine/generation_test.go`: 7 tests — (1) parent chain over two runs + snapshot TTL/content, (2) three-op diff create/modify/delete + `author_agent` attribution, (3) nil-Redis/nil-D1 (both functions error-free, generation alive: VFS untouched + second run), (4) diff without Redis but with D1 (mirror path), (5) snapshot recovery from Redis by another room (modify, not fake create), (6) verdict→status mapping, (7) advisory errors (empty id/missing generation). D1 simulated with an in-memory REST server (httptest + `cloudflare.D1Client.SetBaseURL`, already designed for tests) and Redis with a fake over the `generationKV` interface. Evidence: `go vet ./...` clean + `go test ./...` green + `go test -race ./pkg/engine/ -run TestGeneration` green
  - Note: `commit_sha`/`branch` (P1.3.5) and `fork` (P1.3.3/P1.10.7) are deliberately not written in this step; the author-intake (`RecordWriteAuthor`, `backend/pkg/engine/generation.go:264`) is ready for P1.3.3 to just wire up
### P1.3 — Execution hooks + internal Git (IDs preserved; implementation maps to target P5)

- **id:** P1.3 · **status:** mixed (P1.3.1/P1.3.2 `verified`; P1.3.3 `implemented_unverified`; P1.3.4–P1.3.6 `planned`).
- **Goal:** lineage hooks live; versioned system state in D1-CAS Git.
- **Why (platform):** hooks = lineage primitive (P1); internal Git = system-versioning substrate (P5).
- **Dependencies:** P0.3–P0.9 for multi-tenant exposure; P1.1/P1.2 done.
- **Acceptance criteria:** per sub-item.
- **Verification:** `cd backend && go vet ./... && go test ./...` incl. `gitrepo_test.go`.
  - [x] P1.3.0 — ✅ (2026-09-29) **Decision: D1-CAS** (full rationale + rejection of the two alternatives in `docs/DEV_SPEC_P1A.md` → P1.3.0): internal history as a content-addressed object store in **the same D1** as lineage (`git_objects`: `sha`/`kind`/compressed `content` + a ref row), **no bare repo on disk**; `go-git` stays so SHAs remain real and the promised P1.3.4 API (`Init/Open/Commit/Log/Diff/Revert`) is unchanged. **(a) volume rejected:** the backend container has no volume and runs on ephemeral FS (`docker-compose.yml:24`; the only volume belongs to Redis: `docker-compose.yml:16-17`), the image is `nonroot`/distroless (`backend/Dockerfile:19`), and a stable path is impossible in local dev on macOS. **(b) R2-remote is blocked, not unsuitable:** R2 is not enabled on this account (`wrangler.v2.jsonc:49-51`, enforced by `scripts/validate-docs-invariants.mjs:120`) **and there is zero R2 code in Go** (measured across all of `backend/`: no match in `*.go`; the only `\\br2\\b` match is a roadmap item id: `backend/docs/architecture-roadmap.json:100`) ⇒ revisit deferred to the "R2 enabled" event. Numbers: new section **`docs/CF_LIMITS.md` §8** (source `[S7]` = official D1 Limits page; `Last updated 2026-04-21`, `Last verified 2026-09-29`) — row cap `2 MB`, statement length `100 KB`, `bind` params `100`, DB size `500 MB` (Free)/`10 GB`, Time Travel `7/30 days`, query duration `30s`, single-threaded. Evidence: `bun run docs:check` green (3 validators incl. `CF_LIMITS` invariants and line refs).
  - [x] P1.3.1 — ✅ (2026-09-29) hook in `runTeam` (`team.go`): signature moved to `(string, error)` (`backend/pkg/engine/team.go:112`) and returns the reviewer's real verdict (`approve`/`request_changes`, else `done`); no second record opened because it nests inside `runDualModelPipeline` (one generation = one row). Evidence: `go test -race ./pkg/engine/` green + `TestParseReviewVerdict` present and 4 hook tests in `backend/pkg/engine/generation_hook_test.go`; the real team path end-to-end is not covered without an engine (team approve E2E stays manual)
  - [x] P1.3.2 — ✅ (2026-09-29) hook in `runDualModelPipeline` (`dual_model.go`): record start at function top (`backend/pkg/engine/dual_model.go:41`) + three explicit `Finish` calls before each `finalizeGeneration` (`backend/pkg/engine/dual_model.go:158`/`backend/pkg/engine/dual_model.go:191`/`backend/pkg/engine/dual_model.go:231`) + deferred safety net for early-death paths (`backend/pkg/engine/dual_model.go:80-84`: error→`failed`, else `cancelled`). Tests: 4 tests in `backend/pkg/engine/generation_hook_test.go` (full run→`succeeded`/`done` + correct diff; empty plan→`failed`/`error`; reject→`cancelled`+NULL+audit; cancel→`cancelled`+NULL+audit) — evidence: `go vet ./...` clean + `go test ./...` green + `go test -race ./pkg/engine/ -run 'TestRunDualModelPipeline|TestGeneration'` green
  - [ ] P1.3.3 — `author_agent` threading — STATUS: `implemented_unverified` (code threaded, verification missing; do NOT tick until P1.3.3-verify lands). Implementation: explicit `author` on `UpsertFile`/`DeleteFile` (`backend/pkg/engine/room.go:628`, `backend/pkg/engine/room.go:648`) recorded via `RecordWriteAuthor` (`backend/pkg/engine/generation.go:264`); tool path carries `coder` through `roomVFSStore` (`backend/pkg/engine/plan_execute.go:47`); `notifyWriteTool` only broadcasts (`backend/pkg/engine/plan_execute.go:95`); gapfill passes `gapfill` (`backend/pkg/engine/gapfill.go:67`); dual-model passes `coder`/`gapfill` (`backend/pkg/engine/dual_model.go:284`, `backend/pkg/engine/dual_model.go:333`, `backend/pkg/engine/dual_model.go:385`); legacy fallback passes `legacy` (`backend/pkg/engine/room.go:1104`). Missing for `verified`: (a) end-to-end test that every write path lands its expected non-empty `author_agent` in `generation_files`, (b) `fork` flag on concurrent manual write, (c) `author` from editor/import channels.
  - [ ] P1.3.4 — Go internal Git: `go-git` in `go.mod` + `backend/pkg/engine/gitrepo.go` (init/open/commit/log/diff/revert per-appId; backend = **D1-CAS per P1.3.0**: `git_objects` table + ref, **no bare repo on disk**) + `docs/CF_LIMITS.md` §8 constraints
  - [ ] P1.3.5 — Git hook in `finalizeGeneration` (single batch commit on `gen/{genId}`) + `author` param on `UpsertFile/DeleteFile` for intake (record, not separate commits — else commit explosion on chunks)
  - [ ] P1.3.6 — pre-commit secret-scan (token|secret|api_key) + per-file size cap (1MB, skip legacy) + mandatory `author`
### P1.4 — History API + diff + rollback (+ GitHub mirror) — STATUS: `planned` → target P5 (system version/diff/rollback surface). Spec: `docs/DEV_SPEC_P1A.md` P1.4.
  - [ ] P1.4.1 — types in `src/api-types.ts` (+ `commit_sha`/`branch` on the generation model)
  - [ ] P1.4.2 — methods in `src/lib/api-client.ts` (+ push/PR/import)
  - [ ] P1.4.3 — Go handler: `backend/pkg/api/generations.go` + D1 access via `backend/pkg/cloudflare/d1.go` (not `worker/database/services/`, which does not exist)
  - [ ] P1.4.4 — route registration in `backend/pkg/api/routes.go` (+ types/methods in `src/api-types.ts` and `src/lib/api-client.ts`, via `controlPlane.baseUrl`)
  - [ ] P1.4.5 — atomic mirror: replace one-by-one `pushFiles` with Git Data API (tree→commit→ref) — one generation = one atomic commit; store `last_pushed_sha`
  - [ ] P1.4.6 — "Push / create PR" banner with diff summary after `team_completed` (base=main, head=gen/{id}, auto body); `repo` token scope for PRs
  - [ ] P1.4.7 — import endpoint (GitHub → VFS): diff against internal HEAD → clean fast-forward / fork + conflict banner; `author=import`
### P1.5 — Frontend History (+ SHA, push banner) — STATUS: `planned` → target P4 (inspection UI for system state). Spec: `docs/DEV_SPEC_P1A.md` P1.5.
  - [ ] P1.5.1 — History tab with verdict badge + `commit_sha`/branch display
  - [ ] P1.5.2 — diff summary view (op + size + hash)
  - [ ] P1.5.3 — rollback with confirm + refresh after `team_completed` (new revert-commit, never history rewrite)
  - [ ] P1.5.4 — mirror-pending state: offline/bad token → internal generation succeeds + "mirror pending" banner
### P1.6 — Reviewer baseline + hub-awareness — STATUS: `planned` → target P2 (diff-aware review feeds plan validation + repair). Spec: `docs/DEV_SPEC_P1B.md` P1.6.
  - [ ] P1.6.1 — `03_reviewer.md` (diff relative to previous generation)
  - [ ] P1.6.2 — inject `changed_files` in `team.go`
  - [ ] P1.6.3 — hub-awareness: identify hub-files (most-referenced files from plan/VFS) + report blast-radius in the reviewer task
### P1.7 — P1 exit gate — STATUS: `planned` → target P1. Spec: checklist (no separate impl spec).
  - [ ] P1.7.1 — `go vet/test` green (incl. `gitrepo_test.go`: generation → SHA, rollback → revert-commit)
  - [ ] P1.7.2 — `bun run typecheck/lint/build` green
  - [ ] P1.7.3 — E2E: two generations → two SHAs on two branches with correct parent → diff → rollback
  - [ ] P1.7.4 — push E2E: one atomic commit in the user's repo + PR with auto body; round-trip (VFS → Git → VFS identical?)
### P1.8 — Difficulty gate for `canRunTeam` — STATUS: `planned` → target P2 (first slice of team routing). Spec: `docs/DEV_SPEC_P1B.md` P1.8.
  - [ ] P1.8.1 — lightweight classifier: step count + dependency density from VFS/plan (≤2 files and no dependencies → single coder, else team)
  - [ ] P1.8.2 — gate tests (easy→single, hard→team) + decision logged to audit
### P1.9 — `vfs_claim` tool (claim/status/release) — STATUS: `planned` → target P2 (write coordination for composed teams). Spec: `docs/DEV_SPEC_P1B.md` P1.9.
  - [ ] P1.9.1 — `claim`/`release`/`status` in `teamTools` (ownership signal only, no history sharing)
  - [ ] P1.9.2 — coordinator: claim plan paths before delegating to coder; release on completion
  - [ ] P1.9.3 — collision test (two concurrent claims on one path → second queued/errored)
### P1.10 — Identified generation + manual-write pipeline — STATUS: `planned` → target P4 (artifact identity for the workbench loop). Spec: `docs/DEV_SPEC_P1B.md` P1.10.
  - [ ] P1.10.1 — coder contract: `data-vibe-block/section/id/slots` in `02_coder.md` + Go validator (Suspense repair: unique id, block guessed from plan)
  - [ ] P1.10.2 — extract `vibe.meta.json` at finalize (list of id/block/section/slots, no HTML parsing in the frontend)
  - [ ] P1.10.3 — G0 intake: `author` param on `UpsertFile/DeleteFile` (merge with P1.3.5, don't duplicate)
  - [ ] P1.10.4 — G1 syntax: new pass with acorn in `preview-normalize` (today regex-only) + htmlparser2/jsonc + extend `SanitizeJS` (status=broken + precise line, still save; merge with P3.1, don't duplicate)
  - [ ] P1.10.5 — G2 identity: `pkg/design/identity.go` + reviewer check (missing → unmanaged, not an error)
  - [ ] P1.10.6 — G3 wiring: missing-ref from the existing `resolve()` + frontend banner + zero-token micro-fix
  - [ ] P1.10.7 — G4 conflict: `fork=true` flag in lineage (same as P1.1.1b) + "mine / agent's" banner
  - [ ] P1.10.8 — G5 preview: red/yellow/gray banner + "back to last healthy" + "fix with AI"
  - [ ] P1.10.9 — Monaco editable + explicit Save (drop readOnly, keep lazy; no autosave)

### P1.11 — G-maxTokens: token budget honored on engine path — STATUS: `planned` → target P1 (budget plumbing the router will drive).
  - [ ] P1.11.1 — `Room.streamLLM` (`backend/pkg/engine/room.go:434-457`) passes `maxTokens` only to the raw path (`streamLLMRaw`); when `r.eng != nil` it calls `Engine.RunStepWith`, which takes no budget. Result: per-role overrides (`PLANNER_MAX_TOKENS`/`CODER_MAX_TOKENS`) and the truncation-retry loop (`maxTokens *= 2`, `backend/pkg/engine/room.go:1062`) are **no-ops** on the live engine path.
  - [ ] P1.11.2 — fix: pass `model.WithMaxTokens` via `adk.WithChatModelOptions` (sweep `backend/pkg/agent/eino_engine.go:201-285`). Note: options are per-agent but `maxTokens` here is per-call, so `RunStepWith` must take a budget param to preserve step-doubling.
  - [ ] P1.11.3 — test: mock `model.BaseModel` reading the budget from options, asserting both passes (12288 and 24576) reach the model.
  - Note: the team path is unaffected (each role's budget is baked into its own model via `Engine.NewRoleModel`). Details: `docs/MULTI_AGENT.md` → Known gaps #6.

## P2 — Agentic Compiler and Composition (target)

Purpose: turn a user outcome into a validated executable agent system —
outcome parsing, capability discovery, agent selection, model routing, tool
selection, team composition, delegation, ExecutionPlan as universal system
IR, system synthesis, plan validation, policy validation.
Platform reading: P1.6/P1.8/P1.9 are the first compiler slices (review
input, team routing, write coordination). Full outcome→system synthesis is
new work filed here as it is specified — no placeholder IDs invented.
Legacy node-catalog work below (P2.1–P2.6) is the first
**capability-registry content** (target P1 primitive); its ownership halves
live in P0.3–P0.7 and its execution halves in P3 — see the map.

### P2.1 — Capability manifest contract + storage — STATUS: `planned` → target P1 (registry content). Spec: `docs/DEV_SPEC_P2.md` P2.1.
  - [ ] P2.1.1 — node manifest (`name@version` + `kind` + `paramsSchema` + `credentials`)
  - [ ] P2.1.2 — `node_packages` table + `0012_node_packages.sql`
  - [ ] P2.1.3 — KV cache (`nodepkg:{name}@{version}`)
### P2.2 — Registry-aware Go validation — STATUS: `planned` → target P1 (registry content). Spec: `docs/DEV_SPEC_P2.md` P2.2.
  - [ ] P2.2.1 — `backend/pkg/engine/nodepkg.go`
  - [ ] P2.2.2 — `Validate(wf, registry)` in `workflowschema.go`
  - [ ] P2.2.3 — tests green
### P2.3 — Table-driven TS dispatch — STATUS: `planned` → target P1 (registry content). Spec: `docs/DEV_SPEC_P2.md` P2.3.
  - [ ] P2.3.1 — `kind → handler` table in `worker/workflow/VibeWorkflow.ts`
  - [ ] P2.3.2 — resolve manifest → validate → execute
  - [ ] P2.3.3 — `NonRetryableError` for bad nodes + test
### P2.4 — Catalog API — STATUS: `planned` → target P1 (registry content). Spec: `docs/DEV_SPEC_P2.md` P2.4. Route facts stay pinned to `docs/POSTMAN_COLLECTION_README.md` §7.
  - [ ] P2.4.1 — `GET /api/nodes` + `GET /api/nodes/:name@:version`
  - [ ] P2.4.2 — `POST /api/workflows/validate`
### P2.5 — Agentic-team wiring — STATUS: `planned` → target P1 (registry content). Spec: `docs/DEV_SPEC_P2.md` P2.5.
  - [ ] P2.5.1 — `02_coder.md`: emit `node.pkg@version`
  - [ ] P2.5.2 — `03_reviewer.md`: catalog check + reference credentials
### P2.6 — 20 curated nodes — STATUS: `planned` → target P1 (registry content). Spec: `docs/DEV_SPEC_P2.md` P2.6.
  - [ ] P2.6.1 — notify: email/slack/telegram
  - [ ] P2.6.2 — http: request/webhook-trigger/webhook-call
  - [ ] P2.6.3 — data: db.query/kv/queue
  - [ ] P2.6.4 — time: cron/sleep + ai: prompt/classify/extract
  - [ ] P2.6.5 — logic: branch/loop/map + premium: stripe/sheets
  - [ ] P2.6.6 — correct/incorrect examples in each node's manifest (two samples, for the coder)
### P2.7 — Isolation + node security + per-app quotas — STATUS: `planned`. Split by map: ownership halves → P0.3–P0.7 (do there, not here); execution halves (log masking, version pinning, quotas) → P3.
  - [ ] P2.7.1 — `user_id` scoping on all `workflow_*` queries + ownership check on trigger
  - [ ] P2.7.2 — credential masking in logs (reference only, never plaintext) + Dual-LLM for untrusted data
  - [ ] P2.7.3 — version pinning + manifest checksum (ETDI) + cross-server dataflow boundary
  - [ ] P2.7.4 — per-app quotas (generation/commit/runtime caps) + secret-storage decision (GitHub token: encrypted KV or D1-ciphertext + revoke cycle) — merge with P6.4, don't duplicate
### P2.8 — Validation + exit path — STATUS: `planned`. Split by map: bundle gate → P3; open export → P5.
  - [ ] P2.8.1 — E2E: `webhook.trigger → ai.extract → slack.postMessage` on CF
  - [ ] P2.8.2 — worker bundle size: `wrangler deploy --outdir bundled/ --dry-run` + log `Total Upload` (platform cap is 64 MiB uncompressed; the 3MiB cap was removed 2026-09-04 — `docs/CF_LIMITS.md` §4)
  - [ ] P2.8.3 — full DAG + history export to an open format (user exit path)

## P3 — Durable Agent Runtime (target)

Purpose: run agentic systems reliably in production — durable execution,
workflow engine, events, retries, timeouts, backoff, human approval,
webhooks, schedules, idempotency, cancellation, execution state,
observability, cost tracking, evaluations. Receives the P2.7-execution and
P2.8-bundle halves plus the P6.5/P6.6/P6.8 runtime pieces (see P4/P6 notes).
Legacy suspense/circuit work below (P3.1–P3.4) is the **repair-loop**
building stock (target P6 Factory); P4 sandbox halves below belong to the
P4 Workbench — see the map.

### P3.1 — Zero-token suspense — STATUS: `planned` → target P6 (repair). Spec: `docs/DEV_SPEC_P3P4.md` P3.1 (merge with P1.10.4, don't duplicate).
### P3.2 — Prompt-cache-aware + catalog RAG — STATUS: `planned` → target P6 (repair context). Spec: `docs/DEV_SPEC_P3P4.md` P3.2.
### P3.3 — Correct/incorrect manifest examples + REQUEST_CHANGES log — STATUS: `planned` → target P6 (repair data). Spec: `docs/DEV_SPEC_P3P4.md` P3.3 (examples merge with P2.6.6 — log only here).
### P3.4 — Deterministic circuit breaker — STATUS: `planned` → target P6 (repair guard). Spec: `docs/DEV_SPEC_P3P4.md` P3.4.
  - [ ] P3.4.1 — pre-persist hook: catastrophic-delete guard + size cap + bailout after 3 failed retries
  - [ ] P3.4.2 — micro-pass at temperature 0 for targeted fixes (no full-file rewrite, no off-by-one-prone bulk patch)
  - [ ] P3.4.3 — internal reflect (ReflexiCoder) vs external reviewer: keep both — reflect for surface errors, reviewer for verdicts
### P3.5 — Open postmortems (Temporal / Dify Human-Input / Mastra runners) — STATUS: `planned` → target P6 decision notes, not code. Spec: `docs/DEV_SPEC_P3P4.md` P3.5.

## P4 — Agent Development Workbench (target)

Purpose: give agents the capabilities for real software and business work —
code, browser, database, HTTP, MCP, shell, files, sandbox, tests, Git,
deployment tooling — plus the inspection UIs (History P1.5, identity P1.10,
workflow inspection P6.1–P6.3 reframed). Sandbox stays outside the light
worker (10 ms CPU on Workers Free; no sandbox/container binding — and the
3 MiB cap no longer exists, `docs/CF_LIMITS.md` §4).

### P4.1 — Per-chat sandbox + Bash in the same FS — STATUS: `planned` → target P4. Control-plane-side container, not the Worker. Spec: `docs/DEV_SPEC_P3P4.md` P4.
### P4.2 — Real verifier + dual-key — STATUS: `planned` → target P4. Spec: `docs/DEV_SPEC_P3P4.md` P4.

## P5 — Agent Systems and Marketplace (target)

Purpose: turn agents, teams, workflows, and systems into reusable/versioned
artifacts — reference systems, system templates, agent/team/workflow
marketplace, system versioning, system diff, fork/remix, publishing,
install/deploy, creator ecosystem.
Platform reading: legacy "App Runtime" (P5.1–P5.6) is reframed as the first
**reference system** (per-app backend proving the platform), not a vertical
SaaS. Legacy internal-Git work (P1.3.4–P1.3.6) is the versioning substrate;
P1.4 is the version/diff/rollback surface.

> Original motive (preserved): without per-app backend, inventory/accounting
> stays a localStorage demo. Real ERP = transactions + roles + audit.

### P5.1 — Per-app data model (fixed tables + `data_json`, no dynamic DDL) — STATUS: `planned` → target P5 reference system. Spec: `docs/DEV_SPEC_P5.md` P5.1.
  - [ ] P5.1.1 — `app_records` table (`app_id`, `table_name`, `row_id`, `data_json`) + indexes — per-app DDL forbidden (D1 constraint)
  - [ ] P5.1.2 — separate real table for stock (`stock`: unique `app_id`+`sku` — for atomic decrement) + reference template: `products`/`movements`/`invoices` (not hardcoded)
  - [ ] P5.1.3 — migration + indexes (`app_id`, `sku`, `created_at`)
### P5.2 — Transactional API — STATUS: `planned` → target P5 reference system. Spec: `docs/DEV_SPEC_P5.md` P5.2.
  - [ ] P5.2.1 — generated per-table CRUD with mandatory `app_id + user_id` scope
  - [ ] P5.2.2 — atomic stock decrement (`UPDATE ... WHERE stock >= qty`, never read-then-write)
  - [ ] P5.2.3 — automatic audit of every write (who, which row, before/after) in `generation_audit` or a separate table
### P5.3 — Roles and access — STATUS: `planned` → target P5 reference system. Depends on P0.3–P0.5 + P0.9 for auth (do not invent auth). Spec: `docs/DEV_SPEC_P5.md` P5.3.
  - [ ] P5.3.1 — roles: `admin`/`storekeeper`/`accountant` + `app_members` table
  - [ ] P5.3.2 — per-table per-role policy (storekeeper: stock RW, accounting RO, and vice versa)
  - [ ] P5.3.3 — wire to existing auth (current JWT/session) + basic penetration test
### P5.4 — Frontend + agent wiring — STATUS: `planned` → target P5 reference system. Spec: `docs/DEV_SPEC_P5.md` P5.4.
  - [ ] P5.4.1 — dashboard from the real API (replaces localStorage) + readable-offline mode
  - [ ] P5.4.2 — coder: generate forms/reports against the real API (no mock data)
  - [ ] P5.4.3 — reviewer: "does data come from the API?" added to the `03_reviewer.md` checklist
### P5.5 — Migration tool (Excel/CSV import with per-row error report, never whole-file fail) — STATUS: `planned` → target P5 reference system. Spec: `docs/DEV_SPEC_P5.md` P5.5.
### P5.6 — Validation: concurrent-sale scenario (two simultaneous writes → stock never negative) + wave-1 E2E — STATUS: `planned` → target P5 reference system. Spec: `docs/DEV_SPEC_P5.md` P5.6.

## P6 — Autonomous Software Factory (target)

Purpose: move from agent construction to continuous autonomous
software/system lifecycle management — intent → system synthesis, build,
test, review, deploy, operate, observe, evaluate, repair, replan, continuous
improvement.
Platform reading: the legacy P6 canvas items below are reframed — graph is
an advanced inspection/debugging representation (P4 workbench surface), while
test-run (P6.5), triggers (P6.6), and observability (P6.8) are durable-runtime
pieces (P3). Repair-loop items (P3.1–P3.4) belong to this phase's
evaluate/repair loop. New factory-loop tasks (synthesis→evaluate→replan)
are filed here as specified — no invented IDs.

> Original motive (preserved): generate → visualize → execute is ready
> (`workflow.json` + `WorkflowVisualizer` + `VibeWorkflow`) but the edit loop
> is open. Build order: L1 → L3 → L2. Prerequisites: wave 1 starts without
> P2 (on the current 7 types); waves 2+ need P2.

- [ ] P6.1 — Wave 1: live canvas on the current 7 types (no P2) — STATUS: `planned` → inspection surface owned by P4; runtime halves → P3.
  - [ ] P6.1.1 — `WorkflowVisualizer`: state-driven component (`useNodesState`/`useEdgesState`) + `onNodeClick` select + `onConnect` with cycle check + delete (node + edges + orphan warning). Note: `showInteractive` on `<Controls>` only locks zoom controls — it does not enable editing (see `docs/DEV_SPEC_P6.md` P6.1); P6.1 = making the component stateful.
  - [ ] P6.1.2 — 7-type palette (`trigger/http/db/ai/email/condition/sleep`) + drag new node with position + default params (complete renderers for all 7 first — see `docs/DEV_SPEC_P6.md` P6.1.1b; drop `function`)
  - [ ] P6.1.3 — save to VFS → new generation (`author=user`) + lineage + internal Git commit (P1-git)
- [ ] P6.2 — L1: params panel (per-node form) — STATUS: `planned` → P4 inspection surface.
  - [ ] P6.2.1 — dynamic form from `validateNodeParams` (later: from P2 node manifest, not hardcode)
  - [ ] P6.2.2 — frontend validation with the existing `validateWorkflowV2` in `worker/workflow/VibeWorkflow.ts` before save
- [ ] P6.3 — L3: guarded JSON editing (parallel with P6.2) — STATUS: `planned` → P4 inspection surface.
  - [ ] P6.3.1 — Monaco on `workflow.json` + schema v2 (live autocomplete/errors)
  - [ ] P6.3.2 — save → G1 pipeline (jsonc-parser) → invalid = draft + precise error banner (not reject)
- [ ] P6.4 — Generic credential vault (wave-2 prerequisite, with P2.7) — STATUS: `planned`. Depends on P0.6 (merge, don't duplicate).
  - [ ] P6.4.1 — credentials table (reference, never plaintext) + "new connection" UI + `repo` scope for PRs
  - [ ] P6.4.2 — log masking + Dual-LLM for untrusted data (merge with P2.7.2, don't duplicate)
- [ ] P6.5 — Test-run (trial execution without real side effects) — STATUS: `planned` → durable-runtime piece owned by P3.
  - [ ] P6.5.1 — side-effect level table in each node's manifest: `safe` (really execute) / `mocked` (log "would send to X" only) / `blocked` (error in test)
  - [ ] P6.5.2 — `dryRun` mode on `POST /api/workflows/trigger` (validate + simulate, no external commit)
  - [ ] P6.5.3 — UI: Test button with sample input + run list + per-step log (backend `workflow_instances/step_logs` already exists)
- [ ] P6.6 — Active toggle + real triggers (wave 4) — STATUS: `planned` → durable-runtime piece owned by P3.
  - [ ] P6.6.1 — per-workflow webhook URL (`/wh/:workflowId` on the Worker) — webhooks first, then cron
  - [ ] P6.6.2 — Worker Cron Triggers + Workflows (minute precision + CF quotas documented; numbers only from `docs/CF_LIMITS.md` §1/§2 — cron-expression length cap is `⚠️ unverified` in official docs, never cite a number for it)
  - [ ] P6.6.3 — Active/Inactive toggle + W3 guard: `email/stripe/http-POST` nodes get a separate confirm banner ("a real email goes out — sure?")
- [ ] P6.7 — Validation: full scenario + bundle budget — STATUS: `planned` → P3 gate.
- [ ] P6.8 — End-to-end observability (one trace-id plan → push) — STATUS: `planned` → durable-runtime piece owned by P3.

## Rules

1. Tick only when the task's tests/build are green (rule 1 in header).
2. When a phase's exit gate passes, record the date next to the gate item —
   never backfill completion dates, never use future dates.

| Target phase | Status | Date |
|---|---|---|
| P0 Foundation | ⬜ open (P0.3–P0.9 `planned`) | — |
| P1 Agent Primitive | ⬜ open (P1.7 exit gate `planned`) | — |
| P2 Compiler | ⬜ open (no exit gate filed yet) | — |
| P3 Durable Runtime | ⬜ open (no exit gate filed yet) | — |
| P4 Workbench | ⬜ open (no exit gate filed yet) | — |
| P5 Systems & Marketplace | ⬜ open (no exit gate filed yet) | — |
| P6 Factory | ⬜ open (no exit gate filed yet) | — |

Legacy per-phase ticks were removed: the old table mixed legacy phase names
with the new model. Per-item status above is the source of truth.

