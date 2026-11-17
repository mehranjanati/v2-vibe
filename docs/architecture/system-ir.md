# Canonical System IR v1 (P7.2)

> **Scope:** the intermediate representation every creation surface compiles to
> and every runtime executes: what v1 must represent, its invariants, how it is
> versioned and diffed, what it deliberately omits, and the four-case test that
> proves it is expressive enough to start. Implements checklist task **P7.2**
> ([`../DEV_CHECKLIST.md`](../DEV_CHECKLIST.md)); closes the design half of
> open decision **O9** ([`../10-decisions-and-open-questions.md`](../10-decisions-and-open-questions.md)).
> Decision record: [`adr-system-ir-v1.md`](adr-system-ir-v1.md).
>
> **Status:** `implemented_unverified` — the contract, four fixtures and the
> `system-ir` gate are in place and run in `bun run docs:check`; no compiler or
> runtime consumes the IR yet (P7.3/P7.4/P7.5). Last reviewed: 2026-11-16.
>
> **Bounded by:** [`../12-agentic-platform-strategy.md`](../12-agentic-platform-strategy.md)
> (IR requirements), [`manifest-schemas.md`](manifest-schemas.md) (manifest
> envelope and semver), [`../06-functions-and-tools.md`](../06-functions-and-tools.md)
> (System Manifest = serializable IR v1).

## Contents

