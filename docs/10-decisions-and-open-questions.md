# Decisions & Open Questions (PROPOSED)

> **Scope:** the decision log for the AI-Native Business OS strategy
> reframe, and every unresolved question with its evaluation criteria.
> Vision: [`00-product-vision.md`](00-product-vision.md). Register of
> technology candidates: [`04-system-architecture.md`](04-system-architecture.md).
>
> **Status:** `proposed` — decisions D1–D5 are strategy direction adopted
> 2026-10-06 (documentation only; no implementation). Open questions O1–O6
> are explicitly **unresolved — none may be treated as finalized**. Last
> reviewed: 2026-10-06.

## Adopted decisions (strategy direction)

| ID | Decision | Rationale / consequence |
|---|---|---|
| D1 | **Wedge = AI Sales (lead qualification + follow-up + CRM update + reporting).** Supersedes the "no vertical SaaS in sales" clause of [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md) strategic non-goals. | Sales impact is the fastest measurable/validatable revenue outcome and seeds CRM → automation → Business OS. The clause is revised by this strategy, not silently dropped. |
| D2 | **ERP + full AI suite + Marketplace + broad Automation = destination architecture, NOT MVP scope.** | Prevents the parallel build-out failure mode; the roadmap is dependency-ordered. |
| D3 | **Outcome-first interaction: users request outcomes; workflow/node design stays an inspection surface.** | Continues the existing thesis; also rules out a visual workflow editor in the MVP. |
| D4 | **Multi-tenant by design; tenancy is a launch requirement of the wedge (M1), not later hardening.** | Product contract for engineering P0. |
| D5 | **Vendor replaceability; no orchestration vendor owns business data.** | Dify (if adopted) orchestrates only; the data layer stays platform-owned. |

## Open questions (unresolved — do not claim as decided)

| ID | Question | Evaluation criteria / notes |
|---|---|---|
| O1 | Final ICP and problem validation evidence. | Phase M0 exit gate ([`01-problem-and-icp.md`](01-problem-and-icp.md)) — interviews + WTP evidence before any M1 build scope lock. |
| O2 | Frontend (SvelteKit vs live React SPA), identity (Appwrite vs live D1 auth), database (TiDB vs D1/Redis), generated-app builder (Bolt/bolt.diy vs live pipeline), function runtime (Bun where appropriate). | Each needs a trade-off ADR against the live implementation; rewrite only with explicit migration cost/benefit. Candidates are recorded, not chosen ([`04-system-architecture.md`](04-system-architecture.md)). |
| O3 | Tenant data-model granularity (shared schema + tenant column by default; when is a tenant-specific model justified?). | Guiding constraint: never assume every tenant needs a custom database ([`07-multitenancy-security.md`](07-multitenancy-security.md)). |
| O4 | Durable execution + gateway choice (Rivet or equivalent vs live Cloudflare Workflows; FastAPI vs Rivet gateway vs live Go Fiber). | Decide together with the runtime interface boundary; P3 checklist items are the first slice. |
| O5 | AI orchestration (continue Eino-first vs adopt Dify for parts). | Boundary rule: agents must remain composable either way; pilot evidence from the wedge decides. |
| O6 | Pricing / willingness-to-pay for the wedge. | Hypotheses in [`01-problem-and-icp.md`](01-problem-and-icp.md); price only after M0 evidence. |

## Conflicts resolved by this reframe

1. **Vertical-SaaS non-goal vs sales wedge** — resolved by D1 (explicit
   supersession, recorded above).
2. **Builder-first ICP vs business-buyer wedge** — resolved: the wedge
   buyer is the business ICP from M0; the builder ICP remains the long-term
   platform/ecosystem audience ([`PRODUCT_THESIS.md`](PRODUCT_THESIS.md)).
3. **Proposed candidate stack vs live stack** — resolved by the register in
   [`04-system-architecture.md`](04-system-architecture.md): live stack is
   truth, candidates are options pending ADRs; no rewrite is decided.

## Rule

Any future ADR that resolves O1–O6 must update this log and the linked
documents in the same change — unresolved questions must never be quoted
as finalized decisions.
