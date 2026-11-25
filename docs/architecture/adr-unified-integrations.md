# ADR — Unified Integrations on One Contract (P1 · P7)

> **Status:** `accepted` (2026-11-22) — consolidation decision; no code in this
> change. Research: `ecosystem/19-integration-research.md` (proposals 1–7).
> Implements decision D18. Related: D8.1 (MCP PoC exception), D12 (Cloudflare as
> provider), D13 (wasmCloud evaluation-only), D17 (one control plane).

## Context

Three integration families arrived with three overlapping dialects: MCP servers
(stdio/SSE), Agent Skills (`SKILL.md` + scripts), and plain HTTP APIs. The
repository already has one enforcement point (`mcp.Gate` + `AuditSink`) and a
provisional `Transport` seam (`backend/pkg/mcp/stdio.go`). The open question was
whether each family gets its own registry, runner and auth — i.e. three parallel
platforms — or maps onto one contract. The ecosystem strategy (one control
plane, D17; Cloudflare as provider not identity, D12) and the D8 M1 freeze forbid
parallel-platform sprawl: every new engine is a new tenancy, audit and versioning
story.

## Options considered

### A. One platform per family *(rejected)*

A Skill runner, an MCP gateway service, and an "API connector" service, each
with its own discovery, auth and execution. **Pro:** each is simple in isolation.
**Con:** three tenant models, three audit trails (none of which agree), three
permission languages, and the engine would need capability objects that know
their origin. Every new family (WASM components, A2A agents) would fork the
system again. This is precisely the failure mode the roadmap forbids, and it
would violate D8's intent even where it respects its letter.
**Verdict:** rejected as unscalable by construction.

### B. MCP as the universal substrate *(rejected)*

Wrap Skills and OpenAPIs as local MCP servers behind a multiplexer. **Pro:** one
protocol on the wire today. **Con:** it forces Skills' progressive-loading
economics through a chatty RPC (the `SKILL.md` ~100-token resolve becomes a
tool-listing round trip per agent turn), invents a fake server per OpenAPI
service nobody publishes, and makes "which MCP server?" the unit of reuse
instead of "which pinned capability?". It inverts responsibility: the untrusted
boundary would own what should be platform-owned (versioning, provenance),

### C. One contract, many loaders *(chosen)*

Every artifact family maps onto the same vocabulary (Tool Manifest: name +
I/O schema + side effect + approvals + idempotency + pinned version) and calls
through the same authority (`mcp.Gate`: registry → policy → grant →
idempotent execution → audit). Each family lands as a **loader** (MCP server
adapters, `SKILL.md` parser, OpenAPI importer, builtin registry) plus optionally
a transport; enforcement, tenancy and audit are implemented exactly once.

- **Pro:** one Gate to harden, one audit trail to query, one permission language
  to review; a new family is a few hundred lines, not a new service. It keeps
  the proven boundaries (discovery≠grant, refs≠secrets, refs are pinned) intact
  for every family, and it is the only option consistent with D8/D12/D13/D17.
- **Con:** the Tool Manifest vocabulary must stay expressive enough for all
  families (the `P7-SKL`/`P7-OPI` specs own that), and `Gate` becomes the most
  depended-on package — changes to it need the 12 + transport tests green.
- **Neutral:** nothing in the live runtime changes; this records the direction
  already taken by `P1-AGT-003` (`StdioTransport`) and `P1-AGT-004` (SSE binding + session-guarded routes, landed 2026-11-24).

## Decision

Adopt option C:

1. Tool Manifest is the **only** capability contract; nothing executes what it
   cannot express.
2. `mcp.Gate` is the **only** execution authority; no route, agent, or loader
   invokes a server/backend directly.
3. Grants are tenant-scoped, revocable, idempotent and audited — for tools,
   skills and imports alike.
4. Servers/refs are pinned; registries mirror (never replace) the official MCP
   Registry; trust is a marketplace decision (`Private`/`Verified`/`Community`),
   never a field on the wire.
5. WASM Components are sandboxed execution for tools (proposal 6), not an MCP
   transport and not a fifth registry; the Edge carries caches and publication
   (proposal 7), never grants.

## Consequences

- **Positive:** any future family (A2A agents, new transports, Component
  artifacts) arrives as a loader + contract fill-in, not a platform. M5
  marketplaces distribute manifests, not servers/widgets.
- **Negative / accepted costs:** the manifest vocabulary and the Gate become
  load-bearing; `P7-SKL`/`P7-OPI` must prove the vocabulary is sufficient (a
  field the contracts cannot express is a blocker, not a workaround invitation).
- **Neutral:** no user-facing capability is enabled by this record alone.

## Revisit triggers

1. A family provably cannot be expressed as a Tool Manifest without losing
   safety semantics.
2. `Gate` becomes a measured latency bottleneck that per-call spawning cannot
   explain away (=> persistent sessions, a follow-up beyond P1-AGT-004).
3. The official Registry API or the agentskills.io spec makes a breaking change
   the contracts cannot absorb.
4. Legal review (W3.1/O23 lineage: provenance, M5 settlement) requires a
   different provenance split.

## Evidence

- Contract: `docs/architecture/system-ir.md`, `docs/architecture/mcp-trust-slice.md`.
- Executed: `backend/pkg/mcp/execution_test.go` (12),
  `backend/pkg/mcp/stdio_test.go` (5),
  `backend/pkg/agentskills/agentskills_test.go` (12 functions; the loader narrows
  authority and a grantless skill script refuses before the transport runs),
  `backend/pkg/openapiimport/openapiimport_test.go` (11 functions; imported
  manifests validate against `tool.schema.json` and an ungranted call is denied
  before any HTTP),
  `backend/pkg/toolsearch/toolsearch_test.go` (8 functions; a bounded,
  golden-tested slice where a surfaced tool still needs a live grant and Code
  Mode cannot run without a sandbox).
- Research: `docs/ecosystem/19-integration-research.md` proposals 1–7 with named
  sources (MCP Registry, ToolHive, Arcade, agentskills.io, Cloudflare).
breaking the "provenance, not trust" split.
**Verdict:** rejected — one wire protocol is not one trust model.