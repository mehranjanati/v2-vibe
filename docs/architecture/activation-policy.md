# Activation Policy — Normative Contract (ADI-CTX-002; eligibility core implemented by ADI-CTX-003)

> **Status:** `implemented` for the eligibility core and its persisted trace (2026-11-25, ADI-CTX-003 + ADI-CTX-004): `backend/pkg/activation` is the pure `Decide()` this contract specifies — deny-overrides, default-deny, the closure invariant with `E_ACTIVATION_ESCALATION` + audit, budget-before-bound, ranking intersection, and the §5 decision record with `decidedAt` excluded from its digest — and §5's record is persisted append-only by `backend/pkg/activation/d1store` (migration `0021`, one row per run, §11). Still design-only: ranking **providers**, any caller (no assembler, no policy source). Twelve decision tests plus the trace/store tests in `backend/pkg/activation` (incl. `-race`) prove §10 and §11.
> **Consumes:** the context envelope ([`context-assembly.md`](context-assembly.md)) — candidates only ever come from there. **Consumed by:** the activation engine (ADI-CTX-003) and the decision trace (ADI-CTX-004). **Umbrella:** [`context-activation-and-memory.md`](context-activation-and-memory.md) §4-§5.
> **Existing vocabulary reused, not replaced:** `SideEffect` (`safe`/`external`/`irreversible`), `ApprovalClass` (`external-message`/`external-write`/`payment`/`human-review`), `resource:action` permissions and `RequiresHuman` from `backend/pkg/mcp`; the four approval classes from the manifest schemas; grants and audit from `mcp.Gate` (D18). The vocabulary is **mirrored** in `backend/pkg/activation` (parity-tested) so the eligibility core imports no grant store.

## 1. The question this contract answers

Given **candidates** (from the composition snapshot, carried in the context envelope) and a **policy**, which capabilities may this run use — and why not the others?

Activation answers that question. It does **not** answer "may this tool actually execute?" — that is `mcp.Gate`, at call time, per grant. Keeping those two questions apart is what makes the whole design safe: a bug in activation can only make a run *less* capable, never more.

## 2. Two stages, one authority

| Stage | Nature | May it add a capability? | May it decide on its own? |
|---|---|---|---|
| **A. Eligibility** | Deterministic: policy rules, grants, scope, reviewed side-effect class, budget | **No** — it only removes | **Yes** — final for what is *allowed* |
| **B. Ranking** | Probabilistic: relevance ordering supplied by a provider, plus the candidate bound | **No** — it only orders and trims within A | **No** — it cannot revive anything A removed |
| **C. Call-time enforcement** | Deterministic: `mcp.Gate` with tenant-scoped grants | No | **Yes** — final for what *executes* |

**Final authority is A plus C.** Stage B is a convenience for the model, never an authority. This is the acceptance criterion stated as an invariant:

```text
activated ⊆ ranked ⊆ eligible ⊆ candidates(context) ⊆ closure(snapshot)
```

Every link is enforced, and each one has a consequence when it is violated:

| Violated link | Meaning | Response |
|---|---|---|
| `candidates ⊄ closure` | Something tried to activate a capability the composition never declared | `E_ACTIVATION_ESCALATION`, audited — this is a security event, not a bug report |
| `eligible ⊄ candidates` | A rule would allow an undeclared capability | `E_ACTIVATION_POLICY` at policy load: a policy cannot grant what the closure does not contain |
| `ranked ⊄ eligible` | A ranking provider returned an ineligible ref | `E_ACTIVATION_POLICY` (provider contract violation), the ref is ignored and the run continues without it |
| call outside `activated` ∪ granted | The gate is the only place that can catch this | `mcp.Gate` refusal, already implemented and audited |

## 3. Stage A — eligibility rules (deterministic)

Eligibility is a pure function of *(candidate, policy, snapshot, tenant grants)*. Same inputs, same answer, no model, no clock, no randomness.

A rule is one object with a closed shape:

| Field | Rule |
|---|---|
| `id` | Stable identifier, cited in the decision trace. |
| `effect` | `allow` \| `deny`. **Deny wins** whenever both match — there is no ordering, no last-match, no priority tie-break to get wrong. |
| `match` | Closed selector set, §3.1. Every field present must match (AND); lists are OR within themselves. |
| `reason` | Human-readable, copied into the trace verbatim. Required for `deny`. |

