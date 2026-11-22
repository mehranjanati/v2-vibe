// Package agentskills is the P7-SKL-001 loader for Agent Skills
// (agentskills.io `SKILL.md`): a parser, a progressive-loading reader, and a
// pure mapping onto the mcp trust slice.
//
// What this package is (and is not):
//
//   - It is a *loader*, not an engine. Under decision D18 every integration
//     family (MCP servers, skills, OpenAPI imports, builtins) converges on one
//     capability vocabulary and one execution authority (mcp.Gate). This
//     package therefore never spawns a process, never dials a server, and never
//     authorizes anything by itself: Bind only reports which of a skill's
//     pinned tools a tenant's *existing* grants already cover.
//   - It is not wired into any agent, route, or run. Capability stays closed by
//     the D8 M1 freeze; this is the same proving-ground status as the stdio
//     transport PoC (D8.1). Enabling a skill in an agent run is a separate,
//     O1-gated task.
//   - `allowed-tools` is advisory, as upstream marks it experimental. The Gate
//     ToolPolicy is the authority; a skill can narrow authority, never widen it.
//   - Trust (`Private`/`Verified`/`Community`) is supplied by the caller: the
//     marketplace owns trust levels, it is never a field in the document
//     (D18 §4). `Community` skills are read-only — their `scripts/` are denied
//     until a `Verified` review.
//
// Economics the loader exists to protect (research log, proposal 2):
// resolve costs roughly 100 tokens (name + description only), the body stays
// lazy and under 5000 tokens, and `references/`/`assets/` load on demand.
package agentskills
