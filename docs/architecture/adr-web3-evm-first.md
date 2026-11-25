# ADR — EVM-First Chain Strategy (PROPOSED)

> **Status:** `proposed` 2026-11-25. Needs owner approval.
> **Related:** `architecture/web3-chain-abstraction.md`, D14 in
> `ecosystem/10-decisions-and-open-questions.md` (no token/chain MVP).

## Context

The platform needs one initial execution target for contracts while keeping
provider interfaces neutral. Evidence: no chain code exists in the tree;
Solidity targets EVM with a documented ABI, optimizer, and metadata output
(source `https://docs.soliditylang.org`, read 2026-11-25).

## Options

### A. Multi-VM from day one *(rejected)*

- Pro: broader reach. Con: multiplies adapters, toolchains, audits, and
  security review before any MVP evidence; violates the dependency-order rule.

### B. EVM-first with neutral interfaces *(proposed)*

- Ship one EVM adapter plus Solidity pipeline; every other VM needs a new
  adapter plus a new ADR. No claim of Solana/Move support until then.

## Proposed direction

Adopt option B. Testnet first; mainnet only under the signing policy in
`architecture/web3-security-signing.md`.

## Consequences

- Positive: smallest auditable scope; interfaces stay replaceable.
- Cost: non-EVM users are explicitly out of scope for the MVP.

## Open

Chain-ID registry source, confirmation policy, verifier default.
