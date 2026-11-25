# Web3 — Deterministic Compilation and Artifact Pipeline (PROPOSED, DESIGN ONLY)

> **Status:** `proposed` 2026-11-25. Design only: **no compiler, sandbox, build
> job, dependency, route, or table exists** in the tree.
> Task: `W1-WEB3-02` (acceptance: reproducibility rules recorded, artifact
> manifest shape defined, sandbox boundary referenced with no keys in the sandbox).
> Companions: `web3-solidity-generation-scope.md` (W1-WEB3-01: scope plus the
> pinned OZ 5.6.1 module set this build consumes), `web3-toolchain-pipeline.md`
> (stage order), `web3-security-signing.md` (key invariants),
> `adr-web3-execution-sandbox.md` (W3-SANDBOX), `web3-smart-contract-ir.md`
> (Contract IR v1 inputs).
> Decisions: **D22** (Contract IR v1) accepted; `W3-TOOL` (framework) still
> PROPOSED, so this design binds **no** framework — it defines the compiler
> invocation the framework or the direct `solc` call must satisfy.
> Labels: FACT (measured locally on 2026-11-25 or read from a primary source),
> ASSUMPTION, OPEN.

## What this task settles

1. **A pin and a settings profile** that make "compile the same IR twice → the same
   bytes" a checkable claim instead of a hope.
2. **An artifact manifest** that records every input which can change the output,
   so a third party can re-derive the artifact.
3. **A sandbox boundary** for the build stage: what goes in, what comes out, what is
   denied — and why no key may ever enter.

The measured facts below come from running the official `solc` 0.8.39 binary in a
throwaway `/tmp` directory. Nothing was added to the repository: `go.mod`,
`package.json`, and `bun.lock` are untouched, and no sandbox or build job exists.

## Compiler pin

Two distribution channels exist for the same compiler. A build profile picks one
and records which — mixing channels for one contract version is not allowed,
because their byte-identical equivalence is unproven (OPEN).

**Native binary (preferred inside the sandbox: no Node runtime).**

| Field | linux-amd64 | macosx-amd64 |
|---|---|---|
| Version | `0.8.39` | `0.8.39` |
| Build (commit) | `commit.ce47f2c8` | `commit.ce47f2c8` |
| `longVersion` | `0.8.39+commit.ce47f2c8` | `0.8.39+commit.ce47f2c8` |
| SHA-256 | `b8b0642d46838575d3177628b685564b52f51f2aee546cb4dce781951d23c666` | `6134d77c433e3b714b64b4101a6147cacff93d50137c0b66ab80825fb274ef51` |
| keccak256 | `0x1160f6dbe1b6bc5eb2f4d3e5a9f1ad36b439c37f9421058d88ac2c6136f5095d` | `0x6e1d75c0f920a5e76565d3eca5cf3c349ee990d2e1dda8b3e2c3d34076f43649` |
| Downloaded and hashed locally | from `binaries.soliditylang.org` | **verified: local digest matched the published `list.json`** |

**npm `solc` (wasm; for a Node-based image only).**

| Field | Value |
|---|---|
| Version | `0.8.39`, MIT |
| shasum (SHA-1) | `56400b9bc78b4fb02baf68fa6dc3e04fe0d26d35` |
| integrity (SHA-512) | `sha512-DZgQzDMLd0U+DhaIi9o+W1Kb1GxySJgQRVRHMhbSzfQ5sp6a8sA8ePiAc9ZbgzUxGclUJtjFGcuiGF5X0pt4YA==` |

Rules:

- The pin is **version + commit + per-platform digest**. `latest` is refused, and so
  is "whatever the image has".
- The compiler identity recorded in an artifact is `longVersion` (which is shared
  across platform builds — FACT above) **plus** the platform digest, so a rebuild
  on another OS is explainable rather than mysterious.
- 0.8.39 satisfies the module-set constraint from `W1-WEB3-01` (highest pragma
  `^0.8.22`). Any later pin must re-check that constraint.

## Settings profile (normative)

