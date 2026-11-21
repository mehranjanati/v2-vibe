import { useState } from 'react';
import { Badge, Button, Dialog, Empty, Loader, cn } from '@cloudflare/kumo';
import {
	ArrowCounterClockwise,
	ClockCounterClockwise,
	GitPullRequest,
} from '@phosphor-icons/react';
import {
	useCreatePullRequest,
	useGenerationDiff,
	useGenerationHistory,
	useRollbackGeneration,
} from '@/hooks/use-generation-history';
import type { GenerationSummary } from '@/api-types';

/**
 * P1.5 — History tab.
 *
 * Reads the P1.4 control-plane API: the list comes from `GET
 * /api/generations/:chatId` (verdict + commit_sha + branch), the diff from the
 * generation's recorded rows (op + hashes + sizes, never contents), and
 * rollback creates a NEW revert commit — the copy says so instead of implying a
 * history rewrite.
 *
 * The pull-request action (P1.4.6) uses the Edge mirror: it pushes this
 * generation onto `gen/<id>` as one atomic commit and opens (or reuses) a PR
 * against the base branch. The mirror-pending state (P1.5.4) stays absent — a
 * failed mirror is reported by the action itself, not by a stale flag.
 */

type BadgeVariant = 'success' | 'warning' | 'error' | 'neutral' | 'orange';

const VERDICT_VARIANT: Record<string, BadgeVariant> = {
	approve: 'success',
	done: 'neutral',
	request_changes: 'warning',
	error: 'error',
	cancelled: 'neutral',
};

const OP_VARIANT: Record<string, BadgeVariant> = {
	create: 'success',
	modify: 'warning',
	delete: 'error',
};

/** Commits are shown short, like Git does; the full SHA stays in the tooltip. */
function shortSha(sha?: string | null): string {
	return sha ? sha.slice(0, 8) : '—';
}

function shortHash(hash?: string | null): string {
	return hash ? hash.slice(0, 7) : '—';
}

/**
 * D1 stores `created_at` as `YYYY-MM-DD HH:MM:SS` in UTC (CURRENT_TIMESTAMP), so
 * a naive `new Date()` would read it as local time. Normalise first, and fall
 * back to the raw value when it does not parse.
 */
