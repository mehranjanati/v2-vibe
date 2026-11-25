# ADR — Multi-Provider Execution Lifecycle (D20, P7)

> **Status:** `accepted` (2026-11-24, documentation only) — extends D12/D16,
> changes neither. No code, migration, route or provider implementation.
> **Related:** D12/D13/D16 (`ecosystem/10-decisions-and-open-questions.md`),
> `architecture/adr-execution-provider-contract.md`,
> `ecosystem/06-runtime-and-execution-architecture.md`,
> `architecture/run-snapshot.md`.

## Context

D12 made Cloudflare one provider; D16 defined the nine-operation lifecycle
contract with an 18-case conformance matrix. What is still implicit: the
**separation between orchestration, lifecycle, placement, runtime substrate
and capability execution**, and how a second provider earns production
without weakening D16 (at-least-once, idempotency, Step-as-durability,
C1–C18, explicit-unsupported-over-silent-downgrade).

## Decision (D20)

Adopt the five-layer execution model; each layer has exactly one job:

1. **Control Plane** — Systems, IR validation, composition, snapshots, runs,
   policy, registry, evaluation. Sole authority for admission.
2. **Orchestration (Eino)** — decides *what to do next*; owns no durability.
   See `architecture/adr-eino-orchestration-substrate.md`.
3. **Execution Provider** — implements the stable D16 surface (`start ·
   status · cancel · pause · resume · step · sleep · history ·
   capabilities`). Providers declare supported operations; unsupported is an
   explicit refusal, never a simulated guarantee. D16 operations, states,
   error taxonomy and C1–C18 are unchanged by this record.
4. **Placement** — *where* an admitted workload may run (Cloudflare,
   Fastly edge, wasmCloud host, customer infra, Kubernetes). Placement is
   not provider selection; a placement without a conforming provider
   executes nothing.
5. **Runtime substrate / capability execution** — *how* code runs (native Go,
   WASM/WASI, Wasmtime, Workflows). A substrate is **not** a provider until a
   conforming adapter exists; WASM Components execute tools, they do not own
   runs. Durable state never lives inside an ephemeral component or host.

## Provider evaluation (no false equivalence)

| Provider | Role | Gate |
|---|---|---|
| Cloudflare Workflows | durable managed provider (live, PARTIAL per D16 mapping) | close cancel/pause/capabilities/history gaps via P7 tasks |
| Native Go | baseline + fallback | keep green; reference for perf comparison |
| wasmCloud | distributed component environment, EVALUATION ONLY (D13) | contract + PoC first — see `architecture/eino-wasm-compatibility.md` |
| Fastly Compute | edge/request target | prove durable semantics explicitly; never assume them |
| Future providers | candidates | green C1–C18 + declared capability matrix, or not a provider |

## Rules carried forward

- No live migration of an active Run between providers without an explicit
  checkpoint/state/idempotency/provenance design (new ADR).
- Provider-specific behaviour must not silently change the platform execution
  contract; `Capabilities()` refusals are admission-time (C16).
- `pause` ≠ `long-sleep` ≠ `waiting`; cancellation requested ≠ propagated ≠
  settled — the D16 distinctions stand.

## Revisit triggers

1. A workload needs a guarantee the six D16 states cannot express.
2. Cross-provider recovery is required for a real use case.
3. wasmCloud (or any substrate) demonstrates a durability model `step`
   cannot represent — new ADR, D16 amendment proposal, not an edit here.
