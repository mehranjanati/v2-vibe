# Context Assembly — Normative Contract (PROPOSED, ADI-CTX-001)

> **Status:** `proposed` (2026-11-23) — design only. No provider, store, route or code in this change.
> **Umbrella:** [`context-activation-and-memory.md`](context-activation-and-memory.md) (the five concepts, memory scopes, privacy). **Consumes:** the composition snapshot ([`run-snapshot.md`](run-snapshot.md)) — candidates come from the closure, never from a live registry. **Feeds:** activation ([`context-activation-and-memory.md`](context-activation-and-memory.md) §4, ADI-CTX-002).
> **Precedents reused, not re-invented:** hard candidate/token bounds from `backend/pkg/toolsearch` (8 candidates, 600 estimated tokens, honest `Missed`); canonical serialization from `backend/pkg/ir`; injected clock and pure-function shape from `backend/pkg/composition`.

## 1. What assembly is, and what it is not

Context assembly is a **pure function** from *(normalized task or event, composition snapshot, provider results, budget, clock)* to a **context envelope**. It selects, labels and bounds. It does not decide what runs.

| Assembly does | Assembly does not |
|---|---|
| Normalize a task/event into a typed input | Parse raw platform events (that is ADI-EVT-001) |
| Attach provenance, scope and kind to every segment | Judge relevance beyond the rank a provider reports |
| Enforce tenant scope and the token/candidate budgets | Decide which capabilities activate (ADI-CTX-002) |
| Record what was omitted and why | Persist memory or knowledge (ADI-MEM-*) |
| Produce one envelope with a stable identity | Call a model or execute a tool |

Assembly runs **after** the composition lock and **before** activation, which is what makes the order meaningful: the envelope contains *candidates* from the snapshot, and activation selects a subset with a trace that cites the context id.

## 2. The normalized input

Two shapes, one interface. Both are already normalized before assembly sees them; assembly never parses raw transport events. The event shape **is** [`event-envelope.md`](event-envelope.md) (ADI-EVT-001): `eventId`, `category`, `source`, `occurredAt`/`receivedAt`, `correlationId`, `dedupeKey` and a schema-referenced `payload`. This document does not restate those fields — it consumes them.

| Field | Task input | Event input | Rule |
|---|---|---|---|
| `kind` | `task` | `event` | Exactly one of the two. |
| `id` | caller-supplied task id | event envelope id | Immutable; reused in the trace. |
| `intent` | free text, bounded | derived from the event type | Optional for events. |
| `payload` | typed object | typed object | Bounded by policy; never a credential. |
| `source` | `api` \| `ui` \| `schedule` \| `delegation` | `user` \| `task` \| `business` \| `schedule` \| `system` \| `package` | Recorded verbatim; never inferred. |
| `correlationId` | optional | required | What a run is *about*, across steps. |
| `receivedAt` | injected clock | injected clock | No `time.Now()` inside assembly. |

An event is **input, never authority**: carrying an event into context grants nothing. Grants come from the tenant authorization path (`mcp.Gate`), and a segment that says "a payment event arrived" cannot by itself authorize a refund.

## 3. The envelope

```json
{
  "budget": {"candidateLimit": 8, "estimatedTokens": 704, "limitTokens": 4000},
  "contextId": "ctx:sha256:<hex>",
  "inputs": {"correlationId": "lead-42", "id": "task-1", "kind": "task", "source": "api"},
  "omitted": [{"kind": "knowledge", "reason": "budget", "ref": "knowledge/icp-docs@5.0.0"}],
  "segments": [ {"kind": "task", "scope": "run", "source": "api", "text": "Qualify lead 42", "tokens": 6} ],
  "snapshot": {"snapshotId": "sha256:<hex>", "systemRef": "system/m1-qualify-then-draft@1.0.0"},
  "tenantKey": "tenant-a",
  "version": "1.0.0"
}
```

### 3.1 Envelope fields

