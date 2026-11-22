# ADR — Execution Provider Contract (P7-RUN-001a)

> **Status:** `accepted` (2026-11-22) — contract recorded; adapters and the
> conformance suite landed 2026-11-23 (P7-RUN-001b) in `backend/pkg/execution`.
> **Deciders:** platform architecture. **Supersedes:** none.
> **Related:** D12/D13 (`ecosystem/10-decisions-and-open-questions.md`),
> `ecosystem/06-runtime-and-execution-architecture.md`.

## Context

Execution is the layer most likely to be replaced and the layer with the most
hidden semantics. The repository already contains two execution implementations —
the Cloudflare Workflow interpreter (durable steps, retry/timeout, tenant refusal)
and the Go engine paths — and the roadmap adds local, WASM and container
execution. Without a contract, "durable", "retried" and "cancelled" mean three
different things, and moving a workload between providers silently changes its
guarantees.

Two constraints are already decided: Cloudflare is one provider, not the platform
identity (D12), and wasmCloud never enters production before the contract plus a
PoC exist (D13). Both presuppose a contract that does not exist yet.

## Options considered

### A. Let each provider define its own semantics; document the differences *(rejected)*

- **Pro:** fastest to add providers; no abstraction tax.
- **Con:** guarantee drift becomes invisible. A plan that pauses for human
  approval on one provider would silently not pause on another; a `cancel` that
  returns success but does nothing is indistinguishable from a real cancel. That
  is the fail-open pattern the security work already rejected.
- **Verdict:** rejected.

### B. Adopt an existing durable-execution standard wholesale *(rejected)*

- **Pro:** less design work.
- **Con:** no current standard covers our actual unit (plan → steps → run state →
  tenant refusal → metering boundary) across serverless and self-hosted targets;
  adopting one would re-couple the platform to a single vendor's semantics, which
  is exactly what D12 forbids.
- **Verdict:** rejected. We borrow vocabulary, not a dependency.

### C. Minimal normative contract plus a conformance suite, plan-agnostic *(chosen)*

Nine operations, six run states, capability advertisement, an error taxonomy, and
an 18-case conformance matrix that every provider must pass.

- **Pro:** guarantees are testable, not aspirational; a new provider is judged by
  evidence; the contract does not mention Cloudflare anywhere, so it survives
  substrate change; and it is deliberately ignorant of where the plan came from,
  so the System IR work (P1-AGT-002) can land without reopening it.
- **Con:** up-front design cost, and the suite becomes a gate that can block a
  provider we want for cost reasons.
- **Verdict:** chosen. A blocked provider is the intended outcome; a silently
  weaker provider is the failure mode.

### D. Contract that also owns plan parsing/validation *(rejected)*

- **Pro:** one component to reason about.
- **Con:** mixes "what to run" (runtime + compiler, which needs IR) with "how to
  run it durably" (provider). It would force every provider to re-implement
  schema and IR validation, and would make the contract change whenever the IR
  changes.
- **Verdict:** rejected; the split is why this contract could land before
  P1-AGT-002.

## Decision

Adopt option C. Specifically:

1. The contract defines `start · status · cancel · pause · resume · step · sleep ·
   history · capabilities`; `step` is the only durability primitive.
2. Delivery is **at-least-once**, never claimed exact-once; side-effecting steps
   need idempotency keys.
3. Run states: `queued · running · succeeded · failed · cancelled · paused`, plus
   `rejected` for admission refusals — deliberately separate from `failed`.
4. Providers **advertise** capabilities; a missing capability refuses the plan at
   admission and is never a silent downgrade.
5. Errors map to a fixed taxonomy (`transient`, `permanent`, `policy-denied`,
   `quota-exceeded`, `cancelled`, `admission-refused`).
6. Durable run state and best-effort step events are different tiers; metering
   reads durable results only.
7. Nothing ships as a provider without a green C1–C18 suite.

## Consequences

- **Positive:** provider portability becomes measurable; the self-hosting promise
  gets an acceptance test; losing a background log write can no longer distort
  billing; cancellation has a defined meaning before anyone implements it.
- **Negative / accepted costs:** the current Cloudflare path is officially
  PARTIAL (no cancel/pause/capabilities/unified errors), and closing that gap is
  real work that delivers no new user-visible feature. The suite adds test
  surface that must be maintained per provider.
- **Neutral:** nothing in the live interpreter changes today; the contract
  describes behaviour it already has partially.

## Revisit triggers

1. A real workload needs a guarantee the six states cannot express (e.g. a
   distinct `waiting` state for approvals).
2. The suite blocks a provider for a reason that is cost, not correctness.
3. The IR payload (P1-AGT-002) cannot be expressed as a plan without changing the
   `step` primitive.
4. wasmCloud or a container runtime demonstrates a durability model that the
   `step` abstraction cannot represent.

## Evidence

- Contract, state machine, capability table, error taxonomy, conformance matrix:
  `docs/ecosystem/06-runtime-and-execution-architecture.md`.
- Live behaviour it describes: `worker/workflow/VibeWorkflow.ts` (validation,
  cycle refusal, tenant refusal, retry/timeout mapping, step logs) and
  `backend/pkg/engine/` execution paths.
- Executed: `backend/pkg/execution` — the reference `Fake` provider passes all
  eighteen cases (`TestFakePassesTheFullMatrix`), a provider advertising nothing
  runs fourteen and *reports* the four capability-gated skips
  (`TestPartialProviderSkipsWithReason`), admission rules C8/C9/C16 are proven in
  `ValidatePlan`/`Admit`, and the Cloudflare adapter's mapping, error taxonomy and
  capability refusals are stubbed (`cloudflare_test.go`). The adapter is **not**
  verified against a live account: C1–C7/C10 for it need a deployed workflow.
- Existing constraints it must respect: D12/D13 in
  `docs/ecosystem/10-decisions-and-open-questions.md`.
