# V2 Vibe API Postman Collection — Two-Plane Smoke Tests

> Active collection: `docs/v1dev-api-collection.postman_collection.json` (v2.0.0, **12 requests in 2 plane folders**) + environment `docs/v1dev-environment.postman_environment.json`.
> Sources of truth: `backend/pkg/api/routes.go` (Go control plane) and `worker/light/lightApp.ts` (light Worker). Last verified: **2026-09-24**.
> Legacy artifact (29 requests, single `{{baseUrl}}`): archived at `docs/archive/v1dev-api-collection.legacy.postman_collection.json` + `docs/archive/v1dev-environment.legacy.postman_environment.json` — **historical only, do not use for smoke tests**.

## 1. Topology: two planes, no shared base URL

There is **no valid single shared base URL**. Worker and Control/Go are distinct planes:

| Plane | Base variable | Serves | Session/auth |
|---|---|---|---|
| **Worker** (light Worker, Hono) | `{{workerUrl}}` — local `http://localhost:5173` (`bun run dev`); production: your `*.workers.dev` host | auth (`/api/auth/*`), CSRF, providers, OAuth GitHub, logout, `/api/apps/public`, `/api/status`, `/api/capabilities`, `/api/limits/usage`, GitHub export | `access_token` HttpOnly cookie + KV session (`session:token:*`), issued by the Worker itself |
| **Control/Go** (Fiber backend) | `{{controlUrl}}` — local `http://localhost:8080`; production: your Go backend host | `/health`, `/api/agent` (NDJSON), `/api/agent/session`, `/api/agent/:id/connect`, `/api/apps*`, `/api/user/apps`, `/api/projects/:id/*`, `/api/workflows/*`, `/ws/:id`, `/api/auth/*` stubs (PG-backed register/login, dev csrf-token stub) | Independent PG-backed session (`sessionId` in response body); **no shared session with the Worker** |

### R2 answer — cross-plane auth/session dependency

**Answer: the two planes do NOT share session state.** The Worker stores sessions in KV (`session:token:<token>` → userId, `worker/light/lightApp.ts:319-366`); Go register/login are backed by Postgres (`backend/pkg/api/auth_pg.go`) and return a bare `sessionId` with no cookie, and no Go handler reads the Worker cookie (grep of `backend/pkg/api/*.go` shows no `Cookie`/`Authorization` session check). The only cross-plane coupling is file flow: the Worker fetches generated files from Go (`GET /api/projects/:id/files`) for GitHub export — it does not forward user session.

**Required execution order for a smoke run:** (1) run the **Worker Plane** folder first (CSRF → register/login → profile → OAuth helper → logout) to prove Worker auth; (2) then run the **Control Plane** folder (health → `POST /api/agent` → session → connect → app details) against the Go backend. Control requests must NOT depend on the Worker cookie, and the collection prerequest fetches CSRF from `workerUrl` only.

## 2. Folders and plane markers

| Folder | Plane | Requests |
|---|---|---|
| Worker Plane | `worker` | Get CSRF Token, Register User, Login with Email, Get User Profile, OAuth - GitHub (Browser Only), OAuth Helper - Get GitHub URL, Logout (7) |
| Control Plane (Go) | `control` | Health Check (Control), Start Code Generation (NDJSON), Create Agent Session, Connect Agent, Get App Details (5) |

Every active request carries an explicit plane marker as a structured description prefix (`plane: worker` or `plane: control`) — folder names alone are not the contract. The validator cross-checks the marker against the URL base variable (`{{workerUrl}}` vs `{{controlUrl}}`).

## 3. NDJSON agent_id extraction rule (R1)

`POST /api/agent` (`backend/pkg/api/routes.go:116-134`) returns **NDJSON, not the `{success, data.agentId}` envelope**: exactly one JSON object per line with `Content-Type: application/x-ndjson`, currently a single line `{"agentId": "<uuid>", "websocketUrl": "ws://<host>/ws/<uuid>", ...}`.

The collection test script implements this rule:

1. `pm.response.text().split('\n')` into lines;
2. `JSON.parse` each non-empty line inside `try/catch` (non-JSON lines are logged and skipped);
3. take the **last valid event containing `agentId`** and save it to `agent_id` (plus `websocketUrl` → `websocket_url`);
4. fail loudly (`Agent ID present in NDJSON response`) with the raw body in the Postman console if no line yields `agentId`.

