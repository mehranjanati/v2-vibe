# Controlled Promotion and Rollback — Normative (PROPOSED, ADI-EVAL-004)

> **Status:** `proposed` (2026-11-23) — design only. No route, no table, no code in this change.
> **Inherits:** [`candidate-lifecycle.md`](candidate-lifecycle.md) (states, pointer, compare-and-set) and [`evaluation-contract.md`](evaluation-contract.md) (verdicts and gates). **Umbrella:** [`evaluation-and-evolution.md`](evaluation-and-evolution.md) §4.

## 1. Scope

The candidate lifecycle decides *whether a candidate exists and is approved*; this contract decides *how the pointer moves, and how it moves back*. Promotion is the only operation in ADI that changes what production runs, and rollback is the only operation that can undo it — so both are defined here in terms of the conditions that must hold and the trail they must leave.

## 2. The promotion gate is a conjunction, never a score

A promotion succeeds only when **all four** hold:

| # | Condition | Failure |
|---|---|---|
| 1 | The evaluation gate for the **target environment** is `pass` on every required layer (§4) | `E_PROMO_EVIDENCE` |
| 2 | The approval requirement for that environment is met (§3) | `E_PROMO_APPROVAL` |
| 3 | The candidate has not expired (`expiresAt`) | `E_CAND_EXPIRED` |
| 4 | The pointer compare-and-set on `expectedCurrentSnapshotId` succeeds | `E_CAND_CONFLICT` |

There is no partial credit and no weighting: a beautiful evaluation with no approval does not promote, and a signed approval over a failing gate does not promote. "One of four" is how production ends up running something nobody actually cleared.

## 3. Approval

| Rule | Detail |
|---|---|
| Actor identity | Taken from the authenticated session, never from the request body |
| Binding | An approval binds to the **evaluation artifact digest**. A new evaluation requires a new approval — an approval is not a reusable permission |
| Expiry | Approvals expire with the candidate; there is no standing "approved forever" state |
| Reason | Required, free text, copied into the audit row |
| Count per environment | Policy: `development` 1, `staging` 1, `production` 2 (proposal, §11) |
| Separation of duties | The person who proposed the candidate may not be its sole approver in `production` |

**The solo-operator exception, stated rather than hidden.** A single-maintainer tenant will be author, proposer and approver. The contract allows it only as an explicit, recorded exception (`selfApproval: true` on the approval), so the audit trail says "one person cleared this" instead of implying an independent review that never happened. Pretending the rule cannot be violated is how audit trails become fiction.

## 4. What "controlled rollout" means without a router

The platform has **no traffic-splitting layer** — there is one control plane and one execution path (D17), and a canary percentage would need a router that does not exist. What exists is the environment ladder from the System's own `spec.deployment.environment`, so the rollout is defined on environment pointers:

```text
development → staging → production
```

| Rule | Detail |
|---|---|
| Each step is its own promotion | Own gate evidence, own approval, own compare-and-set, own audit row |
| Evidence does not travel automatically | A `development` pass does not authorize `production` unless the gate policy explicitly waives the layer — and a waiver is recorded (§3 of `evaluation-contract.md`) |
| The ladder may be shortened only by policy | A tenant may promote straight to `production`; the gate still applies, so the shortcut costs evidence, not safety |
| Environments are labels on one control plane | With D17 there is no second host; "staging" is a pointer, not an environment to prove things in |

Percentage canaries, shadow traffic and A/B splits are **out of scope by absence of infrastructure**, not by preference. They need a routing layer and its own ADR.

## 5. Rollback is two different operations

Conflating them is the classic failure: a silent rollback to a poisoned version, or an ad-hoc pin that bypasses evidence with no trace. The contract separates them by *what the target is*.

### 5.1 `rollback` — to a snapshot that was previously stable

| Rule | Detail |
|---|---|
| Target proof | The target must appear as a previous stable value **in this pointer's promotion history**. "I think it used to be stable" is not enough |
| New evaluation | Not required: it already ran at this pointer. Requiring a fresh gate would make rollback slower than the incident it answers |
| Retirement check | If any asset in the target closure is retired or obsolete, the rollback is **refused** (`E_PROMO_RETIRED`) — returning to a version withdrawn for a safety reason is not a rollback, it is a regression |
| Approval | One approver; the reason is mandatory and appears in the audit row |
| Pointer | Compare-and-set, exactly like promotion: the caller states what they believe is current |

### 5.2 `emergency pin` — to any verified snapshot

| Rule | Detail |
|---|---|
| When | The previous stable snapshot is also unusable (the incident is in both) |
| Requirements | A verified snapshot, one named decision, a mandatory reason, and the retirement check of §5.1 |
| Marker | The pointer is marked `pinned-emergency` until a normal promotion clears it, so the state is visible rather than silently permanent |
| Audit | Same row shape, different action (`composition.pointer.pinned_emergency`) |

