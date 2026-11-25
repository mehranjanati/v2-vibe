# ADR — System IR vs Smart Contract IR Separation (ACCEPTED, D22)

> **Status:** `accepted` 2026-11-25 by owner decision **D22**. The contract
> and fixtures exist (`architecture/contract-ir/`); no generator or compiler.
> **Related:** `architecture/adr-system-ir-v1.md`,
> `architecture/system-ir.md`, `architecture/web3-smart-contract-ir.md`,
> `architecture/adr-web3-contract-ir-versioning.md`.

## Context

System IR v1 is the `kind System` manifest spec with exactly one execution
shape and no runtime state. The Web3 direction needs deterministic contract
semantics (storage layout, gas, upgradeability, audit provenance) that do not
belong in an agent/team definition. Without a boundary, every agent manifest
would accumulate compiler flags and bridge settings.

## Options

### A. One IR for everything *(rejected)*

- Pro: single format. Con: couples agents to Solidity and to an interop
  vendor; forces a `MAJOR` IR bump for every contract-toolchain change.

### B. Separate versioned Contract IR, referenced by System IR *(chosen)*

- System IR references contracts by pinned `publisher/name@semver` plus a
  contract manifest; compiler and bridge detail lives only in the Contract IR
  and its target/interop extensions.

## Decision (D22)

Adopt option B, with the owner's structure:

1. Contract IR is a **versioned, canonical, target-independent** model.
2. It has a **generic semantic core** (`spec.abstract`) and **explicit
   target-specific extensions** (`spec.targets` per VM, `spec.extensions` per
   vendor). Anything EVM- or bridge-specific is illegal in the core.
3. **EVM/Solidity is the initial implementation target.**
4. Contract IR **remains separate from System IR**, while System IR can
   **compose and reference on-chain contract capabilities** through pinned
   identifiers.
5. **Generated source is derived from the IR and stays traceable** to its
   specification, toolchain, and artifacts.

## Consequences

- Positive: System IR needs no change; D7 holds; a new VM is a new
  `targets.<vm>` block plus an adapter ADR, not a core rewrite.
- Cost: two specs plus a reference-resolution rule, and a contract registry
  becomes a prerequisite for any Contract Studio work.
- Neutral: no runtime change today; the schema gate validates fixtures only.

## Revisit triggers

1. A real contract cannot be expressed without a core field that is
   inherently VM-specific.
2. System IR needs a reference form that pinned `publisher/name@semver` cannot
   express.
3. Traceability from source to IR to artifact proves insufficient for audit.

