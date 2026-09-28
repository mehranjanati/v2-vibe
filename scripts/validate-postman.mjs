// T3 validator: explicit two-plane Postman contract.
// Usage: node scripts/validate-postman.mjs [--collection <path>] [--environment <path>] [--contract <path>]
// Exit non-zero on any violation.
import fs from 'node:fs';
import path from 'node:path';

import { fileURLToPath } from 'node:url';
const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
function arg(name, fallback) {
  const i = process.argv.indexOf(name);
  return i >= 0 && process.argv[i + 1] ? process.argv[i + 1] : fallback;
}
const collectionPath = arg('--collection', path.join(root, 'docs/v1dev-api-collection.postman_collection.json'));
const environmentPath = arg('--environment', path.join(root, 'docs/v1dev-environment.postman_environment.json'));
const contractPath = arg('--contract', path.join(root, 'scripts/postman-route-contract.json'));

const errors = [];
const fail = (msg) => errors.push(msg);
const load = (p, label) => {
  try { return JSON.parse(fs.readFileSync(p, 'utf8')); }
  catch (e) { fail(`${label}: cannot parse ${p}: ${e.message}`); return null; }
};
const collection = load(collectionPath, 'collection');
const environment = load(environmentPath, 'environment');
const contract = load(contractPath, 'contract');
if (!collection || !environment || !contract) { console.error(errors.join('\n')); process.exit(1); }

// --- environment checks ---
const envKeys = new Map((environment.values || []).filter(v => v.enabled !== false).map(v => [v.key, v]));
for (const k of (contract.requiredEnv || ['workerUrl', 'controlUrl'])) {
  if (!envKeys.has(k)) fail(`environment: required variable '${k}' missing or disabled`);
}
for (const k of (contract.deprecatedEnv || ['baseUrl', 'localUrl'])) {
  if (envKeys.has(k)) fail(`environment: deprecated variable '${k}' must not be present/enabled`);
}

// --- collect active requests ---
function walk(items, folder, out) {
  for (const it of (items || [])) {
    if (it.item) walk(it.item, folder ? folder + ' / ' + it.name : it.name, out);
    else out.push({ folder: folder || '(root)', item: it });
  }
}
const active = [];
walk(collection.item, '', active);
if (active.length === 0) fail('collection: no active requests found');

const textOf = (o) => JSON.stringify(o || {});
const planeMarker = (item) => {
  const d = (item.request && item.request.description) || item.description || '';
  const m = String(d).match(/plane:\s*(worker|control)/i);
  return m ? m[1].toLowerCase() : null;
};
const rawUrl = (item) => {
  const u = item.request && item.request.url;
  if (!u) return '';
  if (typeof u === 'string') return u;
  return u.raw || '';
};

const allowedVars = new Set([...(environment.values || []).map(v => v.key), 'workerUrl', 'controlUrl', 'csrf_token', 'user_id', 'session_id', 'agent_id', 'app_id', 'websocket_url', 'workflow_id']);
const normPath = (raw) => {
  let p = String(raw || '').replace(/\{\{[^}]+\}\}/g, ':param');
  p = p.replace(/^https?:\/\/[^/]*/, '');
  const q = p.indexOf('?'); if (q >= 0) p = p.slice(0, q);
  const h = p.indexOf('#'); if (h >= 0) p = p.slice(0, h);
  return p.replace(/\/+$/, '') || '/';
};
const deadPatterns = (contract.dead_on_both_planes || []).map(d => {
  const [m, ...rest] = String(d).split(' ');
  return { method: m, path: rest.join(' ') };
});
function pathMatches(pattern, actual) {
  const pp = pattern.split('/').filter(Boolean);
  const ap = actual.split('/').filter(Boolean);
  if (pp.length !== ap.length) return false;
  return pp.every((seg, i) => seg.startsWith(':') || seg === ap[i]);
}

