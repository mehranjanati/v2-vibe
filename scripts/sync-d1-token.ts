#!/usr/bin/env tsx
/**
 * Sync a working Cloudflare bearer token into `.env` for the Go control plane.
 *
 * Why: `backend/cmd/main.go` → `backend/pkg/cloudflare/d1.go` needs a
 * `CLOUDFLARE_API_TOKEN` to read/write D1. The interactive `wrangler login`
 * flow already stores a short-lived OAuth token (with `offline_access`, so
 * wrangler refreshes it on every command) and the D1 HTTP API accepts it. So:
 *
 *   1. read the OAuth token wrangler already stored and validate it,
 *   2. only if that token no longer works, run `wrangler whoami` (Node >= 22,
 *      neutral cwd) to force a refresh and read the new one,
 *   3. write it into `.env` as `CLOUDFLARE_API_TOKEN=…` (and restore
 *      `D1_DATABASE_ID` from `wrangler.v2.jsonc` when `.env` lost it).
 *
 * Usage: `bun run d1:token`
 * Re-run it whenever the control plane logs `401` or
 * `d1: accountID, apiToken and databaseID are required`.
 *
 * Durable alternative: create a static token in the Cloudflare dashboard
 * (My Profile → API Tokens → Custom token → Account → D1 → Edit) and paste it
 * into `.env` once — this script refuses to overwrite a non-OAuth token unless
 * you pass `--force`.
 */
