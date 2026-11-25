# ADR — LayerZero as a Modular Interop Adapter (PROPOSED, RESEARCH-GATED)

> **Status:** `proposed` 2026-11-25, **verification gate passed for
> architecture**: the official V2 docs were read on 2026-11-25 (protocol
> architecture, OApp, OFT) and recorded in
> `architecture/web3-layerzero-interop.md`. Remaining before any adapter code:
> pin SDK/contract versions and the target-chain list (O32 partial).
> **Related:** `architecture/web3-layerzero-interop.md`; task `W0-WEB3-01`
> (DONE 2026-11-25, research plus documentation only).

## Context

Cross-chain messaging needs a provider behind a generic interface so the
core platform never depends on one bridge. LayerZero V2 is the candidate
first provider (OApp/OFT patterns, Endpoint model, DVN plus executor roles).

## Options

### A. LayerZero as a core dependency *(rejected)*

- Con: every app pays bridge complexity; a bridge change forces a platform
  change. Rejected on D11/D12 grounds.

### B. Generic interop interface plus a LayerZero adapter *(proposed)*

- App sees routes and delivery states; the adapter owns Endpoint IDs, DVN
  lists, executor settings, and option blobs. Non-omnichain apps carry zero
  LayerZero artifacts.

## Proposed direction

Adopt option B after the verification gate in
`architecture/web3-layerzero-interop.md` passes (official docs re-read with
URLs and dates, one pinned SDK version, stub Endpoint conformance first).

## Consequences

- Positive: bridge replaceability; reviewable DVN/peer config.
- Cost: adapter plus conformance suite before any mainnet route.

## Open

All protocol details until the docs verification lands; OApp/OFT API claims.
