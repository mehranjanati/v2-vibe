## Usage Limits UI Rules

Defines when each limits-related UI element renders, based on `UsageSummary` from `GET /api/limits/usage`.

## Inputs

| Field | Meaning |
| --- | --- |
| `config.unlimited` | True when the user has no free-tier cap (Cloudflare-connected, or self-hosted with `ENABLE_CLOUDFLARE_LIMITS != 'true'`). |
| `config.limit.maxValue` | Free-tier cap for the current window. `Infinity` when unlimited (serializes to `null`). |
| `config.limit.window` | `daily` (calendar-aligned UTC) or `rolling`. |
| `config.limit.resetAt` | ISO timestamp when the free window resets. |
| `hasUserToken` | Server has a decrypted Cloudflare OAuth token for the user (HttpOnly cookie). |
| `hasCloudflareConfigured` | User has selected an account + AI Gateway. |
| `cloudflareCredits` | Fetched Cloudflare AI Gateway balance (nullable). |
| `limitCheck.withinLimits` | False when free-tier is exhausted. |
| `cloudflareConnectEnabled` | Feature flag; when false, connect/limits UI is hidden. |

Cloudflare OAuth tokens are server-side only (HttpOnly cookie). There is no browser-side token check.

Derived state is centralized in `useUsageLimitsBadgeState()` (`src/components/usage-limits-badge.tsx`) and reused by the sidebar CTA and auth button.

| Derived flag | Meaning |
| --- | --- |
| `hidden` | `cloudflareConnectEnabled` is false (after load). |
| `needsConfiguration` | `hasUserToken && !hasCloudflareConfigured`. |
| `showCredits` | `hasUserToken && hasCloudflareConfigured && cloudflareCredits` present. |
| `showUsage` | `config.limit` exists AND `!config.unlimited`. |
| `isExhausted` | `!limitCheck.withinLimits` (when free-tier usage is tracked). |

## Sidebar Connect CTA — `src/components/layout/app-sidebar.tsx`

Full-width brand button above the auth row in `SidebarFooter`. Collapsed sidebar shows glyph-only `size-9`.

| User state | CTA |
| --- | --- |
| Not authenticated / loading / `hidden` | Hidden |
| `!hasUserToken` | "Connect Cloudflare" → OAuth (`/oauth/login`) |
| `needsConfiguration` | "Configure AI Gateway" → `/settings` |
| Configured (`hasUserToken && hasCloudflareConfigured`) | Hidden — even if `cloudflareCredits` is null |

Do **not** gate this CTA on `!showCredits`. That incorrectly re-prompts OAuth when the user is configured but the credits payload failed to load.

## Auth button status + menu — `src/components/auth/auth-button.tsx`

Uses the same `useUsageLimitsBadgeState()` hook. Status line under the avatar (sidebar display); limits actions in the account dropdown.

### Status line (sidebar display)

| User state | Status text |
| --- | --- |
| Loading | "Checking credits..." |
| `needsConfiguration` | "Configure AI Gateway" (amber) |
| `showCredits` | `$X.XX credits` (or with gateway name) |
| Exhausted, no token | "Free limit exhausted" (red) |
| Free tier remaining | usage text (e.g. `N free credits left`) |
| Otherwise | "Connect Cloudflare" |

### Dropdown menu limits actions

| User state | Menu item |
| --- | --- |
| `needsConfiguration` | "Configure AI Gateway" → `/settings` |
| `showCredits` | "Manage AI Gateway" → `/settings` |
| `!hasUserToken` (Connect) | No menu item — Connect lives on the sidebar CTA only |

Settings and Sign Out always remain.

## Badge helper — `src/components/usage-limits-badge.tsx`

`useUsageLimitsBadgeState()` is the shared derivation layer. The visual `UsageLimitsBadge` component is retained but not mounted in the shell (header badge is commented out in `global-header.tsx`).

| User state | Derived / badge content |
| --- | --- |
| Loading | `loading` |
| Token missing, free tier remaining | Connect + `showUsage` |
| Token present, gateway not selected | `needsConfiguration` |
| Connected (token + gateway + credits) | `showCredits` + optional `showUsage` |
| Connected, free tier exhausted, credits known | `showCredits` only |
| Connected but `cloudflareCredits` missing | no `showCredits` (status may still say Connect; sidebar CTA stays hidden) |
| Free tier exhausted, no token | `isExhausted` + Connect |

Rules:

*   Credit balance requires **all three**: `hasUserToken` AND `hasCloudflareConfigured` AND `cloudflareCredits` present.
*   Free-tier usage only when `config.limit` exists AND `!config.unlimited`.
*   Badge click target (if remounted): `/settings` when `needsConfiguration` or `showCredits`, otherwise OAuth connect.

## Credits banner — `src/components/credits-banner.tsx`

| User state | Banner content | CTA |
| --- | --- | --- |
| No `limitsData` / dismissed | Hidden | — |
| `config.unlimited` + connected (BYOK / feature flag on) | `$X.XX` balance · free credits reset in Yh | Dismiss only |
| `config.unlimited` + not connected (self-hosted) | Hidden | — |
| Free tier active, not connected | `N free credits remaining · resets in Yh` | "Connect" + dismiss |
| Free tier active, connected | Same free-tier text | Dismiss only (no connect CTA) |
| Free tier exhausted, connected | Gateway balance · free credits reset in Yh | Dismiss only |

**Threshold:** Banner is also hidden when remaining free credits **or** gateway credits are ≥ `CREDITS_BANNER_THRESHOLD` (`shared/constants/limits.ts`, default `10`). This is intentional noise reduction and is not part of the popup / connect gating.

