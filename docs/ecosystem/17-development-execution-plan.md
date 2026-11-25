# Development Execution Plan — P0–P9 Task Order (Canonical)

> **Status:** `adopted` 2026-11-21; integration-loader line added 2026-11-23.
> Backlog: `tasks/MASTER_BACKLOG.json`.
> Next: `tasks/NEXT_EXECUTION.md`. Graph: `tasks/DEPENDENCY_GRAPH.md`.
> Integration research: `ecosystem/19-integration-research.md` (proposals 1–7).

Execute P0 blockers first, then P1 foundation, then product slices in roadmap
order. Preserve historical IDs; new IDs use PHASE-MODULE-TASK. Solo-developer
sizing S/M/L/XL with explicit tests and docs per task. P7–P9 stay backlog
until prerequisites land. Recommended first five: P0-SEC-001 session rollout,
P0-SEC-002 tenant isolation tests, P0-SEC-003 WebSocket auth, P0-SEC-004 vault
plus revocation, P0-SEC-005 workflow isolation.

## Integration-loader execution line (added 2026-11-23)

Every integration lands as a **loader onto one core** (Tool Manifest → `mcp.Gate`
→ AuditSink), never as a new engine. Recommended order, each a standalone task
with the explicit tests from the matching proposal:

1. `P1-AGT-004` (**DONE 2026-11-24**): finish the MCP surface — authenticated
   grant/revoke/discover/execute routes, `SSETransport`, generic `vault:` store
   (proposal: MCP appendix A; D8.2/D8.3/D8.4). The stdio PoC (`P1-AGT-003`) stays the execution
   proving ground; the loader tasks make it product-usable.
2. `P7-SKL-001` (proposed): Skill Loader — `SKILL.md` frontmatter validation
   (agentskills.io: `name` 1–64 chars, `description` 1–1024, SPDX license tag),
   progressive-loading budget (~100 tokens/resolve, full body < 5000, resources
   on demand), `scripts/` execution only through the Gate with `allowed-tools`
   intersection (proposal 2). Community Skills start read-only.
3. `P7-OPI-001` (proposed): OpenAPI Importer — one operation → one Tool
   Manifest (proposal 3); every surface needs owner + version pin + the same
   approval/idempotency classes before it conducts.
4. `P7-SCH-001` (proposed): dynamic tool search (`search_tools`, proposal 5)
   so 100+ surfaces do not explode the context window; distinct from (not a
   substitute for) static manifests of proposal 2.
5. Registry mirror + publish flow (M5 prep, proposal 4): read-only cache of the
   official `server.json`, `PublisherRecord` with provenance, review gate;
   `Verified` at marketplaces, never before.

Sizes: (2) and (3) are M each, (4) is M, (5) is L. (1) is already sized L.

## Adaptive Intelligence workstreams (PROPOSED 2026-11-23, design-only, BLOCKED until deps/decisions clear)

ADI-FOUNDATION (IR parity audit, lock, resolver, snapshot) → ADI-CONTEXT-ACTIVATION (context, policy, engine design, trace) → ADI-MEMORY-PERSONALIZATION (provider interface, scope enforcement, profile, retrieval) → ADI-EVENTS (envelope, durable delivery design, event-to-activation; no broker without ADR) → ADI-EVALUATION-EVOLUTION (contract, benchmark design, candidate lifecycle, promotion/rollback) → ADI-ECOSYSTEM (bundle, registry design, provenance/signing, marketplace; BLOCKED on O24/O25). First executable proof: bounded M0-consistent single-agent composition (D8/D9). Task IDs: `tasks/MASTER_BACKLOG.json`.
