// D22 validator: Smart Contract IR fixtures must validate against the schema,
// be canonically serialized, keep the semantic core target-independent, and
// never carry runtime state.
//
// Reuses the same dependency-free JSON-Schema subset as the manifest (P7.1)
// and System IR (P7.2) gates, so all three docs contracts share one
// semantics. Scope: docs/architecture/contract-ir only — nothing here touches
// runtime code, and no generator consumes the IR yet.
//
// Usage: node scripts/validate-contract-ir.mjs [--verbose]
// Exit non-zero on any violation.
import fs from 'node:fs';
import path from 'node:path';

import { fileURLToPath } from 'node:url';

import { createSubsetValidator } from './lib/schema-subset.mjs';

const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
const dir = 'docs/architecture/contract-ir';
const exampleDir = `${dir}/examples`;

const errors = [];
const fail = (msg) => errors.push(msg);
const { validate, canonical, serialize } = createSubsetValidator(errors);

// --- schema -------------------------------------------------------------------
const schemaRel = `${dir}/contract.schema.json`;
let schema = null;
try {
  schema = JSON.parse(fs.readFileSync(path.join(root, schemaRel), 'utf8'));
} catch (e) {
  fail(`${schemaRel}: cannot read schema (${e.message})`);
}
if (schema) {
  if (schema.$schema !== 'https://json-schema.org/draft/2020-12/schema') {
    fail(`${schemaRel}: must declare $schema draft 2020-12`);
  }
  if (schema.properties?.kind?.const !== 'SmartContract') {
    fail(`${schemaRel}: properties.kind must pin the const 'SmartContract'`);
  }
  if (!schema.properties?.apiVersion?.const) {
    fail(`${schemaRel}: properties.apiVersion must pin the envelope const`);
  }
  const core = schema.$defs?.abstract?.properties ?? {};
  for (const leaked of ['vm', 'language', 'solc', 'chains', 'evm']) {
    if (leaked in core) fail(`${schemaRel}: the semantic core must not declare '${leaked}'`);
  }
  for (const required of ['functions', 'provenance', 'upgradeability']) {
    if (!(schema.$defs?.abstract?.required ?? []).includes(required)) {
      fail(`${schemaRel}: core must require '${required}'`);
    }
  }
}

// --- fixtures -----------------------------------------------------------------
let files = [];
try {
  files = fs.readdirSync(path.join(root, exampleDir)).filter((f) => f.endsWith('.contract.json')).sort();
} catch {
  fail(`${exampleDir}: directory missing`);
}
if (files.length === 0) fail(`${exampleDir}: no contract fixtures found`);

const known = new Map();
const loaded = [];
for (const file of files) {
  const rel = `${exampleDir}/${file}`;
  const abs = path.join(root, rel);
  if (!fs.existsSync(abs)) continue;
  const raw = fs.readFileSync(abs, 'utf8');
  let doc = null;
  try {
    doc = JSON.parse(raw);
  } catch (e) {
    fail(`${rel}: invalid JSON (${e.message})`);
    continue;
  }
  if (!schema) continue;
  validate(doc, schema, schema, '$', rel);
  const id = `${doc.metadata?.publisher}/${doc.metadata?.name}@${doc.metadata?.version}`;
  if (known.has(id)) fail(`${rel}: duplicate contract identity ${id} (also ${known.get(id)})`);
  known.set(id, rel);
  loaded.push({ rel, doc, raw, id });
}

// --- canonical round-trip ------------------------------------------------------
for (const { rel, doc, raw } of loaded) {
  const canon = serialize(doc);
  if (raw !== canon) {
    fail(`${rel}: not canonically serialized (sorted keys, 2-space indent, single trailing newline)`);
  }
  if (canonical(JSON.parse(canon)) !== canonical(doc)) fail(`${rel}: round-trip changed the document`);
}

// --- semantic invariants (D22) -------------------------------------------------
// Keys that are only legal in `spec.targets` (VM/compiler/chain) or in a
// namespaced `spec.extensions` block (vendor/bridge). Finding one in the core
// means the core stopped being target-independent.
const CORE_FORBIDDEN_KEY =
  /^(vm|language|solc|evmVersion|optimizer|chainId|chains|eid|endpoint|endpointId|dvn|dvns|dvnThreshold|executor|peer|peers|tokenStandard|tokenStandards|erc20|erc721|erc1155|lzReceive|lzSend|messagingProvider)$/i;
// Runtime state never belongs in a desired-state document.
const RUNTIME_STATE_KEY = /^(status|address|deployedAt|deployedBlock|txHash|balance|nonce|health|owner)$/i;
const VENDOR_IN_CORE_VALUE = /(layerzero|erc-?20|erc-?721|erc-?1155)/i;

function walk(node, at, onKey) {
  if (Array.isArray(node)) {
    node.forEach((item, i) => walk(item, `${at}[${i}]`, onKey));
    return;
  }
  if (node && typeof node === 'object') {
    for (const [key, value] of Object.entries(node)) {
      onKey(key, value, `${at}.${key}`);
      walk(value, `${at}.${key}`, onKey);
    }
  }
}

