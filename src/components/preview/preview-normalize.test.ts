import { describe, expect, it } from 'vitest';
import {
	normalizeForPreview,
	resolveTemplate,
	isStaticProject,
	sanitizeDependencies,
} from './preview-normalize';

/**
 * Real-world regression fixtures.
 *
 * The store fixture mirrors the EXACT files the Go backend generated for the
 * "online store" prompt (pulled from the Redis VFS): a TypeScript/Express/
 * Mongoose server that can never run in Nodebox, plus a static frontend in
 * `public/` and the raw LLM transcript `generated-output.txt`.
 */
const STORE_PROJECT: Record<string, string> = {
	'generated-output.txt': '+399 lines of raw LLM transcript…',
	'src/index.ts':
		"import express from 'express';\nimport mongoose from 'mongoose';\nmongoose.connect('mongodb://localhost/ecommerce');\n",
	'src/package.json': JSON.stringify({
		name: 'simple-web-app',
		version: '1.0.0',
		scripts: { start: 'ts-node src/index.ts' },
		dependencies: { express: '^4.17.1' },
		devDependencies: {
			'@types/express': '^4.',
			'@types/node': '^17.0.',
			'ts-node': '^10..1',
			typescript: '^4.9.4',
		},
	}),
	'src/tsconfig.json': '{"compilerOptions":{"target":"es6"}}',
	'src/routes/products.ts': 'export const r = 1;',
	'public/index.html':
		'<!doctype html><html><head><link rel="stylesheet" href="styles.css"></head><body><h1>Store</h1><script src="script.js"></script></body></html>',
	'public/script.js': "fetch('/api/products').then(r => r.json());",
	'public/styles.css': 'body { margin: 0; }',
};

describe('resolveTemplate static detection (A2)', () => {
	it('maps a pure static project (public/ + no root package.json) to "static"', () => {
		const files = {
			'public/index.html': '<html></html>',
			'public/styles.css': 'body {}',
			'public/js/main.js': "console.log('hi');",
		};
		expect(isStaticProject(files)).toBe(true);
		expect(resolveTemplate('node', files)).toBe('static');
		expect(resolveTemplate('nextjs', files)).toBe('static');
	});

	it('does not map runnable projects (root package.json) to "static"', () => {
		const files = {
			'package.json': JSON.stringify({ scripts: { start: 'node server.js' } }),
			'server.js': "require('http');",
			'public/index.html': '<html></html>',
		};
		expect(isStaticProject(files)).toBe(false);
		expect(resolveTemplate('node', files)).toBe('node');
	});

	it('falls back to "node" for unknown templates on non-static projects', () => {
		expect(resolveTemplate('vanilla', { 'src/index.ts': 'x' })).toBe('node');
		expect(resolveTemplate('node', {})).toBe('node');
	});
});

