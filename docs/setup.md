# VibeSDK Setup Guide

> Strategy: [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md). Current reality:
> [`DEV_STATUS.md`](DEV_STATUS.md). Ordered work:
> [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md).

Set up VibeSDK for local development and production deployment.

**Make sure to read through the entire guide for important notes, and have all the required information ready before starting.**

> **قاعده تاریخ بازبینی مستندات:** تاریخ وضعیت یا بازبینی هر فایل بیانگر آخرین بررسی همان سند در برابر کد و پلتفرم است و فایل‌ها مستقل از هم تاریخ‌گذاری می‌شوند.
> **Status (2026-09-27):** the platform is a **dual-plane** system — a Go control plane in `backend/` (agent sessions, generation, VFS/Redis, Cloudflare Pages deploys) plus the light Worker `vibesdk-v2` (`worker/light-index.ts`). Generated-app previews render in the browser inside a sandboxed iframe, and **Deploy to Cloudflare** publishes the project VFS to Cloudflare Pages through the control plane. No preview runtime, Durable Object workspace, Artifacts namespace, sandbox container or persistent preview server is involved. See `README.md` (Architecture) and `docs/llm.md#current-architecture`.

## Prerequisites

Before getting started, make sure you have:

### Required

- **Bun** — the repository runs through Bun and `bun.lock` is the tracked lockfile.
- **Cloudflare account** with an API token (permissions table below).
- **Redis 7 with the RediSearch module** and **Go 1.25.5+** — the Go control plane in `backend/` owns chat, generation and deploys, and the app cannot chat without it (see `docs/LOCAL_DEV.md`).

### Recommended

- **Docker**, if you prefer `docker compose up -d redis backend` over starting Redis and Go by hand.
- **Custom domain** for the light Worker — a `*.workers.dev` URL works without one.

### API token permissions used by the v2 flows

| Flow | Permission |
|---|---|
| `bun run deploy` (light Worker `vibesdk-v2`) | Workers Scripts:Edit |
| KV binding `VibecoderStore` | Workers KV Storage:Edit |
| `bun run db:migrate:remote` (D1 `v2-vibe`) | D1:Edit |
| Generated-app deploys (`POST /api/projects/:id/deploy` → Cloudflare Pages) | Cloudflare Pages:Edit |
| `bun run cf-typegen` and wrangler bookkeeping | Account Settings:Read |
| Only when routing models through AI Gateway | AI Gateway:Read, AI Gateway:Edit, AI Gateway:Run |

> **No longer needed:** Workers for Platforms, Containers, Cloudchamber, Browser Rendering and R2
> permissions belonged to the retired sandbox/container preview path and the Artifacts-backed
> workspace; both were removed with the dual-plane migration (see the Architecture section of
> `README.md`).

## Quick start

The live deploy config is `wrangler.v2.jsonc` (Worker `vibesdk-v2`); this tree has **no**
`wrangler.jsonc`. Either run the interactive bootstrap (`bun run setup`, read-only first with
`bun run setup --check`) or configure both planes directly:

```bash
# 1. Dependencies + local variables
bun install
cp .dev.vars.example .dev.vars     # then fill in the keys listed under "Configuration values"

# 2. D1 schema for the light Worker (database `v2-vibe`)
bun run db:generate
bun run db:migrate:remote          # needs CLOUDFLARE_API_TOKEN with D1:Edit

# 3. Regenerate binding types
bun run cf-typegen

# 4. Redis + the Go control plane (chat, generation, deploys)
docker compose up -d redis backend # or: redis-server, then: cd backend && go run ./cmd

# 5. SPA + light Worker, pointed at the control plane
VITE_CONTROL_PLANE_URL=http://localhost:8080 bun run dev

# 6. Optional: deploy the light Worker
bun run deploy                     # reads .prod.vars and wrangler.v2.jsonc
```

Open `http://localhost:5173`.

> ℹ️ **`bun run setup` is dual-plane aware (docs-audit task T14).** It resolves `wrangler.v2.jsonc`
> (falling back to a legacy `wrangler.jsonc`), reuses the KV and D1 resources by the ids already
> declared in that config, verifies or creates what is missing, writes `.dev.vars` / `.prod.vars`, and
> only fills in the ids of resources it had to create — the committed routes, `vars` and
> `workers_dev` are never rewritten. V1-only steps (R2 templates, dispatch namespaces, the sandbox
> Dockerfile) are skipped automatically because the v2 config has no such bindings.
>
> **Check first, then run:** `bun run setup --check` prints the resolved config and the resources the
> run would manage, with no prompts, no Cloudflare calls and no writes (same as
> `VIBESDK_SETUP_CHECK=1`). The manual steps above remain the alternative when you want full control.
> The V1 prompt flow is archived in `docs/archive/setup-legacy.md`.

