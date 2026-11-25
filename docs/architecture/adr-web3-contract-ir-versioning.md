# ADR — Smart Contract IR Versioning and Compatibility (ACCEPTED, D22)

> **Status:** `accepted` 2026-11-25 by owner decision **D22** (versioned,
> canonical, target-independent IR).
> **Related:** `architecture/web3-smart-contract-ir.md`,
> `architecture/adr-web3-ir-separation.md`,
> `architecture/adr-system-ir-v1.md` (pattern reference).
> Gate: `scripts/validate-contract-ir.mjs`.

## Context

The Contract IR needs the same versioning discipline as System IR and
manifests: immutable artifacts, pinned refs, auditable migrations — or
contract templates will drift silently across generator versions. D22 also
requires the document be canonical and target-independent.

## Decision (D22)

Two counters with distinct jobs plus one structural rule:

1. Envelope `apiVersion` (`vibesdk-contract/v1`) versions the IR **shape**;
   contract `version` (semver) versions the **content**.
2. A breaking shape change ships as a new `apiVersion` with a migration note.
3. Content refs are exact and pinned (`publisher/name@X.Y.Z`); no ranges, no
   `latest`, no git refs.
4. The document is **canonical**: keys sorted recursively, 2-space indent,
   exactly one trailing newline, no duplicate keys — byte-identical
   round-trip is enforced by the gate.
5. The document is **target-independent**: the core (`spec.abstract`) carries
   no VM, compiler, chain, token-standard, or vendor field. Target and vendor
   detail live in `spec.targets` and `spec.extensions` under namespaced keys.
6. Provenance is mandatory: `irDigest` plus `toolchain.name` and
   `toolchain.version`, so generated source can be traced to the exact IR and
   build inputs.
7. Validation is staged — schema shape, then semantic invariants (no vendor
   leakage, no runtime state, pinned deps), then registry resolution. Never
   silent repair.

## Consequences

- Positive: reproducible builds; reviewable diffs; registry-compatible pins;
  a non-EVM target is additive.
- Cost: migration notes per breaking shape change; two layers to validate.
- Neutral: fixtures only — no generator consumes the IR yet.

## Open

NatSpec requirement; registry storage shape (shared O27); corpus linkage;
additional `targets.<vm>` blocks (each needs an adapter ADR).

