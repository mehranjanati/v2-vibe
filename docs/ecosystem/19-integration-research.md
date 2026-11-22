# Integration Research — MCP · Skills · Tools (Evidence Pack)

> **Status:** `adopted` 2026-11-23 as the loader-line evidence pack; strategy
> decision in D18, contract in ADR `architecture/adr-unified-integrations.md`,
> execution line in `ecosystem/17-development-execution-plan.md`.
> Scope: how Vibe reaches **maximum integrability with minimum complexity** by
> mapping every integration family onto **one Tool Manifest vocabulary, one
> `mcp.Gate`, one AuditSink**.
>
> Sources (all read, section references below): the official
> [MCP Registry](https://github.com/modelcontextprotocol/registry/) (API freeze
> v0.1, `ghcr.io/modelcontextprotocol/registry`), [ToolHive
> architecture](https://github.com/stacklok/toolhive/blob/main/docs/arch/00-overview.md)
> (proxy / middleware / workloads / virtual server), the [Arcade 2026 MCP
> taxonomy](https://www.arcade.dev/blog/mcp-gateways-runtimes-registries-guide/)
> (registry vs gateway vs runtime), the [Agent Skills
> specification](https://agentskills.io/specification) (SKILL.md frontmatter +
> progressive loading), and Cloudflare's [MCP transport +
> authorization docs](https://developers.cloudflare.com/agents/model-context-protocol/protocol/transport/)
> plus [Code Mode](https://blog.cloudflare.com/code-mode-mcp) + Portals.
>
> What this document is **not**: a build order change (see `17`), a license
> decision (see `08-open-source-and-community.md`), or an implementation. It is
> the evidence pack the loader tasks cite instead of re-stating the research.

## Contents

| Section | Anchor |
|---|---|
| The one-line strategy | [`#strategy`](#strategy) |
| Where Vibe stands today | [`#today`](#today) |
| Proposal 1 — one contract, many loaders | [`#p1`](#p1) |
| Proposal 2 — Skill Loader | [`#p2`](#p2) |
| Proposal 3 — OpenAPI Importer | [`#p3`](#p3) |
| Proposal 4 — registry mirror + publish flow | [`#p4`](#p4) |
| Proposal 5 — dynamic tool search / Code Mode | [`#p5`](#p5) |
| Proposal 6 — WASM Components as sandboxed execution | [`#p6`](#p6) |
| Proposal 7 — Cloudflare split + unified routes | [`#p7`](#p7) |
| Appendix A — current MCP surface traps | [`#appendix-a`](#appendix-a) |
| Appendix B — source index | [`#appendix-b`](#appendix-b) |

<a id="strategy"></a>
## The one-line strategy

**One Tool Manifest vocabulary, one execution gate, one audit sink — every
integration family lands as a loader, never as a new engine.** MCP servers,
Skills, OpenAPI services, builtins and (later) Components are different
doorways into the same house. The alternative — one platform per family — is
three tenant models, three audit trails that disagree, and a new fork per
family: the exact failure the M0–M6 dependency order forbids.

<a id="today"></a>
## Where Vibe stands today (2026-11-23, verified in-tree)

- `backend/pkg/mcp`: `Gate` with 11 checks, tenant-scoped grants, idempotency,
  append-only audit; `Registry` + `Transport` seam (stdio shipped).
- Executed: 12 gate tests + 5 transport tests spawning a real stub server
  (`backend/pkg/mcp/stdio_test.go`); per-room WebSocket authorization;
  `vault:`-only secret refs enforced by the manifest schemas.
- Missing (each is a numbered proposal below, each independently shippable):
  authenticated routes into the Gate, an SSE transport, a Skill loader, an
  OpenAPI importer, a registry cache, dynamic search, a generic vault,
  and the marketplace review gates.
<a id="p1"></a>
## Proposal 1 — one contract, many loaders

**Claim:** every artifact family converges on the Tool Manifest vocabulary
(name + I/O schema + side effect + approvals + idempotency + pinned version)
and executes only through `mcp.Gate` (registry → policy → grant → idempotent
execution → audit). Loaders differ; enforcement does not.

**Evidence.** The repo already behaves this way: `Transport` is a seam with a
live stdio adapter (`backend/pkg/mcp/stdio.go`); the gate's 11 checks are
transport-blind (`architecture/mcp-trust-slice.md` §gate). Arcade's 2026
taxonomy is the external confirmation: registries do *discovery*, gateways do
*routing with shared identity*, **runtimes** do *per-user authorized execution
with vaulted secrets and structured audit* — and only the runtime shape passes
compliance. Vibe already chose the runtime shape; proposal 1 says: keep it, for
every family.

**What it forbids (normative):** a loader may not invent its own tenant model,
its own grant, its own audit event, or its own secret handling. A family that
cannot be expressed as a Tool Manifest is a blocker, never a side-channel —
that is a `P7-SKL`/`P7-OPI` spec problem, not an excuse to bypass the Gate.

**Tests (carried by every loader task):** discovery without a grant performs no
execution (already proven: `TestGrantlessCallNeverSpawns`); a revoked/expired/
cross-tenant grant fails closed; secrets never appear in manifests, args, or
audit rows (already covered by the manifest schemas + P0-SEC-004 log review).

<a id="p2"></a>
## Proposal 2 — Skill Loader (agentskills.io)

**Claim:** Agent Skills are the cheapest integration surface because the standard
was designed for exactly these economics: `SKILL.md` frontmatter (`name`:
1–64 lowercase slug, `description`: 1–1024 chars, optional SPDX `license` tag,
`compatibility`, `metadata`, `allowed-tools`), plus `scripts/`, `references/`,
and `assets/` directories — with **progressive loading** (~100 tokens/resolve,
full body <5000, resources only on demand).

**Why it beats MCP for procedures:** a Skill advertises one paragraph, not one
tool per step; 1000 skills cost roughly what 10 chatty tool-servers cost in
context. Skills and MCP are complements: skills carry *procedure*, MCP carries
*action*.

**Adapter contract (for `P7-SKL-001`):**
1. Parse and validate `SKILL.md` frontmatter per the spec; reject upstream
   deviations from the field rules above.
2. Register `name → description` for resolve-time ranking only; load bodies
   lazily, resources on demand — never eagerly into the agent context.
3. Execute `scripts/` only through the Gate, with the skill's `allowed-tools`
   intersected against the tenant grant. A script is an agent capability, not a
   shell escape.
4. `Community` skills start read-only; anything with side effects needs the same
   `Verified` review as an MCP server (marketplace `trust levels`).

**Open research (not for now):** the `allowed-tools` field is marked experimental
upstream; treat it as advisory until the spec stabilizes, and keep the Gate's
`ToolPolicy` as the authority.

**Landed 2026-11-23 (`P7-SKL-001`, loader only):** `backend/pkg/agentskills` is
the parser + progressive-loading reader + Gate mapping for this contract —
`Loader.Load/All/Cards` validate the frontmatter and expose cards (name +
description, `ResolveTokenBudget`), `Skill.Activate` is the only body path
(`BodyTokenBudget`), `Skill.Resource` reads `scripts/`/`references/`/`assets/`
on demand with traversal refusal, `Community` skills are denied their scripts
until `Verified`, and `Skill.Bind(grants, now)` is a pure intersection so a
skill can only narrow authority. Pinned `mcp/<server>/<tool>@x.y.z` entries
become refs; bare upstream tool names stay advisory and authorize nothing, while
a malformed `mcp/` entry is rejected rather than silently downgraded.
Deliberately absent (D8): no skill activation inside an agent run, no route, no
runner. The package is `agentskills`, because `backend/pkg/skills` is the
existing per-role prompt registry and is unrelated.

<a id="p3"></a>
## Proposal 3 — OpenAPI Importer

**Claim:** the largest instant catalog in the world is the set of existing HTTP
APIs. Mapping one operation → one Tool Manifest (method + path + request/response
schemas) imports thousands of integrations without writing one MCP server, and
each imported surface inherits the five classes (approvals, idempotency,
`vault:` secrets, version pin, review gate) for free.

**Adapter contract (for `P7-OPI-001`):** every generated manifest carries owner +
version pin; an import exposes *exactly* the audited surface, never the whole
spec by default (endpoint allow-list per import); credential-shaped fields
(`Authorization`, tokens, keys) are forced to `vault:` refs at import time, not
optional afterwards.

**What it is not:** a substitute for first-party MCP servers where the vendor
maintains one. Imported surfaces are a fast lane; curated servers remain the
quality lane — and the publish flow of proposal 4 is what promotes between them.

**Landed 2026-11-23 (`P7-OPI-001`, loader only):** `backend/pkg/openapiimport`
implements this contract. `Load` reads OpenAPI 3.0/3.1 in JSON or YAML; `Import`
turns one allow-listed operation into one pinned manifest (identity from
`operationId` or a stable method+path slug, `resource:action` permissions,
method→authority: GET `safe`, POST/PUT/PATCH `external`+`external-write`, DELETE
`irreversible`+`human-review`, idempotency from the addressed path parameter or a
body `idempotencyKey`, object bodies inlined, `tenant` required, provenance from
owner/commit). Credential material never lands in a manifest: security schemes
and `Authorization`/`X-Api-Key`/cookie parameters become
`vault:<publisher>/<slug>` refs and are removed from the input schema; an
undefined or unsupported scheme fails the import instead of producing a silently
unauthenticated tool. The audited surface is mandatory (no allow-list = refusal,
`AllowAll` is the explicit reviewed escape hatch, typos fail loudly), output is
canonically serialized and sorted, and `Merge` makes a repeat import idempotent
while refusing same-ref/different-content. `Imported.Policy()` maps onto
`mcp.ToolPolicy`, so an ungranted call is denied before any HTTP. No transport,
no registry persistence, no Code Mode: those are not this loader's job.

<a id="p4"></a>
## Proposal 4 — registry mirror + publish flow

**Claim:** mirror the official MCP Registry, do not rebuild it. The Registry
(API freeze v0.1, `ghcr.io/modelcontextprotocol/registry`) publishes via the
`mcp-publisher` CLI with namespace ownership proven by GitHub OAuth/OIDC or
DNS/HTTP challenge, and ships a versioned `server.json` per server. Vibe caches
**read-only**: name, version, transports, packages/remotes, publisher identity
(the provenance half). Trust is *our* layer on top: `Private` (self-installed),
`Verified` (reviewed + signed), `Community` (unreviewed, read-only until review).

**Consequences:** no proprietary `serverId` scheme (use the Registry
`io.*`/`com.*` names + semver as the identity); publisher identity travels with
the artifact from day one (it feeds the ADRs' provenance slots); `Verified` is
awarded at the marketplace review gate (M5), never claimed by the presence of a
manifest.

**ToolHive lessons absorbed here:** the five ways to run
(`thv run <name> | <image> | uvx:// | npx:// | <URL>`) become our loader input
kinds; **virtual MCP server** (compose many servers behind one endpoint) is the
pattern for multi-skill teams in M1; per-runner resource limits plus middleware
chain is the shape our SSE/Streamable layer should converge on.
<a id="p5"></a>
## Proposal 5 — dynamic tool search / Code Mode

**Claim:** static manifests do not scale to hundreds of surfaces, so the agent
also needs *search*, not instead of manifests but beside them:
`search_tools(query)` returns a handful of pinned candidates for the current
task, exactly like Claude Code's dynamic search — and Cloudflare's Code Mode
result shows the prize: an entire API in ~1000 tokens by exposing **two tools
that run agent-written code in a sandbox** instead of one tool per endpoint.

**Design constraints (both drawn from the sources, both normative):**
- Search **augments** the Gate; it never replaces it. Every searched-in tool
  still goes: manifest → policy → grant → idempotent audited call.
- Code Mode needs a real sandbox (WASM isolate on Edge for hosted workloads,
  container where we control the host). Without the sandbox it is just remote
  code execution — this is why it is sequenced after the execution provider
  work (P7), not before it.

**Test shape (for `P7-SCH-001`):** fixed candidate bound (the suite asserts the
context slice has an upper bound), all surfaced tools grantees-verified before
call, and an explicit "search missed a capability" negative case so the ranking
is evaluated, not assumed.

**Landed 2026-11-23 (`P7-SCH-001`, search only):** `backend/pkg/toolsearch`
implements the contract beside the gate. `Catalog` is built from the loaders'
output (`AddImported` for the OpenAPI importer, `AddSkill` for an agentskills
skill's pinned `allowed-tools`), so search covers exactly the capabilities that
already have manifests. `Search(query, limit)` ranks on name then description
with fixed, documented weights, drops one-character and stop-word tokens, and
returns at most `MaxCandidates` (8) entries and `ContextTokenBudget` (600)
estimated tokens, reporting truncation instead of hiding it. `Verify`/`GrantID`
turn a match into a live grant id or nothing: `CallRequest` refuses an ungranted
match, and the Gate still re-checks registration, revocation, expiry, drift,
approvals and idempotency. A query with no match returns an empty `Missed`
result with a note telling the model to state the missing capability — there is
no "return everything" fallback. Code Mode is a gate, not a runtime: `NewCodeMode`
requires an explicit flag **and** a `Sandbox`, the zero value refuses, and a
static test asserts the package links no `os/exec`, `plugin` or `syscall`
execution path. No agent or route calls any of this yet (D8).

<a id="p6"></a>
## Proposal 6 — WASM Components as sandboxed execution

**Claim:** WebAssembly is an execution layer, not an MCP transport and not a
fifth registry. Its job in this architecture is one thing: run tool code —
including `scripts/` from proposal 2 and generated code from proposal 5 — inside
a sandbox with explicit capability interfaces (WIT), instead of on the host
process. A WASM-hosted MCP server still speaks the same protocol through the
same `Transport` seam; WASM changes *where a tool runs*, never *how the agent
addresses it*, and `StdioTransport` remains the compatibility adapter for the
servers the ecosystem actually publishes.

**Consequences:** no WASM-side grant store, permission language or registry
(D13 already keeps wasmCloud evaluation-only); no claim that a component is
"safe because WASM" — the Gate reviews the capability closure either way.

<a id="p7"></a>
## Proposal 7 — Cloudflare split + unified routes

**Three sentences that place every boundary correctly:**
1. Tools execute in the control plane (`StdioTransport` in Go) — the only place
   with processes, tenant-scoped secrets, and the engine's memory.
2. Authentication, discovery caches and publication happen on the Edge, where
   Cloudflare is natively strong (`createMcpHandler`, `workers-oauth-provider`,
   `createMcpHandler` + OAuth scopes, `Code Mode`-style portals) — but the Edge
   never grants, never resolves secrets, and never holds the audit source of
   truth.
3. All conducts — tools, skills, imports — share the same
   grant/revoke/discover/execute routes; there is exactly one auth surface per
   plane, not one per integration family.

**`P1-AGT-004` conformance (the lock this proposal closes):** any new `/api`
surface must answer **JSON 404/503 for unknown paths, never SPA HTML** (the
invariant `buildLightApp()` already enforces and `docs:check` gates);
per-family auth middleware is banned — identity comes from the session token,
tenancy from the tenant store, every time.

<a id="appendix-a"></a>
## Appendix A — current MCP surface traps

Three design traps the stdio adapter already had to close; any future
transport (SSE), loader, or `search_tools` must close them again:

1. **Non-JSON stdout treated as error, never as data** (`TestStdioTransportRejectsNonJSONStdout`).
2. **Hanging server killed at the deadline, reported as denial** (`TestStdioTransportTimesOut`).
3. **Grantless calls denied before the server process spawns at all**
   (`TestStdioTransportGrantlessCallNeverSpawns`) — the Gate, not the
   transport, decides.

<a id="appendix-b"></a>
## Appendix B — source index

| Source | What it contributed |
|---|---|
| `modelcontextprotocol/registry` (v0.1 API freeze) | publish-via-CLI, namespace ownership, `server.json`, provenance-not-trust |
| ToolHive `docs/arch/00-overview.md` | proxy + middleware + Registry, five run modes, virtual server, K8s operator shape |
| Arcade "gateways, runtimes & registries" (2026) | registry/gateway/runtime split; vaulted secrets + per-user audit as the Runtime test |
| `agentskills.io/specification` | `SKILL.md` frontmatter rules; progressive loading budgets; experimental `allowed-tools` |
| Cloudflare transport + authorization docs | Streamable HTTP as the standard; `createMcpHandler`; OAuth Provider split; per-tool scopes |
| Cloudflare Code Mode + Portals | two-tools-in-1000-tokens; composed gateway with unified auth as the M5 portal shape |

