# ADR — Contract Build and Test Sandbox (PROPOSED)

> **Status:** `proposed` 2026-11-25. Needs owner approval.
> **Related:** `architecture/web3-toolchain-pipeline.md`,
> `architecture/adr-wasm-component-strategy.md` (D21, sandbox story shared),
> `architecture/web3-security-signing.md` (keys never in the sandbox).

## Context

Generated code is untrusted until proven otherwise. Compilation, tests,
fuzz runs, and simulations must not run on the host control plane and must
never see signing secrets.

## Proposed direction

Isolated build/test workers: process, CPU/memory, filesystem, network, and
dependency restrictions with cleanup; network egress denied by default;
compiler, library, template, and tool versions recorded per build. The WASM
Component sandbox story is shared, but EVM semantics come only from the EVM
toolchain — WASM never executes EVM behavior.

## Consequences

- Positive: supply-chain and prompt-injection blast radius contained.
- Cost: sandbox implementation plus per-build provenance storage.

## Open

Container versus isolated-worker implementation; default static-analysis
and fuzz gates.
