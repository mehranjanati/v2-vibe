# Durable Event Delivery — Design (PROPOSED, ADI-EVT-002)

> **Status:** `proposed` (2026-11-23) — design only. No table, dispatcher, queue or code in this change.
> **Semantics it must keep:** [`event-envelope.md`](event-envelope.md) (ADI-EVT-001) — at-least-once, duplicate-is-success, per-correlation best-effort ordering, events never authorize.
> **No broker.** Kafka/NATS/Temporal stay out: the design composes what exists (D1 + the control plane + Cloudflare Workflows) and names the volume threshold that would justify an ADR.

## 1. The problem, in one sentence

An in-process handler loses events on restart, and a fire-and-forget write loses the event when the state change rolls back — so the platform needs **one durable row for the event, written in the same transaction as the fact it describes**, and **one durable record of what was already handled**.

## 2. The constraint that shapes the design

The repository already documents the storage limits this design has to live inside (`../CF_LIMITS.md` §8, source `[S7]`):

| Constraint | Value | Consequence here |
|---|---|---|
| D1 concurrency | **single-threaded per database** | A conditional `UPDATE ... WHERE` is a safe claim primitive — but throughput is bounded, so the dispatcher must batch and back off |
| Row size | 2 MB | An envelope must stay far below it; a large payload is a `ref` (`event-envelope.md` §2.1) |
| SQL statement length | 100 KB; 100 bind params | Envelope stored as **one bind parameter**, never interpolated |
| Query duration | 30 s | The dispatcher claims small batches, never "everything pending" |
| Database size | **500 MB on the current Free plan**, shared with auth/app/`workflow_*` tables | **Outbox rows must be swept.** An unswept outbox is a slow-motion outage |

**Capacity arithmetic, so the sweep is not optional.** At ~1 KB per envelope, 10 000 events/day is ~10 MB/day: the shared 500 MB budget is exhausted in roughly seven weeks with no ambient growth elsewhere. Retention is therefore a contract rule (§9), not an optimisation.

## 3. Producer: the transactional outbox

```text
BEGIN
  <the state change that causes the event>          -- e.g. a candidate promoted
  INSERT INTO event_outbox (...)                     -- the envelope, same transaction
COMMIT
```

| Rule | Why |
|---|---|
| The outbox row is written **in the same transaction** as the state change | "State changed, event lost" and "event emitted, state rolled back" both disappear |
| The row stores the **canonical envelope** (`event-envelope.md`) | One shape everywhere; the dispatcher never rebuilds an event from domain rows |
| The row carries `dedupeKey` (unique per tenant) | Duplicate detection starts at the producer, not at the consumer |
| Nothing is dispatched inside the producing transaction | Delivery is a side effect and must not be able to roll the fact back |

Table shape (design): `id`, `tenant_key`, `dedupe_key`, `envelope` (JSON text), `status` (`pending`/`dispatched`/`dead`), `attempts`, `next_attempt_at`, `claimed_at`, `lease_expires_at`, `created_at`, `dispatched_at`; indexes on `(status, next_attempt_at)` and `(tenant_key, status)`.

## 4. Consumer: the durable inbox

```text
INSERT INTO event_inbox (tenant_key, dedupe_key, received_at, status)
  ON CONFLICT (tenant_key, dedupe_key) DO NOTHING;
-- if the insert changed no row: this event was already handled -> acknowledge, do nothing
```

| Rule | Why |
|---|---|
| **Unique `(tenant_key, dedupe_key)`** is the dedupe | The database is the memory; an in-process map dies with the process |
| The inbox row is written **before** the effect | A crash after the effect but before the mark would replay the effect; the ordering here makes the replay a no-op instead |
| `status` moves `received → done` / `failed` | A `failed` row is visible evidence, not a silent retry loop |
| Inbox retention **must exceed the maximum redelivery window** | An inbox row deleted while the outbox can still retry turns a duplicate into a second execution (§9) |

For consumers that perform an external side effect, the inbox alone is not enough: the effect must also carry the execution contract's idempotency key (`docs/ecosystem/06-runtime-and-execution-architecture.md`), because a crash between effect and commit is still possible.

## 5. Dispatcher: claim, deliver, settle

```text
loop:
  rows = UPDATE event_outbox
           SET claimed_at = now, lease_expires_at = now + lease,
               attempts = attempts + 1, status = 'dispatching'
         WHERE id IN (SELECT id FROM event_outbox
                       WHERE status IN ('pending','dispatching')
                         AND next_attempt_at <= now
                         AND lease_expires_at < now
                       ORDER BY next_attempt_at, id
                       LIMIT :batch)
     RETURNING *
  for each row: deliver(envelope)
     success -> status = 'dispatched', dispatched_at = now
     failure -> attempts >= max ? status = 'dead' : schedule backoff
```

| Property | Rule |
|---|---|
| **Claim** | The conditional `UPDATE` is the lock. D1 is single-threaded, so two dispatchers cannot both win the same row |
| **Lease** | `lease_expires_at` recovers a crashed dispatcher: the row becomes claimable again, so a lost lease costs latency, not the event |
| **Batch bound** | Small fixed batches (a handful of rows) keep every statement far inside the 30 s query limit and the 100-parameter cap |
| **Backoff** | Exponential with jitter, capped; the schedule is recorded per row so a retry is explainable |
| **Max attempts** | After the stated attempts the row is `dead`, not deleted — a dead-letter row is evidence |
| **Fairness** | The claim orders by `next_attempt_at` first; when one tenant has a deep backlog the claim query must round-robin by `tenant_key` rather than starve the rest (open item §12) |
| **Delivery** | In-process handler call. A consumer that needs durability beyond the process starts a **Workflow instance** (below) instead of doing the work inline |

