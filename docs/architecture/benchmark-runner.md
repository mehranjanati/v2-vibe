# Composition Benchmark Runner — Design (PROPOSED, ADI-EVAL-002)

> **Status:** `proposed` (2026-11-23) — design only. No binary, dataset, store or code in this change.
> **Contract it executes:** [`evaluation-contract.md`](evaluation-contract.md) (ADI-EVAL-001) — layers, verdicts, datasets, gates. **Subject:** a stored snapshot ([`run-snapshot.md`](run-snapshot.md)). **Not responsible for:** promotion or rollout (ADI-EVAL-003/004).

## 1. What the runner is

The runner turns a **gate definition** into a **result artifact**: it loads a subject, produces evidence for the layers it can, and reports `pass` / `fail` / `not_evaluated` / `blocked` per layer. It is a batch job on the control plane, not a service and not a request path — evaluation is billable, slow work that must never sit behind a user's request.

```text
EvaluationRequest → [verify subject] → [per-layer evidence] → EvaluationResult (canonical JSON)
```

If a layer cannot produce evidence, the runner **says so** and the verdict is `not_evaluated`. That path is the normal one today, not an exception.

## 2. What the runner needs that a snapshot does not carry

This is the design's central constraint, and it comes from the snapshot contract rather than from preference.

A stored snapshot contains `systemDigest` (a digest of the System bytes) and `resolvedClosure[]` with a **ref and digest per asset**. It does **not** contain the System document, and it does **not** contain asset bytes.

| Layer needs | Snapshot has | Therefore |
|---|---|---|
| The System document (L1 must run `ir.Load`) | only `systemDigest` | The request must supply the document; the runner verifies its canonical digest equals `systemDigest` |
| Asset bytes (L5 secret scan, L3/L4 execution) | `ref` + `digest` per entry | The request must supply an `AssetSource`; the runner verifies each fetched asset's digest against the snapshot |
| The closure itself | full `canonicalLock` | L2 can verify without any external source |

Two consequences, both deliberate:

1. **The snapshot stays lean.** Carrying manifests and asset bytes would make it a second registry and would bloat the row the lock contract already sized at ~264 bytes per entry.
2. **Re-evaluation is explicit about its inputs.** A runner that cannot reach an asset reports `E_EVAL_ASSET` for that layer instead of quietly evaluating less.

This is also why L2 differs from resolution: see §4.

## 3. Request

| Field | Rule |
|---|---|
| `snapshotId` | Required. The runner loads it and runs `composition.Verify` before anything else; a broken digest chain is `E_EVAL_SUBJECT`, not a partially evaluated run |
| `systemDocument` | Required for L1; its canonical digest must equal the snapshot's `systemDigest` |
| `assetSource` | Required for any layer that needs bytes; must satisfy the existing `composition.AssetSource` contract (exact versions, immutable content, reported `ref`) |
| `layers[]` | The layers this gate requires. A layer not listed is not evaluated and cannot silently pass |
| `datasets[]` | Pinned `eval/name@X.Y.Z` refs per dataset-driven layer |
| `thresholds` | Per metric; a metric with no threshold is reported, not judged |
| `budget` | `maxCases`, `maxWallClock`, `maxCost`, plus an injected absolute `deadline` — the deadline is the deterministic form of `maxWallClock`, because the clock is injected and a wall-clock read would make the result unreproducible |
| `judge` | Optional; must be declared (provider, model, version, temperature/seed). Never accepted for L5 |
| `seed`, `now` | Injected, so two runs are comparable |
| `evaluatorVersion` | Recorded; a behaviour change in the runner is then visible without a diff |

## 4. Per-layer execution (what actually runs today)

