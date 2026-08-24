import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import svgr from 'vite-plugin-svgr';
import path from 'path';
import tailwindcss from '@tailwindcss/vite';

// Local-only dev config: NO @cloudflare/vite-plugin (avoids workerd, which
// needs macOS 13.5+). Develop the SPA against the local Go control plane
// (localhost:8080) from your own browser. Auth routes default same-origin
// here; set VITE_AUTH_PLANE_URL to the deployed worker to use it for auth.
export default defineConfig({
	optimizeDeps: {
		exclude: ['format', 'editor.all'],
		include: ['monaco-editor/editor/editor.api'],
		force: true,
	},
	plugins: [react(), svgr(), tailwindcss()],
	resolve: {
		alias: [
			{ find: 'debug', replacement: 'debug/src/browser' },
			{ find: /^mimetext$/, replacement: 'mimetext/browser' },
			{
				find: /^safe-buffer$/,
				replacement: path.resolve(__dirname, './worker/polyfills/safe-buffer.ts'),
			},
			{ find: '@', replacement: path.resolve(__dirname, './src') },
			{ find: 'shared', replacement: path.resolve(__dirname, './shared') },
			{ find: 'worker', replacement: path.resolve(__dirname, './worker') },
		],
	},
	define: {
		'process.env.NODE_ENV': JSON.stringify(process.env.NODE_ENV || 'development'),
		global: 'globalThis',
	},
	server: {
		allowedHosts: true,
	},
});