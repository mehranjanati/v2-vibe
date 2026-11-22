> **Ecosystem consolidation (2026-11-21):** this file is the tenancy/security
> product contract history (P0.3–P0.9 lineage). Canonical ecosystem version with
> evidence matrix, node trust levels, and marketplace supply-chain rules:
> `07-multitenancy-security.md` (this file, updated below) — see consolidated
> sections Status Matrix, Node Trust, Supply Chain toward the end of the file.


# Multi-tenancy & Security Model (PROPOSED)

> **Scope:** tenancy and security as first-class product concerns for the
> Agentic Platform and its future public launch. Layer
> context: [`04-system-architecture.md`](04-system-architecture.md).
> Enforcement point for capabilities:
> [`06-functions-and-tools.md`](06-functions-and-tools.md). Canonical
> strategy: [`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md).
> Live identity
> work today: engineering P0.3–P0.9 in
> [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md).
>
> **Status:** `proposed` — full multi-tenancy is not implemented; the live
> system has per-user auth (D1-backed) and an opt-in control-plane session
> boundary (P0.3). FOUNDATIONAL BLOCKER FOR PUBLIC LAUNCH. Last reviewed:
> 2026-11-15.

## Tenant model (required by design)

- **Strict tenant isolation** — no cross-tenant access is representable at
  any layer; every request, function call and query carries tenant context.
- **Tenant-aware authentication** — sessions/tokens are bound to a tenant.
- **Tenant-aware authorization** — roles and permissions are evaluated per
  tenant; capability checks include tenant policy
  ([`06-functions-and-tools.md`](06-functions-and-tools.md)).
- **Tenant-specific configuration** — settings, feature flags, approval
  policies.
- **Tenant-specific functions** — tenants may extend the function catalog
  without affecting others (M4 marketplace direction:
  [`08-marketplace.md`](08-marketplace.md)).
- **Tenant-specific workflows and agent behavior** — per-tenant config and
  prompt/policy inputs, not code forks. Generated workflows are tenant-scoped
  in D1: `workflow_dags.user_id` (owner, first-write-wins) and
  `workflow_instances.user_id` per run; the control plane authorizes
  trigger/get through `workflowGuard` (cross-tenant → 403 + `workflow.access_denied` audit, unknown/unowned → 404, outage → 503) and the runtime refuses tenant-mismatched runs — see `docs/DEV_CHECKLIST.md` P0.7.
- **Tenant-specific data model where justified** — the default is a shared
  schema with a mandatory tenant column; a separate data model requires an
  explicit justification (constraint: do NOT assume every tenant needs a
  completely custom database).

## Security model

| Concern | Rule |
|---|---|
| Capability authorization | Capability + tenant + role checked in the execution path — never only in prompts (unchanged platform principle). Prompt instructions are not authorization. |
| Agent identity | Every run carries tenant + user/agent identity; cross-tenant access denied by construction. |
| Credentials | Stored as references in a vault, never plaintext in capabilities, manifests, or agent context. |
| API keys | Scoped, revocable, tenant-bound; never shared across tenants. |
| Human approval | External side effects (email, message, payment) require approval or an explicit tenant policy — logged either way. Approval policies and gates are launch blockers. |
| Audit | Every capability call records tenant, user/agent identity, capability, approval state, result. The *target* audit trail must also be signable for third-party verification (W3.1 provenance, W3.3 proof-of-run) — gated to M4, not implemented, and never at the cost of moving business data off the platform data layer ([`11-aaas-web3-strategy.md`](11-aaas-web3-strategy.md)). |
| MCP security | MCP servers are untrusted external boundaries: registration, permission mapping, secret handling, lifecycle, timeouts, tenant isolation, trust policy. Discovery never grants execution — enforced by the P7.3 boundary (`backend/pkg/mcp`, spec: [`architecture/mcp-trust-slice.md`](architecture/mcp-trust-slice.md)). |
| Sandboxing | Untrusted code (community capabilities, generated agents, tenant apps) never executes with control-plane privilege; sandbox boundaries are launch blockers. |
| Rate limiting & quotas | Per tenant and per capability; abuse of one tenant cannot degrade others. |
| Data ownership | Business data lives in the platform's data layer; orchestration vendors (e.g. a candidate Dify deployment) never hold the source of truth. |
| Isolation of generated apps | Tenant applications run sandboxed and tenant-scoped (the live per-project VFS isolation is the seed). |
| Abuse prevention & retention | Abuse prevention, data retention/deletion policies, RBAC — all launch blockers (see [`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md)). |

