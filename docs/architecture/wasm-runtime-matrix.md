# WASM Runtime Compatibility Matrix (D21 — all rows NOT_RUN)

> **Status:** `research_required` (2026-11-24) — empty scoreboard the PoC
> fills in. No row is a claim until it carries a command + versions +
> verdict. **Related:** `architecture/wasm-poc-spec.md`,
> `architecture/eino-wasm-compatibility.md`.

| Dimension | Exp. A (native baseline) | Exp. B (Eino in component) | Exp. C (native Eino + WASM tools) |
|---|---|---|---|
| Pinned toolchain (Go / componentize-go / host / runtime / WASI target) | NOT_RUN | NOT_RUN | NOT_RUN |
| Eino graph (WASM-03) | NOT_RUN (control) | NOT_RUN | NOT_APPLICABLE (orchestrator stays native by design) |
| Model API + TLS (WASM-04) | NOT_RUN | NOT_RUN | NOT_RUN |
| SSE / streaming (WASM-05) | NOT_RUN | NOT_RUN | NOT_RUN |
| Tool calling via Gate (WASM-06) | NOT_RUN | NOT_RUN | NOT_RUN — live spike exists: `worker/wasm/tool-sandbox.test.ts` (pure `add`, no Gate yet) |
| Concurrency: goroutines/sync (WASM-07/10) | NOT_RUN (control) | NOT_RUN — Workers has no threading (single thread, no Web Worker API; `workers/runtime-apis/webassembly/`, 2026-04-23) | NOT_APPLICABLE for the orchestrator side; component-side limits per WASM-12 |
| Networking: beyond WASI HTTP (WASM-12) | NOT_APPLICABLE (native has full net) | NOT_RUN — WASI on Workers is experimental, some syscalls only (same page) | NOT_RUN |
| Binary size / cold start / mem / CPU + total workload cost (WASM-13/15) | NOT_RUN (baseline first; no fixed threshold before measurement) | NOT_RUN | NOT_RUN |
| Snapshot binding preserved (WASM-14) | NOT_RUN (control) | NOT_RUN | NOT_RUN |

## Standing notes (evidence, not verdicts)

1. Workers WASM page (read 2026-11-24): `WebAssembly.instantiate()`
   pre-compiled modules; SIMD yes; **threading no**; bigger binaries slow
   startup (`wasm-opt` advised); **WASI experimental, partial syscalls**.
   No Component Model / WIT / wasmCloud statement on that page.
2. Consequence: Workers is a Model-C (tool sandbox) candidate only; Model-B
   (Eino hosting) is not inferable from that page and needs its own host
   evidence (pinned wasmCloud host/runtime, per `wasm-poc-spec.md`).
3. TinyGo stays scoped to small components: stdlib/reflection/TLS gaps are
   checks, not assumptions.
4. Experiment folders: `poc/wasm-a/` (baseline harness), `poc/wasm-b/`
   (componentize-go attempt), `poc/wasm-c/` (native executor + WASM
   tools). Each folder has its own README with scope, commands and the
   verdict table it must fill; nothing there is production code and
   nothing is imported by `backend/` or `worker/`.
