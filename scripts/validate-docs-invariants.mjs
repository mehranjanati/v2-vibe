// Docs invariants for the two remaining triggered-update routines (T16):
//
//   1. CF_LIMITS.md — every platform number carries a source id. Enforced here:
//      every `[Sn]` reference resolves to a row in the §۷ registry, every `## `
//      section declares its own `**Last verified:**` date, and every registry row
//      keeps an official URL plus the document date + our verification date.
//      (Re-checking the live Cloudflare pages stays a manual, network step; see
//      the procedure in docs/CF_LIMITS.md §۷.)
//   2. Bindings — the binding set is declared in three places that must agree:
//      `wrangler.v2.jsonc` (deployment truth), the generated `worker-configuration.d.ts`
//      (`bun run cf-typegen`) and the "Bindings (light Worker)" table in
//      `docs/architecture-diagrams.md`. A binding added/removed in the config must
//      therefore be regenerated AND documented in the same change.
//
// Usage: node scripts/validate-docs-invariants.mjs   (exit non-zero on violation)
import fs from 'node:fs';
import path from 'node:path';

import { fileURLToPath } from 'node:url';
const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');

const errors = [];
const fail = (msg) => errors.push(msg);

function read(rel) {
  const abs = path.join(root, rel);
  if (!fs.existsSync(abs)) {
    fail(`${rel}: cannot read file`);
    return '';
  }
  return fs.readFileSync(abs, 'utf8');
}

// --- 1) CF_LIMITS.md source registry -----------------------------------------
const CF_LIMITS = 'docs/CF_LIMITS.md';
const cfText = read(CF_LIMITS);
const cfLines = cfText.split(/\r?\n/);

const registryIds = new Set();
const registryRowRe = /^\|\s*`\[S(\d+)\]`\s*\|/;
// Parse the registry table itself (located by its own header row inside §۷), not
// every table in the file: later tables may legitimately open a row with a source
// id, such as the per-source evidence log added in 2026-09-28.
const registryHeaderIdx = cfLines.findIndex((line) => /^\|\s*شناسه\s*\|/.test(line) && /لینک/.test(line));
if (registryHeaderIdx === -1) {
  fail(`${CF_LIMITS}: the §۷ source-registry table header ('| شناسه | منبع | لینک | …') was not found`);
}
for (let i = registryHeaderIdx + 1; registryHeaderIdx !== -1 && i < cfLines.length; i += 1) {
  const line = cfLines[i];
  if (!line.startsWith('|')) break; // end of the registry table
  if (/^\|[\s|:-]+\|$/.test(line)) continue; // header separator
  const m = line.match(registryRowRe);
  if (!m) {
    fail(`${CF_LIMITS}:${i + 1}: malformed source registry row (expected '| \`[Sn]\` | … | link | date | date |'): ${line.trim().slice(0, 60)}`);
    continue;
  }
  registryIds.add(`[S${m[1]}]`);
  if (!/https:\/\//.test(line)) fail(`${CF_LIMITS}:${i + 1}: source registry row ${m[0].trim()} has no official URL`);
  const dates = line.match(/\b\d{4}-\d{2}-\d{2}\b/g) || [];
  if (dates.length < 2) {
    fail(`${CF_LIMITS}:${i + 1}: source registry row ${m[0].trim()} must keep both the document date and our 'Last verified' date`);
  }
}
if (registryIds.size === 0) fail(`${CF_LIMITS}: no [Sn] source rows found in the §۷ registry`);

const referencedIds = new Set(cfText.match(/\[S\d+\]/g) || []);
for (const id of referencedIds) {
  if (!registryIds.has(id)) fail(`${CF_LIMITS}: reference ${id} is not defined in the source registry (§۷)`);
}

// every `## ` section must declare its own Last verified (single-source tables
// inherit the section-level Source/Last verified block).
const sectionStarts = [];
cfLines.forEach((line, i) => {
  if (/^##\s+\S/.test(line)) sectionStarts.push({ line: i, heading: line.trim() });
});
if (sectionStarts.length === 0) fail(`${CF_LIMITS}: no '## ' sections found — cannot verify the Last verified contract`);
sectionStarts.forEach((section, idx) => {
  const end = idx + 1 < sectionStarts.length ? sectionStarts[idx + 1].line : cfLines.length;
  const body = cfLines.slice(section.line + 1, end).join('\n');
  if (!/\*\*Last verified:\*\*/.test(body)) {
    fail(`${CF_LIMITS}:${section.line + 1}: section "${section.heading}" has no '**Last verified:**' line`);
  }
});

// --- 2) bindings parity -------------------------------------------------------
const WRANGLER = 'wrangler.v2.jsonc';
const DTS = 'worker-configuration.d.ts';
const DIAGRAMS = 'docs/architecture-diagrams.md';

const wranglerText = read(WRANGLER);
const configBindings = new Set();
for (const m of wranglerText.matchAll(/"binding":\s*"([A-Za-z0-9_]+)"/g)) configBindings.add(m[1]);
if (configBindings.size === 0) fail(`${WRANGLER}: no "binding" declarations found`);

const dtsText = read(DTS);
for (const name of configBindings) {
  if (!new RegExp(`\\b${name}\\s*:`).test(dtsText)) {
    fail(`${DTS}: binding '${name}' declared in ${WRANGLER} is missing from the generated types — run \`bun run cf-typegen\``);
  }
}

const diagramsText = read(DIAGRAMS);
const diagramsLines = diagramsText.split(/\r?\n/);
const tableStart = diagramsLines.findIndex((line) => /^###\s+Bindings\b/.test(line));
const tableBindings = new Set();
if (tableStart === -1) {
  fail(`${DIAGRAMS}: '### Bindings (light Worker)' section not found`);
} else {
  for (let i = tableStart + 1; i < diagramsLines.length; i += 1) {
    const line = diagramsLines[i];
    if (/^#{2,3}\s/.test(line)) break;
    const m = line.match(/^\|\s*`([A-Za-z0-9_]+)`\s*\|/);
    if (m) tableBindings.add(m[1]);
  }
  if (tableBindings.size === 0) fail(`${DIAGRAMS}: bindings table has no rows — cannot verify binding parity`);
}
// R2 stays unbound on purpose (error 10042); the docs must keep saying so, so that
// re-adding the binding is a deliberate, documented change rather than a silent one.
if (!/TEMPLATES_BUCKET/.test(diagramsText)) {
  fail(`${DIAGRAMS}: the R2 note must keep documenting that TEMPLATES_BUCKET is intentionally unbound`);
}
for (const name of configBindings) {
  if (!tableBindings.has(name)) {
    fail(`${DIAGRAMS}: binding '${name}' is declared in ${WRANGLER} but missing from the bindings table`);
  }
}
for (const name of tableBindings) {
  if (!configBindings.has(name)) {
    fail(`${WRANGLER}: bindings table documents '${name}' but the deploy config does not declare it`);
  }
}

if (errors.length) {
  console.error('docs invariants validation FAILED:');
  for (const e of errors) console.error(' - ' + e);
  process.exit(1);
}
console.log(
  `docs-invariants: OK (CF_LIMITS: ${sectionStarts.length} sections, ${registryIds.size} sources, ${referencedIds.size} ids referenced; ` +
    `bindings: ${configBindings.size} in parity across ${WRANGLER} ↔ ${DIAGRAMS} ↔ ${DTS}).`,
);
