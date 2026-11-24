# Event Envelope — Normative Contract (PROPOSED, ADI-EVT-001)

> **Status:** `proposed` (2026-11-23) — design only. No broker, no table, no transport in this change.
> **Consumed by:** context assembly (`context-assembly.md` §2, the `event` input) and the workflow `trigger` node, which already treats `event.payload` as its execution input. **Mechanism, not semantics:** durable delivery, inbox/outbox, retry and dead-letter are [`durable-event-delivery.md`](durable-event-delivery.md) (ADI-EVT-002) — this document defines identity, shape and duplication *semantics* only.
> **No new infrastructure.** The mission rule holds: no Kafka, no NATS, no Temporal, no new broker without measured need and an ADR. Today there is no event table in `worker/database/schema.ts`; the envelope is a contract, not a deployment.

## 1. Why an envelope exists

A raw payload is a fact with no context: no identity, no owner, no way to tell a retry from a second business event, no way to correlate it with the run it caused. The envelope supplies exactly that context and nothing else.

**The one rule that outranks the rest:** an event is *input*, never *authority*. An event may trigger a run; it may never grant a permission, satisfy an approval, widen a closure or pick a capability. Authorization stays with `mcp.Gate` and the tenant path, and this rule is repeated in `context-assembly.md` §2 for the same reason.

## 2. The envelope

```json
{
  "causationId": "event:9f3a1c",
  "category": "business",
  "correlationId": "lead-42",
  "dedupeKey": "tenant-a|crm|evt-7781",
  "eventId": "evt-7781",
  "eventVersion": "1.0.0",
  "occurredAt": "2026-11-23T09:58:12Z",
  "payload": {"data": {"leadId": "42", "score": 87}, "schema": "event-schema/lead-qualified@1.2.0"},
  "receivedAt": "2026-11-23T10:15:00Z",
  "source": {"id": "crm-sync", "kind": "integration", "version": "3.1.0"},
  "subject": {"id": "42", "kind": "lead"},
  "tenantKey": "tenant-a",
  "type": "business.lead.qualified"
}
```

| Field | Rule |
|---|---|
| `eventVersion` | Semver of the **envelope**. Independent of the payload schema version and of the IR. |
| `eventId` | Stable identity assigned by the **producer**, unchanged across retries and redeliveries. Without it, duplication is undetectable. |
| `dedupeKey` | `tenantKey \| source.id \| eventId`, materialised so a consumer can dedupe with one comparable string. |
| `category` | Closed vocabulary: `user` \| `task` \| `business` \| `schedule` \| `system` \| `package`. |
| `type` | Dotted lowercase name inside its category (`business.lead.qualified`). **Never carries PII**, never carries an id. |
| `tenantKey` | Required and non-empty. An unowned event is rejected at the boundary, not defaulted. |
| `source` | `kind` (`integration` \| `agent` \| `user` \| `schedule` \| `platform` \| `package`), `id`, `version`. Producer provenance, never inferred. |
| `subject` | What the event is about (`kind`, `id`). Distinct from `source` (who emitted it) and from `tenantKey` (who owns it). |
| `occurredAt` | When the producer says it happened. |
| `receivedAt` | When the platform accepted it. **Both are kept**: overwriting `occurredAt` with `receivedAt` would erase lateness, which is often the most important fact about an event. |
| `correlationId` | What a set of events is *about*, shared with the run they may trigger. |
| `causationId` | The event or command that caused this one. Empty for an originating event. |
| `payload` | `schema` (a pinned `event-schema/name@X.Y.Z`) plus `data`, or a `ref` for payloads above the size bound. |

### 2.1 Why the payload is referenced, not described here

This contract deliberately does not define any business payload. A payload is described by a pinned schema ref (`event-schema/lead-qualified@1.2.0`) whose catalog is a separate, versioned artifact. Defining payload shapes here would make this document the second manifest system, which the ecosystem rules forbid.

## 3. Duplication semantics (the acceptance criterion)

1. **Delivery is at-least-once.** Exactly-once is not claimed anywhere in the platform, so no consumer may assume it.
2. **A duplicate is a success, not an error.** A consumer that has seen `dedupeKey` acknowledges and does nothing. Returning an error would make the producer retry forever against a fact it already delivered.
3. **Dedupe is durable, not in-memory.** An in-process map loses history on restart and would re-execute a non-idempotent effect; the durable form is ADI-EVT-002's inbox, and until it exists, a consumer that performs side effects must use the execution contract's idempotency key (`docs/ecosystem/06-runtime-and-execution-architecture.md`).
4. **Ordering is per-correlation and best-effort.** There is no global order and no cross-correlation order. A consumer that needs strict order must sort by (`occurredAt`, `eventId`) within one `correlationId` and must tolerate gaps.
5. **A late event is still valid.** `receivedAt − occurredAt` is recorded, never "corrected". Whether a late event should still trigger a run is a policy decision of the consumer, not of the envelope.
6. **`eventId` reuse is a producer bug.** Two different facts under one `eventId` break dedupe; the platform treats the second as a duplicate and drops it, and that is the correct failure mode for a system that cannot tell them apart.

## 4. Versioning and compatibility

