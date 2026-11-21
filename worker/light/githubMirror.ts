/**
 * Atomic GitHub mirror (P1.4.5).
 *
 * Before this module the export path pushed each file through the Contents API
 * (`PUT /contents/{path}`), so one generated app landed as N separate commits —
 * a half-failed push left the repository in a state no generation ever
 * produced, and "which commit is my app?" had no answer.
 *
 * The mirror is the Git Data API instead:
 *
 *     blobs → tree (base_tree = current head's tree) → commit → ref
 *
 * One generation therefore lands as exactly ONE commit, and the ref move is the
 * commit point: everything before it is invisible to `git log`, so an
 * interrupted mirror cannot corrupt history — it just leaves orphan objects.
 *
 * `github_mirrors` (worker/database/schema.ts) records `last_pushed_sha` plus
 * the internal generation it mirrored (`last_generation_id`), so a second
 * export of an unchanged generation is a no-op instead of a duplicate commit.
 * The D1 state is deliberately best-effort: a deployment that has not applied
 * the mirror migration still pushes atomically, it just cannot skip duplicates.
 *
 * Scope note (P1.4.6/P1.4.7 stay open): this mirrors one app's tree onto one
 * branch. PR creation and import-from-GitHub are separate tasks.
 */
import type { LightWorkerBindings } from '../types/bindings';

/** The GitHub API origin — a constant so tests can assert on full URLs. */
export const GITHUB_API = 'https://api.github.com';

/** Deploy workflow pinned into every mirrored repo (unchanged from the pre-mirror export). */
export const DEPLOY_WORKFLOW_PATH = '.github/workflows/deploy.yml';

/** The step a failure happened in — the caller reports it, never hides it. */
export type MirrorStep =
	| 'ref'
	| 'head-tree'
	| 'blob'
	| 'tree'
	| 'commit'
	| 'write-ref';

export interface MirrorResult {
	ok: boolean;
	/** GitHub commit sha the branch now points at. */
	commitSha?: string;
	/** True when the branch did not exist and was created by this push. */
	createdBranch?: boolean;
	fileCount?: number;
	/** Set when the mirror was skipped because the generation is already mirrored. */
	skipped?: 'up-to-date';
	step?: MirrorStep;
	status?: number;
	error?: string;
}

/** One entry of a Git tree: a blob mode plus the content it must carry. */
export interface TreeEntry {
	path: string;
	mode: '100644' | '100755';
	content: string;
}

const MAX_PATH_LENGTH = 900;
const BLOB_CONCURRENCY = 5;

/**
 * Normalise a VFS path into a safe repository path, or `null` when the path can
 * escape the tree (`..`, absolute, empty). The VFS is agent-written, so this is
 * a boundary check, not cosmetics.
 */
export function normaliseRepoPath(path: string): string | null {
	const trimmed = path.replace(/^\.\/+/, '').replace(/^\/+/, '').trim();
	if (!trimmed || trimmed.length > MAX_PATH_LENGTH) return null;
	if (trimmed.split('/').some((segment) => segment === '..' || segment === '')) return null;
	return trimmed;
}

/**
 * Executable bit for shell scripts; everything else is a regular file. Git only
 * has these two blob modes for files, and a mirrored `*.sh` that is not
 * executable is a mirror that does not run.
 */
export function modeForPath(path: string): '100644' | '100755' {
	return path.endsWith('.sh') ? '100755' : '100644';
}

/**
 * Turn the project files into tree entries, sorted by path (deterministic
 * ordering keeps tests and diffs stable). Unsafe or duplicate paths are
 * dropped — a mirror must never write outside the repository or overwrite a
 * file with a second, ambiguous entry.
 */
export function buildTreeEntries(files: Record<string, string>): TreeEntry[] {
	const seen = new Set<string>();
	const entries: TreeEntry[] = [];
	for (const [rawPath, content] of Object.entries(files)) {
		const path = normaliseRepoPath(rawPath);
		if (!path || seen.has(path)) continue;
		seen.add(path);
		entries.push({ path, mode: modeForPath(path), content });
	}
	entries.sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
	return entries;
}


