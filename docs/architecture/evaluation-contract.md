# Evaluation Contract — Normative (PROPOSED, ADI-EVAL-001)

> **Status:** `proposed` (2026-11-23) — design only. No runner, dataset, store or code in this change.
> **Subject:** a **composition snapshot** ([`run-snapshot.md`](run-snapshot.md)), never a manifest and never a single run. **Consumers:** the candidate lifecycle and promotion gates ([`evaluation-and-evolution.md`](evaluation-and-evolution.md) §3-§5, ADI-EVAL-003/004) and the benchmark runner design (ADI-EVAL-002).
> **Distinct from:** call-time enforcement (`mcp.Gate`), which refuses an individual action — evaluation judges a whole composition against evidence, after the fact.

## 1. What is being evaluated

The subject is a **snapshot**. That single choice settles several downstream questions:

- Evaluation results attach to a `snapshotId`, so an asset update produces a **different** subject and the old result does not carry over. "We tested it last month" is not a claim about today's closure.
- Two runs of the same snapshot share a subject; two snapshots of the same System do not.
- Evaluating a manifest (rather than a snapshot) would test an intention; users experience a closure.

## 2. The five layers (normative)

Each layer has a producer of evidence, a verdict, and a **stated** state of automation. The last column is the honest part: two layers are automated today, one is partly automated, and two have no evidence pipeline at all.

| Layer | What it judges | Evidence producer | Automated today |
|---|---|---|---|
| **L1 Structural** | The composition is a valid System and every manifest validates | `backend/pkg/ir` (I1-I6, 4 fixtures + 13 negative probes in parity with the docs gate) | **Yes** |
| **L2 Capability** | The closure resolves: exact pins, digests, no incompatible versions, no cycles | `backend/pkg/composition` (24 tests, golden 9-entry lock) | **Yes** (bundled mode only) |
| **L3 Behavioral** | The composition achieves the intended outcome against a dataset, with citations where claimed | Dataset runner (ADI-EVAL-002) + judge | **No** — no dataset exists (O20 open) |
| **L4 Operational** | Latency, cost, retry/cancel/pause behave per the provider contract C1-C18 | `backend/pkg/execution` conformance suite | **Partly** — 18/18 on the reference `Fake`; the Cloudflare adapter is not verified live |
| **L5 Safety** | Policy denials hold, tenant isolation holds, no secret leaks, irreversible effects require approval plus idempotency | `mcp.Gate` tests, isolation matrix, the resolver's secret denylist | **Partly** — strong unit-level evidence, no end-to-end safety suite |

**Layer definitions are closed.** Adding a sixth layer is a contract change with an ADR, not a local improvement: a gate that silently grows a criterion is a gate nobody can plan against.

## 3. Verdicts, and the one rule that matters

```text
pass | fail | not_evaluated | blocked
```

| Verdict | Meaning | Effect on promotion |
|---|---|---|
| `pass` | The layer met every required threshold on the stated dataset | Does not block |
| `fail` | A required threshold was missed | **Blocks** |
| `not_evaluated` | No evidence was produced (no dataset, provider absent, budget exhausted) | **Blocks by default** |
| `blocked` | A prerequisite is missing (no snapshot, invalid dataset ref, undeclared judge) | **Blocks** |

**`not_evaluated` is never `pass`.** This is the rule the whole system leans on: the fastest way for an evaluation platform to become theatre is to treat absence of evidence as success. A required layer that was not evaluated blocks promotion exactly like a failure — and the operator action differs (supply evidence vs. fix the composition), which is why the verdicts are distinct.

A layer may be **waived** only by a named approver, with a reason and an expiry, and the waiver is recorded on the result. Waivers are auditable decisions, not configuration defaults.

## 4. Datasets

| Field | Rule |
|---|---|
| Ref | `eval/name@MAJOR.MINOR.PATCH` — the reserved IR namespace, pinned, never a range |
| Immutability | A published dataset version never changes; corrections publish a new version |
| Digest | Recorded on every result, so "which dataset produced this verdict" is answerable |
| Case shape | `id`, `input`, `expectations[]` (assertions, not prose), optional `rubricRef`, `weight` |
| Provenance | Who produced it, from what, under what licence |
| Sample size | Stated on the result. "100% pass" on three cases is reported **as** three cases |

Datasets are not invented here. Assistant and Research datasets stay open as **O20**; Commerce, Cloud and Network datasets arrive with their product slices. An empty dataset catalog is an honest reason for `not_evaluated`, not an excuse for a synthetic one.

## 5. Metrics

A metric is `name`, `direction` (`higher-better` | `lower-better`), `value`, `threshold`, and the layer it belongs to.

**No composite score.** A single blended number is banned because it lets a failing safety metric be averaged away by a strong cost metric. Each required metric is judged on its own, and the layer passes only when every required metric passes.

