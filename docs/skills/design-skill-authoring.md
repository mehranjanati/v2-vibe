# Design Skill Authoring Guide (PROPOSED)

> **Scope:** how to create and organize design/UI skills for the
> *proposed* Dynamic Design Skill Layer. Nothing here is live.
> Canonical architecture:
> [`../architecture/dynamic-design-skill-layer.md`](../architecture/dynamic-design-skill-layer.md).
> Consumption: [`design-skill-consumption.md`](design-skill-consumption.md).
> Current prompts: `backend/skills/00_coordinator.md`,
> `backend/skills/01_planner.md`, `backend/skills/02_coder.md`,
> `backend/skills/03_reviewer.md`.
>
> **Status:** `proposed` — Last reviewed: 2026-10-05.

## What a skill is

A skill is a scoped, versioned, attributed unit of design knowledge
plus (optionally) executable checks — resolved by metadata and
composed into coder/reviewer context per request. It is not a
framework, not a component library, not a runtime.

## What belongs in SKILL.md

Front-matter (`Proposed` fields): `name`, `version` (semver),
`sources` (repo + commit/date), `scope` (intents/file types),
`priority` (override order), `requires` (other skill names).
Body: when-to-use / when-NOT-to-use, core guidance (short,
actionable), hard constraints vs soft guidance labeled as such.

## What belongs in resources

Tokens, layout patterns, accessibility checklists, state/empty-state
matrices. Reference material the composer may include selectively —
never required reading pasted wholesale.

## What belongs in examples

Minimal good-vs-bad snippets illustrating one rule each. Keep them
small; the composer includes at most one per rule under budget
pressure. No full-page dumps.

## What belongs in scripts

Executable validators the future pipeline can run (syntax, token
usage, a11y heuristics). Scripts enforce; prose advises. Scripts
must be sandboxed and secret-free.

## Naming conventions (`Proposed`)

Lowercase hyphenated: `ui-fundamentals`, `landing-page`,
`dashboard`, `form-builder`. Prefix `ui-` for cross-cutting
guidance; bare names for page-type specializations. Names are
registry keys — stable across versions.

## Scope boundaries

One skill = one intent cluster. `ui-fundamentals` (hierarchy,
spacing, contrast, focus states) applies broadly;
`dashboard` (tables, density, empty states) only to dashboards.
If two skills overlap heavily, merge them.

## Dependencies between skills

Declare via `requires` (e.g. `dashboard` requires
`ui-fundamentals`). The resolver loads transitively but the composer
still dedupes overlapping guidance. No cycles allowed.

## Versioning and provenance

Semver per skill; pin versions per generation for reproducibility.
Every distillation records source repo, commit/date, and what was
adapted (especially Tailwind guidance translated to vanilla CSS
tokens, since generated apps forbid Tailwind CDN per
`backend/skills/02_coder.md:151`).

## Review requirements

Imported knowledge lands like code: human review, attribution
check, scope check, conflict check against project-local rules
(which always win). No direct paste from upstream without review.

## When to create a specialized skill

Create `dashboard`, `landing-page`, `form-builder`, etc. when an
intent cluster has rules that would be noise elsewhere (table
density, hero composition, form validation patterns). Signal: the
same three-plus rules recur for that intent and misfire on others.

## When NOT to create a new skill

Do not create one-off, single-rule, or taste-only skills; fold them
into the closest existing skill or leave them to reviewer judgment.
Do not duplicate the live base prompts
(`backend/skills/02_coder.md`, `backend/skills/03_reviewer.md`) —
skills extend them per intent.