## Relationship to live P0 work

The engineering backlog already sequences this foundation as **P0 —
Identity, Security, Tenancy** (`P0.3` control-plane session boundary, `P0.4`
rooms/VFS/deploy ownership, `P0.5` WebSocket authorization, `P0.6` credential
hardening, `P0.7` workflow tenant scope, `P0.8` room lifecycle/quotas and
`P0.9` the identity source-of-truth decision are all
`implemented_unverified` — the code and its unit tests exist, the end-to-end
rollout does not — [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md)). This document
defines the *product contract* those tasks must satisfy: tenancy is a launch
requirement for the Agent Builder (M0), not a later hardening pass.

## Public-launch security gates (blockers)

No public launch without: full tenant isolation tests green; per-call audit
with tenant/user/agent identity; approval gates on external side effects;
credential-vault references (no plaintext in manifests or logs); agent
identity on every run; MCP trust policy enforced; sandbox boundaries for
untrusted code; rate limits and quotas enforced; RBAC; retention/deletion
policies; abuse prevention. See
[`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md).

## WebSocket authorization audit — P0-SEC-003 (2026-11-22)

**Scope:** read-only review of `GET /ws/:id`, its guard, the upgrade handler, and
the room-registration path, plus the existing unit and e2e tests. No execution in
this environment (workerd and the e2e listener need CI), so this entry separates
**proven by code + committed tests** from **unproven**.

### Control matrix

| Control | Status | Evidence |
|---|---|---|
| Authentication required on upgrade | IMPLEMENTED (opt-in) | `wsGuard` → `RequireWebSocketSession`; 401 when missing/unknown |
| Fail-closed when the session store is down | IMPLEMENTED | 503 path; also observed on a deployed plane 2026-11-22 |
| Ticket transports | IMPLEMENTED | short-lived `ticket` query, `X-Session-Token`, `Authorization: Bearer`, `access_token` cookie (cookie requires a trusted Origin) |
| **Per-room authorization (owner only)** | **IMPLEMENTED (opt-in, 2026-11-22)** | `ownerGuardFor` now runs on the `/ws/:id` upgrade; see W1 |
| Origin validation on the handshake | **IMPLEMENTED (2026-11-22)** | `wsOriginGuard` before the upgrade; see W2 |
| Short-lived, single-use ticket | **IMPLEMENTED (2026-11-22)** | `POST /api/ws-ticket` + `engine.WSTicketTTL`; see W3 |
| Cookie transport actually matches the Edge cookie | **IMPLEMENTED (2026-11-22)** | `access_token`, plus a CSRF Origin rule; see W4 |
| Boundary enforced by default | OFF in `render.yaml` | see W2 blast radius |

### W1 — authentication is not authorization (was High — FIXED 2026-11-22)

**Original defect.** `RequireWebSocketSession` resolved the caller and attached the
`Identity`, but `handleWebSocket` never read it: it took the `:id` param and called
`hub.GetOrCreateRoom(chatID)`, which by its own doc comment "never refuses and
never claims". Ownership records already existed (`ClaimRoomOwner`/`RoomOwner`,
P0.4) and were enforced on the HTTP routes through `ownerGuard` — the socket path
simply never consulted them. Anyone holding a valid session of their own could
attach to any chat id, receive its `cf_agent_state`/`agent_connected` payloads,
write files into its VFS, cancel its generation, and create room actors for
arbitrary ids. Only the fact that chat ids are UUIDs stood in the way —
identifier secrecy is not an authorization control.

**Fix.** The upgrade now runs the same guard pair as the HTTP project routes,
in this order:

```text
GET /ws/:id  ->  wsGuard(hub)            (P0.5  authentication: 401 / 503)
             ->  ownerGuardFor(hub,"id") (P0.4  authorization:   401 / 404 / 403 / 503)
             ->  websocket.New(handleWebSocket(hub))
```

`ownerGuardFor` reuses `RequireResourceOwner`, so the contract is identical to the
HTTP routes: no identity → 401, unowned/unknown room → 404 (existence never
leaked), owner ≠ caller → 403, ownership-store outage → 503, owner → upgrade. Both
guards are no-ops without a `SessionVerifier`, so a boundary-off deployment keeps
its previous behaviour.

**Evidence (executed 2026-11-22, Go only — no workerd needed):**

- `backend/pkg/api/ws_auth_test.go` — owner upgrade reaches the upgrader (426),
  cross-tenant ticket → **403**, unowned room → **404**, unknown room with a live
  store → 404, boundary off → 426 without any ownership.
- `backend/e2e/p05_ws_auth_test.go` — a real listener and a real dial: no ticket →
  401 with no socket, unknown ticket → 401, owner ticket → **101** upgrade plus
  the `agent_connected` handshake, boundary off → upgrade without a ticket.
- Two pre-existing tests (`AcceptsValidTicket`, `TokenTransports`) asserted the old
  behaviour on an unclaimed room; they now claim the room first and still test what
  they were written to test (that the *transports* are accepted).
- `cd backend && go vet ./... && go test ./...` green;
  `go test -tags e2e ./e2e/ -run 'P03|P05|P17'` green. The `TestCloudflare*`
  suite fails on this box because it dials the live Worker and the network is
  blocked — unrelated to this change.


### W2 — no Origin validation on the handshake (was High — FIXED 2026-11-22)

**Original defect.** `websocket.New(...)` was registered with no origin
configuration. CORS does not apply to WebSocket handshakes, so with
`CONTROL_PLANE_REQUIRE_SESSION` off any web page could open a socket and drive a
room, and with it on a leaked ticket was usable from any origin.

**Fix.** `wsOriginGuard` runs before the session guard and refuses a foreign
`Origin` with 403. It is middleware rather than an upgrader option because the
Fiber websocket config only supports exact `Origins` matching, while the shipped
allow-list contains a `https://*.` wildcard. Rules: no `Origin` → allowed (a
browser cannot omit it, CSWSH is browser-only, and the SDK/CLI/e2e dial send
none); exact match against the same `corsAllowedOrigins()` list CORS uses →
allowed; `https://*.host` → allowed for real sub-domains only (a suffix trick
like `https://host.evil.com` is refused).

