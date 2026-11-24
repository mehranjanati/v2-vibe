# Composition Resolver — Design (PROPOSED, ADI-FOUND-003)

> **Status:** `proposed` (2026-11-23) — design only. No package, no code, no route in this change.
> **Contract it implements:** `composition-lock.md` (§7 algorithm, §8 failure taxonomy, §16 tests). **Precondition it depends on:** `system-ir.md` + `ir-parity-audit.md` — one validity answer for a System (`backend/pkg/ir.Load`, invariants I1–I6 including cycles and reachability).
> **Decision record:** `adr-adaptive-intelligence-boundaries.md`. **Scope boundary:** the resolver resolves and digests; it does not fetch over the network, authorize, store, or execute.

## 1. What the resolver is

A pure function from *(validated System bytes, asset source, resolution mode, compiler version, clock)* to *(lock, findings)*. Everything it needs from the outside world is behind one interface. That is the whole design: if the resolver reaches the network or reads a map in nondeterministic order, it stops being reproducible.

```text
Resolve(Request) -> (Lock, []Finding)

Request{
  Manifest        []byte        // kind: System, must pass ir.Load with zero findings
  Mode            ResolutionMode // registry | bundled | offline-cache
  Source          AssetSource   // the only I/O seam
  CompilerVersion string        // e.g. vibesdk-compiler/0.1.0
  Now             time.Time     // injected; never time.Now() inside the resolver
}
```

`Findings` is the whole error surface. A non-empty `Findings` means **no usable lock**: the resolver never returns a partial lock with warnings, because a partially resolved composition is exactly the thing that fails mysteriously at run time.

## 2. Registry input contract (`AssetSource`)

This is the acceptance criterion "registry input contract exists". A source is anything that can do exact-version lookup; a registry, an object store, a bundled directory and a cache are all adapters over the same six obligations.

```go
type Asset struct {
    Ref       string   // the exact reference requested, echoed back
    Kind      string   // agent | skill | tool | knowledge | model-policy | eval
    Publisher string   // the asset's metadata.publisher; never synthesized
    Bytes     []byte   // stored bytes; the resolver canonicalizes them itself
    Refs      []string // references this asset makes (its own closure edges)
}

type AssetSource interface {
    Fetch(ctx context.Context, ref string) (Asset, error)
}
```

**Obligations a source must honour (normative):**

1. **Exact versions only.** A source must resolve `kind/name@X.Y.Z` and nothing else. It must not implement ranges, `latest`, channel names or "nearest version" fallback. If a caller passes a non-exact reference, that is a resolver bug (`E_GRAMMAR`), not a source feature.
2. **Immutable content per reference.** Two `Fetch` calls for the same reference inside one resolution must return identical bytes. A source that changes under the resolver produces `E_CONFLICT_DIGEST` (§5), and that is a property to *detect*, never to tolerate.
3. **Publisher is reported, not inferred.** An asset without `metadata.publisher` is a source defect: `E_REF_MISMATCH`/`E_ASSET_UNSIGNED`. The resolver copies provenance; it never guesses.
4. **The returned `Ref` must equal the requested `Ref`.** A source that silently redirects is refused (`E_REF_MISMATCH`).
5. **Errors are classified, not free-form.** A source may only report *not found* or *unavailable (transient)*. Everything else is a bug. This is what lets the resolver distinguish `E_MISSING` (a real resolution failure) from `E_SOURCE` (infrastructure, retryable at a higher layer).
6. **No secrets in asset bytes.** Credentials appear only as `vault:` references (`openapiimport` and the manifest schemas already force this). The resolver re-checks rather than trusting the producer, because the lock is shared.

**What a source is explicitly not responsible for:** canonicalization, digests, cycle detection, version-conflict detection, authorization and tenancy. Those are the resolver's or the gate's job — putting them in the source would make each adapter a place where correctness can drift.

## 2.1 Which manifest fields are dependency edges (normative)

