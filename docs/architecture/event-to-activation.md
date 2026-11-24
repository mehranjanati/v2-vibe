# Event-to-Activation — Normative (PROPOSED, ADI-EVT-003)

> **Status:** `proposed` (2026-11-23) — design only. No subscription table, route or code in this change.
> **Consumes:** the delivered envelope ([`event-envelope.md`](event-envelope.md), [`durable-event-delivery.md`](durable-event-delivery.md)). **Drives:** context assembly ([`context-assembly.md`](context-assembly.md)) and activation ([`activation-policy.md`](activation-policy.md)). **Resolves** the composition through the stable pointer ([`candidate-lifecycle.md`](candidate-lifecycle.md)).

## 1. The last mile, and the one thing it must not do

An event has been delivered durably and deduplicated. Now the platform decides whether it **starts a run**, and if so, with what context.

The one thing this layer must never do is **grant anything**. The event supplies *context and a reason to run*; the composition comes from a pointer, the permissions come from the tenant path, and the capabilities come from activation. A design where "the payment event arrived, so the refund tool is now allowed" would undo every invariant the rest of ADI establishes.

```text
envelope → [eligibility filter] → [resolve pointer → snapshot] → context assembly → activation → run
                  (may it run?)        (what may it run?)        (what does it see?)   (what may it use?)
```

## 2. Subscriptions live in the data layer, not in the IR

Something must say "events of this type, for this tenant, start this System". IR v1 has no subscription surface for agentic Systems (only the procedural `trigger` node), and adding one is an IR change with an ADR (D7). So v1 puts the binding in the **data layer**:

| Field | Rule |
|---|---|
| `bindingId` | Stable identity; used in the run de-duplication key (§5) |
| `tenantKey` | The binding and the envelope must agree; mismatch is `E_EVT3_TENANT` |
| `pointerRef` | `(systemRef, environment)` — resolved through the stable pointer, so an event-triggered run uses exactly what production would use |
| `selector` | `category` + `type` (exact or a single trailing wildcard such as `business.lead.*`). No expressions, no scripts |
| `triggerable` | Whether a match **starts a run**. A non-triggerable subscription only annotates (the event may still become context for a run started elsewhere) |
| `inputMode` | `payload` (the event payload is the run input) or `context-only` (the run needs no input from this event) |
| `enabled` | Operators can disable a subscription without deleting its history |

**Why triggerable is a property of the subscription, not of the event.** The same `business.lead.qualified` may legitimately start a follow-up System and also merely annotate another. Keeping the decision on the binding means the event producer never decides which consumers run — a producer that could do that would be enforcing policy.

## 3. The eligibility filter (deterministic, no model)

The filter runs before anything expensive and can only say *yes* or *no*:

| Check | Failure |
|---|---|
| Binding exists, enabled, tenant matches | `E_EVT3_NO_BINDING` / `E_EVT3_TENANT` |
| `selector` matches `category` and `type` | no match → not a candidate (silent, correct) |
| Subscription is `triggerable` | `E_EVT3_NOT_TRIGGERABLE` when a run was requested anyway |
| Causation depth within bound | `E_EVT3_LOOP` (inherited from delivery) |
| Per-binding rate/backpressure bound | `E_EVT3_BACKPRESSURE` |

No model, no ranking, no cost estimate: this stage decides *whether a run should exist*, not *what it may do*.

## 4. What the event may and may not influence

| The event may | The event may not |
|---|---|
| Provide the task input (payload) | Choose or widen the capability set |
| Set `correlationId` so the run is traceable | Set the tenant (it must match the binding and the envelope) |
| Add an `event` segment to the context | Satisfy an approval or bypass `mcp.Gate` |
| Be the reason a run exists | Bypass the snapshot requirement — the pointer supplies it, and with `CONTROL_PLANE_REQUIRE_SNAPSHOT` on the run is refused without one |
| Carry `causationId` for loop detection | Alter the closure, pins or policy |

**Activation still runs.** An event-triggered run is not a shortcut: it assembles context, then activation decides eligibility exactly as for a user-initiated run. The only difference is what started it.

## 5. One event, at most one run per binding

Fan-out is legitimate — two subscriptions may both start runs for the same event — so de-duplication is per binding, not per event:

```text
runKey = hash(bindingId, dedupeKey)      // dedupeKey = tenant|source|eventId
```

| Rule | Why |
|---|---|
| The run row carries a unique `(binding_id, dedupe_key)` | The inbox marks *received*; it cannot mark *started*. A crash between the inbox insert and the run insert would otherwise start the run twice on redelivery |
| The de-duplication is enforced by the database, not by a check-then-insert | Check-then-insert is a race under at-least-once delivery |
| A duplicate start is a **success**: the existing run id is returned | Same rule as the envelope (§3 of `event-envelope.md`) |
| Two different bindings for one event each get their own run | That is fan-out, not duplication |

