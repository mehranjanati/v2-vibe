# Web3 Technology Stack Review (PROPOSED 2026-11-25, no dependency change)

> **Status:** `proposed`. This matrix records the current stack plus Web3
> candidates. Nothing below adds or replaces a dependency; every Web3 row is
> gated on its W3 decision plus the named verification.

## Existing stack (verified in-tree, unchanged)

| Concern | Current | Web3 role |
|---|---|---|
| Orchestration substrate | Eino (`backend/pkg/agent`, DeepAgent team) | Off-chain reasoning, planning, tool calls only; never EVM execution or signing |
| Canonical model | System IR v1 plus four manifests (`bun run docs:check`) | Whole-system model; references contracts by pinned IDs |
| Execution | D16 provider contract plus Cloudflare Workflow interpreter | Pattern reference for the chain-adapter contract; chains stay out of the core runtime |
| Sandboxing direction | D21 WASM Component strategy (sandboxed tool execution) | Shared sandbox story; EVM semantics come only from the EVM toolchain |
| Secrets | `vault:` refs, sealed provider tokens, per-route auth | Reused posture; keys never in prompts, memory, logs, or artifacts |
| Data | Redis VFS plus D1 lineage | Deployment records and signing audit extend lineage; no new store without ADR |

## Web3 candidates (none adopted)

| Concern | Candidate | Decision gate | Notes |
|---|---|---|---|
| Contract language | Solidity (EVM target) | W3-EVM | Verified language scope in primary docs 2026-11-25; version pin OPEN |
| Dev framework | Foundry primary, Hardhat 3 compatibility path | W3-TOOL (PROPOSED) | Both verified from official docs 2026-11-25; owner approval pending before any default is named |
| Standard libraries | OpenZeppelin-style pinned modules | W3-TOOL | Exact set OPEN |
| Static analysis | Shortlist scanners | W3-TOOL | Detection value vs false-positive cost vs CI runtime |
| Fuzz/invariant | Foundry `testFuzz_*` plus invariant mode (Hardhat 3 has Solidity fuzz tests; invariant coverage OPEN) | W3-TOOL | Required gate class regardless of framework |
| RPC access | Interchangeable providers behind the adapter | W3-CHAIN | Failover plus health probes; no vendor in the core path |
| Wallet connection | WalletConnect-style standard | O30 | Pin after evaluation; session scoping plus revocation required |
| Source verification | Sourcify/Blockscout-style per chain | O33 | Default per chain profile |
| Interop provider | LayerZero V2 adapter (architecture verified 2026-11-25) | W3-LZ (PROPOSED) | Five-layer model, OApp/OFT, X-of-Y-of-N verification; stub conformance before any live route |
| Indexing/monitoring | Polling vs managed indexer | W3-CHAIN | Confirmation-count policy per chain profile |

## MVP versus future

MVP (proposed scope, testnet only): Solidity pipeline, one EVM adapter,
one testnet profile, wallet-signed transactions, isolated sandbox,
policy-gated approvals, deployment records, monitoring handoff. Future:
mainnet with multisig/timelock review, managed signing (ADR-gated),
additional VMs (each a new adapter plus ADR), broader scanner and verifier
coverage.

## Sources checked 2026-11-25

- Solidity docs (`https://docs.soliditylang.org`): EVM target, optimizer, ABI,
  metadata confirmed. No API invented from it.
- LayerZero V2 docs (`https://docs.layerzero.network`): **reachable**;
  protocol architecture (five layers), OApp reference, and OFT reference read
  and recorded with URLs in `web3-layerzero-interop.md`. SDK versions and the
  target-chain list remain OPEN.
- Foundry book (`https://book.getfoundry.sh`): **reachable**; Forge/Anvil/
  Cast/Chisel, Solidity tests, fuzz default, cheatcodes, fork pinning, and the
  bounded-symbolic limitations recorded.
- Hardhat docs (`https://hardhat.org/docs`): **reachable**; Hardhat 3 setup,
  Solidity plus TypeScript tests, Ignition modules, gas and coverage tooling,
  Foundry-compatible cheatcodes recorded.