The code cannot guess this, and two exclusions matter enough to be contractual:

| Kind | Edges |
|---|---|
| `agent` | `spec.tools[]`, `spec.skills[]`, `spec.knowledge[]`, `spec.model.ref`, `spec.model.policy.fallback`, `spec.evaluation.dataset` |
| `skill` | `spec.requires.capabilities[]` |
| `tool` | none — a tool reaches the world through `mcp.Gate` and `vault:` references, neither of which is a closure member |
| `model-policy`, `knowledge`, `eval` | terminal: reserved catalogs have no manifest schema in v1, so the resolver records them and never walks into them |

**`skill.spec.compatibility.agents` is NOT an edge.** It is a compatibility statement ("this skill supports these agents"). Treating it as a dependency would invent the cycle `agent → skill → agent` that the repository's own example assets would otherwise exhibit. This is the single most consequential exclusion in the design, and the implementation pins it with a real-fixture test.

## 2.2 Secret scan (normative denylist)

Field names that must never carry a literal credential: `apiKey`, `api_key`, `token`, `password`, `clientSecret`, `client_secret`, `secretKey`, `secret`, `authorization`, `bearer` (case-insensitive). A value is acceptable only when it is a `vault:`/`vault/` reference or a container whose every string leaf is one. `secrets` (plural) is deliberately **not** on the list: the Tool schema uses it for vault declarations, which is exactly what the resolver wants to record.

## 3. Modes and their adapters

| Mode | Adapter | Today's availability | What a lock in this mode proves |
|---|---|---|---|
| `bundled` | local example asset set (the same set `scripts/validate-manifests.mjs` resolves against: `docs/architecture/manifest-schemas/examples`) | **Available now** | Closure *shape* and determinism — not availability. |
| `registry` | registry adapter (ADI-ECO-002) | Not implemented | Availability at exact versions, subject to registry integrity. |
| `offline-cache` | cache adapter with recorded age | Not implemented | Availability as of the cache timestamp, with staleness on the face of the lock. |

**Honest limitation of `bundled`:** the bundled set covers `agent`, `skill` and `tool` only. A manifest that references `model-policy`, `knowledge` or `eval` cannot be fully resolved in `bundled` mode and must fail with `E_MISSING` — it must not invent a placeholder. That is why the M0/M1 fixtures (agent refs only) are resolvable today and a richer System is not.

## 4. Algorithm (normative, step by step)

```text
Resolve(req):
  1. ADMIT     sys, findings := ir.Load(req.Manifest)
               findings non-empty            -> E_IR (return; nothing is fetched)
               ir.Workflow cycles/reachability are already refused here
  2. MODE      req.Mode not in {registry,bundled,offline-cache} -> E_MODE
               adapter cannot honestly serve the mode -> E_MODE
  3. COLLECT   direct refs, in this fixed order:
                 a. spec.agents keys sorted alphabetically -> agent refs
                 b. spec.workflow.nodes in document order -> capability refs
                 c. spec.evaluation.dataset
               (topology order is deliberately NOT used: run order must not
                change dependency resolution)
  4. WALK      worklist = sorted set of direct refs
               while worklist is not empty:
                 ref := smallest element by canonical string
                 if ref already visited -> merge requestedBy(parent) and continue
                 if not exact ref grammar -> E_GRAMMAR (no fetch)
                 asset, err := source.Fetch(ctx, ref)
                   not found          -> E_MISSING
                   unavailable        -> E_SOURCE
                 asset.Ref != ref     -> E_REF_MISMATCH
                 publisher empty      -> E_ASSET_UNSIGNED
                 secret-shaped value  -> E_SECRET_IN_ASSET
                 canonical := ir.Canonicalize(asset.Bytes)   // normalizes
                 digest    := sha256(canonical)
                 same ref, other digest already recorded -> E_CONFLICT_DIGEST
                 same kind+name, other version recorded  -> E_INCOMPATIBLE_VERSION
                 record entry{ref,kind,publisher,digest,requestedBy}
                 enqueue every child ref (sorted) with this ref as parent
  5. GRAPH     build parent->child edges; run findCycle (same colouring as
               backend/pkg/ir and scripts/validate-system-ir.mjs)
                 cycle -> E_CYCLE with the path a -> b -> a
  6. EMIT      closure sorted by ref; requestedBy sorted; modelPolicyRefs and
               evaluationRefs derived from the closure (not re-fetched)
               generatedAt = req.Now (excluded from the lock digest)
  7. REPORT    findings sorted by (precedence, ref) for a stable message order
```

