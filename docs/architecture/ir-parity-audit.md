# System IR Parity Audit (ADI-FOUND-001)

> **Status:** `complete` (2026-11-23) — audit only; no application code changed.
> **Task:** ADI-FOUND-001 (`../tasks/MASTER_BACKLOG.json`), phase P0/P1 of the ADI workstream line.
> **Question answered:** where do the three answers to "is this IR valid?" disagree — the P7.1 JSON Schema, the docs gate (`scripts/validate-system-ir.mjs`), and the Go reader (`backend/pkg/ir`)?
> **Method:** every finding below was produced by executing a probe against the real code, not by reading alone (commands in §7).

## 1. What was audited

| Surface | File | Role |
|---|---|---|
| Schema | `manifest-schemas/system.schema.json` | Normative shape (draft 2020-12) |
| Docs gate | `scripts/validate-system-ir.mjs` | Fixtures + I1–I6 + 6 negative probes |
| Schema subset | `scripts/lib/schema-subset.mjs` | The keyword subset the gate actually enforces |
| Go reader | `backend/pkg/ir/system.go` | Compiler-side `Load` + `Canonicalize` + I1–I6 |
| Go tests | `backend/pkg/ir/system_test.go` | 4 fixtures, canonical round-trip, 6 negative probes |
| Runtime | `worker/workflow/VibeWorkflow.ts`, `backend/pkg/engine/workflowschema.go` | Where DAG validity is actually enforced today |

## 2. Findings (severity · evidence · impact · fix · blocks next milestone?)

### P-A1 — `metadata.name` / `node.id` length is unenforced in Go (Severity: HIGH)

- **Evidence:** schema caps both at 63 (`docs/architecture/manifest-schemas/system.schema.json:43`, `:254`). `Load` checks only the slug pattern (`backend/pkg/ir/system.go:175`) and its error text claims *"at most 63 chars"* while the regex cannot enforce length. Probe `name-70-chars` and `node-id-70-chars`: Go **ACCEPTED**, schema **REJECTED**.
- **Impact:** a manifest the registry/marketplace will reject compiles fine in Go. The compiler becomes the lenient path — exactly the divergence the parity rule forbids. Identity rules in `../ecosystem/05-agent-platform-architecture.md` state ≤63 as normative.
- **Fix:** length check in `Load` for `metadata.name`, agent keys and node ids (one shared helper), plus two negative Go tests.
- **Blocks next milestone?** Yes — ADI-FOUND-002 (Composition Lock) digests depend on a single validity answer.

### P-A2 — Semver accepts leading zeros in Go (Severity: MEDIUM)

- **Evidence:** schema pattern is `(0|[1-9][0-9]*)` (`:53`, `:49` for refs); Go uses `\d+\.\d+\.\d+` (`backend/pkg/ir/system.go:47-49`). Probe `version-leading-zero` (`01.0.0`): Go **ACCEPTED**, schema **REJECTED**.
- **Impact:** two spellings of one version can resolve to different digests; lock determinism (ADI-FOUND-002) would inherit the ambiguity.
- **Fix:** tighten `semverRe`/`agentRefRe`/`anyRefRe` to the schema's grammar; add a rejection test per regex.
- **Blocks next milestone?** Yes (same reason as P-A1).

### P-A3 — `spec.outcome` is required by the schema but optional in Go (Severity: HIGH)

- **Evidence:** both `oneOf` branches require `outcome` (`docs/architecture/manifest-schemas/system.schema.json:75-89`); Go declares `Outcome` with `omitempty` (`backend/pkg/ir/system.go:74`) and never checks it. Probe `missing-outcome`: Go **ACCEPTED**, schema **REJECTED**.
- **Impact:** an IR with no stated outcome is the one thing the outcome-first thesis (`../PRODUCT_THESIS.md`) cannot allow — yet it loads. Evaluation/lineage would have nothing to attribute a run to.
- **Fix:** require non-empty `outcome` in `checkInvariants`; add the negative test.
- **Blocks next milestone?** Yes — activation and evaluation both key off the outcome.

### P-A4 — `agents` without `topology` passes Go (Severity: MEDIUM)

- **Evidence:** schema branch 1 requires `outcome`+`agents`+`topology`, branch 2 requires `outcome`+`workflow`; `oneOf` therefore demands topology with agents (`:75-89`). Go's I1 only compares *presence* of agents vs workflow (`backend/pkg/ir/system.go:227-232`). Probe `agents-without-topology`: Go **ACCEPTED**, schema **REJECTED**.
- **Impact:** an agent set with no declared run order reaches the runtime, which then improvises order — a silent semantic decision outside the contract.
- **Fix:** extend I1: `agents` implies a non-empty `topology`; negative test.
- **Blocks next milestone?** No, but must land before any composition is compiled.