### 3.1 Selectors (all reuse existing vocabulary)

| Selector | Values | Notes |
|---|---|---|
| `kind` | `agent` \| `skill` \| `tool` \| `knowledge` \| `model-policy` \| `eval` | From the ref grammar. |
| `ref` | exact pinned ref, or a `kind/name@*` prefix form | Exactness is preserved: a prefix may only name one identity, never a version range. |
| `publisher` | string | Provenance, not trust. |
| `trustLevel` | `private` \| `community` \| `verified` | As recorded by the marketplace layer; **never** a substitute for a grant. |
| `sideEffect` | `safe` \| `external` \| `irreversible` | Reviewed value from the Tool Manifest, as `mcp.ToolPolicy` already carries it. |
| `permissions` | `resource:action` list | Must be a **subset** check against the granted permissions; a rule may narrow, never widen. |
| `approvals` | the four approval classes | A rule may require additional classes. |
| `requiresHuman` | boolean | Mirrors `mcp.ToolPolicy.RequiresHuman`. |
| `scope` | context scope vocabulary | Narrows which scoped candidates are usable. |
| `resolutionMode` | `registry` \| `bundled` \| `offline-cache` | Lets a tenant say "bundled-only snapshots are not production". |

**What a selector may not reference:** run-time state (elapsed cost, step count, model output, the task text). Those belong to budgets and to the gate; letting policy read them would make eligibility non-deterministic and un-reviewable.

### 3.2 Evaluation order

1. Compute the candidate's **effective facts** from the snapshot and the reviewed manifest (side effect, approvals, permissions, trust level).
2. Collect every rule whose `match` applies; if any is `deny`, the candidate is `denied` with that rule's id and reason — full stop.
3. Otherwise, if at least one `allow` matched, the candidate is `eligible`; if **none** matched, it is `skipped` with reason `no-allow` (default-deny: silence is never permission).
4. Apply the budget and the candidate bound; anything beyond them is `skipped` with reason `budget` or `bound`.

Default-deny is deliberate: a new capability kind landing in the platform cannot become usable by an old policy that never mentioned it.

## 4. Stage B — ranking (probabilistic, bounded)

The ranking stage receives only the **eligible** set. It may sort and truncate; it may never introduce a ref, and its output is intersected with the eligible set before use. It records:

- provider id and version (`ranking.source`), so an ordering can be explained later;
- the rank it assigned per candidate;
- ties broken by ref ascending — a total order, so two runs with the same eligibility and the same ranks agree.

If no ranking provider is configured, the deterministic order (ref ascending) is used and the trace records `ranking.source: "none"`. A platform without a ranking provider is less clever, never less correct.

## 5. The decision record

One record per run, one entry per candidate. This is the artifact that makes "why did it not use the CRM tool?" answerable.

```json
{
  "activationPolicyRef": "activation-policy/tenant-default@3.0.0",
  "compositionDigest": "sha256:<snapshot id>",
  "contextId": "ctx:sha256:<hex>",
  "decidedAt": "2026-11-23T10:15:00Z",
  "decisions": [
    {"candidate": "tool/score-lead@1.0.0", "decision": "activated", "ruleId": "allow-safe-writes", "stage": "eligibility"},
    {"candidate": "tool/send-email@2.1.0", "decision": "skipped", "reason": "budget", "stage": "budget"},
    {"candidate": "tool/refund@1.0.0", "decision": "denied", "ruleId": "deny-irreversible", "reason": "tenant forbids irreversible side effects without human approval", "stage": "eligibility"}
  ],
  "ranking": {"source": "provider-v1", "version": "1.2.0"},
  "runId": "run-1",
  "tenantKey": "tenant-a"
}
```

