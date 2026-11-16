# Multi-tenancy & Security Model (PROPOSED)

> **Scope:** tenancy and security as first-class product concerns for the
> Agentic Platform and its future public launch. Layer
> context: [`04-system-architecture.md`](04-system-architecture.md).
> Enforcement point for capabilities:
> [`06-functions-and-tools.md`](06-functions-and-tools.md). Canonical
> strategy: [`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md).
> Live identity
> work today: engineering P0.3–P0.9 in
> [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md).
>
> **Status:** `proposed` — full multi-tenancy is not implemented; the live
> system has per-user auth (D1-backed) and an opt-in control-plane session
> boundary (P0.3). FOUNDATIONAL BLOCKER FOR PUBLIC LAUNCH. Last reviewed:
> 2026-11-15.

## Tenant model (required by design)

- **Strict tenant isolation** — no cross-tenant access is representable at
  any layer; every request, function call and query carries tenant context.
- **Tenant-aware authentication** — sessions/tokens are bound to a tenant.
- **Tenant-aware authorization** — roles and permissions are evaluated per
  tenant; capability checks include tenant policy
  ([`06-functions-and-tools.md`](06-functions-and-tools.md)).
- **Tenant-specific configuration** — settings, feature flags, approval
  policies.
- **Tenant-specific functions** — tenants may extend the function catalog
  without affecting others (M4 marketplace direction:
  [`08-marketplace.md`](08-marketplace.md)).
- **Tenant-specific workflows and agent behavior** — per-tenant config and
  prompt/policy inputs, not code forks. Generated workflows are tenant-scoped
  in D1: `workflow_dags.user_id` (owner, first-write-wins) and
  `workflow_instances.user_id` per run; the control plane authorizes
  trigger/get through `workflowGuard` (cross-tenant → 403 + `workflow.access_denied` audit, unknown/unowned → 404, outage → 503) and the runtime refuses tenant-mismatched runs — see `docs/DEV_CHECKLIST.md` P0.7.
- **Tenant-specific data model where justified** — the default is a shared
  schema with a mandatory tenant column; a separate data model requires an
  explicit justification (constraint: do NOT assume every tenant needs a
  completely custom database).

## Security model

| Concern | Rule |
|---|---|
| Capability authorization | Capability + tenant + role checked in the execution path — never only in prompts (unchanged platform principle). Prompt instructions are not authorization. |
| Agent identity | Every run carries tenant + user/agent identity; cross-tenant access denied by construction. |
| Credentials | Stored as references in a vault, never plaintext in capabilities, manifests, or agent context. |
| API keys | Scoped, revocable, tenant-bound; never shared across tenants. |
| Human approval | External side effects (email, message, payment) require approval or an explicit tenant policy — logged either way. Approval policies and gates are launch blockers. |
| Audit | Every capability call records tenant, user/agent identity, capability, approval state, result. The *target* audit trail must also be signable for third-party verification (W3.1 provenance, W3.3 proof-of-run) — gated to M4, not implemented, and never at the cost of moving business data off the platform data layer ([`11-aaas-web3-strategy.md`](11-aaas-web3-strategy.md)). |
| MCP security | MCP servers are untrusted external boundaries: registration, permission mapping, secret handling, lifecycle, timeouts, tenant isolation, trust policy. Discovery never grants execution. |
| Sandboxing | Untrusted code (community capabilities, generated agents, tenant apps) never executes with control-plane privilege; sandbox boundaries are launch blockers. |
| Rate limiting & quotas | Per tenant and per capability; abuse of one tenant cannot degrade others. |
| Data ownership | Business data lives in the platform's data layer; orchestration vendors (e.g. a candidate Dify deployment) never hold the source of truth. |
| Isolation of generated apps | Tenant applications run sandboxed and tenant-scoped (the live per-project VFS isolation is the seed). |
| Abuse prevention & retention | Abuse prevention, data retention/deletion policies, RBAC — all launch blockers (see [`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md)). |

## Relationship to live P0 work

The engineering backlog already sequences this foundation as **P0 —
Identity, Security, Tenancy** (`P0.3` control-plane session boundary, `P0.4`
rooms/VFS/deploy ownership, `P0.5` WebSocket authorization, `P0.6` credential
hardening, `P0.7` workflow tenant scope, `P0.8` room lifecycle/quotas and
`P0.9` the identity source-of-truth decision are all
`implemented_unverified` — the code and its unit tests exist, the end-to-end
rollout does not — [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md)). This document
defines the *product contract* those tasks must satisfy: tenancy is a launch
requirement for the Agent Builder (M0), not a later hardening pass.

## Public-launch security gates (blockers)

No public launch without: full tenant isolation tests green; per-call audit
with tenant/user/agent identity; approval gates on external side effects;
credential-vault references (no plaintext in manifests or logs); agent
identity on every run; MCP trust policy enforced; sandbox boundaries for
untrusted code; rate limits and quotas enforced; RBAC; retention/deletion
policies; abuse prevention. See
[`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md).

## Non-goals

- No per-tenant custom databases by default.
- No tenant-level code deployment (configuration and functions, not forks).
- No anonymous cross-tenant capability calls, including internal/admin paths.