## Configuration values

Three places hold configuration; these are the values the live planes actually read.

### `.dev.vars` - light Worker (local)

| Key | Purpose |
|---|---|
| `JWT_SECRET` | Signs session tokens (`worker/light/lightApp.ts`) |
| `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_API_TOKEN` | Deploy and D1 access (`bun run deploy`, `bun run db:migrate:remote`) |
| `GITHUB_EXPORTER_CLIENT_ID`, `GITHUB_EXPORTER_CLIENT_SECRET` | GitHub login and GitHub export; unset means `/api/auth/providers` reports `github: false` and only email/password is offered |
| `CONTROL_PLANE_URL` | Optional control-plane base URL for the Worker (`VITE_CONTROL_PLANE_URL` is what the SPA reads) |
| `CUSTOM_DOMAIN` | Optional custom domain used for CORS/origin checks |

Run `bun run cf-typegen` after changing bindings so `worker-configuration.d.ts` matches `wrangler.v2.jsonc`.

### Repo-root `.env` - control plane and tooling

`CLOUDFLARE_ACCOUNT_ID`, `D1_DATABASE_ID` and `CLOUDFLARE_API_TOKEN` are the single source of truth for
the Go control plane and the D1 scripts. Fill the token with `bun run d1:token` (it pulls the fresh
OAuth token from wrangler, validates it with a read-only `SELECT 1`, and rewrites `.env`).
`backend/cmd/main.go` loads `.env`, `../.env` and `backend/.env`, so `go run ./cmd` and
`docker compose up -d backend` both pick it up.

### `backend/` - control plane variables

| Key | Purpose |
|---|---|
| `REDIS_URL` | Redis endpoint (default `localhost:6379`) |
| `AI_GATEWAY_URL` | OpenAI-compatible base URL; when empty with `CLOUDFLARE_ACCOUNT_ID` set, the client targets Workers AI |
| `AI_GATEWAY_API_KEY` | Bearer token for the gateway (falls back to `CLOUDFLARE_API_TOKEN`) |
| `DEFAULT_MODEL` | Fallback model when a role has no explicit model |
| `PORT` | API port (default `8080`) |

### Domain and network

No wildcard DNS, tunnel, Advanced Certificate Manager or dispatch-namespace setup is required:
previews render in the browser and deployed apps get their own `.pages.dev` URL. Mapping a custom
domain to the light Worker is optional and done on the Worker itself.

The V1 prompt flow (custom-domain prompts, R2 buckets, dispatch namespaces, provider-key prompts that
the removed `wrangler.jsonc` layout used) is archived in `docs/archive/setup-legacy.md`.

### AI provider configuration

Model choice is per role, not per app: each role (coordinator, planner, coder, reviewer) has one model
and one prompt file in `backend/skills/`, wired by `backend/pkg/skills/registry.go`. Point the control
plane at any OpenAI-compatible endpoint:

- **Cloudflare AI Gateway (recommended)** - set `AI_GATEWAY_URL` to
  `https://gateway.ai.cloudflare.com/v1/<account>/<gateway>/` plus `AI_GATEWAY_API_KEY` (a static token
  with Workers AI permission). Never use the short-lived `wrangler login` OAuth token: it expires and
  the gateway answers HTTP 401.
- **Workers AI directly** - leave `AI_GATEWAY_URL` empty with `CLOUDFLARE_ACCOUNT_ID` set; the client
  targets `https://api.cloudflare.com/client/v4/accounts/<id>/ai/v1`.
- **Another provider** - any OpenAI-compatible `chat/completions` streaming endpoint works; set
  `DEFAULT_MODEL` (and per-role models) to identifiers that endpoint accepts.

`CLOUDFLARE_AI_GATEWAY` in `wrangler.v2.jsonc` is the light Worker's declared gateway name; the Go
control plane does not read it.

### OAuth and login

The light Worker wires exactly two sign-in paths (`worker/light/lightApp.ts:368`):

- **Email/password** - always available, backed by D1 users and sessions.
- **GitHub** - available as soon as `GITHUB_EXPORTER_CLIENT_ID` / `GITHUB_EXPORTER_CLIENT_SECRET` are
  set; the same credentials power GitHub export (`POST /api/github-app/export`).