/** Minimal fetch shape so tests can inject a fake transport. */
export type FetchLike = (input: string, init?: RequestInit) => Promise<Response>;

function headers(token: string, withBody = false): Record<string, string> {
	const out: Record<string, string> = {
		Authorization: `Bearer ${token}`,
		Accept: 'application/vnd.github+json',
		'User-Agent': 'vibesdk-mirror',
	};
	if (withBody) out['Content-Type'] = 'application/json';
	return out;
}

/** A failed GitHub call, carrying the step so the caller can name it. */
class GitHubCallError extends Error {
	constructor(
		readonly step: MirrorStep,
		readonly status: number,
		message: string,
	) {
		super(message);
	}
}

async function ghCall(
	api: FetchLike,
	path: string,
	init: RequestInit,
): Promise<{ status: number; body: Record<string, unknown> }> {
	const res = await api(`${GITHUB_API}${path}`, init);
	const text = await res.text();
	let body: Record<string, unknown> = {};
	if (text) {
		try {
			body = JSON.parse(text) as Record<string, unknown>;
		} catch {
			body = { message: text.slice(0, 200) };
		}
	}
	return { status: res.status, body };
}

function messageOf(body: Record<string, unknown>): string {
	const value = body.message;
	return typeof value === 'string' && value ? value : 'github request failed';
}

/** Current head of `branch`, or `null` when the branch does not exist yet. */
async function resolveHeadSha(
	api: FetchLike,
	token: string,
	owner: string,
	repo: string,
	branch: string,
): Promise<string | null> {
	const { status, body } = await ghCall(api, `/repos/${owner}/${repo}/git/ref/heads/${branch}`, {
		headers: headers(token),
	});
	if (status === 404) return null;
	if (status !== 200) throw new GitHubCallError('ref', status, messageOf(body));
	const object = body.object as { sha?: string } | undefined;
	if (!object?.sha) throw new GitHubCallError('ref', status, 'missing ref sha');
	return object.sha;
}

/** Tree sha of a commit (the `base_tree` every mirror is built on). */
async function resolveTreeSha(
	api: FetchLike,
	token: string,
	owner: string,
	repo: string,
	commitSha: string,
): Promise<string> {
	const { status, body } = await ghCall(api, `/repos/${owner}/${repo}/git/commits/${commitSha}`, {
		headers: headers(token),
	});
	if (status !== 200) throw new GitHubCallError('head-tree', status, messageOf(body));
	const tree = body.tree as { sha?: string } | undefined;
	if (!tree?.sha) throw new GitHubCallError('head-tree', status, 'missing tree sha');
	return tree.sha;
}

async function createBlob(
	api: FetchLike,
	token: string,
	owner: string,
	repo: string,
	content: string,
): Promise<string> {
	const { status, body } = await ghCall(api, `/repos/${owner}/${repo}/git/blobs`, {
		method: 'POST',
		headers: headers(token, true),
		body: JSON.stringify({ content, encoding: 'utf-8' }),
	});
	if (status !== 201) throw new GitHubCallError('blob', status, messageOf(body));
	const sha = body.sha;
	if (typeof sha !== 'string') throw new GitHubCallError('blob', status, 'missing blob sha');
	return sha;
}

/** Bounded concurrency: GitHub tolerates a handful of parallel blobs, not hundreds. */
async function mapLimit<T, R>(items: T[], limit: number, fn: (item: T) => Promise<R>): Promise<R[]> {
	const out: R[] = new Array(items.length);
	let next = 0;
	const workers = Array.from({ length: Math.min(limit, items.length) }, async () => {
		for (;;) {
			const index = next++;
			if (index >= items.length) return;
			out[index] = await fn(items[index]);
		}
	});
	await Promise.all(workers);
	return out;
}

/**
 * Push `files` to `owner/repo` as ONE commit and move `branch` to it.
 *
 * `transport` is injected (callers pass `fetch`) so the whole sequence is
 * testable without network access.
 */
