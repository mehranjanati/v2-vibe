# Product Vision — VibeSDK Open Agentic Platform

> **Scope:** canonical product vision and positioning. This is the strategy
> layer on top of the platform thesis ([`PRODUCT_THESIS.md`](PRODUCT_THESIS.md) —
> agentic system model, moats, competitive analysis, all still canonical).
> Current reality: [`DEV_STATUS.md`](DEV_STATUS.md). Product roadmap:
> [`03-product-roadmap.md`](03-product-roadmap.md). Decision log:
> [`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md).
> Canonical strategy apex: [`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md).
>
> **Status:** `adopted` — Agentic Platform direction adopted 2026-11-15
> (decision D7). It supersedes the 2026-10-06 AI-Native Business OS / AI Sales
> wedge positioning **as product positioning only**; the wedge history is
> preserved below for context and in the decision log. The outcome-first
> principle and the agentic system model of
> [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md) remain canonical.
> Last reviewed: 2026-11-15.
>
> AaaS-only direction with Web3 as next plan: [`11-aaas-web3-strategy.md`](11-aaas-web3-strategy.md)
> (decision D6, preserved); ADR: [`architecture/adr-aaas-only-end-of-saas.md`](architecture/adr-aaas-only-end-of-saas.md).

## Contents

| Section | Anchor |
|---|---|
| Core thesis | [`#core-thesis`](#core-thesis) |
| What we position as | [`#position-as`](#position-as) |
| What we do not position as | [`#do-not-position-as`](#do-not-position-as) |
| Long-term module map (destination, not MVP) | [`#module-map`](#module-map) |
| Strategic framework | [`#strategic-framework`](#strategic-framework) |
| Relationship to the live App Builder | [`#relationship-to-app-builder`](#relationship-to-app-builder) |
| Product non-goals | [`#product-non-goals`](#product-non-goals) |

<a id="core-thesis"></a>
## Core thesis

VibeSDK is an **open, managed agentic platform**: developers, AI builders, and
businesses create, compose, deploy, operate, evaluate, and evolve autonomous
agents and multi-agent systems. The user states the desired **outcome** in
natural language — never a workflow or a set of nodes:

> "Build me an agent that qualifies leads, follows up
> automatically, updates the CRM, and reports results."

The platform's job is to interpret that intent, compile it into the shared
**Canonical System IR** (one representation for Natural Language Studio, the
Visual Builder, the SDK/CLI/API, and manifests — see
[`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md)), compose
the agents and
business functions needed, execute them durably with human approval where
required, and report the result. Workflow/node design is an inspection,
debugging, and advanced-authoring representation — never the primary
interaction. This
continues the outcome-first principle of
[`PRODUCT_THESIS.md`](PRODUCT_THESIS.md); what changes is that the *platform
itself* is the product (Agent Builder first), not a single vertical business
outcome.

Historical note (2026-10-06 wedge, preserved): the prior revision positioned
the first *wedge* as a concrete, sellable business outcome (AI Sales) instead
of a general builder platform. That wedge history is preserved in decision D1
and below; it no longer defines the product identity.

<a id="position-as"></a>
## What we position as

- **Open Agentic Platform** — Build, Compose, Deploy, Operate and Evolve AI
  Agent Systems. Full definition:
  [`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md).
- **Natural Language First** — the primary creation experience is the Natural
  Language Studio (intent → clarification → planning → generation →
  validation → approval → deployment).
- **Visual Builder for developers** — the advanced environment for inspecting,
  editing, testing, and debugging agentic systems on the same Canonical
  System IR (never a mandatory entry point, never a parallel agent model).
- **Developer-first extensibility** — SDK, CLI, REST API, OpenAPI, manifest
  specs, Git integration, import/export on one validation and execution
  architecture.
- **Open Core and Managed Cloud** — open-core direction with managed execution
  and operations (boundary and license are open decisions, not claims).
- **App Builder as a capability and reference workload** — the live App
  Builder is the first reference workload and the seed of the Autonomous
  Software Factory, not the whole identity.

<a id="do-not-position-as"></a>
## What we do NOT position as

- A generic ERP (accounting/inventory suites were the *destination modules*
  of the retired Business OS framing, never the MVP identity — retired by D7).
- A generic no-code builder or a generic AI chatbot.
- A generic workflow automation tool competing on canvas features with
  n8n/Dify/Retool (unchanged from
  [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md) strategic non-goals).
- A marketplace platform for everyone (marketplace is a future ecosystem
  phase, after proven demand — see
  [`08-marketplace.md`](08-marketplace.md)).
- A single vertical SaaS outcome as the product identity (the 2026-10-06 AI
  Sales wedge is preserved as an optional reference template/example in
  [`02-mvp.md`](02-mvp.md), not the mandatory first market product).

<a id="module-map"></a>
## Long-term module map (destination architecture, NOT MVP scope)

**Key decision (D2, preserved):** ERP + full AI suite + Marketplace + broad
Automation
were the **destination architecture** of the retired Business OS framing, not
the MVP scope. Under the Agentic Platform strategy (D7) the destination is
the M0–M6 platform roadmap
([`03-product-roadmap.md`](03-product-roadmap.md)); the table below is
preserved as the history of the 2026-10-06 framing. The MVP is the Agent
Builder ([`02-mvp.md`](02-mvp.md)).

| Module | Phase introduced | Owner doc |
|---|---|---|
| AI Agents (agent runtime) | MVP (wedge) | [`05-agent-runtime.md`](05-agent-runtime.md) |
| Business Automation (triggers, schedules, rules) | Phase 2 | [`03-product-roadmap.md`](03-product-roadmap.md) |
| CRM (customers, pipeline) | Phase 3 | [`03-product-roadmap.md`](03-product-roadmap.md) |
| Commerce (orders, products) | Phase 3 | [`03-product-roadmap.md`](03-product-roadmap.md) |
| ERP capabilities (inventory, supplier ops) | Phase 3 (gradual) | [`03-product-roadmap.md`](03-product-roadmap.md) |
| Analytics | Phase 3 | [`03-product-roadmap.md`](03-product-roadmap.md) |
| Marketplace (functions, agents, apps, suppliers) | Phase 4 | [`08-marketplace.md`](08-marketplace.md) |
| Autonomous Software Factory | Phase 5 | [`09-software-factory.md`](09-software-factory.md) |

<a id="strategic-framework"></a>
## Strategic framework (SPRINT-inspired, customer/problem-first)

1. **Segment before building** — AI Developers and AI Builders first
   ([`01-problem-and-icp.md`](01-problem-and-icp.md) is all hypotheses until
   validated); businesses and teams expand next.
2. **Validate the real customer problem** — interviews before code scope.
3. **Identify willingness to pay** — pricing hypotheses tested in phase 0.
4. **Ship the platform incrementally, one sellable slice at a time** — M0
   Agent Builder first, then multi-agent, runtime, ecosystem, operations,
   marketplace, factory (dependency order in
   [`03-product-roadmap.md`](03-product-roadmap.md)).
5. **Expand from one builder into the broader Agentic Platform** — teams,
   runtime, and marketplace grow *from* the builder's proven surface, in
   dependency order.

<a id="relationship-to-app-builder"></a>
## Relationship to the live App Builder

Today the repository implements an AI App Builder (dual-plane: Go control
plane with the Eino DeepAgent team + light Cloudflare Worker). Under this
vision that builder is repositioned, not discarded:

- It is the **first reference workload** of the eventual Autonomous Software
  Factory ([`09-software-factory.md`](09-software-factory.md)).
- Its generation pipeline (plan → code → review) is the seed of the agent
  runtime ([`05-agent-runtime.md`](05-agent-runtime.md)).
- Its identity/session/tenancy work (checklist P0) is the seed of
  [`07-multitenancy-security.md`](07-multitenancy-security.md).

<a id="product-non-goals"></a>
## Product non-goals (scope-creep guards)

- No full accounting or inventory ERP in the MVP.
- No full marketplace, no large plugin ecosystem in the MVP.
- No complex visual workflow editor in the MVP (must-not-build list in
  [`02-mvp.md`](02-mvp.md)).
- No parallel build-out of all major modules — the roadmap is strictly
  dependency-ordered ([`03-product-roadmap.md`](03-product-roadmap.md)).
- No claims that unresolved architecture candidates (SvelteKit, Appwrite,
  Dify, TiDB, Rivet, Bolt, Chatwoot, Bun) are decided — see
  [`04-system-architecture.md`](04-system-architecture.md) and
  [`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md).

