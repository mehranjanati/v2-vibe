# Composition Lock — Deterministic Dependency Closure (PROPOSED contract)

> **Status:** `proposed` (2026-11-23, ADI-FOUND-002) — normative contract drafted, awaiting owner acceptance. Design only: no code, no store, no route in this change.
> **Decision record:** `adr-adaptive-intelligence-boundaries.md` (the decision to have a lock). **Normative base:** `system-ir.md` (one IR, exactly one execution shape, desired state only), `manifest-schemas.md` (exact `kind/name@MAJOR.MINOR.PATCH` pins, canonical serialization), `../ecosystem/05-agent-platform-architecture.md` (identity/lifecycle/retirement), `../ecosystem/06-runtime-and-execution-architecture.md` (provider contract C1–C18).
> **Evidence behind the rules:** `ir-parity-audit.md` — the audit that made Go and the docs gate agree on what a valid System is. A lock is only meaningful over a single validity answer, which is why ADI-FOUND-002 depended on ADI-FOUND-001a.

## 1. Purpose

The System Manifest states **what a System is allowed to be**. The Composition Lock states **what it actually resolved to**: the exact dependency closure and the content digest of every asset in that closure. It is derived, immutable and content-addressed. It is not a second IR, not a second version counter, and never a place where runtime state lives.

Why it must exist: without it, "which exact assets ran?" is answered by looking at today's registries — which change. With it, a run can be explained a year later from its own snapshot, and dependency upgrades become explicit events instead of silent drift.

## 2. Definitions (normative)

| Term | Definition |
|---|---|
| **Composition** | The System manifest plus every transitively referenced Agent, Skill, Tool, Knowledge, Model-Policy and Eval asset, at exact versions. Desired state only. |
| **Resolution** | The deterministic process that maps every reference in a manifest to exactly one asset and its digest. |
| **Composition Lock** | The immutable artifact recording that resolution: `systemRef`, `systemDigest`, the full `resolvedClosure`, per-asset digests, and the compiler that produced it. |
| **Snapshot** | A lock bound to one run (or to one deployment instance). Snapshots are what a run points at. |
| **Compiler** | The resolver implementation. Identified by `compilerVersion` so two builds can be compared. |
| **Lock format version** | `lockVersion` — the version of *this artifact's shape*. It is **not** the System IR version and **not** the manifest version. |

## 3. Relationship to the System IR (D7 preserved)

1. The only input is a `kind: System` document that passes `backend/pkg/ir.Load` with **zero** findings and validates against the P7.1 schema. A lock over an invalid IR is a defect, not a warning.
2. The lock **never** edits the manifest, never adds fields to it, never becomes an alternative authoring surface.
3. Version identity stays where D15 put it: `metadata.version` on the manifest. `lockVersion` versions *the lock format*, nothing else.
4. `spec.metadata`-style runtime state stays forbidden: no status, health, run id, timings, costs in the lock. Those belong to the data layer (D15, invariant I2).
5. Capability references keep the schema's reserved namespaces — `agent`, `skill`, `tool`, `model-policy`, `knowledge`, `eval` — and stay exact (`@MAJOR.MINOR.PATCH`, no ranges, no `latest`, no git refs).

## 4. Lock artifact — shape (normative)

