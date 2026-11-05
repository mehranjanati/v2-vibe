# ADR: Identity source of truth (P0.9)

> **Status:** `decided` 2026-10-07 — contracts the split, does not unify. Amended 2026-11-05: the PostgreSQL auth handlers were removed and the Go register/login stubs now use D1.
> Owner: control-plane auth boundary (`backend/pkg/api/auth.go`).
> Task: P0.9 in `docs/DEV_CHECKLIST.md` (acceptance: recorded decision + docs updated, no invented unification).

## Context

Three identity paths exist today and no Go handler reads the Edge cookie:

- **Edge light Worker mints sessions.** Register (`worker/light/lightApp.ts:198`), login (`worker/light/lightApp.ts:258`) and GitHub OAuth (`worker/light/lightApp.ts:403`) write `session:token:<token>` → userId (`worker/light/lightApp.ts:229`, `worker/light/lightApp.ts:295`, `worker/light/lightApp.ts:471`) with a 7-day TTL, set the `access_token` HttpOnly cookie (`worker/light/lightApp.ts:18`), and return `accessToken` in the response body (`worker/light/lightApp.ts:310`). Reads resolve the same KV key: `getUserId` (`worker/light/lightApp.ts:482`, `worker/light/lightApp.ts:487`), profile (`worker/light/lightApp.ts:339`), session probe (`worker/light/lightApp.ts:393`), logout (`worker/light/lightApp.ts:322`).
- **Go auth is D1-backed (Postgres removed 2026-11-05).** The former PG handlers were deleted; `POST /api/auth/register` / `POST /api/auth/login` now call `handleRegisterD1`/`handleLoginD1` (`backend/pkg/api/auth_d1.go:96`, `backend/pkg/api/auth_d1.go:152`, registered at `backend/pkg/api/routes.go:80`, `backend/pkg/api/routes.go:81`). They read/write the D1 `users` table and still return a bare random `sessionId` — persisted nowhere, written to no KV namespace, set in no cookie. A Go-login caller holds no Edge token and cannot satisfy the P0.3 boundary.
- **Dead Go D1 handlers.** `handleRegisterD1`/`handleLoginD1` (`backend/pkg/api/auth_d1.go:96`, `backend/pkg/api/auth_d1.go:152`) are unregistered in `backend/pkg/api/routes.go` — never wired, never called.
- **Provisional boundary (P0.3).** `sessionGuard` (`backend/pkg/api/routes.go:301`, `backend/pkg/api/routes.go:338`) runs `RequireSession` (`backend/pkg/api/auth.go:86`) on the seven mutating/project routes (`backend/pkg/api/routes.go:305` … `backend/pkg/api/routes.go:325`); `EdgeKVVerifier` (`backend/pkg/api/auth.go:190`) resolves tokens in `VerifyUserID` (`backend/pkg/api/auth.go:203`) by reading `session:token:<token>` through `KVClient` (`backend/pkg/cloudflare/kv.go:23`). Enforcement is opt-in (`backend/cmd/main.go:108`) and fails closed (503) without a store. Verifier contract: `backend/pkg/engine/hub.go:31`, installed via `backend/pkg/engine/hub.go:154`.

## Decision

**Contract the split — the Edge light Worker is the sole identity authority; the Go control plane only verifies Edge tokens.** No unification is invented:

1. Mint/read/revoke lives only on Edge (KV `session:token:*` + D1 `users`).
2. Go authenticates via `EdgeKVVerifier`; the `RequireSession` identity feeds P0.4/P0.7 ownership scoping.
3. The SPA presents the Edge `accessToken` as `X-Session-Token` / `Authorization: Bearer` (`src/lib/control-plane-session.ts:28`). Known wart (not fixed here): Go's cookie name (`backend/pkg/api/auth.go:53`, `"session"`) does not match Edge's (`worker/light/lightApp.ts:18`, `'access_token'`), so the cookie transport cannot work cross-origin as named — the header is the supported path; alignment is a P0.3-rollout follow-up.
4. Go register/login stay registered as legacy compatibility stubs backed by D1 — **not** a second source of truth; new logins go through Edge. The former PG handlers were removed on 2026-11-05 and `handleRegisterD1`/`handleLoginD1` (`backend/pkg/api/auth_d1.go:96`, `backend/pkg/api/auth_d1.go:152`) are wired at `backend/pkg/api/routes.go:80`, `backend/pkg/api/routes.go:81`.

## Alternatives rejected

- **Unify on Go PG.** Rejected: Edge must authenticate at the edge without a Postgres round-trip; D1 `users` + KV TTL sessions already serve login/profile/OAuth/logout. Would add Edge→PG plumbing for zero gain.
- **Unify on one shared user DB.** Rejected: no shared store exists; new infrastructure contradicts the lightest-task constraint and is implementation, not a decision.
- **Leave undecided.** Rejected: P0.4/P0.5 mechanism choice explicitly waits on this (P0.9 unblock condition); the provisional P0.3 checks already assume Edge authority.

## Consequences

- P0.4 (wired 2026-10-07) scopes rooms/VFS/deploys by the `RequireSession` identity: `ownerGuard` (`backend/pkg/api/routes.go:355`) installs `RequireResourceOwner` (`backend/pkg/api/auth.go:144`) after `sessionGuard` on the project/agent routes, and `handleCreateSession` claims each new room for its caller (`backend/pkg/api/routes.go:499`). P0.7 (wired 2026-10-08) applies the same identity to workflows: `workflowGuard` (`backend/pkg/api/routes.go:389`) scopes both workflow routes by `workflow_dags.user_id` and audits every cross-tenant denial. P0.5 (wired 2026-10-07) gates `GET /ws/:id` (`backend/pkg/api/routes.go:331`) with the same verifier: `wsGuard` (`backend/pkg/api/routes.go:465`) runs `RequireWebSocketSession` (`backend/pkg/api/auth.go:278`), reading the session token from the `ticket` query parameter the frontend carries on the upgrade URL.
- P0.3 rollout needs `CONTROL_PLANE_REQUIRE_SESSION` plus `VIBECODER_STORE_KV_ID` / `CLOUDFLARE_ACCOUNT_ID` / `CLOUDFLARE_API_TOKEN`; a deployment that logs in through the Go PG handlers cannot satisfy the boundary until it logs in via Edge (documented limitation, not fixed here).
- Follow-ups (not this task): deprecate/remove the Go auth stubs, align or drop the cookie transport cross-origin. Postgres removal landed 2026-11-05: the PostgreSQL auth handler file was deleted and the D1 handlers were wired in its place.
