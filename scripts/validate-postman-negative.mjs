// T3/R5 negative self-test: asserts the validator FAILS (non-zero) on fixtures
// containing each violation class:
//   1. collection/environment fixture  -> request-level violations (plane markers,
//      deprecated {{baseUrl}}, dead routes, unresolved variables, NDJSON…), plus
//      the manifest-vs-code cross-check running against the real sources.
//   2. contract fixture (T16)          -> manifest drift: one entry that no longer
//      exists in the sources + one live code route missing from the manifest.
//      The real collection/environment are reused so only the contract is broken.
import { spawnSync } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const dir = path.dirname(fileURLToPath(import.meta.url));

function runValidator(label, args) {
  const r = spawnSync('node', [path.join(dir, 'validate-postman.mjs'), ...args], { encoding: 'utf8' });
  console.log(`--- [${label}] validator stdout ---`);
  console.log(r.stdout);
  console.log(`--- [${label}] validator stderr ---`);
  console.log(r.stderr);
  return r;
}

const collectionCase = runValidator('collection fixture', [
  '--collection', path.join(dir, 'postman-negative.collection.json'),
  '--environment', path.join(dir, 'postman-negative.environment.json'),
]);
const contractCase = runValidator('contract fixture', [
  '--contract', path.join(dir, 'postman-negative.contract.json'),
]);

const failures = [];
if (collectionCase.status === 0) {
  failures.push('collection fixture: validator exited 0 on a fixture full of violations (vacuously green)');
}
if (contractCase.status === 0) {
  failures.push('contract fixture: validator exited 0 on a drifted manifest (stale + missing entries)');
}
if (failures.length > 0) {
  console.error(`NEGATIVE SELF-TEST FAILED: ${failures.join('; ')}`);
  process.exit(1);
}
console.log('NEGATIVE SELF-TEST PASSED: validator correctly failed on both fixture classes (collection violations + manifest drift).');
