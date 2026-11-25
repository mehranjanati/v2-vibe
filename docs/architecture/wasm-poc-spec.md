# PoC Specification — Eino-WASM-wasmCloud Compatibility Spike

> **Status:** `research_required` (2026-11-24) — bounded spike, isolated from
> production. Changes no runtime, contract, route, migration or dependency.
> **Related:** `architecture/eino-wasm-compatibility.md`,
> `architecture/adr-wasm-component-strategy.md` (D21).

## Scope (in)

1. Inventory the exact Eino surface Vibe uses (`backend/go.mod`: `eino
   v0.9.21`, `eino-ext/components/model/openai v0.1.13`; engines
   `backend/pkg/agent/eino_engine.go`, `backend/pkg/engine/team.go`,
   `backend/pkg/engine/plan_execute.go`).
2. Minimal native Go + Eino baseline (Model A) with reproducible command.
3. Standard-Go + `componentize-go` build attempt of the real workload.
4. TinyGo only for one small representative component.
5. WASI P2/P3 only where toolchain + host support are verifiable.
6. One minimal WASM Tool Component behind a WIT interface.
7. Native-Eino ↔ WASM-tool interaction through `mcp.Gate`.
8. Functional + security + performance rows recorded with versions.

## Out of scope (explicit)

No production runtime replacement, no Eino replacement, no D16 rewrite, no
provider adapters, no wasmCloud deployment, no WASI P3 migration, no new
scheduler, no Run-model change, no Control Plane change, no new infra
dependency without an ADR.

## Experiments (independent, comparable)

Three independent experiments answer one question: should the whole
orchestrator move to WASM, or only the execution components?

- **Experiment A — Native Go + Eino (Baseline).** Run the current Eino
  agent in native Go to capture the baseline for speed, memory,
  streaming and concurrency. This is the control every other result is
  compared against.
- **Experiment B — Eino + Standard Go WASI Component (Main feasibility
  test).** Attempt to compile the same agent with Standard Go +
  `componentize-go` into a Component and run it on wasmCloud. Records
  exactly what breaks.
- **Experiment C — Eino + Native Executor + WASM Tools (Fallback /
  likely practical architecture).** Eino runs in a native Go executor
  service, while independent tools and workloads run in WASM
  Components. First production candidate if the PoC passes.

## Test matrix (all `NOT_RUN` until executed)

Each row names the experiment(s) that must produce it. A row without an
executed command stays `NOT_RUN` — never `PASS`.

| ID | Case | Experiment | What must be shown |
|---|---|---|---|
| WASM-01 | Native baseline | A | Model A runs; perf numbers captured |
| WASM-02 | componentize-go build | B | exact toolchain + errors recorded |
| WASM-03 | Eino graph execution | A vs B | same workload native vs component |
| WASM-04 | Model API request | A vs B vs C | TLS path explicit; no leaked credential |
| WASM-05 | SSE and streaming | A vs B vs C | demonstrated, not inferred |
| WASM-06 | Tool calling | A vs B vs C | via Gate in all modes |
| WASM-07 | Concurrent tools | A vs B | goroutine behaviour compared |
| WASM-08 | External durable state | B, C | survives restart; none inside component |
| WASM-09 | Tenant-scoped memory | A vs B vs C | isolation held in all modes |
| WASM-10 | Cancellation | A vs B vs C | requested/propagated/settled distinguished |
| WASM-11 | Host restart and recovery | B, C | at-least-once + idempotency held |
| WASM-12 | Network isolation | B, C | WASI HTTP limits documented |
| WASM-13 | Resource limits | A vs B vs C | binary, cold start, mem, CPU |
| WASM-14 | Snapshot binding | A vs B vs C | same SnapshotId + digest pins all modes |
| WASM-15 | Perf / load comparison | A vs B vs C | native vs WASM, same workload |

## Decision criteria

No fixed, arbitrary performance threshold is set before measurement.
First capture the real Experiment A baseline, then set the acceptable
threshold per workload type. A model is promotable only when **all** of
the following hold:

- All functional and security tests pass.
- Zero tenant-isolation or authorization regression is acceptable.
- Snapshots and composition never depend on the host.
- Cancellation has explicit, provable semantics
  (requested vs propagated vs settled).
- Streaming is real for the actually-required scenarios, not assumed.
- Performance is compared against native Go on the same workload.
- Total workload execution cost is measured — not CPU cost alone.
- Binary size and startup time are recorded.

## Rules

- Every test: reproducible command + exact toolchain/runtime versions.
- Verdicts: `PASS` | `FAIL` | `BLOCKED` | `NOT_APPLICABLE` only;
  unexecuted is `NOT_RUN`, never `PASS`.
- Success = no governance/tenant regression, snapshot pins immutable,
  Gate still authorizes, cancellation/streaming measured, durability
  outside components, unsupported items documented.
- Cost rule: report total cost per representative workload (host +
  egress + storage + invocations where applicable), never a CPU-only
  number presented as the workload cost.
