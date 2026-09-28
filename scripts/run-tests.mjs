// G8 preflight: `bun run test` used to die mid-run with workerd's raw
// "Unsupported macOS version" stack trace (workerd needs macOS 13.5.0+).
// This wrapper checks the environment up front and prints actionable
// guidance instead, then delegates to vitest on supported platforms.
//
// - On macOS >= 13.5 / Linux / Windows: runs `vitest run [args...]`.
// - On older macOS: exits 1 with a clear message (CI / DevContainer hint).
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';

function macVersion() {
	if (process.platform !== 'darwin') return null;
	try {
		const out = spawnSync('sw_vers', ['-productVersion'], { encoding: 'utf8' });
		if (out.status !== 0) return null;
		const [major, minor] = out.stdout.trim().split('.').map((n) => Number(n) || 0);
		return { major: major || 0, minor: minor || 0 };
	} catch {
		return null;
	}
}

const mac = macVersion();
if (mac && (mac.major < 13 || (mac.major === 13 && mac.minor < 5))) {
	const pkg = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'));
	console.error(`
✖ bun run test: the Cloudflare Workers runtime (workerd) needs macOS 13.5.0+;
  this machine is macOS ${mac.major}.${mac.minor}.

  Options:
   1. Run the suite in CI (ubuntu-latest) — see G8 in backend/docs/OUTPUT_QUALITY_BUGS.md
   2. Use a Linux DevContainer / upgrade to macOS 13.5+
   3. Skip the hook locally with SKIP_TESTS=1 when committing

  (Pre-commit runs vitest directly via bunx; use SKIP_TESTS=1 there.)
  vitest ${pkg.devDependencies.vitest ?? ''} is installed but cannot start here.
`);
	process.exit(1);
}

// Delegate to vitest with all passthrough args (e.g. `bun run test src/lib/utils.test.ts`).
const args = process.argv.slice(2);
const runners = [
	['bunx', ['vitest', 'run', ...args]],
	['npx', ['vitest', 'run', ...args]],
];
for (const [cmd, argv] of runners) {
	const res = spawnSync(cmd, argv, { stdio: 'inherit' });
	if (res.error && res.error.code === 'ENOENT') continue; // try next runner
	process.exit(res.status ?? 1);
}
console.error('✖ bun run test: neither bunx nor npx could start vitest');
process.exit(1);
