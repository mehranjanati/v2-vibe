# ADR — Wallet-First Signing Authority (PROPOSED)

> **Status:** `proposed` 2026-11-25. Security invariants are normative;
> mechanism choices need owner approval.
> **Related:** `architecture/web3-security-signing.md`,
> `architecture/web3-chain-abstraction.md`.

## Context

Agents must act on-chain without holding keys. The tree has a vault pattern
(`vault:` refs, sealed provider tokens) and an explicit rotation open item
(O24); Web3 must reuse that posture, not invent a weaker one.

## Options

### A. Agent-held keys or keys in prompts/memory *(rejected)*

- Rejected outright: violates invariants 1 to 2, un-auditable, un-isolatable.

### B. Wallet-first with a separate signing boundary *(proposed)*

- Non-custodial connection by default; external/hardware signers
  first-class; any managed signer is a deferred, separately ADR-gated
  option. Build, simulate, policy check, approve, sign, broadcast, and
  confirm stay distinct stages; mainnet needs explicit approval.

## Proposed direction

Adopt option B. Multisig and timelock integration points are declared per
deployment profile before mainnet use.

## Consequences

- Positive: least-privilege signing; auditable approvals; tenant isolation.
- Cost: approval UX and policy schema are prerequisites for mainnet.

## Open

Wallet standard pin; managed-signer custody decision; approval-policy
schema; secret-store rotation tie-in with O24.