export async function mirrorFiles(
	transport: FetchLike,
	input: {
		token: string;
		owner: string;
		repo: string;
		branch: string;
		files: Record<string, string>;
		message: string;
	},
): Promise<MirrorResult> {
	const entries = buildTreeEntries(input.files);
	if (entries.length === 0) {
		return { ok: false, step: 'tree', error: 'no files to mirror', fileCount: 0 };
	}

	const base = `/repos/${input.owner}/${input.repo}`;
	try {
		const head = await resolveHeadSha(transport, input.token, input.owner, input.repo, input.branch);
		const baseTree = head
			? await resolveTreeSha(transport, input.token, input.owner, input.repo, head)
			: undefined;

		const blobShas = await mapLimit(entries, BLOB_CONCURRENCY, (entry) =>
			createBlob(transport, input.token, input.owner, input.repo, entry.content),
		);

		const treeRes = await ghCall(transport, `${base}/git/trees`, {
			method: 'POST',
			headers: headers(input.token, true),
			body: JSON.stringify({
				...(baseTree ? { base_tree: baseTree } : {}),
				tree: entries.map((entry, index) => ({
					path: entry.path,
					mode: entry.mode,
					type: 'blob',
					sha: blobShas[index],
				})),
			}),
		});
		if (treeRes.status !== 201) throw new GitHubCallError('tree', treeRes.status, messageOf(treeRes.body));
		const treeSha = treeRes.body.sha;
		if (typeof treeSha !== 'string') throw new GitHubCallError('tree', 201, 'missing tree sha');

		const commitRes = await ghCall(transport, `${base}/git/commits`, {
			method: 'POST',
			headers: headers(input.token, true),
			body: JSON.stringify({
				message: input.message,
				tree: treeSha,
				parents: head ? [head] : [],
			}),
		});
		if (commitRes.status !== 201) {
			throw new GitHubCallError('commit', commitRes.status, messageOf(commitRes.body));
		}
		const commitSha = commitRes.body.sha;
		if (typeof commitSha !== 'string') throw new GitHubCallError('commit', 201, 'missing commit sha');

		// The ref move is the commit point: until it lands, `git log` on the
		// branch still shows the previous state, so a failure here leaves the
		// repository untouched (only unreferenced objects are left behind).
		const refRes = head
			? await ghCall(transport, `${base}/git/refs/heads/${input.branch}`, {
					method: 'PATCH',
					headers: headers(input.token, true),
					body: JSON.stringify({ sha: commitSha, force: false }),
				})
			: await ghCall(transport, `${base}/git/refs`, {
					method: 'POST',
					headers: headers(input.token, true),
					body: JSON.stringify({ ref: `refs/heads/${input.branch}`, sha: commitSha }),
				});
		if (refRes.status !== (head ? 200 : 201)) {
			throw new GitHubCallError('write-ref', refRes.status, messageOf(refRes.body));
		}

		return { ok: true, commitSha, createdBranch: !head, fileCount: entries.length };
	} catch (error) {
		if (error instanceof GitHubCallError) {
			return {
				ok: false,
				step: error.step,
				status: error.status,
				error: error.message,
				fileCount: entries.length,
			};
		}
		return {
			ok: false,
			error: error instanceof Error ? error.message : String(error),
			fileCount: entries.length,
		};
	}
}

/** Persisted mirror state for one (user, app) pair. */
export interface MirrorState {
	repoFullName: string;
	branch: string;
	lastPushedSha: string | null;
	lastGenerationId: string | null;
}

/**
 * Read the mirror row. Returns `null` when there is none — and also when the
 * table does not exist yet (an un-migrated deployment must still mirror, it
 * just cannot skip duplicates).
 */
