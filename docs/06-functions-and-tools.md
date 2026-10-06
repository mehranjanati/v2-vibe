# Functions & Tools — Capability Model (PROPOSED)

> **Scope:** the capability layer — reusable business functions that agents
> invoke. Layer context: [`04-system-architecture.md`](04-system-architecture.md).
> Consumer: [`05-agent-runtime.md`](05-agent-runtime.md). Wedge usage:
> [`02-mvp.md`](02-mvp.md). Security/tenancy:
> [`07-multitenancy-security.md`](07-multitenancy-security.md).
>
> **Status:** `proposed` — the business-function registry does not exist yet.
> Live tools today are the App Builder's VFS/REST toolset in the Go control
> plane ([`MULTI_AGENT.md`](MULTI_AGENT.md)) and the 7 workflow node types
> ([`DEV_CHECKLIST.md`](DEV_CHECKLIST.md) P2 registry content). Last
> reviewed: 2026-10-06.

## Concept

A **function** is a reusable business capability with typed input/output
that an agent can invoke: the unit of real work. Agents plan and supervise;
functions act. Business logic lives in functions (versioned, typed,
testable), never inside agent prompts.

## Initial function catalog (wedge scope)

Data access: `db_query`. Integrations: `http_call`, `send_email`,
`send_message`, `invoke_function`. Business objects: `create_customer`,
`update_customer`, `create_order`, `search_products`. Business logic:
`calculate_price`. The catalog grows per roadmap phase — never wholesale.

## Function requirements (every function, no exceptions)

1. **Typed input/output** — a schema the agent plans against and the
   platform validates.
2. **Authorization** — capability-level permission check in the execution
   path.
3. **Tenant context** — every call carries the tenant; no cross-tenant
   access is representable.
4. **Audit log** — who (user/agent), what (function, inputs summary,
   result), when, under which approval.
5. **Rate limiting** — per tenant and per function.
6. **Retry policy** — explicit, per function (idempotent ones may retry;
   external side effects may not silently retry).
7. **Idempotency where needed** — business writes deduplicate by intent key.
8. **Observability** — structured logs/metrics linked to the run lineage.
9. **Side-effect level** — declared metadata: `safe` / `external side
   effect` / `irreversible` — driving human-approval requirements
   ([`05-agent-runtime.md`](05-agent-runtime.md) flow step 9).

## Registry and manifests (proposed shape)

Each function is registered with a manifest: name, version, typed IO schema,
side-effect level, required approvals, tenant-policy hooks, retry/idempotency
policy, owner and provenance. This extends the capability-registry direction
already planned as engineering P1/P2 content (node packages as first registry
entries — [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md)).

## Relationship to today's tools

- The VFS tools (`vfs_read`/`vfs_write`/`vfs_list`) prove typed,
  least-privilege, tenant(room)-scoped tooling today — in the App Builder
  domain.
- The workflow node types (`trigger/http/db/ai/email/condition/sleep`) are
  the first *executed* capability primitives; business functions generalize
  them with typed IO and authorization.
- Function execution technology is open (Go today; Bun-based execution is a
  candidate — see [`04-system-architecture.md`](04-system-architecture.md)
  register; security boundaries are defined in
  [`07-multitenancy-security.md`](07-multitenancy-security.md), not by the
  runtime vendor).

## Non-goals

- No plugin ecosystem in M1 (functions are first-party, curated).
- No functions without manifests, audit and tenant scoping.
- No business data ownership outside the platform's data layer.
