# Community Extension Development (Canonical)

> **Status:** `adopted` 2026-11-21; integration-core conformance added 2026-11-23.
> Architecture: `ecosystem/09-marketplace-architecture.md`.
> Integration research: `ecosystem/19-integration-research.md` (proposals 1–7).

Build, test, publish, version, and maintain integrations, MCP servers, skills,
tools, agents, model adapters, execution providers, and commerce connectors.
Flow: author manifest → validate against schema → local test → sign →
publish with provenance → review plus certification → install as pinned
closure → update/deprecate with notices. Compatibility follows semver;
permissions declared up front; secrets by reference only (`vault:` refs, never literals); community supply
never executes with control-plane privilege.

## Integration-core conformance (added 2026-11-23)

Every extension kind lands through a **loader**, never a new engine. A loader maps
its artifacts onto the shared core (Tool Manifest vocabulary → `mcp.Gate` →
AuditSink) and is judged by the five conformance bullets in proposal 7 of
`ecosystem/19-integration-research.md` (gate-first, manifest-faithful, provenance
preserved, failure-shaped, no new auth plane). The kind-specific contract is
recorded per loader: MCP servers must pass the official Registry v0.1
`server.json` schema and the ToolHive lessons (five run modes, virtual server);
Skills must follow the agentskills.io `SKILL.md` frontmatter and the progressive-loading token budget (proposal 2); OpenAPI imports must carry an owner plus a version pin and expose exactly the audited surface (proposal 3).