export async function readMirrorState(
	env: LightWorkerBindings,
	userId: string,
	agentId: string,
): Promise<MirrorState | null> {
	try {
		const row = await env.DB.prepare(
			`SELECT repo_full_name, branch, last_pushed_sha, last_generation_id
			   FROM github_mirrors WHERE user_id = ? AND agent_id = ? LIMIT 1`,
		)
			.bind(userId, agentId)
			.first<{ repo_full_name: string; branch: string; last_pushed_sha: string | null; last_generation_id: string | null }>();
		if (!row) return null;
		return {
			repoFullName: row.repo_full_name,
			branch: row.branch,
			lastPushedSha: row.last_pushed_sha,
			lastGenerationId: row.last_generation_id,
		};
	} catch (error) {
		console.warn('[mirror] state read failed (migration not applied?):', error instanceof Error ? error.message : String(error));
		return null;
	}
}

/** Upsert the mirror row. Best effort: a failed write never fails the mirror. */
export async function writeMirrorState(
	env: LightWorkerBindings,
	userId: string,
	agentId: string,
	state: MirrorState,
): Promise<void> {
	try {
		const now = new Date().toISOString();
		await env.DB.prepare(
			`INSERT INTO github_mirrors
				(user_id, agent_id, repo_full_name, branch, last_pushed_sha, last_generation_id, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(user_id, agent_id) DO UPDATE SET
				repo_full_name = excluded.repo_full_name,
				branch = excluded.branch,
				last_pushed_sha = excluded.last_pushed_sha,
				last_generation_id = excluded.last_generation_id,
				updated_at = excluded.updated_at`,
		)
			.bind(userId, agentId, state.repoFullName, state.branch, state.lastPushedSha, state.lastGenerationId, now, now)
			.run();
	} catch (error) {
		console.warn('[mirror] state write failed:', error instanceof Error ? error.message : String(error));
	}
}

/** The newest succeeded generation of a chat (Lineage, P1.2), if any. */
export async function latestSucceededGeneration(
	env: LightWorkerBindings,
	chatId: string,
): Promise<{ id: string; commitSha: string | null } | null> {
	try {
		const row = await env.DB.prepare(
			`SELECT id, commit_sha FROM generations
			  WHERE chat_id = ? AND status = 'succeeded'
			  ORDER BY created_at DESC, rowid DESC LIMIT 1`,
		)
			.bind(chatId)
			.first<{ id: string; commit_sha: string | null }>();
		return row ? { id: row.id, commitSha: row.commit_sha } : null;
	} catch (error) {
		console.warn('[mirror] generation lookup failed:', error instanceof Error ? error.message : String(error));
		return null;
	}
}

/**
 * Mirror one app to GitHub atomically, skipping when the newest succeeded
 * generation is already the one in `last_pushed_sha`.
 *
 * The commit message names the app, the generation and the internal commit, so
 * the GitHub history is traceable back to lineage (the P1.4.5 requirement) —
 * `git log` alone answers "which generation produced this tree?".
 */
export async function mirrorProjectToGitHub(
	env: LightWorkerBindings,
	input: {
		userId: string;
		agentId: string;
		token: string;
		owner: string;
		repo: string;
		branch?: string;
		/** Project files; the deploy workflow is added here so every mirror ships it. */
		files: Record<string, string>;
		deployWorkflow?: string;
		transport?: FetchLike;
	},
): Promise<MirrorResult> {
	const branch = input.branch ?? 'main';
	const repoFullName = `${input.owner}/${input.repo}`;
	const generation = await latestSucceededGeneration(env, input.agentId);
	const state = await readMirrorState(env, input.userId, input.agentId);

	if (
		state &&
		generation?.id &&
		state.repoFullName === repoFullName &&
		state.lastGenerationId === generation.id &&
		state.lastPushedSha
	) {
		return { ok: true, skipped: 'up-to-date', commitSha: state.lastPushedSha, fileCount: Object.keys(input.files).length };
	}

	const files = { ...input.files };
	if (input.deployWorkflow && !files[DEPLOY_WORKFLOW_PATH]) {
		files[DEPLOY_WORKFLOW_PATH] = input.deployWorkflow;
	}

	const genLabel = generation?.id.slice(0, 8) ?? 'no-generation';
	const internal = generation?.commitSha?.slice(0, 8);
	const message =
		`chore(mirror): app ${input.agentId.slice(0, 8)} generation ${genLabel}` +
		(internal ? `\n\ninternal commit ${internal}` : '') +
		'\n\nMirrored from the VibeSDK control plane as one atomic commit (P1.4.5).';

	const result = await mirrorFiles(input.transport ?? fetch, {
		token: input.token,
		owner: input.owner,
		repo: input.repo,
		branch,
		files,
		message,
	});

	if (result.ok && result.commitSha) {
		await writeMirrorState(env, input.userId, input.agentId, {
			repoFullName,
			branch,
			lastPushedSha: result.commitSha,
			lastGenerationId: generation?.id ?? null,
		});
	}

	return result;
}


