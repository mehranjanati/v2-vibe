# ADR — Ecosystem Product Independence plus Shared Contracts

> **Status:** `accepted` 2026-11-21. Products: `ecosystem/01-product-ecosystem.md`.

## Context

Eight products must evolve independently without fragmenting identity,
security, or execution guarantees.

## Decision

Adopt the independence rule plus the twelve-contract set in
`ecosystem/02-product-boundaries.md`. Shared code lives behind versioned
contracts; duplication needs a written reason.

## Alternatives

Monorepo duplication (rejected: drift), single-product platform (rejected:
blocks independence), premature microservices split (rejected: no ops need).

## Consequences

Products release independently; contracts change only via ADR plus migration.
