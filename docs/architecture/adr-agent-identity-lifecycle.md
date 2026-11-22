# ADR — Agent Identity, Lifecycle, and Retirement (P1-AGT-001)

> **Status:** `accepted` (2026-11-22) — contract recorded; no runtime consumer.
> **Deciders:** platform architecture. **Supersedes:** none.
> **Related:** D10/D11 (`ecosystem/10-decisions-and-open-questions.md`),
> `ecosystem/05-agent-platform-architecture.md`,
> `architecture/manifest-schemas.md`, `architecture/system-ir.md`,
> `architecture/adr-system-ir-v1.md`.

## Context

The platform must answer "what exactly ran, and is it still allowed to run?"
before agents can be published, shared, or monetised. Today the repository has
versioned *documents* (manifests validated by `bun run docs:check`) but no notion
of an agent as a durable identity with a lifecycle: the running system identifies
roles (coordinator/coder/reviewer) and generation rows, nothing more.

Three constraints shape the decision. The envelope already forbids `status` in
manifests and the IR rejects runtime state (invariant I2), so lifecycle cannot be
smuggled into the file. D10 requires products to evolve independently through
contracts, so identity cannot be product-specific. And the platform must stay
self-hostable, so identity cannot depend on a Cloud-only registry service.

## Options considered

### A. Lifecycle state inside the manifest *(rejected)*

- **Pro:** one self-describing file.
- **Con:** it is runtime state in a signed artifact — the file would go stale the
  moment an instance pauses, diffs would be noisy, and a manifest could lie about
  its own safety. The envelope's ban on `status` and IR invariant I2 exist
  precisely to prevent this; honouring them and then re-adding state elsewhere
  would be worse than either choice.
- **Verdict:** rejected on architectural grounds.

### B. Identity = `kind/name@version` (version is the identity) *(rejected)*

- **Pro:** no separate registry concept; every ref is already unique.
- **Con:** "retire this agent" becomes impossible — you can only retire individual
  versions, so withdrawal needs an unbounded list of records; deprecation notices
  have no subject; and evaluation history cannot roll up per agent.
- **Verdict:** rejected; the identity must outlive its versions.

### C. Three identities, lifecycle in the data layer, tombstoned retirement *(chosen)*

Definition identity (`publisher` + `name`), version identity (semver), and
instance identity (tenant deployment), with lifecycle state held outside the
manifest and retirement implemented as a tombstone.

- **Pro:** matches every requirement — immutable artifacts, auditable state,
  withdrawal without deletion, exact refs, self-hostable (state is just rows).
- **Con:** a registry and deployment store must exist before any of it is
  enforceable; two places to look (manifest vs registry) for a complete picture.
- **Verdict:** chosen. The cost is deferred implementation, not correctness.

### D. Six lifecycle states vs three *(chosen: six)*

`draft · published · active · paused · retired · obsolete`.

- Three states (draft/published/retired) cannot express "deployed but
  temporarily refusing work" without conflating tenant-scoped quota enforcement
  with definition-level withdrawal — the exact confusion that makes an outage
  look like a product retirement.
- Six states stay small enough to reason about, and `obsolete` gives security
  withdrawal a distinct, louder semantics (cancel, don't finish).

## Decision

Adopt option C with option D's six states. Specifically:

1. Three identities are distinct and every run records all three.
2. `kind/name` is the uniqueness key; `name` is immutable once published.
3. Lifecycle is data-layer state, audited on every transition; the manifest never
   carries it.
4. Retirement is a tombstone: resolvable for read/audit and existing pins, closed
   to new pins and new runs, with an explicit successor and finish-vs-cancel rule.
5. `obsolete` (security withdrawal) cancels in-flight runs; `retired`
   (publisher withdrawal) lets them finish.

## Consequences

- **Positive:** withdrawal, deprecation and marketplace delisting all have a
  truthful representation; export/diff stay readable across retirements; the IR
  and manifest contracts need no new field.
- **Negative / accepted costs:** the registry and deployment store become hard
  prerequisites for P4 Studio and any marketplace listing — publishing cannot ship
  as a UI-only feature. Tombstones mean "delete" is never a real delete, so an
  erasure flow needs a legal answer (tracked as an open item).
- **Neutral:** nothing in the live runtime changes today; the current roles and
  generation lineage are unaffected.

## Revisit triggers

1. A tenant requires hard deletion of published artifacts (erasure obligation).
2. A deployment needs lifecycle semantics the six states cannot express.
3. The registry proves it cannot serve exports for retired identities.
4. Signing/provenance (W3.1) needs `publisher` to carry more than a name.

## Evidence

- Contract: `docs/ecosystem/05-agent-platform-architecture.md` (identity, rules,
  state machine, retirement).
- Existing identity constraints: `docs/architecture/manifest-schemas.md`
  (versioning table, immutability rules) and the four schemas under
  `docs/architecture/manifest-schemas/`.
- Gate that keeps the schemas honest today: `bun run docs:check`.