### W3 — the session token travelled in the URL (was Medium — FIXED 2026-11-22)

**Original defect.** `buildWebSocketURL` appended the caller's 7-day Edge session
token as `?ticket=…`, and the guard accepted any session token there. Query
strings are recorded by reverse proxies and CDNs, so a long-lived bearer
credential ended up in logs; the `token` query alias widened that surface.

**Fix.** The credential is split by channel:

| Channel | Credential | Why |
|---|---|---|
| `?ticket=` query | short-lived, single-use ticket (`POST /api/ws-ticket`, 15s TTL) | URLs are logged, so only a narrow, expiring, already-spent-after-one-use credential may travel there |
| `Authorization` / `X-Session-Token` / cookie header | session token (unchanged) | headers are not logged in URLs |
| removed | `?token=` query alias | it accepted the session token in the URL |

A ticket is random (32 bytes), bound to one `(user, room)` pair, and deleted on
redemption (`GETDEL` in Redis for cross-instance single-use). Replaying it,
using it on another room, or presenting a session token in the URL answers 401.
`buildWebSocketURL` now mints a ticket instead of embedding the session token, and
the SPA fetches one per connect (`getWsTicket` in `src/services/controlPlaneClient.ts`)
— the same contract the SDK already called.


### W4 — the documented cookie transport did not exist in production (was Low — FIXED 2026-11-22)

**Original defect.** `sessionCookieName` was `session` and its comment claimed to
mirror the light Worker's `SESSION_COOKIE` — but the Worker's cookie is
`access_token`. The unit test therefore passed for a cookie the Edge never sets,
so the documented transport was dead and the comment was wrong.