Compilation happens through **Standard JSON** (`solc --standard-json`), never a
hand-assembled CLI invocation, because only Standard JSON pins every input in one
object that can be hashed.

```json
{
  "language": "Solidity",
  "sources": { "src/EscrowVault.sol": { "content": "…" } },
  "settings": {
    "remappings": ["@openzeppelin/contracts/=vendor/@openzeppelin/contracts-5.6.1/"],
    "optimizer": { "enabled": true, "runs": 200 },
    "evmVersion": "cancun",
    "metadata": { "bytecodeHash": "ipfs", "appendCBOR": true },
    "outputSelection": {
      "*": { "*": ["abi", "evm.bytecode.object", "evm.deployedBytecode.object", "metadata", "storageLayout"] }
    }
  }
}
```

**Every field above is a build input.** The profile is named (`evm-default`) and
versioned; a change to any value produces a different artifact and must be a new
profile or a new profile version, never an edit in place.

| Setting | Rule | Why |
|---|---|---|
| `evmVersion` | Take the IR `targets.evm.evmVersion`; when the IR omits it, use **`cancun`** — never the compiler default | FACT: 0.8.39 defaults to **`osaka`**, and the IR enum is only `cancun`, `prague`, `shanghai`, `paris`, `london`. Relying on the default would emit a target the IR cannot express |
| `optimizer` | Explicit `enabled` and `runs`; never inherited | Optimizer settings change bytecode; `runs` is recorded and appears in metadata |
| `metadata.bytecodeHash` | `ipfs` by default (keeps on-chain verification linkage); `none` only with a recorded reason | FACT: `none` removes the IPFS metadata hash from the trailer (probe: 510 → 426 hex chars) but keeps the solc version marker |
| `metadata.appendCBOR` | `true`, or `false` **only** with `bytecodeHash: none` or the field omitted | FACT: 0.8.39 rejects `appendCBOR: false` together with `bytecodeHash: ipfs` (`JSONError`) |
| `settings.libraries` | Must stay empty in v1 — no external linked libraries | A linked-library address is not a build input and would make the artifact irreproducible; v1 compiles all sources together |
| `remappings` | Exactly the pinned mapping(s); recorded verbatim in metadata | FACT: metadata records them (observed as `":@openzeppelin/contracts/=…"`) |


## Reproducibility rules (acceptance criterion 1)

Each rule names the failure it prevents. R4 and R5 are the two a naive
implementation gets wrong, and both were measured rather than assumed.

**R1 — Pin the compiler, not the toolchain brand.** Version + commit + platform
digest; a different compiler is a new artifact, never a rebuild.

**R2 — Always pass `evmVersion` from the IR.** The IR enum is the closed set; a
value outside it (`osaka`, `amsterdam`, `@future`) is refused at admission, not
silently coerced.

**R3 — Fix the workspace layout.** Generated source lives at
`src/<ContractName>.sol`; the pinned library lives at
`vendor/@openzeppelin/contracts-5.6.1/`. Both paths are build inputs.

**R4 — Source keys are build inputs; normalize them.** FACT: the same content
compiled under the keys `Contract.sol`, `src/Contract.sol` and `./Contract.sol`
produced three different bytecodes (`78dba0bc…`, `945c23f7…`, `b7b797ec…`, all 510
hex chars). Rule: one canonical relative key per contract, no `./`, no absolute
path, no OS-specific separator.

**R5 — The vendor root name is a build input.** FACT: identical sources compiled
with remapping targets `ozpkg/package/` and `vendor/oz-5.6.1/` produced different
bytecodes (`31fa7a06…` vs `f5a06d17…`, both 1176 hex chars), because metadata
records the *resolved* source path. Rule: the vendor path is frozen by this
document, and renaming it is a profile change.

**R6 — Supply sources by content, not by path.** `sources.<key>.content` is the only
form used. FACT: a `urls` entry is normalized to a content-addressed
`bzz-raw://…` reference and so happens not to leak the filesystem — but content has
no such dependency at all, which removes the failure mode instead of relying on
current behavior.

