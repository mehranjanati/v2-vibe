# MVP Definition — AI Sales Agent Wedge (PROPOSED)

> **Scope:** the single MVP wedge of the AI-Native Business OS vision. ICP
> and problem hypotheses: [`01-problem-and-icp.md`](01-problem-and-icp.md)
> (must validate before build). Vision:
> [`00-product-vision.md`](00-product-vision.md). Roadmap position: phase M1
> in [`03-product-roadmap.md`](03-product-roadmap.md). Agent/function/tenant
> models: [`05-agent-runtime.md`](05-agent-runtime.md) ·
> [`06-functions-and-tools.md`](06-functions-and-tools.md) ·
> [`07-multitenancy-security.md`](07-multitenancy-security.md).
>
> **Status:** `planned` — one explicit wedge; nothing in this scope is
> built yet beyond what the live App Builder already provides. Last
> reviewed: 2026-10-06.

## The wedge (one concrete business outcome)

> **"Build me an AI sales system that qualifies leads, follows up
> automatically, updates the CRM, and reports results."**

Lead qualification + automated follow-up + CRM write-back + weekly sales
summary — delivered as an agent-as-a-service outcome, not as a toolkit.

## Rule

Do NOT build all major modules in parallel. ERP, Marketplace and broad
automation are destination architecture (decision D2 in
[`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md)).
The MVP delivers exactly one measurable outcome for one ICP.

## Must-have (M1 scope)

| Capability | Platform concept | Live seed today |
|---|---|---|
| Natural-language goal input | User layer | Chat UX + plan approval already live |
| AI agent orchestration | Agent layer ([`05-agent-runtime.md`](05-agent-runtime.md)) | Eino DeepAgent coordinator/coder/reviewer team |
| Business data access (leads, customers) | Data layer ([`04-system-architecture.md`](04-system-architecture.md)) | Redis VFS/D1 exist; **tenant business schema is new** |
| Actions/functions | Capability layer ([`06-functions-and-tools.md`](06-functions-and-tools.md)) | Function registry with business actions is new |
| Execution history | Observability | Generation lineage recorder exists |
| Human approval where needed | Human-in-the-loop | Plan-approval gate exists; function-level approvals are new |
| Result reporting (sales summary) | Outcome reporting | New |

## Must-NOT build (M1 non-goals)

- Full accounting ERP; full inventory ERP.
- Full marketplace or any large plugin ecosystem
  ([`08-marketplace.md`](08-marketplace.md)).
- Complex visual workflow editor — workflows are composed by the agent;
  a graph stays an inspection view (unchanged from
  [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md)).
- Broad automation platform features (scheduled jobs, event triggers,
  webhooks, reusable functions platform) — those are phase M2
  ([`03-product-roadmap.md`](03-product-roadmap.md)).
- Multi-segment ICP support — one segment, one outcome.

## Acceptance criteria (M1 exit gate)

1. A pilot tenant states the sales outcome in natural language and the
   system composes: lead-qualification function chain + follow-up agent +
   CRM write-back + weekly summary — with zero node/workflow design by the
   user.
2. Every executed action is visible in an execution history with
   tenant/agent/function identity and verdict.
3. Follow-up actions that send real external messages require human
   approval (or an explicit, logged tenant-level policy).
4. The weekly report states outcomes (leads qualified, follow-ups sent,
   replies received, pipeline updated) — measurable by the metrics defined
   in phase M0.
5. Failure behavior: a failed/missing capability degrades to an explicit,
   logged error with human escalation — never silent loss.

## Dependencies

- Engineering P0 (identity/tenancy/security) — checklist P0.3–P0.9 remain
  the gating foundation ([`DEV_CHECKLIST.md`](DEV_CHECKLIST.md)).
- Function model minimum ([`06-functions-and-tools.md`](06-functions-and-tools.md)):
  `create_customer`, `update_customer`, `search_products`-style typed
  functions with tenant context, audit and approval hooks.
- Durable execution minimum for follow-up sequences (engineering P3 slice).

## What remains intentionally unbuilt

Everything in phases M2–M5 of
[`03-product-roadmap.md`](03-product-roadmap.md), and every technology
candidate marked open in
[`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md).
