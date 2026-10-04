# AGENTS.md

## Tooling
- Use Bun from the repository root. The tracked lockfile is `bun.lock` and install/build hooks invoke Bun even when started through npm. (There is no `workspaces` field and no `space` package left; the root package is the only workspace entry.)
- `bun run setup` is the interactive bootstrap. It resolves `wrangler.v2.jsonc` (legacy `wrangler.jsonc` as fallback), reuses the KV/D1 resources by the ids declared in that config, writes `.dev.vars`/`.prod.vars`, and only writes ids for resources it had to create; `bun run setup --check` prints a read-only summary (no prompts, no Cloudflare calls, no writes). Never commit `.dev.vars*` or `.prod.vars`.
- `bun run dev` starts the React frontend and Worker together through `@cloudflare/vite-plugin` at `http://localhost:5173`. There is no separate Worker dev command.
- `bun run dev:browser` is an optional local Chromium sidecar for the think agent's browser-console tool; absence only produces a warning.

## Verification
- Root checks: `bun run typecheck`, `bun run lint`, `bun run test`, `bun run build`. On macOS < 13.5 the workerd-based suites cannot start — run them in CI (see `scripts/run-tests.mjs`).
- CI gates: `.github/workflows/ci.yml` runs lint/typecheck/test/build in job `ci` plus `go vet ./... && go test ./...` in job `go-test` (`backend/e2e` is behind the `e2e` build tag, so it needs no Redis/secrets). Both deploy workflows (`deploy-staging.yml`, `deploy-release-live.yml`) have their own `lint`, `typecheck`, `test-build` and `go-test` jobs and `deploy` runs `needs: [lint, typecheck, test-build, go-test]` — a red gate stops the deploy. Their `deploy` env block only keeps vars no step reads yet; Worker vars come from `--config wrangler.v2.jsonc`.
- Docs checks: `bun run docs:check` runs three validators and gates both logic and documented facts: `scripts/validate-spec-refs.mjs` (`file:line` references across the live docs tree; default scope `docs/**/*.md` minus `docs/archive/**` and `docs/DOCS_AUDIT_BACKLOG.md`, which quote historical refs), `scripts/validate-postman.mjs` (two-plane Postman contract **and** a cross-check of `scripts/postman-route-contract.json` against the live registrations in `backend/pkg/api/*.go` + `worker/light/lightApp.ts`), and `scripts/validate-docs-invariants.mjs` (`CF_LIMITS.md` `[Sn]` source registry / `Last verified` contract; binding parity across `wrangler.v2.jsonc`, `docs/architecture-diagrams.md` and `worker-configuration.d.ts`). All three read files only (no network); `node scripts/validate-spec-refs.mjs --paths <file…>` points the reference check at an explicit list. It runs in pre-commit (for `docs/`/validator changes) and as the `Docs checks` step of the `ci` workflow — when a route, a platform number or a binding changes, update the matching file (Postman §۷ + manifest / `CF_LIMITS.md` / `architecture-diagrams.md` + `bun run cf-typegen`) in the same change (task T16 in `docs/DOCS_AUDIT_BACKLOG.md`).
- `bun run build` is `vite build` (frontend + Worker bundle) and does not typecheck; run `bun run typecheck` separately.
- Focus a root test with `bunx vitest run path/to/file.test.ts`; test execution uses the Workers pool and `wrangler.test.jsonc`.
- The root Vitest suite excludes `sdk/test/**`, `worker/api/routes/**`, and `cf-git/**` (see `vitest.config.ts`). SDK tests use Bun: `bun run --cwd sdk test`.
- SDK integration tests require a running root dev server and `VIBESDK_INTEGRATION_API_KEY`; run `bun run --cwd sdk test:integration`. They can take 5-10 minutes; `VIBESDK_INTEGRATION_RUN_PREVIEW=1` enables the slower preview case.
- Root typecheck/lint do not validate `sdk`; run `bun run --cwd sdk package` after changing the SDK. There is no `space` package in this repo, so `bun run --cwd space ...` fails.
- ESLint checks only `src/**` and `worker/**` and deliberately ignores tests; do not treat `bun run lint` as repository-wide validation.
- Pre-commit typechecks staged TypeScript and runs related Vitest tests. When staged files touch `docs/` or `scripts/validate-*.mjs` it first runs `bun run docs:check` (fails the commit on a stale/missing `file:line` reference). `RUN_ALL_TESTS=1` selects its broader suite; `SKIP_TESTS=1` bypasses the hook.

