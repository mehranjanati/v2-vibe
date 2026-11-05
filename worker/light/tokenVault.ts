/**
 * Credential vault (P0.6) — encrypted-at-rest storage for provider tokens.
 *
 * The GitHub OAuth `access_token` used to be persisted plaintext in D1
 * (`github_tokens.access_token`, plus the login write to `linked_identities`).
 * Every token now crosses D1 as an AES-256-GCM envelope:
 *
 *     enc:v1:<iv-b64>:<ciphertext-b64>
 *
 * - Key: SHA-256("vibesdk.v2.d1-credential-vault.v1|" + JWT_SECRET) — derived
 *   from the existing Worker secret, so no new binding is introduced and the
 *   wrangler/docs binding parity invariants stay untouched.
 * - Fresh random 12-byte IV per seal; AES-GCM additional data binds the
 *   ciphertext to `github_token:<userId>`, so a row copied between users
 *   fails to open.
 * - Resolve fails closed: an envelope that does not decrypt returns `null`
 *   and the caller prompts a re-authentication instead of leaking a token.
 *
 * Legacy plaintext rows (written before P0.6) are re-sealed lazily on read,
 * and migration `0012_github_tokens_seal.sql` purges any that are never read
 * — so D1 converges to ciphertext-only whether or not the migration has run.
 *
 * P2.7/P6.4 (credential vault): this module is the merge point — add
 * providers here, never plaintext columns.
 */
import type { LightWorkerBindings } from '../types/bindings';

/** Envelope marker — also what migration 0012 matches to purge plaintext rows. */
export const CIPHER_PREFIX = 'enc:v1:';

const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();
const KEY_CONTEXT = 'vibesdk.v2.d1-credential-vault.v1|';
const IV_BYTES = 12;

/** Derive the AES-GCM data key from the Worker secret (domain-separated SHA-256). */
async function vaultKey(env: LightWorkerBindings): Promise<CryptoKey> {
	const raw = await crypto.subtle.digest(
		'SHA-256',
		textEncoder.encode(KEY_CONTEXT + (env.JWT_SECRET || 'dev-secret')),
	);
	return crypto.subtle.importKey('raw', raw, { name: 'AES-GCM' }, false, ['encrypt', 'decrypt']);
}

/** Base64 of raw bytes (binary, not UTF-8 text). */
function b64(bytes: Uint8Array): string {
	let bin = '';
	for (const byte of bytes) bin += String.fromCharCode(byte);
	return btoa(bin);
}

/** Raw bytes from base64. */
function unb64(value: string): Uint8Array {
	const bin = atob(value);
	const bytes = new Uint8Array(bin.length);
	for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
	return bytes;
}

/** AES-256-GCM seal → `enc:v1:<iv>:<ct>` envelope. */
async function seal(key: CryptoKey, plaintext: string, aad: string): Promise<string> {
	const iv = crypto.getRandomValues(new Uint8Array(IV_BYTES));
	const ciphertext = await crypto.subtle.encrypt(
		{ name: 'AES-GCM', iv, additionalData: textEncoder.encode(aad) },
		key,
		textEncoder.encode(plaintext),
	);
	return `${CIPHER_PREFIX}${b64(iv)}:${b64(new Uint8Array(ciphertext))}`;
}

/** Open an `enc:v1:` envelope; `null` when tampered / wrong AAD / not an envelope. */
async function open(key: CryptoKey, envelope: string, aad: string): Promise<string | null> {
	if (!envelope.startsWith(CIPHER_PREFIX)) return null;
	const body = envelope.slice(CIPHER_PREFIX.length);
	const sep = body.indexOf(':');
	if (sep <= 0) return null;
	try {
		const plaintext = await crypto.subtle.decrypt(
			{ name: 'AES-GCM', iv: unb64(body.slice(0, sep)), additionalData: textEncoder.encode(aad) },
			key,
			unb64(body.slice(sep + 1)),
		);
		return textDecoder.decode(plaintext);
	} catch {
		// Tampered ciphertext, wrong key, or bound to another user — fail closed.
		return null;
	}
}

/** AAD context binding a `github_tokens` ciphertext to its owning user. */
function githubTokenAad(userId: string): string {
	return `github_token:${userId}`;
}

/** Seal a GitHub token for a user (for callers storing it outside `github_tokens`). */
export async function sealGitHubToken(
	env: LightWorkerBindings,
	userId: string,
	token: string,
): Promise<string> {
	return seal(await vaultKey(env), token, githubTokenAad(userId));
}

/** Store the GitHub token for a user — D1 receives ciphertext only. */
export async function storeGitHubToken(
	env: LightWorkerBindings,
	userId: string,
	token: string,
	username: string,
): Promise<void> {
	const sealed = await sealGitHubToken(env, userId, token);
	await env.DB.prepare(
		`INSERT INTO github_tokens (user_id, access_token, username, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (user_id) DO UPDATE SET access_token = excluded.access_token, username = excluded.username`,
	)
		.bind(userId, sealed, username, new Date().toISOString())
		.run();
}

/** Resolve the stored GitHub token for a user (decrypt; re-seal legacy rows). */
export async function getGitHubToken(
	env: LightWorkerBindings,
	userId: string,
): Promise<{ token: string; username: string } | null> {
	const row = await env.DB.prepare('SELECT access_token, username FROM github_tokens WHERE user_id = ?')
		.bind(userId)
		.first();
	if (!row) return null;
	const stored = String(row.access_token ?? '');
	const username = String(row.username ?? '');
	const key = await vaultKey(env);
	const aad = githubTokenAad(userId);

	if (stored.startsWith(CIPHER_PREFIX)) {
		const token = await open(key, stored, aad);
		// Fail closed: tampered ciphertext or a row bound to another user.
		return token === null ? null : { token, username };
	}

	// Legacy plaintext row (pre-P0.6): resolve it once, then re-seal in place
	// so D1 converges to ciphertext-only even if migration 0012 has not run yet.
	await env.DB.prepare('UPDATE github_tokens SET access_token = ?, updated_at = ? WHERE user_id = ?')
		.bind(await seal(key, stored, aad), new Date().toISOString(), userId)
		.run();
	return { token: stored, username };
}

/** Revoke the stored GitHub credential (the local delete is authoritative). */
export async function revokeGitHubToken(env: LightWorkerBindings, userId: string): Promise<void> {
	await env.DB.prepare('DELETE FROM github_tokens WHERE user_id = ?').bind(userId).run();
	// `linked_identities` has no migration in this chain (the OAuth login write
	// ignores failures), so tolerate its absence on delete as well.
	try {
		await env.DB.prepare('DELETE FROM linked_identities WHERE user_id = ? AND provider = ?')
			.bind(userId, 'github')
			.run();
	} catch {
		// Table absent — the github_tokens revoke already succeeded.
	}
}
