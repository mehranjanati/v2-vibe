# MVP Definition — Agent Builder (PROPOSED)

> **Scope:** the Agent Builder MVP (M0) of the Agentic Platform vision. ICP
> and problem hypotheses: [`01-problem-and-icp.md`](01-problem-and-icp.md)
> (must validate before build). Vision:
> [`00-product-vision.md`](00-product-vision.md). Roadmap position: phase M0
> in [`03-product-roadmap.md`](03-product-roadmap.md). Canonical strategy:
> [`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md).
> Agent/function/tenant
> models: [`05-agent-runtime.md`](05-agent-runtime.md) ·
> [`06-functions-and-tools.md`](06-functions-and-tools.md) ·
> [`07-multitenancy-security.md`](07-multitenancy-security.md).
>
> **Status:** `planned` — Agent Builder scope; nothing in this scope is
> built yet beyond what the live App Builder already provides. The prior AI
> Sales wedge is retained below as an optional reference template, not the
> MVP. Last
> reviewed: 2026-11-15.

## The MVP (one usable Agent Builder)

> **"Describe the agent you want in natural language; inspect and refine it
> visually; run it governed and auditable."**

Natural-language agent creation + visual inspection/editing on one shared
system representation, single-agent execution, basic tools, basic model
selection, basic memory, versioning, execution history, auth + tenant
isolation, import/export.

## Rule

Do NOT build all major platform phases in parallel. Multi-agent systems,
durable runtime depth, developer ecosystem breadth, operations, marketplace,
and the factory are destination phases (decision D7 in
[`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md)).
The MVP delivers one usable Agent Builder for the initial segments.

## Must-have (M0 scope)

| Capability | Platform concept | Live seed today |
|---|---|---|
| Natural-language agent creation | Natural Language Studio | Chat UX + plan approval already live |
| Visual inspection + basic editing | Visual Builder (advanced) | Workflow visualizer (inspection only); editing is new |
| Single-agent execution | Agent layer ([`05-agent-runtime.md`](05-agent-runtime.md)) | Eino DeepAgent coordinator/coder/reviewer team (App Builder domain) |
| Basic tools + model selection | Capability + model layers | VFS/REST toolset live; registry + gateway are new; built-in Tools + starter Skills catalog in [`06-functions-and-tools.md`](06-functions-and-tools.md) |
| Basic memory | Memory layer | Redis VFS/state exists; scoped memory is new |
| Versioning + import/export | System state | Internal-Git lineage direction exists; manifests are new |
| Execution history | Observability | Generation lineage recorder exists |
| Human approval where needed | Human-in-the-loop | Plan-approval gate exists; capability-level approvals are new |
| Auth + tenant isolation | Tenancy | D1 auth + opt-in session boundary live; full isolation per P0 |

## Must-NOT build (M0 non-goals)

- Multi-agent teams and delegation (phase M1).
- Durable runtime depth: schedules, triggers, webhooks, pause/resume
  (phase M2).
- Public API/SDK/CLI/OpenAPI/manifest specs as stable contracts (phase M3).
- Advanced evaluation, regression, cost optimization (phase M4).
- Registries with third-party supply, marketplace, revenue sharing (phase M5).
- Autonomous generation of whole systems (phase M6).
- Full accounting/inventory ERP; full marketplace or plugin ecosystem.
- Two parallel agent models (NL vs visual) — one Canonical System IR only.

## Acceptance criteria (M0 exit gate)

1. A user creates an agent through natural language — no node design.
2. The same agent can be inspected and edited in the visual editor; both
   interfaces use the same canonical system representation.
3. The agent executes at least one useful task end to end.
4. Every tool call is permission-checked and auditable with
   tenant/agent/identity and verdict.
5. Runs have traceable identity and status in execution history.
6. Tenant isolation tests pass.
7. No critical security boundary is left unverified (launch gates in
   [`07-multitenancy-security.md`](07-multitenancy-security.md)).
8. Failure behavior: a failed/missing capability degrades to an explicit,
   logged error with human escalation — never silent loss.

## Optional reference template (NOT the MVP)

AI Sales (lead qualification + follow-up + CRM write-back + weekly summary)
is preserved as an optional reference template/example that exercises the
Agent Builder — never as the mandatory first market product. If built as a
demo, it must satisfy the same representation, permission, audit, and
approval gates above.

## Dependencies

- Engineering P0 (identity/tenancy/security) — checklist P0.3–P0.9 remain
  the gating foundation ([`DEV_CHECKLIST.md`](DEV_CHECKLIST.md)).
- Capability model minimum ([`06-functions-and-tools.md`](06-functions-and-tools.md)):
  typed tools/functions with tenant context, audit and approval hooks.
- Durable execution minimum (engineering P3 slice).
- Measured against builder activation and run success, never seats
  or MAU (decision D6, preserved). Pricing follows the AaaS direction —
  system value plus
  metered run cost — and is finalized only when O6 evidence exists
  ([`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md),
  [`11-aaas-web3-strategy.md`](11-aaas-web3-strategy.md)).

## What remains intentionally unbuilt

Everything in phases M1–M6 of
[`03-product-roadmap.md`](03-product-roadmap.md), and every technology
candidate marked open in
[`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md).
