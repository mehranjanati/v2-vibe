# Local Development Guide — VibeSDK Hybrid Architecture

> Strategy: [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md). Current reality:
> [`DEV_STATUS.md`](DEV_STATUS.md). Ordered work:
> [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md).

This guide explains how to run the decoupled hybrid stack locally:

- **Control Plane** (Go Fiber + Redis): sessions, VFS, LLM streaming, vector RAG, and Cloudflare Pages deployments.
- **Execution Plane** (Cloudflare Pages): the React SPA frontend that talks to the control plane.

The React frontend sends session creation, WebSocket streaming, and deployment to the Go control plane. The control plane is **required for chat**: the light edge Worker deliberately answers `503 NOT_AVAILABLE` for `/api/agent*`, `/api/projects/*` and `/ws/*` (see `worker/light/lightApp.ts`), so there is no working chat replacement at the edge.

---

## Prerequisites

- Go 1.25.5+
- Redis (with the Redisearch module, for the `idx:vfs` vector index)
- Bun (the repo uses Bun; `npm`/`pnpm` work too)
- A Cloudflare AI Gateway URL + API key (for LLM generation)
- Cloudflare Account ID + API token (D1 persistence + Pages deploys) — fill the token with `bun run d1:token`; optional for local UI testing

---

## 1. Run the Backend (Control Plane)

### 1.1 Start Redis

```bash
redis-server
```

### 1.2 Configure environment

```bash
cd backend
cp .env.example .env
```

Edit `backend/.env`:

```dotenv
# Redis
REDIS_URL=localhost:6379

# HTTP server
PORT=8080

# LLM / AI Gateway (OpenAI-compatible SSE endpoint)
AI_GATEWAY_URL=https://gateway.ai.cloudflare.com/v1/<account>/<gateway>
# Use a STATIC Cloudflare API Token with "Workers AI" permissions
# (Account > API Tokens > Create Token). Never use the short-lived OAuth token
# from `wrangler login` — it expires and causes HTTP 401s from the gateway.
AI_GATEWAY_API_KEY=your-api-key

# Cloudflare Pages deployment (server-side only, never exposed to the bundle)
# CLOUDFLARE_ACCOUNT_ID / CLOUDFLARE_API_TOKEN / D1_DATABASE_ID now live in the
# REPO-ROOT .env (single source of truth). cmd/main.go loads `.env`, `../.env`
# and `backend/.env`, so both run modes pick them up. Fill the token with:
#   bun run d1:token
# CLOUDFLARE_PAGES_PROJECT=my-pages-project   # optional, defaults to vibesdk-<chatId>
```

### 1.3 Start the Go server

```bash
cd backend
go run ./cmd
```

Expected startup logs:

```
VibeSDK Go backend listening on :8080
[vector] created Redis vector index idx:vfs
```

Sanity checks:

```bash
curl http://localhost:8080/health
# → {"status":"ok","rooms":0}

# Create a session (returns a UUID room id + websocket URL)
curl -X POST http://localhost:8080/api/agent/session
# → {"websocketUrl":"ws://localhost:8080/ws/<uuid>","agentId":"<uuid>","behaviorType":"phasic","projectType":"app"}
```

---

## 2. Run the Frontend (React SPA)

From the repository root:

```bash
# Development server pointing the control plane at the Go backend
VITE_CONTROL_PLANE_URL=http://localhost:8080 bun run dev
```

Open `http://localhost:5173`.

If you also want to exercise the execution plane (Cloudflare Workers/Workflows/Vectorize), set:

```bash
VITE_EXECUTION_PLANE_URL=https://your-worker.workers.dev
```

> When `VITE_CONTROL_PLANE_URL` is unset, `controlPlane.baseUrl` falls back to the same origin (`src/config/api.ts`), so `/api/*` calls hit whatever serves the SPA. In `bun run dev` that is the Vite dev server, which has no `/api/*` backend — chat therefore only works when `VITE_CONTROL_PLANE_URL` points at the Go backend. `VITE_EXECUTION_PLANE_URL` below configures a separate, optional Edge plane (Workers/Workflows/Vectorize); it is not a chat replacement.

---

## 3. Verify the End-to-End Flow

### 3.1 Session creation goes to the control plane

1. Open the app and start a new chat (`/chat/new`).
2. Check the Go server logs:

```
[api] created agent session <uuid>
```

3. The browser DevTools Network tab should show a `POST /api/agent/session` to `http://localhost:8080`, and a WebSocket connection to `ws://localhost:8080/ws/<uuid>`.

### 3.2 LLM generation streams files

1. Type a prompt and submit.
2. The Go backend calls the AI Gateway and pushes tokens:
   - `file_chunk_generated` events stream into the chat (VFS state updates live).
3. On completion, `generation_complete` fires and files appear in the file explorer.

> Tip: if no files appear, the LLM must emit fenced code blocks whose info string is the file path (e.g. `` ```src/index.ts ``). Check the Go logs for parsing results.

**Client-side preview hydration:** when a chat is reopened, the SPA seeds the
preview from `GET /api/projects/:id/files` — a read-only VFS snapshot that
never spawns a room actor (falls back to the persisted Redis `vfs:{id}` hash
when no live room exists). Live WebSocket updates always win over this
snapshot. No preview traffic reaches the Go server afterwards: the static
preview is built and rendered entirely in the browser
(`src/components/preview/PreviewPanel.tsx` → sandboxed `<iframe srcdoc>`).

### 3.3 Deploy to Cloudflare Pages

1. Once files are generated, click **Deploy to Cloudflare**.
2. The frontend calls `POST /api/projects/:id/deploy` on the Go backend.
3. Watch the WebSocket events:
   - `deployment_started` → `deploy_progress` (0–100) → `deployment_completed { previewURL }`.
4. The deployment UI shows the live `.pages.dev` URL.

### 3.4 Chat is not served by the edge Worker

With the Go control plane stopped, the SPA still loads but chat cannot work. The retry path in `src/routes/chat/hooks/use-chat.ts` logs:

```
Control plane unavailable; falling back to legacy session creation
```

and then calls `apiClient.createAgentSession` (`POST /api/agent`), which targets `controlPlane.baseUrl` (`src/lib/api-client.ts:167-170` and `:347-351`) — i.e. the same Go backend that just failed. When `VITE_CONTROL_PLANE_URL` is unset the request goes to the same origin instead, where the light edge Worker answers `503 NOT_AVAILABLE` for `/api/agent*`. Treat this as a **compatibility hook, not a working fallback**: fix the control plane rather than expecting the edge to serve chat.

---

## 4. Production-Style Build

```bash
# Build the Go backend
cd backend && go build ./...

# Build the frontend SPA bundle (outputs dist/client/)
bun run build
```

The `dist/client/` folder is a static SPA ready for Cloudflare Pages (includes `_routes.json` + `_redirects` for client-side routing).

---

## Troubleshooting

| Symptom | Fix |
|---|---|
| `AI_GATEWAY_URL is not configured` | Set `AI_GATEWAY_URL` in `backend/.env` |
| Vector index warning at startup | Redis needs the Redisearch module (`FT.CREATE` supported) |
| No files after generation | Ensure the model emits fenced code blocks with file-path info strings |
| Deploy fails with auth error | Verify `CLOUDFLARE_API_TOKEN` has `Pages:Edit` permission and `CLOUDFLARE_ACCOUNT_ID` is correct |
| Chat logs `Control plane unavailable; falling back to legacy session creation` | Expected when the Go backend is down — the edge Worker cannot serve `/api/agent*`. Confirm `VITE_CONTROL_PLANE_URL=http://localhost:8080` is set and the Go server is running |