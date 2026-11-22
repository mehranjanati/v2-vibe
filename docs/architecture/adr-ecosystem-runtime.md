# ADR — Cloudflare as Provider, wasmCloud Evaluation-Only, No Token MVP

> **Status:** `accepted` 2026-11-21. Runtime: `ecosystem/06-runtime-and-execution-architecture.md`.

## Context

The live system runs on Cloudflare; future portability must not rewrite the MVP.

## Decision

Cloudflare is one execution provider behind the provider contract.
wasmCloud stays evaluation-only until contract plus PoC exist.
No Kubernetes requirement, no chain dependency, no token in any MVP.

## Consequences

Provider swaps need no product rewrites; distributed and Web3 work stays
in P7–P9 backlog until gates pass.
