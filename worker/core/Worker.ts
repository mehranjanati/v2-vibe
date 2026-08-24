/**
 * Base Worker class.
 *
 * Provides a shared class-based entry point for the light auth/GitHub-export
 * worker (`worker/light-index.ts`). Subclasses override `handleFetch` with
 * their domain-specific routing. This keeps per-worker logic in its own file
 * while sharing the Cloudflare Worker lifecycle (`fetch` -> `handleFetch`).
 */

/**
 * Options that any Worker subclass may receive in its constructor.
 *
 * `Bindings` is generic so the light worker can pass `LightWorkerBindings`
 * without collapsing it into one monolithic type.
 */
export interface WorkerOptions<Bindings> {
	/** Cloudflare bindings injected by the runtime. */
	env: Bindings;
	/** Event context (waitUntil, passThroughOnException). */
	ctx: ExecutionContext;
}

/**
 * Abstract base class for a Cloudflare Worker.
 */
export abstract class Worker<Bindings = unknown> {
	protected readonly env: Bindings;
	protected readonly ctx: ExecutionContext;

	constructor(options: WorkerOptions<Bindings>) {
		this.env = options.env;
		this.ctx = options.ctx;
	}

	/**
	 * The Cloudflare `fetch` entry point. Subclasses implement the actual
	 * request handling in {@link handleFetch}.
	 */
	async fetch(request: Request): Promise<Response> {
		return this.handleFetch(request);
	}

	/**
	 * Handle an incoming request. Subclasses override this to wire up their
	 * Hono routing / domain logic.
	 */
	protected abstract handleFetch(request: Request): Promise<Response>;
}

/**
 * Convenience factory: instantiate a Worker subclass from a runtime `fetch`
 * call. Wraps the constructor so subclass entry points stay declarative:
 *
 * ```ts
 * export default {
 *   fetch(request, env, ctx) {
 *     return run(new MyWorker({ env, ctx }), request);
 *   },
 *   // Optional: export DO classes on the same module.
 * }
 * ```
 */
export function run<B>(
	worker: Worker<B>,
	request: Request,
): Promise<Response> {
	return worker.fetch(request);
}