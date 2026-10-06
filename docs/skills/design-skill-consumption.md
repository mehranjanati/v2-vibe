# Design Skill Consumption Guide (PROPOSED)

> **Scope:** how coordinator/coder/reviewer are *expected* (future) to consume
> design skills. Nothing here is live behavior.
> Canonical architecture:
> [`../architecture/dynamic-design-skill-layer.md`](../architecture/dynamic-design-skill-layer.md).
> Authoring: [`design-skill-authoring.md`](design-skill-authoring.md).
> Current behavior: [`../MULTI_AGENT.md`](../MULTI_AGENT.md),
> [`../llm.md`](../llm.md).
>
> **Status:** `proposed` — Last reviewed: 2026-10-05.

## How a user intent maps to skills (`Proposed`)

The coordinator derives an intent cluster per file step from the user
request + plan (e.g. "SaaS admin dashboard" → dashboard cluster;
"coffee-shop landing page" → landing-page cluster). The mapping is
metadata-driven (intent keywords, file type, plan design block), not
prompt-embedded. The live coordinator contract this extends is
`backend/skills/00_coordinator.md`.

## How coordinator may select skills

Per file step the coordinator names a small skill set (2–4 skills),
never pasting bodies. Example (`Proposed`): dashboard step →
`ui-fundamentals` + `dashboard`. Landing-page step →
`ui-fundamentals` + `landing-page`. Unrelated sets must not mix:
dashboard density rules never enter landing-page context.

## How registry resolves skill metadata

The future registry extension (of `backend/pkg/skills/registry.go`)
resolves names → pinned versions → content, or returns an explicit
missing-skill signal. Current `Load` semantics
(`backend/pkg/skills/load.go:15`) remain the fallback: base prompts
alone must always produce valid output.

## How skill content is composed into coder context

The context composer (proposed, not built) selects the minimal
relevant slice under a per-call token budget: SKILL.md core guidance
first, then resources, then at most one example per rule. The coder
receives base prompt (`backend/skills/02_coder.md`) + slice and
writes to VFS. Guidance never overrides the coder output contract
(`backend/skills/02_coder.md:121`): entire response is file content,
no fences, no prose.

## How reviewer consumes the same design intent

The reviewer receives the same selected slice + task requirements and
judges conformance. Its live input (paths + requirements per
`backend/skills/03_reviewer.md:7`) is extended, not replaced. Its live
output contract (`APPROVE` / `REQUEST_CHANGES` per
`backend/skills/03_reviewer.md:29`) is unchanged.

## Guidance vs hard constraints

- **Coder:** hard constraints (wiring, tokens, syntax, static-SPA
  rules from `backend/skills/02_coder.md:138`) always win over skill
  taste guidance. When they conflict, follow the constraint and note
  nothing (output contract forbids prose).
- **Reviewer:** subjective guidance (density, taste) informs but never
  blocks alone — consistent with the live "strict but fair" rule
  (`backend/skills/03_reviewer.md:39`). Only rendering, wiring, or
  stated-requirement breaks yield `REQUEST_CHANGES`.

## Missing or conflicting skills

- **Missing:** warn + continue on base prompt, unless marked required
  (then fail the step explicitly). Never substitute an unrelated skill
  silently.
- **Conflicting:** project-local rules outrank generic external
  guidance; the run log records the override. Reviewer must not flag
  the winning behavior as a defect.

## Avoiding irrelevant context

Prefer fewer, higher-signal skills; exclude rather than truncate;
log resolved names + versions + token cost per call (`Future`
observability). If in doubt, omit: the base prompts
(`backend/skills/02_coder.md`, `backend/skills/03_reviewer.md`) are
the safe default.
