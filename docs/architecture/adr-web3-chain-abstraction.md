# ADR — Chain Adapter and Provider Abstraction (PROPOSED)

> **Status:** `proposed` 2026-11-25. Needs owner approval.
> **Related:** `architecture/web3-chain-abstraction.md`,
> `architecture/adr-execution-provider-contract.md` (pattern reference only).

## Context

Chain access must survive RPC vendor change and chain addition without
rewriting the control plane. The execution-provider contract (D16) proved
the pattern: contract first, then conformance, then adapters.

## Proposed direction

Chain registry plus adapter interface plus RPC provider interface plus
capability advertisement (a missing capability refuses at admission, never
degrades silently). Chain differences (gas, nonce, reorgs, finality) leak
by design through the interface.

## Consequences

- Positive: EVM ships first; a second VM is a new adapter plus ADR.
- Cost: conformance suite per adapter before mainnet use.

## Open

Registry source; confirmation policy; verifier default; indexing strategy.
