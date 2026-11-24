# Memory Provider — Normative Interface (PROPOSED, ADI-MEM-001)

> **Status:** `proposed` (2026-11-23) — design only. No table, no migration, no adapter, no code in this change.
> **Umbrella:** [`context-activation-and-memory.md`](context-activation-and-memory.md) §2 (scopes) and §7 (privacy/retention). **Consumed by:** context assembly (`context-assembly.md`, `kind: memory`) and personalization (ADI-MEM-003). **Historical task it satisfies the design half of:** P7.6 in [`../DEV_CHECKLIST.md`](../DEV_CHECKLIST.md) — "provider-independent abstractions over Redis/D1; no new store decision".

## 1. What exists today (evidence, not assumption)

| Piece | Reality | Consequence |
|---|---|---|
| Redis retrieval | `backend/pkg/engine/vector.go` keeps a RediSearch index (`idx:vfs`) over **VFS files** | It is a *knowledge/document* index, not a memory store |
| Embedding | `HashEmbedding` is a deterministic n-gram hash, not a semantic model | Retrieval today is lexical-ish; the provider must never claim semantic recall |
| D1 lineage | `generations`, `generation_files`, `generation_audits`, `workflow_instances` | Facts about *what happened*, not user memory |
| Memory storage | **None.** No memory table, no memory keyspace | Nothing to migrate; the interface comes first, by necessity |

That last row is the important one: there is no memory store to replace, so this contract cannot break anything — and it must not pretend a store already exists.

## 2. The interface (three operations, no more)

```go
type Scope string // run | conversation | agent | team | tenant

type Provenance struct {
    Source     string // explicit | observed | inferred | tool-result | system
    Producer   string // provider or feature id
    Version    string // producer version, so a recall is explainable
}

type MemoryRecord struct {
    ID         string     // content-addressed or provider-assigned; stable
    TenantKey  string
    Scope      Scope
    Subject    string     // who the memory is about (user id, agent ref, team id, conversation id)
    Kind       string     // fact | preference | summary | event-note
    Key        string     // stable key; REQUIRED when Kind == preference, empty otherwise (see §4)
    Text       string     // model-facing prose (at most one payload form)
    Data       map[string]any // structured facts
    Provenance Provenance
    CreatedAt  time.Time  // injected clock
    ExpiresAt  *time.Time // nil means "until forgotten"
}

type RecallQuery struct {
    TenantKey string
    Scopes    []Scope    // must be within the caller's read authority (§3)
    Subject   string     // optional filter
    Kind      string     // optional filter
    Text      string     // optional query text; empty means "most recent"
    MaxItems  int        // hard bound, requested by the caller
    MaxTokens int        // hard bound on the returned slice
}

type MemoryItem struct {
    MemoryRecord
    Score float64 // provider score; ordering rule in §6
}

type Provider interface {
    Remember(ctx context.Context, record MemoryRecord) (string, error)
    Recall(ctx context.Context, query RecallQuery) ([]MemoryItem, error)
    Forget(ctx context.Context, request ForgetRequest) (int, error)
    Capabilities() Capabilities
}

// Capabilities lets a caller know what it is getting without guessing.
type Capabilities struct {
    SemanticRecall bool     // false for a lexical/hash provider — say so
    SupportsTTL    bool
    MaxItems       int
    MaxTokens      int
}
```

**Why no `Update`:** a correction is `Forget` + `Remember`, which keeps provenance honest (a memory that changed its content is a different memory with a different source). Silently rewriting a record would destroy the ability to answer "what did we believe, and when?". This mirrors the append-only rule the snapshot store already follows.

**Why `Capabilities`:** the honest-degradation rule. A provider that cannot do semantic recall must be able to say so, so assembly can record `degraded` instead of the platform quietly returning worse results (`context-assembly.md` §7).

## 3. Scope authority (the part that must never be wrong)

The five scopes form a containment ladder, narrowest first:

```text
run ⊂ conversation ⊂ agent ⊂ team ⊂ tenant
```

| Operation | Allowed by default | Requires explicit policy |
|---|---|---|
| `Recall` | the run's own scope **and every broader scope on the ladder** | — (tenant is still a hard wall) |
| `Remember` | the run's own scope | writing to a broader scope (`team`, `tenant`) |
| `Forget` | records the run itself created, in its own scope | broader scopes, or another subject |