Google and Cloudflare ("Login with Cloudflare") sign-in are **not wired** in the dual-plane tree:
`/api/auth/providers` returns `google: false` and `cloudflare: false`, and neither plane reads
`GOOGLE_CLIENT_*`, `CLOUDFLARE_OAUTH_*` or `CF_OAUTH_ENCRYPTION_KEY`. The V1 instructions for that flow
(OAuth client scopes, redirect URLs, `ENABLE_CLOUDFLARE_LIMITS`) are archived in
`docs/archive/setup-legacy.md`, and the matching rows of the toggle table below are marked
"declared only".

### Generated-app preview requirements

Generated-app previews need **no Worker binding at all**: the SPA builds and renders them in the browser from the room VFS, and `GET /api/capabilities` reports `requiresSandbox: false`. The light Worker's real bindings are `ASSETS`, `DB` (D1), `VibecoderStore` (KV), `AI` and `WORKFLOWS` — see `wrangler.v2.jsonc` and `docs/architecture-diagrams.md`. Deploying a generated app requires the Go control plane plus `Pages:Edit` on `CLOUDFLARE_API_TOKEN`; Docker is only needed to run Redis and the control plane locally (`docker-compose.yml`).

### Dashboard-managed feature toggles

Feature settings are intentionally omitted from the committed wrangler `vars`. For deployed environments, set them in the Cloudflare dashboard so the committed config stays environment-neutral; for local development, set them in `.dev.vars`. Do not add these settings back to `wrangler.v2.jsonc`.

> **Read-status note (verified 2026-09-27):** in the current tree the live planes read only `DB`, `ASSETS`, `VibecoderStore`, `JWT_SECRET`, `GITHUB_EXPORTER_CLIENT_ID` / `GITHUB_EXPORTER_CLIENT_SECRET` and `CONTROL_PLANE_URL` (light Worker — `worker/light/lightApp.ts`) plus the control plane's own variables (`REDIS_URL`, `AI_GATEWAY_URL`, `AI_GATEWAY_API_KEY`, `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_API_TOKEN`). The flags in the table below are declared in `worker/types/env.d.ts` (some are still written into `.dev.vars` by `bun run setup`), but **none of them is read by a live plane yet**, so setting them currently changes nothing. Repeat with `grep -rn "<FLAG>" worker/ src/ backend/`. Treat the table as the intended contract for upcoming work, not as live behavior.

| Variable | Effect | Unset default | Notes |
| --- | --- | --- | --- |
| `ENABLE_ARTIFACTS` | **Legacy — not read by any plane** | — | The Artifacts-backed workspace it used to enable was removed with the dual-plane migration. The flag is not even declared in `worker/types/env.d.ts`, and `grep -rn ENABLE_ARTIFACTS worker/ src/ backend/` returns nothing. |
| `ENABLE_READ_REPLICAS` | Enables D1 read replicas | Off | Declared only (`worker/types/env.d.ts`); no reader in the live planes. |
| `ENABLE_EMAIL_AUTH` | Enables email/password authentication | On | Declared only — `GET /api/auth/providers` currently reports `email: true` unconditionally (`worker/light/lightApp.ts:368`), so setting `"false"` does not make the deployment OAuth-only yet. |
| `ENABLE_CLOUDFLARE_LIMITS` | Enables AI Gateway connect | Off | Declared only; no reader in the live planes. |
| `ENABLE_USER_ACCOUNT_DEPLOY` | Deploys Think apps to the user's Cloudflare account (**reserved — not read by any plane**) | Off | Setting `"true"` **currently has no effect**: both `GET /api/capabilities` implementations return `userAccountDeploy: false` and Think deploys always use the platform path (see `docs/usage-limits-ui.md`). |
| `ALLOWED_EMAIL` | Restricts sign-in to one email address | Off | Declared only; no reader in the live planes. |
| `ALLOCATION_STRATEGY` | Selects the legacy sandbox allocation strategy | Default strategy | Declared only (`worker/types/env.d.ts`); the sandbox allocation code path it belonged to is gone. |
| `USE_CLOUDFLARE_IMAGES` | Enables Cloudflare Images uploads | Off | Declared only; no reader in the live planes. |
| `USE_TUNNEL_FOR_PREVIEW` | Uses a tunnel for local previews | Off | Dev-only; set in `.dev.vars`, not the production dashboard. Declared only — local previews run in the browser. |

Existing deployments retain previously configured dashboard values when this configuration is deployed — but per the read-status note above, none of these flags currently changes behavior in the live planes.

## Manual resource setup