### 4.1 Why the worklist is sorted

Resolution order must not depend on map iteration or on which parent happened to be visited first. A sorted worklist makes the *fetch order* reproducible, which matters twice: the same input produces the same `requestedBy` accumulation, and two sources that disagree produce the same `E_CONFLICT_DIGEST` report.

### 4.2 Diamond dependencies are legal, cycles are not

`A → B`, `A → C`, `B → D`, `C → D` is a diamond, not a cycle: `D` resolves once with `requestedBy: [B, C]`. The colouring in step 5 only reports an edge back to a **grey** (in-progress) node, which is exactly what a cycle is. A shared dependency is not.

### 4.3 Exact versions and the incompatible-version rule

Exactness is enforced at `E_GRAMMAR` (no `^`, `~`, `*`, `latest`, ranges, or git refs). The separate, more interesting case is an **incompatible** closure: two parents pinning different versions of the same identity —

```text
skill/a@1.0.0 -> tool/http-call@1.2.0
skill/b@1.0.0 -> tool/http-call@2.0.0
```

**v1 rule: `E_INCOMPATIBLE_VERSION`.** The same `kind/name` may appear at exactly one version in one closure. Reasons: identity is `kind/name` (D15), so two versions in one composition would make "which definition ran?" ambiguous at the level the lock exists to answer; and silently picking one version is a semantic decision the resolver is not allowed to make. Revisit trigger: a real workload where two skills legitimately need different major versions of one tool — that needs its own ADR (multi-version closure is a runtime-namespacing change, not a resolver flag).

## 5. Determinism: the obligations and the traps

**Obligations** — for fixed *(manifest, asset bytes, mode, compilerVersion)* the lock must be byte-identical:

1. closure sorted by ref; `requestedBy` and both derived arrays sorted;
2. worklist order fixed by sorting, never by map iteration;
3. digest over canonical bytes only — no timestamps, no paths, no tenant ids;
4. `generatedAt` injected through `Now`, excluded from the lock digest;
5. findings sorted deterministically;
6. `compilerVersion` recorded so a behaviour change is visible without a diff.

**Traps that break determinism** (each is a defect, not a preference):

- reading asset bytes straight into the digest without canonicalizing (whitespace or key order becomes semantic);
- `time.Now()` inside the resolver;
- walking a Go map for closure or `requestedBy` output;
- allowing the source to return assets in a different order between runs;
- sorting with a locale-sensitive comparator;
- including registry metadata (freshness, download counts) in the digest — freshness belongs in `resolutionMode`, not in content.

## 6. Failure precedence (stable reporting)

When more than one finding exists, the primary code is the first in this order, so two runs of the same broken input report the same thing first:

```text
E_IR > E_MODE > E_GRAMMAR > E_REF_MISMATCH > E_ASSET_UNSIGNED > E_ASSET_INVALID
     > E_SECRET_IN_ASSET > E_MISSING > E_SOURCE > E_CONFLICT_DIGEST
     > E_INCOMPATIBLE_VERSION > E_CYCLE
```

`E_ASSET_INVALID` was added during implementation: an asset whose bytes are not canonicalizable JSON cannot be digested, and pretending it can would put an unrepresentable digest in a lock.

