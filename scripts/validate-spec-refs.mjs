// T8 validator: every `file:line` reference inside the docs tree must use a full
// repo-relative path and must point at a line that exists in that file.
//
// Usage:
//   node scripts/validate-spec-refs.mjs
//   node scripts/validate-spec-refs.mjs --paths docs/DEV_SPEC_P1A.md docs/POSTMAN_COLLECTION_README.md
//
// Default scope: every `docs/**/*.md` except `docs/archive/**` (frozen history)
// and `docs/DOCS_AUDIT_BACKLOG.md` (quotes the pre-fix defects on purpose).
// `--paths <file…>` overrides the scope with an explicit list.
//
// Exit non-zero on any violation.
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

import { fileURLToPath } from 'node:url';

const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
const docsDir = path.join(root, 'docs');

function argValues(name) {
  const argv = process.argv.slice(2);
  const out = [];
  for (let i = 0; i < argv.length; i += 1) {
    if (argv[i] !== name) continue;
    for (let j = i + 1; j < argv.length && !argv[j].startsWith('--'); j += 1) {
      out.push(argv[j]);
    }
  }
  return out;
}

const explicitPaths = argValues('--paths');

// Documents that are allowed to keep historical / unresolvable references:
// - `docs/DOCS_AUDIT_BACKLOG.md` quotes the pre-fix defects on purpose (it also
//   records the historical `README.md:116` reference from before docs audit
//   task T11, which restored README.md and re-applied that edit at line 157).
// - `docs/archive/**` holds frozen historical snapshots.
const IGNORED = new Set(['docs/DOCS_AUDIT_BACKLOG.md']);

function defaultTargets() {
  const out = [];
  const visit = (dir) => {
    const entries = fs
      .readdirSync(dir, { withFileTypes: true })
      .sort((a, b) => a.name.localeCompare(b.name));
    for (const entry of entries) {
      const abs = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        if (entry.name === 'archive') continue;
        visit(abs);
      } else if (entry.name.endsWith('.md')) {
        const rel = path.relative(root, abs);
        if (!IGNORED.has(rel)) out.push(rel);
      }
    }
  };
  visit(docsDir);
  return out;
}

const targets = explicitPaths.length > 0 ? explicitPaths : defaultTargets();

if (targets.length === 0) {
  console.error('validate-spec-refs: no spec files found to validate');
  process.exit(2);
}

// Extensions we treat as file references (mirrors the languages used by the specs).
const EXTENSIONS = ['go', 'ts', 'tsx', 'js', 'mjs', 'cjs', 'json', 'jsonc', 'sql', 'md'];
const REF_RE = new RegExp(
  `(^|[^A-Za-z0-9_/.-])((?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+\\.(?:${EXTENSIONS.join('|')})):((?:\\d+)(?:[-,]\\d+)*)`,
  'g',
);

const errors = [];
const fail = (msg) => errors.push(msg);

const SKIP_DIRS = new Set([
  'node_modules',
  '.git',
  '.wrangler',
  'dist',
  'bundled',
  'coverage',
  'vendor',
]);

function walk(dir, depth) {
  if (depth > 6) return [];
  const out = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name.startsWith('.') && entry.name !== '.github') continue;
    const abs = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (SKIP_DIRS.has(entry.name)) continue;
      out.push(...walk(abs, depth + 1));
    } else {
      out.push(path.relative(root, abs));
    }
  }
  return out;
}

function repoFileIndex() {
  try {
    return execFileSync('git', ['ls-files'], { cwd: root, encoding: 'utf8' })
      .split('\n')
      .filter(Boolean);
  } catch {
    return walk(root, 0);
  }
}

const fileIndex = repoFileIndex();
const lineCountCache = new Map();

function countLines(abs) {
  if (!lineCountCache.has(abs)) {
    const text = fs.readFileSync(abs, 'utf8');
    lineCountCache.set(abs, text === '' ? 0 : text.replace(/\n$/, '').split('\n').length);
  }
  return lineCountCache.get(abs);
}

let refCount = 0;

for (const target of targets) {
  const abs = path.join(root, target);
  if (!fs.existsSync(abs)) {
    fail(`${target}: cannot read file`);
    continue;
  }

  const contentLines = fs.readFileSync(abs, 'utf8').split(/\r?\n/);
  let fenced = false;

  contentLines.forEach((line, index) => {
    const lineNo = index + 1;
    if (/^\s*```/.test(line)) {
      fenced = !fenced;
      return;
    }
    if (fenced) return;

    REF_RE.lastIndex = 0;
    let match;
    while ((match = REF_RE.exec(line)) !== null) {
      const ref = match[2];
      const numbers = match[3];
      refCount += 1;
      const where = `${target}:${lineNo}`;
      const refAbs = path.join(root, ref);

      if (!fs.existsSync(refAbs) || !fs.statSync(refAbs).isFile()) {
        const candidates = ref.includes('/')
          ? []
          : fileIndex.filter((f) => path.basename(f) === ref).slice(0, 5);
        const hint = candidates.length > 0 ? ` — did you mean: ${candidates.join(', ')}` : '';
        fail(`${where}: unresolved reference '${ref}:${numbers}'${hint}`);
        continue;
      }

      const total = countLines(refAbs);
      if (numbers.includes('-')) {
        const [start, end] = numbers.split('-').map((n) => Number.parseInt(n, 10));
        if (start > end) fail(`${where}: reversed line range '${ref}:${numbers}'`);
      }
      for (const n of numbers.split(/[-,]/).map((value) => Number.parseInt(value, 10))) {
        if (!Number.isInteger(n) || n < 1 || n > total) {
          fail(`${where}: line ${n} out of range for ${ref} (file has ${total} lines)`);
        }
      }
    }
  });
}

if (errors.length > 0) {
  console.error(errors.join('\n'));
  console.error(
    `\nvalidate-spec-refs: ${errors.length} violation(s) in ${refCount} checked reference(s)`,
  );
  process.exit(1);
}

console.log(`validate-spec-refs: OK (${refCount} refs in ${targets.length} files)`);
