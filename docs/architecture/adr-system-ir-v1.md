# ADR — Canonical System IR v1 (PROPOSED, adopted as documentation)

> **Status:** `accepted` (2026-11-16) — decision recorded; implementation is the
> P7.1/P7.2 contract and gate, with no runtime consumer yet.
> **Deciders:** platform architecture. **Supersedes:** none. **Related:** D7
> ([`../10-decisions-and-open-questions.md`](../10-decisions-and-open-questions.md)),
> O9, O10, [`system-ir.md`](system-ir.md),
> [`manifest-schemas.md`](manifest-schemas.md).

## Context

Four creation surfaces (Natural Language Studio, Visual Builder, SDK/CLI/API,
hand-written manifests) must converge on one validated system representation,
or the platform grows two incompatible agent models — the single failure mode
decision D7 exists to prevent. At the same time the strategy explicitly forbids
building an over-general universal IR before it is proven, and the live system
already has real workloads (app generation, a seven-node workflow interpreter)
that must not be re-modelled into something they are not.

Constraints in force: Go + Eino stay the orchestration substrate (no new
engine); Cloudflare Workflows stays the durable runtime until a replaceable
adapter lands; P0–P6 task IDs are frozen; no Web3, no new store, no provider
coupling; no claim of capability beyond what the code proves.

## Options considered

### A. Separate IR format alongside the manifests *(rejected)*

Define `*.ir.json` with its own envelope and lifecycle, then map to/from
manifests.

- **Pro:** the IR could optimize for compiler needs without manifest baggage.
- **Con:** two version counters, two canonical forms, and a translation layer
  that is itself unvalidatable — the exact divergence D7 forbids. Every
  migration would need to be done twice.
- **Verdict:** rejected on architectural grounds (duplicate source of truth).

### B. Full universal IR now (memory, models, retries, telemetry, UI layout) *(rejected)*

- **Pro:** no future schema growth.
- **Con:** every field is a guess; the strategy warns that an over-general IR
  cannot be validated against real cases, and `additionalProperties: false`
  would then lock guesses into the contract. Provider fields would leak model
  configuration into the system definition.
- **Verdict:** rejected as premature; the four-case test is the remedy.

### C. Node-graph-first IR (everything is a DAG of nodes) *(rejected)*

- **Pro:** one mental model; matches the existing workflow interpreter.
- **Con:** forces agents and teams into node form, which is precisely the
  "generic workflow canvas" positioning the product refuses; it also makes the
  agent contract (role, instructions, memory scopes) unrepresentable without
  inventing node kinds that are really agents.
- **Verdict:** rejected — it inverts the product's primary concept.

### D. Manifest as IR, two execution shapes, no hybrid *(chosen)*

IR v1 **is** the `kind: System` manifest `spec`, with exactly one of two
validated shapes per system: agentic (`agents` + `topology`) or procedural
(`workflow` over the seven live node kinds).

- **Pro:** one format, one version counter, one canonical form, one validator
  (already in `docs:check`). Both real workloads are expressible without
  invention. The invariant set is small enough to enforce mechanically.
- **Con:** hybrid systems are rejected in v1; procedural workflows cannot carry
  agent-level memory/instructions; a future mixed workload needs an ADR.
- **Verdict:** chosen. The cost is a documented limitation, not a hidden one.

## Decision

Adopt option D. IR v1:

1. is the `kind: System` manifest `spec` (envelope `vibesdk/v1`);
2. accepts exactly one execution shape — agentic or procedural — enforced by
   `spec.oneOf`;
3. carries desired state only; runtime state is rejected by invariant I2;
4. pins every capability ref to exact semver and rejects floating refs;
5. is canonically serialized so diffs are semantic;
6. treats the four cases (App Builder, M0 single agent, M1 team, visual
   workflow) as the expressiveness contract: if a case needs a new field, the
   IR is wrong for v1;
7. is versioned only through the envelope `apiVersion` — no second counter.

## Consequences

- **Positive:** Natural Language, Visual, SDK/CLI and hand-written manifests can
  no longer drift; the compiler has one contract; the docs gate proves the
  contract against four fixtures and six negative probes on every commit.
- **Negative / accepted costs:** hybrid agent+workflow systems are unsupported
  until an ADR with evidence; workflow nodes cannot express agent-level memory
  or instructions (they express capability calls, not collaborators); the
  `workflow` shape adds a second compiler path to maintain.
- **Neutral:** nothing in the live runtime changes; the IR is not yet consumed
  by the compiler or the runtime, so no behaviour shifts today.

## Revisit triggers

Reopen this decision (new ADR, not an edit) when any of these is observed:

1. a real workload needs agents *and* a procedural DAG in one system;
2. a capability closure cannot be resolved without a field v1 refuses;
3. a `MAJOR` bump is required for a reason other than the four cases;
4. the runtime adapter (P7.5) needs IR fields the compiler cannot derive.

## Evidence

- Contract + invariants: [`system-ir.md`](system-ir.md).
- Schema: [`manifest-schemas/system.schema.json`](manifest-schemas/system.schema.json).
- Fixtures: `docs/architecture/system-ir/examples/*.system.json` (four cases).
- Gate: `scripts/validate-system-ir.mjs` inside `bun run docs:check` — 4/4
  cases, 6 negative probes rejected, canonical round-trip.