let agentNdjsonOk = false;
let appChainOk = false;
const seen = new Set();
for (const { folder, item } of active) {
  const label = `${folder} / ${item.name}`;
  const raw = rawUrl(item);
  const method = (item.request && item.request.method) || 'GET';
  const plane = planeMarker(item);
  if (!plane) fail(`${label}: missing explicit plane marker (description must start with 'plane: worker' or 'plane: control')`);
  if (plane && !['worker', 'control'].includes(plane)) fail(`${label}: conflicting plane marker '${plane}'`);
  const key = method + ' ' + raw;
  if (seen.has(key)) fail(`${label}: duplicate request definition '${key}'`);
  seen.add(key);
  if (/\{\{\s*baseUrl\s*\}\}/.test(raw)) fail(`${label}: uses deprecated {{baseUrl}} (use plane base variable)`);
  if (/\{\{\s*localUrl\s*\}\}/.test(raw)) fail(`${label}: depends on localUrl`);
  if (plane === 'worker' && !raw.includes('{{workerUrl}}')) fail(`${label}: plane=worker but URL does not use {{workerUrl}}`);
  if (plane === 'control' && !raw.includes('{{controlUrl}}')) fail(`${label}: plane=control but URL does not use {{controlUrl}}`);
  if (plane === 'worker' && raw.includes('{{controlUrl}}')) fail(`${label}: plane=worker but URL uses {{controlUrl}}`);
  if (plane === 'control' && raw.includes('{{workerUrl}}')) fail(`${label}: plane=control but URL uses {{workerUrl}}`);
  // unresolved variables
  for (const m of String(raw).matchAll(/\{\{([^}]+)\}\}/g)) {
    const v = m[1].trim();
    if (!allowedVars.has(v) && !(environment.values || []).some(e => e.key === v)) fail(`${label}: unresolved variable {{${v}}}`);
  }
  // dead routes
  const np = normPath(raw);
  for (const d of deadPatterns) {
    if (d.method !== method) continue;
    if (pathMatches(d.path, np)) fail(`${label}: targets dead-on-both-planes route '${d.method} ${d.path}'`);
  }
  const allText = textOf(item);
  if (/\{\{\s*baseUrl\s*\}\}/.test(allText) && !/plane:\s*(worker|control)/i.test(allText.replace(/\{\{\s*baseUrl\s*\}\}/g, ''))) fail(`${label}: script/helper still references baseUrl`);
  if (/pm\.environment\.get\(['"]baseUrl['"]\)/.test(allText)) fail(`${label}: script reads pm.environment.get('baseUrl')`);
  // chain scripts
  if (/\/api\/agent$/.test(np) && method === 'POST') {
    if (allText.includes('split') && allText.includes('JSON.parse') && allText.includes('agentId')) agentNdjsonOk = true;
    else fail(`${label}: POST /api/agent must implement documented NDJSON multi-line parsing (split by newline + JSON.parse per line + agentId extraction)`);
  }
  if (/\/api\/apps\//.test(np) && method === 'GET' && /app_id/.test(allText)) appChainOk = true;
}
if (!agentNdjsonOk) fail('collection: POST /api/agent NDJSON agent_id extraction script not found');
if (!appChainOk) fail('collection: app_id response-chain script (GET app details) not found');

// collection-level prerequest must be worker-aware
const preText = textOf(collection.event);
if (/pm\.environment\.get\(['"]baseUrl['"]\)/.test(preText)) fail('collection prerequest: must not obtain CSRF from baseUrl (use workerUrl)');
if (!/workerUrl/.test(preText)) fail('collection prerequest: CSRF acquisition must be explicitly Worker-aware (reference workerUrl)');
// no self-referential collection variable
for (const v of (collection.variable || [])) {
  if (v.key === 'baseUrl') fail('collection: self-referential/legacy collection variable baseUrl must be removed');
  if (v.value && String(v.value).includes('{{baseUrl}}')) fail(`collection: variable '${v.key}' self-references {{baseUrl}}`);
}

// --- live route cross-check (T16) ---
// `<plane>.live` is the docs' route map; until now nothing re-derived it from the
// sources, so it drifted silently (e.g. the Worker entry comment and this manifest
// both claimed a `/health` route that only the Go control plane serves). The
// registrations below are the source of truth: Fiber `app.Get("/x")` in
// `backend/pkg/api/*.go` and Hono `app.get('/x')` in `worker/light/lightApp.ts`.
// 503 stubs are declared as patterns (`/api/agent`, `/api/agent/*`) and belong to
// `not_available_503` rather than `<plane>.live`.
const notAvailablePaths = (contract.worker && contract.worker.not_available_503) || [];
function isNotAvailablePath(routePath) {
  return notAvailablePaths.some((pat) => {
    if (pat === routePath) return true;
    if (pat.endsWith('/*')) {
      const prefix = pat.slice(0, -1);
      return routePath === pat.slice(0, -2) || routePath.startsWith(prefix);
    }
    return false;
  });
}

function readSource(rel) {
  const p = path.join(root, rel);
  if (!fs.existsSync(p)) {
    fail(`route-contract: missing source file ${rel}`);
    return '';
  }
  return fs.readFileSync(p, 'utf8');
}

const METHOD_BY_NAME = new Map([
  ['get', 'GET'],
  ['post', 'POST'],
  ['put', 'PUT'],
  ['patch', 'PATCH'],
  ['delete', 'DELETE'],
  ['options', 'OPTIONS'],
  ['all', 'ALL'],
]);
const REGISTRATION_RE = /app\.(get|post|put|patch|delete|options|all)\s*\(\s*(['"])([^'"]+)\2/gi;

function registrationsIn(source) {
  const out = new Set();
  for (const m of source.matchAll(REGISTRATION_RE)) {
    out.add(`${METHOD_BY_NAME.get(m[1].toLowerCase())} ${m[3]}`);
  }
  return out;
}

const controlSources = [];
const controlDir = path.join(root, 'backend/pkg/api');
if (!fs.existsSync(controlDir)) {
  fail('route-contract: backend/pkg/api is missing — cannot verify the control plane routes');
} else {
  for (const file of fs.readdirSync(controlDir).filter((f) => f.endsWith('.go') && !f.endsWith('_test.go')).sort()) {
    const rel = `backend/pkg/api/${file}`;
    controlSources.push({ rel, text: readSource(rel) });
  }
  if (controlSources.length === 0) fail('route-contract: no Go sources found under backend/pkg/api');
}
const controlInCode = new Set();
for (const { text } of controlSources) {
  for (const r of registrationsIn(text)) controlInCode.add(r);
}
if (controlInCode.size === 0) {
  fail('route-contract: no Fiber route registrations found in backend/pkg/api/*.go — cannot verify the control plane');
}

const workerSource = readSource('worker/light/lightApp.ts');
const workerPathsInCode = new Set();
const workerRegistrationsInCode = registrationsIn(workerSource);
if (workerRegistrationsInCode.size === 0) {
  fail('route-contract: no Hono route registrations found in worker/light/lightApp.ts — cannot verify the worker plane');
}
for (const r of workerRegistrationsInCode) {
  const method = r.slice(0, r.indexOf(' '));
  const routePath = r.slice(r.indexOf(' ') + 1);
  // Catch-alls (`*`, `/api/*`) and the explicit 503 stubs are covered by the
  // `not_available_503` section instead of `<plane>.live`.
  if (routePath === '*' || routePath === '/api/*') continue;
  if (isNotAvailablePath(routePath)) continue;
  workerPathsInCode.add(`${method} ${routePath}`);
}

const contractWorkerLive = new Set((contract.worker && contract.worker.live) || []);
const contractControlLive = new Set((contract.control && contract.control.live) || []);
for (const r of controlInCode) if (r.startsWith('ALL ')) controlInCode.delete(r);

function compareLive(plane, contractLive, codeLive) {
  for (const r of contractLive) {
    if (!codeLive.has(r)) {
      fail(`route-contract: ${plane}.live lists '${r}' but no matching registration exists in the sources (stale manifest entry — fix the manifest or register the route)`);
    }
  }
  for (const r of codeLive) {
    if (!contractLive.has(r)) {
      fail(`route-contract: ${plane} code registers '${r}' but the manifest does not list it (add it to '${plane}.live', or to 'not_available_503' when it is a 503 stub)`);
    }
  }
}
compareLive('worker', contractWorkerLive, workerPathsInCode);
compareLive('control', contractControlLive, controlInCode);

if (errors.length) { console.error('Postman contract validation FAILED:'); for (const e of errors) console.error(' - ' + e); process.exit(1); }
console.log(`Postman contract validation PASSED: ${active.length} active requests, workerUrl+controlUrl present, plane markers OK, manifest matches code (${workerPathsInCode.size} worker + ${controlInCode.size} control routes, last_verified ${contract.last_verified || 'unset'}).`);