**R7 — No absolute paths anywhere.** FACT: with `--base-path` set, an *absolute*
remapping target was re-rooted under the base path and the import failed
(`Source "…/Ownable.sol" not found: … Searched … "/tmp"`). Relative targets resolve
correctly. Rule: source names, remappings and include paths are relative, and the
sandbox `chdir`s into the workspace.

**R8 — Close the dependency set inside the pinned tarball.** Imports resolve only
from `vendor/`; a path outside the closure is a hard failure, and the closure digest
is recorded.

**R9 — Empty `libraries`, no network.** No external linked library, no import
callback that fetches, no network egress during the build.

**R10 — Double-build before publishing.** Build twice in two separate sandbox
instances from the same input and require identical `abi`, `bytecode`,
`deployedBytecode` and `metadata` digests; only then does the manifest carry
`reproducibility.verified: true`. A single build is `verified: false` — honest, not
"probably fine".

**R11 — Digests are identity, not metadata.** Two artifacts with different digests
are two artifacts even if the source "looks the same". Nothing is overwritten; a
rebuild that differs is an incident, not an update.

**R12 — Metadata is the contract.** Same IR digest + same profile + same module
closure + same compiler ⇒ same metadata ⇒ same bytecode. FACT: solc metadata
contains **no timestamp**, so wall-clock time is not a source of drift and no
`SOURCE_DATE_EPOCH` workaround is needed.

### Measured evidence for the rules (2026-11-25, solc 0.8.39, `/tmp` only)

| Experiment | Result |
|---|---|
| Same Standard JSON, two different working directories | bytecode and metadata **identical** |
| Source key `Contract.sol` / `src/Contract.sol` / `./Contract.sol` | three different bytecode hashes (R4) |
| Remapping target `ozpkg/package/` vs `vendor/oz-5.6.1/` | different bytecode hashes (R5) |
| `sources` given by `content` vs by `urls` | metadata identical; `urls` normalized to `bzz-raw://…` (R6) |
| `metadata.bytecodeHash: "none"` | IPFS hash dropped from the trailer, solc version retained |
| `appendCBOR: false` with `bytecodeHash: "ipfs"` | rejected with `JSONError` |
| Metadata inspected for a timestamp | none present (R12) |


## Artifact manifest shape (acceptance criterion 2)

One manifest per contract per build, stored next to the artifacts. It is the only
thing downstream stages are allowed to trust: the signing stage signs digests from
it, the lineage record references its `build.id`, and the marketplace lists its
`contract.ref`.

```json
{
  "apiVersion": "vibesdk-artifact/v1",
  "kind": "ContractArtifact",
  "contract": {
    "ref": "vibe/escrow-vault@0.1.0",
    "irDigest": "sha256:7f0c…",
    "templateRef": "vibe/tpl-escrow-vault@0.1.0"
  },
  "build": {
    "id": "sha256:2b91…",
    "profile": "evm-default/v1",
    "sourceDigest": "sha256:6a3e…",
    "closureDigest": "sha256:c4d1…"
  },
  "compiler": {
    "name": "solc",
    "version": "0.8.39",
    "longVersion": "0.8.39+commit.ce47f2c8",
    "distribution": "native-binary",
    "platform": "linux-amd64",
    "sha256": "0xb8b0642d46838575d3177628b685564b52f51f2aee546cb4dce781951d23c666",
    "keccak256": "0x1160f6dbe1b6bc5eb2f4d3e5a9f1ad36b439c37f9421058d88ac2c6136f5095d"
  },
  "settings": {
    "evmVersion": "cancun",
    "optimizer": { "enabled": true, "runs": 200 },
    "bytecodeHash": "ipfs",
    "appendCBOR": true,
    "libraries": {},
    "remappings": ["@openzeppelin/contracts/=vendor/@openzeppelin/contracts-5.6.1/"]
  },
  "modules": {
    "package": "@openzeppelin/contracts",
    "version": "5.6.1",
    "tarballSha256": "sha256:bd202ef9a2fd09f6005edc18437b4d3090841d0f676c01c814ee9608581d2e63",
    "entryPoints": ["access/Ownable.sol", "utils/ReentrancyGuard.sol"]
  },
  "outputs": {
    "abi": { "path": "abi/EscrowVault.json", "digest": "sha256:9c11…", "bytes": 1420 },
    "bytecode": { "path": "bin/EscrowVault.bin", "digest": "sha256:5d7b…", "bytes": 5482 },
    "deployedBytecode": { "path": "bin/EscrowVault.runtime.bin", "digest": "sha256:a0f2…", "bytes": 4106 },
    "metadata": { "path": "metadata/EscrowVault.json", "digest": "sha256:31fa…", "ipfs": "ipfs://Qm…" },
    "storageLayout": { "path": "storage/EscrowVault.json", "digest": "sha256:7e90…" }
  },
  "reproducibility": { "verified": true, "runs": 2, "independentInstances": true },
  "sandbox": { "imageDigest": "sha256:aa01…", "networkAccess": "none", "keysPresent": false }
}
```

