/**
 * P0.6 credential vault — D1-content tests.
 *
 * Proves the acceptance criteria for credential hardening:
 *   - D1 only ever receives ciphertext (no `bind()` carries a plaintext token)
 *   - resolve (`getGitHubToken`) round-trips back to the original token
 *   - legacy plaintext rows are re-sealed on read (migration-lag convergence)
 *   - a ciphertext copied to another user's row fails closed (`null`)
 *   - revoke deletes the stored credential and tolerates a missing table
 *
 * Runs on the vitest pool-workers runtime (WebCrypto available), driving the
 * vault against a scripted fake D1 that records every prepared statement.
 */
import { describe, it, expect } from 'vitest';
import type { LightWorkerBindings } from '../types/bindings';
import {
	CIPHER_PREFIX,
	getGitHubToken,
	revokeGitHubToken,
	sealGitHubToken,
	storeGitHubToken,
} from './tokenVault';

const PLAINTEXT = 'gho_plaintext_token_for_p06_test';

type Call = { sql: string; params: unknown[] };

/** Scripted fake D1: records binds and applies the vault's row mutations. */
function makeVault(options: {
	row?: Record<string, unknown> | null;
	breakLinkedIdentities?: boolean;
} = {}) {
	const calls: Call[] = [];
	let row: Record<string, unknown> | null = options.row ?? null;

	const db = {
		prepare(sql: string) {
			return {
				bind(...params: unknown[]) {
					calls.push({ sql, params });
					return {
						async first() {
							return row;
						},
						async run() {
							if (options.breakLinkedIdentities && sql.includes('linked_identities')) {
								throw new Error('no such table: linked_identities');
							}
							if (sql.includes('INSERT INTO github_tokens')) {
								row = { user_id: params[0], access_token: params[1], username: params[2] };
							} else if (sql.includes('UPDATE github_tokens')) {
								row = { ...(row ?? {}), access_token: params[0] };
							} else if (sql.includes('DELETE FROM github_tokens')) {
								row = null;
							}
							return { success: true };
						},
						async all() {
							return { results: [] as Record<string, unknown>[], success: true };
						},
					};
				},
			};
		},
	};

	const env = { JWT_SECRET: 'test-secret', DB: db } as unknown as LightWorkerBindings;
	return { env, calls, getRow: () => row };
}

describe('P0.6 credential vault — D1 content', () => {
	it('storeGitHubToken binds only ciphertext — no plaintext token reaches D1', async () => {
		const { env, calls } = makeVault();
		await storeGitHubToken(env, 'user-a', PLAINTEXT, 'alice');

		const insert = calls.find((c) => c.sql.includes('INSERT INTO github_tokens'));
		expect(insert).toBeDefined();
		const stored = String(insert!.params[1]);
		expect(stored.startsWith(CIPHER_PREFIX)).toBe(true);
		expect(stored).not.toContain(PLAINTEXT);

		// Full-scan guarantee: no bind() anywhere in the flow carries plaintext.
		for (const call of calls) {
			for (const param of call.params) expect(String(param)).not.toContain(PLAINTEXT);
		}
	});

	it('resolve round-trips: getGitHubToken returns the original token', async () => {
		const { env } = makeVault();
		await storeGitHubToken(env, 'user-a', PLAINTEXT, 'alice');
		const resolved = await getGitHubToken(env, 'user-a');
		expect(resolved).toEqual({ token: PLAINTEXT, username: 'alice' });
	});

	it('re-seals a legacy plaintext row on read (migration-lag convergence)', async () => {
		const { env, calls, getRow } = makeVault({
			row: { user_id: 'user-a', access_token: PLAINTEXT, username: 'alice' },
		});

		// Resolve still works for the user…
		const resolved = await getGitHubToken(env, 'user-a');
		expect(resolved?.token).toBe(PLAINTEXT);

		// …but the row was rewritten as an envelope, never left plaintext.
		const update = calls.find((c) => c.sql.includes('UPDATE github_tokens'));
		expect(update).toBeDefined();
		expect(String(update!.params[0]).startsWith(CIPHER_PREFIX)).toBe(true);
		expect(String(update!.params[0])).not.toContain(PLAINTEXT);
		expect(String((getRow() as { access_token: string }).access_token)).not.toContain(PLAINTEXT);

		// And the re-sealed row resolves again through the decrypt path.
		const again = await getGitHubToken(env, 'user-a');
		expect(again?.token).toBe(PLAINTEXT);
	});

	it('fails closed when a ciphertext is copied into another user’s row', async () => {
		const { env: envA } = makeVault();
		const envelope = await sealGitHubToken(envA, 'user-a', PLAINTEXT);
		expect(envelope.startsWith(CIPHER_PREFIX)).toBe(true);

		const { env: envB } = makeVault({
			row: { user_id: 'user-b', access_token: envelope, username: 'mallory' },
		});
		expect(await getGitHubToken(envB, 'user-b')).toBeNull();
	});

	it('revokeGitHubToken deletes the credential and tolerates a missing linked_identities table', async () => {
		const { env, calls, getRow } = makeVault({
			row: { user_id: 'user-a', access_token: `${CIPHER_PREFIX}abcd:efgh`, username: 'alice' },
			breakLinkedIdentities: true,
		});

		await revokeGitHubToken(env, 'user-a');

		const del = calls.find((c) => c.sql.includes('DELETE FROM github_tokens'));
		expect(del).toBeDefined();
		expect(del!.params[0]).toBe('user-a');
		expect(getRow()).toBeNull();
	});
});
