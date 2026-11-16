# Autonomous Software Factory (PROPOSED — Phase M6)

> **Scope:** the long-horizon capability where the platform creates and
> deploys agentic systems, agents, functions, and applications from
> natural-language
> requirements. Vision: [`00-product-vision.md`](00-product-vision.md).
> Roadmap position: phase M6 in
> [`03-product-roadmap.md`](03-product-roadmap.md). Canonical strategy:
> [`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md).
> Engineering owner: P6 in
> [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md).
>
> **Status:** `planned` (destination, M6) — NOT part of the MVP. What exists
> today is the App Builder reference workload described in
> [`DEV_STATUS.md`](DEV_STATUS.md). The App Builder is preserved as the
> reference workload and future factory capability — it never defines the
> whole platform identity. Last reviewed: 2026-11-15.

## Goal

> The user states a requirement in natural language; the platform synthesizes the
> agents, functions, and applications needed, deploys them into the governed
> runtime, operates them, evaluates them and repairs/improves them —
> with human approval at sensitive side-effect boundaries.

This generalizes today's proven loop — natural-language request → plan
approval → coordinator/coder/reviewer generation → VFS → deploy
([`MULTI_AGENT.md`](MULTI_AGENT.md)) — from building static apps to
building **operating agentic systems** (agents/teams +
capability layer + agent behavior + runtime, per
[`04-system-architecture.md`](04-system-architecture.md)).

## What today's App Builder already proves (the seed)

- Intent → ExecutionPlan synthesis with human approval
  ([`MULTI_AGENT.md`](MULTI_AGENT.md) execution trace).
- Multi-agent generation with least-privilege tools and review gate.
- Lineage: generations, files and audits per run (engineering P1 lineage).
- Deploy pipeline (Cloudflare Pages today; the layer interface is what
  matters, per vendor replaceability).
- Design-quality direction via the proposed Dynamic Design Skill Layer
  ([`architecture/dynamic-design-skill-layer.md`](architecture/dynamic-design-skill-layer.md)).

## What M6 adds (destination scope)

- Generated **agents and agent teams** (typed, manifest-registered —
  [`05-agent-runtime.md`](05-agent-runtime.md)) with mandatory
  review before activation.
- Generated **capabilities** (typed, manifest-registered —
  [`06-functions-and-tools.md`](06-functions-and-tools.md)) with mandatory
  review before tenant activation.
- Generated **applications** composed from the capability registry.
- Automated tests, review, deployment, evaluation, repair, rollback.
- Continuous evaluate/repair loop over deployed systems (engineering P6
  factory-loop tasks in [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md)).
- Tenant-scoped deployment and rollback of generated systems
  ([`07-multitenancy-security.md`](07-multitenancy-security.md)).
- **Proof-of-run trust anchors (W3.3, gated to M5–M6):** a hash of the
  generation/run record anchored on chain while the record itself stays in the
  platform data layer, so a factory-built system is verifiable without custody
  transfer ([`11-aaas-web3-strategy.md`](11-aaas-web3-strategy.md)). No chain is
  chosen and no implementation is authorized here.

## Preconditions

Phases M0–M5: proven Agent Builder, multi-agent systems, runtime platform,
developer ecosystem, operations/governance, and
registry/marketplace supply — plus engineering P2 (compiler), P3 (durable
runtime),
P4 (workbench) gates. The factory is the *last* phase because it composes
everything before it; building it first is the "parallel modules" mistake
the roadmap forbids ([`03-product-roadmap.md`](03-product-roadmap.md)).

## Non-goals

- No autonomous deployment without human approval gates at side-effect
  boundaries.
- No generation that bypasses tenant scoping, audit or the function
  requirements of [`06-functions-and-tools.md`](06-functions-and-tools.md).
- No claim that factory capabilities exist today beyond the App Builder
  reference workload.