**Duplicates are expected, not exceptional.** A crash between `deliver` and the `dispatched` mark causes redelivery by design. That is the price of at-least-once, and the inbox is what makes the price acceptable.

## 6. Where the consumer runs (`step` stays the only durability primitive)

| Consumer kind | Where it runs | Notes |
|---|---|---|
| Cheap, idempotent projection (counters, audit mirrors) | Inline in the dispatcher | The inbox row makes it safe |
| Anything that fires a run | A Cloudflare Workflow instance | The instance is started by the dispatcher and **is snapshot-bound**; the event carries no composition (`event-envelope.md` §6) |
| Anything long-running or stepped | A Workflow, one `step` per durable unit | Aligns with the execution contract: work outside a step may be re-executed, and the handler must be written accordingly |

The dispatcher itself is **not** a run: it has no cancel/pause semantics, no run state and no history surface. Trying to model it as one would mean inventing a seventh run state for a component that only moves rows.

## 7. Loop protection

An event handler that emits events can amplify itself into a storm — the classic failure in every event system.

| Rule | Detail |
|---|---|
| Every handler-emitted event carries `causationId` of the event it handled | Chain reconstruction and detection both depend on it (`event-envelope.md` §2) |
| A **maximum causation depth** is enforced at consumption | Exceeding it refuses the event with `E_EVT_LOOP` and writes an audit row rather than continuing quietly |
| Per-correlation rate bound | A single `correlationId` may not enqueue unbounded events inside a window; the excess is shed with `E_EVT_BACKPRESSURE` |
| A tenant's backlog cannot starve others | §5 fairness; a shed event is recorded, never silently dropped |

## 8. Failure modes

| Code | Condition | Behaviour |
|---|---|---|
| `E_EVT_LEASE` | A row was claimed but the lease expired before settlement | Redelivery; the inbox makes the repeat harmless |
| `E_EVT_DUPLICATE` | `(tenant, dedupeKey)` already in the inbox | **Not an error**: acknowledge (`event-envelope.md` §3) |
| `E_EVT_DLQ` | Max attempts reached | Row becomes `dead`; visible, replayable only by an explicit operator action |
| `E_EVT_LOOP` | Causation depth exceeded | Refuse the event, audit it, do not enqueue its output |
| `E_EVT_BACKPRESSURE` | Per-correlation or per-tenant bound exceeded | Shed with a recorded reason; the producer's outbox row stays `dead` for review |
| `E_EVT_TENANT` | A dispatcher or inbox write crosses tenants | Refuse |

## 9. Retention (a correctness rule, not housekeeping)

| Table | Retention | Why |
|---|---|---|
| `event_outbox` `dispatched` rows | Swept after a short replay window (shipped value: days, not weeks) | §2: the shared 500 MB budget cannot absorb an ever-growing outbox |
| `event_outbox` `dead` rows | Kept until replayed or explicitly cleared | A dead letter is evidence; deleting it silently deletes the incident |
| `event_inbox` rows | Kept **longer than the maximum redelivery window** | Deleting a dedupe row while a retry can still arrive converts a duplicate into a second execution — the one failure this whole design exists to prevent |

The asymmetry is deliberate: the outbox may forget, the inbox must not forget too early.

## 10. What this design does not do

- No broker, no queue service, no new database (§2 shows what exists is enough to start).
- No exactly-once claim; no ordering guarantee.
- No event sourcing: platform state lives in its tables, not as a replay of events.
- No automatic DLQ replay; a replay is an operator decision with an audit row.
- No dispatcher-as-run: the execution provider contract stays about runs.

## 11. Tests this design will need (ADI-EVT-002a, when code starts)

1. A state change and its outbox row commit or roll back together (forced failure in the second write).
2. A redelivered event is handled once: the inbox insert is a no-op and the effect count stays one.
3. Two concurrent dispatchers never deliver the same row twice (conditional-UPDATE claim).
4. A crashed dispatcher's lease expiry makes the row claimable, and the retry succeeds.
5. Attempts beyond the maximum move the row to `dead` and stop retrying.
6. Backoff is monotonic per row and recorded on the row.
7. A handler-emitted event carries the `causationId` of its trigger.
8. Causation depth beyond the bound refuses with `E_EVT_LOOP` and enqueues nothing.
9. An event-triggered run without a snapshot is refused when binding is required (the dispatcher does not smuggle a composition).
10. Sweeping `dispatched` rows leaves `dead` rows and inbox dedupe rows intact.
11. An inbox row deleted while a retry is pending reproduces a second execution in the test (proving why §9 forbids it).
12. A tenant with a deep backlog does not prevent another tenant's event from being delivered inside the bound.

## 12. Open items

| Item | Why it is open |
|---|---|
| Trigger for adopting Cloudflare Queues (or another transport) | Needs a measured volume: the honest threshold is "the dispatcher's batch loop cannot drain the outbox within the backoff budget". Adopting one is a broker decision with an ADR, not a refactor |
| Claim fairness algorithm (round-robin vs weighted per tenant) | §5 states the requirement; the algorithm needs a real multi-tenant load shape |
| Whether the dispatcher lives in the Worker or the Go plane | D1 is reachable from both; the Go plane already owns the run path, the Worker owns D1 auth data — this is a placement decision, not a contract |
| Whether `event_outbox` and `event_inbox` live beside `composition_snapshots` or in a separate database | Same O27-adjacent storage decision |
| Replay tooling and its permission model | A DLQ replay is powerful; it needs an owner and an audit shape before it ships |
