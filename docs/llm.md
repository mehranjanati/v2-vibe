# VibeSDK Developer Guide

> **Scope:** this guide documents the **current dual-plane architecture only** — the Go control plane in `backend/` plus the light Cloudflare Worker. The retired ThinkAgent/SpaceDO/Cloudflare Artifacts guide was moved verbatim to [`archive/llm-legacy.md`](archive/llm-legacy.md) (docs audit task T7, [`DOCS_AUDIT_BACKLOG.md`](DOCS_AUDIT_BACKLOG.md)).
> Follow [`AGENTS.md`](../AGENTS.md) for commands, code style and change paths. Companion documents: [`architecture-diagrams.md`](architecture-diagrams.md) · [`CF_LIMITS.md`](CF_LIMITS.md) · [`POSTMAN_COLLECTION_README.md`](POSTMAN_COLLECTION_README.md) §7.
## Current architecture

The project uses a **dual-plane architecture**:

- **Go control plane** (`backend/`) owns code generation: it streams from Cloudflare AI Gateway (OpenAI-compatible SSE, see `backend/pkg/llm/client.go`), stores projects in Redis, and deploys generated apps to Cloudflare Pages.
- **Light Cloudflare Worker** (`worker/light-index.ts` → `worker/light/lightApp.ts`) serves the frontend SPA (ASSETS binding) and a minimal API surface: auth (email/password, GitHub OAuth) via D1, GitHub export, and health/status endpoints. It deliberately carries no Durable Object or container dependency, keeping startup time and CPU per request low (Workers Free allows 10 ms CPU per request). The old "3 MiB compressed script" limit was removed on 2026-09-04 — see `docs/CF_LIMITS.md` for the current platform limits. Components dropped by the migration are listed under [Historical content](#historical-content).
- Deployment config lives in `wrangler.v2.jsonc` (Worker `vibesdk-v2`, entry `worker/light-index.ts`).

Inside the Go control plane the generation stack is layered:

| Layer | Responsibility | Code |
|---|---|---|
| LLM client | AI Gateway / Workers AI streaming, SSE parsing, truncation detection | `backend/pkg/llm/` |
| Roles (skills) | One model + one prompt file per role (planner, coder, coordinator, reviewer) | `backend/pkg/skills/`, `backend/skills/` |
| Planner / coder contracts | `ExecutionPlan` JSON contract, validation, per-step coder calls | `backend/agent/` |
| Engine | One room per chat, the three generation paths, gap-fill, finalize | `backend/pkg/engine/` |
| Agents | eino/ADK ReAct engine, plan-execute-replan, multi-agent team | `backend/pkg/agent/`, `backend/pkg/engine/plan_execute.go`, `backend/pkg/engine/team.go` |
| Tools | VFS read/write/delete/list, REST calls, JSON-repair middleware | `backend/pkg/agent/tools/` |
| Retrieval | Redis vector index (`idx:vfs`) over the VFS, RAG context for the planner | `backend/pkg/engine/vector.go` |

The stable anchor `#current-architecture` is referenced by
[`architecture-diagrams.md`](architecture-diagrams.md) and
[`archive/architecture-legacy.md`](archive/architecture-legacy.md) — keep the heading text
`Current architecture` unchanged when editing this file.

## Contents

| Section | Anchor |
|---|---|
| Authoritative paths | [`#authoritative-paths`](#authoritative-paths) |
| Development commands | [`#development-commands`](#development-commands) |
| LLM inference | [`#llm-inference`](#llm-inference) |
| Roles and skills | [`#roles-and-skills`](#roles-and-skills) |
| Generation pipeline | [`#generation-pipeline`](#generation-pipeline) |
| Tools | [`#tools`](#tools) |
| Agents and teams | [`#agents-and-teams`](#agents-and-teams) |
| Retrieval and vector index | [`#retrieval-and-vector-index`](#retrieval-and-vector-index) |
| WebSocket protocol | [`#websocket-protocol`](#websocket-protocol) |
| Persistence | [`#persistence`](#persistence) |
| API surface by plane | [`#api-surface-by-plane`](#api-surface-by-plane) |
| Testing and verification | [`#testing-and-verification`](#testing-and-verification) |
| Historical content | [`#historical-content`](#historical-content) |

<a id="authoritative-paths"></a>

## Authoritative paths

| Area | Path |
|---|---|
| Light Worker entry | `worker/light-index.ts` |
| Light Worker routes (auth, GitHub export) | `worker/light/lightApp.ts` |
| Shared Worker base class | `worker/core/Worker.ts` |
| Frontend API types and client | `src/api-types.ts`, `src/lib/api-client.ts` |
| Shared protocol/type modules | `worker/types/` (`websocketTypes` at `worker/api/websocketTypes.ts`) |
| Go control plane API routes | `backend/pkg/api/routes.go` |
| Go engine (rooms, generation paths, gap-fill) | `backend/pkg/engine/` |
| Planner/coder contracts and models | `backend/agent/` |
| LLM client and stream parsing | `backend/pkg/llm/` |
| Skills (role system prompts + loader) | `backend/skills/`, `backend/pkg/skills/` |
| Agent tools | `backend/pkg/agent/tools/` |
| Stateless eino/ADK engine | `backend/pkg/agent/eino_engine.go` |
| Design brief catalog | `backend/pkg/design/` |
| Redis vector index / RAG | `backend/pkg/engine/vector.go` |
| Workflow DAG schema (v2) | `backend/pkg/engine/workflowschema.go` |
| D1 schema (light Worker) | `worker/database/schema.ts` |
| Platform limits | `docs/CF_LIMITS.md` |
| Architecture diagrams | `docs/architecture-diagrams.md` |
| Route ownership by plane | `docs/POSTMAN_COLLECTION_README.md` §7 |
| Setup and deployment | `scripts/setup.ts`, `wrangler.v2.jsonc` (`bun run deploy`) |
| Backend design notes | `backend/docs/` |

<a id="development-commands"></a>

## Development commands

Frontend and light Worker (run from the repository root with Bun):

```bash
bun install
bun run setup       # interactive Cloudflare/resource bootstrap -> .dev.vars
bun run dev         # Vite + @cloudflare/vite-plugin on http://localhost:5173
bun run dev:browser # optional local Chromium sidecar for the browser-console tool
bun run typecheck
bun run lint        # only src/** and worker/**
bun run test
bun run build       # vite build (frontend + Worker bundle); does not typecheck
bun run deploy      # needs .prod.vars
```

Go control plane:

```bash
cd backend
go vet ./...
go test ./...
go run ./cmd        # Fiber API on :8080 (PORT)
```

Hybrid local stack (Redis + Go backend + SPA):

```bash
redis-server
cd backend && go run ./cmd
# from the repo root:
VITE_CONTROL_PLANE_URL=http://localhost:8080 bun run dev
```

Full instructions, environment variables and sanity checks live in
[`LOCAL_DEV.md`](LOCAL_DEV.md). The control plane is **required for chat**: the edge Worker
answers `503 NOT_AVAILABLE` for `/api/agent*`, `/api/projects/*` and `/ws/*`.

<a id="llm-inference"></a>

## LLM inference

### Provider configuration

`llm.NewConfigFromEnv()` (`backend/pkg/llm/client.go`) resolves the provider at startup:

| Setting | Env var | Default / fallback |
|---|---|---|
| Provider base URL | `AI_GATEWAY_URL` | if empty and `CLOUDFLARE_ACCOUNT_ID` is set, `https://api.cloudflare.com/client/v4/accounts/<id>/ai/v1` (Workers AI, OpenAI-compatible) |
| Bearer token | `AI_GATEWAY_API_KEY` | falls back to `CLOUDFLARE_API_TOKEN` (Workers AI auth) |
| Default model | `DEFAULT_MODEL` | `@cf/meta/llama-3.3-70b-instruct-fp8-fast` |
| Request timeout | — | 5 minutes, bounds one streaming request |

`client.StreamChat(ctx, llm.ChatRequest{...})` POSTs to `<base>/chat/completions` with
`stream: true` and returns `<-chan llm.StreamChunk`. Both providers speak the same
OpenAI-compatible SSE dialect, so there is no provider-specific code path.

### Streaming contract

- `StreamChunk{Content, Done, FinishReason, Err}` is one SSE delta. `Done` is set on
  `data: [DONE]`; `FinishReason` carries the terminal `choices[0].finish_reason`
  (`stop`, `length`, …) — the value the truncation logic keys on.
- `emitChunk` decodes `choices[0].delta.content` as `json.RawMessage` and coerces it
  (`coerceDeltaContent`). Workers AI streams single numeric tokens as **bare JSON numbers**
  (`"content":8`); decoding those into a `string` field used to drop every digit of the generated
  code, so the raw value's literal text is used instead.
- Non-JSON payloads (gateway keep-alives) are ignored rather than treated as errors.
- A stream body that ends before `[DONE]` becomes `*ConnectionLostError`; use
  `llm.IsConnectionLost(err)` to tell a provider cut-off (retryable) from a real failure.

### Fence parsing and JavaScript repair

- `llm.StreamParser` splits the token stream into per-file events while the model is still
  writing: `EventStart` (path from the fence info string, or the nearest preceding `### path`
  heading), `EventChunk`, `EventEnd` (full content). `Flush()` salvages a truncated stream and
  `TruncatedPaths()` reports the files that were cut off.
- `llm.ParseFileBlocks(output)` is the non-streaming counterpart, used when a stream produced
  nothing at all.
- `llm.SanitizeJS(code)` repairs the known LLM JavaScript slips (empty values, truncated
  decimals, empty comparison/`||` operands) before a `.js` file reaches the VFS;
  `llm.LooksLikeJS(path)` gates it.

<a id="roles-and-skills"></a>

## Roles and skills

Every pipeline role maps to **one model + one markdown prompt file**
(`backend/pkg/skills/registry.go`, prompts in `backend/skills/`). The prompt file is injected
verbatim as the system message of every call made for that role.

| Role | Skill file | Default model | Temperature | Max tokens |
|---|---|---|---|---|
| `coordinator` | `00_coordinator.md` | `@cf/meta/llama-3.3-70b-instruct-fp8-fast` | 0.2 | 8192 |
| `planner` | `01_planner.md` | `@cf/meta/llama-3.3-70b-instruct-fp8-fast` | 0.2 | 8192 |
| `coder` | `02_coder.md` | `@cf/qwen/qwen2.5-coder-32b-instruct` | 0.1 | 8192 |
| `reviewer` | `03_reviewer.md` | `@cf/meta/llama-3.3-70b-instruct-fp8-fast` | 0.1 | 4096 |

- `SKILLS_DIR` overrides the prompt directory. Without it the registry probes `skills`,
  `../skills`, `../../skills` and `backend/skills` relative to the working directory (the
  container runs from `/app`, so the prompts live in `/app/skills`).
- Per-role env overrides: `<ROLE>_MODEL`, `<ROLE>_MAX_TOKENS`, `<ROLE>_TEMPERATURE` (for example
  `PLANNER_MODEL`, `CODER_MAX_TOKENS`). Invalid values are ignored, so a typo never hard-fails
  startup.
- `Registry.Load()` fails fast with the exact missing file, so a broken skills directory surfaces
  at startup instead of producing prompt-less model calls later.
- `backend/cmd/main.go` loads the prompts once and hands them to the hub
  (`hub.SetPlannerPrompt`, `hub.SetTeamPrompts`); every room created afterwards inherits them. A
  present planner prompt is what enables the dual-model pipeline, and the three team prompts are
  what enable the multi-agent team path.
- The planner prompt owns the design direction and the `ExecutionPlan` contract; the coder prompt
  is contracted to emit raw file content only (a wrapping fence is treated as drift and stripped).

<a id="generation-pipeline"></a>

## Generation pipeline

`ProjectRoom.StartGeneration` (`backend/pkg/engine/room.go`) remembers the prompt in Redis
(`lastprompt:<chatId>`, 24 h) and runs generation in a background goroutine that recovers from
panics, so one bad run cannot take the process down. `runGeneration` then tries three paths in
order; a failure that is neither a plan rejection nor a cancellation falls through to the next.

### Path 1 — dual-model: planner JSON → coder (`runDualModelPipeline`)

Runs whenever the planner skill loaded (`plannerPrompt != ""`).

1. **Design brief** — `design.Brief(ctx, prompt)` (`backend/pkg/design/`) assembles one
   brand/design direction (section pattern, style, palette tokens, typography, motion, a11y)
   shared by the planner and every coder step; a catalog failure logs and falls back to a neutral
   default instead of blocking generation.
2. **Planner context** — `plannerVFSContext` = compact VFS snapshot (`path (N bytes)` per file, or
   `(empty)`) plus the RAG block (`BuildRAGContext`, top 6 chunks), so iterate passes see real
   existing code rather than sizes only.
3. **Plan** — `agentplan.GeneratePlan` (`backend/agent/planner.go`) runs the planner model in JSON
   mode against the `ExecutionPlan` schema and returns a validated plan (`thought_process`,
   `subtasks`, `goal`, `steps[]` with `action` ∈ `create|modify|delete`).
4. **Broadcast** — `plan_structure` (machine-readable subtasks + steps), then the human-readable
   `plan_proposed`.
5. **Approval gate (B7)** — `waitForPlanApproval` pauses for the client: approve proceeds, reject
   returns `errPlanRejected` (final — no fallback generation), cancel ends the run, timeout
   auto-approves.
6. **Execution** — the multi-agent team when `canRunTeam` is true (team prompts present + a
   tool-calling model), otherwise the sequential per-step coder loop:
   - per step, `executeStepOnRoom` reads the current file from the VFS, streams the coder model
     (`agentplan.ExecuteStep*`) as `file_chunk_generated`, then persists and broadcasts
     `file_generated`; `delete` steps only remove the file (`file_deleted`);
   - `StepContext` gives every coder call the original request, the plan goal/subtasks/siblings and
     the design brief — a step description alone says nothing about the product;
   - `siblingFileContents` hands the coder the real contents of already-written files, so selectors,
     ids and class names actually match.
7. **Recovery + finalize** — truncated steps are retried with a doubled token budget and then
   dropped for gap-fill; `fillMissingReferencedFiles` closes referenced-but-missing assets;
   `finalizeGeneration` publishes state.

### Path 2 — plan-execute-replan (`runPlanExecute`)

Fallback when the dual-model pipeline is unavailable. `proposePlan` produces a plan — structured
JSON via the planner skill with a single contract-repair round trip when it violates
`ExecutionPlan`, otherwise the built-in prose architect prompt — shows it, waits at the same
approval gate, then runs eino's `planexecute` prebuilt
(`backend/pkg/engine/plan_execute.go`): planner → executor → replanner with `MaxIterations: 4`.
The executor's tools are `vfs_write` (wrapped so writes replay as `file_generating` /
`file_generated`), `vfs_read` and `vfs_list`. A "successful" run that wrote no files is treated as
an error so the caller falls back to path 3, and gap-fill runs before finalize.

### Path 3 — fence-streaming fallback

The original single-shot path: `generationSystemPrompt` (`backend/pkg/engine/prompts.go`) asks for
fenced code blocks whose info string is the file path, ordered so the JavaScript logic files land
before HTML/CSS. `streamAndApplyFiles` feeds the `StreamParser`, applying each file event as it
completes, and retries up to `maxRetries = 2` while `finish_reason == "length"` and files remain
unfinished. The system prompt also requires a `workflow.json` DAG (schema v2) that the frontend
renders as an interactive ReactFlow graph.

### Recovery, gap-fill and finalize

- **Truncation:** a coder pass that hits `finish_reason=length` is re-run with a doubled budget
  (`maxStepTruncateRetries = 2`); a still-partial asset is deleted rather than kept, because a
  truncated file is worse than a missing one — gap-fill regenerates it.
- **Gap-fill:** `fillMissingReferencedFiles` scans the VFS for referenced-but-missing assets (for
  example a `main.js` a cut-off stream never opened), generates them, and stubs the rest so the
  preview stays alive.
- **Finalize:** `finalizeGeneration` is the single funnel for all three paths. It parses the full
  output when nothing streamed, validates and persists `workflow.json` (`persistWorkflowDag` →
  Redis `workflow:dag:<chatId>` + D1 `workflow_dags`; an invalid DAG is recorded, never fatal),
  reindexes the VFS into `idx:vfs`, then broadcasts `cf_agent_state` and `generation_complete`.
- **Cancellation:** `generationContext(5 * time.Minute)` plus room stop close the context; streams
  end, `generation_cancelled` is emitted, and a cancellation is never reported as a failure.

<a id="tools"></a>

## Tools

### VFS tools (`backend/pkg/agent/tools/vfs_tool.go`)

`vfs_write`, `vfs_read`, `vfs_delete`, `vfs_list`, plus `NewVFSTools` returning the full set for
the ReAct loop. Two invariants matter:

- **The model cannot choose the scope.** The chat id is attached to the tool-call context by
  `tools.WithVFSContext(ctx, chatID)` before dispatch; a call without it fails with
  `tools: vfs: no chat context attached to tool call`.
- **Writes are observable.** `notifyWriteTool` (`backend/pkg/engine/plan_execute.go`) wraps
  `vfs_write` so a successful write replays as the standard `file_generating` / `file_generated`
  WS events and runs `SanitizeJS` on `.js` content — tool-driven writes are indistinguishable from
  streamed ones for the frontend. `roomVFSStore` adapts the room's Redis-backed VFS to the
  `tools.VFSStore` interface.

### REST tool (`backend/pkg/agent/tools/rest_tool.go`)

`rest_api` executes operations described by an endpoint list, exposing one generic input
(`operation_id`, `path_params`, `query`, `headers`, `body`) so the tool stays dynamic when the
schema changes. In `backend/cmd/main.go` it is enabled by `AI_TOOLS_BASE_URL`, the endpoint list
comes from `REST_TOOL_ENDPOINTS` (`"METHOD:PATH:SUMMARY"` entries, comma-separated) and
`AI_TOOLS_API_KEY` becomes the default `Authorization: Bearer …` header. The `operation_id` the
model must pass is `<METHOD>_<path with "/" replaced by "_">`. Defaults: 30 s timeout per call,
16 KiB response cap fed back to the model, `application/json` for non-GET calls.

### Robustness middleware (`backend/pkg/agent/tools/repair.go`)

`NewJSONRepairTool` wraps every tool (see `agentTools` in `backend/cmd/main.go`). When a call
fails, `RepairJSON` makes one attempt to fix the classic LLM JSON mistakes — raw control
characters inside strings, trailing commas, unterminated strings, unbalanced braces from truncated
output — and retries. If the repaired argument string is unchanged, the original error is
surfaced.

<a id="agents-and-teams"></a>

## Agents and teams

### Stateless ReAct engine (`backend/pkg/agent/eino_engine.go`)

`agent.NewEngine` builds an eino-ext OpenAI chat model pointed at the same base URL as the LLM
client and wraps it with `newTolerantStreamModel`, which turns a single malformed stream frame
(for example Workers AI's numeric `content`) into a graceful stream end instead of a
`NodeRunError` through the ADK graph. `Engine` holds **no per-session state**: history is loaded
from and saved to Redis by the handler layer (`store.RedisCheckpointStore`). `ChatModel()` and
`SupportsPlanExecute()` expose the tool-calling interface the plan-execute and team paths need.
`backend/cmd/main.go` registers the stateless socket at `/ws-agent/:id` when the engine is
available.

### Multi-agent team (`backend/pkg/engine/team.go`)

Used only after a plan is approved, when the coordinator/coder/reviewer prompts are loaded and the
model supports tool calling. Three `adk.NewChatModelAgent`s are composed with `AgentAsTool`:

| Agent | Max iterations | Tools | Writes? |
|---|---|---|---|
| `coordinator` | 24 | `coder`, `reviewer` (as tools) | no |
| `coder` | 8 | `vfs_write` (notifying), `vfs_read`, `vfs_list` | yes |
| `reviewer` | 6 | `vfs_read`, `vfs_list` | **no** |

That split is the least-privilege boundary: the reviewer physically cannot modify the VFS. The run
broadcasts `team_started`, then `subagent_activity` per tool call, and finishes with
`team_completed` whose verdict is parsed from the reviewer summary (`APPROVE` → `approve`,
`REQUEST_CHANGES` → `request_changes`). Zero files written is an error — a team run can never be a
silent success.

<a id="retrieval-and-vector-index"></a>

## Retrieval and vector index

`backend/pkg/engine/vector.go` keeps a per-project code index in Redis so iterative requests plan
against the code that actually exists.

| Setting | Value |
|---|---|
| Index | `idx:vfs` (Redis Search) |
| Vector | 256 dimensions, `COSINE` distance, HNSW |
| Scope tag | `chat` — per-room filtering |
| Embedding | `HashEmbedding` (FNV-based, deterministic, local; no embedding API call) |
| Chunking | n-gram extraction over the file text |

- `EnsureVectorIndex` runs once at startup (`backend/cmd/main.go`) and drops/recreates an index
  left over from the older schema that had no `chat` tag — without that tag every project shared
  one namespace and the planner received other projects' files as "existing code".
- `IndexFile` / `ReindexVFS` sync the index; `finalizeGeneration` calls `ReindexVFS` after every
  generation so the next plan sees the new files.
- `SearchVFS` / `BuildRAGContext(ctx, prompt, k)` return the top chunks; the dual-model planner
  path injects them as "Relevant existing code context from the project VFS". A missing index or a
  search error degrades to no context, never to a failed generation.

<a id="websocket-protocol"></a>

## WebSocket protocol

The control plane serves room sockets at `GET /ws/:id` (`controlPlane.wsUrl`) and a stateless
agent socket at `/ws-agent/:id`. Message shapes live in `backend/pkg/models/websocket.go` and are
mirrored for the frontend through `worker/api/websocketTypes.ts` (the SDK re-exports that file, so
protocol changes must stay SDK-compatible); the frontend dispatch lives in
`src/routes/chat/utils/handle-websocket-message.ts`.

| Type | Meaning |
|---|---|
| `generation_started` | A run began |
| `conversation_response` | Assistant text (streamed or complete) with a conversation id |
| `plan_proposed` | Plan shown; the client approves/rejects at the gate |
| `plan_structure` | Machine-readable plan (goal, subtasks, ordered steps) |
| `file_generating` | A file block opened (real path known) |
| `file_chunk_generated` | Incremental file content |
| `file_generated` | Completed file (authoritative content) |
| `file_deleted` | A file was removed by a delete step |
| `cf_agent_state` | Full agent state snapshot (VFS map, last query, generating flag) |
| `team_started` / `subagent_activity` / `team_completed` | Multi-agent team lifecycle (verdict in `team_completed`) |
| `generation_complete` | Run finished |
| `generation_cancelled` | Run cancelled by the user or room stop |
| `generation_interrupted` | Partial state: some files may be missing or partial |
| `error` | Recoverable error surfaced to the UI |
| `deployment_started` / `deploy_progress` / `deployment_completed` / `deployment_failed` | Cloudflare Pages deploy lifecycle |

Handshake/state messages (`cf_agent_state`, `agent_connected`) are delivered to the connecting
client only, not broadcast to every tab. Client → server control messages carry the plan-gate
answer and the cancel/stop request.

<a id="persistence"></a>

## Persistence

| Store | Data | Where |
|---|---|---|
| Redis | Per-room VFS hash | `vfs:<chatId>` (mirrored in an in-memory map guarded by `vfsMu`) |
| Redis | Last generation prompt (24 h) | `lastprompt:<chatId>` |
| Redis | Latest validated workflow DAG envelope | `workflow:dag:<chatId>` |
| Redis | Agent conversation checkpoints | `store.RedisCheckpointStore` |
| Redis Search | Code index | `idx:vfs` |
| Redis | Generation VFS baseline snapshot (7-day TTL, written by `StartGenerationRecord`, read back when a run is finished by another process) | `vfs:snap:{genID}` (`backend/pkg/engine/generation.go:46`) |
| D1 (`v2-vibe`) | Workflow DAG rows for the runtime Worker | `workflow_dags` (schema in `worker/database/schema.ts`) |
| D1 (`v2-vibe`) | Generation lineage rows (`generations`, `generation_files`, `generation_audits`) — tables created by `migrations/0011_jittery_the_liberteens.sql`; written by the Go control plane recorder (`StartGenerationRecord` / `FinishGenerationRecord`, P1.2). The run hook itself lands with P1.3 | `worker/database/schema.ts`, `backend/pkg/engine/generation.go` |
| D1 (`v2-vibe`) | Auth: users, sessions, OAuth states, API keys, audit logs | light Worker (`worker/light/lightApp.ts`) |

`UpsertFile` / `DeleteFile` write the in-memory map and the Redis hash together (Redis is skipped
when no client is configured), so a room restart reloads its files from Redis. `REDIS_URL` defaults
to `localhost:6379`.

<a id="api-surface-by-plane"></a>

## API surface by plane

| Plane | Runtime | Owns |
|---|---|---|
| Edge / auth | light Worker (`worker/light-index.ts`) | SPA assets, `/api/auth/*`, GitHub OAuth + GitHub App export, `/api/status`, `/api/capabilities`, `/api/limits/usage`, read-only app lists. Unknown `/api/*` → JSON 404; chat/project/WS paths → JSON 503 `NOT_AVAILABLE`. |
| Control | Go Fiber (`backend/pkg/api/routes.go`) | `/health`, `POST /api/agent`, `POST /api/agent/session`, `GET /api/agent/:id/connect`, `/api/projects/:id/{files,deploy,github-export}`, `/api/workflows/*`, `GET /ws/:id`, `/ws-agent/:id` (stateless agent), `/api/apps*`. |
| Execution (optional) | Cloudflare Edge (`VITE_EXECUTION_PLANE_URL`) | Workers / Workflows / Vectorize execution when configured; dependent features stay disabled when the URL is empty. |

The frontend resolves plane base URLs in `src/config/api.ts` and sends HTTP through
`src/lib/api-client.ts` / `src/services/controlPlaneClient.ts`. Per-endpoint ownership, with
`routes.go` / `lightApp.ts` line references, is maintained in
[`POSTMAN_COLLECTION_README.md`](POSTMAN_COLLECTION_README.md) §7 — do not duplicate that table
here.

<a id="testing-and-verification"></a>

## Testing and verification

```bash
# Root (Bun): typecheck, lint, tests, build
bun run typecheck
bun run lint
bun run test
bun run build

# Go control plane
cd backend && go vet ./... && go test ./...

# SDK (independent Bun package)
bun run --cwd sdk test
```

- Focus a root test: `bunx vitest run path/to/file.test.ts` (Workers pool, `wrangler.test.jsonc`).
- The root suite runs through the Workers pool with `wrangler.test.jsonc` (`vitest.config.ts`) and excludes the SDK tests, which run with Bun; `bun run lint` only checks `src/**` and `worker/**` — always pair it with `bun run typecheck` and `bun run build`.
- Backend tests live next to the code (`*_test.go`). `backend/e2e` skips itself unless
  `VIBE_E2E=1`; SDK integration tests need a running dev server plus `VIBESDK_INTEGRATION_API_KEY`.
- Platform limits that specs depend on are quoted with sources in [`CF_LIMITS.md`](CF_LIMITS.md).
- The workerd-based suites cannot start on macOS < 13.5 — run them in CI (`scripts/run-tests.mjs`).

<a id="historical-content"></a>

## Historical content (removed)

The following were removed with the dual-plane migration. They are **not** part of the current
system and must not be documented as live:

- ThinkAgent, SpaceDO, Cloudflare Artifacts, Worker Loader, App Facet — the `space/` package was deleted and nothing imports it.
- CodeGen Durable Object, the Cloudflare Agents SDK state machine and phase-based generation (blueprint → phase → review → fix).
- Hono route tree `worker/api/routes/`, `worker/agents/**`, `worker/database/services/`, `worker/services/sandbox/` — these paths do not exist in this repo.
- Cloudflare Sandbox SDK, containers, dispatch namespaces and sandbox preview containers.
- `/api/agent*`, `/api/projects/*` and `/ws/*` on the Edge Worker: they answer `503 NOT_AVAILABLE`; the Go control plane owns them.

The full pre-migration guide (5 000+ lines describing those components) is archived verbatim in
[`archive/llm-legacy.md`](archive/llm-legacy.md) — every top-level section there carries a `legacy`
status banner. The old diagrams live in
[`archive/architecture-legacy.md`](archive/architecture-legacy.md).

## Related documents

- [`AGENTS.md`](../AGENTS.md) — commands, code style, change paths, boundaries.
- [`architecture-diagrams.md`](architecture-diagrams.md) — current dual-plane diagram and bindings.
- [`CF_LIMITS.md`](CF_LIMITS.md) — platform limits (script size, cron, Workflows, CPU).
- [`POSTMAN_COLLECTION_README.md`](POSTMAN_COLLECTION_README.md) — API collection and route ownership.
- [`LOCAL_DEV.md`](LOCAL_DEV.md) — running the hybrid stack locally.
- [`usage-limits-ui.md`](usage-limits-ui.md) — usage-limit / deploy-gate UI invariants.
- [`DOCS_AUDIT_BACKLOG.md`](DOCS_AUDIT_BACKLOG.md) — docs tasks (this file was split by T7).
- `backend/docs/architecture-roadmap.json`, `backend/docs/EINO_ALIGNMENT_PLAN.md`,
  `backend/docs/OUTPUT_QUALITY_BUGS.md` — backend design and bug notes.

---

_Last reviewed: 2026-09-25 (docs audit task T7 — active/legacy split)._

