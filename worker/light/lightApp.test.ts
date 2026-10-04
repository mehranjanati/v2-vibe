/**
 * Light Worker — Hono app route tests.
 *
 * Verifies the API contract for the lightweight edge worker:
 *   - Unknown `/api/*` routes return JSON 404 (not HTML from SPA fallback)
 *   - Known routes still work as expected
 *   - Backend-only routes return 503 NOT_AVAILABLE
 *
 * Runs against the vitest pool-workers runtime so `cloudflare:workers` modules
 * resolve, but drives `buildLightApp()` directly with a minimal fake env.
 */

import { describe, it, expect } from 'vitest';
import { buildLightApp } from './lightApp';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/** Minimal fake ASSETS binding that returns a known HTML response. */
function makeFakeAssets() {
	return {
		async fetch(): Promise<Response> {
			return new Response('<!DOCTYPE html><html><body>SPA</body></html>', {
				status: 200,
				headers: { 'Content-Type': 'text/html' },
			});
		},
	};
}

/**
 * Minimal fake env with required bindings for the light worker.
 *
 * `sessionUserId` seeds the `VibecoderStore` KV binding so a request carrying
 * the `access_token` cookie resolves to that user id (the `session:token:<token>`
 * contract used by `getUserId()`).
 */
function makeFakeEnv(options: { sessionUserId?: string } = {}) {
	const store = new Map<string, string>();
	if (options.sessionUserId) store.set('session:token:test-session-token', options.sessionUserId);
	return {
		ASSETS: makeFakeAssets(),
		DB: {
			prepare: () => ({
				bind: () => ({
					first: async () => null,
					run: async () => ({ success: true }),
					all: async () => ({ results: [], success: true }),
				}),
			}),
		},
		JWT_SECRET: 'test-secret',
		KV: {
			get: async () => null,
			put: async () => {},
			delete: async () => {},
		},
		VibecoderStore: {
			get: async (key: string) => store.get(key) ?? null,
			put: async (key: string, value: string) => {
				store.set(key, value);
			},
			delete: async (key: string) => {
				store.delete(key);
			},
		},
		R2: {
			get: async () => null,
			put: async () => null,
		},
		AI: {
			run: async () => null,
		},
	} as any;
}

/** Helper to make a request to the Hono app. */
async function makeRequest(
	path: string,
	init?: RequestInit,
	envOptions?: { sessionUserId?: string },
) {
	const app = buildLightApp();
	const env = makeFakeEnv(envOptions);
	const ctx = { waitUntil: () => {}, passThroughOnException: () => {} } as any;
	const url = `https://example.com${path}`;
	const req = new Request(url, init);
	return app.fetch(req, env, ctx);
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('Light Worker — API route contract', () => {
	describe('unknown /api/* routes', () => {
		it('returns JSON 404 for unknown /api/* routes (not HTML)', async () => {
			const res = await makeRequest('/api/unknown-route');
			expect(res.status).toBe(404);

			const contentType = res.headers.get('Content-Type');
			expect(contentType).toContain('application/json');

			const body = await res.json();
			expect(body).toEqual({
				success: false,
				error: {
					type: 'NOT_FOUND',
					message: 'API route not found',
				},
			});
		});

		it('returns JSON 404 for /api/typo-route', async () => {
			const res = await makeRequest('/api/capabilitis'); // typo
			expect(res.status).toBe(404);
			const body = await res.json();
			expect(body.success).toBe(false);
			expect(body.error.type).toBe('NOT_FOUND');
		});

		it('returns JSON 404 for /api/v2/unknown', async () => {
			const res = await makeRequest('/api/v2/unknown');
			expect(res.status).toBe(404);
			const body = await res.json();
			expect(body.success).toBe(false);
		});
	});

	describe('known routes still work', () => {
		it('GET /api/status returns 200 with JSON', async () => {
			const res = await makeRequest('/api/status');
			expect(res.status).toBe(200);
			const body = await res.json();
			expect(body.success).toBe(true);
			expect(body.data.status).toBe('ok');
		});

		it('GET /api/capabilities returns 200 with JSON', async () => {
			const res = await makeRequest('/api/capabilities');
			expect(res.status).toBe(200);
			const body = await res.json();
			expect(body.success).toBe(true);
			expect(body.data.features).toBeDefined();
		});

		it('GET /api/limits/usage returns 200 with JSON', async () => {
			const res = await makeRequest('/api/limits/usage');
			expect(res.status).toBe(200);
			const body = await res.json();
			expect(body.success).toBe(true);
		});

		it('GET /api/apps/public returns 200 with empty feed', async () => {
			const res = await makeRequest('/api/apps/public');
			expect(res.status).toBe(200);
			const body = await res.json();
			expect(body.success).toBe(true);
			expect(body.data.apps).toEqual([]);
		});
	});

	describe('GET /api/auth/session', () => {
		it('returns JSON 401 when there is no session cookie', async () => {
			const res = await makeRequest('/api/auth/session');
			expect(res.status).toBe(401);
			expect(res.headers.get('Content-Type')).toContain('application/json');
			const body = await res.json();
			expect(body.success).toBe(false);
			expect(body.error).toBe('Not authenticated');
		});

		it('returns JSON 401 when the cookie token has no KV session', async () => {
			const res = await makeRequest('/api/auth/session', {
				headers: { Cookie: 'access_token=unknown-token' },
			});
			expect(res.status).toBe(401);
			const body = await res.json();
			expect(body.success).toBe(false);
		});

		it('returns { userId } for a valid session cookie', async () => {
			const res = await makeRequest(
				'/api/auth/session',
				{ headers: { Cookie: 'access_token=test-session-token' } },
				{ sessionUserId: 'user-abc' },
			);
			expect(res.status).toBe(200);
			const body = await res.json();
			expect(body).toEqual({ success: true, data: { userId: 'user-abc' } });
		});
	});

	describe('backend-only routes return 503', () => {
		it('GET /api/agent/* returns 503 NOT_AVAILABLE', async () => {
			const res = await makeRequest('/api/agent/test-id');
			expect(res.status).toBe(503);
			const body = await res.json();
			expect(body.success).toBe(false);
			expect(body.error.type).toBe('NOT_AVAILABLE');
		});

		it('GET /api/projects/* returns 503 NOT_AVAILABLE', async () => {
			const res = await makeRequest('/api/projects/test-id/files');
			expect(res.status).toBe(503);
			const body = await res.json();
			expect(body.error.type).toBe('NOT_AVAILABLE');
		});

		it('GET /api/secrets/* returns 503 NOT_AVAILABLE', async () => {
			const res = await makeRequest('/api/secrets');
			expect(res.status).toBe(503);
			const body = await res.json();
			expect(body.error.type).toBe('NOT_AVAILABLE');
		});

		it('GET /ws/* returns 503 NOT_AVAILABLE', async () => {
			const res = await makeRequest('/ws/test-id');
			expect(res.status).toBe(503);
			const body = await res.json();
			expect(body.error.type).toBe('NOT_AVAILABLE');
		});
	});

	describe('SPA fallback', () => {
		it('non-API routes fall back to ASSETS (HTML)', async () => {
			const res = await makeRequest('/some/spa/route');
			expect(res.status).toBe(200);
			const contentType = res.headers.get('Content-Type');
			expect(contentType).toContain('text/html');
			const text = await res.text();
			expect(text).toContain('SPA');
		});
	});
});
