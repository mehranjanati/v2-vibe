# Product Boundaries and Shared Contracts (Canonical)

> **Status:** `adopted` 2026-11-21. Products: `ecosystem/01-product-ecosystem.md`.
> Architecture: `04-system-architecture.md`. Security: `07-multitenancy-security.md`.

## Independence rule

Each product owns its surface, API, SDK integration, deployment, releases,
docs, and offer. Sharing happens through versioned contracts, never copies.
Duplication requires a written reason.

## What varies per product (and what never does)

Products vary only in: user surface and product API shape, System IR
composition (which agents/skills/tools/knowledge are pinned), policy and
approval defaults, and commercial offer. Products never vary in: control
plane, edge/auth plane, identity truth, manifest/IR dialects, execution
contract, memory-scope enforcement, event envelope, metering source, or
audit sink. Scale, provider choice, and tool/MCP surface are the only
technical axes of differentiation.

## Plane split

- **Control plane** (Go in backend): sessions, rooms/VFS, LLM streaming,
  Pages deploys, workflow runs. Per-route auth; no blanket gateway. PARTIAL
  (session boundary opt-in).
- **Edge/auth plane** (light Worker): SPA, D1 auth, GitHub OAuth/export,
  app lists, status, capabilities, usage limits. IMPLEMENTED.
- **Execution plane** (Vibe Runtime): runs agents/tools/skills/workflows
  under quotas with history and tracing. PARTIAL.
- **Product services**: own UX/APIs; depend on contracts below. PLANNED
  except Coder/Studio seeds.

## Shared contract set (all versioned)

Identity, Agent Manifest, System IR, Model Provider, Tool/Skill, Execution
Provider, Memory Provider, Storage Provider, Event Provider, Marketplace
Extension, Metering, Audit. Owners: `05-agent-platform-architecture.md`,
`06-runtime-and-execution-architecture.md`, `07-multitenancy-security.md`,
`09-marketplace-architecture.md`, `14-product-and-business-model.md`.

Web3 extension (PROPOSED 2026-11-25, additive only): Smart Contract IR,
Chain Profile, Interop Manifest, Deployment Record, Signing Request. None
replaces a contract above; System IR stays the whole-system model and
references contracts by pinned identifiers. Schemas land only after the W3
decisions are accepted.

## Interop

Delegation via typed agent-to-agent calls carrying tenant/user/agent identity.
External reach via MCP (tools), UCP/AP2 (commerce), OpenAI-compatible APIs
(models), WASM/WIT (portable components). Adapters independently versioned;
conformance tests before any compliance claim.

ADI boundary (2026-11-23, PROPOSED): composition vs activation stay separate; activation never mints grants or bypasses the Gate; extensions never create identity/audit/secret/execution authority. Contracts: `architecture/composition-lock.md`, `architecture/context-activation-and-memory.md`.
