# ADR: AaaS-Only — End of Legacy SaaS (ADOPTED)

> **Status:** `adopted` — strategy direction adopted 2026-11-05.
> Companion: [`11-aaas-web3-strategy.md`](../11-aaas-web3-strategy.md).
> Vision: [`00-product-vision.md`](../00-product-vision.md).
> Roadmap: [`03-product-roadmap.md`](../03-product-roadmap.md).
> Decision log: [`10-decisions-and-open-questions.md`](../10-decisions-and-open-questions.md) (D6).
>
> Last reviewed: 2026-11-05.

## Context

The legacy SaaS model (sell access — per-seat subscription to a fixed UI)
has three structural failures for this platform:

1. Commoditization: any vertical SaaS UI is clonable in weeks by AI
   builders and automation tools. Competing on canvas features is already
   an explicit strategic non-goal.
2. Value gap: the customer pays for logins, not for completed work.
   Churn follows because usage does not equal outcome.
3. Margin inversion: per-seat pricing against per-token and per-run AI
   cost destroys margin as usage grows.

The repository already reframed toward outcome-first Agent-as-a-Service
(see the product vision and the AI Sales wedge), but the old SaaS framing
still lingers in prose and in backlog habit (new CRUD pages, generic
settings, seat-based metrics). This ADR closes that door explicitly.

## Decision

The platform builds **Agent-as-a-Service only**:

- Customers buy a measurable business outcome, not infrastructure or seats.
- Every new capability ships as a versioned, typed, tenant-scoped
  function with audit and approval metadata — never as a standalone
  SaaS page.
- The primary interaction is natural-language outcome request; workflow
  and node views stay inspection and debugging surfaces for experts.
- Web3 (identity, settlement, trust) is the **next plan** — the trust and
  settlement layer for the M4 marketplace and M5 software factory — and
  is explicitly out of the M1 wedge scope.

## Decision drivers

- Outcome-first thesis continuity: interpret intent, compose agents and
  business functions, execute durably with approval, report results.
- Wedge economics: price per outcome and per run with cost pass-through
  plus margin, plus marketplace take-rate — never raw model tokens.
- Distribution moat: the capability registry plus marketplace network
  effects, not another dashboard.
- Vendor replaceability: no orchestration or chain vendor owns business
  data or business logic coupling.

## Chosen architecture (implications)

- User layer states outcomes; agent layer plans; capability layer acts;
  runtime layer executes durably; data layer owns tenant business data;
  application layer generates tenant interfaces on demand.
- Pricing moves from seat and MAU to runs, outcomes, and take-rate.
- No new SaaS-vertical pages, no seat-gated feature flags, no generic
  CRUD modules outside the dependency-ordered roadmap phases.
- Web3 enters only through the M4 and M5 gates with invisible UX
  (embedded wallets, gas abstraction) and vendor-neutral interfaces.

## Alternatives considered

1. **Keep SaaS + add agents as a feature.** Rejected: preserves the
   value gap and the margin inversion; agents become a chatbot add-on
   instead of the product surface.
2. **Web3 now, in the wedge.** Rejected: wallet friction kills M0
   validation, gas volatility kills unit economics, token risk kills
   B2B sales. Web3 before proven demand repeats the empty-shelves
   marketplace failure.
3. **Marketplace before wedge.** Rejected: distribution layer without
   proven demand produces empty shelves and integration burden.
4. **Token-first monetization.** Rejected: regulatory and trust risk
   before any outcome is sold; stablecoin and fiat settlement come first.

## Consequences

- Positive: single sellable unit (outcome), defensible margin, registry
  network effects, clean gates for each roadmap phase.
- Negative: short-term feature velocity appears slower (no quick CRUD
  wins); pricing and metering must be built alongside the wedge; Web3
  work must be deliberately deferred even when it looks exciting.

## Risks

- Building generic automation too early under the AaaS label.
  Mitigation: strictly dependency-ordered phases, each sellable alone.
- Re-introducing seat pricing through the back door.
  Mitigation: M0 and M1 metrics are outcome counts, never seats or MAU.
- Coupling to one chain or one orchestration vendor.
  Mitigation: interface boundaries, replaceability principle, ADR per
  adoption.

## Future migration points

- M4 settlement pilot on one low-cost L2 with stablecoin escrow.
- DID and verifiable credentials for versioned agents and functions.
- Proof-of-run audit anchors (hash on chain, data off chain).
- Each point needs its own ADR before any implementation begins.