| Field | Rule |
|---|---|
| `version` | Envelope format version. Not the IR version, not the lock version. |
| `contextId` | `ctx:` + digest of the canonical envelope minus its own id and clock — so an identical assembly is recognisable. |
| `inputs` | The normalized input of §2, echoed verbatim. |
| `snapshot` | `snapshotId` + `systemRef` of the composition this context was assembled for. Assembly **refuses** an unknown snapshot rather than assembling against nothing. |
| `tenantKey` | The run's tenant. Every segment must match it. |
| `segments` | Sorted, typed, provenance-carrying (§4). |
| `budget` | `candidateLimit`, `estimatedTokens` (what the envelope costs) and `limitTokens` (what policy allows). |
| `omitted` | Every dropped candidate with `kind`, `ref`, `reason` (`budget` \| `bound` \| `rank` \| `scope`). |
| `degraded` | Optional; set when an optional provider was unavailable, naming it. |

### 3.2 Canonical form

The envelope is serialized with the repository's canonical rules (sorted keys, 2-space indent, one trailing newline, no duplicate keys) so `contextId` is stable. The clock is excluded from the digest, exactly as `generatedAt` is excluded from the lock digest.

## 4. Segments: kind, scope, provenance

Every segment is one object. The six fields below are mandatory on every kind, which is what makes "knowledge vs memory vs profile" a **structural** distinction rather than a convention.

| Field | Rule |
|---|---|
| `kind` | Closed vocabulary, §4.1. |
| `scope` | Closed vocabulary, §4.2. |
| `source` | Who produced it: a provider id plus version, or a ref. Never blank. |
| `tokens` | Estimated cost, always present (a missing count is a defect, not an approximation). |
| `text` / `data` | Exactly one payload form; `text` for model-facing prose, `data` for structured facts. |
| `rank` | Optional relevance rank from the provider; absent means "not ranked", never "rank 0". |

### 4.1 Kind (closed vocabulary)

| kind | What it is | Producer | Truth source |
|---|---|---|---|
| `task` | The normalized input itself | caller | §2 |
| `event` | The normalized event, when the input is an event | caller | §2 |
| `composition` | The snapshot's identity and closure metadata (systemRef, snapshotId, pinned refs) | snapshot store | `run-snapshot.md` |
| `candidate` | A capability the closure allows, with its estimated cost | snapshot + tool catalog | `backend/pkg/composition`, `backend/pkg/toolsearch` |
| `policy` | Constraints that limit what may run (tenant policy, agent policies) | policy store | manifest `spec.policies` |
| `knowledge` | Retrieved source material with citations | knowledge provider | retrieved documents |
| `memory` | Recalled prior interaction, scoped and dated | memory provider | memory store |
| `profile` | Explicit or inferred preference | personalization store | user/team settings |

**Distinguishability rule (normative):** `knowledge` and `memory` are never merged, never share a kind, and a retrieval hit may not be relabelled as memory (or vice versa). A consumer must be able to answer "is this something the user told us, something we recalled, or something we looked up?" without inspecting prose. This is the acceptance criterion, expressed as a type.

### 4.2 Scope (closed vocabulary)

`run` · `conversation` · `agent` · `team` · `tenant` · `user` · `system`.

The scope names **who could see the segment if it were stored**. It is not a permission grant; it is a recording of the boundary the provider enforced. Every segment additionally carries the run's `tenantKey` implicitly through the envelope, and assembly refuses any segment whose tenant differs (`E_CONTEXT_TENANT`).

### 4.3 Precedence is preserved, not resolved

When an explicit user preference and an inferred one both arrive, assembly keeps **both** segments with their provenance and scope. It does not pick a winner: precedence (platform constraints > tenant policy > team/agent config > explicit user > inferred) is applied by the activation/policy layer, which can then cite the envelope. Silently dropping the lower-precedence segment would destroy the evidence that a conflict existed.

## 5. Bounds (the acceptance criterion in numbers)

| Bound | Default | Enforced by | On overflow |
|---|---|---|---|
| Candidate capabilities | **8** | assembly | further candidates go to `omitted` with reason `bound` |
| Capability slice cost | **600** estimated tokens | assembly | lowest-ranked candidates are omitted with reason `budget` |
| Run context budget | policy-provided (`limitTokens`), reserved against the model's context window minus output reservation | assembly | drop in the order of §5.1 |
| Per-segment size | provider-bounded | provider + assembly check | a single oversized optional segment is omitted whole, never cut mid-sentence |

These numbers are not new policy: 8 and 600 are the bounds `backend/pkg/toolsearch` already enforces for tool cards, reused so the platform has one answer to "how big is a capability slice".