**Fix.** The constant is now `access_token` (the real Edge cookie), the comment
states the actual caveat — the cookie is scoped to the Worker origin, so it only
works when both planes share one origin (proxied or self-hosted single-domain) —
and the old `session` name is pinned as dead by a test.

**CSRF rule added in the same change.** Making a cookie transport real means the
browser will attach the credential automatically, which is exactly the condition
CSRF needs. So a **cookie-authenticated** request must present a trusted `Origin`
(the same allow-list CORS and the WebSocket guard use) or none at all — which a
browser cannot do for a cross-site request. The header transports are unaffected,
because a cross-site page cannot set them. Applied in both `RequireSession` and
the WebSocket session guard.

**Evidence.** `TestSessionCookieNameMatchesEdge` (Edge name accepted, `session`
name dead) and `TestCookieAuthRequiresTrustedOrigin` (cookie + foreign Origin →
403; cookie + allowed Origin → passes; header transport not origin-gated) in
`backend/pkg/api/ws_auth_test.go`; the P0.3/P0.5 transport tests and the P03 e2e
now send the real cookie name. `go vet ./...`, `go test ./...` and
`go test -tags e2e ./e2e/ -run 'P03|P05|P17'` green.


### What is proven today, and what is not

- **Proven by code and executed tests:** 401 without a ticket, 401 for an unknown
  ticket, 503 when the session store is unavailable, owner-scoped upgrade (owner →
  426/101, cross-tenant → 403, unknown/unowned → 404), the four live token
  transports, short-lived single-use tickets (replay and cross-room → 401), the
  Origin allow-list (foreign → 403), cookie authentication only from a trusted
  Origin, and boundary-off no-op (`backend/pkg/api/ws_auth_test.go`); a real
  listener and a real dial fetching a ticket over HTTP in
  `backend/e2e/p05_ws_auth_test.go` (build tag `e2e`); and the 401 / 503
  behaviour reproduced on a deployed control plane on 2026-11-22.
- **Not proven:** nothing in this audit is outstanding — W1, W2, W3 and W4 are
  closed. The remaining caveats are environmental: no dedicated staging plane
  (D17: no staging plane exists) and the browser-level SPA behaviour, which needs a running stack.

### Remaining follow-ups (from this audit)

| Item | Task |
|---|---|
| — (none outstanding) | — |

W1 (per-room authorization), W2 (Origin allow-list), W3 (short-lived single-use
tickets) and W4 (Edge cookie name + cookie CSRF rule) are all closed — see the
sections above for the executed evidence.


### Exact commands to close the execution gap (when an environment exists)

```bash
cd backend && go vet ./... && go test ./...          # unit: ws_auth_test.go
cd backend && go test -tags e2e ./e2e/ -run P05 -v   # real listener + real dial
```

A per-room test must be added with the W1 fix: user B's ticket against user A's
room → 403; unknown room → 404; owner → 101.

## Cross-tenant isolation audit — P0-SEC-002 (2026-11-22)

**Scope:** every guarded control-plane resource family, verified with executed
tests rather than assumed from the presence of a guard.

### Matrix (boundary on, resource owned by user A)

| Family | Cross-tenant | Unknown | Anonymous | Owner | Test |
|---|---|---|---|---|---|
| `GET /api/projects/:id/files` | 403 | 404 | 401 | not denied | `isolation_test.go`, `auth_test.go` |
| `POST /api/projects/:id/deploy` | 403 | 404 | 401 | not denied | same |
| `POST /api/projects/:id/github-export` | 403 | 404 | 401 | not denied | same |
| `GET /api/agent/:id/connect` | 403 | 404 | 401 | not denied | same |
| `GET /api/generations/:chatId` | 403 | 404 | 401 | not denied | `isolation_test.go` |
| `GET /api/generations/:chatId/:genId/diff` | 403 | 404 | 401 | not denied | same |
| `POST /api/generations/:chatId/:genId/rollback` | 403 | 404 | 401 | not denied | same |
| `POST /api/workflows/trigger` | 403 + audit | 404 | 401 | 200 | `workflows_auth_test.go` |
| `GET /api/workflows/:workflowId` | 403 + audit | 404 | 401 | 200 | same |
| `GET /ws/:id` | 403 | 404 | 401 | upgrade | `ws_auth_test.go`, e2e P05 |
| Worker `VibeWorkflow` tenant stamp | run failed, no step executes | — | — | runs | `worker/workflow/VibeWorkflow.test.ts` (CI) |