Three rules that are not negotiable:

1. **Tenant is a wall, not a filter.** A query for tenant B's memory from tenant A is `E_MEMORY_TENANT`, never an empty result — an empty result still tells the caller that no data leaked, which is the right answer only if no read was attempted.
2. **`Scopes` may only narrow the caller's authority.** Passing a scope the caller may not read is a refusal, not a clamp: a provider that silently drops it would hide a caller bug.
3. **No cross-subject reads inside a scope.** A run acting for user X cannot recall user Y's `conversation`-scope memory. Subject is part of the authority check, not a filter.

## 4. Provenance and the `inferred` label

Every record carries `Source`, `Producer`, `Version`. The rule from the personalization contract applies here in its strongest form:

- `explicit` ("the user said this") and `inferred` ("we concluded this") are different sources and **must remain distinguishable after storage**, not only before it.
- An `inferred` record may never be written over an `explicit` one for the same `(scope, subject, key)` — a conflict is surfaced to the caller, which resolves it by precedence (`context-assembly.md` §4.3).
- `tool-result` and `system` records are observations, not preferences; they must not be presented to a model as user intent.

## 5. Secrets are refused, not filtered

A memory record may contain a `vault:` reference and never a literal credential — the same denylist the resolver applies (`composition-resolver.md` §2.2) applies on write. A provider that stores a token would put it in every future context; refusing it at write time (`E_MEMORY_SECRET`) is the only cheap moment to stop it.

## 6. Bounds and deterministic ordering

1. **`MaxItems` and `MaxTokens` are hard bounds.** A query that exceeds the provider's advertised capability is refused (`E_MEMORY_BOUNDS`), not silently clamped — a caller that asks for 100 items and gets 10 without being told will misjudge what it knows.
2. **Ordering is total and recorded:** `Score` descending, then `CreatedAt` descending, then `ID` ascending. Ties never depend on storage order.
3. **Capability honesty beats false parity.** A lexical provider returns `SemanticRecall: false`; assembly then marks the segment set as degraded. The platform would rather say "recall is lexical" than imply semantic memory it does not have — `HashEmbedding` in `backend/pkg/engine/vector.go` is exactly the case that motivates this rule.
4. **Recall is bounded work.** No unbounded scan: the query carries its bounds and the provider is expected to honour them at the index, not after fetching everything.

## 7. Retention, deletion and erasure

| Case | Rule |
|---|---|
| Write with `ExpiresAt` | The provider must stop returning the record after that instant; a provider without TTL support (advertised in `Capabilities`) must refuse an expiring write rather than accept one it cannot honour |
| `Forget` by id | Idempotent; returns 1 if it existed, 0 if not. Returns the **actual** count deleted, never an optimistic one |
| `Forget` by scope/subject | Returns the count; refuses without the authority of §3 |
| Tenant erasure | Deletes tenant-scoped records and reports the count; `run`-scope records go with their runs. Tombstones for audit are a separate, longer-lived record — memory content itself does not survive erasure |
| Cross-tenant delete | Impossible by construction: `TenantKey` is required on every request and checked before any write |

Deletion is where "honest" matters most: a `Forget` that returns a count it did not actually delete turns a privacy guarantee into a claim.

## 8. Failure modes

| Code | Condition | Behaviour |
|---|---|---|
| `E_MEMORY_TENANT` | Tenant mismatch on any operation | Refuse; never an empty result |
| `E_MEMORY_SCOPE` | Requested scope or subject outside the caller's authority | Refuse; never clamp |
| `E_MEMORY_SECRET` | Record contains a literal credential | Refuse the write |
| `E_MEMORY_BOUNDS` | `MaxItems`/`MaxTokens` above the advertised capability, or ≤ 0 | Refuse before querying |
| `E_MEMORY_UNAVAILABLE` | Provider cannot answer | Assembly continues and records `degraded` (memory is an optional provider) |
| `E_MEMORY_RETENTION` | Expiring write against a provider without TTL support | Refuse the write |

## 9. What this contract does not do

- No embedding model, no vector database, no ranking algorithm, no chunking strategy.
- No knowledge/documents (that is the retrieval side of `context-assembly.md`).
- No new mandatory database: the first adapter may be a D1 table and/or Redis keys, and that choice is explicitly **deferred** (§10).
- No change to `worker/database/schema.ts` in this task — there is no memory table today and this design does not create one.
- No cross-tenant sharing, no marketplace of memories, no memory-as-a-service.

