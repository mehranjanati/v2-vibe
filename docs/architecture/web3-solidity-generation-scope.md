# Web3 — Solidity Generation Scope and Template Strategy (PROPOSED, DESIGN ONLY)

> **Status:** `proposed` 2026-11-25. Design only: **no generator, compiler,
> dependency, route, table, or template file exists** in the tree.
> Task: `W1-WEB3-01` (acceptance: scope list with in/out, pinned module set with
> digests, template lifecycle rules).
> Companions: `web3-smart-contract-ir.md` (the accepted IR this scope compiles
> from), `web3-toolchain-pipeline.md` (stages), `web3-security-signing.md`
> (invariants), `web3-chain-abstraction.md`.
> Decisions: **D22** accepted Contract IR v1; `adr-web3-toolchain.md` is still
> PROPOSED, so **no template may name a default framework** here.
> Labels: FACT (verified 2026-11-25 from primary sources or computed locally),
> ASSUMPTION, OPEN.

## What this task settles, and what it refuses to

It settles three things and nothing else:

1. **Which contract kinds v1 generates**, and which it explicitly refuses.
2. **Which third-party modules generated code may import**, pinned by exact
   version and per-file SHA-256.
3. **How a template is versioned, reviewed, and retired** so a generated
   contract is reproducible years later.

It refuses to: emit Solidity, choose a compiler invocation (that is
`W1-WEB3-02`), choose Foundry or Hardhat (that is `W3-TOOL`, still PROPOSED),
define deployment or signing (that is `W3-WEB3-01` plus
`adr-web3-signing-authority.md`), or add a field to Contract IR v1. Contract IR
is frozen at `vibesdk-contract/v1` for this work; every rule below maps onto
fields that already exist in `contract-ir/contract.schema.json`.

The pipeline rule this document assumes is already normative: **template → IR →
generated source → artifacts**, never template → source. A template that emits
Solidity directly would bypass IR validation, the security gates, and the
lineage record, so it is not a template in this design.

## Scope — contract kinds (acceptance criterion 1)

A kind is *in scope* only when all four hold: the IR can express it with existing
v1 fields, the logic is dominated by access control and state transitions rather
than numeric math, a standard library module covers the risky part, and a
mutation-testable acceptance list can be written without a fork of a live
protocol. Kinds that fail any of the four are out, with the reason recorded.

| # | Kind (IR shape) | `assets.kind` | `targets.evm.tokenStandards` | `upgradeability.strategy` | Why in scope |
|---|---|---|---|---|---|
| 1 | Escrow / vault custody | `fungible` | — | `immutable` | Matches the accepted core-only fixture; state machine plus owner policy; no external price input |
| 2 | Treasury / payment splitter | `fungible` | — | `immutable` | Fixed shares, pull-payment pattern; caps blast radius |
| 3 | Fixed-supply token | `fungible` | `erc20` | `immutable` | Supply minted once at construction; no rebasing or fee-on-transfer |
| 4 | Mintable/burnable token with roles | `fungible` | `erc20` | `immutable` | Role policy expressed as `spec.abstract.policies`; supply caps are invariants |
| 5 | NFT collection | `non-fungible` | `erc721` | `immutable` | Sequential mint plus base URI; no marketplace or royalty hooks in v1 |
| 6 | Multi-token collection | `non-fungible` | `erc1155` | `immutable` | Batch mint/transfer; per-id supply as an invariant |
| 7 | Access-controlled registry / allowlist | `none` | — | `immutable` | Pure role plus membership model; cheapest kind to prove correct |
| 8 | Upgradeable registry (UUPS) | `none` | — | `proxy` | Allowed **only** with `risksAcknowledged: true`; see the upgradeability rule below |

Two scope rules that are not negotiable:

- **Upgradeability is opt-in and explicit.** A `proxy` contract requires
  `spec.abstract.upgradeability.risksAcknowledged = true`; the generator refuses
  `proxy` without it, and the emitted NatSpec carries the acknowledged risk. An
  agent may never silently choose an upgradeable shape.
- **No payable function without a policy.** The docs gate already refuses a
  `payable` function with no `policy`; the scope adds that every referenced policy
  must be one of four authority shapes — owner-only, role-gated, allowlist, or
  open — so a generator never invents an authority model.

### Explicitly out of scope for v1

Each exclusion names the reason it is excluded. Being on this list is a decision,
not an oversight.

