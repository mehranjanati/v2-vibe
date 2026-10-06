# Autonomous Software Factory (PROPOSED — Phase M5)

> **Scope:** the long-horizon capability where the platform creates and
> deploys business applications, agents and functions from natural-language
> requirements. Vision: [`00-product-vision.md`](00-product-vision.md).
> Roadmap position: phase M5 in
> [`03-product-roadmap.md`](03-product-roadmap.md). Engineering owner: P6 in
> [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md).
>
> **Status:** `planned` (destination) — NOT part of the MVP. What exists
> today is the App Builder reference workload described in
> [`DEV_STATUS.md`](DEV_STATUS.md). Last reviewed: 2026-10-06.

## Goal

> The tenant states a business need; the platform synthesizes the
> application, agents and functions needed, deploys them into the tenant's
> space, operates them, evaluates them and repairs/improves them.

This generalizes today's proven loop — natural-language request → plan
approval → coordinator/coder/reviewer generation → VFS → deploy
([`MULTI_AGENT.md`](MULTI_AGENT.md)) — from building static apps to
building **operating business systems** (application layer +
capability layer + agent behavior, per
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

## What M5 adds (destination scope)

- Generated **business functions** (typed, manifest-registered —
  [`06-functions-and-tools.md`](06-functions-and-tools.md)) with mandatory
  review before tenant activation.
- Generated **agents/workflows** composed from the capability registry.
- Continuous evaluate/repair loop over deployed systems (engineering P6
  factory-loop tasks in [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md)).
- Tenant-scoped deployment and rollback of generated systems
  ([`07-multitenancy-security.md`](07-multitenancy-security.md)).

## Preconditions

Phases M1–M4: proven wedge, automation platform, Business OS modules and
marketplace supply — plus engineering P2 (compiler), P3 (durable runtime),
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