import { spawnSync } from 'node:child_process';
import { existsSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { homedir, tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
const envPath = join(repoRoot, '.env');
const force = process.argv.includes('--force');

/** Possible locations of wrangler's stored OAuth credentials. */
const wranglerConfigPaths = [
	join(homedir(), 'Library', 'Preferences', '.wrangler', 'config', 'default.toml'),
	join(homedir(), '.config', '.wrangler', 'config', 'default.toml'),
	join(homedir(), '.wrangler', 'config', 'default.toml'),
];

/** Read a `key = value` scalar out of the wrangler TOML config. */
function tomlValue(text: string, key: string): string | null {
	const match = text.match(new RegExp(`^${key}\\s*=\\s*"?([^"\\n]+)"?`, 'm'));
	return match ? match[1].trim() : null;
}

/**
 * The D1 database id lives in `wrangler.v2.jsonc`; `AGENTS.md` requires the
 * `.env` copy to equal it. Deriving it here means a missing key can no longer
 * silently degrade the Go plane's D1 client to "no lineage".
 */
function databaseIdFromConfig(): string | null {
	const configPath = join(repoRoot, 'wrangler.v2.jsonc');
	if (!existsSync(configPath)) return null;
	const match = readFileSync(configPath, 'utf8').match(
		/"database_id"\s*:\s*"([0-9a-f-]{36})"/,
	);
	return match ? match[1] : null;
}

/**
 * Wrangler needs Node >= 22, and `bun --bun wrangler whoami` exits silently on
 * this box (macOS 12.6): it prints "Getting User settings…" and dies without
 * output, which the old version of this script misread as "not logged in".
 * Resolve a real Node binary instead, preferring an explicit `WRANGLER_NODE`.
 */
function resolveWranglerRuntime(): { cmd: string; args: string[] } | null {
	const wranglerBin = join(repoRoot, 'node_modules', 'wrangler', 'bin', 'wrangler.js');
	if (!existsSync(wranglerBin)) return null;

	const candidates: string[] = [];
	const explicit = process.env.WRANGLER_NODE?.trim();
	if (explicit) candidates.push(explicit);

	const nvmRoot = join(homedir(), '.nvm', 'versions', 'node');
	if (existsSync(nvmRoot)) {
		for (const version of readdirSync(nvmRoot).sort().reverse()) {
			candidates.push(join(nvmRoot, version, 'bin', 'node'));
		}
	}
	candidates.push('/usr/local/bin/node', '/opt/homebrew/bin/node');

	for (const candidate of candidates) {
		if (!existsSync(candidate)) continue;
		const probe = spawnSync(candidate, ['--version'], { encoding: 'utf8' });
		const major = Number((probe.stdout ?? '').trim().replace(/^v/, '').split('.')[0]);
		if (Number.isFinite(major) && major >= 22) return { cmd: candidate, args: [wranglerBin] };
	}
	return null;
}

/** Read the OAuth token + expiry wrangler last stored (null when absent). */
function readStoredToken(): { token: string; expiresAt: string | null } | null {
	const configPath = wranglerConfigPaths.find((p) => existsSync(p));
	if (!configPath) return null;
	const text = readFileSync(configPath, 'utf8');
	const token = tomlValue(text, 'oauth_token');
	if (!token) return null;
	return { token, expiresAt: tomlValue(text, 'expiration_time') };
}

/**
 * Force an OAuth refresh by running `wrangler whoami` from a NEUTRAL cwd.
 * Running it inside the repo made wrangler load `.env`, prefer the (possibly
 * stale) `CLOUDFLARE_API_TOKEN` over the OAuth session, and fail with
 * `Invalid access token [code: 9109]` — the refresh never touched the session.
 */
function refreshOAuthSession(): { ok: boolean; detail: string } {
	const runtime = resolveWranglerRuntime();
	if (!runtime) {
		return {
			ok: false,
			detail:
				'no Node >= 22 available for wrangler (set WRANGLER_NODE); bun cannot drive `wrangler whoami` on this box',
		};
	}
	const result = spawnSync(runtime.cmd, [...runtime.args, 'whoami'], {
		cwd: tmpdir(),
		encoding: 'utf8',
		timeout: 60_000,
		env: { ...process.env, CLOUDFLARE_API_TOKEN: '', CLOUDFLARE_API_KEY: '' },
	});
	if (result.status === 0) return { ok: true, detail: 'OAuth session refreshed' };
	const detail = (result.stderr || result.stdout || result.error?.message || '')
		.trim()
		.slice(0, 300);
	return { ok: false, detail: detail || `wrangler exited with ${result.status ?? 'no status'}` };
}

/** Read `KEY=` from `.env` (values may be quoted). */
function readEnvValue(text: string, key: string): string | null {
	const match = text.match(new RegExp(`^${key}=(.*)$`, 'm'));
	if (!match) return null;
	const value = match[1].trim().replace(/^["']|["']$/g, '');
	return value === '' ? null : value;
}

/** Replace `KEY=…` in `.env`, preserving every other line. */
function setEnvValue(text: string, key: string, value: string): string {
	const line = `${key}=${value}`;
	if (new RegExp(`^${key}=`, 'm').test(text)) {
		return text.replace(new RegExp(`^${key}=.*$`, 'm'), line);
	}
	return `${text.replace(/\n*$/, '')}\n${line}\n`;
}

/** Never print secrets: show only the prefix and length. */
function fingerprint(token: string): string {
	return `${token.slice(0, 5)}…(len=${token.length})`;
}

/** Read-only D1 query proving token + account + database all work. */
async function validateToken(
	token: string,
	accountId: string,
	databaseId: string,
): Promise<{ ok: boolean; message: string }> {
	const url = `https://api.cloudflare.com/client/v4/accounts/${accountId}/d1/database/${databaseId}/query`;
	try {
		const res = await fetch(url, {
			method: 'POST',
			headers: {
				Authorization: `Bearer ${token}`,
				'Content-Type': 'application/json',
			},
			body: JSON.stringify({ sql: 'SELECT 1 AS ok' }),
			signal: AbortSignal.timeout(20_000),
		});
		const payload = (await res.json()) as {
			success?: boolean;
			errors?: { code?: number; message?: string }[];
		};
		if (payload.success) return { ok: true, message: 'D1 SELECT 1 → success' };
		const first = payload.errors?.[0];
		return {
			ok: false,
			message: `D1 API rejected the token (HTTP ${res.status}): ${
				first
					? `${first.code ?? ''} ${first.message ?? ''}`.trim()
					: 'unknown error'
			}`,
		};
	} catch (error) {
		return { ok: false, message: `D1 API request failed: ${String(error)}` };
	}
}

async function main(): Promise<void> {
	if (!existsSync(envPath)) {
		console.error(`✖ ${envPath} not found.`);
		process.exit(1);
	}
	const envText = readFileSync(envPath, 'utf8');
	const accountId = readEnvValue(envText, 'CLOUDFLARE_ACCOUNT_ID');
	if (!accountId) {
		console.error('✖ CLOUDFLARE_ACCOUNT_ID must be set in .env.');
		process.exit(1);
	}
	// Derive the database id when `.env` lost it: AGENTS.md requires the two to
	// match, and a missing key silently degrades the Go plane's D1 client.
	const envDatabaseId = readEnvValue(envText, 'D1_DATABASE_ID');
	const databaseId = envDatabaseId ?? databaseIdFromConfig();
	if (!databaseId) {
		console.error(
			'✖ No D1 database id: set D1_DATABASE_ID in .env or keep database_id in wrangler.v2.jsonc.',
		);
		process.exit(1);
	}

	// 1) Validate the token wrangler already stored, and refresh ONLY when it
	//    does not work: a forced `whoami` refresh needs a Node+wrangler pair and
	//    a live OAuth grant, so it must not sit on the happy path.
	let stored = readStoredToken();
	if (stored) {
		const probe = await validateToken(stored.token, accountId, databaseId);
		if (!probe.ok) {
			console.log(`• Stored wrangler token is unusable (${probe.message}); refreshing the OAuth session…`);
			stored = null;
		}
	}
	if (!stored) {
		const refresh = refreshOAuthSession();
		if (!refresh.ok) {
			console.error(`✖ ${refresh.detail}`);
			console.error('  Run `bunx wrangler login` (Node >= 22), or paste a static D1 token into .env.');
			process.exit(1);
		}
		stored = readStoredToken();
		if (!stored) {
			console.error('✖ wrangler refreshed but stored no oauth_token; run `bunx wrangler login`.');
			process.exit(1);
		}
	}
	const token = stored.token;
	const expiresAt = stored.expiresAt;

	// 3) Never clobber a token the user manages by hand (e.g. a static one).
	const existing = readEnvValue(envText, 'CLOUDFLARE_API_TOKEN');
	if (existing && !existing.startsWith('cfoat') && existing !== token && !force) {
		console.log(
			`✓ .env already holds a non-OAuth CLOUDFLARE_API_TOKEN (${fingerprint(existing)}) — keeping it. Pass --force to replace it.`,
		);
		return;
	}

	// 4) Only write a token that actually works against this database.
	console.log('• Validating the token against the D1 HTTP API…');
	const result = await validateToken(token, accountId, databaseId);
	if (!result.ok) {
		console.error(`✖ ${result.message}`);
		console.error('  Run `bunx wrangler login` again, or paste a static D1 token into .env.');
		process.exit(1);
	}

	let nextEnv = setEnvValue(envText, 'CLOUDFLARE_API_TOKEN', token);
	if (!envDatabaseId) nextEnv = setEnvValue(nextEnv, 'D1_DATABASE_ID', databaseId);
	writeFileSync(envPath, nextEnv, 'utf8');
	console.log(`✓ CLOUDFLARE_API_TOKEN updated in .env (${fingerprint(token)})`);
	if (!envDatabaseId) {
		console.log(`✓ D1_DATABASE_ID restored from wrangler.v2.jsonc (${databaseId})`);
	}
	console.log(`  ${result.message}`);
	if (expiresAt) {
		console.log(`  OAuth token expires at ${expiresAt} — re-run \`bun run d1:token\` after that.`);
	}
	console.log('  Durable option: dashboard → My Profile → API Tokens → Account → D1 → Edit.');
}

await main();

