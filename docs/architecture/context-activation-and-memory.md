# Context, Activation, Memory, Personalization (PROPOSED)

> **Status:** `proposed` 2026-11-23, mission VIBE-ARCH-CONSOLIDATION-ADI-001. Awaiting owner acceptance. Companion docs: `adaptive-intelligence-architecture.md`, `composition-lock.md`, `evaluation-and-evolution.md`, `adr-adaptive-intelligence-boundaries.md`. Live base: Redis VFS (`backend/pkg/engine/vector.go`, `vfs:{chatId}`), D1 lineage (`generations`, `generation_files`, `generation_audits`), gateway/router (`backend/pkg/gateway`, `backend/pkg/modelrouter`), mcp.Gate (`backend/pkg/mcp`), execution contract (`backend/pkg/execution`). Scopes: run/conversation/agent/team/tenant (`../ecosystem/05-agent-platform-architecture.md`).

## 1. Five separate concepts

Working Context (assembled per run: task, event, retrieved knowledge, authorized memory slices, explicit profile inputs), Persistent Memory (scoped, timestamped, retained, deletable), Knowledge (retrieved sources, provenance-tagged), Personalization Profile (explicit preferences plus governed inferred preferences), Execution State (durable run rows). Never merged into one store by default; composed at context-construction time through provider interfaces.

## 2. Memory scopes and enforcement

> Authoritative interface: [`memory-provider.md`](memory-provider.md) (ADI-MEM-001). Summary:

Scopes run, conversation, agent, team, tenant, in a containment ladder (`run ⊂ conversation ⊂ agent ⊂ team ⊂ tenant`). A run may **read** its own scope and every broader one (tenant is a hard wall, so a cross-tenant read is `E_MEMORY_TENANT`, never an empty list) and may **write** only its own scope unless policy explicitly allows broader. Three operations only — `Remember`, `Recall`, `Forget`; a correction is forget-plus-remember, so provenance stays honest. Every record carries source (`explicit`/`observed`/`inferred`/`tool-result`/`system`), producer, version, timestamp and expiry; an `inferred` record can never overwrite an `explicit` one. Recall is bounded (`MaxItems`/`MaxTokens`, refused rather than clamped) and ordered totally (score, then created-at, then id). A provider advertises its capabilities, so a lexical backend says `SemanticRecall: false` instead of implying semantic memory.

**Existing Redis vector retrieval is not a memory store** — it is a RediSearch index over VFS files with a deterministic n-gram `HashEmbedding` (`backend/pkg/engine/vector.go`), and there is no memory table in `worker/database/schema.ts`. Provider interfaces come before any new storage; the adapter choice (D1 vs Redis vs both) is deferred and recorded in `memory-provider.md` §10.

## 3. Context construction

> Authoritative contract: [`context-assembly.md`](context-assembly.md) (ADI-CTX-001). Summary of what that document fixes, so the two never disagree:

- **Input:** a normalized task *or* event plus the composition snapshot (`run-snapshot.md`). Assembly runs after the lock and before activation, which is why the envelope carries **candidates** rather than activated capabilities.
- **Output:** one envelope with a `contextId`, the run tenant, the snapshot identity, a sorted `segments[]`, a `budget`, an `omitted[]` list and an optional `degraded` list.
- **Segments** carry `kind` (`task`/`event`/`composition`/`candidate`/`policy`/`knowledge`/`memory`/`profile`), `scope`, `source`, `tokens` and provenance. Knowledge, memory and profile are **structurally** distinguishable — never merged into one blob and never relabelled.
- **Bounds:** 8 candidate capabilities and a 600-token capability slice (the same numbers `backend/pkg/toolsearch` already enforces), plus a policy-provided run budget. Mandatory segments are never truncated; if they alone exceed the budget, assembly refuses.
- **Precedence is preserved, not resolved:** an explicit and an inferred preference both survive with provenance; the activation/policy layer decides the winner and can cite the envelope.

## 4. Activation inputs, outputs, boundary

> Authoritative contract: [`activation-policy.md`](activation-policy.md) (ADI-CTX-002). Summary, so the two cannot disagree:

- **Input:** the context envelope (candidates only ever come from the snapshot closure) plus a pinned activation policy.
- **Two stages:** *eligibility* is deterministic and may only remove (policy rules, grants, reviewed `sideEffect`/`approvals`/`permissions` from `backend/pkg/mcp`, scope, budget); *ranking* is probabilistic and may only order and trim the eligible set. **Final authority is the deterministic set plus `mcp.Gate` at call time** — ranking never decides.
- **The invariant:** `activated ⊆ ranked ⊆ eligible ⊆ candidates(context) ⊆ closure(snapshot)`. A candidate outside the closure is `E_ACTIVATION_ESCALATION` and an audited security event; a policy that allows something outside the closure fails to load.
- **Default-deny:** no matching `allow` means `skipped`, never activated. `deny` wins over `allow` with no ordering rules to misread.
- **Activation may require an approval; it can never record one.** Approvals stay with the existing approval path and the gate, so the platform keeps one approval mechanism.
- Activation still cannot install packages, change pins, issue grants, bypass `mcp.Gate`, or mutate the manifest — those were true before this contract and remain so.

## 5. Decision trace fields

One record per run with one entry per candidate: `runId`, `tenantKey`, `contextId`, `compositionDigest`, pinned `activationPolicyRef`, `decisions[]` (candidate, `activated`/`skipped`/`denied`, `stage`, `ruleId`, `reason`, `approvalRequired`), `ranking` (provider id + version, or `none`) and an injected `decidedAt` excluded from any digest. The exact shape, ordering rules and the rule that every decision must be attributable live in [`activation-policy.md`](activation-policy.md) §5. Persisted with the run lineage; denied/skipped entries name the policy rule or the missing grant.

## 6. Authorization boundaries

Activation selects only already-resolved plus already-authorized capabilities. Grants come from the tenant authorization path (mcp.Gate, tenant-scoped grants, audit); activation never mints them. Events never authorize by themselves (see backlog ADI-EVENTS: versioned envelope with tenant/source/identity/timestamp/correlation; inbox/outbox, duplicate handling, retry/dead-letter; events feed context, not authority).

## 7. Privacy and retention

Inferred preferences never silently overwrite explicit user settings — authoritative contract: [`personalization-profile.md`](personalization-profile.md) (ADI-MEM-003). The precedence is **three namespaces rather than one ladder** (a single ladder was wrong in one place: an organisation's default tone must not beat a user's explicit choice, while an organisation's constraint must):

- `constraint.*` — platform/tenant only; highest layer wins outright; a user write is refused;
- `default.*` — tenant/team/agent defaults; an explicit user entry beats them;
- `preference.*` — explicit user beats everything below and every inferred value; inference only fills a key with no explicit entry, requires an evidence reference, and is refused (`E_PROFILE_CONFLICT`) rather than applied when an explicit value exists.

A profile is **not authorization**: it can never widen permissions, relax an approval class or select a capability outside the closure. Tenant data never crosses tenants. Retention/deletion per scope; erasure requests preserve tombstones where audit requires them (see open item on GDPR vs tombstones). Provider interfaces first; no Kafka/NATS/Temporal, no new vector DB, no new broker without measured need plus ADR (evaluate D1/Redis/execution infra first).
