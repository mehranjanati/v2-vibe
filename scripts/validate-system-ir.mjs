// P7.2 gate: the Canonical System IR v1 fixtures must (a) validate against the
// System Manifest schema, (b) satisfy the IR invariants, (c) round-trip
// canonically, and (d) prove the four required cases are expressible without
// expanding the schema — including negative probes that must be rejected.
//
// IR v1 is the serializable form of `kind: System` (docs/06, docs/12): one
// representation, no parallel format. This gate reads the P7.1 schema instead
// of defining a second one.
//
// Usage: node scripts/validate-system-ir.mjs [--verbose]
// Exit non-zero on any violation.
import fs from 'node:fs';
import path from 'node:path';

import { fileURLToPath } from 'node:url';

import { createSubsetValidator } from './lib/schema-subset.mjs';

const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
const SCHEMA = 'docs/architecture/manifest-schemas/system.schema.json';
const FIXTURE_DIR = 'docs/architecture/system-ir/examples';
const NODE_KINDS = ['trigger', 'http', 'db', 'ai', 'email', 'condition', 'sleep'];

// The four cases P7.2 must prove (docs/architecture/system-ir.md §Four cases).
const REQUIRED_CASES = [
  { file: 'app-builder.system.json', label: 'App Builder reference workload', shape: 'agents' },
  { file: 'm0-single-agent.system.json', label: 'M0 single agent', shape: 'agents' },
  { file: 'm1-team.system.json', label: 'M1 multi-agent team', shape: 'agents' },
  { file: 'visual-workflow.system.json', label: 'visual workflow (7 node kinds)', shape: 'workflow' },
];

const errors = [];
const notes = [];
const failDefault = (msg) => errors.push(msg);
const fail = failDefault;
const { validate, canonical, serialize } = createSubsetValidator(errors);

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

const schema = readJson(SCHEMA);
if (!schema) {
  console.error(errors.join('\n'));
  process.exit(1);
}

// --- IR invariants (beyond JSON Schema) ---------------------------------------
const AGENT_REF = /^agent\/[a-z0-9]([-a-z0-9]*[a-z0-9])?@(\d+)\.(\d+)\.(\d+)$/;
const ANY_REF = /^(agent|skill|tool|model-policy|knowledge|eval)\/[a-z0-9]([-a-z0-9]*[a-z0-9])?@(\d+)\.(\d+)\.(\d+)$/;
const SLUG = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
const RUNTIME_ONLY_KEYS = ['status', 'state', 'health', 'lastRun', 'runId', 'metrics', 'telemetry', 'logs'];

function walkKeys(node, visit, at = '$') {
  if (Array.isArray(node)) {
    node.forEach((item, i) => walkKeys(item, visit, `${at}[${i}]`));
    return;
  }
  if (node && typeof node === 'object') {
    for (const [key, value] of Object.entries(node)) {
      visit(key, `${at}.${key}`);
      walkKeys(value, visit, `${at}.${key}`);
    }
  }
}

function checkInvariants(label, manifest, fail = failDefault) {
  const spec = manifest.spec || {};
  const agents = spec.agents || {};
  const hasAgents = Object.keys(agents).length > 0;
  const hasWorkflow = Boolean(spec.workflow);

  // I1 — exactly one execution shape in v1.
  if (hasAgents && hasWorkflow) {
    fail(`${label}: declares both 'agents' and 'workflow' (IR v1 allows exactly one execution shape)`);
  }
  if (!hasAgents && !hasWorkflow) {
    fail(`${label}: declares neither 'agents' nor 'workflow' (IR v1 needs one execution shape)`);
  }

  // I2 — desired state only: runtime state never lives in the IR.
  walkKeys(manifest, (key, at) => {
    if (RUNTIME_ONLY_KEYS.includes(key)) {
      fail(`${label}: ${at} carries runtime state '${key}' (the IR is desired state; runs live in the data layer)`);
    }
  });

  // I3 — agent refs are pinned Agent refs; delegation targets exist.
  for (const [name, agent] of Object.entries(agents)) {
    if (!SLUG.test(name)) fail(`${label}: agent key '${name}' is not a slug`);
    if (!AGENT_REF.test(agent.ref || '')) {
      fail(`${label}: agent '${name}' ref '${agent.ref}' must be agent/name@MAJOR.MINOR.PATCH`);
    }
    for (const target of agent.delegatesTo || []) {
      if (!(target in agents)) fail(`${label}: agent '${name}' delegates to unknown agent '${target}'`);
      if (target === name) fail(`${label}: agent '${name}' delegates to itself`);
    }
  }

  // I4 — topology references declared agents, one step per agent.
  const seenSteps = new Set();
  for (const [i, step] of (spec.topology || []).entries()) {
    if (!(step.run in agents)) fail(`${label}: topology[${i}] runs unknown agent '${step.run}'`);
    if (seenSteps.has(step.run)) fail(`${label}: topology[${i}] runs '${step.run}' twice (v1: one step per agent)`);
    seenSteps.add(step.run);
    if (step.after !== undefined && !(step.after in agents)) {
      fail(`${label}: topology[${i}] after unknown agent '${step.after}'`);
    }
  }

  // I5 — workflow graph integrity: unique ids, known kinds, resolvable edges.
  if (spec.workflow) {
    const nodes = spec.workflow.nodes || [];
    const ids = new Set();
    for (const [i, node] of nodes.entries()) {
      if (ids.has(node.id)) fail(`${label}: workflow node id '${node.id}' duplicated (index ${i})`);
      ids.add(node.id);
      if (!NODE_KINDS.includes(node.kind)) {
        fail(`${label}: workflow node '${node.id}' has unknown kind '${node.kind}'`);
      }
      if (node.capability && !ANY_REF.test(node.capability)) {
        fail(`${label}: workflow node '${node.id}' capability '${node.capability}' must be a pinned ref`);
      }
    }
    for (const node of nodes) {
      for (const next of node.next || []) {
        if (!ids.has(next)) fail(`${label}: workflow node '${node.id}' points to unknown node '${next}'`);
      }
    }
    for (const entry of spec.workflow.entry || []) {
      if (!ids.has(entry)) fail(`${label}: workflow entry '${entry}' does not exist`);
    }
    const reachable = new Set();
    const queue = [...(spec.workflow.entry || (nodes[0] ? [nodes[0].id] : []))];
    while (queue.length) {
      const id = queue.shift();
      if (reachable.has(id)) continue;
      reachable.add(id);
      const node = nodes.find((n) => n.id === id);
      for (const next of node?.next || []) queue.push(next);
    }
    for (const node of nodes) {
      if (!reachable.has(node.id)) {
        notes.push(`${label}: workflow node '${node.id}' is unreachable from the entry set`);
      }
    }
  }

  // I6 — approval classes use the shared vocabulary and a typed approver.
  for (const [i, approval] of (spec.approvals || []).entries()) {
    if (!/^(role|user|group):[A-Za-z0-9_.-]+$/.test(approval.approver || '')) {
      fail(`${label}: approvals[${i}].approver must be role:/user:/group:`);
    }
  }
}


