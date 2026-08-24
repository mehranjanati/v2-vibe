/**
 * Execution Plane Client.
 *
 * Talks directly to the Cloudflare Edge ecosystem (Workers / Workflows /
 * Vectorize) for end-user agent execution. This is separate from the
 * control plane (Go backend) so the two planes can be deployed and scaled
 * independently.
 *
 * All calls are additive and gated on `executionPlane.isConfigured`.
 */

import { executionPlane } from '@/config/api';

/** Generic response envelope from a Cloudflare Worker. */
export interface ExecutionPlaneResponse<T = unknown> {
	ok: boolean;
	data?: T;
	error?: string;
}

/** A Cloudflare Workflow instance status. */
export interface WorkflowStatus {
	instanceId: string;
	status: 'queued' | 'running' | 'paused' | 'errored' | 'terminated' | 'complete';
	output?: unknown;
	error?: string;
}

/** A Vectorize query result. */
export interface VectorizeMatch {
	id: string;
	score: number;
	metadata?: Record<string, unknown>;
}

async function request<T>(
	path: string,
	init?: RequestInit,
): Promise<ExecutionPlaneResponse<T>> {
	if (!executionPlane.isConfigured) {
		return { ok: false, error: 'Execution plane is not configured (set VITE_EXECUTION_PLANE_URL)' };
	}
	try {
		const res = await fetch(`${executionPlane.baseUrl}${path}`, {
			...init,
			headers: {
				'Content-Type': 'application/json',
				...(init?.headers ?? {}),
			},
		});
		if (!res.ok) {
			return { ok: false, error: `HTTP ${res.status}` };
		}
		return (await res.json()) as ExecutionPlaneResponse<T>;
	} catch (err) {
		return { ok: false, error: err instanceof Error ? err.message : String(err) };
	}
}

/** Invoke a Cloudflare Worker endpoint directly. */
export async function invokeWorker<T>(
	workerPath: string,
	body?: unknown,
): Promise<ExecutionPlaneResponse<T>> {
	return request<T>(workerPath, {
		method: 'POST',
		body: body === undefined ? undefined : JSON.stringify(body),
	});
}

/** Trigger a Cloudflare Workflow instance. */
export async function triggerWorkflow<T>(
	workflowName: string,
	input?: unknown,
): Promise<ExecutionPlaneResponse<T>> {
	return request<T>(`/workflows/${workflowName}/instances`, {
		method: 'POST',
		body: input === undefined ? undefined : JSON.stringify(input),
	});
}

/** Fetch the status of a Cloudflare Workflow instance. */
export async function getWorkflowStatus(
	workflowName: string,
	instanceId: string,
): Promise<ExecutionPlaneResponse<WorkflowStatus>> {
	return request<WorkflowStatus>(`/workflows/${workflowName}/instances/${instanceId}`);
}

/** Query a Cloudflare Vectorize index for similar vectors. */
export async function queryVectorize(
	indexName: string,
	vector: number[],
	topK = 5,
): Promise<ExecutionPlaneResponse<{ matches: VectorizeMatch[] }>> {
	return request<{ matches: VectorizeMatch[] }>(`/vectorize/${indexName}/query`, {
		method: 'POST',
		body: JSON.stringify({ vector, topK }),
	});
}

/** Convenience accessor for the execution-plane base URL. */
export const executionPlaneBaseUrl = executionPlane.baseUrl;