`E_IR` wins because a manifest that is not a valid System makes every later statement meaningless. `E_CYCLE` is last because it can only be known after the closure is built — order here is *reporting* precedence, not discovery order.

## 7. Test specification — as implemented

`backend/pkg/composition/resolve_test.go` runs 24 cases against the repository's real fixtures and example assets. Two premises changed when the code met the data, and the changes are recorded rather than hidden:

- The **real bundled example set is not self-closing**: `lead-qualifier.agent.json` references `knowledge/icp-docs@5.0.0`, `model-policy/standard@1.0.0`, `model-policy/cheap@1.0.0` and `eval/lead-set@2.0.0`, none of which ship. So "the fixtures resolve" is false for the richer fixtures, and the honest positive case is the one with no references at all.
- A cleaner statement of the same thing: `bundled` mode proves closure *shape*, not availability.

### Positive

| # | Test | What it proves |
|---|---|---|
| P1 | `TestP1EmptyClosureFromRealBundledSet` | `visual-workflow.system.json` resolves over the real example set to an **empty** closure; `systemDigest` equals sha256 of the fixture's canonical bytes |
| P2 | `TestP2M1TransitiveClosure` | M1 over the real manifests **plus catalog stubs** yields a 9-entry sorted closure: 3 agents, the skill and tool reached transitively, and the three catalog kinds; `tool/score-lead` is attributed to **both** the agent and the skill; `modelPolicyRefs`/`evaluationRefs` are derived, not re-fetched |
| P3 | `TestP3RealBundledSetIsNotSelfClosing` | `m0-single-agent.system.json` over the real set fails `E_MISSING` instead of inventing placeholders |
| P4 | `TestP4DiamondResolvesOnce` | a diamond resolves the shared leaf once with both parents in `requestedBy` |
| P5 | `TestP5TransitiveAttribution` | a transitive-only asset is attributed to the asset that pulled it in, and the root is attributed to the system |
| P6 | `TestP6ReResolutionIsIdentical` | two resolutions with the injected clock are byte-identical and share a lock digest |

**The negative in P2's data set is the point:** the M1 fixture contains a skill whose `compatibility.agents` names the agent that references it. Had that field been treated as an edge, P2 would fail with `E_CYCLE`. It resolves — that is the exclusion being proven by real bytes, not by a comment.

### Negative

| # | Test | Code |
|---|---|---|
| N1 | invalid manifest (agents without topology) + counting source | `E_IR` with **zero** source calls |
| N2 | asset whose child reference does not exist | `E_MISSING` |
| N3 | `@latest`, `^1.0.0`, `1.x`, `01.0.0`, uppercase, trailing space — and an asset declaring one | `E_GRAMMAR` |
| N4 | identity exists only at another version | `E_MISSING` |
| N5 | source returns a different ref | `E_REF_MISMATCH` |
| N6 | asset without publisher | `E_ASSET_UNSIGNED` |
| N7 | literal `apiKey` refused / `vault:` accepted (scanner asserted directly, plus through a manifest) | `E_SECRET_IN_ASSET` |
| N8 | source content drifts between resolutions | lock digest changes — see below |
| N9 | one identity at `1.2.0` and `2.0.0` | `E_INCOMPATIBLE_VERSION` |
| N10 | cycle `agent → skill → agent` | `E_CYCLE` carrying a `a -> b -> a` path |
| N11 | unknown mode | `E_MODE` |
| N12 | source reports unavailability | `E_SOURCE`, and **not** `E_MISSING` |

**N8 is honest about its own reachability.** With a sorted worklist and single-visit semantics the same reference is never fetched twice in one resolution, so `E_CONFLICT_DIGEST` is a **defensive** code: it exists for adapters that cache per call and for the future verification pass. What the test pins is the observable guarantee — changed bytes produce a different lock digest, so content can never be silently reused.

### Determinism

