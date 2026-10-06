# Multi-tenancy & Security Model (PROPOSED)

> **Scope:** tenancy and security as first-class product concerns. Layer
> context: [`04-system-architecture.md`](04-system-architecture.md).
> Enforcement point for functions:
> [`06-functions-and-tools.md`](06-functions-and-tools.md). Live identity
> work today: engineering P0.3–P0.9 in
> [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md).
>
> **Status:** `proposed` — full multi-tenancy is not implemented; the live
> system has per-user auth (D1-backed) and an opt-in control-plane session
> boundary (P0.3). Last reviewed: 2026-10-06.

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
  prompt/policy inputs, not code forks.
- **Tenant-specific data model where justified** — the default is a shared
  schema with a mandatory tenant column; a separate data model requires an
  explicit justification (constraint: do NOT assume every tenant needs a
  completely custom database).

## Security model

| Concern | Rule |
|---|---|
| Function authorization | Capability + tenant + role checked in the execution path — never only in prompts (unchanged platform principle). |
| Credentials | Stored as references in a vault, never plaintext in functions or agent context. |
| Human approval | External side effects (email, message, payment) require approval or an explicit tenant policy — logged either way. |
| Audit | Every function call records tenant, user/agent identity, function, approval state, result. |
| Rate limiting & quotas | Per tenant and per function; abuse of one tenant cannot degrade others. |
| Data ownership | Business data lives in the platform's data layer; orchestration vendors (e.g. a candidate Dify deployment) never hold the source of truth. |
| Isolation of generated apps | Tenant applications run sandboxed and tenant-scoped (the live per-project VFS isolation is the seed). |

## Relationship to live P0 work

The engineering backlog already sequences this foundation as **P0 —
Identity, Security, Tenancy** (`P0.3` control-plane session boundary is
`implemented_unverified`; `P0.4–P0.9` planned —
[`DEV_CHECKLIST.md`](DEV_CHECKLIST.md)). This document defines the *product
contract* those tasks must satisfy: tenancy is a launch requirement for the
wedge (M1), not a later hardening pass.

## Non-goals

- No per-tenant custom databases by default.
- No tenant-level code deployment (configuration and functions, not forks).
- No anonymous cross-tenant capability calls, including internal/admin paths.
