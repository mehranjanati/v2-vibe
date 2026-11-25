# Web3 Chain Abstraction — Provider-Independent Blockchain Access (PROPOSED)

> **Status:** `proposed` 2026-11-25. No code exists: no wallet, no RPC client,
> no deploy path in the tree (see `ARCHITECTURE_CONSOLIDATION_REPORT.md`).
> Companion docs: `web3-smart-contract-ir.md`, `web3-layerzero-interop.md`,
> `web3-security-signing.md`. Decisions: `adr-web3-chain-abstraction.md`
> (PROPOSED). Labels: FACT (verified 2026-11-25), ASSUMPTION, OPEN.

## Principle

Chain access is a provider interface, never a vendor SDK scattered through
the control plane. EVM is the first implementation target; the interfaces
stay neutral so a second VM needs a new adapter plus a new ADR, not a
rewrite. All labels below use `evm-` prefixes where EVM-specific.

## Concepts

- Chain registry: `(chainId, name, vm, rpcProfiles, explorer, finalityRule,
  currencySymbol)`. Chain IDs follow the EVM chain-id registry (ASSUMPTION:
  numeric chain id per EIP-155; OPEN: pin the registry source in the ADR).
- Chain adapter interface (language-neutral, planned):
  `getChainProfile · estimateFees · readContract · buildTransaction ·
  simulateTransaction · submitSignedTransaction · getReceipt ·
  subscribeEvents · getFinality`.
- RPC provider interface: endpoint plus failover order plus health probe;
  providers are interchangeable (self-hosted node, managed RPC). No single
  vendor in the core path.
- Contract lifecycle: spec to Contract IR to source to artifact (ABI plus
  bytecode plus metadata) to deployment record (address, chain, tx hash,
  compiler triple, source digest) to verified source on an explorer
  (ASSUMPTION: Sourcify/Blockscout-style verification; OPEN: choose default).
  The record fields and the nine-stage lifecycle are specified in
  `web3-signing-and-deployment.md`.
- Transaction lifecycle: build, simulate, policy check, approve, sign,
  broadcast, confirm. Signing is a separate boundary owned by
  `web3-security-signing.md`; this interface never sees a private key.
- Finality: each chain profile declares its confirmation rule (for example
  N confirmations or epoch rule). Receipt status is never treated as
  finality; monitoring owns the confirmation count.
- Capability discovery: `capabilities()` per adapter (for example
  `simulate:bool, archiveNode:bool, feeHistory:bool`); a missing capability
  refuses the plan at admission, mirroring the execution-provider rule.

## Initial scope: EVM-first

- Language v1: Solidity targeting EVM (FACT: Solidity is an EVM-targeting,
  statically typed language with ABI spec, optimizer, and metadata output;
  `https://docs.soliditylang.org` read 2026-11-25).
- Libraries v1: the package set is pinned in
  `web3-solidity-generation-scope.md` (`@openzeppelin/contracts` 5.6.1, 17 entry
  points with per-file SHA-256); the compiler and build settings are pinned in
  `web3-compilation-and-artifacts.md`.
- Networks v1: one low-cost EVM testnet for development, Ethereum mainnet
  semantics for the security model; no second VM until its adapter ADR lands.
- Explicitly not claimed: Solana, Move/Aptos/Sui, Bitcoin script, or any
  non-EVM execution semantics.

## What is NOT hidden by the abstraction

Chain differences that leak by design: gas model and fee markets, nonce and
replacement semantics, reorg behavior, address formats, signature schemes,
block times, and verifier APIs. The adapter surfaces them; it never papers
over them.

## Open items

- Registry source for chain IDs and RPC defaults.
- Confirmation-count policy per chain profile.
- Explorer/verification provider choice.
- Indexing strategy (polling vs managed indexer).
