/**
 * Dual-Plane API Configuration Adapter.
 *
 * Separates the backend planes the frontend talks to:
 *   - Auth Plane    : the lightweight Cloudflare Worker on Edge (D1/KV) —
 *                     email/password auth, GitHub OAuth, sessions, API keys.
 *   - Control Plane : the Go Fiber backend (Coolify/Hetzner) — project
 *                     rooms, VFS state, WebSocket, LLM generation, deploy.
 *   - Execution Plane: Cloudflare Edge (Workers / Workflows / Vectorize) —
 *                      end-user agent execution.
 *
 * All values are read from Vite environment variables at build time.
 */

const AUTH_PLANE_URL = import.meta.env.VITE_AUTH_PLANE_URL as
	| string
	| undefined;
const CONTROL_PLANE_URL = import.meta.env.VITE_CONTROL_PLANE_URL as
	| string
	| undefined;
const EXECUTION_PLANE_URL = import.meta.env.VITE_EXECUTION_PLANE_URL as
	| string
	| undefined;

/** Normalize a base URL by stripping a trailing slash. */
function trimTrailingSlash(url: string): string {
	return url.replace(/\/+$/, '');
}

/** Convert an http(s) base URL into its ws(s) equivalent. */
function toWebSocketUrl(baseUrl: string): string {
	const trimmed = trimTrailingSlash(baseUrl);
	if (trimmed.startsWith('https://')) {
		return 'wss://' + trimmed.slice('https://'.length);
	}
	if (trimmed.startsWith('http://')) {
		return 'ws://' + trimmed.slice('http://'.length);
	}
	return trimmed;
}

/**
 * Control plane configuration (Go Fiber backend). Falls back to the
 * same-origin host so the app still works in local dev (Vite proxy /
 * Cloudflare plugin) when VITE_CONTROL_PLANE_URL is not set.
 */
export const controlPlane = {
	baseUrl: CONTROL_PLANE_URL
		? trimTrailingSlash(CONTROL_PLANE_URL)
		: window.location.origin,
	/** WebSocket URL derived from the control plane base URL. */
	wsUrl: CONTROL_PLANE_URL
		? toWebSocketUrl(CONTROL_PLANE_URL)
		: `${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}`,
};

/**
 * Auth plane configuration (light Worker on Edge, D1-backed). Falls back
 * to the control plane base URL so production same-origin deployments
 * (where the Worker serves the SPA and auth) keep working without setting
 * VITE_AUTH_PLANE_URL.
 */
export const authPlane = {
	baseUrl: AUTH_PLANE_URL
		? trimTrailingSlash(AUTH_PLANE_URL)
		: controlPlane.baseUrl,
	isConfigured: Boolean(AUTH_PLANE_URL),
};

/**
 * Execution plane configuration (Cloudflare Edge). Optional when features
 * that depend on it are disabled when unset.
 */
export const executionPlane = {
	baseUrl: EXECUTION_PLANE_URL ? trimTrailingSlash(EXECUTION_PLANE_URL) : '',
	isConfigured: Boolean(EXECUTION_PLANE_URL),
};

/** Convenience alias for the WebSocket control-plane URL. */
export const wsControlPlaneUrl = controlPlane.wsUrl;
