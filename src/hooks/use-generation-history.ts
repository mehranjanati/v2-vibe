import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api-client';
import { queryKeys } from '@/lib/query-keys';
import type {
	GenerationDiffData,
	GenerationPullRequestData,
	GenerationRollbackData,
	GenerationSummary,
} from '@/api-types';

/**
 * P1.4/P1.5 — generation history of one chat, newest first.
 *
 * Server state via TanStack Query: the list is what the History tab renders and
 * what a rollback invalidates, so the cache key carries the chat id (a user
 * switching chats must never see another project's revisions).
 */
export function useGenerationHistory(chatId?: string | null) {
	return useQuery({
		queryKey: queryKeys.generations.history(chatId ?? 'unknown'),
		queryFn: async (): Promise<GenerationSummary[]> => {
			const res = await apiClient.getGenerationHistory(chatId ?? '');
			if (!res.success || !res.data) {
				throw new Error(res.error?.message || 'Failed to load generation history');
			}
			return res.data.generations;
		},
		enabled: Boolean(chatId),
		// The list grows only when a generation finishes, so a short staleness
		// window is enough; rollback invalidates it explicitly.
		staleTime: 15_000,
		retry: false,
	});
}

/**
 * P1.4 — the recorded diff of one generation (op + hashes + sizes, never file
 * contents). Disabled until a generation is selected.
 */
export function useGenerationDiff(
	chatId?: string | null,
	generationId?: string | null,
) {
	return useQuery({
		queryKey: queryKeys.generations.diff(
			chatId ?? 'unknown',
			generationId ?? 'unknown',
		),
		queryFn: async (): Promise<GenerationDiffData> => {
			const res = await apiClient.getGenerationDiff(
				chatId ?? '',
				generationId ?? '',
			);
			if (!res.success || !res.data) {
				throw new Error(res.error?.message || 'Failed to load the generation diff');
			}
			return res.data;
		},
		enabled: Boolean(chatId && generationId),
		// A generation's diff is immutable once the run closed.
		staleTime: 5 * 60_000,
		retry: false,
	});
}

/**
 * P1.4.6 — open (or reuse) a pull request for the newest succeeded generation.
 *
 * The Edge mirrors the tree onto `gen/<short>` and opens the PR against the
 * app's base branch; the head branch comes from lineage, not from the browser.
 * A repeated click returns the existing open PR (`reused`), so this mutation
 * has no invalidation side effects — nothing local changes.
 */
export function useCreatePullRequest(chatId?: string | null) {
	return useMutation({
		mutationFn: async (options: { title?: string; notes?: string } = {}): Promise<GenerationPullRequestData> => {
			const res = await apiClient.pushPullRequest({
				agentId: chatId ?? '',
				...(options.title ? { title: options.title } : {}),
				...(options.notes ? { notes: options.notes } : {}),
			});
			if (!res.success || !res.data) {
				throw new Error(res.error?.message || 'Failed to open the pull request');
			}
			return res.data;
		},
	});
}

/**
 * P1.4 — roll a generation back. The server writes a NEW revert commit, so the
 * history list changes (and the VFS snapshot may have been restored), which is
 * why both caches are invalidated on success.
 */
export function useRollbackGeneration(chatId?: string | null) {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (generationId: string): Promise<GenerationRollbackData> => {
			const res = await apiClient.rollbackGeneration(chatId ?? '', generationId);
			if (!res.success || !res.data) {
				throw new Error(res.error?.message || 'Rollback failed');
			}
			return res.data;
		},
		onSuccess: async () => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.generations.all });
			// The working set may have been restored server-side, so the preview
			// snapshot is stale too.
			await queryClient.invalidateQueries({
				queryKey: queryKeys.projects.files(chatId ?? 'unknown'),
			});
		},
	});
}