If you prefer to create or verify the Cloudflare resources by hand:

1. **`.dev.vars`** - `cp .dev.vars.example .dev.vars` and fill in the keys from "Configuration values"
   (at minimum `JWT_SECRET`; add `GITHUB_EXPORTER_*` for GitHub login).
2. **D1 database** - the light Worker expects the database `v2-vibe` (binding `DB`); its id is already in
   `wrangler.v2.jsonc`. Verify it, then migrate:

    ```bash
    bunx wrangler d1 list                     # confirm db `v2-vibe` (id matches wrangler.v2.jsonc)
    bun run db:generate && bun run db:migrate:remote
    ```

3. **KV namespace** - binding `VibecoderStore`, id also in `wrangler.v2.jsonc`. Recreate it only when you
   switch accounts, then paste the new id into the config:

    ```bash
    bunx wrangler kv namespace create VibecoderStore
    ```

4. **No R2 bucket and no dispatch namespace.** The R2 `TEMPLATES_BUCKET` binding was removed (the light
   Worker never reads it) and there is no Workers-for-Platforms dispatch path to configure.
5. **Repo-root `.env`** - `CLOUDFLARE_ACCOUNT_ID`, `D1_DATABASE_ID`, `CLOUDFLARE_API_TOKEN`
   (`bun run d1:token` refreshes and validates the token).

## Starting development

```bash
bun run db:migrate:local   # apply the D1 migrations to the local (miniflare) database
bun run dev                # SPA + light Worker on http://localhost:5173
```

Chat, generation and deploys live in the Go control plane, so start it as well (Redis plus
`go run ./cmd`, or `docker compose up -d redis backend`) and point the SPA at it with
`VITE_CONTROL_PLANE_URL=http://localhost:8080`; otherwise the light Worker answers
`503 NOT_AVAILABLE` for `/api/agent*`, `/api/projects/*` and `/ws/*`.

**Note**: without OAuth credentials you must register an account with email/password the first time.

**Note**: `bun run db:migrate:local` needs workerd, which refuses to start on macOS < 13.5 - run it on
Linux/CI, or use the read-only remote equivalent
(`bun --bun wrangler d1 execute v2-vibe --remote --config wrangler.v2.jsonc --command "SELECT 1"`).

## Troubleshooting

### Common Issues

**D1 "Unauthorized" or migration errors**:
- Your API token lacks `D1:Edit`
- `CLOUDFLARE_ACCOUNT_ID` / `D1_DATABASE_ID` in the repo-root `.env` belong to another account
- **Fix**: `bun run d1:token` (validates with a read-only `SELECT 1`), or paste a static
  `Account -> D1 -> Edit` token into `.env`

**Permission errors**: re-check the permission table in Prerequisites - Workers Scripts, Workers KV
Storage, D1, Cloudflare Pages and Account Settings:Read, plus AI Gateway only when you use a gateway.

**Domain not found**: only relevant when you map a custom domain to the light Worker - the domain must
live in the same Cloudflare account and the token needs zone access for it.

**No files after generation**: the model must emit fenced code blocks whose info string is the file path
(for example a fence opened with `src/index.ts`).

**`AI_GATEWAY_URL is not configured`**: set it in the control plane's environment, or leave it empty with
`CLOUDFLARE_ACCOUNT_ID` set to use Workers AI directly.

**Vector index warning at startup**: Redis needs the RediSearch module (`FT.CREATE`); `docker-compose.yml`
uses `redis/redis-stack-server`.

**Models not behaving as expected**: change the per-role model/prompt in `backend/skills/` (registry
`backend/pkg/skills/registry.go`) or set `DEFAULT_MODEL` in the control plane's environment. The V1
`worker/agents/inferutils/config.ts` file no longer exists.

**Preview issues**:
- Previews are built in the browser from the room VFS: confirm the control plane is reachable (`VITE_CONTROL_PLANE_URL`) and that the room socket (`/ws/:id`) connected.
- A blank or partial preview usually means a missing referenced asset — the control plane gap-fills referenced-but-missing files and re-sends them.
- Use `bun run dev:browser` when local browser-console inspection is needed.

**"Deploy to Cloudflare" button issues (chat interface)**:
- **The button does nothing locally**: The route is served by the Go control plane (`POST /api/projects/:id/deploy`) — start it and point the SPA at it with `VITE_CONTROL_PLANE_URL`; the light Worker answers `503 NOT_AVAILABLE` there.
- **Deploy fails with an authentication error**: Check `CLOUDFLARE_ACCOUNT_ID` and that `CLOUDFLARE_API_TOKEN` has `Pages:Edit` (repo-root `.env`).
- **Note**: This deploys generated apps from the chat interface to Cloudflare Pages; it is unrelated to the GitHub repository deploy button.