Neither operation restores anything: **both re-point** (`candidate-lifecycle.md` §5). Runs already in flight keep their own snapshots and are unaffected.

## 6. Audit (the second acceptance criterion)

Every pointer movement emits exactly one row in the existing `audit_logs` table (`worker/database/schema.ts`) — no new table, no second sink:

| Column | Value |
|---|---|
| `entity_type` | `composition_pointer` |
| `entity_id` | `<tenantKey>:<systemRef>:<environment>` — the pointer's identity |
| `action` | `composition.candidate.proposed` \| `.approved` \| `.rejected` \| `.expired` \| `.promoted` \| `.superseded` \| `.rolled_back` \| `.pointer.pinned_emergency` |
| `old_values` | `{snapshotId}` before the move (null for the first promotion) |
| `new_values` | `{snapshotId, candidateId, evaluationArtifactDigest, approverId, reason}` |
| `user_id`, `ip_address`, `user_agent` | The authenticated actor of the request |
| `created_at` | Injected clock |

Rules:

1. **No pointer movement without its audit row.** The pointer change and the audit insert are one unit: if either fails, neither is kept. A promotion that happened and cannot be explained is worse than a promotion that failed loudly.
2. **Append-only.** Nothing in this flow updates or deletes an audit row.
3. **The row must be enough to reconstruct the decision** — which snapshot, from which, approved by whom, on which evaluation artifact, and why. If a future engineer cannot answer "why is production running this?" from the row plus the snapshot, the row is incomplete.
4. **Retention follows audit retention**, not run retention: the trail outlives the runs it describes.

## 7. Failure modes

| Code | Condition | Behaviour |
|---|---|---|
| `E_PROMO_EVIDENCE` | Missing or non-passing gate for the target environment | Refuse; name the layer and its verdict |
| `E_PROMO_APPROVAL` | Approval missing, expired, bound to a different artifact, or insufficient count | Refuse; name which condition failed |
| `E_PROMO_RETIRED` | The target closure contains a retired/obsolete asset | Refuse rollback and emergency pin alike |
| `E_PROMO_TARGET` | A rollback target that never was stable at this pointer | Refuse; suggest an emergency pin instead (different operation, different record) |
| `E_PROMO_TENANT` | Pointer or candidate from another tenant | Refuse |
| `E_PROMO_AUDIT` | The audit row could not be written | Refuse the move; do not promote unlogged |
| `E_CAND_CONFLICT`, `E_CAND_EXPIRED`, `E_CAND_STATE` | Inherited from the lifecycle contract | Unchanged behaviour |

## 8. What this contract does not do

- No traffic splitting, canary percentage, shadow traffic or A/B (needs a routing layer and an ADR).
- No automatic rollback: a controller that reverts on metric regression is a design of its own, and it must not be smuggled in as "just a trigger".
- No approval UI, notification or escalation workflow.
- No new table, route or store: the audit sink already exists, and the pointer storage decision remains in `candidate-lifecycle.md` §11.
- No restart of in-flight runs: they keep their snapshots.

## 9. Tests this contract will need (ADI-EVAL-004a, when code starts)

1. Promotion with a passing gate and a valid approval succeeds, and one audit row is written.
2. The same promotion with a failing gate is refused with `E_PROMO_EVIDENCE` and writes nothing.
3. An approval bound to a previous evaluation artifact is refused (the binding check).
4. `production` with a single approver in a two-approver policy is refused; the same request with `selfApproval: true` is refused for the *second* approver but recorded as an accepted exception for one.
5. A stale `expectedCurrentSnapshotId` yields `E_CAND_CONFLICT` and leaves the pointer and the audit log untouched.
6. A rollback to a snapshot that was previously stable succeeds without a new evaluation, with the reason in the audit row.
7. A rollback to a snapshot that never was stable is refused with `E_PROMO_TARGET`.
8. A rollback whose target closure contains a retired asset is refused with `E_PROMO_RETIRED`.
9. An emergency pin requires a named decision and marks the pointer `pinned-emergency`; a later normal promotion clears the marker.
10. An audit-write failure refuses the promotion (`E_PROMO_AUDIT`) and leaves the pointer unchanged.
11. Promoting to `staging` does not authorize `production`; the second promotion needs its own evidence and approval.
12. A run started before a rollback still resolves its original closure afterwards.

## 10. Open items

| Item | Why it is open |
|---|---|
| Canary/shadow routing | Requires a traffic layer that does not exist; O4-adjacent, and needs its own ADR |
| Automatic rollback on regression | Distinct from auto-promotion and still needs an evaluation-driven controller design; until then a human decides |
| Two-approver policy for `production` | Proposed here, not decided; a single-maintainer deployment may need a different default |
| Change-freeze windows | Operationally valuable, but the calendar belongs to the tenant, not to the platform contract |
| Whether rollback should ever require re-evaluation | Today: no, because the target already ran at this pointer; revisit if evidence freshness becomes a policy lever |