## 10. Adapter options (a decision for the implementation task, not this one)

| Option | Fit | Cost |
|---|---|---|
| D1 table `memory_records` (+ index on `(tenant_key, scope, subject, created_at)`) | Durable, queryable, joins with lineage; additive migration like `composition_snapshots` | One migration; no TTL support (needs a sweep or `expires_at` filter) |
| Redis keys per scope, `EXPIRE` for retention | TTL for free, fast hot path | Volatile unless persisted; not queryable |
| Redis vector index (the `idx:vfs` pattern) | Only if semantic recall becomes a real requirement with a real embedding model | Needs a model decision and measured recall — not an MVP dependency |

**Decision taken for the first adapter (ADI-MEM-001a, 2026-11-23): D1 with `expires_at` filtering.** Rationale: durability and one storage story with snapshots (O27-adjacent); Redis TTL is added only if a measured latency problem appears, and the vector index stays a later capability with its own evidence. Revisit trigger: recall p95 above the run budget on a real workload — not a preference.

## 10a. Implementation status — ADI-MEM-001a (2026-11-23)

| Piece | Evidence |
|---|---|
| `backend/pkg/memory` | Types, `Provider` interface, `ValidateRecord`/`ValidateQuery`, the scope ladder (`AllowedScopes`), the total order (`Compare`/`SortItems`), lexical `Score`, `EstimateTokens`, `ContentID`, the secret scan and the closed kind/source vocabularies |
| `backend/pkg/memory/d1store` | D1 adapter over a two-method `Querier`; `INSERT ... ON CONFLICT(id) DO NOTHING` (a retried write is a no-op), bounded recall with a lexical score and the documented order, `Forget` returning the real count, `NewWithoutTTL` so the retention refusal is reachable |
| `memory_records` table | `migrations/0018_adorable_luke_cage.sql` — additive only: one `CREATE TABLE`, three indexes, no `DROP`, no backfill |
| Tests | 11 cases in `d1store_test.go` (M1–M10 plus idempotent write), all green, including the fake-`Querier` boundary so no test touches a database |

**Deliberate limits.** The provider is lexical (`SemanticRecall: false`), has no route and no agent caller (so D8 is untouched), and a `Forget` for another tenant's id reports 0 rather than an error — reporting "exists elsewhere" would confirm existence, which is the leak the tenant wall prevents. Cross-tenant *sharing* of one record remains the open decision in §12.

## 11. Tests this contract will need

1. A write then read round-trips all fields, including provenance and `ExpiresAt`.
2. A cross-tenant recall is refused with `E_MEMORY_TENANT` (not an empty list).
3. A recall asking for a scope outside authority is refused with `E_MEMORY_SCOPE`.
4. An `inferred` write over an existing `explicit` record for the same key is refused or reported as a conflict, never silently applied.
5. A record containing an `apiKey` literal is refused with `E_MEMORY_SECRET`.
6. A recall over the provider's `MaxItems` is refused with `E_MEMORY_BOUNDS`.
7. Ordering is total: equal scores break on `CreatedAt`, then `ID`.
8. `Forget` returns the real count (1 for an existing record, 0 for a missing one), and is idempotent.
9. An expiring write against a `SupportsTTL: false` provider is refused with `E_MEMORY_RETENTION`.
10. A provider advertising `SemanticRecall: false` makes assembly mark the memory segments degraded rather than silently ranking them as if semantic.

## 12. Open items

| Item | Why it is open |
|---|---|
| Storage choice (D1 vs Redis vs both) | §10; should be decided with O27 so the platform picks one storage story, not two. |
| Whether memory records are content-addressed like snapshots | Content addressing would dedupe; memory is mutable-by-nature (forget/remember), so the id scheme needs its own decision. |
| Default retention per scope (`run` vs `tenant`) | A product/privacy decision; needs a data-retention policy, not a default. |
| Whether user-explicit preferences live here or in settings | They are read by personalization either way; storing them twice would create two truths. |
| Semantic recall (embedding model, index) | Requires an evaluation dataset and a measured win before it becomes a dependency. |
