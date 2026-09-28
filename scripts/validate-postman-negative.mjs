// T3/R5 negative self-test: asserts the validator FAILS (non-zero) on a fixture
// collection/environment containing one of each violation class.
import { spawnSync } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const dir = path.dirname(fileURLToPath(import.meta.url));
const r = spawnSync('node', [path.join(dir, 'validate-postman.mjs'),
  '--collection', path.join(dir, 'postman-negative.collection.json'),
  '--environment', path.join(dir, 'postman-negative.environment.json'),
], { encoding: 'utf8' });
console.log('--- validator stdout ---');
console.log(r.stdout);
console.log('--- validator stderr ---');
console.log(r.stderr);
if (r.status !== 0) { console.log('NEGATIVE SELF-TEST PASSED: validator correctly failed on fixture violations.'); process.exit(0); }
console.error('NEGATIVE SELF-TEST FAILED: validator exited 0 on a fixture full of violations (vacuously green).');
process.exit(1);
