# ADR — Agentic Execution on Snapshot-Bound Runs (PROPOSED)

> **Status:** `proposed` (2026-11-24, post-ADI audit). This ADR decides nothing;
> it records the open question so a future execution does not smuggle an answer
> inside a compiler or runtime change. It changes no accepted decision (D7–D18
> intact) and proposes no code, migration, route, or dependency.
> Related: `ir-to-run-binding.md` S2/S10, `run-snapshot.md`,
> `adi-implementation-evidence.md` (ADI_COMPILER row), `activation-policy.md`.

## Context

Procedural Systems (`workflow` shape) compile 1:1 onto the existing DAG today
(`backend/pkg/plan/plan.go:105`, 10 tests). Agentic Systems (`agents` +
`topology`) resolve, lock, persist and admit exactly like procedural ones, but
`Compile` refuses them with `E_BIND_SHAPE` — and the producer keeps the
snapshot while starting no run (`workflows_produce_test.go`:
`TestProduceRefusesAgenticShapeButKeepsTheSnapshot`). The snapshot half of the
pipeline is shape-agnostic; the execution half is not: no runtime node type
runs an agent, and the Eino paths (`backend/pkg/engine/team.go`,
`plan_execute.go`) take prompts and tools today, not a snapshot.

## Options (all open)

A. **New `agent` node kind in the DAG/IR.** Agentic Systems compile onto a
   runtime the interpreter already walks. Cost: an IR change (D7 amendment),
   interpreter work in `worker/workflow/VibeWorkflow.ts`, and a mapping for
   delegation/topology that does not exist yet.
B. **Topology-aware executor beside the DAG.** The compiler emits a second plan
   kind; a new executor walks agents/topology/delegation. Cost: a new runtime
   surface, new C-series contract questions (capabilities? cancel finality?),
   and new conformance work — without reusing the C1–C18 proof.
C. **Lowering (rejected unless this ADR says otherwise).** Compile agents into
   `trigger → ai → email` chains. Rejected by default: it silently discards
   delegation, topology order and per-agent policy, producing a plan that runs
   but does not mean what the manifest says (`ir-to-run-binding.md` S2).
D. **Keep refusing (status quo).** Agentic Systems stay resolvable, storable
   and evaluable (L1/L2 run on any snapshot) but not runnable. Cost: the
   headline ADI story — "a run that thinks in agents" — stays unwritten.

## Recommendation (none yet)

No recommendation. Options A and B both need measured need plus the owner
decision this ADR is waiting on; option C stays rejected; option D is where the
repository stands.

## Consequences if decided later

- A choice here amends exactly one boundary: D7 (option A) or the execution
  contract (option B). Everything else — resolve → persist → compile → admit,
  closure invariant, `mcp.Gate` final authority — is unchanged.
- The compiler version (`vibesdk-plan/0.1.0`) must bump with the decision, and
  existing procedural plans must keep verifying (no retroactive widening).
- Activation (ADI-CTX-003) is the first consumer of either outcome: an `agent`
  node and a topology step are both activation candidates under the same
  closure invariant, so this ADR and ADI-CTX-003 land in that order.

## Revisit triggers

A real agentic workload that cannot be expressed procedurally; an executor
passing a conformance suite the way the DAG passed C1–C18; O1/O27 decisions
that change what a snapshot may reference.