## Frontend UI
- Tailwind CSS v4 via CSS-first setup in `src/index.css` (`@import 'tailwindcss'`, `@theme`, Kumo tokens); no `tailwind.config.*`.
- Prefer `@cloudflare/kumo` for new UI. List components with `bun kumo ls`; component docs via `bun kumo doc Button` (swap name as needed). Legacy shadcn/Radix under `src/components/ui/` still exists—do not add new primitives there when Kumo covers the case.
- Icons: `@phosphor-icons/react`. Dark mode is `data-mode="dark"` on the root (not a `class` strategy).
- Path aliases: `@/*` → `src/*`, `shared/*`, `worker/*` (see `tsconfig.app.json`).

## Frontend Data Fetching
- Use TanStack Query for frontend server state and network-call caching. `QueryClientProvider` is wired at the React root; configure shared defaults in `src/lib/query-client.ts`.
- Keep TanStack query keys centralized in `src/lib/query-keys.ts`. Use hierarchical keys so broad invalidation works, for example `queryKeys.apps.all` should invalidate app list/favorite variants.
- Frontend HTTP still goes through `src/lib/api-client.ts`; query functions should wrap existing `apiClient` methods rather than calling `fetch` directly from components.
- Include user/account identity in query keys when cached data is user-specific, or explicitly clear/remove those queries on logout/user switch. `enabled: !!user` prevents fetching but does not clear old cached data.
- Mutations that change cached server state must update cache with `queryClient.setQueryData` or invalidate the relevant `queryKeys` on success. Do not rely on a local `refetch()` in one component if sidebar or other shared UI consumes the same data.
- Prefer query hooks (`useQuery`, `useMutation`) over ad-hoc loading/error state in React contexts. Context remains appropriate for client-only UI state or providers required by libraries.

## Boundaries
- `src/` is the React app (`src/main.tsx`, routes in `src/routes.tsx`). API contracts live in `src/api-types.ts`; frontend HTTP calls belong in `src/lib/api-client.ts`.
- `worker/light-index.ts` is the Worker entrypoint (`LightWorker` extends the shared base in `worker/core/Worker.ts`) and the `VibeWorkflow` export surface. Hono routes are wired by `buildLightApp()` in `worker/light/lightApp.ts`, and the deploy config is `wrangler.v2.jsonc`. The legacy `worker/api/controllers/**` tree exists but is not imported by the current entry point, so it is not the live route surface.
- `space/` is **not part of the current architecture**: its source was deleted with the dual-plane migration (committed as part of P1.0), nothing imports it, and there is no `space/package.json` in this tree. Only leftover `dist/` + `node_modules/` remain in the working tree — tracked in `docs/DEV_STATUS.md` Known Documentation Drift; remove them in a cleanup, do not add code there.
- `sdk/` is an independent Bun package with its own lockfile, scripts, and tests. It imports the platform WebSocket protocol from `worker/api/websocketTypes.ts`, so protocol changes must remain SDK-compatible.
- Shared frontend/backend types belong in `shared/`; Worker-only types stay under `worker/`.
- Architecture overview: `docs/llm.md`. Product strategy (Outcome-First Agentic Software Platform): `docs/PRODUCT_THESIS.md`. Current-state dashboard: `docs/DEV_STATUS.md`. Ordered backlog: `docs/DEV_CHECKLIST.md`. Platform limits (script size, cron, Workflows): `docs/CF_LIMITS.md`. Production deploy: `bun run deploy` (needs `.prod.vars`).

