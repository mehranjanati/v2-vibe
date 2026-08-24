# Local Development Guide — VibeSDK Hybrid Architecture

This guide explains how to run the decoupled hybrid stack locally:

- **Control Plane** (Go Fiber + Redis): sessions, VFS, LLM streaming, vector RAG, and Cloudflare Pages deployments.
- **Execution Plane** (Cloudflare Pages): the React SPA frontend that talks to the control plane.

The React frontend first tries the Go control plane for session creation, WebSocket streaming, and deployment; it falls back to the legacy Cloudflare Worker only if the control plane is unreachable.

---

## Prerequisites

- Go 1.22+
- Redis (with the Redisearch module, for the `idx:vfs` vector index)
- Bun (the repo uses Bun; `npm`/`pnpm` work too)
- A Cloudflare AI Gateway URL + API key (for LLM generation)
- Cloudflare Account ID + API token (for Pages deploys) — optional for local UI testing

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
AI_GATEWAY_API_KEY=your-api-key

# Cloudflare Pages deployment (server-side only, never exposed to the bundle)
CLOUDFLARE_ACCOUNT_ID=your-account-id
CLOUDFLARE_API_TOKEN=your-api-token
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

> When `VITE_CONTROL_PLANE_URL` is unset, the frontend falls back to the same origin (Vite proxy / Cloudflare plugin) — i.e. the legacy Worker path.

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

### 3.3 Deploy to Cloudflare Pages

1. Once files are generated, click **Deploy to Cloudflare**.
2. The frontend calls `POST /api/projects/:id/deploy` on the Go backend.
3. Watch the WebSocket events:
   - `deployment_started` → `deploy_progress` (0–100) → `deployment_completed { previewURL }`.
4. The deployment UI shows the live `.pages.dev` URL.

### 3.4 Fallback check (optional)

With the Go backend stopped, the dev server still runs via the legacy Worker path (if a Worker dev server is available). Verify the app degrades gracefully and logs:

```
Control plane unavailable; falling back to legacy session creation
```

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
| Frontend falls back to legacy | Confirm `VITE_CONTROL_PLANE_URL=http://localhost:8080` is set and the Go server is running |