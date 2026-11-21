#!/usr/bin/env node
/**
 * Diagnose control-plane session auth (P0.3 boundary) in one command.
 *
 * The failure mode this exists for: the boundary is ON, the user is logged in,
 * and every request still answers 401 — because the Go plane verifies against a
 * KV namespace that does not contain the Edge's sessions. That looks identical
 * to "not logged in" in the browser console and cost real time to identify by
 * hand (see the P0 rollout runbook).
 *
 * What it does, read-only:
 *   1. counts live `session:token:*` keys per candidate namespace,
 *   2. takes ONE token from the namespace that actually has sessions,
 *   3. probes the control plane with it on a harmless route,
 *   4. prints a verdict: PASS (404/200), MISMATCH (401), MISCONFIGURED (503).
 *
 * It never prints a session token or a userId; session key names are counted
 * but never printed (namespace ids are printed — they are non-secret config).
 *
 * Usage:
 *   node scripts/check-control-plane-auth.mjs --url https://vibesdk-backend.onrender.com
 *   bun run check:cp-auth --url https://…
 */
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
const envPath = join(repoRoot, '.env');

/** Namespace ids worth checking: the Edge's (prod) store and anything configured. */
const KNOWN_NAMESPACES = [
	{ id: '56d249930a4447ee92493a7ff061a3ae', label: 'VibecoderStore (Edge mints here)' },
	{ id: '4748f35d77274731a17fa4f32c8c3e00', label: 'VibecoderStore-staging (empty unless a staging Edge exists)' },
];

function readEnv(key) {
	if (!existsSync(envPath)) return '';
	const match = readFileSync(envPath, 'utf8').match(new RegExp(`^${key}=(.*)$`, 'm'));
	return match ? match[1].trim().replace(/^["']|["']$/g, '') : '';
}

function argValue(flag) {
	const index = process.argv.indexOf(flag);
	return index >= 0 ? process.argv[index + 1] : '';
}

/** List `session:token:*` keys of one namespace (names only; never printed). */
async function listSessionKeys(accountId, token, namespaceId) {
	const url = `https://api.cloudflare.com/client/v4/accounts/${accountId}/storage/kv/namespaces/${namespaceId}/keys?prefix=session%3Atoken%3A&limit=10`;
	const res = await fetch(url, {
		headers: { Authorization: `Bearer ${token}` },
		signal: AbortSignal.timeout(15_000),
	});
	const payload = await res.json().catch(() => ({}));
	if (!payload.success) {
		const first = (payload.errors ?? [])[0];
		return { ok: false, keys: [], error: `${res.status} ${first?.code ?? ''} ${first?.message ?? ''}`.trim() };
	}
	return { ok: true, keys: (payload.result ?? []).map((entry) => entry.name), error: '' };
}

async function main() {
	const baseUrl = (argValue('--url') || process.env.CONTROL_PLANE_URL || '').replace(/\/$/, '');
	const probePath = argValue('--probe') || '/api/generations/__auth_probe__';
	const accountId = readEnv('CLOUDFLARE_ACCOUNT_ID');
	const cfToken = readEnv('CLOUDFLARE_API_TOKEN');

	if (!baseUrl) {
		console.error('✖ --url <control-plane base URL> is required (e.g. https://vibesdk-backend.onrender.com)');
		process.exit(2);
	}
	if (!accountId || !cfToken) {
		console.error('✖ .env needs CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN (run `bun run d1:token`).');
		process.exit(2);
	}

	const configuredId = readEnv('VIBECODER_STORE_KV_ID');
	const candidates = [...KNOWN_NAMESPACES];
	if (configuredId && !candidates.some((entry) => entry.id === configuredId)) {
		candidates.push({ id: configuredId, label: 'VIBECODER_STORE_KV_ID from .env' });
	}

	console.log(`• Control plane: ${baseUrl}`);
	console.log('• Live sessions per namespace (names are never printed):');
	let tokenWithSessions = '';
	let bestCount = 0;
	let bestNamespaceId = '';
	for (const candidate of candidates) {
		const result = await listSessionKeys(accountId, cfToken, candidate.id);
		if (!result.ok) {
			console.log(`  - ${candidate.id}  ${candidate.label}: ERROR ${result.error}`);
			continue;
		}
		console.log(`  - ${candidate.id}  ${candidate.label}: ${result.keys.length} session(s)`);
		if (result.keys.length > bestCount) {
			bestCount = result.keys.length;
			bestNamespaceId = candidate.id;
			tokenWithSessions = result.keys[0].replace(/^session:token:/, '');
		}
	}

	if (!tokenWithSessions) {
		console.error('\n✖ No live session anywhere: sign in on the SPA first (the Edge writes `session:token:*`).');
		process.exit(1);
	}

	const anonymous = await fetch(`${baseUrl}${probePath}`, {
		signal: AbortSignal.timeout(20_000),
	});
	anonymous.body?.cancel?.();
	console.log(`\n• Anonymous probe ${probePath} → HTTP ${anonymous.status}`);
	if (anonymous.status !== 401 && anonymous.status !== 503) {
		console.error('? Boundary appears OFF — the probe answered without a session token.');
		console.error('  Turn CONTROL_PLANE_REQUIRE_SESSION=true on the staging service and repeat;');
		console.error('  a PASS with the boundary off proves nothing.');
		process.exit(1);
	}
	if (anonymous.status === 503) {
		console.error('? Anonymous probe answered 503: either the boundary is on with no readable KV store,');
		console.error('  or the boundary is off and the handler has no D1. The token probe below disambiguates.');
	}

	const probe = await fetch(`${baseUrl}${probePath}`, {
		headers: { 'X-Session-Token': tokenWithSessions },
		signal: AbortSignal.timeout(20_000),
	});
	const body = await probe.text().catch(() => '');
	const head = body.slice(0, 160);

	console.log(`\n• Probe ${probePath} → HTTP ${probe.status}`);
	if (probe.status === 404 || probe.status === 200) {
		console.log('✓ PASS — the control plane accepted a live Edge session token.');
		console.log('  Boundary is enforcing and the identity store matches. Continue with the runbook.');
		return;
	}
	if (probe.status === 401) {
		console.error('✖ MISMATCH — the token exists in KV but the control plane rejected it.');
		console.error('  The service is verifying against a namespace that does not hold the Edge sessions.');
		console.error(`  Set VIBECODER_STORE_KV_ID=${bestNamespaceId || '<edge namespace>'} on the service and redeploy.`);
		console.error(`  Response: ${head}`);
		process.exit(1);
	}
	if (probe.status === 503) {
		console.error('✖ MISCONFIGURED — the verifier could not read KV at all.');
		console.error('  Check CLOUDFLARE_ACCOUNT_ID / CLOUDFLARE_API_TOKEN / VIBECODER_STORE_KV_ID on the service.');
		console.error(`  Response: ${head}`);
		process.exit(1);
	}
	console.error(`? UNEXPECTED status ${probe.status}: ${head}`);
	process.exit(1);
}

await main();
