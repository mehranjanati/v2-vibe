// P7.1 validator: the four VibeSDK manifests (Agent/Skill/Tool/System) must
// validate against their JSON Schemas, use canonical serialization, and keep
// every content-addressed reference resolvable.
//
// Dependency-free on purpose: `ajv` exists in node_modules only as a
// transitive dependency, so a docs gate must not rely on it. The validator
// implements the JSON-Schema keyword subset the schemas use (documented in
// docs/architecture/manifest-schemas.md): type, enum, const, pattern,
// minLength, maxLength, minimum, maximum, required, properties,
// additionalProperties, items, minItems, uniqueItems, oneOf, and local
// `$ref` pointers into `$defs`.
//
// Usage: node scripts/validate-manifests.mjs [--verbose]
// Exit non-zero on any violation.
import fs from 'node:fs';
import path from 'node:path';

import { fileURLToPath } from 'node:url';

import { createSubsetValidator } from './lib/schema-subset.mjs';

const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
const schemaDir = 'docs/architecture/manifest-schemas';
const exampleDir = `${schemaDir}/examples`;
// Use-case bundles live outside the platform schemas on purpose: a use case
// (docs/use-cases/<name>/manifests) must not mix into the stack's own example
// set, but its manifests are still schema-validated here.
const useCaseRoot = 'docs/use-cases';
const KINDS = ['Agent', 'Skill', 'Tool', 'System'];

const errors = [];
const notes = [];
const fail = (msg) => errors.push(msg);

function readJson(rel) {
  const abs = path.join(root, rel);
  if (!fs.existsSync(abs)) {
    fail(`${rel}: missing`);
    return null;
  }
  try {
    return JSON.parse(fs.readFileSync(abs, 'utf8'));
  } catch (e) {
    fail(`${rel}: invalid JSON (${e.message})`);
    return null;
  }
}

// --- JSON Schema subset + canonical serialization (shared) --------------------
// Implementation lives in ./lib/schema-subset.mjs so the P7.2 IR gate uses
// the exact same semantics.
const { validate: validateNode, canonical, serialize: serializeManifest } =
  createSubsetValidator(errors);

// --- load schemas + examples --------------------------------------------------
const schemas = new Map();
for (const kind of KINDS) {
  const rel = `${schemaDir}/${kind.toLowerCase()}.schema.json`;
  const schema = readJson(rel);
  if (!schema) continue;
  if (schema.$schema !== 'https://json-schema.org/draft/2020-12/schema') {
    fail(`${rel}: must declare $schema draft 2020-12 (got ${schema.$schema})`);
  }
  if (schema.properties?.kind?.const !== kind) {
    fail(`${rel}: properties.kind must pin the const '${kind}'`);
  }
  schemas.set(kind, schema);
}
if (schemas.size !== KINDS.length) {
  fail(`${schemaDir}: expected ${KINDS.length} schemas, found ${schemas.size}`);
}

let exampleFiles = [];
try {
  exampleFiles = fs.readdirSync(path.join(root, exampleDir)).filter((f) => f.endsWith('.json')).sort();
} catch {
  fail(`${exampleDir}: directory missing`);
}
if (exampleFiles.length === 0) fail(`${exampleDir}: no example manifests found`);

// Use-case bundles: docs/use-cases/<use-case>/manifests/*.json. A bundle is
// its own closed reference set — an agent there may only reference manifests
// shipped in the same bundle, so a use case cannot silently depend on a
// platform example.
const bundles = [];
try {
  const cases = fs
    .readdirSync(path.join(root, useCaseRoot), { withFileTypes: true })
    .filter((d) => d.isDirectory())
    .map((d) => d.name)
    .sort();
  for (const name of cases) {
    const dir = `${useCaseRoot}/${name}/manifests`;
    if (!fs.existsSync(path.join(root, dir))) continue;
    const files = fs.readdirSync(path.join(root, dir)).filter((f) => f.endsWith('.json')).sort();
    if (files.length === 0) continue;
    bundles.push({ name, dir, files: files.map((f) => path.join(dir, f)) });
  }
} catch {
  // No use cases yet: nothing to validate.
}

const known = new Map(); // `Kind/name@version` -> rel path
const manifests = [];
const bundleOf = new Map(); // rel path -> bundle name (undefined = platform)

for (const file of exampleFiles) {
  const rel = `${exampleDir}/${file}`;
  const abs = path.join(root, rel);
  const raw = fs.existsSync(abs) ? fs.readFileSync(abs, 'utf8') : null;
  if (raw === null) continue;
  const manifest = readJson(rel);
  if (!manifest) continue;

  const kind = manifest.kind;
  const schema = schemas.get(kind);
  if (!schema) {
    fail(`${rel}: unknown kind ${JSON.stringify(kind)} (expected ${KINDS.join('|')})`);
    continue;
  }
  if ('status' in manifest) {
    fail(`${rel}: 'status' must never be stored in a manifest (runtime state lives in the data layer)`);
  }

  validateNode(manifest, schema, schema, '$', rel);

  const id = `${manifest.kind}/${manifest.metadata?.name}@${manifest.metadata?.version}`;
  if (known.has(id)) fail(`${rel}: duplicate manifest identity ${id} (also ${known.get(id)})`);
  known.set(id, rel);
  manifests.push({ rel, manifest, id, raw });
}

