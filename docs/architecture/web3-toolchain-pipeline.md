# Smart Contract Toolchain — Reproducible Build and Test Pipeline (PROPOSED)

> **Status:** `proposed` 2026-11-25. No toolchain exists in the tree.
> Companions: `web3-smart-contract-ir.md`, `web3-security-signing.md`.
> Decision: `adr-web3-toolchain.md` (PROPOSED, toolchain choice OPEN).
> Labels: FACT (verified 2026-11-25 in primary docs), ASSUMPTION, OPEN.

## Pipeline stages (normative order)

Requirement analysis, specification, Contract IR generation, IR validation,
threat and risk analysis, source generation, dependency resolution,
deterministic compilation, unit testing, integration testing, fuzz and
invariant testing where appropriate, static analysis, simulation and
deployment preview, policy evaluation, human approval where required,
signing and deployment, on-chain verification, monitoring handoff.

## Tooling evaluation (evidence, read 2026-11-25)

- Solidity compiler: required for the EVM-first target (FACT: versioned
  releases with breaking-change notes, optimizer, EVM targets, ABI plus
  metadata outputs; `https://docs.soliditylang.org` read 2026-11-25).
- Framework comparison is now **evidence-backed** (both official sites read
  2026-11-25) — table below. Recommendation is PROPOSED (W3-TOOL), not
  accepted: no manifest may name a default until the owner accepts it.
- OpenZeppelin-style libraries: candidate standard modules (ASSUMPTION:
  access control, token standards, proxy patterns; OPEN: pin exact set).
- Static analysis and scanners: OPEN shortlist with evaluation criteria
  (detection value, false-positive cost, CI runtime, maintenance).
- Contract verification services: ASSUMPTION Sourcify/Blockscout-style;
  OPEN: choose default per chain profile.

### Foundry vs Hardhat (verified capabilities)

| Requirement | Foundry (`book.getfoundry.sh`) | Hardhat 3 (`hardhat.org/docs`) |
|---|---|---|
| Toolkit | Rust: Forge (build/test/debug/deploy/verify), Anvil (local node plus forking), Cast (chain CLI), Chisel (Solidity REPL) | Node-based; `npx hardhat --init`; contracts, test, ignition, scripts plus `hardhat.config.ts` |
| Test language | Solidity-native; `forge test`; `.t.sol`; `forge-std/Test.sol`; `test_` prefix; `setUp()` | Both: Solidity tests (`.t.sol`) and TypeScript tests via viem or ethers.js |
| Fuzzing | Built in: `testFuzz_*`, default 256 runs observed in docs output | Solidity fuzz tests documented in Hardhat 3 tutorial |
| Invariant testing | Present (invariant mode plus `symbolic.invariant_depth` config) | Not observed in the pages read 2026-11-25 — OPEN |
| Fork testing | `--fork-url` plus `--fork-block-number` for reproducible forks | Forking a network documented; pinning depth OPEN |
| Deployment | `forge script` plus broadcast files | Hardhat Ignition deployment modules |
| Cheatcodes | `vm.warp`, `vm.roll`, `vm.prank`, `vm.deal`, `vm.store`, `expectRevert`, `expectEmit` | Foundry-compatible cheatcode set documented (asserts, expect*, deal, prank, warp, …) |
| Honesty tooling | Bounded symbolic testing with z3; docs state a PASS is scoped to the model and "incomplete" is not a proof | Gas statistics, gas snapshots, code coverage |
| Determinism | Single binary plus `foundry.toml` profiles; call isolation default (`--no-isolate` to opt out) | Versioned npm packages plus build profiles and isolated builds |
| Runtime cost | No Node required in the sandbox | Requires Node.js v22.13.0 or later |
| Compatibility path | — | "Migrate from Foundry" guide exists; plugin ecosystem |

### Recommendation (PROPOSED, W3-TOOL)

Use **Foundry as the primary sandbox toolchain** for the contract pipeline:
Solidity-native tests, built-in fuzz plus invariant modes, reproduce-by-block
forking, cheatcode-level failure simulation, and a single pinned binary that
needs no Node runtime inside the untrusted sandbox. Use **Hardhat 3 as the
compatibility path** when a project needs TypeScript/viem tests, the plugin
ecosystem, or Ignition modules.

Both sit behind one pipeline interface, so the choice is per-workspace
configuration rather than a platform fork. Neither is "safer" by default: the
gates in this pipeline (static analysis, simulation, policy) stay mandatory
regardless of framework, and neither tool's passing tests constitute an audit.
Owner acceptance of W3-TOOL is still required before any manifest or template
names a default.

## Execution isolation (normative)

Compilation and testing of generated code are untrusted workloads:

- Sandboxed workers with process, CPU/memory, filesystem, network, and
  dependency restrictions plus cleanup; network egress denied by default.
- The build/test sandbox is separate from chain execution and from the
  signing boundary; it never holds keys (see `web3-security-signing.md`).
- Record compiler, dependency, template, and tool versions per build for
  reproducibility. Artifacts stored: ABI, bytecode, metadata, source digest.
- Relationship to the WASM Component direction: the sandbox story is shared
  (see `adr-wasm-component-strategy.md`), but WASM never executes EVM
  semantics; EVM behavior comes from the EVM toolchain only.

## Open items

- Actual binary/package versions pinned in the sandbox image (release capture
  at install time; the docs pages do not state a current release number).
- Hardhat invariant-testing coverage (not observed in the docs pages read
  2026-11-25).
- Pinned solc version policy and EVM-target policy.
- Default static-analysis and fuzz gates for CI.
- Sandbox implementation choice (container vs isolated worker).