/** One recorded file change of a generation (P1.4 diff rows, no contents). */
export interface GenerationFileRow {
	path: string;
	op: string;
	afterSize: number | null;
	author: string | null;
}

/** The recorded diff of one generation — the evidence the PR body carries. */
export async function generationFiles(
	env: LightWorkerBindings,
	generationId: string,
): Promise<GenerationFileRow[]> {
	try {
		const { results } = await env.DB.prepare(
			`SELECT path, op, after_size, author_agent FROM generation_files
			  WHERE generation_id = ? ORDER BY path`,
		)
			.bind(generationId)
			.all<{ path: string; op: string; after_size: number | null; author_agent: string | null }>();
		return (results ?? []).map((row) => ({
			path: row.path,
			op: row.op,
			afterSize: row.after_size,
			author: row.author_agent,
		}));
	} catch (error) {
		console.warn('[mirror] generation diff read failed:', error instanceof Error ? error.message : String(error));
		return [];
	}
}

/** PR body: what changed, which generation produced it, and the internal commit. */
export function buildPullRequestBody(input: {
	appId: string;
	generationId: string | null;
	verdict: string | null;
	internalCommit: string | null;
	branch: string;
	files: GenerationFileRow[];
	fileCount: number;
	notes?: string;
}): string {
	const lines: string[] = [];
	lines.push(
		input.notes?.trim() ||
			`Generation ${input.generationId?.slice(0, 8) ?? 'latest'} of app ${input.appId.slice(0, 8)}.`,
	);
	lines.push('');
	lines.push('| field | value |');
	lines.push('|---|---|');
	lines.push(`| app | \`${input.appId}\` |`);
	lines.push(`| generation | \`${input.generationId ?? 'unknown'}\` |`);
	lines.push(`| verdict | ${input.verdict ?? 'not recorded'} |`);
	lines.push(`| internal commit | ${input.internalCommit ? `\`${input.internalCommit}\`` : 'not recorded'} |`);
	lines.push(`| branch | \`${input.branch}\` |`);
	lines.push('');
	if (input.files.length > 0) {
		const shown = input.files.slice(0, 40);
		lines.push(`### Changed files (${input.files.length})`);
		lines.push('');
		lines.push('| path | op | size | author |');
		lines.push('|---|---|---|---|');
		for (const file of shown) {
			lines.push(`| \`${file.path}\` | ${file.op} | ${file.afterSize ?? '—'} | ${file.author ?? 'unknown'} |`);
		}
		if (input.files.length > shown.length) {
			lines.push('');
			lines.push(`…and ${input.files.length - shown.length} more.`);
		}
	} else {
		lines.push(`### Files (${input.fileCount})`);
		lines.push('');
		lines.push('The lineage diff for this generation is not recorded; the tree carries all files.');
	}
	lines.push('');
	lines.push('---');
	lines.push(
		'Mirrored from the VibeSDK control plane as one atomic commit (P1.4.5); this PR was opened from the app History surface (P1.4.6).',
	);
	return lines.join('\n');
}


export interface PullRequestResult {
	ok: boolean;
	prUrl?: string;
	prNumber?: number;
	/** Commit the head branch points at after the mirror. */
	commitSha?: string;
	head?: string;
	base?: string;
	fileCount?: number;
	/** True when an open PR for the same head already existed and was returned. */
	reused?: boolean;
	step?: MirrorStep | 'pull-request' | 'pull-list';
	status?: number;
	error?: string;
}

