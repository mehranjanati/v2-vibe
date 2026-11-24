# ADR — Adaptive Intelligence Boundaries (PROPOSED)

> **Status:** `proposed` 2026-11-23, mission VIBE-ARCH-CONSOLIDATION-ADI-001. Awaiting owner acceptance; does NOT change D7-D18. Related: `adaptive-intelligence-architecture.md`, `composition-lock.md`, `context-activation-and-memory.md`, `evaluation-and-evolution.md`, `../ecosystem/10-decisions-and-open-questions.md`.

## Context

Vibe has four manifest kinds, one System IR, a Go reader, loaders plus search, a trust gate, a gateway/router, and an execution contract — but no stated boundary for composing, activating, personalizing, evaluating, and evolving those capabilities as one platform faculty. Without it each product invents its own composition semantics. Existing constraints: D7 one IR; D8/D8.1 M1/MCP freeze; D9 M0 slice; D10/D11 products plus contracts; D12 provider neutrality; D13 wasmCloud evaluation-only; D14 no token/chain in MVP; D15 identity/lifecycle; D16 provider contract; D17 one control plane; D18 one Tool vocabulary plus one Gate plus one AuditSink.

## Alternatives

A. Ninth product (rejected): ships its own surface, identity, store — forks tenancy/audit/versioning. B. New IR or manifest kind in v1 (rejected): violates D7, splits tooling, needs migration without evidence. C. New runtime (rejected): duplicates execution/durability already contracted in C1-C18. D. Shared capability on existing contracts (recommended): composition/activation/context/memory/eval/evolution as logical modules on the Go plane plus Worker edge plus D1/Redis plus Workflows plus gateway/router plus Gate.

## Recommended architecture

ADI as shared capability: resolver plus lock derive immutable snapshots from System manifests; context assembly plus deterministic activation select authorized subsets with full traces; memory/personalization via scoped provider interfaces; model routing via gateway/router; evaluation gates plus controlled evolution over immutable snapshots; bundles as distribution grouping only. Modular monolith first; no new services, brokers, stores, or training platform without measured need plus ADR.

## Consequences and compatibility

Docs gain five PROPOSED contracts; backlog gains ADI-FOUNDATION through ADI-ECOSYSTEM workstreams (all BLOCKED until deps/decisions clear; first executable proof is M0-consistent single-agent). No code, schema, migration, route, or pricing change in this mission. Fully backward compatible: existing manifests, IR fixtures, gates, loaders, and C1-C18 suites keep passing unchanged.

## Rejected alternatives

Recorded above (A/B/C). Also rejected: hybrid agent-plus-workflow systems without ADR; silent dependency upgrades; activation-minted grants; events as authority; inferred-preference silent override; fine-tuning as MVP requirement; Kafka/NATS/new vector DB/broker by diagram preference; marketplace Verified without registry plus provenance plus review.

## Revisit triggers

Measured resolution/lock load that D1/Redis cannot meet; a second execution provider passing C1-C18 that needs lock-aware admission; registry existence forcing resolver-shape changes; O-decisions (O2/O3/O4/O5/O15/O16) resolving against the chosen options; any D-change by explicit owner ADR.
