#!/usr/bin/env tsx
/**
 * Sync a working Cloudflare bearer token into `.env` for the Go control plane.
 *
 * Why: `backend/cmd/main.go` → `backend/pkg/cloudflare/d1.go` needs a
 * `CLOUDFLARE_API_TOKEN` to read/write D1. The interactive `wrangler login`
 * flow already stores a short-lived OAuth token (with `offline_access`, so
 * wrangler refreshes it on every command) and the D1 HTTP API accepts it. So:
 *
 *   1. run `wrangler whoami` to force an OAuth refresh,
 *   2. validate the token with a read-only D1 `SELECT 1`,
 *   3. write it into `.env` as `CLOUDFLARE_API_TOKEN=…`.
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
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
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
	const databaseId = readEnvValue(envText, 'D1_DATABASE_ID');
	if (!accountId || !databaseId) {
		console.error('✖ CLOUDFLARE_ACCOUNT_ID and D1_DATABASE_ID must be set in .env.');
		process.exit(1);
	}

	// 1) Force a refresh: any wrangler command refreshes the OAuth token when
	//    it is near expiry (the login carries `offline_access`).
	console.log('• Refreshing the wrangler OAuth session…');
	const whoami = spawnSync('bun', ['--bun', 'wrangler', 'whoami'], {
		cwd: repoRoot,
		encoding: 'utf8',
		// Unset so wrangler uses the stored OAuth login: if .env's
		// CLOUDFLARE_API_TOKEN leaked into this process env (e.g. via an
		// IDE/direnv loader), wrangler would prefer it over OAuth and the
		// "refresh" below would never touch the OAuth session.
		env: { ...process.env, CLOUDFLARE_API_TOKEN: '', CLOUDFLARE_API_KEY: '' },
	});
	if (whoami.status !== 0) {
		console.error('✖ wrangler is not logged in. Run `bunx wrangler login` and retry.');
		console.error((whoami.stderr || whoami.stdout || '').trim().slice(0, 300));
		process.exit(1);
	}

	// 2) Read the freshly refreshed token.
	const configPath = wranglerConfigPaths.find((p) => existsSync(p));
	if (!configPath) {
		console.error('✖ No wrangler config found; run `bunx wrangler login` first.');
		process.exit(1);
	}
	const configText = readFileSync(configPath, 'utf8');
	const token = tomlValue(configText, 'oauth_token');
	const expiresAt = tomlValue(configText, 'expiration_time');
	if (!token) {
		console.error(`✖ No oauth_token in ${configPath}; run \`bunx wrangler login\`.`);
		process.exit(1);
	}

	// 3) Never clobber a token the user manages by hand (e.g. a static one).
	const existing = readEnvValue(envText, 'CLOUDFLARE_API_TOKEN');
	if (existing && !existing.startsWith('cfoat') && !force) {
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

	writeFileSync(envPath, setEnvValue(envText, 'CLOUDFLARE_API_TOKEN', token), 'utf8');
	console.log(`✓ CLOUDFLARE_API_TOKEN updated in .env (${fingerprint(token)})`);
	console.log(`  ${result.message}`);
	if (expiresAt) {
		console.log(`  OAuth token expires at ${expiresAt} — re-run \`bun run d1:token\` after that.`);
	}
	console.log('  Durable option: dashboard → My Profile → API Tokens → Account → D1 → Edit.');
}

await main();