Field rules:

| Field | Rule |
|---|---|
| `apiVersion` / `kind` | Fixed envelope; a breaking shape change is a new `apiVersion` with a migration note (same rule as System IR and Contract IR) |
| `contract.ref` | `publisher/name@semver`, matching the IR `metadata`; never resolved through `latest` |
| `contract.irDigest` | The IR provenance digest the source was generated from — the link that makes "regenerate from IR" checkable |
| `build.id` | `sha256` over the canonical Standard JSON input (ordered sources with content digests, the settings block, and the compiler identity). Deterministic, so the same inputs always produce the same id |
| `build.sourceDigest` | `sha256` over the canonical generated source tree (sorted paths plus content digests) |
| `build.closureDigest` | `sha256` over the resolved import closure (sorted resolved paths plus file digests) — the pin's transitive half |
| `compiler.*` | Copied verbatim from the verified binary; `platform` is recorded because the digest is per platform |
| `settings.*` | The exact Standard JSON settings; `libraries` must be `{}` in v1 |
| `modules.entryPoints` | The subset of the W1-WEB3-01 allowlist the contract actually imports (entry points, not the closure) |
| `outputs.*.digest` | `sha256` over the **raw bytes** (`abi`/`metadata`: the canonical JSON bytes; `bin`: the decoded bytecode, not its hex text) |
| `outputs.metadata.ipfs` | The value the CBOR trailer commits to; present only when `bytecodeHash: ipfs` |
| `reproducibility.verified` | `true` **only** after R10's double build; a single build records `false` |
| `sandbox.keysPresent` | Must be `false`; a `true` value is a security incident, not a warning |

Deliberately absent: **timestamps, build hosts, user ids, and run numbers**. Those
are volatile, and a manifest that changes on every rebuild cannot be compared. Who
built what and when belongs in the lineage/audit record that references `build.id`
— the same split the repo already uses for generation lineage.

## Sandbox boundary (acceptance criterion 3)

The build stage is an untrusted workload (`adr-web3-execution-sandbox.md`). This
section fixes what crosses the boundary; it does not choose its implementation
(container versus isolated worker stays OPEN).

**Inputs (all digest-verified before the run):**

1. The Contract IR document (or its digest plus a store reference).
2. Generated source files, supplied as content with canonical workspace-relative
   keys (R3/R4).
3. The pinned library tarball, extracted at the frozen `vendor/` path.
4. The build profile (`evm-default/v1`) and the module-set pin.
5. The compiler binary, verified against the digest in the pin before execution.

**Outputs (nothing else leaves):**

1. `abi/`, `bin/`, `metadata/`, and `storage/` outputs.
2. The artifact manifest.
3. A build log with no secrets (paths, digests, compiler diagnostics).

**Denied — by construction, not by policy review:**