| Change | Version action | Consumer rule |
|---|---|---|
| Add an optional envelope field | `eventVersion` MINOR | A MINOR reader must ignore unknown fields, not fail |
| Change a field's meaning, remove one, or make one required | `eventVersion` MAJOR | A consumer that does not understand the MAJOR **rejects** the event (`E_EVENT_VERSION`) rather than guessing |
| Change the payload shape | payload `schema` ref (its own semver) | Independent of `eventVersion`; an envelope may carry `@1.2.0` of one payload and `@2.0.0` of another |

The two versions move independently on purpose: an envelope rarely changes, and a business payload changes often. Coupling them would force a platform-wide version bump every time a domain field is added.

## 5. Tenancy

- `tenantKey` is required at the boundary. A producer that does not know the tenant does not get to invent one (`E_EVENT_TENANT`).
- Delivery is tenant-scoped by construction: a consumer is handed events for one tenant, and a cross-tenant hand-off is a defect, not a filter.
- The runtime's existing tenant check still applies: the workflow instance records the tenant it was started for, and refuses to execute when the DAG it points at belongs to another tenant (P0.7/P0-SEC-005). The envelope does not replace that check; it supplies the value.

## 6. Events and the run path

```text
producer → envelope → (transport) → consumer → [context assembly] → [activation] → run
```

Three properties of that path are contractual:

1. **The envelope is the boundary object.** Whatever the transport, the consumer sees this shape — which is why `context-assembly.md` §2 can name it as its normalizer.
2. **The `trigger` node consumes `payload`.** Today the workflow runtime already treats `event.payload` as the trigger's execution input; the envelope makes that input typed and attributable instead of a bare blob.
3. **An event does not carry a composition.** A run still needs a snapshot; an event cannot substitute for one, and with `CONTROL_PLANE_REQUIRE_SNAPSHOT` on, an event-triggered run without a snapshot is refused like any other (`run-snapshot.md` §13).
4. **How an event becomes a run is specified in [`event-to-activation.md`](event-to-activation.md)** (ADI-EVT-003): data-layer subscriptions, a deterministic eligibility filter, pointer-resolved snapshots and per-binding de-duplication. This document stops at the envelope.

## 7. Privacy

| Part | Rule |
|---|---|
| Envelope metadata | No PII. `type` describes a class of fact (`business.lead.qualified`), never a person or an id. |
| `subject.id` | An opaque identifier; the meaning lives behind the tenant boundary. |
| `payload` | May legitimately contain personal data. Retention is decided **per payload schema**, not per envelope, so metadata can outlive the data it describes. |
| Redaction | A payload may be redacted after the fact (retention/erasure); the envelope stays, which keeps the audit trail of *that something happened* without keeping *what*. |

## 8. Failure modes

| Code | Condition | Behaviour |
|---|---|---|
| `E_EVENT_SCHEMA` | Envelope or payload violates the contract | Reject at the boundary; never partially accept |
| `E_EVENT_VERSION` | Unknown `eventVersion` MAJOR | Reject; do not guess |
| `E_EVENT_TENANT` | Missing or empty `tenantKey` | Reject |
| `E_EVENT_PAYLOAD` | Payload exceeds the size bound with no `ref`, or names no `schema` | Reject; an untyped payload is not an event |
| `E_EVENT_DUPLICATE` | `dedupeKey` already processed | **Not an error**: acknowledge and skip (see §3) |

## 9. What this contract does not do

- No transport, no broker, no queue, no table. Kafka/NATS/Temporal are explicitly out of scope without measured need and an ADR.
- No delivery mechanism: inbox/outbox, retries, backoff and dead-lettering are ADI-EVT-002.
- No payload catalog: only the reference grammar is fixed here.
- No event sourcing of the platform's own state; runs are recorded in `workflow_instances` and lineage tables, not derived by replaying events.

## 10. Tests this contract will need (ADI-EVT-002 and later)

1. An envelope with every field validates; one missing `tenantKey` is rejected.
2. The same `eventId` delivered twice is acknowledged once and processed once (dedupe by `dedupeKey`).
3. A MINOR envelope version with an unknown extra field is accepted.
4. An unknown MAJOR version is rejected with `E_EVENT_VERSION`.
5. A payload without a `schema` ref is rejected with `E_EVENT_PAYLOAD`.
6. An oversized payload with a `ref` is accepted; the same payload inlined is rejected.
7. `occurredAt` and `receivedAt` are both preserved, and a late event keeps its original `occurredAt`.
8. Two events with the same `correlationId` delivered out of order are ordered by (`occurredAt`, `eventId`) by the consumer, not by the transport.
9. An event-triggered run without a snapshot is refused when binding is required — the envelope does not smuggle a composition.
10. A cross-tenant hand-off is impossible: the consumer only ever receives its tenant's events.

## 11. Open items

| Item | Why it is open |
|---|---|
| Producer-assigned `eventId` vs platform-assigned fallback | A producer that cannot generate ids would need a platform fallback, which weakens dedupe; needs a decision, not a default. |
| Payload schema catalog ownership and its first entries | The ref grammar is fixed; the catalog does not exist yet and belongs with the registry work. |
| Retention windows per payload schema | A privacy decision per domain, not a platform constant. |
| Whether a transport ever needs ordering guarantees | If a real workload requires cross-correlation order, that is a broker decision with its own ADR — and it must not be smuggled into the envelope. |