## Change Paths
- API endpoint: pick the plane first. Control plane (Go — agent sessions, rooms/VFS, generation, deploys, workflow runs): add the handler under `backend/pkg/api/` and register it in `backend/pkg/api/routes.go`; persist via `backend/pkg/cloudflare/d1.go` or Redis. Edge/auth plane (light Worker — D1-backed auth, GitHub export): add the route inside `buildLightApp()` in `worker/light/lightApp.ts`, above the SPA fallback `app.all('*', ...)`; unknown `/api/*` paths must answer JSON 404/503, never the SPA HTML. In both cases: contracts in `src/api-types.ts`, HTTP calls in `src/lib/api-client.ts`, routed through `controlPlane.baseUrl` / `authPlane.baseUrl` from `src/config/api.ts`.
- There is no `worker/database/services/`, `worker/api/routes/`, `worker/app.ts`, or `worker/index.ts` in this repo; do not create files there.
- WebSocket message: update `worker/api/websocketTypes.ts`, backend streaming/broadcast in `backend/pkg/engine/` (`room.go`, `team.go`, `dual_model.go`), and frontend handling in `src/routes/chat/utils/handle-websocket-message.ts`; verify SDK tests because its protocol re-exports these types.
- LLM tool: add it under `backend/pkg/agent/tools/` and wire it into the tool sets built by `backend/pkg/engine/plan_execute.go` (`planExecuteTools`) or `backend/pkg/engine/team.go` (`teamTools`, per-role least privilege).
- There is no ThinkAgent/SpaceDO tool path in the current architecture (`worker/agents/**` does not exist); see `docs/llm.md`.
- D1 schema source is `worker/database/schema.ts`; generate with `bun run db:generate` (drizzle-kit) and apply with `bun run db:migrate:local` / `db:migrate:remote` — both pass `--config wrangler.v2.jsonc` with the DB name `v2-vibe` and run wrangler through `bun --bun`, so installing Node >= 22 is not required. Caveat: `--local` needs workerd, which refuses to start on macOS < 13.5; run local migrations on Linux/CI, or use the read-only remote equivalent (`bun --bun wrangler d1 execute v2-vibe --remote --config wrangler.v2.jsonc --json -y --command "..."`).
- D1 + Cloudflare credentials: the **repo-root `.env`** is the single source of truth (`CLOUDFLARE_ACCOUNT_ID`, `D1_DATABASE_ID`, `CLOUDFLARE_API_TOKEN`). Refresh the token with `bun run d1:token` (`scripts/sync-d1-token.ts`): it pulls the fresh OAuth token from `wrangler login`, validates it with a read-only D1 `SELECT 1`, and rewrites `.env` (it never clobbers a non-OAuth token unless you pass `--force`). The Go control plane loads `.env`, `../.env` and `backend/.env` (`backend/cmd/main.go`), so `go run ./cmd` from `backend/` and `docker compose up -d backend` both pick it up; paste a static `Account → D1 → Edit` token into `.env` instead for a durable setup.
- After changing Wrangler bindings, run `bun run cf-typegen`; `worker-configuration.d.ts` is consumed by setup and TypeScript configs.

## Constraints
- Do not introduce new `any` types even though ESLint currently permits existing ones; find or define a concrete type. Frontend API types should import from `@/api-types`.
- Worker code reads bindings from `env`; do not use Vite environment variables there.
- There is no blanket `/api/*` auth gate: the Go control plane and the light Worker each authenticate per route (`backend/pkg/api/routes.go` handlers; session/CSRF checks inside `buildLightApp()`). New routes must add their own explicit auth/session check in the plane that serves them.
- User secrets RPC methods return `null`/`boolean` on failure rather than throwing; preserve that contract when editing `worker/services/secrets/`.
- For usage-limit UI behavior and its cross-component invariants, read `docs/usage-limits-ui.md` before editing the badge, credits banner, or limit popups.