| Out of scope | Reason |
|---|---|
| AMM/DEX math, lending, liquidation, perpetuals | Invariant and rounding errors are the dominant loss source; needs audit-grade math and fork-based invariant testing, not prompt generation |
| Governance (`Governor`, timelock, voting escrow) | Needs a tokenomics and quorum decision that does not exist (O6) |
| Bridge/OApp generation (LayerZero adapters) | `adr-web3-layerzero-adapter.md` is still PROPOSED; interop stays a manifest namespace, never generated in v1 |
| EIP-2535 diamonds, EIP-4626 vaults, ERC-4337 accounts | Each needs its own IR mapping and review; none is expressible acceptably with v1 fields |
| Oracle/price consumers | Requires a price-source trust and staleness policy; v1 has no `oracle` concept |
| `Yul`, inline assembly, `unchecked` blocks | Defeats the review story; compiler settings belong to `W1-WEB3-02` |
| Formal verification artifacts | A future gate (`W3-VER` extension), not an IR field in v1 |
| Tokenomics, emissions, price design | Product decision, not generation scope |
| Non-EVM targets | Each needs a `targets.<vm>` block plus an adapter ADR (D22 rule) |
| Fee-on-transfer, rebasing, ERC-777 hooks, `permit` in v1 | Each changes the accounting invariants; `permit` is a candidate v1.1 addition |
| Gas-golfing micro-optimizations | Never a generation goal; correctness and auditability come first |

## Pinned module set (acceptance criterion 2)

Generated code may import **only** the files listed below. The set is one npm
package at an exact version; every digest was computed locally from the published
tarball on 2026-11-25 (`shasum -a 256`), not quoted from a page.

**Package provenance** (FACT: read from `https://registry.npmjs.org/@openzeppelin/contracts/latest` and the tarball, 2026-11-25):

| Field | Value |
|---|---|
| Package | `@openzeppelin/contracts` |
| Version | `5.6.1` (exact; ranges and `latest` are refused) |
| License | MIT |
| Tarball | `https://registry.npmjs.org/@openzeppelin/contracts/-/contracts-5.6.1.tgz` |
| Tarball SHA-256 | `bd202ef9a2fd09f6005edc18437b4d3090841d0f676c01c814ee9608581d2e63` |
| npm shasum (SHA-1) | `90c1cd427b3c1007ada4f42378ce84cc2a2145a5` |
| npm integrity (SHA-512) | `sha512-Ly6SlsVJ3mj+b18W3R8gNufB7dTICT105fJhodGAGgyC2oqnBAhqSiNDJ8V8DLY05cCz81GLI0CU5vNYA1EC/w==` |
| Declared pragma (observed in the files) | `^0.8.20`, except `proxy/utils/UUPSUpgradeable.sol` at `^0.8.22` |

**Allowed entry points** (FACT: SHA-256 of each file inside the tarball):

| Module path (inside `package/`) | Purpose | Pragma | SHA-256 |
|---|---|---|---|
| `access/Ownable.sol` | single-owner authority | `^0.8.20` | `38578bd71c0a909840e67202db527cc6b4e6b437e0f39f0c909da32c1e30cb81` |
| `access/Ownable2Step.sol` | two-step ownership transfer | `^0.8.20` | `c75345d7b9feef17ae29f2a800ca2f65f8021cf80436a41d6ec00f57f5180f36` |
| `access/AccessControl.sol` | role-based authority | `^0.8.20` | `97abba9ae4ee97f16f17b7b688d90f6f1195b79edfba344eacc58406e088b51e` |
| `utils/Pausable.sol` | emergency stop | `^0.8.20` | `2c26130748f06552c65ca74d89eaf78c9324a3cbac3ba6fdbadaa8beba0cb0b4` |
| `utils/ReentrancyGuard.sol` | reentrancy guard on value paths | `^0.8.20` | `94e409e8f6e3184236651a6cc2b1a6a3ea0f0a25eb85b71e524ad5791bb2fbc8` |
| `utils/Address.sol` | address checks | `^0.8.20` | `73e54e15285455f0e01e136c3663732641537614723ae5dbd820eb3cb036b1fd` |
| `utils/Strings.sol` | string rendering for URIs | `^0.8.20` | `e318709bbe73a7831d64c1351e8bdc43780b245f25191234e361e6ab9b98c67b` |
| `utils/structs/EnumerableSet.sol` | allowlist membership | `^0.8.20` | `76c7af99039281b3075780b21b4e2f6be0166d09f89c24171b4bed3b1961c810` |
| `utils/cryptography/ECDSA.sol` | signature checks | `^0.8.20` | `ba7c2d314fcd61c9783e0b3f0c664d004b76b408fda6a84edc8267d17c5012d6` |
| `utils/cryptography/EIP712.sol` | typed-data domain | `^0.8.20` | `2845caaaf70757b6cce612ae4ffbec2c382cb3d1cb9765c98fa649c0f5f1c794` |
| `utils/introspection/ERC165.sol` | interface identification | `^0.8.20` | `61a2b12c96857aaa35f48aeda67a4281637a42494c0b3795be0ae976aa7b3793` |
| `token/ERC20/ERC20.sol` | fungible token base | `^0.8.20` | `50f34ae16a067a41c2c1091445d11d63788e54c717677cb6d6b0e4cdea2ad21d` |
| `token/ERC721/ERC721.sol` | NFT base | `^0.8.20` | `ef8e2398c78851bd2ec12bc48d923058c557889d5a3d5ee4919c17ff0fb3517e` |
| `token/ERC1155/ERC1155.sol` | multi-token base | `^0.8.20` | `35ad182b74ce08152cde00fd41226850c635e1b9445f3a9d7b4888f2ba55717e` |
| `proxy/ERC1967/ERC1967Proxy.sol` | upgradeable deployment | `^0.8.20` | `41a3040398d53999dea3251ff8906e11ec1a699362a1d8f4a55bfc7709cc00f3` |
| `proxy/utils/UUPSUpgradeable.sol` | UUPS upgrade path | `^0.8.22` | `b5af3bfd79a32c6da2d09b0be38d9b63dcd0d159a1a4a0198431fd00acc1fd52` |
| `proxy/utils/Initializable.sol` | initializer guard | `^0.8.20` | `f527a063813c2bf60c153fb08e38539578935402894fcc36fac42324ca325d3b` |

