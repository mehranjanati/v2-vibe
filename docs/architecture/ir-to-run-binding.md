# System ↔ Run Binding — Design (PROPOSED, ADI-FOUND-005)

> **Status:** `proposed` (2026-11-23) — design only. No compiler, plan type, column or code in this change.
> **Question:** how does a stored composition snapshot become something that actually runs? Nothing answers it today, which is why no run is snapshot-bound and `CONTROL_PLANE_REQUIRE_SNAPSHOT` refuses every run (`run-snapshot.md` §13).
> **Related:** `system-ir.md` (the two shapes), `run-snapshot.md` (the subject), `composition-resolver.md` (how the closure is produced), `../ecosystem/06-runtime-and-execution-architecture.md` (its open item "bind RunPlan to the System IR payload").

## 1. The gap, stated plainly

A snapshot records *what* a System resolved to. A run needs a **plan**: an ordered, acyclic set of durable steps, a tenant, and the guarantees it requires. The repository has all three halves and no bridge between them.

| Half | What exists | Where |
|---|---|---|
| Declarative System | `kind: System`, exactly one shape: `agents`+`topology`, or `workflow` | `backend/pkg/ir`, `system-ir.md` |
| Procedural runtime | DAG schema v2 — nodes plus `WorkflowV2Edge{source,target,condition}`, seven node types | `worker/workflow/VibeWorkflow.ts` |
| Execution contract | `RunPlan{ID, Tenant, Steps, Requirements}`, `Step{Name, DependsOn, Retry, Timeout, IdempotencyKey}` | `backend/pkg/execution/contract.go` |

The bridge is a **compiler**: snapshot + System document → plan. This document defines that contract and, more importantly, says where it is honest to build it today.

## 2. The asymmetry that shapes the design

The two IR shapes are not equally closeable to a runnable plan, and pretending otherwise would be the classic "the architecture could support it" mistake.

| IR shape | Compiles to the current runtime? | Why |
|---|---|---|
| **Procedural** (`workflow` with `nodes`/`next`/`when`) | **Yes, 1:1** | The IR's node kinds are the same seven the interpreter already runs (`trigger`, `http`, `db`, `ai`, `email`, `condition`, `sleep` in `backend/pkg/ir` and `WORKFLOW_NODE_TYPES`), and IR `next[]` maps onto DAG edges |
| **Agentic** (`agents` + `topology`) | **No** | No runtime node type runs an agent. An agentic System is executable only by the Eino engine paths (`backend/pkg/engine/team.go`, `plan_execute.go`), which take prompts and tools today, not a snapshot |

Two honest outcomes follow:

1. **Procedural Systems get a real compile path now** — existing kinds, existing interpreter, no new runtime.
2. **Agentic Systems are refused with `E_BIND_SHAPE`**, not lowered. Lowering an agentic System into a `trigger -> ai -> email` chain would silently discard delegation, topology order and per-agent policies: a plan that runs but does not mean what the manifest says.

## 3. The compile contract

```text
Compile(snapshot, systemDocument) -> (Plan, []Finding)

Plan{
  Kind             PlanKind      // "workflow" today; "agentic" reserved and refused
  SnapshotID       string        // the subject this plan was derived from
  SystemRef        string
  Tenant           string
  Steps            []Step        // execution.Step, so the provider contract is reused
  Requirements     []Capability  // what the provider must advertise (C16)
  CompilerVersion  string
  PlanDigest       string        // sha256 over the canonical plan, clock excluded
}
```

| Rule | Why |
|---|---|
| **The snapshot is the only source of capability truth.** A step may reference only a ref present in the closure; anything else is `E_BIND_CAPABILITY` | The plan must not widen what the lock recorded |
| **The document must be the snapshot's document** (canonical digest compared before compiling) | Two different Systems must never share a run |
| **Deterministic** — same snapshot and `compilerVersion` give a byte-identical plan, step order included | Otherwise the plan cannot be explained and `planDigest` is meaningless |
| **No secrets, grants or runtime state** in a plan | Same rule as the lock: derived data, never authority |
| **Refuse, never degrade** — unknown kind, unclosed capability, agentic shape | A plan that silently drops a step is worse than no plan |


## 4. Mapping, step by step (procedural)

| IR | Plan / DAG | Notes |
|---|---|---|
| `workflow.nodes[].id` | Step `Name` | Already a slug, already unique (I5), stable across replays |
| `workflow.nodes[].kind` | DAG node type | Identity — the vocabularies are the same seven values |
| `workflow.nodes[].next[]` | Edges `source -> target`, and `Step.DependsOn` as the inverse | One mapping, two consumers: the interpreter reads edges, the provider reads `DependsOn` |
| `workflow.nodes[].when` | Edge `condition` on the **incoming** edge | The `when` belongs to the *target* node (the step that runs conditionally), carried verbatim; the compiler never evaluates a condition |
| `workflow.nodes[].capability` | Step requirement reference | Must resolve to a closure entry (`E_BIND_CAPABILITY` otherwise) |
| `workflow.entry` | DAG entry set | Reachability is already guaranteed by I5 |
| `spec.approvals[].on` | Gate on the matching step class | The same four-class vocabulary `mcp.Gate` uses |
| `spec.routing.onError` | Step retry/abort policy | `fail-run` and `skip-step` are expressible today; `escalate-human` maps to an approval gate |
| `spec.deployment.replicas` | Not a plan concern | Deployment is desired state; a run is one instance |