### P-A5 — Approval class (`on`) is an enum in the schema, unvalidated in Go (Severity: MEDIUM)

- **Evidence:** schema restricts `on` to `external-message|external-write|payment|human-review`; Go's I6 checks only the approver shape (`backend/pkg/ir/system.go:304`). Probe `approval-on-invalid-enum`: Go **ACCEPTED**, schema **REJECTED**.
- **Impact:** an approval gate could be declared for a class the policy engine never evaluates — a human-review requirement that silently does not bind. Security-relevant.
- **Fix:** validate `on` against the shared vocabulary in Go (single exported list, mirroring `NodeKinds`).
- **Blocks next milestone?** No, but must land before policy validation in the lock pipeline.

### P-A6 — `evaluation.dataset` required by schema, optional in Go (Severity: LOW)

- **Evidence:** probe `evaluation-without-dataset`: Go **ACCEPTED**, schema **REJECTED** (`missing required property 'dataset'`). `Evaluation.Dataset` carries no `omitempty` but an empty string passes (`backend/pkg/ir/system.go:127`).
- **Impact:** an evaluation block with no dataset looks configured but can score nothing; evaluation gates (`evaluation-and-evolution.md`) would be vacuous.
- **Fix:** require non-empty dataset when `evaluation` is present.
- **Blocks next milestone?** No (evaluation is P2/P3), but it is a one-line invariant — cheapest to fix with P-A1.

### P-A7 — `uniqueItems` on `next` / `delegatesTo` is schema-only (Severity: LOW)

- **Evidence:** schema sets `uniqueItems: true`; the gate enforces it through `scripts/lib/schema-subset.mjs:101`; Go has no set check. Probe `duplicate-next`: Go **ACCEPTED**, schema **REJECTED**.
- **Impact:** duplicate edges inflate fan-out counts and make AND-join semantics depend on the provider, not the contract.
- **Fix:** dedupe check in I3/I5 with a named error.
- **Blocks next milestone?** No.

### P-A8 — Cycles are refused at RUNTIME, never at the IR boundary (Severity: HIGH)

