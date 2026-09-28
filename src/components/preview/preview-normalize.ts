/**
 * Normalization pipeline that turns LLM-generated project files into a
 * layout Sandpack (Nodebox or static template) can actually run.
 *
 * Extracted from PreviewPanel so it can be unit-tested in isolation.
 */
import type {
	SandpackFiles,
	SandpackPredefinedTemplate,
} from '@codesandbox/sandpack-react';

/** Sandpack templates powered by Nodebox (SSR-capable). */
export const NODEBOX_TEMPLATES = ['nextjs', 'node', 'astro', 'vite'] as const;

export type PreviewTemplate = (typeof NODEBOX_TEMPLATES)[number];

/** True when the project is a pure static site: an index.html anywhere in
 *  root / public/ / src/, and NO root package.json (which would make it
 *  potentially Nodebox-runnable). Such projects render fastest and most
 *  reliably in a plain srcdoc iframe, bypassing Nodebox entirely (A2). */
export function isStaticProject(files: Record<string, string>): boolean {
	const keys = new Set(Object.keys(files).map((p) => (p.startsWith('/') ? p : `/${p}`)));
	if (!['/public/index.html', '/src/index.html', '/index.html'].some((p) => keys.has(p))) {
		return false;
	}
	return !keys.has('/package.json');
}

/**
 * Normalize to a Nodebox-backed Sandpack preset; unknown values fall back to
 * "node". Pure static projects (public/index.html + js/css, no root
 * package.json) map to 'static' so PreviewPanel renders them in a plain
 * srcdoc iframe — no Nodebox WASM boot, no bundler (A2).
 */
export function resolveTemplate(
	template: string,
	files: Record<string, string>,
): SandpackPredefinedTemplate {
	if (isStaticProject(files)) return 'static';
	const normalized = NODEBOX_TEMPLATES.includes(template as PreviewTemplate)
		? template
		: 'node';
	return normalized as SandpackPredefinedTemplate;
}

export type PreviewFilesResult = {
	files: SandpackFiles;
	template: SandpackPredefinedTemplate;
};

/** Paths that carry no runtime value for the preview and only confuse Nodebox. */
const IGNORED_FILES = ['generated-output.txt', '/generated-output.txt'];

/**
 * Minimal CommonJS static file server injected when the generated project has
 * no directly-runnable Node entry (e.g. its entry is TypeScript, or there is
 * no root package.json). Serves `public/` when present, else the project root.
 */
const STATIC_SERVER_CJS = `const http = require('http');
const fs = require('fs');
const path = require('path');

const ROOTS = ['public', '.'].map((r) => path.resolve(process.cwd(), r));
const MIME = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css',
  '.json': 'application/json', '.png': 'image/png', '.jpg': 'image/jpeg',
  '.svg': 'image/svg+xml', '.ico': 'image/x-icon', '.woff2': 'font/woff2',
};

http.createServer((req, res) => {
  const urlPath = decodeURIComponent((req.url || '/').split('?')[0]);
  let rel = urlPath === '/' ? '/index.html' : urlPath;
  for (const root of ROOTS) {
    const full = path.join(root, rel);
    if (fs.existsSync(full) && fs.statSync(full).isFile()) {
      res.writeHead(200, { 'Content-Type': MIME[path.extname(full)] || 'application/octet-stream' });
      fs.createReadStream(full).pipe(res);
      return;
    }
    // SPA fallback: try index.html in this root
    const idx = path.join(root, 'index.html');
    if (fs.existsSync(idx)) {
      res.writeHead(200, { 'Content-Type': MIME['.html'] });
      fs.createReadStream(idx).pipe(res);
      return;
    }
  }
  res.writeHead(404); res.end('Not found: ' + urlPath);
}).listen(3000, () => console.log('preview server on :3000'));
`;

export interface PkgJson {
	name?: string;
	main?: string;
	scripts?: Record<string, string>;
	dependencies?: Record<string, string>;
	[key: string]: unknown;
}

