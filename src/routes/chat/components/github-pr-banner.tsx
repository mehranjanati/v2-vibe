import { useState } from 'react';
import { Button } from '@cloudflare/kumo';
import { GitPullRequest, X } from '@phosphor-icons/react';
import { useCreatePullRequest } from '@/hooks/use-generation-history';

interface GitHubPullRequestBannerProps {
	chatId: string;
	/** Hides the banner until the next completed run. */
	onDismiss: () => void;
}

/**
 * P1.4.6 — post-run "push / create pull request" banner.
 *
 * Appears once a run completes (`generation_complete` / `team_completed`) and
 * offers the single action that turns the finished generation into a reviewable
 * GitHub change: the Edge mirrors the tree onto `gen/<id>` as one atomic commit
 * and opens (or reuses) a PR against the app's base branch. The head branch is
 * derived from lineage server-side, so the banner never names a branch.
 *
 * Failure is shown inline, not hidden: an app that was never exported answers
 * `409 NOT_EXPORTED`, and the message says to export first.
 */
export function GitHubPullRequestBanner({ chatId, onDismiss }: GitHubPullRequestBannerProps) {
	const createPr = useCreatePullRequest(chatId);
	const [message, setMessage] = useState<{ tone: 'success' | 'error'; text: string } | null>(null);

	const create = () =>
		createPr.mutate({}, {
			onSuccess: (data) => {
				setMessage({
					tone: 'success',
					text: data.reused
						? `Pull request #${data.prNumber ?? '—'} is already open (${data.head} → ${data.base}).`
						: `Opened pull request #${data.prNumber ?? '—'} (${data.head} → ${data.base}, ${data.files} file(s)).`,
				});
				if (data.prUrl) {
					window.open(data.prUrl, '_blank', 'noopener,noreferrer');
				}
			},
			onError: (error) => {
				setMessage({
					tone: 'error',
					text: error instanceof Error ? error.message : 'Failed to open the pull request',
				});
			},
		});

	return (
		<div className="mx-4 mb-3 flex items-start gap-3 rounded-md border border-border bg-bg-1 px-3 py-2">
			<GitPullRequest className="mt-0.5 size-4 shrink-0 text-text-50/70" />
			<div className="min-w-0 flex-1">
				<p className="text-sm text-text-primary">Run complete — propose it for review</p>
				<p className="mt-0.5 text-xs text-text-50/70">
					Mirrors this generation onto <span className="font-mono">gen/&lt;id&gt;</span> as one
					atomic commit and opens a pull request against the base branch. Exporting the app
					first is required; clicking twice reuses the open PR.
				</p>
				{message && (
					<p
						className={
							message.tone === 'success'
								? 'mt-1.5 text-xs text-emerald-500'
								: 'mt-1.5 text-xs text-red-500'
						}
					>
						{message.text}
					</p>
				)}
			</div>
			<Button variant="secondary" size="sm" onClick={create} loading={createPr.isPending}>
				Create pull request
			</Button>
			<button
				type="button"
				aria-label="Dismiss"
				onClick={onDismiss}
				className="p-1 rounded hover:bg-kumo-elevated transition-colors"
			>
				<X className="size-4 text-text-primary/60" />
			</button>
		</div>
	);
}
