/**
 * V2 vibe — Lightweight Worker entry point.
 *
 * Serves the frontend (via ASSETS binding) and a minimal set of API routes:
 *   - /api/capabilities, /api/status — platform info
 *   - /api/auth/* (csrf-token, register, login, logout, profile,
 *     providers, sessions, api-keys, identities) — auth via D1
 *   - /api/auth/github/* — GitHub OAuth (when credentials are configured)
 *   - /api/github-app/* — GitHub export (create repo, push files, deploy)
 *   - /api/health — health check
 *
 * This worker intentionally avoids heavy dependencies (CodeGen DO,
 * ThinkAgent, SpaceDO, containers, sandbox, dispatch) so it fits within
 * the free-tier 3 MiB script size limit.
 *
 * Architecture: extends the shared {@link Worker} base class. This is the
 * only worker in the project, keeping the script within the free-tier
 * 3 MiB size limit.
 */
import { Worker } from './core/Worker';
import { buildLightApp } from './light/lightApp';
import type { LightWorkerBindings } from './types/bindings';

/**
 * Light Worker — auth (email/password, GitHub OAuth) + GitHub export.
 *
 * Extends the shared {@link Worker} base with the minimal binding set that
 * the free-tier edge worker actually binds (D1, KV, R2, AI, ASSETS).
 */
export class LightWorker extends Worker<LightWorkerBindings> {
	private readonly app: ReturnType<typeof buildLightApp>;

	constructor(options: ConstructorParameters<typeof Worker<LightWorkerBindings>>[0]) {
		super(options);
		this.app = buildLightApp();
	}

	/** The Cloudflare `fetch` entry point. */
	override async fetch(request: Request): Promise<Response> {
		return this.app.fetch(request, this.env, this.ctx);
	}

	/** Handle an incoming request (delegates to the Hono app). */
	protected override async handleFetch(request: Request): Promise<Response> {
		return this.fetch(request);
	}
}

// Cloudflare Worker entry point.
export default {
	async fetch(request: Request, env: LightWorkerBindings, ctx: ExecutionContext): Promise<Response> {
		return new LightWorker({ env, ctx }).fetch(request);
	},
};