This is the same shape as the snapshot store's content-addressed dedupe: the database key *is* the guarantee.

## 6. Mapping into context assembly

The envelope becomes segments; assembly's contract does the rest:

| Segment | Source | Notes |
|---|---|---|
| `kind: event` | the envelope metadata | `source` = `envelope.source`, `scope: tenant`, provenance = `source.id`/`source.version` |
| task input | the envelope `payload` when `inputMode: payload` | Treated as **mandatory**: if the payload alone exceeds the budget, assembly refuses (`E_CONTEXT_BUDGET`) rather than running without its input |
| `kind: composition` | the resolved snapshot | Mandatory, as always |
| `kind: candidate` / `kind: knowledge` / `kind: memory` / `kind: profile` | the normal providers | The event changes nothing about how these are obtained |

The payload's size is bounded by the envelope contract (`E_EVENT_PAYLOAD` above the bound without a `ref`); a `ref` payload is fetched by the run, not inlined into the event row.

## 7. Failure modes

| Code | Condition | Behaviour |
|---|---|---|
| `E_EVT3_NO_BINDING` | No enabled subscription matches, but a run was requested | Refuse; the event is still recorded for audit |
| `E_EVT3_TENANT` | Envelope tenant ≠ binding tenant | Refuse; never a cross-tenant run |
| `E_EVT3_NOT_TRIGGERABLE` | A non-triggerable subscription was asked to start a run | Refuse; annotation-only bindings do not run |
| `E_EVT3_SELECTOR` | A selector is invalid (expression, script, unknown category) | Refuse the binding at creation, not per event |
| `E_EVT3_SNAPSHOT` | The pointer resolves to no snapshot, or the snapshot fails `Verify` | Refuse; an event-triggered run is bound like any other |
| `E_EVT3_BACKPRESSURE` | Per-binding rate bound exceeded | Shed with a recorded reason; the outbox row stays `dead` for review |
| `E_EVT3_LOOP` | Causation depth exceeded | Refuse and audit (inherited) |

## 8. What this design does not do

- No subscription syntax in the IR (that would need an ADR); bindings are data.
- No capability selection, no approval, no policy: the event never decides what a run may do.
- No ordering guarantees and no "replay all events to rebuild state" (that is event sourcing, which this platform does not do).
- No cross-tenant subscriptions, no marketplace of event bindings.
- No new table in this task; the binding and the `(binding_id, dedupe_key)` uniqueness are designed, not migrated.

## 9. Tests this design will need (ADI-EVT-003a, when code starts)

1. A matching, enabled, triggerable binding starts exactly one run for one event.
2. The same event redelivered starts no second run (unique `(binding_id, dedupe_key)`), and the existing run id is returned.
3. Two bindings for the same event produce two runs — fan-out is not duplication.
4. A non-triggerable subscription refuses with `E_EVT3_NOT_TRIGGERABLE` and starts nothing.
5. An envelope from another tenant is refused with `E_EVT3_TENANT`.
6. A disabled binding starts nothing and records the decision.
7. The run is bound to the snapshot the pointer resolves to; a pointer with no verifiable snapshot is `E_EVT3_SNAPSHOT`.
8. With `CONTROL_PLANE_REQUIRE_SNAPSHOT` on, an event-triggered run is refused when no snapshot can be resolved — the event does not smuggle one.
9. The assembled context contains an `event` segment with the envelope's provenance and a mandatory payload segment.
10. A payload that alone exceeds the context budget refuses with `E_CONTEXT_BUDGET` instead of running without input.
11. A handler-emitted event that triggers another run carries `causationId`, and depth beyond the bound yields `E_EVT3_LOOP`.
12. A selector containing an expression or script is refused at binding creation (`E_EVT3_SELECTOR`), not silently ignored at delivery.

## 10. Open items

| Item | Why it is open |
|---|---|
| Whether subscriptions eventually belong in the IR | A `spec.subscriptions` block would need an ADR; v1 deliberately avoids it to keep D7 clean |
| Selector language scope | Exact `type` plus one trailing wildcard is enough for v1; anything richer becomes a language with its own review |
| Fan-out bound per event | Two bindings are natural; twenty suggest a design problem, and the limit needs real load data |
| Whether a binding may target a candidate instead of the stable pointer | Today: no — only the pointer, so an event-triggered run uses what production uses. Testing a candidate by event would need an explicit preview mode |
| Where binding state lives | Same O27-adjacent storage decision as snapshots and outbox/inbox |
