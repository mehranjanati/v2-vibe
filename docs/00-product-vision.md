# Product Vision — AI-Native Business OS (Agent-as-a-Service)

> **Scope:** canonical product vision and positioning. This is the strategy
> layer on top of the platform thesis ([`PRODUCT_THESIS.md`](PRODUCT_THESIS.md) —
> agentic system model, moats, competitive analysis, all still canonical).
> Current reality: [`DEV_STATUS.md`](DEV_STATUS.md). Product roadmap:
> [`03-product-roadmap.md`](03-product-roadmap.md). Decision log:
> [`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md).
>
> **Status:** `proposed` — strategy direction adopted 2026-10-06. It supersedes
> the *product positioning and ICP* of the 2026-10-01 thesis revision (the
> builder-first ICP and the "no vertical SaaS in sales" non-goal); see
> decision D1 in
> [`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md).
> Last reviewed: 2026-10-06.

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

The user asks for the **outcome**, not for a workflow or a set of nodes:

> "Build me an AI sales system that qualifies leads, follows up
> automatically, updates the CRM, and reports results."

The platform's job is to interpret that intent, compose the agents and
business functions needed, execute them durably with human approval where
required, and report the result. Workflow/node design is an inspection and
debugging representation for experts — never the primary interaction. This
continues the outcome-first principle of
[`PRODUCT_THESIS.md`](PRODUCT_THESIS.md); what changes is that the first
*wedge* is a concrete, sellable business outcome instead of a general
builder platform.

<a id="position-as"></a>
## What we position as

- **AI-native Business OS** — the system of record and execution for a
  business's operational outcomes.
- **Outcome-driven business automation** — the customer buys a measurable
  business result, not infrastructure.
- **Agent-as-a-Service platform** — agents are the product surface that
  orchestrates reusable business capabilities.

<a id="do-not-position-as"></a>
## What we do NOT position as

- A generic ERP (accounting/inventory suites are *destination modules*,
  never the MVP identity).
- A generic no-code builder or a generic AI chatbot.
- A generic workflow automation tool competing on canvas features with
  n8n/Dify/Retool (unchanged from
  [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md) strategic non-goals).
- A marketplace platform for everyone (marketplace is phase 4 of the
  roadmap, after the wedge proves value — see
  [`08-marketplace.md`](08-marketplace.md)).

<a id="module-map"></a>
## Long-term module map (destination architecture, NOT MVP scope)

**Key decision (D2):** ERP + full AI suite + Marketplace + broad Automation
are the **destination architecture**, not the MVP scope. The MVP is one
narrow, measurable business outcome ([`02-mvp.md`](02-mvp.md)).

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

1. **Segment before building** — one narrow ICP first
   ([`01-problem-and-icp.md`](01-problem-and-icp.md) is all hypotheses until
   validated).
2. **Validate the real customer problem** — interviews before code scope.
3. **Identify willingness to pay** — pricing hypotheses tested in phase 0.
4. **Sell a concrete outcome, not infrastructure** — the wedge is a business
   result with a number attached (qualified leads, follow-ups sent).
5. **Expand from one wedge into the broader Business OS** — CRM, automation
   and marketplace grow *from* the wedge's proven surface, in dependency
   order.

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

