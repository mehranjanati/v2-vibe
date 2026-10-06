# Marketplace Strategy (PROPOSED — Phase M4)

> **Scope:** why the marketplace is destination architecture, what it
> becomes, and the preconditions that must hold before building it. Vision:
> [`00-product-vision.md`](00-product-vision.md). Roadmap position: phase M4
> in [`03-product-roadmap.md`](03-product-roadmap.md). Function model it
> extends: [`06-functions-and-tools.md`](06-functions-and-tools.md).
>
> **Status:** `planned` (destination) — explicitly NOT part of the MVP
> ([`02-mvp.md`](02-mvp.md) must-not-build). Last reviewed: 2026-10-06.

## Position

The marketplace is a **distribution layer on a proven platform**, never a
launch strategy. It becomes four surfaces over time:

1. **Function marketplace** — third-party business capabilities as
   manifests ([`06-functions-and-tools.md`](06-functions-and-tools.md)),
   versioned like packages.
2. **Agent marketplace** — composable business-role agents
   ([`05-agent-runtime.md`](05-agent-runtime.md)).
3. **Business app marketplace** — generated applications
   ([`09-software-factory.md`](09-software-factory.md)) installable by
   other tenants.
4. **Supplier/product marketplace** — commerce-side supply surfaces from
   phase M3 modules.

## Preconditions (build order is normative)

- Versioned function registry with typed IO, tenant scoping and audit
  ([`06-functions-and-tools.md`](06-functions-and-tools.md)) — engineering
  P5 registry direction.
- Tenancy/security model enforced in production
  ([`07-multitenancy-security.md`](07-multitenancy-security.md)).
- Demonstrated demand from phases M1–M3: tenants actually asking to extend
  or reuse.
- Credential vault with least-privilege wiring for third-party functions.

## Why deferred (the anti-goal)

A marketplace for everyone is one of the explicit "do not position as"
items ([`00-product-vision.md`](00-product-vision.md)). Launching a
marketplace before a wedge proves usage produces empty shelves and
integration burden with no demand — the classic cold-start failure. The
wedge (AI Sales) is the demand-generation strategy the marketplace later
harvests.

## Value exchange

- Sellers get distribution + the take-rate model considered in
  [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md) business-model section.
- Buyers get capabilities that extend the OS without building.
- Platform gets network effects on top of the capability registry — moat
  hypothesis 9 in [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md).

## Non-goals

- No marketplace in M1–M3.
- No un-vetted functions (review, provenance and tenancy audit are
  mandatory — same discipline as the Dynamic Design Skill Layer imports in
  [`architecture/dynamic-design-skill-layer.md`](architecture/dynamic-design-skill-layer.md)).
- No marketplace as a substitute for first-party wedge quality.
