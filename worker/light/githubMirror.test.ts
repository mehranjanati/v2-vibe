import { describe, expect, it } from 'vitest';
import {
	buildTreeEntries,
	GITHUB_API,
	mirrorFiles,
	mirrorProjectToGitHub,
	modeForPath,
	normaliseRepoPath,
} from './githubMirror';

interface RecordedCall {
	method: string;
	url: string;
	body: Record<string, unknown> | null;
}

/**
 * Fake GitHub transport. Handlers are keyed by `METHOD path`; anything
 * unhandled answers 404, which is what a real GitHub does for unknown routes.
 */
function fakeTransport(handlers: Record<string, (call: RecordedCall) => { status: number; body?: unknown }>) {
	const calls: RecordedCall[] = [];
	const transport = async (url: string, init?: RequestInit) => {
		const method = (init?.method ?? 'GET').toUpperCase();
		const path = url.startsWith(GITHUB_API) ? url.slice(GITHUB_API.length) : url;
		const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : null;
		const call = { method, url: path, body };
		calls.push(call);
		const handler = handlers[`${method} ${path}`];
		const result = handler ? handler(call) : { status: 404, body: { message: 'Not Found' } };
		return new Response(result.body === undefined ? '' : JSON.stringify(result.body), {
			status: result.status,
			headers: { 'content-type': 'application/json' },
		});
	};
	return { transport, calls };
}