| Denial | Reason |
|---|---|
| Network egress (DNS, HTTP, RPC, package registries) | Dependencies come from the pinned tarball; a fetch at build time is an unreviewed input |
| Any signing key, seed, mnemonic, or API token | Invariants 1–2 of `web3-security-signing.md`: nothing unsigned touches a key |
| Deployment, broadcasting, or address assignment | Those are distinct stages with distinct owners (invariant 3) |
| Writing to the control-plane database or Redis | The sandbox returns artifacts; the control plane persists lineage |
| Filesystem access outside the workspace | Absolute paths are refused anyway (R7) |

**Checks the runner performs and records:**

- compiler digest matches the pin → otherwise refuse to start;
- tarball digest matches the module pin → otherwise refuse to start;
- every resolved import stays inside `vendor/` (R8) → otherwise fail the build;
- `settings.libraries` is empty (R9);
- the two independent builds agree (R10) → otherwise publish `verified: false` and
  raise an incident instead of shipping;
- `keysPresent: false` is asserted from the sandbox spec (no secret binding exists
  to mount), not inferred from behavior.

**Boundary summary:** the sandbox converts *IR + pinned dependencies + pinned
compiler* into *artifacts + a manifest*, and it cannot do anything else — no keys,
no network, no deploy, no history rewrite.

## Relationship to the rest of the Web3 track

| Task | Consumes from here | Adds |
|---|---|---|
| `W2-WEB3-01` security gates | the manifest (artifact digests, closure digest), the settings rules, the double-build evidence | static analysis, simulation, policy evaluation, out-of-scope-kind refusal, NatSpec checks, scanner integration |
| `W3-WEB3-01` wallet/signing/deploy | `outputs.bytecode.digest` and the manifest identity | signing authority, deployment profiles, on-chain verification |
| `W4-WEB3-01` interop | nothing (no OApp generation in v1) | LayerZero adapter implementation |
| `W1-WEB3-01` scope | — | supplies the module pin this manifest copies |
| `W0-WEB3-03` Contract IR v1 | the `provenance` block it fills in | — |

## What this task explicitly does not do

No compiler is vendored, downloaded into the repo, or added as a dependency; no
sandbox image is built; no artifact store, table, or route is created; no framework
is chosen (`W3-TOOL` stays PROPOSED); no key, wallet, or RPC client appears; and no
runtime path in `backend/`, `worker/`, or `src/` changes. `go.mod`, `package.json`
and `bun.lock` are untouched by design.

## Acceptance criteria mapping

| Criterion (backlog) | Where it is answered |
|---|---|
| Reproducibility rules recorded | "Compiler pin", "Settings profile", and rules R1–R12 with the measured evidence table |
| Artifact manifest shape defined | "Artifact manifest shape": full example plus a field-rule table including the deliberate omissions |
| Sandbox boundary referenced, no keys in sandbox | "Sandbox boundary": inputs, outputs, denials, runner checks, and `sandbox.keysPresent: false` |

Verification for this task is documentation-only: `bun run docs:check` must stay
green (six validators), and no code, dependency, image, route, or table may appear.

## Open items

- **wasm vs native byte-identity.** Whether npm `solc` (wasm) emits byte-identical
  output to the native binary is unproven. Until measured, one contract version has
  exactly one distribution channel, recorded in the manifest.
- **Closure digest format.** Whether `build.closureDigest` is a single sorted digest
  (as assumed here) or a per-file list is a detail `W2-WEB3-01` can refine without
  changing the envelope.
- **Artifact store.** Where manifests and blobs live (D1 lineage tables, R2, or a
  content-addressed store) is an O27-family decision; the manifest shape is designed
  to be storage-agnostic.
- **`--evm-version` beyond the IR enum.** If a chain profile needs `osaka`, the IR
  enum must be widened first (a D22 amendment), not worked around in the build.
- **Solc upgrade policy.** When 0.8.4x becomes the pin, the rule is a new profile
  version plus a re-run of the double build; how often that happens is a maintenance
  decision.
- **Build reproducibility across OS images.** R5/R4 remove the known path inputs; a
  cross-platform double build (linux plus macOS) is a later hardening step.