### 5.1 Drop order (deterministic)

Mandatory segments — `task`/`event`, `composition`, `policy` — are **never** dropped or truncated. If the mandatory set alone exceeds `limitTokens`, assembly **refuses** with `E_CONTEXT_BUDGET`: a run that cannot be told its own policy must not start.

Optional segments are dropped in this order until the budget fits:

1. `profile` segments with `inferred` provenance (never explicit ones);
2. `memory` segments, oldest first;
3. `knowledge` segments, lowest `rank` first, then largest first;
4. `candidate` segments beyond the candidate bound or the slice cost.

Every drop is recorded in `omitted` with `kind`, `ref` and `reason`. A silent truncation is a contract violation, not an optimisation.

## 6. Determinism

1. **Given the same segments, the same envelope.** Segments are sorted by (`kind`, `scope`, `source`, then ref/id); no map iteration reaches the output.
2. **The clock is injected.** `receivedAt` and any timestamp come from the caller, so a replay with a fixed clock is byte-identical.
3. **Given the same *stores*, the same segments.** Retrieval ranking is a provider property: the envelope records the provider id and version plus each `rank`, so a replay is **explainable** even when the store has moved on. Claiming bit-identical replay across a changing retrieval index would be a lie; recording provenance is the honest equivalent.
4. **`contextId` excludes the clock** so re-assembling at a different second yields the same id — the same rule that keeps a lock digest stable across `generatedAt`.

## 7. Failure modes

| Code | Condition | Behaviour |
|---|---|---|
| `E_CONTEXT_SNAPSHOT` | The snapshot id is unknown or its digest chain fails. | Refuse; assembly never runs against an unverified composition. |
| `E_CONTEXT_TENANT` | A segment's tenant is not the run's tenant. | Refuse; never a silent filter (a filtered read still leaked existence). |
| `E_CONTEXT_SCOPE` | A provider returned a segment outside the requested scope. | Refuse; the provider bug must not become runtime behaviour. |
| `E_CONTEXT_BUDGET` | Mandatory segments alone exceed the budget. | Refuse before execution. |
| `E_CONTEXT_PROVIDER` | A mandatory provider is unavailable. | Refuse. |
| `E_CONTEXT_PROVIDER` (degraded) | An **optional** provider is unavailable. | Continue, list it under `degraded`, and omit its segments; the run is explicitly marked incomplete rather than silently thinner. |

## 8. What this contract deliberately excludes

No new database, no vector index choice, no embedding model, no ranking algorithm, no prompt template. Assembly composes what providers return; the first provider implementations are the existing Redis VFS retrieval and the D1 lineage rows, and nothing here requires a new store. Token counting is an **estimate** with a recorded method (`tokens` per segment), not a tokenizer dependency: a tokenizer would pin a provider, and the bound exists to protect a context window, not to be exact.

## 9. Tests this contract will need (ADI-CTX-003a, when code starts)

1. An envelope with one segment of each kind validates against the field table, and `contextId` is stable across two assemblies with the same clock.
2. A tenant-mismatched segment is refused with `E_CONTEXT_TENANT`, not filtered.
3. Nine candidates yield eight segments plus one `omitted` entry with reason `bound`.
4. A 900-token capability slice drops the lowest-ranked candidates and records `budget` for each.
5. Mandatory segments that alone exceed the budget refuse with `E_CONTEXT_BUDGET`.
6. An unavailable optional provider produces `degraded` plus `omitted`, and the envelope still validates.
7. An explicit and an inferred preference for the same key both survive, with provenance, in a defined order.
8. A knowledge hit and a memory row with identical text remain distinguishable by `kind` and `source`.

## 10. Open items

| Item | Why it is open |
|---|---|
| Token estimate method (characters/4 vs a provider tokenizer) | Needs a measured comparison; the bound only needs to be conservative and recorded. |
| Whether `profile` is a store or a projection over settings | Depends on O27-adjacent storage decisions; the contract only fixes the segment shape. |
| Default `limitTokens` per product | Policy decision per product (Assistant vs Coder have different windows); stays open. |
| Whether `composition` segments may be omitted for tiny runs | Tempting for cost, dangerous for provenance: today they are mandatory. |
