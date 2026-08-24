/**
 * Light Worker — Hono app builder.
 *
 * Builds the Hono application used by the free-tier edge worker. Contains
 * the auth (email/password, GitHub OAuth), GitHub export, apps (D1-backed),
 * usage limits, and platform info routes. The routes are typed against
 * `LightWorkerBindings` so the light worker never references heavy-only
 * bindings, and anything requiring the full backend (codegen, sandbox,
 * WebSockets, stats, secrets, etc.) returns a structured JSON 503.
 */
import { Hono } from 'hono';
import { cors } from 'hono/cors';
import { getCookie, setCookie, deleteCookie } from 'hono/cookie';
import bcrypt from 'bcryptjs';

import type { LightWorkerBindings } from '../types/bindings';

const SESSION_COOKIE = 'access_token';
const CSRF_COOKIE = 'csrf_token';

/** Allowed CORS origins (SPA runs cross-origin in dev and on Pages). */
const ALLOWED_ORIGINS = [
	'https://vibeos-dda.pages.dev',
	'https://production.vibeos-dda.pages.dev',
	'https://vibesdk-v2.mehranjannati.workers.dev',
];
for (const origin of [
	'http://localhost:5173',
	'http://localhost:8080',
	'http://localhost:4173',
]) {
	if (!ALLOWED_ORIGINS.includes(origin)) ALLOWED_ORIGINS.push(origin);
}

/** Generate a random hex string. */
function randHex(bytes = 16): string {
	const arr = new Uint8Array(bytes);
	crypto.getRandomValues(arr);
	return [...arr].map((b) => b.toString(16).padStart(2, '0')).join('');
}

/** Simple JSON success response. */
function ok(data: unknown) {
	return { success: true, data };
}

/** JSON error response. */
function fail(message: string, status = 400) {
	return { success: false, error: message, status };
}

/** SHA-256 hex of a string. */
async function sha256hex(input: string): Promise<string> {
	const buf = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(input));
	return [...new Uint8Array(buf)].map((b) => b.toString(16).padStart(2, '0')).join('');
}

/** Base64 encode a string (for GitHub Contents API). */
function b64encode(input: string): string {
	return btoa(unescape(encodeURIComponent(input)));
}