describe('buildTreeEntries', () => {
	it('sorts by path, marks shell scripts executable and rejects escaping paths', () => {
		const entries = buildTreeEntries({
			'public/index.html': '<html></html>',
			'./public/app.js': 'console.log(1)',
			'scripts/build.sh': 'echo hi',
			'../escape.txt': 'nope',
			'/absolute.txt': 'absolute',
			'public//double.txt': 'double',
		});
		expect(entries.map((entry) => entry.path)).toEqual([
			'public/app.js',
			'public/index.html',
			'scripts/build.sh',
		]);
		expect(entries.find((entry) => entry.path === 'scripts/build.sh')?.mode).toBe('100755');
		expect(entries.find((entry) => entry.path === 'public/app.js')?.mode).toBe('100644');
	});

	it('keeps the first of two paths that normalise to the same repository path', () => {
		const entries = buildTreeEntries({ 'public/a.js': 'first', './public/a.js': 'second' });
		expect(entries).toHaveLength(1);
		expect(entries[0].content).toBe('first');
	});

	it('normalises the exported helpers', () => {
		expect(normaliseRepoPath('public/x.css')).toBe('public/x.css');
		expect(normaliseRepoPath('')).toBeNull();
		expect(normaliseRepoPath('a/../b')).toBeNull();

describe('mirrorFiles', () => {
	it('pushes one atomic commit on an existing branch and moves the ref last', async () => {
		let blobCount = 0;
		const { transport, calls } = fakeTransport({
			'GET /repos/o/r/git/ref/heads/main': () => ({ status: 200, body: { object: { sha: 'head1' } } }),
			'GET /repos/o/r/git/commits/head1': () => ({ status: 200, body: { tree: { sha: 'tree-base' } } }),
			'POST /repos/o/r/git/blobs': () => ({ status: 201, body: { sha: `blob-${++blobCount}` } }),
			'POST /repos/o/r/git/trees': () => ({ status: 201, body: { sha: 'tree-new' } }),
			'POST /repos/o/r/git/commits': () => ({ status: 201, body: { sha: 'commit-new' } }),
			'PATCH /repos/o/r/git/refs/heads/main': () => ({ status: 200, body: {} }),
		});

		const result = await mirrorFiles(transport, {
			token: 't',
			owner: 'o',
			repo: 'r',
			branch: 'main',
			files: { 'public/index.html': '<html></html>', 'public/app.js': 'x' },
			message: 'mirror',
		});

		expect(result).toMatchObject({ ok: true, commitSha: 'commit-new', createdBranch: false, fileCount: 2 });
		// Exactly one commit call — that is the atomicity claim.
		expect(calls.filter((call) => call.method === 'POST' && call.url.endsWith('/git/commits'))).toHaveLength(1);
		// The tree is built on top of the current head's tree.
		const treeCall = calls.find((call) => call.method === 'POST' && call.url.endsWith('/git/trees'));
		expect(treeCall?.body?.base_tree).toBe('tree-base');
		// The commit's single parent is the head.
		const commitCall = calls.find((call) => call.method === 'POST' && call.url.endsWith('/git/commits'));
		expect(commitCall?.body?.parents).toEqual(['head1']);
		// The ref move is the LAST call: nothing is visible before it.
		const last = calls[calls.length - 1];
		expect(last.method).toBe('PATCH');
		expect(last.url).toBe('/repos/o/r/git/refs/heads/main');
	});

	it('creates the branch when the repository has no head yet', async () => {
		let blobCount = 0;
		const { transport, calls } = fakeTransport({
			'POST /repos/o/r/git/blobs': () => ({ status: 201, body: { sha: `blob-${++blobCount}` } }),
			'POST /repos/o/r/git/trees': () => ({ status: 201, body: { sha: 'tree-new' } }),
			'POST /repos/o/r/git/commits': () => ({ status: 201, body: { sha: 'commit-first' } }),
			'POST /repos/o/r/git/refs': () => ({ status: 201, body: {} }),
		});

		const result = await mirrorFiles(transport, {
			token: 't',
			owner: 'o',
			repo: 'r',
			branch: 'main',
			files: { 'public/index.html': '<html></html>' },
			message: 'mirror',
		});

		expect(result).toMatchObject({ ok: true, commitSha: 'commit-first', createdBranch: true });
		const commitCall = calls.find((call) => call.method === 'POST' && call.url.endsWith('/git/commits'));
		expect(commitCall?.body?.parents).toEqual([]);
		expect(calls.some((call) => call.method === 'PATCH')).toBe(false);
	});

	it('fails loudly at the failing step and never moves the ref', async () => {
		const { transport, calls } = fakeTransport({
			'GET /repos/o/r/git/ref/heads/main': () => ({ status: 200, body: { object: { sha: 'head1' } } }),
			'GET /repos/o/r/git/commits/head1': () => ({ status: 200, body: { tree: { sha: 'tree-base' } } }),
			'POST /repos/o/r/git/blobs': () => ({ status: 201, body: { sha: 'blob-1' } }),
			'POST /repos/o/r/git/trees': () => ({ status: 422, body: { message: 'Invalid tree' } }),
		});

		const result = await mirrorFiles(transport, {
			token: 't',
			owner: 'o',
			repo: 'r',
			branch: 'main',
			files: { 'public/index.html': '<html></html>' },
			message: 'mirror',
		});

		expect(result).toMatchObject({ ok: false, step: 'tree', status: 422, error: 'Invalid tree' });
		expect(calls.some((call) => call.url.includes('/git/refs'))).toBe(false);
	});

	it('refuses an empty file map instead of committing nothing', async () => {
		const { transport, calls } = fakeTransport({});
		const result = await mirrorFiles(transport, {
			token: 't',
			owner: 'o',
			repo: 'r',
			branch: 'main',
			files: {},
			message: 'mirror',
		});
		expect(result.ok).toBe(false);
		expect(calls).toHaveLength(0);
	});
});

		expect(modeForPath('deploy.sh')).toBe('100755');
		expect(modeForPath('deploy.yml')).toBe('100644');
	});

/** Tiny D1 stand-in: enough of prepare/bind/first/run for the mirror state. */
function fakeDB(
	options: {
		state?: Record<string, unknown> | null;
		generation?: Record<string, unknown> | null;
		missingTable?: boolean;
	} = {},
) {
	const writes: unknown[][] = [];
	return {
		writes,
		prepare(sql: string) {
			if (options.missingTable) throw new Error('no such table: github_mirrors');
			const statement = {
				bound: [] as unknown[],
				bind(...values: unknown[]) {
					statement.bound = values;
					return statement;
				},
				async first() {
					if (sql.includes('FROM github_mirrors')) return options.state ?? null;
					if (sql.includes('FROM generations')) return options.generation ?? null;
					return null;
				},
				async run() {
					writes.push(statement.bound);
					return { success: true };
				},
			};
			return statement;
		},
	};
}

function envWith(db: unknown) {
	return { DB: db } as unknown as Parameters<typeof mirrorProjectToGitHub>[0];
}

describe('mirrorProjectToGitHub', () => {
	it('skips when the newest succeeded generation is already the mirrored one', async () => {
		const db = fakeDB({
			state: { repo_full_name: 'o/r', branch: 'main', last_pushed_sha: 'sha-old', last_generation_id: 'gen-1' },
			generation: { id: 'gen-1', commit_sha: 'internal-1' },
		});
		const { transport, calls } = fakeTransport({});

		const result = await mirrorProjectToGitHub(envWith(db), {
			userId: 'u1',
			agentId: 'app-1',
			token: 't',
			owner: 'o',
			repo: 'r',
			files: { 'public/index.html': '<html></html>' },
			transport,
		});

		expect(result).toMatchObject({ ok: true, skipped: 'up-to-date', commitSha: 'sha-old' });
		expect(calls).toHaveLength(0);
		expect(db.writes).toHaveLength(0);
	});

	it('mirrors, adds the deploy workflow and records last_pushed_sha + generation', async () => {
		let blobCount = 0;
		const db = fakeDB({ state: null, generation: { id: 'gen-2', commit_sha: 'internal-2' } });
		const { transport, calls } = fakeTransport({
			'POST /repos/o/r/git/blobs': () => ({ status: 201, body: { sha: `blob-${++blobCount}` } }),
			'POST /repos/o/r/git/trees': () => ({ status: 201, body: { sha: 'tree-new' } }),
			'POST /repos/o/r/git/commits': () => ({ status: 201, body: { sha: 'commit-gh' } }),
			'POST /repos/o/r/git/refs': () => ({ status: 201, body: {} }),
		});

		const result = await mirrorProjectToGitHub(envWith(db), {
			userId: 'u1',
			agentId: 'app-1',
			token: 't',
			owner: 'o',
			repo: 'r',
			files: { 'public/index.html': '<html></html>' },
			deployWorkflow: 'name: deploy',
			transport,
		});

		expect(result).toMatchObject({ ok: true, commitSha: 'commit-gh', fileCount: 2 });
		const treeCall = calls.find((call) => call.method === 'POST' && call.url.endsWith('/git/trees'));
		const paths = (treeCall?.body?.tree as Array<{ path: string }>).map((entry) => entry.path);
		expect(paths).toContain('.github/workflows/deploy.yml');
		const commitCall = calls.find((call) => call.method === 'POST' && call.url.endsWith('/git/commits'));
		expect(String(commitCall?.body?.message)).toContain('generation gen-2');
		expect(String(commitCall?.body?.message)).toContain('internal commit internal-2');
		expect(db.writes).toHaveLength(1);
		expect(db.writes[0]).toContain('commit-gh');
		expect(db.writes[0]).toContain('gen-2');
	});

	it('still mirrors when the mirror table is missing (migration not applied)', async () => {
		const db = fakeDB({ missingTable: true });
		const { transport } = fakeTransport({
			'POST /repos/o/r/git/blobs': () => ({ status: 201, body: { sha: 'blob-1' } }),
			'POST /repos/o/r/git/trees': () => ({ status: 201, body: { sha: 'tree-new' } }),
			'POST /repos/o/r/git/commits': () => ({ status: 201, body: { sha: 'commit-gh' } }),
			'POST /repos/o/r/git/refs': () => ({ status: 201, body: {} }),
		});

		const result = await mirrorProjectToGitHub(envWith(db), {
			userId: 'u1',
			agentId: 'app-1',
			token: 't',
			owner: 'o',
			repo: 'r',
			files: { 'public/index.html': '<html></html>' },
			transport,
		});

		expect(result.ok).toBe(true);
		expect(result.commitSha).toBe('commit-gh');
	});
});

});
