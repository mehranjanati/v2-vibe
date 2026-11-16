# Marketplace Strategy (PROPOSED — Phase M5)

> **Scope:** why the marketplace is destination architecture, what it
> becomes, and the preconditions that must hold before building it. Vision:
> [`00-product-vision.md`](00-product-vision.md). Roadmap position: phase M5
> in [`03-product-roadmap.md`](03-product-roadmap.md). Capability model it
> extends: [`06-functions-and-tools.md`](06-functions-and-tools.md).
> Canonical strategy:
> [`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md).
>
> **Status:** `planned` (destination, M5) — explicitly NOT part of the MVP
> ([`02-mvp.md`](02-mvp.md) must-not-build). A registry/discovery experience
> may precede commercial marketplace; commerce only on proven demand +
> security controls. Last reviewed: 2026-11-15.

## Position

> **Serverless by design.** Agents run fully serverless: no always-on
> servers per agent, no per-agent ops. Execution is event-driven and scales
> to zero; state lives in the platform's durable stores (checkpoints,
> lineage, memory metadata), never in agent-local memory. The marketplace
> and developer surfaces below therefore distribute **manifests, not
> servers**: installing an agent means resolving a versioned closure and
> running it on the shared serverless runtime under tenant quotas — never
> provisioning infrastructure.

The marketplace is a **distribution layer on a proven platform**, never a
launch strategy. It becomes surfaces over time:

| # | Surface | Goal |
|---|---|---|
| 1 | Agent Templates | Share ready-made agents (Agent Manifests) |
| 2 | Skills Registry | Share skills (Skill Manifests) |
| 3 | Tools Registry | Share tools (Tool Manifests) |
| 4 | MCP Catalog | Discover and connect MCP servers through the governed adapter |
| 5 | System Templates | Share multi-agent systems (System Manifests) |
| 6 | Developer SDK | Build agents outside the UI (public API + SDK on stable contracts) |
| 7 | CLI | Build, test, and deploy from the terminal (manifest-first workflow) |
| 8 | Git Integration | Versioning and collaboration (manifests in Git; import/export round-trip) |
| 9 | Community Contributions | Contribute artifacts under review, provenance, and trust gates |

Surfaces 1–5 are **registries/catalogs first** (discovery + versioned
install); commerce only on proven demand + security controls. Surfaces 6–9
are the **developer contribution path**: author locally → validate →
publish → review → install. Full manifest shapes and the versioning contract:
[`06-functions-and-tools.md`](06-functions-and-tools.md).

## Preconditions (build order is normative)

- Stable manifests (agent, tool, skill, system) with versioning and
  compatibility validation
  ([`06-functions-and-tools.md`](06-functions-and-tools.md),
  [`05-agent-runtime.md`](05-agent-runtime.md)) — engineering
  P5 registry direction.
- Tenancy/security model enforced in production
  ([`07-multitenancy-security.md`](07-multitenancy-security.md)).
- Demonstrated demand from phases M0–M4: builders actually asking to extend
  or reuse.
- Credential vault with least-privilege wiring for third-party capabilities.
- Publisher identity, security scanning, trust/provenance, install/update
  flows, usage tracking (see
  [`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md)).

## Why deferred (the anti-goal)

A marketplace for everyone is one of the explicit "do not position as"
items ([`00-product-vision.md`](00-product-vision.md)). Launching a
marketplace before the platform proves usage produces empty shelves and
integration burden with no demand — the classic cold-start failure. The
Agent Builder through Operations phases (M0–M4) are the demand-generation
strategy the registry/marketplace later harvests.

## Settlement and trust (M5 — gated, not built)

Marketplace supply is only half of the M5 problem; cross-border payouts and
provenance are the other half. Both are **gated to M5** and are the entire
reason Web3 is the next plan rather than MVP scope
([`11-aaas-web3-strategy.md`](11-aaas-web3-strategy.md)):

- **Settlement (W3.2).** Instant escrow and revenue split in stablecoin for
  third-party function, agent and app sales, with the fiat path retained.
  Chain selection (O7) and settlement currency (O8) stay **unresolved** —
  no chain, rail or token is chosen by this document.
- **Identity and provenance (W3.1).** DIDs plus verifiable credentials for
  versioned agents, functions and apps, so a buyer can verify what it installs
  and who authored it.
- **Trust anchors (W3.3).** Proof-of-run hashes anchored on chain with the
  underlying business data kept off chain in the platform data layer — audit
  and insurance grade verifiability without custody transfer.

Guard rules that do not change when Web3 lands: business data never leaves the
platform data layer (decision D5), wallets stay invisible to business buyers
(embedded wallet + gas abstraction), and every adoption needs its own ADR.
Building settlement before M0–M4 demand exists is the same cold-start mistake
described above.

## Value exchange

- Sellers get distribution + the take-rate model considered in
  [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md) business-model section.
- Buyers get capabilities that extend the platform without building.
- Platform gets network effects on top of the capability registry — moat
  hypothesis 9 in [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md).

## Non-goals

- No marketplace in M1–M3.
- No un-vetted functions (review, provenance and tenancy audit are
  mandatory — same discipline as the Dynamic Design Skill Layer imports in
  [`architecture/dynamic-design-skill-layer.md`](architecture/dynamic-design-skill-layer.md)).
- No marketplace as a substitute for first-party M0 builder quality.