| Layer | What the runner does | Evidence today |
|---|---|---|
| **L1 Structural** | `ir.Load(systemDocument)` plus the canonical digest comparison | Fully implementable with existing code |
| **L2 Capability** | **Verifies** the stored closure: every `ref` parses as an exact pin, every `digest` has the right prefix, no identity appears at two versions, no cycle in the closure graph | Fully implementable with existing code |
| **L3 Behavioral** | Assembles a context per case, activates, executes, asserts (§5) | `not_evaluated` — no dataset (O20) and no run path accepts a snapshot yet |
| **L4 Operational** | Runs the C1-C18 conformance suite against the provider the request names | `Fake` works (18/18); a live provider is `not_evaluated`/`provider-absent` |
| **L5 Safety** | Derives what the manifests allow: irreversible tool without an approval class, a literal credential, a policy denial that would not bind | Partly — needs asset bytes from `assetSource`; end-to-end denial tests need a gate harness with grants |

**Why L2 verifies instead of re-resolving.** Re-resolution needs a registry, and there is none (`composition-resolver.md` §3: only bundled assets exist). Verifying the stored closure is not a weaker substitute dressed up as a stronger one: it is a different, still useful check — it detects tampering, floating refs, incompatible versions and cycles in the artifact a run will actually point at. Re-resolution becomes possible when a registry lands, and then it is a *second* check, not a replacement.

## 5. The case-execution contract (for L3, when it can run)

```text
for each case in dataset (bounded by budget):
    context  = assemble(task=case.input, snapshot, providers)      # context-assembly.md
    decision = activate(context, activationPolicy)                  # activation-policy.md
    outcome  = execute(decision, provider)                          # C1-C18 contract
    verdict  = assert(case.expectations, outcome)
```

| Rule | Why |
|---|---|
| A case that cannot be assembled (missing provider, scope refusal) is `skipped`/`error`, reported per case — never counted as pass | A dataset result must distinguish "failed" from "could not run" |
| Assertions are structured, not prose | "Looks good" is not a verdict; a judge may produce it only for L3 quality metrics, never for correctness assertions |
| Per-case cost and latency are recorded | L4 evidence falls out of L3 execution for free |
| The activation decision is recorded per case | A behavioral failure caused by a denial is a different finding from a behavioral failure caused by the model |

## 6. Result artifact

One canonical JSON document (sorted keys, 2-space indent, one trailing newline) with the reproducibility record of `evaluation-contract.md` §7, plus:

| Field | Contents |
|---|---|
| `verdicts[]` | Per layer: verdict, metrics with values and thresholds, and the reason when not `pass` |
| `cases[]` | Per case: status (`pass`/`fail`/`error`/`skipped`), cost, latency, and the activation decision id when one exists |
| `artifactDigest` | `sha256:` over the canonical document minus the clock — so two runs are comparable as digests |
| `budget` | What was allowed and what was spent |
| `evaluatorVersion`, `judge` | As requested, echoed |

## 7. Budget and exit codes

| Exit | Meaning | Operational response |
|---|---|---|
| `0` | Every required layer `pass` | Promote is *eligible* — not performed |
| `1` | At least one required layer `fail` | Fix the composition |
| `2` | `not_evaluated` / `blocked` on a required layer | Supply evidence (dataset, provider, budget) |
| `3` | Contract violation (`E_EVAL_*`) | Fix the request; a broken request is not a failed benchmark |

Separating `1` from `2` and `3` is deliberate: CI must be able to tell "the software got worse" from "we did not measure" from "the request was wrong". A single non-zero exit would collapse three different conversations into one.

## 8. Determinism

1. Fixed `seed` and injected `now`; the artifact digest excludes the clock.
2. Case order is the dataset order; dataset digests are recorded.
3. A judge's variance is recorded rather than averaged away: if a judge is used, the artifact says so and the model/version/seed are part of the record.
4. Two runs of the same request against the same inputs produce the same `artifactDigest` **unless** a provider is nondeterministic — in which case the difference is explainable from the recorded `judge`/provider identity, not mysterious.

## 9. Failure modes

