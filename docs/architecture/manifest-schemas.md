# Manifest Schemas — Agent · Skill · Tool · System (P7.1)

> **Scope:** the normative specification behind the four VibeSDK manifests:
> envelope, per-kind `spec` shapes, semver and compatibility rules,
> content-addressed references, canonical serialization, and the validation
> gate. Implements checklist task **P7.1**
> ([`../DEV_CHECKLIST.md`](../DEV_CHECKLIST.md)); closes open decision **O10**
> ([`../10-decisions-and-open-questions.md`](../10-decisions-and-open-questions.md)).
>
> **Status:** `implemented_unverified` — schemas, examples and the validator
> exist and run in `bun run docs:check`; no runtime code consumes them yet
> (registry/compiler are P7.2/P7.3/P7.4). Last reviewed: 2026-11-16.
>
> **Bounded by:** [`../06-functions-and-tools.md`](../06-functions-and-tools.md)
> (manifest-first rule + minimal shapes),
> [`../05-agent-runtime.md`](../05-agent-runtime.md) (thirteen-part Agent
> contract), [`../12-agentic-platform-strategy.md`](../12-agentic-platform-strategy.md)
> (Canonical System IR, observability).

## Contents

| Section | Anchor |
|---|---|
| Files in this directory | [`#files`](#files) |
| Envelope (all four kinds) | [`#envelope`](#envelope) |
| `kind: Agent` | [`#agent`](#agent) |
| `kind: Skill` | [`#skill`](#skill) |
| `kind: Tool` | [`#tool`](#tool) |
| `kind: System` | [`#system`](#system) |
| Versioning and compatibility | [`#versioning`](#versioning) |
| References and resolution | [`#references`](#references) |
| Canonical serialization | [`#serialization`](#serialization) |
| Validation gate | [`#validation`](#validation) |
| Deliberate omissions (v1) | [`#omissions`](#omissions) |
| Open items | [`#open-items`](#open-items) |

<a id="files"></a>
## Files in this directory

| File | Purpose |
|---|---|
| `agent.schema.json` | JSON Schema (draft 2020-12) for `kind: Agent` |
| `skill.schema.json` | JSON Schema for `kind: Skill` |
| `tool.schema.json` | JSON Schema for `kind: Tool` |
| `system.schema.json` | JSON Schema for `kind: System` |
| `examples/lead-qualifier.agent.json` | Canonical Agent example (13-part contract) |
| `examples/qualification-rubric.skill.json` | Canonical Skill example |
| `examples/score-lead.tool.json` | Canonical Tool example |
| `examples/inbound-sales.system.json` | Canonical System example composing the above |

Schemas are documentation artifacts in this phase — they define the contract
the registry and compiler must enforce later. No runtime code is added by
P7.1.

### Use-case bundles live elsewhere

Concrete workloads are **not** added to `examples/`. A use case ships as its
own closed bundle under `docs/use-cases/<use-case>/manifests/`, with runtime
code in `backend/pkg/<use-case>/`, so a product use case never mixes into the
platform's canonical examples:

| Use case | Bundle | Runtime |
|---|---|---|
| Spot trader (paper) | `docs/use-cases/spot-trader/manifests/` | `backend/pkg/trader/` |

The validator scans both locations. A bundle is a **closed reference set**: a
manifest inside `docs/use-cases/<name>/manifests/` may only reference manifests
in the same bundle, so a use case cannot silently depend on a platform example.
This matches the export contract in this document: a System exports together
with its pinned closure as one bundle, so the bundle must be self-contained —
reuse means copying the pinned manifest into the bundle, not referencing out.
See [`../use-cases/spot-trader/README.md`](../use-cases/spot-trader/README.md).

<a id="envelope"></a>
## Envelope (all four kinds)

Every manifest is a single JSON object with exactly four root properties and
`additionalProperties: false`:

| Field | Rule |
|---|---|
| `apiVersion` | `"vibesdk/v1"` (const). A breaking envelope change bumps this string. |
| `kind` | One of `Agent` · `Skill` · `Tool` · `System` (const per schema). |
| `metadata` | `name` (DNS-safe, ≤63, immutable once published), `version` (semver), `publisher`, `license`; optional `description`, `labels`. |
| `spec` | The per-kind contract (below). |

`status` is **forbidden** at any level: runtime state (run status, health,
deployment) lives in the data layer, never in the file. The validator fails a
manifest that carries it.

<a id="agent"></a>
## `kind: Agent`

Required `spec`: `role`, `instructions`, `model`, `inputSchema`,
`outputSchema`, `policies`. Optional: `tools`, `skills`, `memory`,
`knowledge`, `evaluation`.

- `model`: `ref` (+ optional `policy` with `temperature`, `maxTokens`,
  `fallback`). Agents never name a provider; the gateway resolves the policy.
- `tools` / `skills` / `knowledge`: arrays of content-addressed refs
  (`uniqueItems`).
- `memory.scope`: `run` · `conversation` · `agent` · `team` · `tenant`;
  `retentionDays` 0–3650.
- `policies.approvals`: subset of `external-message`, `external-write`,
  `payment`, `human-review`; `policies.resourceLimits`: `maxSteps` (1–500),
  `timeoutSeconds` (1–86400).
- `evaluation`: `dataset` ref + `thresholds` (0–1 per metric).

The thirteen parts of
[`../05-agent-runtime.md`](../05-agent-runtime.md) map one-to-one: Identity →
`metadata`; Role/Instructions → `spec.role`/`spec.instructions`; Model →
`spec.model`; Tools/Skills → `spec.tools`/`spec.skills`; Memory/Knowledge →
`spec.memory`/`spec.knowledge`; Policies → `spec.policies`; Input/Output
Schema → `spec.inputSchema`/`spec.outputSchema`; Execution Limits →
`spec.policies.resourceLimits`; Evaluation Rules → `spec.evaluation`.

<a id="skill"></a>
## `kind: Skill`

Required `spec`: `description`, `instructions`, `requires.capabilities`.
Optional: `requires.agentApiVersion`, `compatibility`.

- Skills never execute code; they extend what Agents know how to do.
- `requires.capabilities` pins the Tool refs the skill depends on.
- `compatibility.agents` lists Agent refs the skill is tested against;
  `compatibility.models` lists model-policy slugs.

<a id="tool"></a>
## `kind: Tool`

Required `spec`: `description`, `inputSchema`, `outputSchema`, `permissions`,
`tenant`, `sideEffect`. Optional: `approvals`, `timeoutSeconds`, `retry`,
`idempotency`, `rateLimits`, `secrets`, `adapter`, `provenance`.

- `permissions`: `resource:action` patterns (e.g. `leads:read`), minimum one.
- `tenant`: `{ isolation: "required", contextKey }` — isolation cannot be
  relaxed in v1.
- `sideEffect`: `safe` · `external` · `irreversible`; `irreversible` requires
  an approval class and an idempotency key by policy (enforced by the
  compiler, not the schema).
- `secrets`: `vault:<path>` references only — a literal secret value is
  structurally impossible.
- `adapter.type`: `native` · `function` · `http` · `mcp`. MCP-exposed tools
  carry the server ref; discovery still grants no execution

<a id="system"></a>
## `kind: System`

Required `spec`: `outcome` plus **exactly one execution shape** — agentic
(`agents` + `topology`) or procedural (`workflow`). Optional: `routing`,
`approvals`, `deployment`, `evaluation`. The shape rule is enforced by
`spec.oneOf`; a document carrying both shapes is invalid in v1.

- `agents`: map of stable local name → `{ ref, delegatesTo? }`. `ref` is an
  Agent ref; `delegatesTo` lists local names (delegation semantics live in the
  composition, not in the Agent).
- `topology`: ordered steps, each `{ run, on?, after?, when? }`; `run`/`after`
  reference local agent names, `on`/`when` are trigger/condition expressions
  evaluated by the runtime (not by the schema).
- `workflow`: procedural shape — `nodes` (unique `id`, `kind` from the seven
  live interpreter types, optional `capability` ref, optional `next`) plus
  `entry`. Used when the workload is a pipe rather than a collaboration.
- `routing`: `parallel` (bool) and `onError` (`fail-run` · `escalate-human` ·
  `skip-step`).
- `approvals`: `{ on, approver }` with `approver` = `role:…`/`user:…`/`group:…`.
- `deployment`: `environment` (`development` · `staging` · `production`),
  `replicas` (0–100).
- `evaluation`: `dataset` ref + `thresholds`.

The System Manifest **references, never inlines** the other three kinds, so
each part stays independently versioned, diffable and reusable. It is the
serializable form of Canonical System IR v1 — the same bytes, no parallel
format; the IR contract, its invariants and the four-case test live in
[`system-ir.md`](system-ir.md) (P7.2) and
[`system-ir/examples/`](system-ir/examples/).

<a id="versioning"></a>
## Versioning and compatibility

Semantic versioning, enforced by schema pattern (no `latest`, no ranges):

| Bump | Meaning | Allowed on the same `apiVersion` |
|---|---|---|
| `MAJOR` | Contract changes: removed/renamed field, narrowed enum, tightened validation, changed semantics. | Yes, as a new identity — never a silent upgrade. |
| `MINOR` | Additive optional fields only; existing consumers keep working. | Yes. |
| `PATCH` | Instructions/description text, publisher/legal metadata; no contract change. | Yes. |

Rules:

1. `metadata.name` is immutable once published; renaming publishes a new name.
2. A System pins exact refs; the compiler rejects floating refs at deploy
   (the schema already rejects anything but `MAJOR.MINOR.PATCH`).
3. A breaking change requires a `MAJOR` bump **plus** a migration note in the
   publishing change (an unchanged `MAJOR` may not change meaning).
4. `apiVersion` bumps only when the envelope itself changes; that is a
   platform-level, ADR-backed event.


<a id="references"></a>
## References and resolution

Reference grammar: `kind/name@MAJOR.MINOR.PATCH` where `kind` is one of
`agent` · `skill` · `tool`, plus the reserved external namespaces
`model-policy` · `knowledge` · `eval`.

- Refs are content-addressed, never paths — a manifest stays portable between
  local dev, Git and the registry.
- The validator resolves `agent`/`skill`/`tool` refs against the bundled
  examples and fails on an unresolved target; external namespaces are reported
  as notes because their catalogs are not shipped here yet.
- Cross-kind cycles (Skill → Tool → Skill) are legal in v1 but must be
  reported by the compiler (P7.2); the schema does not detect cycles.

<a id="serialization"></a>
## Canonical serialization

Manifests are UTF-8 JSON (`.json` in this directory; the platform accepts
YAML with the same shape when the registry lands) with:

1. keys sorted recursively,
2. 2-space indent,
3. exactly one trailing newline,
4. no comments, no `status`, no derived fields.

Round-trip is normative: `parse → validate → canonical-serialize` must be
byte-identical to the stored file, so diffs stay reviewable and exports stay
reproducible. The validator enforces byte equality against the example files.

<a id="validation"></a>
## Validation gate

`node scripts/validate-manifests.mjs` (also runs inside `bun run docs:check`)
checks, with field-level errors:

1. every schema is draft 2020-12 and pins its own `kind` const;
2. every example validates against its schema (`additionalProperties: false`
   catches typos and stray `status`);
3. identities are unique and `kind/name@version` well-formed;
4. every `agent`/`skill`/`tool` ref resolves; external namespaces are noted;
5. canonical serialization round-trips byte-identically.

The validator is dependency-free by design: `ajv` is only a transitive
dependency here, so a docs gate must not rely on it. It implements the
keyword subset these schemas use (`type`, `enum`, `const`, `pattern`,
`minLength`, `maxLength`, `minimum`, `maximum`, `required`, `properties`,
`additionalProperties`, `items`, `minItems`, `uniqueItems`, `allOf`, `oneOf`,
local `$ref`). Anything beyond that subset must be added to the validator in
the same change that uses it.

<a id="omissions"></a>
## Deliberate omissions (v1)

- No `status`, health, run or deployment state in any manifest.
- No provider-specific model fields (the gateway resolves policies).
- No inline secrets anywhere (`vault:` refs only).
- No version ranges, `latest`, or git-style refs.
- No signature/DID fields yet — the envelope reserves publisher + license, and
  a signature slot arrives with the W3.1 identity work (ADR first).
- No visual-graph fields: the Visual Builder compiles to the System Manifest,
  it does not extend it.

<a id="open-items"></a>
## Open items

| Item | Owner |
|---|---|
| YAML acceptance + parser choice for the registry | P7.2 / P7.4 |
| Draft 2020-12 `unevaluatedProperties` support (not needed by these schemas) | validator, only if a schema needs it |
| Signature/DID provenance slot | W3.1 (ADR, M5) |
| Cycle detection + capability-closure checks | P7.2 compiler |
| Promotion from `implemented_unverified` to `verified` | after a real registry/compiler consumer lands and its tests consume these schemas |

  ([`../07-multitenancy-security.md`](../07-multitenancy-security.md)).