| Field | Type | Required | Rule |
|---|---|---|---|
| `lockVersion` | string (semver) | yes | Format version of this artifact. `1.0.0` is the only accepted value until a migration is documented. |
| `systemRef` | string | yes | `system/<name>@<version>` of the composed System; must equal the manifest's `metadata.name`/`version`. |
| `systemDigest` | string | yes | `sha256:<hex>` over the System's canonical bytes (§5). |
| `compilerVersion` | string | yes | Semver of the resolver build, e.g. `vibesdk-compiler/0.1.0`. |
| `resolutionMode` | enum | yes | `registry` \| `bundled` \| `offline-cache` (§6). Recorded because it qualifies how much the closure can be trusted to be complete. |
| `resolvedClosure` | array | yes | One entry per resolved reference, sorted by `ref` (§7). Empty is legal only for a System that references nothing. |
| `resolvedClosure[].ref` | string | yes | The exact reference as written in the manifest (`kind/name@version`). |
| `resolvedClosure[].kind` | enum | yes | `agent` \| `skill` \| `tool` \| `knowledge` \| `model-policy` \| `eval`. |
| `resolvedClosure[].publisher` | string | yes | Copied from the asset's `metadata.publisher` — provenance is preserved, never inferred. |
| `resolvedClosure[].digest` | string | yes | `sha256:<hex>` over that asset's canonical bytes. |
| `resolvedClosure[].requestedBy` | array | yes | The references that pulled this asset in (`system` plus any parent refs). Makes the closure explainable, not just complete. |
| `modelPolicyRefs` | array | yes | Subset of `resolvedClosure` with `kind: model-policy`, in order of first use. Recorded separately because model policy is the one closure member with a cost consequence. |
| `evaluationRefs` | array | yes | Subset with `kind: eval`. Recorded separately because evaluation gates read them (see `evaluation-and-evolution.md`). |
| `generatedAt` | string (RFC 3339) | yes | Wall-clock time of resolution. **Excluded from digest computation** (§5) so identical closures produce identical digests. |

**Forbidden in a lock** (structural, checked by the resolver): every secret and credential (`vault:` references are recorded as references only, never resolved values), any tenant or user identifier, any authorization grant, any token, and any provider key. A lock is safe to store, diff, log and share inside a tenant.

### 4.1 Example (derived from the M1 fixture)

```json
{
  "compilerVersion": "vibesdk-compiler/0.1.0",
  "evaluationRefs": [],
  "generatedAt": "2026-11-23T10:15:00Z",
  "lockVersion": "1.0.0",
  "modelPolicyRefs": [],
  "resolutionMode": "bundled",
  "resolvedClosure": [
    {
      "digest": "sha256:<hex-of-followup-drafter-0.9.0>",
      "kind": "agent",
      "publisher": "vibesdk",
      "ref": "agent/followup-drafter@0.9.0",
      "requestedBy": ["system/m1-qualify-then-draft@1.0.0"]
    },
    {
      "digest": "sha256:<hex-of-lead-qualifier-1.2.0>",
      "kind": "agent",
      "publisher": "vibesdk",
      "ref": "agent/lead-qualifier@1.2.0",
      "requestedBy": ["system/m1-qualify-then-draft@1.0.0"]
    }
  ],
  "systemDigest": "sha256:<hex-of-canonical-m1-team>",
  "systemRef": "system/m1-qualify-then-draft@1.0.0"
}
```

Two details in the example are deliberate: keys are sorted at every level, and the placeholders are visibly placeholders — this document does **not** ship computed digests for fixtures that no resolver has processed yet.

## 5. Canonical bytes and digests

**Canonical form** is the one the repository already enforces (`manifest-schemas.md` §canonical serialization, implemented by `backend/pkg/ir.Canonicalize` and `scripts/lib/schema-subset.mjs`):

1. keys sorted recursively;
2. 2-space indent;
3. exactly one trailing newline;
4. HTML escaping disabled (the IR carries conditions such as `score >= 80`);
5. exactly one JSON document — trailing content is a defect;
6. no duplicate object keys.

**Digest:** `sha256:<lowercase-hex>` over the canonical bytes of the asset. The algorithm is *proposed* as SHA-256/hex and remains open as **O26** — but the shape (`<algorithm>:<hex>`) is normative precisely so a future algorithm change is a visible prefix change, never a silent reinterpretation.

**What the digest does and does not cover:**

- It covers the asset's canonical bytes as published. It does **not** cover registry metadata (download counts, review status) — trust is not content.
- `generatedAt` is excluded from the lock's own digest: the lock digest is computed over the lock with `generatedAt` removed, so re-resolving an unchanged closure produces a byte-identical digest.
- A digest mismatch between a stored lock and a recomputed closure is always a **hard failure** (§8), never a warning.

## 6. Resolution modes

| Mode | What it means | When allowed |
|---|---|---|
| `registry` | Every reference resolved against the authoritative registry at exact version. | Production, once a registry exists (ADI-ECO-002). |
| `bundled` | References resolved against an explicit local asset set shipped with the manifest (the way `scripts/validate-manifests.mjs` resolves against bundled examples). | Fixtures, local development, and the first single-agent proof. |
| `offline-cache` | Resolved against a mirrored/cached registry snapshot whose freshness is recorded. | Edge/self-hosted operation with a documented cache age. |