describe('normalizeForPreview', () => {
	it('TS server + public/ frontend → static template with hoisted public files', () => {
		const { files, template } = normalizeForPreview(STORE_PROJECT, 'node');

		expect(template).toBe('static');

		// Frontend hoisted to root
		expect(files['/index.html']).toBeDefined();
		expect(files['/script.js']).toBeDefined();
		expect(files['/styles.css']).toBeDefined();
		const idx = files['/index.html'];
		const idxHtml = typeof idx === 'string' ? idx : idx.code;
		expect(idxHtml).toContain('<h1>Store</h1>');

		// No server/TS files, no nested package.json, no LLM transcript
		expect(files['/src/index.ts']).toBeUndefined();
		expect(files['/package.json']).toBeUndefined();
		expect(files['/src/package.json']).toBeUndefined();
		expect(Object.keys(files).some((p) => p.includes('generated-output'))).toBe(false);
	});

	it('never hands malformed version specifiers to Sandpack', () => {
		const { files, template } = normalizeForPreview(STORE_PROJECT, 'node');
		const serialized = JSON.stringify(files);
		for (const bad of ['^4."', '^17.0."', '^10..1', '^.1']) {
			expect(serialized).not.toContain(bad);
		}
		expect(template).toBe('static');
	});

	it('non-runnable project without public/ → injects dependency-free static server', () => {
		const filesIn: Record<string, string> = {
			'src/index.ts': "console.log('hi');",
			'src/package.json': JSON.stringify({
				name: 'x',
				scripts: { start: 'ts-node src/index.ts' },
				dependencies: { express: '^.' },
			}),
		};
		const { files, template } = normalizeForPreview(filesIn, 'node');

		expect(template).toBe('node');
		const pkg = JSON.parse((files['/package.json'] as { code: string }).code);
		expect(pkg.scripts.start).toBe('node .vibesdk-preview/server.cjs');
		// malformed deps dropped entirely
		expect(pkg.dependencies).toBeUndefined();
		expect(files['/.vibesdk-preview/server.cjs']).toBeDefined();
	});

	it('runnable JS project passes through with its own start script and valid deps', () => {
		const filesIn: Record<string, string> = {
			'package.json': JSON.stringify({
				name: 'api',
				scripts: { start: 'node server.js' },
				dependencies: { express: '^4.17.1', broken: '^.' },
			}),
			'server.js': "require('express')();",
		};
		const { files, template } = normalizeForPreview(filesIn, 'node');

		expect(template).toBe('node');
		const pkg = JSON.parse((files['/package.json'] as { code: string }).code);
		expect(pkg.scripts.start).toBe('node server.js');
		expect(pkg.dependencies.express).toBe('^4.17.1');
		expect(pkg.dependencies.broken).toBeUndefined(); // malformed → dropped
		expect(files['/server.js']).toBeDefined();
	});

	it('empty input → empty output', () => {
		const { files, template } = normalizeForPreview({}, 'node');
		expect(Object.keys(files).length).toBe(0);
		expect(template).toBe('node');
	});
});

describe('sanitizeDependencies', () => {
	it('drops malformed specifiers, keeps valid ones', () => {
		const out = sanitizeDependencies({
			dependencies: {
				express: '^4.17.1',
				broken1: '^.',
				broken2: '^10..1',
				broken3: '^17.0.',
				ok: '~1.2.3',
				star: '*',
				latest: 'latest',
				range: '>=14.0.0',
			},
		});
		expect(out.dependencies).toEqual({
			express: '^4.17.1',
			ok: '~1.2.3',
			star: '*',
			latest: 'latest',
			range: '>=14.0.0',
		});
	});

	it('also sanitizes devDependencies', () => {
		const out = sanitizeDependencies({
			devDependencies: { typescript: '^4.9.4', 'ts-node': '^10..1' },
		});
		expect(out.devDependencies).toEqual({ typescript: '^4.9.4' });
	});
});

describe('buildStaticSrcDoc', () => {
	const { files } = normalizeForPreview(STORE_PROJECT, 'node');

	it('inlines stylesheets and scripts into a self-contained document', async () => {
		const { buildStaticSrcDoc } = await import('./preview-normalize');
		const html = buildStaticSrcDoc(files);

		expect(html).toContain('<h1>Store</h1>');
		// css inlined → no <link> left, <style> present
		expect(html).toContain('<style>');
		expect(html).toContain('body { margin: 0; }');
		expect(html).not.toMatch(/<link[^>]*stylesheet/);
		// js inlined → no external src, code present
		expect(html).toContain("fetch('/api/products')");
		expect(html).not.toMatch(/<script[^>]*src=/);
	});

	it('stubs /api fetches so the UI renders without a backend', async () => {
		const { buildStaticSrcDoc } = await import('./preview-normalize');
		const html = buildStaticSrcDoc(files);
		expect(html).toContain("new Response('[]'");
	});
});