**Pin rules (normative):**

1. A module enters the set by exact path plus digest plus pragma. There is no
   wildcard, no version range, and no "compatible" entry point.
2. **Transitive imports must be closed.** An allowed file imports other files from
   the same tarball (for example `ERC20.sol` pulls `IERC20`, `Context`,
   `IERC20Metadata`, and `draft-IERC6093`). The generator resolves imports only
   inside the pinned tarball and records the resolved closure in the build
   provenance; a closure file outside the set is a hard failure, never a warning.
3. A digest mismatch at build time is a **hard failure**. Regenerating a digest
   because it changed is exactly the supply-chain attack this pin exists to stop.
4. Upgrading the package is one deliberate change: a new pin block, a new
   contract IR `provenance.toolchain` value, a re-run of the security gates, and a
   changelog line. Never a silent bump.
5. `contract-ir/examples/` fixtures keep placeholder digests on purpose. Pin
   digests live here and, later, in the artifact manifest — never in a fixture.
6. The set is an allowlist of *entry points*, not a claim that the library is
   safe. Passing the pin says "this is the code we reviewed", not "this is an
   audit" (invariant 5 of `web3-security-signing.md`).

## NatSpec policy

NatSpec is required on generated source because it is the only documentation that
travels with the bytecode metadata. It is not an audit and must never be phrased
as one.

| Element | Rule |
|---|---|
| Contract | `@title` plus `@notice` stating the contract's purpose in one sentence |
| Every public/external function | `@notice` (user-facing behaviour) and `@param` for **every** input |
| Every returning function | `@return` for **every** output value |
| State-changing risk | `@dev` naming the guard used (owner, role, pause, reentrancy) |
| Value movement | `@custom:security` describing the threat considered (reentrancy, replay, front-running) |
| Upgradeable contracts | `@custom:security` repeating the acknowledged upgradeability risk |
| Traceability | Source header carries `@custom:ir-digest` and `@custom:toolchain` (name plus exact version), so an auditor can re-derive the artifact from the IR |
| Forbidden | Secrets, keys, endpoints, API tokens, or live addresses in NatSpec; speculative claims ("audited", "safe", "guaranteed") |

A missing `@param` for a declared input is a generation failure, not a style
nit — `W2-WEB3-01` turns that into a gate.

## Template strategy (acceptance criterion 3)

A template is **not** a project scaffold. It is a pinned, reviewed starting point
that produces Contract IR; source is then generated from the IR under the pinned
module set. This keeps one generation path and one validation gate.

**A template contains exactly four things:**

1. A Contract IR skeleton with documented placeholders (kind, roles, policies,
   invariants, required tests) that must validate against
   `contract-ir/contract.schema.json`.
2. An acceptance list: the named unit, invariant and negative tests the generated
   contract must pass, expressed as IR `spec.abstract.tests` entries plus the
   intended test names.
3. A NatSpec outline (the sections above, with placeholders for the
   contract-specific sentences).