| Field | Rule |
|---|---|
| `runId`, `tenantKey` | The run this decision belongs to; the tenant makes the record safe to store beside runs. |
| `contextId` | Exactly one envelope. A decision that cites no context cannot be reproduced. |
| `compositionDigest` | The snapshot. Together with `contextId` it pins *what could have been chosen*. |
| `activationPolicyRef` | Pinned policy version (`activation-policy/name@X.Y.Z`). A policy edit is a new version, exactly like a manifest. |
| `decisions[]` | Sorted by `candidate`; each carries `stage` (`eligibility` \| `budget` \| `bound`) and, for non-activation, a `reason` or `ruleId`. |
| `ranking` | Provider id plus version, or `"none"`. |
| `decidedAt` | Injected clock; **excluded** from any digest over the record, the same rule as the lock's `generatedAt`. |

**Every decision is attributable.** A `skipped` without a reason, or an `activated` without a `ruleId`, is a contract violation. "It wasn't used" is not an explanation.

## 6. Approvals: activation may require, never satisfy

A rule can add an approval requirement to a candidate (`approvals: [payment]`, `requiresHuman: true`). The decision then carries `approvalRequired: [<class>]`.

What activation **cannot** do: record that an approval was given. Approvals are satisfied through the existing approval path (IR `spec.approvals`, `mcp.ApprovalClass`, the execution gate); activation merely states the precondition. A capability whose approval is pending is activated *with a precondition* — the run can plan around it, and the gate will refuse the call if the precondition is unmet. This keeps one approval mechanism, not two.

## 7. Determinism

1. **Set membership is deterministic:** eligible is a pure function of (context, policy, snapshot, grants). No clock, no model, no randomness.
2. **Deny-overrides** removes ordering ambiguity: no rule can be "more specific but later".
3. **Total order:** activated candidates are emitted sorted by ref; ties in ranking break on ref.
4. **Policy and ranking versions are recorded,** so a replay is explainable even when a provider changes. Claiming bit-identical replay across a changing ranking provider would be dishonest; recording the provider is the honest equivalent.
5. **No silent widening:** any path that would add a candidate outside the closure is a hard failure (`E_ACTIVATION_ESCALATION`), never a warning.

## 8. Failure modes

| Code | Condition | Behaviour |
|---|---|---|
| `E_ACTIVATION_CONTEXT` | The envelope is missing, or its `snapshotId` does not match the run. | Refuse before deciding. |
| `E_ACTIVATION_POLICY` | The policy is unreadable, invalid, or a rule references an undeclared selector. | Refuse; a policy that cannot be reviewed cannot be enforced. |
| `E_ACTIVATION_ESCALATION` | A candidate is not in the snapshot closure. | Refuse **and audit**; treat as a security event. |
| `E_ACTIVATION_BUDGET` | After eligibility the set is empty and the policy requires at least one capability (`minCapabilities > 0`). | Refuse before execution. |
| — (not an error) | The eligible set is empty because the System genuinely has no capabilities. | Proceed; the run has nothing to call, which is a legitimate outcome. |

## 9. What this contract does not do

- It does not execute or authorize execution — `mcp.Gate` does, per call.
- It does not install, upgrade or pin anything; the closure is fixed by the snapshot.
- It does not evaluate model output, cost so far, or deadlines.
- It introduces no second approval mechanism, no second permission model, no second audit sink.
- It does not rank with a model of its own; ranking is an injected provider or nothing.

## 10. Tests this contract will need (ADI-CTX-003a, when code starts)

> **Landed 2026-11-25** as `backend/pkg/activation/activation_test.go` — the twelve functions below are the §10 list, one to one: `TestDenyOverridesAllow`, `TestEscalationOutsideClosureRefusedAndAudited`, `TestPolicyAllowingOutsideClosureFailsToLoad`, `TestDefaultDenyYieldsSkipped`, `TestBoundTrimsToEightWithSkippedBound`, `TestIneligibleRankingRefIgnored`, `TestDeterministicReplayByteIdentical`, `TestApprovalRequiredRecordedNotApproved`, `TestEmptyCapabilitiesEmptyDecisions`, `TestBudgetRefusalOnlyWhenMinCapabilitiesPositive`, plus the negative `TestNoGrantMintingNoManifestMutation` (go/parser import scan: no `backend/pkg/mcp`, no exec/SQL in non-test code) and the vocabulary-parity `TestMirroredVocabularyMatchesMcp`.

