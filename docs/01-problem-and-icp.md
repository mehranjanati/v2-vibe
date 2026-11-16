# Problem & ICP — Agentic Platform Validation (HYPOTHESES, not facts)

> **Scope:** the phase-0 customer/problem validation pack for the Agentic
> Platform Agent Builder ([`02-mvp.md`](02-mvp.md)).
> Vision:
> [`00-product-vision.md`](00-product-vision.md). Roadmap position:
> [`03-product-roadmap.md`](03-product-roadmap.md) phase M0.
> Canonical strategy:
> [`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md).
>
> **Status:** `planned` — every statement in this document is a **hypothesis
> to validate through customer interviews before phase M1 scope is locked**.
> Nothing here is a validated fact about any customer segment. Last
> reviewed: 2026-11-15.

## Contents

| Section | Anchor |
|---|---|
| Why phase 0 exists | [`#why-phase-0`](#why-phase-0) |
| Problem statement (hypothesis) | [`#problem-statement`](#problem-statement) |
| ICP definition (hypothesis) | [`#icp`](#icp) |
| Jobs-to-be-done | [`#jtbd`](#jtbd) |
| Existing workarounds | [`#workarounds`](#workarounds) |
| Willingness-to-pay hypothesis | [`#wtp`](#wtp) |
| Success metrics | [`#success-metrics`](#success-metrics) |
| Required validation outputs (exit gate) | [`#exit-gate`](#exit-gate) |

<a id="why-phase-0"></a>
## Why phase 0 exists

Segment before building. The single biggest strategic risk is building the
wedge for a segment that does not feel the problem acutely enough to pay.
Phase M0 exists to replace the hypotheses below with interview evidence
before any wedge feature work starts.

<a id="problem-statement"></a>
## Problem statement (hypothesis)

Building a production-grade AI agent system means reassembling the same
undifferentiated machinery every time: model access and routing, tool/MCP
wiring, memory and state, permissions and policy, durable execution with
retries and approvals, evaluation, observability, versioning, and deployment.
Frameworks provide pieces; runtimes provide primitives; app builders provide
a narrow output. Nobody provides the **whole loop from a stated outcome to an
operating, governed, improvable agentic system**. Candidate pains (all
unvalidated): fragmented agent development, orchestration glue code, tool
integration toil, no durable runtime, no evaluation/observability loop, no
governance for side effects, no path from prototype to operated system.

Historical note (preserved): the 2026-10-06 revision hypothesized a B2B sales
follow-up pain (lost leads between arrival and follow-up). That wedge history
is preserved in the decision log (D1, D7) and as an optional reference
template in [`02-mvp.md`](02-mvp.md); it is not the platform ICP.

<a id="icp"></a>
## ICP definition (hypothesis — to be narrowed and validated)

Candidate shape (validate/narrow before M1). All segments and pains below are
hypotheses — no interview evidence is claimed:

- **AI Developers (INITIAL_PRIMARY):** developers building production AI
  agents and multi-agent applications. Hypothesized needs: fine-grained
  control, visual editing, SDK and API, custom tools, MCP integration,
  versioning, debugging, deployment, observability.
- **AI Builders (INITIAL_PRIMARY):** technical and semi-technical users who
  build through natural language and reusable components. Hypothesized needs:
  natural-language creation, agent templates, tool integrations, simple
  configuration, testing, deployment.
- **Businesses and Teams (SECONDARY expansion):** organizations deploying
  agents for operations. Hypothesized needs: ready-to-use agents, team
  collaboration, governance, approvals, reliability, cost controls, business
  outcomes. Explicitly NOT the initial validation focus.
- Candidate profiles to test: technical founders, AI engineers, independent
  developers, small product teams, AI agencies.

<a id="jtbd"></a>
## Jobs-to-be-done (hypotheses for AI Developers and AI Builders)

1. When I describe a desired agent outcome in natural language, I want a
   working agent composed, validated, and deployable, so I skip orchestration
   glue code.
2. When the generated system needs adjustment, I want to inspect and edit it
   visually (or via SDK/API/manifest), so I keep control without rebuilding.
3. When my agent uses tools or external services, I want governed,
   permission-checked execution with approvals, so side effects stay safe.
4. When my agent runs in production, I want traces, history, versions, and
   evaluation, so I can operate and improve it.

<a id="workarounds"></a>
## Existing workarounds (what we replace)

- Hand-rolled orchestration glue (prompt chains + ad-hoc scripts — brittle,
  ungoverned, unobservable).
- Single-purpose agent frameworks or chatbot builders (narrow output, no path
  to operated multi-agent systems).
- Generic workflow automation stacks (forms → sheets → email tools — brittle,
  no agent judgment, no evaluation loop).
- Manual ops: copy-paste deployment, no versioning, no approval gates, no
  cost attribution.

<a id="wtp"></a>
## Willingness-to-pay hypothesis

- WTP anchors to *durable builder value* (working agents shipped without glue
  code) plus metered run cost — not seats, not raw tokens.
- Seat-based pricing is out of scope by decision D6 (AaaS-only, preserved);
  the sellable unit is a validated agentic system plus metered runs
  ([`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md),
  [`11-aaas-web3-strategy.md`](11-aaas-web3-strategy.md)).
- Pricing is NOT decided here; hypothesis only. Charge where durable value
  accrues (aligned with the business-model considerations in
  [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md)).

<a id="success-metrics"></a>
## Success metrics (phase M0)

- 10+ problem interviews completed with the candidate ICP.
- ≥60% of interviewees confirm the problem as top-3 pain (validate or
  pivot the wedge before building).
- ≥3 interviewees willing to pre-commit (pilot or paid pilot) — the real
  willingness-to-pay test.
- ICP narrowed to one segment with a written problem statement and JTBD
  evidence.

<a id="exit-gate"></a>
## Required validation outputs (M0 exit gate)

Before phase M1 scope lock, this document must be updated from hypothesis
to evidence: final ICP definition, validated problem statement, JTBD list,
documented workarounds, willingness-to-pay evidence, and the success
metrics that phase M1 will be judged against. Until then
[`02-mvp.md`](02-mvp.md) remains a *planned* scope, not a committed build
list.