The mode is recorded because a lock produced in `bundled` mode proves *shape*, not *availability*. Anything that claims marketplace readiness on a `bundled` lock is overclaiming.

## 7. Resolution algorithm (the contract ADI-FOUND-003 implements)

1. **Admit the manifest.** Parse and run `backend/pkg/ir.Load`; any finding stops resolution (fail closed, no partial lock).
2. **Collect direct references.** In a deterministic order: `spec.agents` refs (keys sorted), then workflow node `capability` refs (document order), then `spec.evaluation.dataset`, then any reference carried by those assets as they are fetched. Topology order is deliberately *not* used — run order must not change dependency resolution. The implementable form of this step list is [`composition-resolver.md`](composition-resolver.md) §4.
3. **Fetch each reference at exactly the pinned version.** A missing asset is an error in every mode; there is no "nearest version" fallback.
4. **Canonicalize and digest** each fetched asset (§5).
5. **Walk transitively.** An asset's own references (Skill → Tool → Skill) become new closure members. Record `requestedBy` for each.
6. **Detect cycles.** Capability-graph cycles are rejected with the path (`a -> b -> a`), mirroring `findCycle` in `backend/pkg/ir`. Workflow cycles never reach this step — I5 already refused them.
7. **Detect conflicts** (§8).
8. **Emit** the lock: sorted `resolvedClosure`, sorted key order everywhere, `generatedAt` last (still alphabetically sorted in the file).

**Determinism rule:** given (manifest bytes, asset bytes, `compilerVersion`, `resolutionMode`) the output must be byte-identical. Anything that would break that — map iteration order, wall-clock time inside a digest, locale-dependent sorting, a registry that changes between two fetches in the same run — is a defect in the resolver.

## 8. Failure taxonomy (every one is fail-closed)

