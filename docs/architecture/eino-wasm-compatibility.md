# Architecture Note — Eino ↔ wasmCloud Compatibility (RESEARCH_REQUIRED)

> **Status:** `research_required` (2026-11-24) — feasibility input, not a
> decision. Decides nothing; blocks nothing except its own promotion claims.
> **Related:** `architecture/adr-wasm-component-strategy.md` (D21),
> `architecture/adr-multi-provider-execution.md` (D20),
> `architecture/adr-eino-orchestration-substrate.md`,
> `architecture/adr-agentic-execution.md`.

## Position

Do **not** make wasmCloud success depend on running the complete Eino
framework inside a WASM Component. Three models, three different burdens:

- **Model A — Native Go + Eino (baseline, WORKS TODAY).** The live engine
  (`backend/pkg/agent/eino_engine.go`, `backend/pkg/engine/team.go`,
  `backend/pkg/engine/plan_execute.go` on `eino v0.9.21`) is the control
  against which B and C are measured.
- **Model B — Eino inside a Standard-Go WASI Component
  (NOT_YET_PROVEN).** Primary feasibility experiment (= Experiment B in
  `wasm-poc-spec.md`): compile the actual Eino workload via Standard
  Go + `componentize-go`, run it on a pinned wasmCloud host/runtime,
  and report what breaks. Scaffold: `poc/wasm-b/README.md`.
- **Model C — Native Eino orchestrator + WASM tool components
  (PREFERRED practical path, REQUIRES POC).** Keep orchestration native;
  isolate selected tools as WASM Components behind WIT. First production
  candidate if the PoC passes (= Experiment C in `wasm-poc-spec.md`).
  Live Step 1 spike: `worker/wasm/tool-sandbox.ts` (pure `add`, zero
  imports); next steps scaffolded in `poc/wasm-c/README.md`.

Go paths to evaluate, in order: **Standard Go + `componentize-go`**
(primary; pin Go version, `componentize-go` version, host/runtime version,
WASI target, and every Eino dependency) then **TinyGo** (secondary, for
small components only — check stdlib gaps, reflection, goroutines, TLS,
binary size/startup). **WASI P2** for HTTP/sync components where toolchain
+ host support is verifiable; **WASI P3** research-only until async
interfaces have verifiable host support.

## What the PoC must actually check (no inference)

Three independent experiments (`wasm-poc-spec.md` Experiments A/B/C,
scaffolded in `poc/wasm-a/`, `poc/wasm-b/`, `poc/wasm-c/`) answer one
question: move the whole orchestrator to WASM, or only the execution
components? Experiment A captures the native baseline first (speed,
memory, streaming, concurrency); B attempts the full-Eino component;
C keeps Eino native and isolates tools. No fixed performance threshold
before measurement — thresholds are set per workload type from the
real A baseline. Promotion needs all decision criteria in
`wasm-poc-spec.md`: functional + security pass, zero tenant/auth
regression, host-independent snapshots, provable cancellation,
real streaming, same-workload perf comparison, total workload cost
(not CPU-only), recorded binary + startup.

Eino version + imported packages (Eino vs EinoExt, e.g.

Eino version + imported packages (Eino vs EinoExt, e.g.
`eino-ext/components/model/openai v0.1.13`), model SDKs, HTTP/TLS, JSON,
SSE/streaming, tool calling, concurrent execution, goroutines/sync,
state persistence, memory integration, cancellation, timeouts/retries,
WASI networking, Go/WASM compile compat, binary size, cold start, memory,
CPU, tracing, host capability security.

## Rules

1. A successful Go→WASM build proves nothing about Eino compat.
2. WASI HTTP ≠ raw sockets/WebSocket/gRPC/unrestricted networking.
3. WASI P3 spec existence ≠ host support.
4. No durable Run state inside ephemeral components.
5. No model credentials exposed to untrusted components.
6. No tool invocation bypassing `mcp.Gate`.
7. WASM sandboxing ≠ application authorization.
8. Every unsupported dependency + workaround is documented.

## Cloudflare Workers page (2026-11-24 read)

`developers.cloudflare.com/workers/runtime-apis/webassembly/` (last updated
2026-04-23 per page): `WebAssembly.instantiate()` for pre-compiled modules;
Workers-in-Rust via bindings; feature parity tracks Chrome (SIMD yes,
**threading no — single thread, no Web Worker API**); larger WASM binaries
slow startup; WASI is **experimental, only some syscalls implemented**
(open-source implementation + blog linked from the page). No Component
Model / WIT / `componentize-go` / wasmCloud claim on that page — anything
beyond it needs its own source. Consequence for the PoC: Workers WASM is a
*tool-sandbox* candidate (Model C shape), never assumed Eino hosting
(Model B); Eino's goroutine/concurrency surface vs no-threading is an
explicit WASM-10 check.