**Note:** Default backend policy (`excludeCloudflareConnected: false`) keeps connected users on the free-tier counter until exhausted. They only hit the unlimited row when in BYOK mode (`excludeBYOKUsers` bypass) or when `excludeCloudflareConnected` is explicitly enabled.

Rules:

*   Reset text prefers `config.limit.resetAt` (server-provided); falls back to client-side calculation via `getResetDate(window, periodSeconds)`.
*   `rolling` window without `resetAt` uses the verb "resets within" (upper bound); everything else uses "resets in".
*   Connect CTA renders only when `!isConnected` (where `isConnected = hasUserToken && hasCloudflareConfigured`).

## Limit popups — `src/utils/usage-limit-checker.tsx`

Triggered by `checkCanSendPrompt` (pre-flight) and `getBackendLimitDialog` (on backend `USAGE_LIMIT_EXCEEDED`).

| Condition | Dialog | Primary action |
| --- | --- | --- |
| `loading || !limitsData` | None (optimistic allow) | — |
| `limitCheck.withinLimits` (pre-flight only) | None | — |
| `!hasUserToken` | **Daily free limit exhausted** | "Connect Cloudflare" (OAuth) |
| `hasUserToken && !hasCloudflareConfigured` | **Configure AI Gateway** | Navigate to `/settings?config_needed=true` |
| `hasUserToken && hasCloudflareConfigured && credits < MINIMUM_CLOUDFLARE_BALANCE` | **Insufficient credits** (`$X.XX`) | Open `dash.cloudflare.com/{accountId}/ai/ai-gateway/credits` |
| Everything else | None (allow) | — |

`MINIMUM_CLOUDFLARE_BALANCE` is defined in `shared/constants/limits.ts`.

## Deploy gate popup — `src/utils/usage-limit-checker.tsx` (`getDeployGateDialog`)

Unlike the pre-flight limit popups, this one is **backend-error-triggered**: a user-account deploy (`target: 'user'`, think behavior) can fail with a structured `code` on the `cloudflare_deployment_error` WebSocket message (`worker/api/websocketTypes.ts`). `handle-websocket-message.ts` forwards the code via `onCloudflareDeployGate`; `chat.tsx` renders the dialog. The toast + chat message still show in all cases.

**Dormant in the current architecture:** no plane emits `cloudflare_deployment_error` yet, so this popup never appears today — see the note after the table.

| `code` | Emitted when | Dialog | Primary action |
| --- | --- | --- | --- |
| `cloudflare_not_connected` | No decrypted Cloudflare OAuth token for the user | **Connect Cloudflare to deploy** | OAuth flow (`/oauth/login?return_url=...`) |
| `cloudflare_not_configured` | Multiple accounts connected and none selected (a sole connected account is used automatically — deploying needs no AI Gateway) | **Select a Cloudflare account** | Navigate to `/settings?config_needed=true` |
| _(absent)_ | Any other deploy failure | None (toast only) | — |

**Current behavior — `userAccountDeploy` is hardwired to `false`:** both `GET /api/capabilities` implementations return `userAccountDeploy: false` (the light Worker in `buildLightApp()`, `worker/light/lightApp.ts`; the Go control plane in `backend/pkg/api/routes.go`) and **neither plane reads `ENABLE_USER_ACCOUNT_DEPLOY`** — the flag exists only as a declaration in `worker/types/env.d.ts` plus the `docs/setup.md` / `README.md` tables. `src/routes/chat/chat.tsx` therefore resolves `capabilities?.userAccountDeploy ?? false` to `false`: the think deploy button is labelled **Deploy** and calls `handleDeployToCloudflare(..., 'platform')`.

Platform-targeted deploys run through `POST /api/projects/:id/deploy` on the Go control plane (`src/services/controlPlaneClient.ts` → `handleDeployProject` in `backend/pkg/api/routes.go` → `room.DeployRoomVFS` in `backend/pkg/engine/deploy.go`), which uploads to Cloudflare Pages with the **platform** credentials (`CLOUDFLARE_ACCOUNT_ID` / `CLOUDFLARE_API_TOKEN`). The user's Cloudflare connection is never consulted, so a think deploy cannot fail on a connection gate and **this popup is dormant in the current dual-plane architecture**. It becomes reachable only if a plane actually reports `userAccountDeploy: true` (configuring the env var alone is not enough) *and* starts emitting `cloudflare_deployment_error` with a `code`; today no producer exists on either plane — `CloudflareDeploymentErrorCode` (`worker/api/websocketTypes.ts`) is a protocol type consumed by clients (frontend + SDK) only, and Go deploy failures are broadcast as `deployment_started` / `deploy_progress` / `deployment_completed` / `deployment_failed`.

The legacy WebSocket `deploy` message carrying `target` — the only remaining `target: 'user'` route — is a fallback in `src/routes/chat/hooks/use-chat.ts` used solely when the REST deploy request fails.

## Backend interactions

| Behavior | Source |
| --- | --- |
| BYOK users bypass LLM rate limits | `llmConfig.excludeBYOKUsers` (default: `true`) |
| Optionally exempt all connected users from limits | `llmConfig.excludeCloudflareConnected` (default: `false`) |
| Free tier uses calendar-daily window (UTC midnight reset) | `llmConfig.calendarDaily` |
| App-creation limits are independent of LLM limits | Separate `appCreation` config; not touched by connected/BYOK bypass |
| Limits feature disabled entirely | `ENABLE_CLOUDFLARE_LIMITS != 'true'` → `checkUsageAndBalance` returns `limit: Infinity` immediately |