const SRC_HTML_PROJECT: Record<string, string> = {
	'src/index.html': '<!doctype html><html><body><h1>SRC App</h1><script src="app.js"></script></body></html>',
	'src/app.js': "console.log('hi');",
	'src/styles.css': 'p { color: red; }',
	'src/package.json': JSON.stringify({ scripts: { start: 'ts-node src/index.ts' }, devDependencies: { 'ts-node': '^10..1' } }),
};

describe('normalizeForPreview with src/ index.html', () => {
	it('hoists from /src even when index.html lives under src/', () => {
		const { files, template } = normalizeForPreview(SRC_HTML_PROJECT, 'node');
		expect(template).toBe('static');
		expect(files['/index.html']).toBeDefined();
		expect(files['/app.js']).toBeDefined();
		expect(files['/styles.css']).toBeDefined();
		expect(files['/src/index.ts']).toBeUndefined();
	});
});
describe('sanitizeJs', () => {
	it('does nothing to already-valid JavaScript', async () => {
		const { sanitizeJs } = await import('./preview-normalize');
		const valid = "window.App = { a: 1, b: 'x', c: 4.5 };";
		expect(sanitizeJs(valid)).toBe(valid);
	});

	it('repairs empty values after identifier keys', async () => {
		const { sanitizeJs } = await import('./preview-normalize');
		expect(sanitizeJs('const x = { rating: , price: 4.5 };')).toBe(
			'const x = { rating: null, price: 4.5 };',
		);
		expect(sanitizeJs('v = { hotelId:, type: 1 }')).toBe('v = { hotelId: null, type: 1 }');
	});

	it('repairs empty values after string keys', async () => {
		const { sanitizeJs } = await import('./preview-normalize');
		expect(sanitizeJs("{ x: { 'rating': , y: 2 } }")).toBe("{ x: { 'rating': null, y: 2 } }");
	});

	it('repairs truncated decimals', async () => {
		const { sanitizeJs } = await import('./preview-normalize');
		expect(sanitizeJs('const a = rating: 4., b: 3.5 }')).toBe('const a = rating: 4, b: 3.5 }');
	});

	it('makes the real corrupt data.js parseable', async () => {
		const { sanitizeJs } = await import('./preview-normalize');
		// Exact broken fixture from the Redis VFS (chat 38e544d8): empty `rating:`,
		// truncated `4.` and empty `hotelId:`.
		const broken = [
			'const hotels = [',
			"{ id: 1, name: 'Hotel 2', price: 150, rating: 4., image: 'x' },",
			"{ id: 3, name: 'Hotel 3', price: 100, rating: , image: 'x' },",
			"{ id: 4, hotelId:, type: 'Double', price: 120 },",
			'];',
		].join('\n');
		const fixed = sanitizeJs(broken);
		expect(fixed).not.toContain('rating: 4.');
		expect(fixed).not.toContain('rating: ,');
		expect(fixed).not.toContain('hotelId:,');
	});

	it('keeps valid decimals like 4.5 intact (no over-eager stripping)', async () => {
		const { sanitizeJs } = await import('./preview-normalize');
		expect(sanitizeJs('const x = { a: 4.5, b: 3.5 }')).toBe('const x = { a: 4.5, b: 3.5 }');
	});
});

describe('buildStaticSrcDoc script isolation', () => {
	it('wraps each inlined script in try/catch so one failure is isolated', async () => {
		const { buildStaticSrcDoc, normalizeForPreview } = await import('./preview-normalize');
		const filesIn: Record<string, string> = {
			'public/index.html':
				'<!doctype html><html><head></head><body><h1>X</h1><script src="a.js"></script><script src="b.js"></script></body></html>',
			'public/a.js': "throw new Error('boom');",
			'public/b.js': 'window.APP_B = true;',
		};
		const { files } = normalizeForPreview(filesIn, 'node');
		const html = buildStaticSrcDoc(files);
		// Both scripts present, each guarded by its own IIFE + try/catch.
		expect((html.match(/\btry \{/g) || []).length).toEqual(2);
		expect(html).toContain('window.APP_B = true;');
		expect(html).not.toMatch(/<script[^>]*src=/);
	});
});