// --- use-case bundles --------------------------------------------------------
for (const bundle of bundles) {
  const local = new Map();
  for (const rel of bundle.files) {
    const raw = fs.readFileSync(path.join(root, rel), 'utf8');
    const manifest = readJson(rel);
    if (!manifest) continue;
    const kind = manifest.kind;
    const schema = schemas.get(kind);
    if (!schema) {
      fail(`${rel}: unknown kind ${JSON.stringify(kind)} (expected ${KINDS.join('|')})`);
      continue;
    }
    if ('status' in manifest) {
      fail(`${rel}: 'status' must never be stored in a manifest (runtime state lives in the data layer)`);
    }
    validateNode(manifest, schema, schema, '$', rel);
    const id = `${manifest.kind}/${manifest.metadata?.name}@${manifest.metadata?.version}`;
    if (local.has(id)) fail(`${rel}: duplicate manifest identity ${id} (also ${local.get(id)})`);
    local.set(id, rel);
    bundleOf.set(rel, bundle.name);
    manifests.push({ rel, manifest, id, raw });
  }
  for (const [id, rel] of local) known.set(`${bundle.name}\x00${id}`, rel);
}

// --- reference resolution -----------------------------------------------------
const REF_RE = /^(agent|skill|tool)\/([a-z0-9](?:[-a-z0-9]*[a-z0-9])?)@(\d+)\.(\d+)\.(\d+)$/;
const EXTERNAL_RE = /^(model-policy|knowledge|eval)\/([a-z0-9](?:[-a-z0-9]*[a-z0-9])?)@(\d+)\.(\d+)\.(\d+)$/;

// ref-bearing keys. `ref` and `dataset` hold a single ref string; the array
// keys hold lists of refs (a Skill's `requires.capabilities`, an Agent's
// `skills`/`tools`/`knowledge`). Without the array keys a capability list was
// never resolved, which made the closed-bundle rule unenforceable.
const SINGLE_REF_KEYS = new Set(['ref', 'dataset']);
const ARRAY_REF_KEYS = new Set(['skills', 'tools', 'knowledge', 'capabilities']);

function collectRefs(node, out, at) {
  if (Array.isArray(node)) {
    node.forEach((item, i) => collectRefs(item, out, `${at}[${i}]`));
    return;
  }
  if (node && typeof node === 'object') {
    for (const [key, value] of Object.entries(node)) {
      if (SINGLE_REF_KEYS.has(key) && typeof value === 'string') {
        out.push({ ref: value, at: `${at}.${key}` });
      } else if (ARRAY_REF_KEYS.has(key) && Array.isArray(value)) {
        value.forEach((item, i) => {
          if (typeof item === 'string') out.push({ ref: item, at: `${at}.${key}[${i}]` });
          else collectRefs(item, out, `${at}.${key}[${i}]`);
        });
      } else {
        collectRefs(value, out, `${at}.${key}`);
      }
    }
  }
}

for (const { rel, manifest } of manifests) {
  const refs = [];
  collectRefs(manifest, refs, '$');
  const bundleName = bundleOf.get(rel);
  for (const { ref, at } of refs) {
    if (EXTERNAL_RE.test(ref)) {
      notes.push(`${rel}: ${at} references external namespace '${ref}' (not shipped in examples)`);
      continue;
    }
    const m = REF_RE.exec(ref);
    if (!m) {
      fail(
        `${rel}: ${at} has malformed reference '${ref}' ` +
          '(expected kind/name@MAJOR.MINOR.PATCH; floating refs are rejected)',
      );
      continue;
    }
    const [, kind, name, major, minor, patch] = m;
    const target = `${kind[0].toUpperCase()}${kind.slice(1)}/${name}@${major}.${minor}.${patch}`;
    const resolved = bundleName ? known.has(`${bundleName}\x00${target}`) : known.has(target);
    if (!resolved) {
      const scope = bundleName ? `use-case bundle '${bundleName}'` : 'the platform examples';
      fail(`${rel}: ${at} references unresolved manifest '${target}' (not in ${scope})`);
    }
  }
}

// --- canonical round-trip -----------------------------------------------------
for (const { rel, manifest, raw } of manifests) {
  const canonicalForm = serializeManifest(manifest);
  if (raw !== canonicalForm) {
    fail(`${rel}: not canonically serialized (sorted keys, 2-space indent, single trailing newline)`);
  }
  const reparsed = JSON.parse(canonicalForm);
  if (canonical(reparsed) !== canonical(manifest)) {
    fail(`${rel}: round-trip changed the manifest`);
  }
}

// --- report -------------------------------------------------------------------
if (process.argv.includes('--verbose')) for (const note of notes) console.log(`note: ${note}`);

if (errors.length) {
  console.error('manifest validation FAILED:');
  for (const e of errors) console.error(' - ' + e);
  process.exit(1);
}

console.log(
  `manifests: OK (${manifests.length} manifests across ${schemas.size} schemas, ` +
    `${known.size} identities, ${notes.length} external reference(s), canonical round-trip + refs resolved)`,
);
