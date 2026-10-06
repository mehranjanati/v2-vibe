# Problem & ICP — Phase 0 Validation (HYPOTHESES, not facts)

> **Scope:** the phase-0 customer/problem validation pack for the AI Sales
> wedge ([`02-mvp.md`](02-mvp.md)). Vision:
> [`00-product-vision.md`](00-product-vision.md). Roadmap position:
> [`03-product-roadmap.md`](03-product-roadmap.md) phase M0.
>
> **Status:** `planned` — every statement in this document is a **hypothesis
> to validate through customer interviews before phase M1 scope is locked**.
> Nothing here is a validated fact about any customer segment. Last
> reviewed: 2026-10-06.

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

Small/mid B2B businesses capture inbound leads but lose revenue in the gap
between "lead arrives" and "salesperson properly works the lead":
qualification is manual and inconsistent, follow-ups depend on individual
discipline, the CRM is updated late or never, and management has no
reliable picture of pipeline activity. The pain is frequent (every lead),
monetizable (each lost lead has an estimable value), and currently patched
with people and duct-taped tools rather than solved.

<a id="icp"></a>
## ICP definition (hypothesis — to be narrowed to ONE segment)

Candidate shape (validate/narrow before M1):

- Business: B2B service companies or agencies with a steady inbound flow
  (forms, DMs, email) and 1–10 sales-adjacent staff.
- Buyer: founder/owner or sales lead who personally feels missed-follow-up
  pain and can decide on a tool without a procurement cycle.
- NOT the ICP for the wedge: enterprises with existing SFA governance,
  pure B2C shops, teams with no inbound flow.
- The technical-builder ICP of
  [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md) remains relevant for the
  long-term platform ecosystem, but it is **not** the wedge buyer.

<a id="jtbd"></a>
## Jobs-to-be-done

1. When a lead arrives, I want it qualified and responded to immediately, so
   that hot leads never cool.
2. When I am busy selling, I want follow-ups to continue without me, so
   that no deal dies from silence.
3. When a lead takes an action, I want my CRM to reflect it without manual
   data entry, so that my pipeline view is trustworthy.
4. Every week I want a summary of what the AI sales system did and what it
   produced, so that I can decide where to spend my own time.

<a id="workarounds"></a>
## Existing workarounds (what we replace)

- Manual CRM discipline + reminders (fails under load, uneven quality).
- Generic email sequences (no qualification, no CRM write-back, no
  judgment).
- Hiring a VA / SDR (expensive, unscalable, still inconsistent).
- Glued-together automation stacks (forms → spreadsheets → email tools —
  brittle, no agent judgment, no reporting).

<a id="wtp"></a>
## Willingness-to-pay hypothesis

- WTP anchors to the *value of recovered leads*, not to tool cost: if the
  wedge recovers even a few leads per month, a per-outcome or per-seat
  monthly price in the low hundreds (USD) should be defensible.
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
