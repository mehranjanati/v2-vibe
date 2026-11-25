# Experiment B — Eino + Standard Go WASI Component (main feasibility test)

> **Status:** `research_required` · PoC-only. Nothing here ships.
> Parent spec: `docs/architecture/wasm-poc-spec.md` (Experiment B,
> WASM-02..05/07/10/12). Nothing in this folder is imported by
> `backend/` or `worker/`.

## Question

Can the actual Eino workload compile via Standard Go +
`componentize-go` and run on a pinned wasmCloud host — and if not,
what exactly breaks?

## What lives here (planned)

- `PINNED_VERSIONS.md` — Go version, `componentize-go` version,
  wasmCloud host/runtime version, WASI target, every Eino dependency
  version. All empty until pinned at execution time.
- `build.sh` — the single reproducible build command (expected to fail
  informatively at first: record the exact compiler errors).
- `RESULTS.md` — per-test verdicts (`PASS`/`FAIL`/`BLOCKED`/
  `NOT_APPLICABLE`) for WASM-02 through WASM-05, WASM-07, WASM-10,
  WASM-12 with commands + versions. `NOT_RUN` until executed.

## Rules

- A successful build proves nothing about Eino compat — only the
  executed WASM-03/04/05/07 rows do.
- Every unsupported dependency + workaround is documented; no silent
  stubbing of a guarantee (cancellation, TLS, streaming).
- Known headwinds (not verdicts): Workers has no threading; WASI on
  Workers is partial — but B runs on wasmCloud, so B must report its
  *own* host's answers, not borrow Workers' page.