| Layer | Example metrics (not the catalog) |
|---|---|
| L1 | invariant violations = 0; schema violations = 0 |
| L2 | unresolved refs = 0; digest mismatches = 0; incompatible versions = 0 |
| L3 | task success rate; grounded-citation rate; judged quality (declared judge) |
| L4 | p95 latency; cost per run; retry rate; cancel-finality violations |
| L5 | policy denials that executed = 0; cross-tenant reads = 0; secret literals stored = 0 |

The concrete catalog per product is deliberately not fixed here: a metric that no dataset can measure is decoration.

## 6. Gates

A gate is `{layers[], datasets[], thresholds, onFail: block}`.

1. **A gate failure blocks promotion.** That is the acceptance criterion and the only reason gates exist.
2. **Every required layer must reach a non-`fail`, non-`not_evaluated` verdict.** A gate that only lists L1 and L2 is a structural gate and says so.
3. **`blocked` propagates:** if L2 cannot run because the snapshot is unverified, the whole gate is `blocked`, not "L1 passed so far".
4. **No silent override.** Waiving a layer is the recorded, expiring, named decision of §3.
5. **Gates are versioned with the policy that declares them,** so "the bar moved" is a diff rather than a mystery.

## 7. Reproducibility record

Every result carries:

| Field | Why |
|---|---|
| `snapshotId`, `systemRef` | What was judged |
| `datasetRef` + dataset digest | Against what |
| `evaluatorVersion` | Which harness produced it |
| `judge` | When an LLM judge is used: provider, model, version, temperature/seed, and the judge's own evaluation status |
| `sampleSize` | So a result cannot be over-read |
| `cost`, `startedAt`/`finishedAt` | Evaluation is billable work and must be budgeted |
| `verdicts[]` per layer with metric values | The evidence itself, not only the conclusion |

The clock is injected, as everywhere else in ADI, so a replay can be compared.

**The judge rule:** an LLM judge may inform L3. It may never be the sole evidence for L1, L2 or L4, and **never** for L5 — a policy denial that executed is a fact, not an opinion, and judging safety with a model that can be talked out of its verdict would convert the strongest layer into the weakest.

## 8. Failure modes

| Code | Condition | Behaviour |
|---|---|---|
| `E_EVAL_SUBJECT` | The subject is not a verified stored snapshot | Refuse; evaluation never runs against an unverified composition |
| `E_EVAL_DATASET` | A dataset ref is missing, floating, or its digest does not match | Refuse the layer |
| `E_EVAL_JUDGE` | An LLM judge is used without declaring it, or for a layer where it is banned | Refuse |
| `E_EVAL_BUDGET` | The evaluation exceeds its cost/time budget | The layer is `not_evaluated` with reason `budget` — never a partial pass |
| `E_EVAL_INCOMPLETE` | A required layer produced no evidence | Gate is `blocked`; promotion refused, with the missing layer named |

## 9. What this contract does not do

- No training, fine-tuning, prompt optimisation or automatic repair.
- No automatic promotion: evidence informs a decision, it does not make one.
- No runner design (ADI-EVAL-002), no candidate lifecycle (ADI-EVAL-003), no rollout (ADI-EVAL-004).
- No dataset, no store, no table, no new dependency.
- No composite score, by explicit prohibition (§5).

## 10. Tests this contract will need (ADI-EVAL-002 and later)

1. A snapshot with an invalid IR yields L1 `fail`, and the gate is `fail` regardless of other layers.
2. An unresolved ref yields L2 `fail`; a cyclic closure yields L2 `fail` with the path.
3. A missing dataset yields L3 `not_evaluated`, and the gate blocks — never `pass`.
4. A gate listing L1-L3 on an unimplemented L3 is `blocked` with the layer named.
5. An undeclared LLM judge is refused with `E_EVAL_JUDGE`; a declared judge on L5 is refused too.
6. A budget-exhausted layer is `not_evaluated`/`budget`, and the result records the cost spent so far.
7. A waived layer records the approver, reason and expiry, and the gate passes only for that window.
8. Two results for the same snapshot, dataset and evaluator version produce identical verdicts (clock excluded).
9. An asset change produces a new snapshot, and the previous result is not reused for it.
10. A result with `sampleSize: 3` reports that number; nothing in the record implies statistical significance.

## 11. Open items

| ID | Question | Why it is open |
|---|---|---|
| O20 | Assistant and Research evaluation datasets | No dataset exists; inventing one in a contract would be fiction |
| — | LLM-judge policy (which models may judge, and their own evaluation bar) | A judge needs its own evidence before it can produce evidence |
| — | Statistical bar (minimum sample size per layer) | Needs a real dataset to reason about variance; a made-up number would be worse than none |
| — | L4 cadence: per-commit vs per-release conformance | Cost decision; the Fake suite is cheap, a live provider run is not |
| — | Whether L5 always requires a human sign-off, or only for irreversible effects | Safety policy with legal weight; not a platform default |
