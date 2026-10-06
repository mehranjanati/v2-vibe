/**
 * Control-plane session token storage (P0.3).
 *
 * The Go control plane authenticates mutating routes with a session token sent
 * as `X-Session-Token` (or `Authorization: Bearer`); see
 * `backend/pkg/api/auth.go`. The token is the Edge session token minted by the
 * light Worker (`worker/light/lightApp.ts`), which the login/register response
 * returns as `accessToken`.
 *
 * The HttpOnly `session` cookie cannot be read by JS and is not sent to a
 * cross-origin control plane, so the token is mirrored here (localStorage)
 * for the control-plane client to attach. `anonymous_session_token` (created
 * by `src/lib/api-client.ts` for anonymous visitors) is the fallback so a
 * pre-login session still identifies the caller.
 *
 * NOTE: identity unification across the two planes is owned by the P0.9
 * decision record (`docs/DEV_CHECKLIST.md` P0.9) — this module only carries
 * the token the browser is allowed to hold.
 */

/** localStorage key holding the Edge session token. */
export const CONTROL_PLANE_SESSION_TOKEN_KEY = 'vibesdk_session_token';

/** localStorage key of the anonymous session token created by api-client. */
const ANONYMOUS_SESSION_TOKEN_KEY = 'anonymous_session_token';

/** Persist the token returned by a login/register response. No-op when absent. */
export function storeControlPlaneSessionToken(
	token: string | null | undefined,
): void {
	if (typeof window === 'undefined') return;
	if (!token) return;
	try {
		window.localStorage.setItem(CONTROL_PLANE_SESSION_TOKEN_KEY, token);
	} catch {
		// Private-mode / storage-disabled browsers: the cookie path still works
		// for same-origin control-plane deployments.
	}
}

/** Drop the stored session token (logout). */
export function clearControlPlaneSessionToken(): void {
	if (typeof window === 'undefined') return;
	try {
		window.localStorage.removeItem(CONTROL_PLANE_SESSION_TOKEN_KEY);
	} catch {
		// ignore
	}
}

/**
 * The token the control-plane client should present, or null when the caller
 * is fully anonymous.
 */
export function readControlPlaneSessionToken(): string | null {
	if (typeof window === 'undefined') return null;
	try {
		return (
			window.localStorage.getItem(CONTROL_PLANE_SESSION_TOKEN_KEY) ||
			window.localStorage.getItem(ANONYMOUS_SESSION_TOKEN_KEY)
		);
	} catch {
		return null;
	}
}