/**
 * Normalize generated project files into a layout Nodebox can actually run:
 *
 *  1. Sandpack-ify paths (leading "/") and drop noise files.
 *  2. Hoist a nested `package.json` (e.g. `src/package.json`) to the root —
 *     Nodebox only reads `/package.json`.
 *  3. If the project has no directly-runnable entry (no root package.json,
 *     no start script, or a TypeScript main), inject a minimal root
 *     package.json + static server that serves `public/` (or the root).
 *     A TS entry cannot run on Nodebox's plain Node runtime, so the static
 *     server at least renders the frontend faithfully.
 */
export function normalizeForPreview(
	files: Record<string, string>,
	baseTemplate: SandpackPredefinedTemplate,
): PreviewFilesResult {
	const merged: SandpackFiles = {};
	for (const [rawPath, contents] of Object.entries(files)) {
		const key = rawPath.startsWith('/') ? rawPath : `/${rawPath}`;
		if (IGNORED_FILES.includes(key)) continue;
		merged[key] = { code: contents };
	}
	if (Object.keys(merged).length === 0) return { files: merged, template: baseTemplate };

	// --- Hoist nested package.json to root -------------------------------
	let pkgPath: string | null = null;
	for (const candidate of ['/package.json', '/src/package.json']) {
		if (merged[candidate]) {
			pkgPath = candidate;
			break;
		}
	}
	// Any other nested package.json (e.g. /server/package.json).
	if (!pkgPath) {
		pkgPath =
			Object.keys(merged).find(
				(p) => p.endsWith('/package.json') && !p.includes('node_modules'),
			) ?? null;
	}

	let pkg: PkgJson | null = null;
	if (pkgPath) {
		try {
			const raw = merged[pkgPath];
			const text = typeof raw === 'string' ? raw : raw.code;
			pkg = JSON.parse(text) as PkgJson;
		} catch {
			pkg = null;
		}
		if (pkg) {
			// Sanitize whatever package.json we found (root OR nested) and write
			// the cleaned version to /package.json. Malformed LLM version
			// specifiers (e.g. "express": "^.") make Nodebox's CDN install fail
			// with InvalidPackageSpecifier, killing the whole preview.
			const safe = sanitizeDependencies(pkg);
			merged['/package.json'] = { code: JSON.stringify(safe, null, 2) };
			pkg = safe;
		}
	}

	// --- Decide whether the project is directly runnable -----------------
	// A start script that relies on ts-node/tsx/tsc or runs a .ts file is NOT
	// runnable: Nodebox's plain Node runtime can't execute TypeScript (and
	// malformed TS toolchain deps get sanitized away anyway).
	const startScript = pkg?.scripts?.start;
	const startNeedsTs =
		!!startScript && (/\b(ts-node|tsx|tsc)\b/.test(startScript) || /\.tsx?\b/.test(startScript));
	const mainEntry = pkg?.main ?? 'index.js';
	const entryPath = startScript ? null : resolveEntry(mainEntry, Object.keys(merged));
	const runnable =
		!!pkg &&
		!!merged['/package.json'] &&
		((!!startScript && !startNeedsTs) || (!startScript && !!entryPath && entryPath.endsWith('.js')));

	if (runnable) return { files: merged, template: baseTemplate };

	// --- Static-first fallback ------------------------------------------
	// If the project ships an `index.html` anywhere — /public, /src, or the
	// root — treat it as a pure frontend and render it directly in a plain
	// iframe (see buildStaticSrcDoc). This bypasses Sandpack/Nodebox entirely,
	// which is what keeps hitting parser errors like "Unknown character".
	const htmlDir =
		['/public/index.html', '/src/index.html', '/index.html'].find(
			(p) => merged[p],
		) ?? null;
	if (htmlDir) {
		const dir = htmlDir.replace('/index.html', ''); // '' | '/public' | '/src'
		const stripPrefix = (p: string): string =>
			dir && p.startsWith(`${dir}/`) ? p.slice(dir.length) : p;
		const staticFiles: SandpackFiles = {};
		for (const [p, file] of Object.entries(merged)) {
			if (dir === '' ? p.startsWith('/index.html') : p.startsWith(`${dir}/`)) {
				staticFiles[stripPrefix(p)] = file;
			} else if (
				// Stray assets written OUTSIDE the entry dir (e.g. root-level
				// "/js/main.js" from a backend gap-fill pass). Include them so
				// buildStaticSrcDoc's resolve() can still inline them.
				/^\/(js|css|assets|img|images|fonts)(\/|$)/i.test(p)
			) {
				staticFiles[p] = file;
			}
		}
		if (Object.keys(staticFiles).length > 0) {
			return { files: staticFiles, template: 'static' };
		}
	}

	// --- Fallback: inject a static server for public/ (or root) ----------
	// NOTE: original dependencies are intentionally DROPPED here. The static
	// server uses only Node built-ins, and LLM-generated dependency lists
	// often contain malformed version specifiers (e.g. "express": "^.")
	// which make Nodebox's CDN install fail with InvalidPackageSpecifier —
	// killing the whole preview even though the server needs none of them.
	const injectedPkg: PkgJson = {
		name: (pkg?.name as string) ?? 'preview',
		version: '1.0.0',
		scripts: { start: 'node .vibesdk-preview/server.cjs' },
	};
	merged['/package.json'] = { code: JSON.stringify(injectedPkg, null, 2) };
	merged['/.vibesdk-preview/server.cjs'] = { code: STATIC_SERVER_CJS };
	return { files: merged, template: baseTemplate };
}