- **Evidence:** probes `workflow-cycle`: Go **ACCEPTED** AND schema **ACCEPTED**. Nothing in `backend/pkg/ir` or `scripts/validate-system-ir.mjs` looks for cycles. Cycle refusal exists only downstream: `worker/workflow/VibeWorkflow.ts:462` ("workflow contains a cycle; edges must form a DAG") and `backend/pkg/engine/workflowschema.go:375` (Kahn's algorithm).
- **Impact:** a cyclic System Manifest validates, locks (once ADI-FOUND-002 lands) and only then is refused at admission. The contract in `system-ir.md` (I5 "workflow graph integrity") is read by implementers as compile-time; the code says otherwise. A lock over a cyclic graph is a deterministic artifact of an unexecutable system.
- **Fix:** add cycle detection to I5 in both implementations (same DFS/Kahn's, same message), plus a 7th negative probe in the gate and a Go negative test. Do **not** move the runtime check — it stays as defence in depth.
- **Blocks next milestone?** Yes — a resolver/lock must refuse cycles before writing a digest.

### P-A9 — Reachability is a non-failing note in one implementation and silent in the other (Severity: MEDIUM)

- **Evidence:** the gate walks from `entry` (defaulting to the first node) and pushes a `note` for unreachable nodes (`scripts/validate-system-ir.mjs:143-156`); `notes` never fail the build. Go's I5 has no reachability walk at all. Probe `unreachable-node`: both **ACCEPTED**.
- **Impact:** dead steps are shipped as part of the composition; evaluation counts them, cost estimates include them, and "what did this system do?" gains nodes that cannot run.
- **Fix:** decide the contract explicitly — either an error (my recommendation: unreachable node = error, since a declared step that can never run is a modelling bug) or a documented warning in both. Then implement identically in Go and JS.
- **Blocks next milestone?** No, but the decision is needed before the lock is declared deterministic.

### P-A10 — `Canonicalize` accepts trailing content (Severity: LOW, honesty gap)

- **Evidence:** `Load` rejects trailing JSON via `dec.More()` (`backend/pkg/ir/system.go:164`) — verified: `trailing-json → problems=1 "trailing content after the System document"`. `Canonicalize` performs a single `Decode` with no `More()` check (`backend/pkg/ir/system.go:326`); probe: `canonicalize-accepts-trailing=true` for all ten documents.
- **Impact:** two byte streams with the same first document canonicalize identically — a digest computed over the canonical form (ADI-FOUND-002) would not distinguish them. Low today, structural for the lock.
- **Fix:** mirror `Load`'s `More()` check in `Canonicalize`; one test.
- **Blocks next milestone?** Yes, in the strict sense: content-addressing must be total.

### P-A11 — Duplicate JSON keys are silently resolved (Severity: LOW, both sides)

- **Evidence:** `{"kind":"NotSystem","kind":"System",...}` loads with `problems=0` (last-wins). `JSON.parse` in the gate behaves the same.
- **Impact:** a hand-edited manifest can carry a shadowed `kind`/`apiVersion`/`ref` that no tool reports. Not a security control today, but it weakens the "canonical bytes" claim.
- **Fix:** require duplicate-key rejection in the canonical parser (Go: `json.Decoder` token walk, or a pre-scan); document it in `manifest-schemas.md` §canonical serialization.
- **Blocks next milestone?** No.

## 3. Parity matrix (executed, both sides)

`Go` = `ir.Load`; `Schema` = the P7.1 schema through `scripts/lib/schema-subset.mjs`.

| Probe | Go | Schema | Verdict |
|---|---|---|---|
| `name-70-chars` | ACCEPTED | REJECTED | **divergent** (P-A1) |
| `node-id-70-chars` | ACCEPTED | REJECTED | **divergent** (P-A1) |
| `version-leading-zero` | ACCEPTED | REJECTED | **divergent** (P-A2) |
| `missing-outcome` | ACCEPTED | REJECTED | **divergent** (P-A3) |
| `agents-without-topology` | ACCEPTED | REJECTED | **divergent** (P-A4) |
| `approval-on-invalid-enum` | ACCEPTED | REJECTED | **divergent** (P-A5) |
| `evaluation-without-dataset` | ACCEPTED | REJECTED | **divergent** (P-A6) |
| `duplicate-next` | ACCEPTED | REJECTED | **divergent** (P-A7) |
| `workflow-cycle` | ACCEPTED | ACCEPTED | **both lenient** (P-A8) |
| `unreachable-node` | ACCEPTED | ACCEPTED (note only) | **both lenient** (P-A9) |
| trailing JSON after document | REJECTED in `Load`, ACCEPTED in `Canonicalize` | n/a | **internal inconsistency** (P-A10) |
| duplicate JSON key | ACCEPTED (last wins) | ACCEPTED | **both lenient** (P-A11) |

Direction of the divergence: **Go is the lenient side in 8 of 10 cases.** The gate is the stricter contract; the reader is the permissive one. That is the dangerous direction, because the reader is what a compiler will call.

## 4. Verdict against this task's acceptance criteria

| Criterion | Verdict | Basis |
|---|---|---|
| Required-field behavior is consistent | **NOT MET** | P-A3, P-A4, P-A6 |
| Name length constraints enforced consistently | **NOT MET** | P-A1 |
| Trailing JSON is rejected | **PARTIAL** | `Load` yes; `Canonicalize` no (P-A10) |
| Workflow cycles and reachability match the normative contract | **NOT MET** | P-A8, P-A9 |
| Canonical serialization is deterministic | **PARTIAL** | deterministic ordering/indent/newline hold and round-trip byte-identically; it is not *total* (P-A10, P-A11) |
| Positive and negative test requirements documented | **MET** | §5 |
| No application code modified | **MET** | probes were transient; `git status` clean under `backend/` |

Consequence: the audit *succeeds as an audit* and *fails as a parity claim*. `system-ir.md` describing the reader as "the same answer" to "is this IR valid?" is currently aspirational. The fix is tracked as **ADI-FOUND-001a**; the lock work (ADI-FOUND-002) must not start before it.

## 5. Test requirements (the contract for ADI-FOUND-001a)

**Positive (must keep loading with zero findings):** the four canonical fixtures; a maximal document exercising `routing`, `deployment`, `evaluation` with a dataset, `approvals` with all four `on` classes, and `labels` — proving the tightened rules do not over-reject.

**Negative, one per finding (each must be rejected *for the stated reason*, asserted on the message, not merely on "something failed"):**

| ID | Probe | Expected reason |
|---|---|---|
| N1 | 64-char `metadata.name` | length, not slug |
| N2 | 64-char node `id` | length, not slug |
| N3 | `version: 01.0.0` | semver grammar (leading zero) |
| N4 | ref `agent/x@01.0.0` | pinned-ref grammar |
| N5 | missing `spec.outcome` | required field |
| N6 | `agents` without `topology` | execution-shape contract |
| N7 | `approvals[].on: not-a-class` | approval vocabulary |
| N8 | `evaluation` without `dataset` | required field |
| N9 | `next: [b, b]` | duplicate edge |
| N10 | cyclic graph `a↔b` | cycle |
| N11 | unreachable node | reachability (per the §P-A9 decision) |
| N12 | trailing document after the IR | trailing content (`Canonicalize` too) |
| N13 | duplicate JSON key `kind` | duplicate key |

**Cross-implementation rule (unchanged, now enforceable):** every negation above must be added to **both** `scripts/validate-system-ir.mjs` (as a 7th+ negative probe) and `backend/pkg/ir/system_test.go` in the same commit, and `bun run docs:check` plus `go test ./pkg/ir/` must both be green. A negative probe that passes is a failure.

## 6. What this audit does not change

No schema edit, no Go edit, no validator edit, no manifest kind, no new IR version, no runtime behaviour. Every finding above is a documentation-level statement about existing code; the fixes are a separate implementation task (ADI-FOUND-001a) that needs its own review. Nothing here weakens or reinterprets D7: there is still exactly one IR, and this audit exists to make its two readers agree.

## 7. Commands executed

```bash
cd backend && go test ./pkg/ir/            # ok backend/pkg/ir (fixtures, round-trip, 6 probes)
cd backend && go vet ./pkg/ir/            # clean
cd backend && go test ./pkg/ir/ -run TestZZParityProbe -v   # transient probe -> §3 (removed after)
node /tmp/adi_probe.mjs                   # schema side of §3 (removed after)
node scripts/validate-system-ir.mjs       # gate green before and after
```

Probe artifacts were deleted; `git status backend/` is clean, so the audit added documentation only.

## 8. Resolution — ADI-FOUND-001a (2026-11-23)

Every divergent row above is closed. Both readers now refuse the same documents, proven by re-running the identical matrix after the change:

| Probe | Go after fix | Schema alone | Docs gate (schema + invariants) |
|---|---|---|---|
| `name-70-chars` | REJECTED | REJECTED | REJECTED |
| `node-id-70-chars` | REJECTED | REJECTED | REJECTED |
| `version-leading-zero` | REJECTED | REJECTED | REJECTED |
| `missing-outcome` | REJECTED | REJECTED | REJECTED |
| `agents-without-topology` | REJECTED | REJECTED | REJECTED |
| `approval-on-invalid-enum` | REJECTED | REJECTED | REJECTED |
| `evaluation-without-dataset` | REJECTED | REJECTED | REJECTED |
| `duplicate-next` | REJECTED | REJECTED | REJECTED |
| `workflow-cycle` | REJECTED | **ACCEPTED** (not expressible) | REJECTED (I5 walk) |
| `unreachable-node` | REJECTED | **ACCEPTED** (not expressible) | REJECTED (I5 walk) |
| trailing document | REJECTED in `Load` **and** `Canonicalize` | n/a | n/a (JSON.parse rejects it) |
| duplicate JSON key | REJECTED in `Load` **and** `Canonicalize` | n/a | **not detectable** (`JSON.parse` is last-wins) |

Two asymmetries remain, both documented rather than hidden:

1. **Graph properties are invariants, not schema.** JSON Schema cannot express acyclicity or reachability, so a schema-only validator will always accept those two. The docs gate is *schema plus invariants*; that pair is the contract, and Go now matches it.
2. **Duplicate keys are undetectable in JavaScript.** `JSON.parse` keeps the last occurrence and offers no hook, so the gate cannot report it. Go rejects it on both paths (compiler side). Mitigation: the gate compares canonical bytes against stored fixtures, so a shadowed key in a committed fixture would fail the round-trip; a probe constructed in memory cannot exhibit the defect.

### What changed in code

| File | Change |
|---|---|
| `backend/pkg/ir/system.go` | 63-char caps on `metadata.name`, agent keys and node ids; schema-grade semver and ref grammar; required `outcome` (plus 1000 cap); agents imply topology; `approvals[].on` vocabulary; pinned `evaluation.dataset` + 0–1 thresholds; routing and deployment enums plus replica bounds; metadata caps; duplicate `delegatesTo`/`next`; cycle detection (same message as the runtime); reachability as an error; `Canonicalize` rejects trailing content and duplicate keys; duplicate-key scanner |
| `backend/pkg/ir/system_test.go` | `TestPositiveControlLoads` + `TestRejectsParityProbes` (N1–N11, asserted on the stated reason) + `TestRejectsTrailingAndDuplicateKeys` (N12/N13) |
| `scripts/validate-system-ir.mjs` | cycle detection (`findCycle`, mirrors Go), duplicate-edge check, reachability promoted from note to error, tightened ref regexes, positive-control validation, 11 new negative probes (17 total) |
| `docs/architecture/system-ir/positive-control.system.json` | New shared over-rejection guard: every optional field populated, read by both implementations |

### Evidence (executed)

```bash
cd backend && go test ./pkg/ir/ -v   # TestPositiveControlLoads, N1-N11 subtests, N12/N13 -> PASS
cd backend && go vet ./pkg/ir/       # clean
node scripts/validate-system-ir.mjs  # 4/4 cases, positive control clean, 17 negative probes rejected
bun run docs:check                   # all five validators green
```

No manifest schema changed, no `apiVersion` bump, no new manifest kind, no runtime behaviour change.