/** Extract owner/repo from a GitHub URL. */
function extractRepoInfo(url: string): { owner: string; repo: string } | null {
	let clean = url;
	if (clean.startsWith('git@github.com:')) {
		clean = 'https://github.com/' + clean.slice('git@github.com:'.length);
	}
	clean = clean.replace(/\.git$/, '').replace(/\/$/, '');
	const trimmed = clean.replace(/^https?:\/\/github\.com\//, '');
	const parts = trimmed.split('/');
	if (parts.length < 2) return null;
	return { owner: parts[0], repo: parts[1] };
}

/** GitHub Actions workflow that builds + deploys to Cloudflare Pages. */
const CLOUDFLARE_PAGES_WORKFLOW = `name: Deploy to Cloudflare Pages

on:
  push:
    branches: [main]

jobs:
  build-and-deploy:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      deployments: write
    steps:
      - uses: actions/checkout@v4

      - name: Setup Node
        uses: actions/setup-node@v4
        with:
          node-version: 20

      - name: Install dependencies
        run: npm ci || npm install

      - name: Build
        run: npm run build

      - name: Deploy to Cloudflare Pages
        uses: cloudflare/wrangler-action@v3
        with:
          apiToken: \${ secrets.CLOUDFLARE_API_TOKEN }
          accountId: \${ secrets.CLOUDFLARE_ACCOUNT_ID }
          command: pages deploy dist --project-name \${ vars.CLOUDFLARE_PAGES_PROJECT }
`;

/** App context type for the light worker's Hono app. */
export type LightAppEnv = { Bindings: LightWorkerBindings };

/** Build the light worker's Hono application. */
export function buildLightApp(): Hono<LightAppEnv> {
	const app = new Hono<LightAppEnv>();

	// CORS. Because cookies use `credentials: 'include'`, echo the exact
	// request origin and expose the headers the browser attaches. Preflight
	// is handled by Hono.
	app.use(
		'/api/*',
		cors({
			origin: (origin) => {
				if (!origin) return '';
				return ALLOWED_ORIGINS.includes(origin) ? origin : '';
			},
			allowMethods: ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS'],
			allowHeaders: ['Content-Type', 'Accept', 'X-Session-Token', 'X-CSRF-Token'],
			exposeHeaders: ['Content-Length', 'Content-Type'],
			credentials: true,
			maxAge: 86400,
		}),
	);

	// ------------------------- Platform info -------------------------

	app.get('/api/capabilities', (c) =>
		c.json(
			ok({
				version: '2.0.0',
				features: [
					{
						id: 'app',
						name: 'Application',
						description: 'Full-stack web applications',
						enabled: true,
						capabilities: {
							hasPreview: true,
							hasLiveReload: true,
							requiresSandbox: false,
							requiresWebSocket: true,
							supportedViews: ['editor', 'preview', 'docs', 'blueprint'],
							defaultView: 'editor',
							supportedExports: ['github'],
							hasCustomHeaderActions: true,
							hasCustomSidebar: false,
							hasCustomFileFilter: false,
							behaviorType: 'think',
						},
					},
				],
				userAccountDeploy: false,
			}),
		),
	);

	app.get('/api/status', (c) =>
		c.json(ok({ status: 'ok', service: 'vibesdk-v2', time: Date.now() })),
	);

	// Public apps feed. The Go control plane is not reachable from the edge,
	// so serve an empty paginated feed here (matching the Go stub shape) so
	// the SPA's public apps page renders without a 403.
	app.get('/api/apps/public', (c) =>
		c.json(
			ok({
				apps: [],
				pagination: { limit: 20, offset: 0, total: 0, hasMore: false },
			}),
		),
	);

	// -------------------------------------------------------------------------
	// CSRF
	// -------------------------------------------------------------------------

	app.get('/api/auth/csrf-token', (c) => {
		const token = randHex();
		setCookie(c, CSRF_COOKIE, token, { httpOnly: true, sameSite: 'None', secure: true, path: '/' });
		return c.json(ok({ token, expiresIn: 7200 }));
	});

	// -------------------------------------------------------------------------
	// Register (email/password)
	// -------------------------------------------------------------------------

	app.post('/api/auth/register', async (c) => {
		const body = (await c.req.json().catch(() => null)) as {
			email?: string;
			password?: string;
			name?: string;
		} | null;
		if (!body?.email || !body?.password) {
			return c.json(fail('Email and password required'));
		}
		if (body.password.length < 8) {
			return c.json(fail('Password must be at least 8 characters'));
		}

		const email = body.email.toLowerCase();
		const userId = crypto.randomUUID();
		// bcrypt hash — matches the Go control plane (backend/pkg/api) so a
		// user registered here can log in there too (and vice-versa).
		const passwordHash = await bcrypt.hash(body.password, 10);

		try {
			await c.env.DB.prepare(
				`INSERT INTO users
					(id, email, username, display_name, provider, provider_id, password_hash, created_at)
				 VALUES (?, ?, ?, ?, 'email', ?, ?, ?)`,
			)
				.bind(userId, email, body.name || email.split('@')[0], body.name || 'User', userId, passwordHash, new Date().toISOString())
				.run();

			const sessionToken = await sha256hex(`${c.env.JWT_SECRET || 'dev-secret'}:${userId}:${randHex()}`);
			const sessionId = userId;
			await c.env.VibecoderStore.put(`session:v2:${userId}`, sessionToken, { expirationTtl: 86400 * 7 });
			await c.env.VibecoderStore.put(`session:token:${sessionToken}`, userId, { expirationTtl: 86400 * 7 });
			setCookie(c, SESSION_COOKIE, sessionToken, { httpOnly: true, sameSite: 'None', secure: true, path: '/' });

			return c.json({
				success: true,
				data: {
					user: {
						id: userId,
						email,
						displayName: body.name || '',
						username: body.name || email.split('@')[0],
						provider: 'email',
					},
					sessionId,
					expiresAt: new Date(Date.now() + 86400 * 7 * 1000).toISOString(),
					accessToken: sessionToken,
				},
			});
		} catch (err) {
			const msg = err instanceof Error ? err.message : String(err);
			if (msg.includes('UNIQUE')) return c.json(fail('Email already registered', 409), 409);
			return c.json(fail('Registration failed', 500), 500);
		}
	});

	// -------------------------------------------------------------------------
	// Login
	// -------------------------------------------------------------------------

	app.post('/api/auth/login', async (c) => {
		const body = (await c.req.json().catch(() => null)) as {
			email?: string;
			password?: string;
		} | null;
		if (!body?.email || !body?.password) {
			return c.json(fail('Email and password required'));
		}

		const row = await c.env.DB.prepare('SELECT * FROM users WHERE email = ?')
			.bind(body.email.toLowerCase())
			.first();
		if (!row) return c.json(fail('Invalid email or password', 401), 401);

		const user = row as Record<string, unknown>;
		// Stored password_hash may be either a bcrypt hash (accounts created
		// via the Go control plane or the light worker after the migration)
		// or a legacy "salt:hash" (sha256) for older accounts. Accept both.
		const storedHash = String(user.password_hash || '');
		if (!storedHash) {
			return c.json(fail('Invalid email or password', 401), 401);
		}
		let valid = false;
		if (storedHash.startsWith('$2')) {
			valid = await bcrypt.compare(body.password, storedHash);
		} else if (storedHash.includes(':')) {
			const [salt, expectedHash] = storedHash.split(':');
			valid = !!salt && !!expectedHash && (await sha256hex(`${salt}:${body.password}`)) === expectedHash;
		}
		if (!valid) {
			return c.json(fail('Invalid email or password', 401), 401);
		}

		const userId = user.id as string;
		const sessionToken = await sha256hex(`${c.env.JWT_SECRET || 'dev-secret'}:${userId}:${randHex()}`);
		const sessionId = userId;
		await c.env.VibecoderStore.put(`session:v2:${userId}`, sessionToken, { expirationTtl: 86400 * 7 });
		await c.env.VibecoderStore.put(`session:token:${sessionToken}`, userId, { expirationTtl: 86400 * 7 });
		setCookie(c, SESSION_COOKIE, sessionToken, { httpOnly: true, sameSite: 'None', secure: true, path: '/' });

		return c.json({
			success: true,
			data: {
				user: {
					id: userId,
					email: user.email as string,
					displayName: (user.display_name as string) || '',
					username: (user.username as string) || '',
					provider: 'email',
				},
				sessionId,
				expiresAt: new Date(Date.now() + 86400 * 7 * 1000).toISOString(),
				accessToken: sessionToken,
			},
		});
	});

	// -------------------------------------------------------------------------
	// Logout
	// -------------------------------------------------------------------------

	app.post('/api/auth/logout', async (c) => {
		const token = getCookie(c, SESSION_COOKIE);
		if (token) {
			const userId = await c.env.VibecoderStore.get(`session:token:${token}`);
			if (userId) {
				await c.env.VibecoderStore.delete(`session:v2:${userId}`);
				await c.env.VibecoderStore.delete(`session:token:${token}`);
			}
		}
		deleteCookie(c, SESSION_COOKIE, { path: '/', sameSite: 'None', secure: true });
		return c.json(ok({ message: 'Logged out' }));
	});

	// -------------------------------------------------------------------------
	// Profile / providers / sessions / api-keys / identities
	// -------------------------------------------------------------------------

	app.get('/api/auth/profile', async (c) => {
		const token = getCookie(c, SESSION_COOKIE);
		if (!token) return c.json(fail('Not authenticated', 401), 401);
		const userId = await c.env.VibecoderStore.get(`session:token:${token}`);
		if (!userId) return c.json(fail('Not authenticated', 401), 401);

		const row = await c.env.DB.prepare('SELECT * FROM users WHERE id = ?')
			.bind(userId)
			.first();
		if (!row) return c.json(fail('User not found', 404), 404);

		const user = row as Record<string, unknown>;
		const sessionToken = await c.env.VibecoderStore.get(`session:v2:${userId}`);

		return c.json(
			ok({
				user: {
					id: row.id,
					email: row.email,
					username: row.username,
					displayName: row.display_name,
					avatarUrl: row.avatar_url,
					bio: (user.bio as string | undefined) || '',
					emailVerified: Boolean(user.email_verified),
					provider: user.provider as string | undefined,
				},
				sessionId: sessionToken || userId,
				expiresAt: null,
			}),
		);
	});

	app.get('/api/auth/providers', (c) =>
		c.json(
			ok({
				providers: {
					email: true,
					github: Boolean(c.env.GITHUB_EXPORTER_CLIENT_ID),
					google: false,
					cloudflare: false,
				},
				hasOAuth: Boolean(c.env.GITHUB_EXPORTER_CLIENT_ID),
				requiresEmailAuth: true,
			}),
		),
	);

	app.get('/api/auth/sessions', (c) => c.json(ok({ sessions: [] })));
	app.get('/api/auth/api-keys', (c) => c.json(ok({ apiKeys: [] })));
	app.get('/api/auth/identities', (c) => c.json(ok({ identities: [] })));

	// -------------------------------------------------------------------------
	// GitHub OAuth (login)
	// -------------------------------------------------------------------------

	app.get('/api/auth/oauth/github', (c) => {
		const clientId = c.env.GITHUB_EXPORTER_CLIENT_ID;
		if (!clientId) {
			return c.json(fail('GitHub OAuth not configured', 503), 503);
		}
		const baseUrl = new URL(c.req.url).origin;
		const redirectUri = `${baseUrl}/api/auth/github/callback`;
		const state = randHex(16);
		return c.redirect(
			`https://github.com/login/oauth/authorize?client_id=${clientId}&redirect_uri=${encodeURIComponent(redirectUri)}&scope=user:email&state=${state}`,
		);
	});

	app.get('/api/auth/github/callback', async (c) => {
		if (!c.env.GITHUB_EXPORTER_CLIENT_ID || !c.env.GITHUB_EXPORTER_CLIENT_SECRET) {
			return c.json(fail('GitHub OAuth not configured', 503), 503);
		}

		const code = c.req.query('code');
		if (!code) return c.json(fail('Missing code'));

		const tokenRes = await fetch('https://github.com/login/oauth/access_token', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
			body: JSON.stringify({
				client_id: c.env.GITHUB_EXPORTER_CLIENT_ID,
				client_secret: c.env.GITHUB_EXPORTER_CLIENT_SECRET,
				code,
			}),
		});
		const tokenData = (await tokenRes.json()) as { access_token?: string };
		const accessToken = tokenData.access_token;
		if (!accessToken) return c.json(fail('Failed to exchange OAuth code', 401), 401);

		const userRes = await fetch('https://api.github.com/user', {
			headers: { Authorization: `Bearer ${accessToken}` },
		});
		const gh = (await userRes.json()) as { id?: number; login?: string; email?: string; name?: string };
		const email = gh.email || `${gh.id}@github.local`;

		let userId = crypto.randomUUID();
		try {
			const existing = await c.env.DB.prepare('SELECT id FROM users WHERE email = ?')
				.bind(email)
				.first();
			if (existing) {
				userId = existing.id as string;
			} else {
				await c.env.DB.prepare(
					`INSERT INTO users
						(id, email, username, display_name, provider, provider_id, password_hash, created_at)
					 VALUES (?, ?, ?, ?, 'github', ?, ?, ?)`,
				)
					.bind(userId, email, gh.login || '', gh.name || gh.login || 'GitHub User', String(gh.id), 'oauth', new Date().toISOString())
					.run();
			}
			await c.env.DB.prepare(
				`INSERT INTO linked_identities (user_id, provider, provider_user_id, access_token, created_at)
				 VALUES (?, 'github', ?, ?, ?)`,
			)
				.bind(userId, String(gh.id), accessToken, new Date().toISOString())
				.run();
		} catch {
			// ignore failure; we still redirect
		}

		const sessionToken = await sha256hex(`${c.env.JWT_SECRET || 'dev-secret'}:${userId}:${randHex()}`);
		await c.env.VibecoderStore.put(`session:v2:${userId}`, sessionToken, { expirationTtl: 86400 * 7 });
		await c.env.VibecoderStore.put(`session:token:${sessionToken}`, userId, { expirationTtl: 86400 * 7 });
		setCookie(c, SESSION_COOKIE, sessionToken, { httpOnly: true, sameSite: 'None', secure: true, path: '/' });

		return c.redirect('/');
	});

	// -------------------------------------------------------------------------
	// GitHub App Export (create repo, push files, deploy)
	// -------------------------------------------------------------------------

	/** Get the current user ID from the session cookie. */
	async function getUserId(c: { env: LightWorkerBindings; req: { raw: Request } }): Promise<string | null> {
		const cookieHeader = c.req.raw.headers.get('Cookie') || '';
		const match = cookieHeader.match(new RegExp(`${SESSION_COOKIE}=([^;]+)`));
		if (!match) return null;
		const token = decodeURIComponent(match[1]);
		return await c.env.VibecoderStore.get(`session:token:${token}`);
	}

	/** Fetch generated files from the Go control plane. */
	async function fetchProjectFiles(env: LightWorkerBindings, agentId: string): Promise<Record<string, string> | null> {
		const controlPlaneUrl = env.CONTROL_PLANE_URL || 'http://localhost:8080';
		try {
			const res = await fetch(`${controlPlaneUrl}/api/projects/${agentId}/files`);
			if (!res.ok) return null;
			const json = (await res.json()) as { success?: boolean; data?: { files?: Record<string, string> } };
			return json.data?.files || null;
		} catch {
			return null;
		}
	}

	/** Create a GitHub repository. Returns the repo URL or null. */
	async function createGitHubRepo(token: string, name: string, description: string, isPrivate: boolean): Promise<string | null> {
		const res = await fetch('https://api.github.com/user/repos', {
			method: 'POST',
			headers: {
				Authorization: `Bearer ${token}`,
				Accept: 'application/vnd.github+json',
				'Content-Type': 'application/json',
			},
			body: JSON.stringify({ name, description, private: isPrivate, auto_init: true }),
		});
		if (res.status === 422) return 'exists';
		if (res.status !== 201) return null;
		const data = (await res.json()) as { html_url?: string };
		return data.html_url || null;
	}

	/** Push a single file to a GitHub repo via the Contents API. */
	async function pushFile(token: string, owner: string, repo: string, path: string, content: string): Promise<boolean> {
		const url = `https://api.github.com/repos/${owner}/${repo}/contents/${path}`;

		let sha = '';
		const getRes = await fetch(url, {
			headers: { Authorization: `Bearer ${token}`, Accept: 'application/vnd.github+json' },
		});
		if (getRes.ok) {
			const existing = (await getRes.json()) as { sha?: string };
			sha = existing.sha || '';
		}

		const putRes = await fetch(url, {
			method: 'PUT',
			headers: {
				Authorization: `Bearer ${token}`,
				Accept: 'application/vnd.github+json',
				'Content-Type': 'application/json',
			},
			body: JSON.stringify({
				message: `Update ${path}`,
				content: b64encode(content),
				sha: sha || undefined,
			}),
		});
		return putRes.ok;
	}

	/** Push all files (plus deploy workflow) to a GitHub repo. */
	async function pushFiles(token: string, owner: string, repo: string, files: Record<string, string>): Promise<boolean> {
		const allFiles: Record<string, string> = {
			...files,
			'.github/workflows/deploy.yml': CLOUDFLARE_PAGES_WORKFLOW,
		};

		for (const [path, content] of Object.entries(allFiles)) {
			const ok = await pushFile(token, owner, repo, path, content);
			if (!ok) return false;
		}
		return true;
	}

	/** Store the GitHub token in D1 for a user. */
	async function storeGitHubToken(env: LightWorkerBindings, userId: string, token: string, username: string): Promise<void> {
		await env.DB.prepare(
			`INSERT INTO github_tokens (user_id, access_token, username, created_at)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT (user_id) DO UPDATE SET access_token = excluded.access_token, username = excluded.username`,
		)
			.bind(userId, token, username, new Date().toISOString())
			.run();
	}

	/** Get the stored GitHub token for a user from D1. */
	async function getGitHubToken(env: LightWorkerBindings, userId: string): Promise<{ token: string; username: string } | null> {
		const row = await env.DB.prepare('SELECT access_token, username FROM github_tokens WHERE user_id = ?')
			.bind(userId)
			.first();
		if (!row) return null;
		return { token: row.access_token as string, username: row.username as string };
	}

	// POST /api/github-app/export — initiate GitHub export
	app.post('/api/github-app/export', async (c) => {
		const userId = await getUserId(c);
		if (!userId) return c.json(fail('Authentication required', 401), 401);

		const body = (await c.req.json().catch(() => null)) as {
			repositoryName?: string;
			description?: string;
			isPrivate?: boolean;
			agentId?: string;
		} | null;
		if (!body?.repositoryName || !body?.agentId) {
			return c.json(fail('repositoryName and agentId are required'));
		}

		const stored = await getGitHubToken(c.env, userId);
		if (stored) {
			const files = await fetchProjectFiles(c.env, body.agentId);
			if (!files || Object.keys(files).length === 0) {
				return c.json(fail('No generated files found for this project', 404), 404);
			}

			const repoUrl = await createGitHubRepo(
				stored.token,
				body.repositoryName,
				body.description || '',
				body.isPrivate ?? false,
			);
			if (repoUrl === null) {
				return c.json(fail('Failed to create GitHub repository', 500), 500);
			}

			const finalRepoUrl = repoUrl === 'exists'
				? `https://github.com/${stored.username}/${body.repositoryName}`
				: repoUrl;

			const repoInfo = extractRepoInfo(finalRepoUrl);
			if (!repoInfo) {
				return c.json(fail('Invalid repository URL', 500), 500);
			}

			const pushed = await pushFiles(stored.token, repoInfo.owner, repoInfo.repo, files);
			if (!pushed) {
				return c.json(fail('Failed to push files to GitHub', 500), 500);
			}

			return c.json(ok({
				success: true,
				repositoryUrl: finalRepoUrl,
				skippedOAuth: true,
			}));
		}

		const baseUrl = new URL(c.req.url).origin;
		const state = b64encode(JSON.stringify({
			userId,
			agentId: body.agentId,
			repositoryName: body.repositoryName,
			description: body.description || '',
			isPrivate: body.isPrivate ?? false,
		}));
		const redirectUri = `${baseUrl}/api/github-app/callback`;
		const authUrl = `https://github.com/login/oauth/authorize?client_id=${c.env.GITHUB_EXPORTER_CLIENT_ID}&redirect_uri=${encodeURIComponent(redirectUri)}&scope=repo,user:email&state=${state}`;

		return c.json(ok({ authUrl }));
	});

	// GET /api/github-app/authorize — start OAuth flow
	app.get('/api/github-app/authorize', (c) => {
		const clientId = c.env.GITHUB_EXPORTER_CLIENT_ID;
		if (!clientId) {
			return c.json(fail('GitHub OAuth not configured', 503), 503);
		}
		const baseUrl = new URL(c.req.url).origin;
		const redirectUri = `${baseUrl}/api/github-app/callback`;
		const state = randHex(16);
		return c.redirect(
			`https://github.com/login/oauth/authorize?client_id=${clientId}&redirect_uri=${encodeURIComponent(redirectUri)}&scope=repo,user:email&state=${state}`,
		);
	});

	// GET /api/github-app/callback — handle OAuth callback, store token, export
	app.get('/api/github-app/callback', async (c) => {
		if (!c.env.GITHUB_EXPORTER_CLIENT_ID || !c.env.GITHUB_EXPORTER_CLIENT_SECRET) {
			return c.json(fail('GitHub OAuth not configured', 503), 503);
		}

		const code = c.req.query('code');
		const stateParam = c.req.query('state');
		if (!code) return c.json(fail('Missing code'));
		if (!stateParam) return c.json(fail('Missing state'));

		const tokenRes = await fetch('https://github.com/login/oauth/access_token', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
			body: JSON.stringify({
				client_id: c.env.GITHUB_EXPORTER_CLIENT_ID,
				client_secret: c.env.GITHUB_EXPORTER_CLIENT_SECRET,
				code,
			}),
		});
		const tokenData = (await tokenRes.json()) as { access_token?: string };
		const accessToken = tokenData.access_token;
		if (!accessToken) return c.json(fail('Failed to exchange OAuth code', 401), 401);

		const userRes = await fetch('https://api.github.com/user', {
			headers: { Authorization: `Bearer ${accessToken}` },
		});
		const gh = (await userRes.json()) as { login?: string; email?: string; name?: string };

		let stateData: { userId?: string; agentId?: string; repositoryName?: string; description?: string; isPrivate?: boolean } = {};
		try {
			stateData = JSON.parse(decodeURIComponent(escape(atob(stateParam))));
		} catch {
			// State may be a plain random hex (from /authorize) — ignore.
		}

		if (stateData.userId) {
			await storeGitHubToken(c.env, stateData.userId, accessToken, gh.login || '');

			if (stateData.agentId && stateData.repositoryName) {
				const files = await fetchProjectFiles(c.env, stateData.agentId);
				if (files && Object.keys(files).length > 0) {
					const repoUrl = await createGitHubRepo(
						accessToken,
						stateData.repositoryName,
						stateData.description || '',
						stateData.isPrivate ?? false,
					);
					if (repoUrl && repoUrl !== 'exists') {
						const repoInfo = extractRepoInfo(repoUrl);
						if (repoInfo) {
							await pushFiles(accessToken, repoInfo.owner, repoInfo.repo, files);
						}
					}
				}
			}
		}

		return c.redirect('/');
	});

	// POST /api/github-app/check-remote — check remote repository status
	app.post('/api/github-app/check-remote', async (c) => {
		const userId = await getUserId(c);
		if (!userId) return c.json(fail('Authentication required', 401), 401);

		const body = (await c.req.json().catch(() => null)) as {
			repositoryUrl?: string;
			agentId?: string;
		} | null;
		if (!body?.repositoryUrl || !body?.agentId) {
			return c.json(fail('repositoryUrl and agentId are required'));
		}

		const stored = await getGitHubToken(c.env, userId);
		if (!stored) {
			return c.json(fail('No cached GitHub token. Please re-authenticate.', 401), 401);
		}

		const files = await fetchProjectFiles(c.env, body.agentId);
		if (!files) {
			return c.json(fail('No generated files found for this project', 404), 404);
		}

		const repoInfo = extractRepoInfo(body.repositoryUrl);
		if (!repoInfo) {
			return c.json(fail('Invalid repository URL', 400), 400);
		}

		const res = await fetch(`https://api.github.com/repos/${repoInfo.owner}/${repoInfo.repo}`, {
			headers: { Authorization: `Bearer ${stored.token}`, Accept: 'application/vnd.github+json' },
		});
		if (!res.ok) {
			return c.json(ok({
				compatible: false,
				behindBy: 0,
				aheadBy: Object.keys(files).length,
				divergedCommits: [],
			}));
		}

		return c.json(ok({
			compatible: true,
			behindBy: 0,
			aheadBy: Object.keys(files).length,
			divergedCommits: [],
		}));
	});

	// -------------------------------------------------------------------------
	// Apps (backed by D1)
	// -------------------------------------------------------------------------

	/** Map a D1 `apps` row to the frontend's AppWithFavoriteStatus shape. */
	function mapAppRow(row: Record<string, unknown>, isFavorite: boolean): Record<string, unknown> {
		const updatedAt = row.updated_at as string | number | null;
		return {
			id: row.id,
			title: row.title,
			description: row.description ?? '',
			iconUrl: row.icon_url ?? null,
			originalPrompt: row.original_prompt ?? '',
			finalPrompt: row.final_prompt ?? null,
			framework: row.framework ?? null,
			userId: row.user_id ?? null,
			sessionToken: row.session_token ?? null,
			visibility: row.visibility ?? 'private',
			status: row.status ?? 'completed',
			deploymentId: row.deployment_id ?? null,
			githubRepositoryUrl: row.github_repository_url ?? null,
			githubRepositoryVisibility: row.github_repository_visibility ?? null,
			isArchived: Boolean(row.is_archived),
			isFeatured: Boolean(row.is_featured),
			version: row.version ?? 1,
			parentAppId: row.parent_app_id ?? null,
			screenshotUrl: row.screenshot_url ?? null,
			screenshotCapturedAt: row.screenshot_captured_at ?? null,
			createdAt: row.created_at ?? null,
			updatedAt,
			lastDeployedAt: row.last_deployed_at ?? null,
			isFavorite,
			updatedAtFormatted: updatedAt ? new Date(updatedAt as string).toISOString() : '',
		};
	}

		app.get('/api/apps', async (c) => {
			const userId = await getUserId(c);
			if (!userId) return c.json(fail('Authentication required', 401), 401);

			try {
				const { results } = await c.env.DB.prepare(
					`SELECT a.*, CASE WHEN f.id IS NOT NULL THEN 1 ELSE 0 END AS is_favorite
					 FROM apps a
					 LEFT JOIN favorites f ON f.app_id = a.id AND f.user_id = ?
					 WHERE a.user_id = ? AND COALESCE(a.is_archived, 0) = 0
					 ORDER BY COALESCE(a.updated_at, a.created_at) DESC`,
				)
					.bind(userId, userId)
					.all();
				const apps = ((results || []) as unknown as Record<string, unknown>[]).map((row) =>
					mapAppRow(row, Number(row.is_favorite) === 1),
				);
				return c.json(ok({ apps }));
			} catch {
				return c.json(fail('Failed to fetch apps', 500), 500);
			}
		});

		app.get('/api/apps/favorites', async (c) => {
			const userId = await getUserId(c);
			if (!userId) return c.json(fail('Authentication required', 401), 401);

			try {
				const { results } = await c.env.DB.prepare(
					`SELECT a.*, 1 AS is_favorite
					 FROM apps a
					 INNER JOIN favorites f ON f.app_id = a.id AND f.user_id = ?
					 WHERE a.user_id = ? AND COALESCE(a.is_archived, 0) = 0
					 ORDER BY COALESCE(a.updated_at, a.created_at) DESC`,
				)
					.bind(userId, userId)
					.all();
				const apps = ((results || []) as unknown as Record<string, unknown>[]).map((row) =>
					mapAppRow(row, true),
				);
				return c.json(ok({ apps }));
			} catch {
				return c.json(fail('Failed to fetch favorite apps', 500), 500);
			}
		});

	// -------------------------------------------------------------------------
	// Usage limits (lightweight stub: platform defaults, no gateway billing)
	// -------------------------------------------------------------------------

	app.get('/api/limits/usage', (c) =>
		c.json(
			ok({
				cloudflareConnectEnabled: false,
				config: { unlimited: true },
				usage: { credits: { rolling: 0 } },
				limitCheck: { withinLimits: true, exceededLimits: [] },
			}),
		),
	);

	// -------------------------------------------------------------------------
	// Heavy-backend routes → explicit JSON 503
	// -------------------------------------------------------------------------
	// Agent sessions, WebSockets, deployments, stats, model configs, secrets,
	// etc. require the full backend (CodeGen DO, ThinkAgent, SpaceDO). The
	// lightweight Worker cannot serve them; return structured JSON so the SPA
	// can show a proper error instead of failing to parse HTML.
	function notAvailable(): Response {
		return new Response(
			JSON.stringify({
				success: false,
				error: {
					message: 'This feature requires the full backend and is not available on the lightweight deployment.',
					type: 'NOT_AVAILABLE',
				},
			}),
			{ status: 503, headers: { 'Content-Type': 'application/json' } },
		);
	}
	app.all('/api/agent/*', () => notAvailable());
	app.all('/api/agent', () => notAvailable());
	app.all('/api/projects/*', () => notAvailable());
	app.all('/api/user/*', () => notAvailable());
	app.all('/api/stats/*', () => notAvailable());
	app.all('/api/model-configs/*', () => notAvailable());
	app.all('/api/model-configs', () => notAvailable());
	app.all('/api/secrets/*', () => notAvailable());
	app.all('/api/vault/*', () => notAvailable());
	app.all('/api/cloudflare/*', () => notAvailable());
	app.all('/api/github-exporter/*', () => notAvailable());
	app.all('/api/analytics/*', () => notAvailable());
	app.all('/ws/*', () => notAvailable());

	// Fallback: serve the static frontend SPA
	app.all('*', (c) => c.env.ASSETS.fetch(c.req.raw));

	return app;
}

