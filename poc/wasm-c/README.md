# Experiment C — Native Eino Executor + WASM Tools (likely practical path)

> **Status:** `research_required` · PoC-only. Nothing here ships.
> Parent spec: `docs/architecture/wasm-poc-spec.md` (Experiment C,
> WASM-04..06/08..11/13..15). Nothing in this folder is imported by
> `backend/` or `worker/`.

## Question

Can Eino stay native while independent tools run as isolated WASM
Components — keeping governance, snapshot binding and cancellation
intact?

## What lives here (planned)

- Live Step 1 already exists: `worker/wasm/tool-sandbox.ts` +
  `worker/wasm/tool-sandbox.test.ts` (pure `add`, zero imports, 41 B).
  That spike stays where it is; this folder grows the *next* steps.
- `NEXT.md` — the ordered step list: WIT interface for one real tool →
  Gate-authorized call native→WASM → tenant-isolation row (WASM-09) →
  cancellation row (WASM-10) → restart/durability rows (WASM-08/11).
- `RESULTS.md` — per-test verdicts for the C rows with commands +
  versions. `NOT_RUN` until executed.

## Rules

- The Gate authorizes every tool call in every mode; a WIT import never
  implies a permission (D21).
- Durable state and secrets never enter the component (WASM-08 rule).
- Cancellation/streaming must be measured on the real path, not the
  `add` toy — the toy proves instantiation only.
