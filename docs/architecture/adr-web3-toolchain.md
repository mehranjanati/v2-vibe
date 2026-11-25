# ADR — Smart Contract Toolchain Choice (PROPOSED, evidence-backed)

> **Status:** `proposed` 2026-11-25. A recommendation now exists with
> primary-source evidence; owner approval is still required before any
> manifest, template, or sandbox image names a default.
> **Related:** `architecture/web3-toolchain-pipeline.md` (evidence table).
> Task: `W0-WEB3-02` (DONE 2026-11-25, research plus documentation only).

## Context

The pipeline needs a Solidity compiler manager, a dev framework, standard
libraries, scanners, and verification services. Solidity itself is settled
(EVM target; `https://docs.soliditylang.org` read 2026-11-25). Both framework
candidates are now verified from their official docs (read 2026-11-25):
Foundry (`https://book.getfoundry.sh`) and Hardhat 3
(`https://hardhat.org/docs`).

## Options considered

### A. Foundry only *(partially adopted — primary)*

- Pro: Rust toolkit (Forge, Anvil, Cast, Chisel); Solidity-native tests
  (`.t.sol`, `forge-std/Test.sol`); built-in fuzz (`testFuzz_*`, 256 runs
  default) and invariant mode; fork testing by URL plus block number for
  reproducible forks; cheatcodes (`vm.prank`, `vm.deal`, `vm.warp`,
  `expectRevert`, `expectEmit`); bounded symbolic testing whose docs
  explicitly say an incomplete run is not a proof; single binary with
  `foundry.toml` profiles; no Node runtime inside the sandbox.
- Con: separate Rust toolchain to provision; Solidity-only tests (no
  TypeScript suite without extra tooling); newer symbolic engine is bounded.

### B. Hardhat 3 only *(rejected as primary)*

- Pro: Solidity tests plus TypeScript tests (viem/ethers), plugin ecosystem,
  Hardhat Ignition deployment modules, gas snapshots, code coverage,
  Foundry-compatible cheatcodes, and a "migrate from Foundry" path.
- Con: requires Node.js v22.13.0 or later inside the build sandbox; invariant
  testing not observed in the docs read 2026-11-25; heavier dependency
  surface for an untrusted workload.

### C. Foundry primary, Hardhat 3 compatibility path *(recommended)*

- Both behind one pipeline interface; per-workspace choice, no platform fork.

## Decision (proposed)

Adopt option C. Foundry is the default sandbox toolchain for generated
contracts; Hardhat 3 is supported when a project needs TypeScript/viem tests,
plugins, or Ignition deployments. Gates stay identical for both: static
analysis, simulation, policy evaluation, and approval. Neither framework's
passing tests are an audit.

## Consequences

- Positive: fastest reproducible loop with minimal sandbox dependencies;
  TypeScript-native teams are not excluded.
- Cost: two toolchain images to maintain, and a version-pin policy per image.
- Neutral: no dependency is added to the repository by this record.

## Open

Release pins captured at install time; Hardhat invariant coverage; scanner
shortlist; verifier default (O33); sandbox implementation choice.