### Getting help

1. Work through the items above, then `docs/LOCAL_DEV.md` for the control-plane side.
2. Review the Cloudflare Workers, D1 and Pages documentation.
3. Re-verify the prerequisites and the API-token permission table.
4. On a clean checkout, `bun run typecheck`, `bun run test` and `bun run docs:check` should still pass — include the failing command in a GitHub issue.

## Production deployment

Deploy the two planes separately:

1. **Light Worker** - put production values in `.prod.vars` (same keys as `.dev.vars`) and run
   `bun run deploy`; it runs `wrangler deploy --config wrangler.v2.jsonc` for the Worker `vibesdk-v2`.
2. **Go control plane + Redis** - build and run the Go service (`cd backend && go build ./...`, or the
   image from `backend/Dockerfile`) next to a Redis instance with RediSearch, and build the SPA with
   `VITE_CONTROL_PLANE_URL` pointing at its public URL.
3. **D1 schema** - `bun run db:migrate:remote` against the production `v2-vibe` database.

`bun run deploy` neither typechecks nor applies migrations: run `bun run typecheck`, `bun run test` and
the migration command explicitly.

## Next steps

1. **Start developing** with `bun run dev` plus the control plane.
2. **Visit** `http://localhost:5173`.
3. **Generate** your first application.
4. **Deploy** a generated app to Cloudflare Pages from the chat interface, or the platform itself with
   `bun run deploy`.

## Files that matter after setup

```
vibesdk/
├── .dev.vars                  # Local Worker vars/secrets (git-ignored)
├── .prod.vars                 # Production Worker vars/secrets (git-ignored)
├── .env                       # Repo-root Cloudflare + D1 credentials (git-ignored)
├── wrangler.v2.jsonc          # Live deploy config (Worker `vibesdk-v2`, bindings, vars)
├── wrangler.test.jsonc        # Config used by the Workers-pool test suite
├── worker-configuration.d.ts  # Generated binding types (`bun run cf-typegen`)
├── backend/                   # Go control plane (Redis, VFS, generation, Pages deploys)
├── migrations/                # D1 migrations (database `v2-vibe`)
└── src/                       # React SPA (built to dist/client)
```

## Important Caveats & Known Issues

### **Legacy tunnel and container configuration**

`USE_TUNNEL_FOR_PREVIEW`, `SandboxDockerfile` and container instance settings belong to the retired sandbox preview path (the `SandboxDockerfile` itself no longer exists in this repo). Current generated-app previews render in the browser from the room VFS — see the status note at the top of this guide and the Architecture section of `README.md`. Do not troubleshoot the current preview path as a Docker or cloudflared tunnel.

### **"Deploy to Cloudflare" requirements (chat interface)**

The "Deploy to Cloudflare" button in the chat interface publishes a generated app to Cloudflare Pages through the Go control plane:

> **Note**: This refers to the deployment button inside the VibeSDK chat interface, not the GitHub repository deploy button.

**Requirements**:
1. **A running control plane** — `POST /api/projects/:id/deploy` is a control-plane route; locally that means Redis plus the Go service (`docker compose up -d redis backend`) with `VITE_CONTROL_PLANE_URL` pointing at it.
2. **Cloudflare credentials for Pages** — `CLOUDFLARE_ACCOUNT_ID` plus a `CLOUDFLARE_API_TOKEN` with `Pages:Edit` (repo-root `.env`).
3. **A generated project** — the room VFS must contain files; the deploy publishes that VFS as the app.

**Why**: each deploy creates its own Cloudflare Pages project and streams `deployment_started` → `deploy_progress` → `deployment_completed { previewURL }` back over the room socket. No dispatch namespace, wildcard custom domain or sandbox container is involved.

### **Generated-app preview and deploy troubleshooting**

Current previews render in the browser from the room VFS (`src/components/preview/PreviewPanel.tsx`), so verify that the control plane is reachable (`VITE_CONTROL_PLANE_URL`), that `GET /api/projects/:id/files` returns the project files, and that the room socket is connected. A **Deploy to Cloudflare** failure is a Cloudflare Pages problem on the control plane: check `CLOUDFLARE_ACCOUNT_ID`, a token with `Pages:Edit`, and the Go logs for the `deployment_failed` reason. If an issue persists, open a GitHub issue with the failing command and the deployment error.