// --- the four cases -----------------------------------------------------------
const cases = [];
for (const { file, label, shape } of REQUIRED_CASES) {
  const rel = `${FIXTURE_DIR}/${file}`;
  const abs = path.join(root, rel);
  if (!fs.existsSync(abs)) {
    fail(`${rel}: the '${label}' case fixture is missing`);
    continue;
  }
  const raw = fs.readFileSync(abs, 'utf8');
  const manifest = readJson(rel);
  if (!manifest) continue;

  if (manifest.kind !== 'System') fail(`${rel}: IR fixtures must be kind: System (got ${manifest.kind})`);
  validate(manifest, schema, schema, '$', rel);
  checkInvariants(rel, manifest);

  const spec = manifest.spec || {};
  const actualShape = spec.workflow ? 'workflow' : 'agents';
  if (actualShape !== shape) {
    fail(`${rel}: '${label}' must use the ${shape} shape (found ${actualShape})`);
  }
  if (spec.workflow) {
    const kinds = new Set((spec.workflow.nodes || []).map((n) => n.kind));
    for (const kind of NODE_KINDS) {
      if (!kinds.has(kind)) notes.push(`${rel}: workflow case does not exercise node kind '${kind}'`);
    }
  }

  const canonicalForm = serialize(manifest);
  if (raw !== canonicalForm) {
    fail(`${rel}: not canonically serialized (sorted keys, 2-space indent, single trailing newline)`);
  }
  if (canonical(JSON.parse(canonicalForm)) !== canonical(manifest)) {
    fail(`${rel}: '${label}' round-trip changed the IR`);
  }

  cases.push({ file, label, shape });
}

// --- negative probes: v1 must reject over-general and runtime fields -----------
const probeErrors = [];
const probeFail = (msg) => probeErrors.push(msg);
const { validate: probeValidate } = createSubsetValidator(probeErrors);

function probeMustFail(rel, label, mutate) {
  const base = readJson(rel);
  if (!base) return;
  const clone = JSON.parse(JSON.stringify(base));
  mutate(clone);
  const before = probeErrors.length;
  const notesBefore = notes.length;
  const probeLabel = `${rel} (probe: ${label})`;
  probeValidate(clone, schema, schema, '$', probeLabel);
  checkInvariants(probeLabel, clone, probeFail);
  if (probeErrors.length === before) {
    fail(`${rel}: negative probe '${label}' was accepted (IR v1 must reject it)`);
  }
  probeErrors.length = before;
  notes.length = notesBefore; // probe notes are noise, not findings
}

const PROBE_BASE = `${FIXTURE_DIR}/m1-team.system.json`;
probeMustFail(PROBE_BASE, 'runtime status', (m) => {
  m.spec.agents.coordinator.status = 'running';
});
probeMustFail(PROBE_BASE, 'telemetry block', (m) => {
  m.spec.telemetry = { cost: 0.02 };
});
probeMustFail(PROBE_BASE, 'unpinned agent ref', (m) => {
  m.spec.agents.drafter.ref = 'agent/followup-drafter@latest';
});
probeMustFail(PROBE_BASE, 'unknown delegation target', (m) => {
  m.spec.agents.coordinator.delegatesTo = ['ghost'];
});
probeMustFail(PROBE_BASE, 'both execution shapes', (m) => {
  m.spec.workflow = { nodes: [{ id: 'only', kind: 'sleep' }] };
});
probeMustFail(`${FIXTURE_DIR}/visual-workflow.system.json`, 'dangling workflow edge', (m) => {
  m.spec.workflow.nodes[0].next = ['does-not-exist'];
});

// --- report -------------------------------------------------------------------
if (process.argv.includes('--verbose')) for (const note of notes) console.log(`note: ${note}`);

if (errors.length) {
  console.error('system IR validation FAILED:');
  for (const e of errors) console.error(' - ' + e);
  process.exit(1);
}

console.log(
  `system-ir: OK (IR v1 = System Manifest spec; ${cases.length}/${REQUIRED_CASES.length} cases ` +
    `[${cases.map((c) => c.shape).join(', ')}], 6 negative probes rejected, canonical round-trip, ` +
    `${notes.length} note(s))`,
);
