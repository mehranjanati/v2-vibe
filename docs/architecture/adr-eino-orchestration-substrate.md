# ADR — Eino as the Current Orchestration Substrate (P1 · P7)

> **Status:** `accepted` (2026-11-24, documentation only) — records what the
> repository already does; decides nothing new. No code, migration, route or
> dependency change in this record.
> **Related:** D7 (one IR), D10/D11 (products/contracts),
> `ecosystem/05-agent-platform-architecture.md`,
> `architecture/adr-system-ir-v1.md`,
> `architecture/adr-agentic-execution.md`,
> `architecture/adr-unified-integrations.md`.

## Context

The Go control plane already orchestrates with Eino (`backend/go.mod` pins
`github.com/cloudwego/eino v0.9.21`): a stateless ReAct engine
(`backend/pkg/agent/eino_engine.go`), plan-execute-replan
(`backend/pkg/engine/plan_execute.go`), and a DeepAgent coordinator team
(`backend/pkg/engine/team.go`). Eino arrived incrementally; no ADR ever drew
its boundary. The risk is scope creep: treating the orchestrator as the owner
of governance, execution durability, or tenant authorization.

## Decision

Eino is the **orchestration substrate** — nothing more:

1. Owns: graph execution, model interaction, tool orchestration, concurrent
   execution, context propagation, streaming, cancellation propagation.
2. Does **not** own: System IR validity, composition/resolution, snapshot
   production, policy/authorization, run admission, tenant isolation,
   registry/provenance, evaluation/promotion. Those stay with the Control
   Plane, `mcp.Gate`, and the D16 execution-provider contract.
3. Consumes only admitted work: prompts + tools today; snapshot-bound plans
   when the agentic-execution ADR decides (options in
   `architecture/adr-agentic-execution.md`). Eino never resolves a System,
   never mints a grant, never persists a run.

## Consequences

- Positive: Eino upgrades stay mechanical; governance survives an
  orchestrator swap.
- Cost: two vocabularies to keep apart (Eino Tool vs Vibe Capability —
  see `architecture/capability-tool-mapping.md` when it lands).
- Neutral: no runtime change; `team.go` and `plan_execute.go` already obey
  this boundary (tools via `backend/pkg/agent/tools`, secrets via `vault:`).

## Revisit triggers

1. Eino (or its replacement) requires governance semantics to function.
2. Snapshot-bound agentic execution needs orchestration guarantees Eino
   cannot express.
3. A second orchestrator is evaluated — new ADR, no silent coexistence.

## Evidence

- Pinned dependency: `backend/go.mod`.
- Engine: `backend/pkg/agent/eino_engine.go`, `backend/pkg/engine/team.go`,
  `backend/pkg/engine/plan_execute.go`.
- Enforcement point Eino never bypasses: `backend/pkg/mcp`.