**Ordering is derived, never invented.** Steps are emitted in a deterministic topological order (Kahn, ties broken by node id), so two compiles of one snapshot agree byte for byte.

## 5. What the run records

Three additive fields on the run row, alongside the existing `snapshot_id`:

| Field | Purpose |
|---|---|
| `plan_kind` | `workflow` today; makes the refusal path visible in data |
| `plan_digest` | Which exact plan ran — recomputable from the snapshot, so it is a check rather than a copy |
| `compiler_version` | A compiler change becomes visible without a diff |

A plan is **not** stored as a blob: it is a deterministic function of (snapshot, compiler version), so recomputing is cheaper and stronger than trusting a stored copy. The digest on the run row is what proves the recomputation matches.

## 6. Refusals

| Code | Condition | Behaviour |
|---|---|---|
| `E_BIND_SNAPSHOT` | Snapshot missing, unverifiable, or the document digest differs | Refuse before compiling |
| `E_BIND_SHAPE` | Agentic System, or an IR without an execution shape | Refuse; a documented limitation, not a bug |
| `E_BIND_KIND` | Unknown node kind | Refuse |
| `E_BIND_CAPABILITY` | A step references a ref outside the closure | Refuse |
| `E_BIND_PLAN` | The compiled graph is cyclic or unreachable | Refuse — defence in depth, since IR I5 already rejects both |

## 7. What this unblocks

1. **A snapshot producer**: resolve → persist → compile → start, the flow `ADI-FOUND-004b` could not complete.
2. **`CONTROL_PLANE_REQUIRE_SNAPSHOT` becoming usable**, because event- and user-triggered runs would finally carry a plan derived from a snapshot.
3. **`ADI-CTX-003`** (activation engine): a decision needs a run that exists and a plan whose steps name capabilities.

## 8. What this design does not do

- No agentic execution path: that needs either an executor that accepts a topology plan or an `agent` node kind. Both are ADRs (the second changes the IR), and neither is smuggled in here.
- No new runtime, no new node type, no change to `VibeWorkflow`.
- No database change in this task: the three columns belong to the implementation task.
- No plan cache and no persistence of plan blobs.

## 9. Tests the implementation will need (ADI-FOUND-005a)

1. The `visual-workflow.system.json` fixture compiles to a plan whose steps, edges and entry match the IR exactly.
2. All seven node kinds survive the mapping unchanged.
3. `DependsOn` is the inverse of `next`, and a diamond produces one step with two dependencies.
4. Step order is deterministic: two compiles produce identical canonical bytes and identical `planDigest`.
5. `m1-team.system.json` (agentic) is refused with `E_BIND_SHAPE`, naming the limitation.
6. A document whose digest differs from the snapshot is `E_BIND_SNAPSHOT`.
7. A step capability outside the closure is `E_BIND_CAPABILITY`.
8. `when` text is carried into the edge condition verbatim (no evaluation, no rewriting).
9. `spec.approvals` produce the matching gates and the four-class vocabulary is preserved.
10. `routing.onError: escalate-human` becomes an approval gate, not a silent ignore.

## 9a. Implementation status — ADI-FOUND-005a (2026-11-23)

| Piece | Evidence |
|---|---|
| `backend/pkg/plan` | `Compile(snapshot, systemDocument)`; `Plan` carrying Steps/DependsOn (provider view) and Edges/When (interpreter view), Entry, Gates, Requirements, OnError, PlanDigest; `Canonical`/`Digest` |
| Refusals | `E_BIND_SNAPSHOT` (missing/unverifiable snapshot, foreign document, invalid IR), `E_BIND_SHAPE` (agentic), `E_BIND_KIND`, `E_BIND_CAPABILITY`, `E_BIND_PLAN` |
| Determinism | Kahn with the smallest ready id first; two compiles are byte-identical |
| Tests | 10 cases in `plan_test.go`, all green |

**One correction the tests forced:** the IR's `when` belongs to the **target** node (the step that runs conditionally), so reading it from the source produced empty conditions. Code and the §4 mapping row were both fixed — the kind of mistake that only surfaces when a test asserts the actual bytes rather than the intent.

## 10. Open items

| Item | Why it is open |
|---|---|
| Agentic lowering: new `agent` node kind vs a topology-aware executor | The first is an IR change (ADR, D7); the second a runtime change. Both are bigger than this binding and neither should be presumed |
| Whether `plan_digest`/`plan_kind` live on `workflow_instances` or a dedicated runs table | Storage shape, the same O27-adjacent decision as snapshots |
| How a plan references approval classes for the *agent* case | Follows from the agentic decision above |
| Whether plans may be regenerated mid-run after a compiler upgrade | Today: no — a run's plan is fixed by its snapshot and the recorded compiler version |
