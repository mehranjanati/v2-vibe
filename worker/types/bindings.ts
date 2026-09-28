/**
 * Worker Bindings Model
 *
 * The light worker is the only worker in this project. It binds a minimal
 * set of Cloudflare resources (D1, KV, R2, AI, ASSETS, Workflows,
 * control-plane URL).
 *
 * This module models:
 *   - `BaseBindings`        : shared core bindings the worker needs.
 *   - `LightWorkerBindings` : the minimal set the auth/GitHub-export worker
 *                              binds (D1, KV, R2, AI, ASSETS, control-plane URL).
 */

/** Shared core bindings present in the worker (auth + GitHub export + platform). */
export interface BaseBindings {
	// Core storage + runtime
	DB: D1Database;
	VibecoderStore: KVNamespace;
	// Optional: R2 is not enabled on all accounts (error 10042); the light
	// worker never accesses it at runtime, so the binding may be absent.
	TEMPLATES_BUCKET?: R2Bucket;
	ASSETS: Fetcher;
	AI: Ai;
	/**
	 * Workflows orchestration binding — used to create/inspect DAG instances.
	 * The bound workflow is backed by the {@link VibeWorkflow} entrypoint class
	 * (exported from `worker/light-index.ts`), which interprets DAG schema v2
	 * documents persisted in D1 (`worker/workflow/VibeWorkflow.ts`).
	 */
	WORKFLOWS: Workflow;
}

/**
 * Light Worker bindings — the minimal edge worker that serves the
 * SPA plus auth (email/password, GitHub OAuth) and GitHub export flows.
 * It intentionally has NO Durable Objects, dispatcher, containers, or
 * browser bindings.
 */
export interface LightWorkerBindings extends BaseBindings {
	// Auth / session secrets (optional — the light worker may run without them)
	JWT_SECRET?: string;
	GITHUB_EXPORTER_CLIENT_ID?: string;
	GITHUB_EXPORTER_CLIENT_SECRET?: string;
	CUSTOM_DOMAIN?: string;
	/** Base URL of the Go control plane (fetches generated files for GitHub export). */
	CONTROL_PLANE_URL?: string;
}

