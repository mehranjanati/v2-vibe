# DOCS UPDATE REPORT — Outcome-First Refactor (2026-10-01)

> Evidence report for the documentation/product-architecture refactor adopting
> the Outcome-First Agentic Software Platform thesis. No runtime code,
> migrations, workflows, or production behavior changed; validation scripts
> were not modified. Claims cite the runs in Verification below.

## Files created

- `docs/PRODUCT_THESIS.md` — canonical product strategy (Problem, Target
  user, Outcome-first model, What is/is-not, Core abstractions,
  Why-graph-secondary, Why-app-builder-is-capability, Agentic system model,
  Market entry, Reference systems, Competitive landscape, Moat hypotheses,
  Business model, Strategic non-goals). Competitor claims sourced to vendor
  docs/sites checked 2026-10-01.
- `docs/DEV_STATUS.md` — single current-state dashboard (14 required
  sections incl. In Progress, Obsolete Work, Known Drift, Roadmap guide).
- `docs/DOCS_UPDATE_REPORT.md` — this file.

## Files modified

- `docs/DEV_CHECKLIST.md` — canonical ordered backlog: normative
  Historical ID → Target Phase Map, full task-template fields on P0.3–P0.9,
  target-phase headers + platform readings on every section, and the whole
  checklist converted to English (historical IDs and evidence verbatim),
  rules table converted (no backfilled dates).
- `docs/DEV_TASKS_SPEC.md` — task-spec quality contract (12 required
  fields + quality rule) plus target-phase spec index.
- `docs/DEV_SPEC_P1A.md`, `DEV_SPEC_P1B.md`, `DEV_SPEC_P2.md`,
  `DEV_SPEC_P3P4.md`, `DEV_SPEC_P5.md`, `DEV_SPEC_P6.md` — English titles
  and headings, target-phase placement banners; implementation-contract
  bodies preserved verbatim (see `DEV_STATUS.md` drift item 4 for the
  reframing policy), historical IDs intact.
- `docs/llm.md`, `docs/MULTI_AGENT.md`, `docs/architecture-diagrams.md` —
  cross-links to thesis/status; diagrams gained a labeled **PLANNED** target
  sketch (bindings table untouched — parity gate green).
- `README.md` — What-is reframed + reading path.
- `AGENTS.md` — fixed stale "`space/` deletion still uncommitted" claim
  (committed in P1.0); added thesis/status/checklist navigation.

## Files archived/moved

None. `docs/archive/**` frozen; `docs/DOCS_AUDIT_BACKLOG.md` stays closed
history, referenced — not rewritten.

## Major documentation changes

1. One canonical thesis; Outcome → System synthesis is the primary path.
2. One dashboard separating CURRENT / TARGET / `verified` /
   `implemented_unverified` / `planned` / `blocked` / `obsolete`.
3. One backlog with the legacy→target map (P0.1/P0.2→P1, P1.3.4–P1.3.6→P5,
   P1.4→P5, P1.5→P4, P1.6/P1.8/P1.9→P2, P1.10→P4, P2.1–P2.6→P1,
   P3.1–P3.4→P6, P5.x→P5 reference system, P6.x→P4/P3).
4. Graph secondary (inspection/debugging); App Builder = capability +
   reference workload; Cloudflare = substrate, never the moat.

## Current verified capabilities

P0.1 skeleton, P0.2 DeepAgent (Eino v0.9.21) + per-role models, P1.0.0 D1
tooling, P1.0.1–P1.0.3 cleanup, P1.1 lineage tables + migration `0011`,
P1.2 recorder (7+4 tests), P1.3.0 D1-CAS decision, P1.3.1/P1.3.2 hooks
(4 tests). Evidence: `go vet/test` green, `docs:check` green (187 refs).

## Current unverified capabilities

P1.3.3 author threading (E2E author test + fork flag + editor/import
authors missing); P0.2 team E2E approve path (manual-only).
`docs:check` 187 refs green; `typecheck` clean; `lint` 0 errors / 3
warnings; `build` green (~6.9s); `go vet` clean; `go test ./...` all ok.

## New blockers

None new. Standing: P1.1.3 local migrate (macOS < 13.5 — Linux/CI);
P0.9 identity decision (unblocks P0.4/P0.5 mechanism; provisional checks
may land first).

## Resolved inconsistencies

- `AGENTS.md` stale `space/` claim → corrected + drift-tracked.
- Backlog phase table mixing legacy/new names → target table.
- Missing dashboard sections → added honestly ("nothing in_progress",
  "nothing obsolete").
- Missing thesis / landscape / domain model → created once in
  `PRODUCT_THESIS.md`, referenced elsewhere (no duplicate definitions).
- Spec files with no phase placement → bannered, no renumbering.

## Remaining risks

`DEV_STATUS.md` Known Architecture Risks 1–8 plus drift items 1–4
(leftover `space/` dirs, `llm.md` stamp, ID-namespace note, spec policy).

## Recommended first implementation task

**P0.3 control-plane auth boundary**: every mutating control route enforces
an explicit check. Unblocks P0.4, P0.5, P0.7 and all multi-tenant P2–P6
work. Verification: per-route Go tests + `go vet ./... && go test ./...`.

## Verification commands executed and results

- `bun run docs:check` → spec-refs OK (187 refs, 19 files); Postman PASSED
  (12 requests, manifest matches code: 21 worker + 26 control routes);
  invariants OK (8 sections, 7 sources; 5 bindings in parity).
- `bun run typecheck` → clean. `bun run lint` → 0 errors, 3 warnings
  (pre-existing react-refresh notes). `bun run build` → ~6.9s, chunk
  warnings only.
- `cd backend && go vet ./...` → clean. `go test ./...` → all packages ok.
- Workerd suites defer to CI on this machine class per repo policy.