### What this task changed

The isolation itself held: no cross-tenant read or write was found. What was
missing was **automated coverage for the History API** — the generations
list/diff/rollback routes had only a manual production run as evidence, and no
test would have caught a future regression there.

`backend/pkg/api/isolation_test.go` now pins the whole matrix in one table
(cross-tenant 403, unknown 404, anonymous 401, owner not denied) plus a dedicated
case proving a denied rollback mutates nothing and leaves the owner's history
reachable.

### The one deliberately unguarded route

`POST /api/agent` (legacy NDJSON) does not run the session guard. It mints a
fresh id and echoes it — it never touches a room or the VFS, so it cannot leak or
write another tenant's data. The property that matters is pinned by
`TestUnguardedLegacyAgentEndpointMintsNoTicket`: with the boundary on it must not
hand out a WebSocket credential, because it has no identity to bind one to (the
URL comes back without a ticket, and the upgrade would then answer 401).

### Not covered here

- Workflow isolation is verified in its own task (`P0-SEC-005`).
- The engine-level VFS is keyed by chat id; the authorization control is the API
  guard above, which is why that layer is where the tests live.
- Workers-pool suites (the `VibeWorkflow` tenant stamp) run in CI — macOS < 13.5
  cannot start workerd here.

## Workflow tenant isolation audit — P0-SEC-005 (2026-11-22)

**Scope:** the two halves of the workflow tenant boundary — the control plane's
authorization + run stamping, and the runtime's refusal to execute a mismatched
run.

### What was already covered

`backend/pkg/api/workflows_auth_test.go` pins the route behaviour: cross-tenant
trigger/get → 403 with a `workflow.access_denied` audit row naming the owner, an
unknown or unowned workflow → 404, an ownership-store outage or unconfigured D1 →
503, the P0.4 room-ownership fallback, boundary-off permissiveness, and — on the
happy path — a run row carrying the tenant. A denied trigger already recorded no
run row.

### The hole this task closed

Route authorization and the run stamp read from **different sources**. The guard
resolved the tenant as "D1 column first, then the P0.4 room claim", but the
trigger handler stamped `dag.UserID` straight from D1. On a legacy row
(`workflow_dags.user_id` NULL) authorized through the room-ownership fallback, the
run recorded a **NULL tenant** — and the runtime's mismatch check
(`dagTenant != "" && dagTenant != runTenant`) is skipped entirely when either side
is empty. So for exactly those runs the second layer of defence was vacuous.

Fix: `workflowGuard` now records the tenant it authorized in the request
(`workflowOwnerLocals`), and the trigger handler stamps **that** value, falling
back to `dag.UserID` only when the guard is not installed (boundary off). Pinned
by `TestWorkflowTriggerStampsAuthorizedTenantForLegacyDag`: a NULL-owner DAG
claimed by room owner A, triggered by A → 200 and the run row carries `user-a`;
B → 403 and no new row. `TestWorkflowTriggerDeniedInvokesNoRuntime` adds the other
half: a refused trigger starts **no** Cloudflare Workflows instance, so no
execution exists to leak or bill.

### Resulting invariant

With the boundary on, **every** run that reaches the runtime carries a non-empty
tenant: an unresolvable owner answers 404 before a run row is written, so a NULL
stamp can no longer happen on an authorized path. The runtime guard therefore has
something to compare in every case, and a mismatch fails the run with a
`NonRetryableError` **before any step executes** (the tenant check sits ahead of
`markRunning` and the step walk in `worker/workflow/VibeWorkflow.ts`).

### Evidence

- `go test ./pkg/api/ -run Workflow` → 14 cases green, including the two new ones.
- `go vet ./...`, `go test ./pkg/{api,engine,mcp}`, `go test ./agent`,
  `go test ./pkg/llm`, and `go test -tags e2e ./e2e/ -run 'P03|P05|P17'` green.
- The runtime-side guard itself is exercised by
  `worker/workflow/VibeWorkflow.test.ts` ("tenant isolation (P0.7)") in CI — the
  workerd pool cannot start on macOS < 13.5.