| # | Test | Assertion |
|---|---|---|
| D1 | `TestD1HundredResolutionsAreIdentical` | 100 resolutions produce byte-identical canonical locks |
| D2 | `TestD2DeclarationOrderDoesNotLeak` | reordering the *object keys* of `spec.agents` does not change the lock |
| D3 | `TestD3ConcurrentResolutionsAgree` | 16 concurrent resolutions agree (also run under `-race`) |
| G1 | `TestG1GoldenLock` | the canonical M1 lock matches `testdata/m1-team.lock.json` byte for byte (`-update` rewrites it deliberately) |

D2 has a deliberate limit, learned while writing it: **topology order is content.** It is a JSON array, so reordering it changes the canonical bytes and therefore `systemDigest` — correctly. Only the order of *object keys* is a non-semantic variation, and that is exactly what the test varies.

## 8. What the resolver deliberately does not do

| Not its job | Owner |
|---|---|
| Authorize a tenant to use an asset | `mcp.Gate` + tenant grants (D18); the lock records references, never permission |
| Decide whether an asset is *trusted* | marketplace trust levels + provenance (W3.1 for signing) |
| Store the lock | ADI-FOUND-004 (snapshot persistence) |
| Verify the lock at execution time | admission in the execution path (§10 of `composition-lock.md`) |
| Fetch over a network | the `AssetSource` adapter it is given |
| Compile a RunPlan from the closure | P1-AGT-002 / runtime adapter work |

## 9. Implementation — DONE (ADI-FOUND-003a, 2026-11-23)

| File | Contents |
|---|---|
| `backend/pkg/composition/doc.go` | package contract: pure function, findings mean no lock, what it is not |
| `backend/pkg/composition/ref.go` | `Ref`/`ParseRef` over the IR's own semver grammar (so resolver and IR cannot disagree), `SystemRef` for the `system/` root prefix |
| `backend/pkg/composition/source.go` | `Asset`/`AssetSource`/`BundledSource` (+ `Assets()` for composing sets), `ParseAsset` with the edge table of §2.1, `ScanForSecret` with the denylist of §2.2, `ErrAssetNotFound`/`ErrSourceUnavailable`/`ErrNotAnAsset` |
| `backend/pkg/composition/lock.go` | lock structs, `Canonical()` through `ir.Canonicalize`, `Digest()` excluding `generatedAt`, `DigestBytes` (`sha256:<hex>`) |
| `backend/pkg/composition/resolve.go` | the eight-step algorithm, sorted worklist, diamond-vs-cycle colouring, failure precedence, derived subsets |
| `backend/pkg/composition/resolve_test.go` | the 24 cases of §7 |
| `backend/pkg/composition/testdata/m1-team.lock.json` | golden lock (9 entries, real digests) |

**Evidence:** `go test ./pkg/composition/` — 24/24 pass, including `-race`; `go vet ./...` clean; `go test ./...` across the backend green.

**Scope actually delivered:** `bundled` mode only. No registry adapter, no cache adapter, no D1, no route, no agent caller — so D8 is untouched. Digest is `sha256` over canonical bytes (O26 remains open as a *pin*, the prefix makes any change visible).

**Honest limits:** the real example assets are not self-closing (P3), and `E_CONFLICT_DIGEST` is unreachable by construction today (N8).

## 10. Open items

| Item | Owner |
|---|---|
| Multi-version closures (the `E_INCOMPATIBLE_VERSION` revisit) | ADR first; needs a real two-major-version workload |
| Digest algorithm pin (O26) and whether the lock digest covers `compilerVersion` (O26a) | `composition-lock.md` §17 |
| Registry adapter + storage shape (O27) | ADI-ECO-002 |
| Freshness semantics for `offline-cache` (max age, staleness on the lock) | ADI-ECO-002 |
| Whether `requestedBy` should carry the manifest version or only the system ref | resolver review; today: system ref with version, since a lock belongs to one version |
