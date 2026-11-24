# Marketplace Architecture (Canonical)

> **Status:** `adopted` 2026-11-21; integration-core rule added 2026-11-23.
> History: `08-marketplace.md` (preserved).
> Lifecycle: `15-community-extension-development.md`.
> Integration research: `ecosystem/19-integration-research.md` (proposals 1–7).

Six surfaces share one registry plus extension architecture; individual
marketplaces come after maturity: Agent, Skills, Tools/MCP, Model/Provider,
Execution Provider, Commerce Integration marketplaces. Shared requirements:
extension manifests and metadata, semver plus compatibility, permissions,
security review plus trust levels, publisher identity plus provenance, signing
plus verification, testing plus certification, publication plus update
lifecycle, deprecation plus vulnerability notices. Registry before commerce;
commerce only on proven demand plus security controls.

## Integration core (normative, added 2026-11-23)

Marketplaces distribute versioned artifacts. But artifacts are useless without a
shared execution contract, so every marketplace surface compiles to the same
core: **one Tool Manifest vocabulary, one `mcp.Gate`, one AuditSink**
(`backend/pkg/mcp`). Loaders differ per artifact kind (MCP server, Skill,
OpenAPI service, builtin function); enforcement does not.

The full mapping — artifact kinds, the official registry to mirror (MCP
Registry `server.json`), the ToolHive lessons (virtual server, five run modes),
the three-sentence Cloudflare rule, and the per-surface activation rule — lives
in `ecosystem/19-integration-research.md` proposals 1–7. Rule of thumb: an
artifact family without a Gate path is a download page, not a marketplace.

ADI note (2026-11-23, PROPOSED): Intelligence Bundles are distribution groupings only — same manifests plus lock, no new kind/authority/secrets (`architecture/composition-lock.md` §9; backlog ADI-ECOSYSTEM, BLOCKED on provenance/signing).