/** Version specifiers Nodebox's CDN can resolve (npm semver range or *). */
const VALID_SPECIFIER = /^(latest|\*|[~^><=]{0,2}\d+(\.\d+){0,2}([-+][\w.-]+)?)$/;

/**
 * Remove dependencies whose version specifier is malformed — LLM-generated
 * package.json files sometimes contain entries like "express": "^." which
 * make the Sandpack CDN fail the entire dependency install.
 */
export function sanitizeDependencies(pkg: PkgJson): PkgJson {
	const out: PkgJson = { ...pkg };
	for (const field of ['dependencies', 'devDependencies'] as const) {
		const deps = pkg[field];
		if (!deps) continue;
		const cleaned: Record<string, string> = {};
		for (const [name, spec] of Object.entries(deps)) {
			if (VALID_SPECIFIER.test(spec.trim())) cleaned[name] = spec;
		}
		out[field] = cleaned;
	}
	return out;
}

/** Resolve package.json `main` to an existing file, trying extensions. */
function resolveEntry(main: string, paths: string[]): string | null {
	const base = main.startsWith('/') ? main : `/${main}`;
	const candidates = [
		base,
		`${base}.js`,
		`${base}.cjs`,
		`${base}.mjs`,
		`${base}.ts`,
		`${base}/index.js`,
		`${base}/index.ts`,
	];
	return candidates.find((c) => paths.includes(c)) ?? null;
}

/**
 * Repair common LLM-induced JavaScript syntax errors so that one corrupt file
 * does not kill the whole preview. Works on string level (best-effort) and
 * only touches the well-known failure modes:
 *
 *   - empty value after a key:        `rating: ,`   / `hotelId:,`
 *   - empty string-keyed value:        `'x': ,`
 *   - truncated decimal:               `rating: 4.` -> `rating: 4`
 *   - dangling comma before a value:   `id: ,`      -> `id: null,`
 *
 * These come from small LLMs (e.g. llama-3.3 / qwen) that occasionally emit
 * half-formed literals, and they make `data.js` — the first loaded script —
 * fail to parse, which blanks out every subsequent script in the bundle.
 */