| Section | Anchor |
|---|---|
| One representation, no parallel format | [`#one-representation`](#one-representation) |
| The pipeline | [`#pipeline`](#pipeline) |
| What v1 represents | [`#represents`](#represents) |
| Two execution shapes | [`#shapes`](#shapes) |
| Invariants | [`#invariants`](#invariants) |
| Desired state vs runtime state | [`#desired-vs-runtime`](#desired-vs-runtime) |
| Versioning, compatibility, migration | [`#versioning`](#versioning) |
| Diff and determinism | [`#diff`](#diff) |
| Four cases (the expressiveness test) | [`#four-cases`](#four-cases) |
| Rejected for v1 | [`#rejected`](#rejected) |
| Validation gate | [`#gate`](#gate) |
| Open items | [`#open-items`](#open-items) |

<a id="one-representation"></a>
## One representation, no parallel format

IR v1 **is** the `kind: System` manifest `spec` — the same envelope, semver,
canonical serialization and validation rules as the other three manifests. It
is not a second file format: "System Manifest" (authoring/registry view) and
"Canonical System IR" (compiler/runtime view) are the same bytes.

Consequence: the Visual Builder, the Natural Language Studio, the SDK/CLI and
hand-written manifests all converge here by construction, which is why the
platform cannot develop two divergent agent models (decision D7).

```mermaid
flowchart LR
    NL["Natural Language Studio"]
    VB["Visual Agent Builder"]
    SDK["SDK / CLI / API / manifests"]
    IR["Canonical System IR v1<br/>= kind: System spec"]
    COMP["Compiler + validation<br/>(P7.2 schema + IR invariants)"]
    RT["Runtime adapter (P7.5)"]
    NL --> IR
    VB --> IR
    SDK --> IR
    IR --> COMP
    COMP --> RT
```

<a id="pipeline"></a>
## The pipeline

User intent → intent parsing → requirement clarification → system planning →
**system compiler** → Canonical System IR → schema validation → policy
validation → capability resolution → deployment planning → agent runtime →
execution → observability → evaluation → versioning.

v1 owns the middle of that pipeline. Everything before it (intent extraction,
clarification) and after it (resolution against a live registry, deployment)
is other tasks; the IR gate only proves the representation is sound.

<a id="represents"></a>
## What v1 represents

Required identity: `apiVersion`, `kind: System`, `metadata.name`,
`metadata.version` (semver), `metadata.publisher`, `metadata.license`.

Required `spec` core:

| Field | Meaning |
|---|---|
| `outcome` | The stated result, in the user's terms — the only free-text purpose field. |
| `agents` **or** `workflow` | The execution shape (exactly one; see below). |
| `topology` | Ordered run steps referencing declared agent names (`run`, `after`, `when`, `on`). |

Optional `spec` fields: `routing` (`parallel`, `onError`), `approvals`
(`on` + typed `approver`), `deployment` (`environment`, `replicas`),
`evaluation` (`dataset` ref + `thresholds`).

References are content-addressed and pinned: `agent/name@X.Y.Z`,
`tool/…`, `skill/…`, plus the external namespaces `model-policy/…`,
`knowledge/…`, `eval/…`. Model choice is a policy ref, never a provider name.

<a id="shapes"></a>
## Two execution shapes

v1 accepts exactly one shape per system, and the schema enforces it
(`spec.oneOf`):

1. **Agentic** — `agents` (one or more, with optional `delegatesTo`) plus
   `topology`. Covers M0 single agents, M1 teams, and the live App Builder
   reference workload.
2. **Procedural** — `workflow` with typed nodes and edges. Covers workloads
   that are pipes rather than collaborators; the node `kind` vocabulary is the
   seven live interpreter node types (`trigger`, `http`, `db`, `ai`, `email`,
   `condition`, `sleep`), so nothing here is speculative.

Both shapes are fully validated and neither is privileged: an agent system is
not "workflow with AI nodes", and a workflow is not a degenerate team. What
they share is identity, outcome, policies, approvals, deployment, evaluation
and refs — which is precisely what makes one IR useful.

A hybrid (agents *and* workflow in one system) is **rejected in v1**. If a real
workload needs it, that is an ADR-level change with evidence, not a silent

<a id="invariants"></a>
## Invariants

JSON Schema covers structure; these invariants cover meaning, and the gate
(`scripts/validate-system-ir.mjs`) enforces them:

| # | Invariant | Why |
|---|---|---|
| I1 | Exactly one execution shape (`agents` xor `workflow`). | Prevents the "every workload is a graph" drift and keeps the compiler honest. |
| I2 | No runtime state anywhere (no `status`, `state`, `health`, `lastRun`, `runId`, `metrics`, `telemetry`, `logs`). | The IR is desired state; runs, cost and health belong to the data layer and the trace (P7.7). |
| I3 | Agent refs are pinned `agent/name@X.Y.Z`; a key is a slug; delegation targets exist and are not self. | Floats and typos must fail at compile, not at deploy. |
| I4 | Every `topology.run` / `.after` names a declared agent; one step per agent in v1. | A step that runs nothing is a lie the runtime would have to interpret. |
| I5 | Workflow node ids are unique; `kind` is one of the seven; `next`/`entry` resolve; every node reachable from the entry set. | Dangling edges are the classic silent-failure source in DAG execution. |
| I6 | `approvals[].approver` is `role:`/`user:`/`group:`. | Approval must name an accountable principal, not a free string. |

<a id="desired-vs-runtime"></a>
## Desired state vs runtime state

The IR declares **what should exist**. It never carries what happened:

| Lives in the IR (desired) | Lives outside the IR (runtime) |
|---|---|
| Outcome, agents/teams, topology, workflow | Run ids, attempts, retries taken |
| Policies, approvals, limits | Approval decisions, approver identity at decision time |
| Deployment target + replicas | Actual replicas, health, rollout state |
| Evaluation dataset + thresholds | Scores, pass/fail, human feedback |
| Pinned refs to capabilities | Resolved closure at run time, secret handles |

This split is what makes an IR file reviewable in a pull request and safe to
export: nothing in it goes stale, and a diff never mixes intent with
telemetry.

<a id="versioning"></a>
## Versioning, compatibility, migration

1. **Envelope version** — `apiVersion: vibesdk/v1`. Changing the envelope is a
   platform event requiring an ADR.
2. **IR version is the envelope version**: there is no separate `irVersion`,
   because a second version counter would immediately disagree with the first.
3. **Semver of a system** — `MAJOR` changes structure or meaning; `MINOR` adds
   optional fields; `PATCH` changes text only. See
   [`manifest-schemas.md`](manifest-schemas.md).
4. **Backward compatibility** — the compiler must accept any `v1` document that
   validated when it was published; adding a required field is a `MAJOR` event,
   not a schema edit.
5. **Migration** — a new `MAJOR` ships with a deterministic transformer from the
   previous version plus a migration note; systems are never silently
   re-interpreted.


<a id="diff"></a>
## Diff and determinism

- Canonical serialization (sorted keys, 2-space indent, one trailing newline,
  no runtime fields) makes diffs semantic rather than cosmetic; the gate fails
  a non-canonical fixture.
- Structural diff classes: `identity`, `outcome` (prose), `topology`/`workflow`
  (execution shape), `capabilities` (refs), `policy` (approvals/limits),
  `deployment`, `evaluation`.
- A `policy` or `capabilities` diff is security-relevant and must be reviewed
  even in a `PATCH`; text-only diffs are not.
- Two documents differing only in key order or whitespace are byte-identical
  after canonicalization, so "no change" means no change.

<a id="four-cases"></a>
## Four cases (the expressiveness test)

v1 is only useful if it covers today's real workloads plus the next two
phases. The gate validates one canonical fixture per case
(`docs/architecture/system-ir/examples/`) — all four must pass, and each is
also checked for its shape:

| # | Case | Fixture | Shape | What it proves |
|---|---|---|---|---|
| 1 | App Builder reference workload (live) | `app-builder.system.json` | agentic | Today's planner + coordinator/coder/reviewer pipeline, approval-gated deploy and evaluation, with no invented fields. |
| 2 | M0 single agent | `m0-single-agent.system.json` | agentic | The MVP's minimal system is a legal IR, not a special case. |
| 3 | M1 multi-agent team | `m1-team.system.json` | agentic | Delegation, ordered steps, conditions and a review gate fit in `topology`. |
| 4 | Visual workflow | `visual-workflow.system.json` | procedural | A seven-kind DAG is expressible without an agent or graph-UI field. |

If a case needs a new field, the IR is wrong for v1 — not the case. That is the
guard against building the universal IR prematurely.

<a id="rejected"></a>
## Rejected for v1 (deliberately absent)

| Absent | Reason | Where it belongs |
|---|---|---|
| Hybrid systems (agents + workflow together) | No evidence it is needed; adds composition semantics v1 cannot validate. | ADR when a real workload demands it |
| Inline memory/knowledge configuration (vectors, chunking, embeddings) | Provider decisions belong to the memory layer. | P7.6 |
| Model parameters beyond a policy ref | Provider leakage into the contract. | Model gateway (P7.4) |
| Per-step retry/timeout matrices | Limits live in agent policies and the runtime adapter. | P7.5 |
| Visual coordinates, canvas layout, node positions | UI state, not system state — it would fork the IR per surface. | Visual Builder (client) |
| Secrets, credentials, provider keys | Structural impossibility is the security control. | Credential vault |
| Marketplace/pricing/publisher reputation | Distribution metadata, not execution semantics. | M5 |
| Telemetry, cost tables, evaluation results | Runtime/observation data. | P7.7 trace |
| Prose fields beyond `outcome` | Free text multiplies ambiguity; instructions live in Agent manifests. | Agent manifest |

<a id="gate"></a>
## Validation gate

`node scripts/validate-system-ir.mjs` (wired into `bun run docs:check`) checks:

1. the fixture is `kind: System` and validates against the P7.1 schema;
2. invariants I1–I6 hold;
3. the declared shape matches the case, and the workflow case exercises all
   seven node kinds;
4. canonical serialization round-trips byte-identically;
5. six negative probes are **rejected** — runtime `status`, a `telemetry`
   block, an unpinned `@latest` ref, an unknown delegation target, both shapes
   at once, and a dangling workflow edge.

A negative probe that passes is a failure: v1's value is what it refuses.

<a id="open-items"></a>
## Open items

| Item | Owner |
|---|---|
| Capability-closure resolution against a live registry (existence, not only grammar) | P7.3 / P7.4 |
| Cycle detection inside capability graphs (skill → tool → skill) | compiler |
| Deterministic `MAJOR` migration transformers | first `MAJOR` bump |
| Compiler-side policy validation (e.g. `irreversible` requires approval + idempotency) | P7.2 follow-up / P7.3 |
| Promotion from `implemented_unverified` to `verified` | after the compiler loads the IR in its own tests |

schema growth.
