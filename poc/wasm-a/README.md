# Experiment A — Native Go + Eino Baseline (control)

> **Status:** `research_required` · PoC-only. Nothing here ships.
> Parent spec: `docs/architecture/wasm-poc-spec.md` (Experiment A,
> WASM-01). Nothing in this folder is imported by `backend/` or
> `worker/`; production code never depends on the PoC.

## Question

What does the current Eino agent actually cost and do — speed, memory,
streaming, concurrency — so B and C have a control to beat or match?

## What lives here (planned)

- `baseline.sh` — one reproducible command running a representative
  workload (prompt → tools → streaming response) against the native
  engine, printing: wall time, peak RSS, time-to-first-token (where the
  workload streams), token counts, binary size of the measured binary.
- `RESULTS.md` — the filled table: toolchain versions (`go version`,
  `eino v0.9.21` from `backend/go.mod`), workload hash, all numbers,
  verdict per WASM-01/WASM-13/WASM-15 rows. Empty until executed.

## Rules

- Same workload for A, B and C comparisons — never a tuned microbench
  for one side.
- Total workload cost, not CPU-only (per the PoC decision criteria).
- No threshold is set here; this experiment *produces* the baseline the
  thresholds are later set from.