export function sanitizeJs(code: string): string {
	let out = code;
	// 1. `key: ,` or `key: ,\n` -> `key: null,`  (identifier keys)
	out = out.replace(/([A-Za-z_$][\w$]*)\s*:\s*,/g, '$1: null,');
	// 2. `'key': ,` / `"key": ,` -> `'key': null,`
	out = out.replace(/(["'][^"']*["'])\s*:\s*,/g, '$1: null,');
	// 3. dot with no digits (not caught by #1/#2): `price: . }` -> `price: null }`
	out = out.replace(
		/([A-Za-z_$][\w$]*)\s*:\s*\.(?=\s*[,}\]])/g,
		'$1: null',
	);
	// 4. truncated decimal before `,` `}` `]` or whitespace+end: `4.` -> `4`
	out = out.replace(/(\d+)\.(?=\s*[,}\])])/g, '$1');
	return out;
}

/**
 * Build a fully self-contained HTML document from the (hoisted) static file
 * set: inline every <link rel=stylesheet> and <script src> so the preview can
 * run inside a plain sandboxed <iframe srcdoc>, with zero network requests.
 *
 * This bypasses Sandpack entirely for static projects — immune to its CDN
 * outages and transpiler quirks (e.g. "Unknown character" errors).
 */
export function buildStaticSrcDoc(files: SandpackFiles): string {
	const getText = (p: string): string | null => {
		const raw = files[p];
		if (!raw) return null;
		return typeof raw === 'string' ? raw : raw.code;
	};

	const resolve = (href: string): string | null => {
		const clean = href.replace(/^\.\//, '').split('?')[0].split('#')[0];
		const candidates = [
			clean.startsWith('/') ? clean : `/${clean}`,
			`/public${clean.startsWith('/') ? clean : `/${clean}`}`,
		];
		for (const c of candidates) {
			const t = getText(c);
			if (t !== null) return t;
		}
		return null;
	};

	const html = getText('/index.html') ?? '';
	let out = html;

	// Inline stylesheets.
	out = out.replace(
		/<link\b[^>]*rel=["']stylesheet["'][^>]*>/gi,
		(tag: string) => {
			const m = tag.match(/href=["']([^"']+)["']/i);
			if (!m) return tag;
			const css = resolve(m[1]);
			return css === null ? tag : `<style>\n${css}\n</style>`;
		},
	);

	// Inline scripts (keep type="module" semantics as-is). Each script is
	// sanitized (repair LLM JS syntax errors) and wrapped in its own try/catch
	// so a runtime failure in one file (e.g. data.js) doesn't blank out the rest.
	out = out.replace(
		/<script\b([^>]*)\bsrc=["']([^"']+)["']([^>]*)><\/script>/gi,
		(_tag: string, pre: string, src: string, post: string) => {
			const raw = resolve(src);
			if (raw === null) return _tag;
			const js = sanitizeJs(raw);
			const scriptOpen = `<script${pre}${post}>`;
			return `${scriptOpen}\n(function () {\n\ttry {\n${js}\n\t} catch (_err) {\n\t\tconsole.error("[preview] script failed:", ${JSON.stringify(src)}, _err);\n\t}\n})();\n</script>`;
		},
	);

	// Suppress fetches the static preview cannot serve (no backend here).
	const guard = `<script>
(function () {
	const realFetch = window.fetch.bind(window);
	window.fetch = function (input, init) {
		const url = typeof input === 'string' ? input : (input && input.url) || '';
		if (/^\\/(api|graphql)\\//.test(url) || url.startsWith(window.location.origin + '/api/')) {
			return Promise.resolve(new Response('[]', { status: 200, headers: { 'Content-Type': 'application/json' } }));
		}
		return realFetch(input, init);
	};
	window.addEventListener('unhandledrejection', (e) => e.preventDefault());
})();
</script>`;
	if (out.includes('</head>')) {
		out = out.replace('</head>', `${guard}\n</head>`);
	} else {
		out = guard + out;
	}
	return out;
}