**Assumption:** the response is newline-delimited JSON with the result event carrying `agentId`. If Go ever emits multi-line progress events before the result, this rule still holds (last `agentId` wins); if the contract changes shape, update the script + this README together.

`POST /api/agent/session` returns plain JSON `{agentId, websocketUrl}`; its script reads both top-level and `data.*` shapes.

## 4. Import instructions (only known consumer is manual import)

1. Postman → **Environments** → Import → `docs/v1dev-environment.postman_environment.json` **first**.
2. Then **Collections** → Import → `docs/v1dev-api-collection.postman_collection.json`.
3. Select the `V2 Vibe Two-Plane Environment` environment (top-right).
4. Set values: `workerUrl` (local default `http://localhost:5173`; production: your Worker host) and `controlUrl` (local default `http://localhost:8080`; production: your Go host). Chain variables (`csrf_token`, `user_id`, `session_id`, `agent_id`, `websocket_url`, `app_id`) start empty and are auto-populated.
5. Run **Worker Plane** top-to-bottom, then **Control Plane** top-to-bottom. OAuth GitHub is browser-only: run the helper, copy the console URL into a browser.

> `baseUrl` and `localUrl` were **removed** from the active environment. If an old environment still defines them, delete it and re-import. Placeholders only — no production domain is invented here.

## 5. Validator + negative self-test

- Route contract manifest: `scripts/postman-route-contract.json` (live routes per plane + `dead_on_both_planes`, derived from Phase-1 source verification). Update it deliberately when routes change.
- Validator: `node scripts/validate-postman.mjs` — checks `workerUrl`/`controlUrl` exist, `baseUrl`/`localUrl` absent, every request has a plane marker consistent with its base variable, no dead routes, no unresolved variables, NDJSON parsing present for `POST /api/agent`, `app_id` chaining present, collection prerequest is Worker-aware, no duplicate/conflicting definitions. Exits non-zero on any violation.
- Negative self-test: `node scripts/validate-postman-negative.mjs` — runs the validator against `scripts/postman-negative.{collection,environment}.json` fixtures (one of each violation class) and asserts non-zero exit.

## 6. What was removed (and why)

Active set shrank **29 → 12**. Removed from active (preserved in `docs/archive/`): `GET /api/health` (live is `/health`), `GET /api/agent/:id` + `/preview` (live is `/api/agent/:id/connect`), `.../ws` (live is `GET /ws/:id`), star/fork, `PUT /api/user/profile`, user analytics, `/api/stats`, all `/api/model-configs*`, all `/api/secrets*`, Google OAuth + its helpers (GitHub OAuth kept, Worker-only). None are live on either plane per the sources of truth above.

Also out of scope (unchanged): `POST /api/ws-ticket` (referenced by `sdk/src/http.ts`, implemented nowhere) — not added, not worked around.

## 7. Ownership table (verified 2026-09-24؛ مسیر کامل `file:line`ها: ۲۰۲۶-۰۹-۲۵ — T8)

| Endpoint (active) | Plane | Source |
|---|---|---|
| `GET /api/auth/csrf-token` | Worker (+ Go dev stub) | `worker/light/lightApp.ts:188`, `backend/pkg/api/routes.go:61` |
| `POST /api/auth/register`, `POST /api/auth/login` | Worker (D1/KV); Go has parallel PG handlers | `worker/light/lightApp.ts:198,258`, `backend/pkg/api/auth_pg.go:98,158` |
| `GET /api/auth/profile`, `GET /api/auth/providers` | Worker (+ Go stubs) | `worker/light/lightApp.ts:336,368`, `backend/pkg/api/routes.go:67,70` |
| `POST /api/auth/logout` | Worker only | `worker/light/lightApp.ts:319` |
| `GET /api/auth/oauth/github` | Worker only | `worker/light/lightApp.ts:391` |
| `GET /health` | Control/Go (Worker answers `/api/status`, not `/health`) | `backend/pkg/api/routes.go:49` |
| `POST /api/agent` (NDJSON) | Control/Go (Worker: 503) | `backend/pkg/api/routes.go:116` |
| `POST /api/agent/session`, `GET /api/agent/:id/connect` | Control/Go (Worker: 503) | `backend/pkg/api/routes.go:296,299` |
| `GET /api/apps/:id` | Control/Go stub + Worker D1 list routes | `backend/pkg/api/routes.go:253`, `worker/light/lightApp.ts:797` |
