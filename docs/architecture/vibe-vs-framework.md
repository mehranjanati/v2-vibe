# Vibe vs Generic Agent Framework — Corrected Layering (Canonical)

> **Status:** `adopted` 2026-11-24. Companion: `adaptive-intelligence-architecture.md`.
> Pipeline: Intent → System Manifest → IR Validation → Capability Resolution →
> Composition Lock → Context Assembly → Activation → Policy Enforcement →
> Execution → Evaluation → Controlled Evolution.
> Boundaries: `adr-eino-orchestration-substrate.md` (accepted),
> `adr-agentic-execution.md` (proposed), `adr-adaptive-intelligence-boundaries.md`.
> IR: `system-ir.md` (D7). Lock: `composition-lock.md`. Snapshot: `run-snapshot.md`.
> Execution contract: `../ecosystem/06-runtime-and-execution-architecture.md` (D16, C1-C18).
> Evaluation: `evaluation-and-evolution.md`.

## 1. Generic Agent Framework vs Vibe (the distinction)

**Generic Agent Framework** — focus: *how to run an agent?*

- Agent / Node / Graph
- Prompt and context
- Tool calling
- Workflow orchestration
- Memory
- Retry and execution loop

**Vibe Intelligence and Execution System** — focus: *what is allowed to be
built, composed, and executed — and how it stays provable?*

- System-level composition
- Immutable dependency closure
- Run snapshots
- Capability resolution
- Controlled activation
- Policy enforcement
- Execution contract
- Tenant isolation
- Provenance
- Evaluation
- Controlled evolution

A generic framework manages execution. Vibe governs the bounds, identity,
structure, authority, and trustworthiness of execution.

## 2. Corrected layering

```text
Vibe — Governed AI System Platform

1. System Definition and Composition
   System IR (kind:System) · Agent · Skill · Tool · Knowledge ·
   Model-Policy · Eval
   Note: Workflow is a shape inside the System IR, not a separate kind.
   Integration is an adapter (MCP/A2A/UCP/AP2/WASM), not a manifest.

2. Resolution and Immutable Composition
   Dependency Closure · Exact Pins (@MAJOR.MINOR.PATCH, never latest) ·
   Digests · Composition Lock (derived, immutable, content-addressed).
   The lock never edits the manifest and never carries runtime state.

3. Run Admission and Snapshot
   Identity (definition/version/instance) · Tenant Binding ·
   Immutable Snapshot (snapshotId == lock digest, append-only, written
   before admission). A run references exactly one snapshot for its life.

4. Intelligence Runtime Controls
   Context Assembly → Memory (scoped: run/conversation/agent/team/tenant) →
   Activation (invariant: activated ⊆ eligible ⊆ closure) → Policy →
   mcp.Gate (final authority at call time). Activation never mints grants.

5. Orchestration Substrate (Eino — swappable)
   Graph execution · ReAct loop · Tool orchestration · Streaming.
   Consumes only admitted work (prompts + tools today; snapshot-bound plans
   when adr-agentic-execution decides). Eino never resolves a System,
   never mints a grant, never persists a run.
   Ordering rule: Eino runs INSIDE the durability primitive (step),
   because step is the only durability primitive (C-contract rule 1).
   The provider owns durability; Eino owns reasoning.

6. Execution Provider Contract (C1-C18)
   start · status · cancel · pause · resume · step · sleep · history ·
   capabilities. A provider executes plans; it never parses, validates,
   or invents them.

7. Infrastructure and Adapters
   Cloudflare Workflows (one provider, not the identity) / Local /
   WASM (tool sandbox, not a durable engine alone) / Container ·
   D1/Redis state · Gateway/Models · MCP/UCP/AP2/A2A adapters
   (each versioned, conformance before any compliance claim).

8. Evaluation and Controlled Evolution
   L1-L5 layers → verdict (pass/fail/not_evaluated/blocked;
   not_evaluated never passes, no composite score) → candidate lifecycle
   (candidate = snapshot; stable = pointer; CAS promotion) →
   rollout on environment pointers → rollback / emergency pin.
   Every pointer movement writes exactly one audit_logs row.
```

## 3. What is cross-cutting (not a layer)

Tenant isolation, provenance, metering source, and audit are not layers —
they ride every layer: snapshots bind to tenants; lineage =
compositionDigest + activation trace + execution history + evaluation
outcome; metering reads durable results only; audit rows gate promotion.

## 4. Why Eino sits above the provider contract (not below)

Placing Eino below the contract inverts ownership: the provider would
then depend on one orchestrator's loop semantics for durability. The
accepted ADR puts it the other way — the contract is orchestrator-neutral
(C1-C18 hold for DAG, agent-node, or topology executor alike), and the
orchestrator is replaceable without touching governance. Eino upgrades
stay mechanical; governance survives an orchestrator swap.
