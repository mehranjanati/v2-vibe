import { useMemo } from 'react';
import {
	SandpackProvider,
	SandpackLayout,
	SandpackPreview,
} from '@codesandbox/sandpack-react';
import { buildStaticSrcDoc, normalizeForPreview, resolveTemplate, type PreviewTemplate } from './preview-normalize';
import { useDebouncedValue } from '@/hooks/use-debounced-value';

/**
 * PreviewPanel — client-side preview built on Sandpack + Nodebox.
 *
 * Nodebox templates ("nextjs", "node", "astro", etc.) run a full Node.js
 * runtime in the browser via WebAssembly, which enables SSR previews
 * (Next.js, Astro, full-stack apps) without a Cloudflare Worker sandbox.
 *
 * The `files` prop is the WebSocket-accumulated Virtual File System
 * (`Record<string, string>`) maintained by `handle-websocket-message.ts`.
 * Sandpack hot-reloads whenever a file's contents change, so streamed
 * `file_chunk_generated` chunks reflect in the preview as they arrive
 * (recompile is debounced via `recompileMode: 'delayed'`).
 */


export interface PreviewPanelProps {
	/** Virtual File System: path -> contents, accumulated from WebSocket events. */
	files: Record<string, string>;
	/**
	 * Nodebox template to run. "nextjs" (SSR) and "node" (generic Node) are
	 * the primary targets; falls back to "node" for unknown values.
	 */
	template?: PreviewTemplate | string;
	/** Debounce (ms) applied before Sandpack recompiles on file changes. */
	recompileDelay?: number;
	className?: string;
}

export function PreviewPanel({
	files,
	template = 'node',
	recompileDelay = 400,
	className,
}: PreviewPanelProps) {
	const { files: sandpackFiles, template: resolvedTemplate } = useMemo(() => {
		return normalizeForPreview(files, resolveTemplate(template, files));
	}, [files, template]);

	const hasFiles = Object.keys(sandpackFiles).length > 0;
	// Static projects render in a plain sandboxed iframe (srcdoc): no Sandpack
	// bundler, no CDN, no Nodebox — the most reliable path for pure frontends.
	// Rebuilding the self-contained doc is O(total file size), so while file
	// chunks stream in we debounce the rebuild (mirrors `recompileDelay`; the
	// Sandpack path is unaffected — it debounces internally).
	const debouncedFiles = useDebouncedValue(
		sandpackFiles,
		resolvedTemplate === 'static' ? recompileDelay : 0,
	);
	const staticSrcDoc = useMemo(
		() => (resolvedTemplate === 'static' ? buildStaticSrcDoc(debouncedFiles) : ''),
		[debouncedFiles, resolvedTemplate],
	);

	if (!hasFiles) {
		return (
			<div
				className={
					className ??
					'flex h-full w-full items-center justify-center rounded-lg border border-border bg-muted/40 text-sm text-muted-foreground'
				}
			>
				Waiting for generated files…
			</div>
		);
	}

	if (resolvedTemplate === 'static') {
		return (
			<div className={className ?? 'h-full w-full overflow-hidden rounded-lg border border-border'}>
				<iframe
					title="Preview"
					srcDoc={staticSrcDoc}
					sandbox="allow-scripts allow-forms allow-modals"
					className="h-full w-full border-0 bg-white"
				/>
			</div>
		);
	}

	return (
		<div className={className ?? 'h-full w-full overflow-hidden rounded-lg border border-border'}>
			<SandpackProvider
				template={resolvedTemplate}
				files={sandpackFiles}
				options={{
					activeFile: sandpackFiles['/index.html'] ? '/index.html' : Object.keys(sandpackFiles)[0],
					recompileMode: 'delayed',
					recompileDelay,
					autorun: true,
				}}
				theme="dark"
			>
				<SandpackLayout style={{ height: '100%' }}>
					<SandpackPreview showNavigator={false} showOpenInCodeSandbox={false} showRefreshButton />
				</SandpackLayout>
			</SandpackProvider>
		</div>
	);
}

export default PreviewPanel;
