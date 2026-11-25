# ADR — WASM and Component Execution Strategy (D21, P7)

> **Status:** `accepted` (2026-11-24, documentation only) — direction record,
> not a build order. No runtime, dependency or binding change.
> **Related:** D13 (wasmCloud evaluation-only), D18
> (`architecture/adr-unified-integrations.md`),
> `ecosystem/19-integration-research.md` proposal 6,
> `architecture/adr-multi-provider-execution.md`.

## Context

WASM appears in three different roles in current discussions: (a) a sandbox
for tool code, (b) the unit that durable runs execute on, (c) the host
`wasmCloud` as an execution provider. Conflating them lets "WASM is
sandboxed" become "this run is authorized" — the exact fail-open pattern the
trust slice already rejected.

## Decision (D21)

1. **WASM Components are sandboxed capability execution** (proposal 6):
   they run tool code — including Skill `scripts/` and Code Mode output —
   behind explicit WIT capability interfaces, instead of on the host
   process. A WASM-hosted MCP server still speaks the same `Transport` seam;
   WASM changes *where a tool runs*, never *how the agent addresses it*.
2. **A component is never a registry, a grant store, a permission language,
   or an authorization decision.** `mcp.Gate` stays the sole authority; a
   WIT import never implies a Vibe permission; a component never
   self-authorizes tenant data access.
3. **Tool Manifest and WIT stay associated but separate:** the Manifest is
   the capability identity (version, I/O schema, permissions, provenance,
   policy); WIT is the Component Model ABI. Same `capabilityId` + audit
   context must survive native and WASM execution.
4. **Full-Eino-inside-WASM is RESEARCH_REQUIRED, never assumed.** Model A
   (native Go + Eino) is the baseline; Model C (native orchestrator + WASM
   tool components) is the preferred practical path; Model B (Eino inside a
   Go WASI component) is the primary feasibility experiment — see
   `architecture/eino-wasm-compatibility.md`. Success of wasmCloud must not
   depend on Model B succeeding.
5. **Every WASM claim needs a PoC row** (`docs/architecture/wasm-runtime-matrix.md`);
   unexecuted rows stay `NOT_RUN`, never `PASS`.

## Consequences

- Positive: one sandbox story for Skills, Code Mode and future tools;
  D13 stays enforceable because "runs WASM" ≠ "passes C1–C18".
- Cost: WIT + Manifest mapping is new spec surface owned by no task yet
  (`ARCH-WASM-002` is the spike that would inform it), not by this record.
- Neutral: no Sandbox implementation lands here
  (`backend/pkg/toolsearch/codemode.go` stays a gate, not a runtime).

## Revisit triggers

1. A capability family cannot be expressed as Tool Manifest + WIT.
2. WASI P3 host support becomes verifiable (today: research only).
3. The PoC proves Model B viable — then, and only then, a promotion ADR.

## Evidence

- Gate the components call through: `backend/pkg/mcp` (+
  `backend/pkg/toolsearch/codemode.go` refusal without a Sandbox).
- Prior art cited, not re-stated:
  `docs/ecosystem/19-integration-research.md` §P6.
