# AaaS + Web3 Strategy — SaaS Is Over, Outcomes Are the Product (ADOPTED)

> **Scope:** canonical strategy for the AaaS-only direction with Web3 as
> the next plan. This is strategy, not build scope: current reality lives
> in [`DEV_STATUS.md`](DEV_STATUS.md), ordered work in
> [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md).
> Vision: [`00-product-vision.md`](00-product-vision.md).
> Roadmap: [`03-product-roadmap.md`](03-product-roadmap.md).
> Decisions: [`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md) (D6, O7, O8).
> ADR: [`architecture/adr-aaas-only-end-of-saas.md`](architecture/adr-aaas-only-end-of-saas.md).
>
> **Status:** `adopted` — strategy direction adopted 2026-11-05.
> Last reviewed: 2026-11-05.

## Contents

| Section | Anchor |
|---|---|
| Why legacy SaaS ends here | [`#why-saas-ends`](#why-saas-ends) |
| What AaaS means in this repo | [`#what-aaas-means`](#what-aaas-means) |
| Why Web3 is next plan, not now | [`#why-web3-next`](#why-web3-next) |
| Economic model | [`#economic-model`](#economic-model) |
| Three horizons | [`#three-horizons`](#three-horizons) |
| Risks and guards | [`#risks`](#risks) |
| Web3 workstreams | [`#web3-workstreams`](#web3-workstreams) |

<a id="why-saas-ends"></a>
## Why legacy SaaS ends here

Legacy SaaS sells access: per-seat subscriptions to a fixed UI. Three
structural reasons it cannot be our future:

1. **Commoditization.** Any vertical SaaS screen is clonable in weeks by
   AI builders and automation tools. Competing on canvas features is
   already an explicit strategic non-goal of the platform thesis.
2. **Value gap.** The customer pays for logins while the work stays
   manual. Usage without outcome drives churn.
3. **Margin inversion.** Per-seat pricing against per-token and per-run
   AI cost destroys margin exactly when usage grows.

Continuing the old model means a price war we lose with a small team.
The only defensible unit is a measured business result.

<a id="what-aaas-means"></a>
## What AaaS means in this repo

The customer states an outcome, for example the AI Sales wedge: qualify
leads, follow up automatically, update the CRM, report results. The
platform interprets intent, composes agents and business functions,
executes durably with human approval where side effects exist, and
reports the result. Workflow and node design stays an inspection and
debugging surface for experts — never the primary interaction.

Concretely, AaaS-only means:

- No new standalone SaaS pages. Every capability ships as a versioned,
  typed, tenant-scoped function with audit and approval metadata.
- No seat-gated features. Pricing keys are runs, outcomes, and
  marketplace take-rate.
- No generic CRUD modules outside the dependency-ordered roadmap. Each
  roadmap phase must deliver a sellable increment on its own.
- The live App Builder is the first reference workload and the seed of
  the agent runtime — repositioned, not discarded.
<a id="why-web3-next"></a>
## Why Web3 is next plan, not now

There is no Web3 code in the repository today: no wallet, no chain
binding, no DID, no settlement. The deploy config carries D1, KV, and
Workflows bindings only.

Putting Web3 into the M1 wedge would kill the wedge: wallet friction
destroys interview validation, gas volatility destroys unit economics,
and token risk destroys B2B sales. Web3 before proven demand repeats
the classic cold-start failure the marketplace strategy already warns
against: empty shelves with integration burden and no demand.

The correct place for Web3 is the M4 marketplace and M5 software
factory, as the trust and settlement layer on top of a proven platform:

1. **Agent identity.** DIDs plus verifiable credentials for versioned
   agents and functions. Lineage in D1 proves history inside the
   platform; verifiable provenance proves it to third parties.
2. **Marketplace settlement.** Cross-border function, agent, and app
   sales need instant escrow and revenue split in stablecoin.
3. **Data ownership and audit.** Business data stays platform-owned in
   the data layer; access grants and execution proofs become auditable
   without handing custody to any vendor.

So: AaaS is the product, Web3 is the trust and settlement layer.

<a id="economic-model"></a>
## Economic model

Charge where durable value accrues — runs and evaluated outcomes plus
marketplace take-rate, never raw model tokens or seats:

1. **Pay per outcome.** For example, per qualified lead and per booked
   meeting in the sales wedge.
2. **Run cost plus margin.** Every durable execution carries metered
   cost with a transparent margin.
3. **Marketplace take-rate.** A platform fee on third-party function,
   agent, and app sales, settled instantly once Web3 settlement lands.

<a id="three-horizons"></a>
## Three horizons

### Horizon 0 — stop SaaS (immediate, no code)

- No new vertical-SaaS features, no seat-based metrics, no parallel
  module build-out.
- Backlog priority stays on tenancy and security foundation plus the
  minimum agent, function, and durable-execution slices the wedge needs.
- Success metrics switch from seats and MAU to outcome counts: leads
  qualified, follow-ups sent, replies received, pipeline updates.

### Horizon 1 — pure AaaS (M0 to M3)

- M0: validate the ICP, the problem, and willingness to pay through
  interviews before any wedge scope lock.
- M1: one wedge — the AI Sales outcome with lead qualification,
  follow-up, CRM write-back, and weekly reporting, approval-gated.
- M2: automation platform grown from the wedge — schedules, triggers,
  reusable functions, webhooks, integrations, business rules.
- M3: Business OS expansion as native modules on the same data and
  capability layers — only as a validated segment demands them.

### Horizon 2 — Web3 (M4 to M5, next plan)

- No Web3 build starts until M1–M3 demonstrate real tenant demand for
  extension and reuse — the documented marketplace precondition.
- First Web3 work is identity and provenance, then settlement, then
  audit anchors. Each needs its own ADR. No governance token is in
  scope; settlement starts with fiat plus stablecoin.

<a id="risks"></a>
## Risks and guards

1. **Early marketplace.** Guard: the marketplace stays a destination
   phase until demand evidence exists.
2. **Token before product.** Guard: no token design until outcomes are
   sold and settled; regulatory review first.
3. **Mandatory wallets.** Guard: Web3 stays invisible to business
   buyers — embedded wallets with gas abstraction.
4. **Vendor coupling.** Guard: execution and settlement interfaces stay
   vendor-neutral; no vendor owns business data.
5. **SaaS relapse.** Guard: any proposal framed in seats, generic pages,
   or parallel modules is out of scope by this strategy.

<a id="web3-workstreams"></a>
## Web3 workstreams

| ID | Workstream | Gate | Notes |
|---|---|---|---|
| W3.1 | Identity and provenance (DID, credentials, signed lineage) | M4 entry | No token; audit-first |
| W3.2 | Settlement (stablecoin escrow, instant revenue split) | M4 pilot on one low-cost L2 | Fiat on-ramp retained |
| W3.3 | Trust anchors (proof-of-run hashes on chain, data off chain) | M4–M5 | Insurance and audit-grade verifiability |

Each workstream resolves its open question through an ADR before
implementation. Unresolved questions are never quoted as final.