function formatWhen(value?: string | null): string {
	if (!value) return '';
	const normalised = value.includes('T') ? value : `${value.replace(' ', 'T')}Z`;
	const parsed = new Date(normalised);
	return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

export function HistoryPanel({ chatId }: { chatId?: string }) {
	const history = useGenerationHistory(chatId);
	const [selectedId, setSelectedId] = useState<string | null>(null);
	const diff = useGenerationDiff(chatId, selectedId);
	const rollback = useRollbackGeneration(chatId);
	const createPr = useCreatePullRequest(chatId);
	const [notice, setNotice] = useState<{ tone: 'success' | 'alert'; text: string } | null>(null);
	const [pendingRollback, setPendingRollback] = useState<GenerationSummary | null>(null);

	const generations = history.data ?? [];
	const selected = generations.find((gen) => gen.id === selectedId) ?? null;

	const runRollback = async () => {
		if (!pendingRollback) return;
		const target = pendingRollback;
		try {
			const result = await rollback.mutateAsync(target.id);
			setNotice({
				tone: 'success',
				text: result.applied
					? `Rolled back to ${shortSha(target.commitSha)} — ${result.restoredFiles} file(s) restored as revert commit ${shortSha(result.commitSha)}.`
					: `Revert commit ${shortSha(result.commitSha)} created. The working set is restored the next time this project opens.`,
			});
		} catch (error) {
			setNotice({
				tone: 'alert',
				text: error instanceof Error ? error.message : 'Rollback failed',
			});
		} finally {
			setPendingRollback(null);
		}
	};

	if (!chatId) {
		return (
			<div className="grid flex-1 place-items-center p-8">
				<Empty
					size="sm"
					title="No project yet"
					description="History appears once this chat has a generated app."
				/>
			</div>
		);
	}
	if (history.isPending) {
		return (
			<div className="grid flex-1 place-items-center p-8">
				<Loader />
			</div>
		);
	}
	if (history.isError) {
		return (
			<div className="flex-1 p-6 text-sm text-text-50/80">
				History is unavailable right now
				{history.error instanceof Error ? `: ${history.error.message}` : '.'}
			</div>
		);
	}
	if (generations.length === 0) {
		return (
			<div className="grid flex-1 place-items-center p-8">
				<Empty
					size="sm"
					title="No generations yet"
					description="Every finished run becomes a revision here, with its commit and file changes."
				/>
			</div>
		);
	}
	return (
		<div className="flex min-h-0 flex-1 flex-col">
			{notice && (
				<div
					className={cn(
						'mx-3 mt-3 rounded-md border px-3 py-2 text-xs',
						notice.tone === 'success'
							? 'border-border bg-bg-2 text-text-primary'
							: 'border-red-500/40 bg-red-500/10 text-text-primary',
					)}
					role="status"
				>
					{notice.text}
				</div>
			)}

			<div className="grid min-h-0 flex-1 grid-cols-1 divide-y divide-border/60 lg:grid-cols-[minmax(0,20rem)_minmax(0,1fr)] lg:divide-x lg:divide-y-0">
				<div className="min-h-0 overflow-y-auto" aria-label="Generations">
					{generations.map((gen) => (
						<button
							key={gen.id}
							type="button"
							onClick={() => setSelectedId(gen.id)}
							className={cn(
								'w-full border-b border-border/60 px-3 py-2.5 text-left transition-colors',
								selectedId === gen.id ? 'bg-bg-3' : 'hover:bg-bg-2',
							)}
						>
							<div className="flex items-center justify-between gap-2">
								<span
									className="font-mono text-xs text-text-primary"
									title={gen.commitSha ?? 'no internal commit'}
								>
									{shortSha(gen.commitSha)}
								</span>
								<Badge variant={VERDICT_VARIANT[gen.verdict ?? gen.status] ?? 'neutral'}>
									{gen.verdict ?? gen.status}
								</Badge>
							</div>
							<div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-text-50/70">
								<span className="font-mono">{gen.branch ?? 'main'}</span>
								<span aria-hidden>·</span>
								<span>{formatWhen(gen.createdAt)}</span>
								{gen.fork && <Badge variant="orange">fork</Badge>}
								{gen.parent && (
									<span className="font-mono" title={`parent ${gen.parent}`}>
										← {gen.parent.slice(0, 8)}
									</span>
								)}
							</div>
						</button>
					))}
				</div>

				<div className="min-h-0 overflow-y-auto p-3">
					{!selectedId ? (
						<div className="grid h-full place-items-center gap-2 text-center text-sm text-text-50/70">
							<ClockCounterClockwise className="size-6" />
							<span>Pick a generation to see what it changed.</span>
						</div>
					) : diff.isPending ? (
						<div className="grid h-full place-items-center">
							<Loader />
						</div>
					) : diff.isError ? (
						<p className="text-sm text-text-50/80">
							The diff for this generation is unavailable
							{diff.error instanceof Error ? `: ${diff.error.message}` : '.'}
						</p>
					) : (
						<div className="space-y-3">
							<div className="flex flex-wrap items-center justify-between gap-2">
								<div className="text-xs text-text-50/70">
									<span className="font-mono text-text-primary">
										{shortSha(diff.data?.commitSha)}
									</span>
									<span> · {diff.data?.files.length ?? 0} file(s) changed</span>
								</div>
								<div className="flex flex-wrap items-center gap-2">
									<Button
										variant="secondary"
										size="sm"
										icon={<GitPullRequest />}
										onClick={() =>
											createPr.mutate({}, {
												onSuccess: (data) => {
													// The Edge derives head/base from lineage, so a
													// second click reuses the open PR instead of
													// opening another one.
													setNotice({
														tone: 'success',
														text: data.reused
															? `Pull request #${data.prNumber ?? '—'} is already open for ${data.head} → ${data.base}.`
															: `Opened pull request #${data.prNumber ?? '—'} (${data.head} → ${data.base}, ${data.files} file(s)).`,
													});
													if (data.prUrl) {
														window.open(data.prUrl, '_blank', 'noopener,noreferrer');
													}
												},
												onError: (error) => {
													setNotice({
														tone: 'alert',
														text:
															error instanceof Error
																? error.message
																: 'Failed to open the pull request',
													});
												},
											})
										}
										loading={createPr.isPending}
										title="Mirror this generation onto gen/<id> and open a pull request against the base branch"
									>
										Create pull request
									</Button>
									<Button
										variant="secondary"
										size="sm"
										icon={<ArrowCounterClockwise />}
										onClick={() => {
											if (selected) setPendingRollback(selected);
										}}
										disabled={!selected?.commitSha || rollback.isPending}
										title={
											selected?.commitSha
												? 'Restore this revision as a new commit'
												: 'This generation has no internal commit to restore'
										}
									>
										Roll back to this
									</Button>
								</div>
							</div>

							{(diff.data?.files.length ?? 0) === 0 ? (
								<p className="text-xs text-text-50/70">This run recorded no file changes.</p>
							) : (
								<table className="w-full border-collapse text-left text-xs">
									<thead>
										<tr className="text-[11px] uppercase tracking-wide text-text-50/60">
											<th className="py-1 pr-2 font-medium">Path</th>
											<th className="py-1 pr-2 font-medium">Op</th>
											<th className="py-1 pr-2 font-medium">Before</th>
											<th className="py-1 pr-2 font-medium">After</th>
											<th className="py-1 font-medium">Author</th>
										</tr>
									</thead>
									<tbody>
										{(diff.data?.files ?? []).map((file) => (
											<tr key={`${file.path}:${file.op}`} className="border-t border-border/60">
												<td className="py-1.5 pr-2 font-mono text-text-primary">{file.path}</td>
												<td className="py-1.5 pr-2">
													<Badge variant={OP_VARIANT[file.op] ?? 'neutral'}>{file.op}</Badge>
												</td>
												<td
													className="py-1.5 pr-2 font-mono text-text-50/70"
													title={file.beforeHash ?? ''}
												>
													{shortHash(file.beforeHash)} · {file.beforeSize ?? '—'} B
												</td>
												<td
													className="py-1.5 pr-2 font-mono text-text-50/70"
													title={file.afterHash ?? ''}
												>
													{shortHash(file.afterHash)} · {file.afterSize ?? '—'} B
												</td>
												<td className="py-1.5 text-text-50/70">{file.author ?? '—'}</td>
											</tr>
										))}
									</tbody>
								</table>
							)}
						</div>
					)}
				</div>
			</div>

			<Dialog.Root
				open={pendingRollback !== null}
				onOpenChange={(open: boolean) => {
					if (!open) setPendingRollback(null);
				}}
			>
				<Dialog className="p-6">
					<Dialog.Title className="text-lg font-semibold">
						Roll back to {shortSha(pendingRollback?.commitSha)}?
					</Dialog.Title>
					<Dialog.Description className="mt-2 text-sm text-kumo-subtle">
						This creates a NEW revert commit that restores that revision&apos;s files.
						History is never rewritten, so rolling forward again stays possible.
					</Dialog.Description>
					<div className="mt-6 flex justify-end gap-2">
						<Button
							variant="secondary"
							onClick={() => setPendingRollback(null)}
							disabled={rollback.isPending}
						>
							Cancel
						</Button>
						<Button
							variant="destructive"
							onClick={runRollback}
							loading={rollback.isPending}
						>
							Roll back
						</Button>
					</div>
				</Dialog>
			</Dialog.Root>
		</div>
	);
}