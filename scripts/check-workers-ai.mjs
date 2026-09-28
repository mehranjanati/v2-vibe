// Diagnostic: figures out which Cloudflare credential can actually reach
// Workers AI. Reads the wrangler OAuth token from the wrangler config and
// tries it + any .env token against several endpoints.
import { readFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

const ACCT = 'fa1010a4ad726c6014a6d6304dc2bc0c';

function readWranglerOAuth() {
	const candidates = [
		join(homedir(), 'Library/Preferences/.wrangler/config/default.toml'),
		join(homedir(), '.wrangler/config/default.toml'),
		join(homedir(), '.config/.wrangler/config/default.toml'),
	];
	for (const p of candidates) {
		try {
			const txt = readFileSync(p, 'utf8');
			const m = txt.match(/oauth_token\s*=\s*"([^"]+)"/);
			if (m) return m[1];
		} catch {}
	}
	return '';
}

function readEnv(key) {
	try {
		for (const line of readFileSync(join(process.cwd(), '.env'), 'utf8').split('\n')) {
			if (line.startsWith(`${key}=`)) return line.slice(key.length + 1).trim();
		}
	} catch {}
	return '';
}

async function tryAuth(label, token) {
	const endpoints = [
		{
			name: '/user/tokens/verify (real API token only)',
			fn: () => fetch('https://api.cloudflare.com/client/v4/user/tokens/verify', { headers: { Authorization: `Bearer ${token}` } }),
		},
		{
			name: '/accounts (what can this cred see?)',
			fn: () => fetch(`https://api.cloudflare.com/client/v4/accounts`, { headers: { Authorization: `Bearer ${token}` } }),
		},
		{
			name: `/accounts/${ACCT}/ai/models/search (Workers AI)`,
			fn: () => fetch(`https://api.cloudflare.com/client/v4/accounts/${ACCT}/ai/models/search?per_page=1`, { headers: { Authorization: `Bearer ${token}` } }),
		},
		{
			name: `/accounts/${ACCT}/ai/v1/chat/completions`,
			fn: () => fetch(`https://api.cloudflare.com/client/v4/accounts/${ACCT}/ai/v1/chat/completions`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
				body: JSON.stringify({ model: '@cf/qwen/qwen2.5-coder-32b-instruct', messages: [{ role: 'user', content: 'hi' }], max_tokens: 1 }),
			}),
		},
	];

	console.log(`\n=== ${label} (${token.slice(0, 10)}... len=${token.length}) ===`);
	for (const e of endpoints) {
		try {
			const res = await e.fn();
			let body = '';
			try { body = JSON.stringify((await res.json())); } catch { body = await res.text(); }
			if (body.length > 160) body = body.slice(0, 160) + '…';
			console.log(`  ${e.name}\n    -> HTTP ${res.status} ${body}`);
		} catch (err) {
			console.log(`  ${e.name}\n    -> ERROR ${err.message}`);
		}
	}
}

const oauth = readWranglerOAuth();
const apiToken = readEnv('CLOUDFLARE_API_TOKEN') || readEnv('AI_GATEWAY_API_KEY');

if (oauth) await tryAuth('WRANGLER OAuth token', oauth);
if (apiToken && apiToken !== oauth) await tryAuth('ENV token (CLOUDFLARE_API_TOKEN/AI_GATEWAY_API_KEY)', apiToken);
if (!oauth && !apiToken) console.log('No tokens found.');