| Code | Condition | Behaviour |
|---|---|---|
| `E_EVAL_SUBJECT` | Snapshot missing, unverifiable, or `systemDocument` digest mismatch | Refuse the whole run (exit 3) |
| `E_EVAL_ASSET` | `assetSource` cannot supply a pinned asset, or its digest differs | That layer is `not_evaluated` with the ref named |
| `E_EVAL_DATASET` | Dataset missing, floating, or digest mismatch | That layer is `not_evaluated` |
| `E_EVAL_JUDGE` | Judge undeclared, or requested for L5 | Refuse the whole run (exit 3) |
| `E_EVAL_BUDGET` | `maxCases`/`maxWallClock`/`maxCost` exhausted | Remaining cases `skipped`, layer `not_evaluated`/`budget` (exit 2) |
| `E_EVAL_INCOMPLETE` | A required layer produced no evidence | Gate `blocked` (exit 2) |

## 10. What this design does not do

- No dataset creation, no case mining, no synthetic data.
- No promotion, no rollout, no rollback (ADI-EVAL-003/004).
- No HTTP route, no queue, no new service: a batch entry point on the control plane.
- No model calls except a declared L3 judge.
- No storage decision for results: the artifact is a file today and may become a table later (§12).

## 11. Tests this design will need (ADI-EVAL-002a, when code starts)

1. A request without `systemDocument` refuses L1 as `blocked` (not `pass`).
2. A `systemDocument` whose canonical digest differs from `systemDigest` is `E_EVAL_SUBJECT` (exit 3).
3. A tampered closure digest yields L2 `fail` with the ref named.
4. Two versions of one identity in the closure yield L2 `fail` (incompatible versions).
5. A closure cycle yields L2 `fail` with the path.
6. L3 on a request with no `datasets` yields `not_evaluated`, exit 2 — never exit 0.
7. `maxCases: 3` over a 10-case dataset leaves 7 `skipped`/`budget` and the layer `not_evaluated`.
8. A judge requested for L5 is refused with `E_EVAL_JUDGE`.
9. The same request twice produces the same `artifactDigest` (clock excluded).
10. An `assetSource` returning different bytes than the snapshot recorded yields `E_EVAL_ASSET` for that layer.

## 11a. Implementation status — ADI-EVAL-002a (2026-11-23)

| Piece | Evidence |
|---|---|
| `backend/pkg/evaluation` | `Layer`/`Verdict`/`Metric`/`LayerResult`, `Request`/`Result`, `Evaluate`, `ExitCode`, `Canonical`, `Digest` — no route, no queue, no store writes, no model call |
| L1 | Re-runs `ir.Load` on the supplied System document; the snapshot's `systemDigest` is compared **as a subject check** before any layer runs |
| L2 | Verifies the stored closure: exact pins, one version per identity, cycle detection over `requestedBy` edges |
| L3/L4/L5 | `not_evaluated` with a precise reason (`no dataset`, `no provider selected`, `no asset source`), and a floating dataset ref blocks L3 instead of being ignored |
| Tests | 13 cases (`go test ./pkg/evaluation/`), all green: pass path, foreign document, missing snapshot, floating ref, unprefixed digest, incompatible versions, cycle, no-dataset, judge-on-L5, digest determinism, expired deadline, missing document, unpinned dataset |

**Two refinements found while implementing.** (1) The document/snapshot digest match belongs to the *subject* phase, not to L1 — it refuses the run (exit 3) instead of reporting a layer failure, because a document that is not the snapshot's makes every later judgement meaningless. (2) The closure digest-prefix check inside L2 is **defence in depth**: `composition.Verify` already rejects an unprefixed digest, so that path is unreachable through a stored snapshot and is exercised directly against `evaluateCapability`.

## 12. Open items

| Item | Why it is open |
|---|---|
| Where results live (file artifact vs D1 table) | The artifact must exist first; storage is the same O27-adjacent decision as snapshots |
| Whether a snapshot should ever carry the System document | **Recommendation: no** — keep the lock lean and make evaluation state its inputs. Revisit only if audit requires byte-level reproduction without an external source |
| Which provider L4 runs against by default | The `Fake` proves the contract; a live run is evidence about Cloudflare, and it is billable |
| Dataset acquisition for L3 | O20; the runner can be built before a dataset exists, and will honestly report `not_evaluated` |
| Judge selection and its own evaluation | A judge needs evidence before it can produce evidence (mirrors the open item in `evaluation-contract.md`) |