| Code | Condition | Behaviour |
|---|---|---|
| `E_MISSING` | A referenced asset does not exist at the pinned version. | Refuse; no lock. |
| `E_GRAMMAR` | The reference is not an exact `kind/name@MAJOR.MINOR.PATCH` in a reserved namespace. | Refuse before any fetch. |
| `E_DIGEST` | Stored lock digest ≠ recomputed digest. | Refuse at admission (before execution). |
| `E_CONFLICT_DIGEST` | One `ref` resolves to two different digests in the same resolution (registry changed mid-walk, or two asset sets disagree). | Refuse; report both digests. |
| `E_CYCLE` | Capability graph contains a cycle. | Refuse with the path. |
| `E_DUPLICATE` | The same ref appears twice with different `requestedBy` order-insensitive content. | Collapse to one entry; if content differs, `E_CONFLICT_DIGEST`. |
| `E_IR` | The manifest is not a valid System (any I1–I6 finding). | Refuse before any fetch; every later statement would be meaningless. |
| `E_SOURCE` | The source reported a transient/unavailable condition. | Refuse as infrastructure failure — distinct from `E_MISSING` so a retry layer can tell them apart. |
| `E_REF_MISMATCH` | The source returned an asset whose `ref` differs from the one requested. | Refuse; silent redirection is never allowed. |
| `E_ASSET_UNSIGNED` | The asset carries no `metadata.publisher`. | Refuse; provenance is copied, never invented. |
| `E_ASSET_INVALID` | An asset's bytes are not canonicalizable JSON, so no digest can represent it. | Refuse; an unrepresentable digest must never enter a lock. |
| `E_SECRET_IN_ASSET` | A credential-shaped field carries a literal instead of a `vault:` reference. | Refuse; the lock is shared and must stay secret-free. |
| `E_INCOMPATIBLE_VERSION` | One `kind/name` appears at two different versions in one closure. | Refuse (v1: one version per identity). Multi-version closures need an ADR. |
| `E_UNAUTHORIZED_ASSET` | The asset exists but the tenant is not allowed to pin it (retired, unpublished, or outside the tenant's catalog). | Refuse; the lock is not the place to discover permissions. |
| `E_MODE` | A mode is used that cannot honestly resolve the closure (e.g. `bundled` for a manifest with registry-only refs). | Refuse. |

A resolver that repairs any of these silently is wrong by construction: the lock's value is what it refuses.

## 9. Runtime snapshot semantics

> Authoritative run-binding specification: [`run-snapshot.md`](run-snapshot.md) (ADI-FOUND-004). The six invariants below are normative and appear there as §2 with the storage record, lifecycle, admission sequence, retention and migration rules.

1. **A run points at one snapshot, forever.** The execution record stores the snapshot digest; the run row never stores a mutable "current composition".
2. **Publishing a new asset version never changes an existing snapshot.** It produces a *different* composition the next time it is resolved — a new lock, not an edit.
3. **Active runs keep running on their snapshot.** They finish on what they started with (the retirement rule in `05-agent-platform-architecture.md`: finishing is allowed, starting is not).
4. **Rollback re-points.** Rolling back means pointing new runs at an earlier snapshot; it never rewrites history.
5. **Completed runs stay attributable.** Answers to "which exact assets produced this outcome?" come from the snapshot, not from today's registry.
6. **Snapshot retention follows run retention.** Deleting a tenant's runs may remove their snapshots; the retirement tombstones that keep *pins* resolvable are a separate, longer-lived record.

## 10. Verification at admission

Execution admission (the trusted gate, before the provider is called) recomputes and compares digests against the snapshot. A mismatch is `E_DIGEST`: the run is refused, not degraded. This is the only place the lock is *enforced*; everywhere else it is an artifact that is read and diffed.

## 11. Security boundaries

- **No secrets, credentials, grants or tenant identifiers** in a lock (§4). Credentials travel as `vault:` references resolved at call time, never as values.
- **Provenance ≠ trust ≠ authorization.** `publisher` is preserved per asset; the Private/Community/Verified level is recorded elsewhere and never substitutes for a tenant grant (`09-marketplace-architecture.md`, D18).
- **A lock cannot grant.** It records what a System references, not what a tenant may execute.
- **Signing** (binding a publisher to a key) is W3.1 work behind its own ADR; until then a lock is integrity-checked, not authorship-verified. This distinction must be stated in any UI that shows a "verified" badge.

## 12. Compatibility and migration

| Change | Version action | Migration |
|---|---|---|
| Adding an optional field to the lock | `lockVersion` MINOR | None; old locks stay readable. |
| Changing a field's meaning, or making a field required | `lockVersion` MAJOR | A documented transformer; the resolver accepts both versions during the window and records which it produced. |
| Changing the digest algorithm | New `<algorithm>:` prefix, `lockVersion` MAJOR | Dual-read window; re-resolution produces the new prefix. |
| Changing the System IR envelope | `apiVersion` bump (separate, ADR-backed) | Lock follows the IR; `lockVersion` does not absorb IR changes. |
| New manifest kind | Not in v1 | Would need an IR ADR first (D7). |

`compilerVersion` exists so a behaviour change in the resolver is visible even when the manifest and assets are unchanged: two locks that differ only in `compilerVersion` mean "the compiler changed", which is exactly what a post-mortem needs to know.

## 13. Bundle note (Intelligence Distribution Bundle)

A Bundle groups one System plus its pinned assets for distribution (install/publish unit). It is packaging metadata around the same manifests and the same lock: no new manifest kind in v1, no second identity or version scheme, no execution authority, no plaintext credentials or grants. Bundle work belongs to ADI-ECO-001, after the resolver exists.

## 14. One naming detail worth stating

`systemRef` uses the `system/<name>@<version>` form. That prefix is **not** a resolvable capability namespace — the manifest ref grammar (`$defs/ref`) deliberately admits only `agent`, `skill`, `tool`, `model-policy`, `knowledge`, `eval`. `systemRef` identifies the composed root; it is never fetched as an asset and never appears inside `resolvedClosure`. Keeping the two grammars apart prevents a System from referencing another System as if it were a capability, which would silently reintroduce composition-recursion (and the cycle question) at a level v1 does not model.

## 15. Implementation status (honest, as of 2026-11-23)

| Piece | Status | Evidence / owner |
|---|---|---|
| Manifest validity precondition (schema + I1–I6, cycles, reachability) | IMPLEMENTED | `backend/pkg/ir` + `scripts/validate-system-ir.mjs` (ADI-FOUND-001a) |
| Canonical serialization + duplicate-key/trailing rejection | IMPLEMENTED | `backend/pkg/ir.Canonicalize`, `scripts/lib/schema-subset.mjs` |
| Deterministic cycle path reporting | IMPLEMENTED (inside IR validation) | `findCycle` in both readers |
| Lock artifact shape | DESIGNED | this document |
| Resolver | DESIGNED | ADI-FOUND-003 |
| Digest computation over closure | DESIGNED | ADI-FOUND-003, algorithm open as O26 |
| Snapshot storage + run binding | DESIGNED | ADI-FOUND-004 |
| Admission-time digest verification | DESIGNED | depends on ADI-FOUND-004 + the execution provider contract |
| Registry / availability of assets | NOT IMPLEMENTED | ADI-ECO-002; today only bundled example assets exist |
| Signing / publisher binding | PLANNED (W3.1) | needs its own ADR |

Nothing in this row set is claimed as working because the contract exists. The contract is what ADI-FOUND-003 will be tested against.

## 16. Acceptance tests for the resolver (ADI-FOUND-003)

**Positive:**

1. The four IR fixtures resolve in `bundled` mode to a lock whose `resolvedClosure` contains every referenced asset exactly once, sorted.
2. `systemDigest` equals `sha256` of the fixture's canonical bytes (the same bytes `Canonicalize` round-trips).
3. Re-running resolution on unchanged inputs produces a byte-identical lock apart from `generatedAt`, and an identical lock digest.
4. The M1 fixture's closure contains both agent refs with distinct digests and `requestedBy` pointing at the system.
5. A transitive case (Skill → Tool) lists both, with the tool's `requestedBy` naming the skill.

**Negative (each must fail with its own code from §8):**

| # | Case | Expected |
|---|---|---|
| 1 | Manifest with an invalid IR (any I1–I6 finding) | refused before resolution |
| 2 | Reference to a version that does not exist | `E_MISSING` |
| 3 | Reference with `@latest` or a leading-zero version | `E_GRAMMAR` |
| 4 | Same ref, two different asset bytes | `E_CONFLICT_DIGEST` |
| 5 | Capability cycle `skill/a → tool/b → skill/a` | `E_CYCLE` with the path |
| 6 | Asset whose content changed after the lock was written | `E_DIGEST` at admission |
| 7 | A credential value placed in an asset | refused at digest/manifest stage; `vault:` reference required |
| 8 | `bundled` mode asked to resolve a registry-only ref | `E_MODE` |
| 9 | A retired/unpublished asset pinned by a new System | `E_UNAUTHORIZED_ASSET` |

Numbers 1, 3, 5 and 6 must be asserted on the **code**, not merely on failure — the same rule the IR gates already follow.

## 16a. Where these tests live

The negative list above is implemented as the resolver's own test suite; the concrete cases (P1–P6, N1–N12, D1–D3, G1) are specified in [`composition-resolver.md`](composition-resolver.md) §7, which adds the codes this table grew in ADI-FOUND-003 (`E_IR`, `E_SOURCE`, `E_REF_MISMATCH`, `E_ASSET_UNSIGNED`, `E_SECRET_IN_ASSET`, `E_INCOMPATIBLE_VERSION`).

## 17. Open decisions

| ID | Question | Why it is open |
|---|---|---|
| O26 | Digest algorithm pin (SHA-256 proposed) | Shape is normative; the algorithm choice stays open until a second implementation must interoperate. |
| O26a | Whether the lock digest covers `compilerVersion` | Argument for: two compilers producing different closures should not compare equal. Argument against: the closure already shows the difference. Needs a decision, not a default. |
| O27 | Registry storage shape and tenant key (D1 vs object store) | ADI-ECO-002; a lock can exist before a registry, but `registry` mode cannot. |
| O29 | Capability-cycle policy (hard reject vs classify-and-allow) | v1 rejects workflow cycles outright; capability cycles may be legitimately recursive (`skill → tool → skill` for pagination). Classification needs a real use case. |
| — | Deprecation/retirement notice windows | Marketplace policy (`09-marketplace-architecture.md`), not a lock concern. |
| — | Promotion gates per environment | P5 Cloud operations. |