- Audit/denial rows: `TestRecordWorkflowAccessDenialWritesAuditRow` (engine) plus
  the route-level audit assertions.

## Credential vault audit — P0-SEC-004 (2026-11-22)

**Scope:** the two credential stores in the tree, what is live, and whether
revocation is effective. Executed evidence is Go/unit level; the Workers-pool
suites run in CI (macOS < 13.5 cannot start workerd).

### Two vaults, only one live

| Store | Status | Where |
|---|---|---|
| **GitHub OAuth token vault** — AES-256-GCM envelopes in D1 | **IMPLEMENTED (live)** | `worker/light/tokenVault.ts`, used by every export route |
| **Generic user-secrets store** (923 lines, WS/RPC surface, `vault-types`) | **DESIGNED — NOT WIRED** | `worker/services/secrets/`; `/api/vault/*` answers 503 `notAvailable()` and nothing imports the store |

The generic store is not a secret-leak risk today (it is unreachable), but it is
the reason `P1-AGT-003` (MCP transport) stays blocked: a real vault needs a route,
a persistence contract and tenant isolation before MCP grants can hold
credentials.

### Control matrix

| Control | Status | Evidence |
|---|---|---|
| No plaintext at rest (GitHub token) | IMPLEMENTED | `enc:v1:` envelope in D1; migration `0012_github_tokens_seal` purges legacy rows; lazy re-seal on read |
| Fail-closed resolve (tamper / wrong user) | IMPLEMENTED | AES-GCM AAD binds the ciphertext to `github_token:<userId>`; a row copied between users decrypts to `null` |
| Key derivation without a new binding | IMPLEMENTED | SHA-256 of a domain-separated `JWT_SECRET`; no extra binding to keep in parity |
| Plaintext never in manifests | IMPLEMENTED (schema-enforced) | `tool.schema.json` accepts only `^vault:` references; `docs:check` validates it |
| Plaintext never in logs | IMPLEMENTED (reviewed) | No control-plane log carries a token (`Identity.Token` is never printed); the vault logs only an error name/status |
| **Revocation is effective at the provider** | **IMPLEMENTED (2026-11-22)** | see below |
| Revocation is effective locally (authoritative) | IMPLEMENTED | `DELETE FROM github_tokens`; every export route gates on the row |

### What P0-SEC-004 changed

The provider-side revoke existed only inside the `/api/github-app/revoke` route
handler: untested, without a timeout, swallowing failures silently, and always
answering `revoked: true` even when GitHub never confirmed. That is a false claim
in a security response.

It now lives in the vault (`revokeGitHubToken`), so every caller gets one
contract:

- reads the token, calls `DELETE /applications/{client_id}/token` with Basic auth,
  **bounded by a 5s timeout**;
- treats `2xx`, `401` and `404` as revoked (GitHub no longer knows the token);
- logs a warning **without the token** when the provider refuses or the call
  fails;
- always performs the local delete, which stays authoritative;
- returns `{ localRevoked, providerRevoked }`, and the route answers
  `{ revoked, providerRevoked }` — the caller is no longer told a hard revoke
  happened when only the local row was removed.

Evidence: `worker/light/tokenVault.test.ts` (provider call shape incl. Basic auth
and body, 204/404 → providerRevoked, 500 → false, no OAuth app → call skipped,
network failure tolerated, local row deleted in every case, token absent from the
result and from every D1 bind) and the route test in `worker/light/lightApp.test.ts`.
Verified locally with `bun run typecheck`; the Workers-pool suites run in CI.

### Remaining gaps

| Gap | Task |
|---|---|
| Generic user-secrets store is not wired (no route, no persistence contract) | P1-AGT-003 blocker |
| Key rotation: rotating `JWT_SECRET` makes every stored token unreadable (users must re-authorise) — intentional, undocumented in ops | O24 |
| Per-provider vault coverage beyond GitHub | P2/P4 (MCP grants need it first) |

## Non-goals

- No per-tenant custom databases by default.
- No tenant-level code deployment (configuration and functions, not forks).
- No anonymous cross-tenant capability calls, including internal/admin paths.