> **Trace tests landed 2026-11-25 (ADI-CTX-004):** `TestTraceAcceptsWhatTheEngineEmits`, `TestTraceRefusesUnattributableDecisions` (13 cases), `TestTraceRefusesUnknownIdentity` (7 cases) and `TestTraceDigestIgnoresTheClock` prove §5 and §11's attributability in `backend/pkg/activation/trace_test.go`; `backend/pkg/activation/d1store/d1store_test.go` proves the store T1–T11 (round trip, idempotent replay, immutability, tenant wall, missing row, corruption, refusal record, absent clock, validate-before-write, no database, loud failures).


2. A candidate absent from the closure is refused with `E_ACTIVATION_ESCALATION` and produces an audit event.
3. A policy containing an `allow` for a ref outside the closure fails to load (`E_ACTIVATION_POLICY`).
4. No matching rule yields `skipped` / `no-allow` (default-deny), never `activated`.
5. Nine eligible candidates with a bound of eight yield eight activated plus one `skipped` / `bound`.
6. A ranking provider returning an ineligible ref cannot add it; the run continues with the intersection.
7. Two runs with identical inputs and a fixed clock produce byte-identical decision records (minus `decidedAt`).
8. `requiresHuman` on a candidate records `approvalRequired` without marking it approved.
9. An empty capability System produces an empty decision list and no error.
10. `E_ACTIVATION_BUDGET` fires only when `minCapabilities > 0` and eligibility left nothing.

## 11. Persisting the record (ADI-CTX-004)

The record is stored with the run lineage, not in a second ledger: one row per run in `activation_decision_traces` (migration `0021`), written through `activation.TraceStore` (`backend/pkg/activation/trace.go`) and implemented by `backend/pkg/activation/d1store`.

| Rule | Why |
|---|---|
| `run_id` is the primary key and the insert is `ON CONFLICT DO NOTHING` | One record per run. A replay of the same decision is a no-op, so a retried write cannot duplicate an explanation. |
| The first write wins, including `decidedAt` | The trace says when the decision was first recorded; a later replay does not move it. |
| A different record under an existing run is `ErrTraceImmutable` | A re-decision is a new run, exactly like a correction is a new snapshot. Nothing rewrites history. |
| A run id already recorded by another tenant is `ErrTraceTenant` | The record sits beside runs, so the tenant wall applies to the row. |
| `trace_digest` is `Record.Digest()` (`decidedAt` excluded) and is re-verified on read | A row that no longer matches its own digest answers `ErrTraceCorrupt` instead of explaining something that never happened. |
| `ValidateRecord` runs before the write | An `activated` without a `ruleId`, or a `skipped`/`denied` without a reason, is refused (`ErrTraceInvalid`) before D1 is touched. |

The whole record is stored as one JSON document (`record`), with identity columns denormalised for lookup and retention (`tenant_key`), rather than split into a second per-candidate table: the entry list is one attributable artifact, and a second copy would be a second answer to one question. Reading a trace is `Load(ctx, runId)`; the store has no update and no delete operation.

**Why a dedicated table, not `audit_logs` or a column on `workflow_instances`.** `audit_logs` carries individual security events — activation still writes its `E_ACTIVATION_ESCALATION` events there through the caller — while this table carries the *whole decision* as one retrievable artifact, so the two cannot compete for the same rows. A JSON column on the run row would make the trace mutable with the run and would leave an unbound run with nowhere to record a decision; a separate keyed row keeps one record per run regardless of which table the run lives in, and keeps the append-only rule enforceable with a primary key.

**Honest limits.** Nothing calls the store yet: no assembler supplies candidates (ADI-CTX-001 is design-only) and no policy source exists, so activation — and therefore its trace — has no producer on the live path. The store is the seam a caller will use; it is not a capability surface (D8 untouched).

## 12. Open items

| Item | Why it is open |
|---|---|
| Which ranking provider, and whether it may be a model at all | A model in the ranking path is allowed by this contract (stage B only) but needs its own evaluation evidence before adoption. |
| Policy scope: tenant-only vs per-agent overlays | The contract supports both (rules are additive with deny-overrides); which one ships first is a product decision. |
| Whether `approvalRequired` becomes a fourth decision state (`gated`) | Today it is a field on `activated`; a distinct state would be clearer in the UI and needs the trace format agreed first. |
| Budget interaction with adaptive re-planning | Re-planning mid-run may want a re-decide; today the decision is per run. |
