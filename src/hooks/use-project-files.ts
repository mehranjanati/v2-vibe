import { useQuery } from '@tanstack/react-query';
import { getProjectFiles } from '@/services/controlPlaneClient';
import { queryKeys } from '@/lib/query-keys';

/**
 * Hydrate a project's VFS from the Go control plane
 * (`GET /api/projects/:id/files`) so the client-side preview has content
 * when reopening a chat before the WebSocket state replay arrives.
 *
 * The endpoint is a server-side snapshot: live WebSocket updates always
 * take precedence, so consumers must merge this snapshot with the LOWEST
 * precedence (see MainContentPanel's preview file merge).
 */
export function useProjectFiles(projectId?: string | null) {
	return useQuery({
		queryKey: queryKeys.projects.files(projectId ?? 'unknown'),
		queryFn: async () => {
			const res = await getProjectFiles(projectId ?? '');
			if (!res.success || !res.data) {
				throw new Error(res.error || 'Failed to load project files');
			}
			return res.data.files;
		},
		enabled: Boolean(projectId),
		// The endpoint is a one-shot snapshot; VFS changes arrive over the
		// room WebSocket, so this query never refetches on its own.
		staleTime: Infinity,
		gcTime: 10 * 60_000,
		retry: false,
	});
}