/**
 * Open (or reuse) a pull request for the newest succeeded generation (P1.4.6).
 *
 * The tree is mirrored onto `gen/<short>` with the same atomic push as P1.4.5,
 * so the head branch is exactly one commit; the PR then targets `base`. A 422
 * "A pull request already exists" is not a failure — the open PR for that head
 * is returned, so a double click never creates two PRs.
 *
 * `head` is derived from lineage, never from the client: the PR always shows
 * the generation the control plane recorded, not whatever the UI had selected.
 */
export async function openGenerationPullRequest(
	env: LightWorkerBindings,
	input: {
		userId: string;
		agentId: string;
		token: string;
		owner: string;
		repo: string;
		base?: string;
		files: Record<string, string>;
		deployWorkflow?: string;
		title?: string;
		notes?: string;
		transport?: FetchLike;
	},
): Promise<PullRequestResult> {
	const transport = input.transport ?? fetch;
	const base = input.base ?? 'main';
	const generation = await latestSucceededGeneration(env, input.agentId);
	const head = `gen/${(generation?.id ?? 'latest').slice(0, 8)}`;
	const fullRepo = `${input.owner}/${input.repo}`;

	const mirror = await mirrorFiles(transport, {
		token: input.token,
		owner: input.owner,
		repo: input.repo,
		branch: head,
		files: {
			...input.files,
			...(input.deployWorkflow ? { [DEPLOY_WORKFLOW_PATH]: input.deployWorkflow } : {}),
		},
		message:
			`chore(mirror): generation ${generation?.id.slice(0, 8) ?? 'latest'} on ${head}` +
			(generation?.commitSha ? `\n\ninternal commit ${generation.commitSha.slice(0, 8)}` : ''),
	});
	if (!mirror.ok) {
		return { ok: false, step: mirror.step, status: mirror.status, error: mirror.error, head, base };
	}

	const diff = generation?.id ? await generationFiles(env, generation.id) : [];
	const title =
		input.title?.trim() ||
		`Generation ${(generation?.id ?? '').slice(0, 8) || 'latest'} of app ${input.agentId.slice(0, 8)}`;
	const body = buildPullRequestBody({
		appId: input.agentId,
		generationId: generation?.id ?? null,
		verdict: null,
		internalCommit: generation?.commitSha ?? null,
		branch: head,
		files: diff,
		fileCount: mirror.fileCount ?? 0,
		...(input.notes ? { notes: input.notes } : {}),
	});

	const create = await ghCall(transport, `/repos/${fullRepo}/pulls`, {
		method: 'POST',
		headers: headers(input.token, true),
		body: JSON.stringify({ title, head, base, body }),
	});
	const common = {
		commitSha: mirror.commitSha,
		head,
		base,
		fileCount: mirror.fileCount,
	};
	if (create.status === 201) {
		return {
			ok: true,
			...common,
			prUrl: typeof create.body.html_url === 'string' ? create.body.html_url : undefined,
			prNumber: typeof create.body.number === 'number' ? create.body.number : undefined,
			reused: false,
		};
	}

	if (create.status === 422) {
		const listed = await ghCall(
			transport,
			`/repos/${fullRepo}/pulls?state=open&head=${encodeURIComponent(`${input.owner}:${head}`)}`,
			{ headers: headers(input.token) },
		);
		if (listed.status === 200 && Array.isArray(listed.body)) {
			const existing = (listed.body as Array<Record<string, unknown>>)[0];
			if (existing) {
				return {
					ok: true,
					...common,
					prUrl: typeof existing.html_url === 'string' ? existing.html_url : undefined,
					prNumber: typeof existing.number === 'number' ? existing.number : undefined,
					reused: true,
				};
			}
		}
		// Nothing to compare: the head tree is already what base has.
		return {
			ok: false,
			...common,
			step: 'pull-request',
			status: 422,
			error: 'no changes to pull (head is already merged into base)',
		};
	}

	return { ok: false, ...common, step: 'pull-request', status: create.status, error: messageOf(create.body) };
}


