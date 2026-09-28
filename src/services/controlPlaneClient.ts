/**
 * Control Plane Client.
 *
 * Talks to the Go Fiber backend (Coolify/Hetzner): auth, project rooms,
 * VFS state, WebSocket management, and LLM generation triggers.
 *
 * This is an additive module — it does not replace the existing
 * `src/lib/api-client.ts`; it provides a decoupled surface for the
 * control plane so the frontend can be pointed at the Go backend via
 * `VITE_CONTROL_PLANE_URL`.
 */

import { controlPlane, wsControlPlaneUrl } from '@/config/api';

/** Generic JSON response envelope from the Go backend. */
export interface ControlPlaneResponse<T = unknown> {
	success: boolean;
	data?: T;
	error?: string;
}

/** Session creation result returned by POST /api/agent/session. */
export interface AgentSession {
	websocketUrl: string;
	agentId: string;
	behaviorType: string;
	projectType: string;
}

/** Connection result returned by GET /api/agent/:id/connect. */
export interface AgentConnection {
	success: boolean;
	websocketUrl: string;
	agentId: string;
}

/** Deployment trigger result returned by POST /api/projects/:id/deploy. */
export interface DeployResult {
	success: boolean;
	message: string;
	projectId: string;
}

/** VFS payload returned by GET /api/projects/:id/files. */
export interface ProjectFilesData {
	files: Record<string, string>;
}

async function request<T>(
	path: string,
	init?: RequestInit,
): Promise<ControlPlaneResponse<T>> {
	const res = await fetch(`${controlPlane.baseUrl}${path}`, {
		...init,
		headers: {
			'Content-Type': 'application/json',
			...(init?.headers ?? {}),
		},
	});
	if (!res.ok) {
		return { success: false, error: `HTTP ${res.status}` };
	}
	const json = (await res.json()) as ControlPlaneResponse<T>;
	return json;
}

/** Create a new agent session (returns the WebSocket URL to connect to). */
export async function createAgentSession(
	query: string,
	projectType = 'app',
): Promise<ControlPlaneResponse<AgentSession>> {
	return request<AgentSession>('/api/agent/session', {
		method: 'POST',
		body: JSON.stringify({ query, projectType }),
	});
}

/** Connect to an existing agent session. */
export async function connectToAgent(
	agentId: string,
): Promise<ControlPlaneResponse<AgentConnection>> {
	return request<AgentConnection>(`/api/agent/${agentId}/connect`);
}

/** Trigger a Cloudflare Pages deployment of a project's VFS. */
export async function deployProject(
	projectId: string,
): Promise<ControlPlaneResponse<DeployResult>> {
	return request<DeployResult>(`/api/projects/${projectId}/deploy`, {
		method: 'POST',
	});
}

/**
 * Fetch a project's VFS as a flat path -> contents map. Used by the chat
 * route to hydrate the client-side preview (Sandpack / static srcdoc) when
 * reopening a chat, before the WebSocket state replay arrives. Read-only
 * on the server: it never spawns a room actor.
 */
export async function getProjectFiles(
	projectId: string,
): Promise<ControlPlaneResponse<ProjectFilesData>> {
	return request<ProjectFilesData>(`/api/projects/${projectId}/files`);
}

/** Health check against the control plane. */
export async function healthCheck(): Promise<boolean> {
	try {
		const res = await fetch(`${controlPlane.baseUrl}/health`);
		return res.ok;
	} catch {
		return false;
	}
}

/**
 * Open a WebSocket to the control plane for a given agent/room.
 * Returns a native WebSocket (or partysocket-compatible) connection.
 */
export function openControlPlaneSocket(agentId: string): WebSocket {
	return new WebSocket(`${wsControlPlaneUrl}/ws/${agentId}`);
}

/** Convenience accessor for the control-plane base URL. */
export const controlPlaneBaseUrl = controlPlane.baseUrl;