4. The module-set pin it depends on (this document's set plus version).

**A template never contains:** generated Solidity, bytecode, deployed addresses,
keys, `foundry.toml`, `hardhat.config.ts`, deploy scripts, or a framework default.
Framework binding is `W3-TOOL` (PROPOSED) and scaffolding is `W1-WEB3-02`; naming
either here would pre-empt an owner decision.

**Identity:** `templateRef = <publisher>/tpl-<kind>@<semver>` (for example
`vibe/tpl-escrow-vault@0.1.0`). The generated system records the `templateRef`,
the module-set id, and the IR digest in its lineage — three separate links, so a
template change never silently rewrites history.

### Template lifecycle rules

| State | Entry condition | What may change |
|---|---|---|
| `draft` | Author writes the IR skeleton plus the acceptance list | Anything; not usable by generation |
| `reviewed` | IR validates; acceptance list complete; NatSpec outline present; a named reviewer signed off | Typos only |
| `pinned` | `reviewed` plus a fixed semver and a recorded module-set pin | **Nothing.** Any change is a new version |
| `deprecated` | A pinned version was superseded, or its module set withdrawn | Nothing; it stays resolvable and readable |

Rules that make the lifecycle enforceable later:

- **Pin means frozen.** A change to a pinned template — even a comment — creates a
  new semver. Generated artifacts reference the exact version they used.
- **Deprecation never deletes.** A deprecated template still resolves so old
  lineage stays explainable; generation refuses it and points at the successor.
- **No `latest`.** Generation requires an explicit `templateRef` (same rule as
  contract `version` and manifest references).
- **Security-relevant change is a major bump.** Changing a role model, a policy,
  an invariant, or the module-set pin bumps the major version and re-runs
  `W2-WEB3-01` before the new version may become `pinned`.
- **Template version and IR version are independent.** A template version is not
  an IR `apiVersion`; a new IR shape requires re-validating every pinned template.

## Relationship to the rest of the Web3 track

| Task | Consumes from here | Adds |
|---|---|---|
| `W1-WEB3-02` compilation + artifacts | the pinned module set, the pragma constraint (solc must satisfy `^0.8.22` when a proxy module is used), the source-header traceability rule | compiler pin, optimizer/EVM settings, artifact manifest, reproducibility evidence |
| `W2-WEB3-01` security gates | the payable-policy rule, the upgradeability acknowledgement, the NatSpec table, the module allowlist | rejection of out-of-scope kinds, dependency-closure check, scanner integration |
| `W3-WEB3-01` wallet/signing/deploy | nothing (no keys or addresses exist here) | signing authority, deployment profiles, verification |
| `W4-WEB3-01` interop | nothing (no OApp generation in v1) | LayerZero adapter implementation |

Nothing in this document adds a Contract IR field, a route, a database table, a Go
package, an npm dependency, or a runtime behaviour. `go.mod` and `package.json`
are untouched by design.

## Evidence

| Claim | Source / method | Date |
|---|---|---|
| `@openzeppelin/contracts` latest is `5.6.1`, MIT | npm registry metadata for the package | 2026-11-25 |
| Tarball SHA-256 `bd202ef9…` | `shasum -a 256` over the downloaded tarball | 2026-11-25 |
| Per-file SHA-256 values in the table | `shasum -a 256` over each file inside the extracted tarball | 2026-11-25 |
| Pragmas `^0.8.20` / `^0.8.22` | `pragma solidity` line read in `Ownable.sol`, `ERC20.sol`, `UUPSUpgradeable.sol` | 2026-11-25 |
| Contract IR fields used by the scope table | `contract-ir/contract.schema.json` in this repo | 2026-11-25 |
| Toolchain choice is still open | `adr-web3-toolchain.md` status banner (PROPOSED) | 2026-11-25 |

Not evidence: the fixtures in `contract-ir/examples/` carry placeholder digests and
are schema fixtures only.

## Acceptance criteria mapping

| Criterion (backlog) | Where it is answered |
|---|---|
| Scope list with in/out of scope | "Scope — contract kinds" plus "Explicitly out of scope for v1" |
| Pinned module set with digests | "Pinned module set": 17 entry points, each with a computed SHA-256, plus the tarball digests |
| Template lifecycle rules | "Template strategy" plus the `draft → reviewed → pinned → deprecated` table and its five rules |

Verification for this task is documentation-only: `bun run docs:check` must stay
green (six validators), and no code, route, table, or dependency may appear.

## Open items

- **Machine-readable pin manifest.** The digest table is human-readable today. A
  JSON pin file plus a verification gate belongs to `W1-WEB3-02`, where a build
  can actually check digests and the closure hash.
- **Transitive closure digest.** Whether provenance records the closure as one
  aggregate digest or a per-file list is `W1-WEB3-02`'s decision.
- **ERC-20 `permit` / `EIP-2612`** in v1.1 — needs an invariants review.
- **Pausable on non-owner roles** — currently owner-only; a role-gated pause is a
  v1.1 scope question.
- **Solc pin vs pragma.** The scope requires a solc satisfying `^0.8.22` whenever a
  proxy module is used; the exact pin (and whether `0.8.28`, the value in the IR
  fixture, stays the default) is `W1-WEB3-02`.
- **NatSpec in the IR.** Whether `@custom:security` becomes a core IR field rather
  than a generator convention is an open D22 follow-up.