function semanticErrors(rel, doc, knownIds) {
  const out = [];
  const fail = (msg) => out.push(msg);
  const core = doc.spec?.abstract ?? {};
  const targets = doc.spec?.targets ?? {};

  // 1) the core stays target-independent — by key and by value
  walk(core, '$.spec.abstract', (key, value, at) => {
    if (CORE_FORBIDDEN_KEY.test(key)) {
      fail(`${rel}: ${at} is target/vendor-specific and must live in spec.targets or spec.extensions`);
    }
    if (typeof value === 'string' && VENDOR_IN_CORE_VALUE.test(value)) {
      fail(`${rel}: ${at} names a VM/vendor detail inside the core ('${value}')`);
    }
  });

  // 2) no runtime state anywhere in the document
  walk(doc, '$', (key, _value, at) => {
    if (RUNTIME_STATE_KEY.test(key)) {
      fail(`${rel}: ${at} carries runtime state; this document is desired state only`);
    }
  });

  // 3) an EVM target must declare its EVM block (schema cannot express if/then)
  if (targets.vm === 'evm' && !targets.evm) {
    fail(`${rel}: spec.targets.vm is 'evm' but the evm block is missing`);
  }

  // 4) a payable function must declare an access policy
  for (const fn of core.functions ?? []) {
    if (fn.mutability === 'payable' && !fn.policy) {
      fail(`${rel}: function '${fn.name}' is payable but declares no policy`);
    }
    if (fn.policy && !(core.policies ?? []).some((p) => p.id === fn.policy)) {
      fail(`${rel}: function '${fn.name}' references undeclared policy '${fn.policy}'`);
    }
  }

  // 5) extensions are namespaced and never override a core/target slot
  const reserved = new Set(['abstract', 'targets', 'extensions']);
  for (const [ns, block] of Object.entries(doc.spec?.extensions ?? {})) {
    if (!/^[a-z][a-z0-9]*(-[a-z0-9]+)*$/.test(ns)) {
      fail(`${rel}: extension namespace '${ns}' must be lowercase kebab-case`);
    }
    if (reserved.has(ns)) fail(`${rel}: extension namespace '${ns}' shadows a core slot`);
    if (block !== null && typeof block !== 'object') {
      fail(`${rel}: extension '${ns}' must be an object`);
    }
  }

  // 6) every dependency ref is pinned and resolvable among the fixtures
  for (const dep of core.dependencies ?? []) {
    if (!knownIds.has(dep.ref)) {
      fail(`${rel}: dependency '${dep.ref}' is unresolved (expected publisher/name@X.Y.Z from the fixtures)`);
    }
  }

  return out;
}

for (const { rel, doc } of loaded) errors.push(...semanticErrors(rel, doc, known));

// --- negative probes -----------------------------------------------------------
// A gate that only ever sees valid input proves nothing, so mutate a copy of a
// real fixture and require each probe to be rejected.
const base = loaded[0]?.doc;
let probeCount = 0;
if (!base) {
  fail('negative probes: no fixture available to derive probes from');
} else {
  const clone = (mutate) => {
    const copy = JSON.parse(JSON.stringify(base));
    mutate(copy);
    return copy;
  };
  const probes = [
    ['vendor value inside the core', clone((d) => { d.spec.abstract.purpose = 'bridged with LayerZero'; })],
    ['target key inside the core', clone((d) => { d.spec.abstract.state[0].vm = 'evm'; })],
    ['runtime state at the root', clone((d) => { d.owner = '0x0000000000000000000000000000000000000000'; })],
    ['evm target without an evm block', clone((d) => { delete d.spec.targets.evm; })],
    ['payable function without a policy', clone((d) => {
      d.spec.abstract.functions[0].mutability = 'payable';
      delete d.spec.abstract.functions[0].policy;
    })],
    ['non-kebab extension namespace', clone((d) => { d.spec.extensions = { LayerZero_V2: {} }; })],
    ['unresolved dependency ref', clone((d) => { d.spec.abstract.dependencies = [{ ref: 'vibe/ghost@9.9.9' }]; })],
  ];
  for (const [name, probe] of probes) {
    if (semanticErrors('probe', probe, known).length === 0) {
      fail(`negative probe '${name}' was not rejected by the semantic layer`);
    }
    probeCount += 1;
  }

  // One schema-level probe: the envelope must refuse runtime state outright.
  if (schema) {
    const schemaErrors = [];
    const { validate: probeValidate } = createSubsetValidator(schemaErrors);
    probeValidate(clone((d) => { d.status = 'deployed'; }), schema, schema, '$', 'schema-probe');
    if (schemaErrors.length === 0) fail('negative probe: a document carrying status was accepted by the schema');
    probeCount += 1;
  }
}

// --- report -------------------------------------------------------------------
if (process.argv.includes('--verbose')) {
  for (const { rel, id } of loaded) console.log(`fixture: ${rel} -> ${id}`);
}

if (errors.length) {
  console.error('contract IR validation FAILED:');
  for (const e of errors) console.error(' - ' + e);
  process.exit(1);
}

console.log(
  `contract-ir: OK (${loaded.length} fixtures, ${known.size} identities, ${probeCount} negative probes rejected, ` +
    'target-independent core, canonical round-trip, provenance + policy + pinned-dependency checks)',
);

