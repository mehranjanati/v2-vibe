# ADR: Dynamic Design Skill Layer (PROPOSED)

> **Status:** `proposed` — decision record for a not-yet-implemented
> architecture. Companion: [`dynamic-design-skill-layer.md`](dynamic-design-skill-layer.md).
> Current behavior: [`../MULTI_AGENT.md`](../MULTI_AGENT.md),
> [`../llm.md`](../llm.md).
>
> Last reviewed: 2026-10-05.

## Context

Each agent role loads one static verbatim prompt
(`backend/pkg/skills/load.go:15`,
`backend/pkg/skills/registry.go:56`). UI quality needs per-intent
design knowledge that does not fit a single static file. External
design-skill repositories exist as knowledge sources, and
google-labs-code/stitch-skills demonstrates a packaging model
(SKILL.md + resources + examples + scripts) worth adopting
structurally.

Corrections to the brief: prompts live in `backend/skills/` (not
`backend/pkg/skills/`); roles include planner
(`backend/pkg/skills/registry.go:56`); the frontend is a React SPA,
not SvelteKit; generated apps forbid Tailwind CDNs/UI kits
(`backend/skills/02_coder.md:151`).

## Decision

Adopt a `Proposed` Dynamic Design Skill Layer: small static base
prompts + dynamically resolved, versioned, locally-vendored design
skills composed per request. External repositories are knowledge
sources, never runtime dependencies. Knowledge (guidance),
enforcement (executable checks), and review (agent verdict) stay
separate concerns.

## Decision drivers

- Per-intent relevance without fixed context cost.
- Local, reviewable, versioned artifacts over opaque runtime fetching.
- No new runtime service; compatible with the DeepAgent
  coordinator/coder/reviewer shape (`docs/MULTI_AGENT.md:41`).
- Executable validation where possible; prose where judgment lives.

## Chosen architecture

Intent → coordinator → skill resolver → registry (metadata +
content) → context composer → coder → VFS → validators → reviewer
against the same selected guidance. Skill interior shape follows the
stitch-skills model (SKILL.md, resources, examples, scripts).
Project-local rules outrank generic external guidance.

## Alternatives considered

1. **Monolithic coder prompt.** Rejected: fixed context cost, leaks
   irrelevant rules, unreviewable growth.
2. **Runtime framework dependency on an external repo.** Rejected:
   opaque updates, network dependence, new service surface.
3. **Blind vendor of whole repositories.** Rejected: unscoped,
   unattributed, unreviewable.
4. **Permanent injection of all design rules.** Rejected: same cost
   problem as the monolith.
5. **Network fetch at generation time.** Rejected: non-reproducible,
   unreviewable, security surface.

## Rejected alternatives (summary)

All rejected options trade away versioning, reviewability, or
context economy — the three properties this decision optimizes for.

## Consequences

- Positive: relevant guidance per intent, bounded context, auditable
  provenance, reviewer judging against stated intent.
- Negative: curation/review burden, resolver-selection risk, skill-rot
  risk, per-call composition complexity (future).

## Risks

- Mis-selection noise; stale skills; license/content drift upstream;
  over-scoped skills re-creating the monolith. Mitigations:
  narrow scopes, version pins, human review on import, selection
  logging (`Future` observability).

## Future migration points

- Pilot one distilled skill as Markdown; registry metadata behind a
  flag; resolver/composer on one intent; graduate prose checks to
  scripts. Live `Load` semantics
  (`backend/pkg/skills/load.go:15`) remain the fallback until then.
