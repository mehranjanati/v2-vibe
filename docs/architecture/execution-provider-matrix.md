# Execution Provider Capability Matrix (D16 · D20)

> **Status:** `accepted` (2026-11-24, documentation only) — declares what
> each provider keeps today; adds no implementation.
> **Contract:** `ecosystem/06-runtime-and-execution-architecture.md` (C1–C18),
> `architecture/adr-multi-provider-execution.md`.

## How to read

`native` = the provider itself keeps the guarantee. `emulated` = a Vibe
adapter keeps it and is separately tested. `no` = the operation refuses
with a well-defined unsupported result (never a silent downgrade).
Conformance = C1–C18 suite in `backend/pkg/execution/suite.go`.

| Capability (D16 op) | Native Go (baseline) | Cloudflare Workflows (live) | wasmCloud (EVAL ONLY) | Fastly Compute (candidate) |
|---|---|---|---|---|
| `start` | native (Fake-passing) | native (live interpreter) | NOT evaluated — needs adapter + suite | NOT evaluated |
| `status` | native | native (row state) | NOT evaluated | NOT evaluated |
| `cancel` (requested→propagated→settled) | native (Fake) | `implemented_unverified` live — terminate + `cancelled` guard (`worker/workflow/VibeWorkflow.ts`, P7-RUN-001c); unit cases in `worker/workflow/VibeWorkflow.test.ts` (CI-run) | NOT evaluated — three-stage semantics must be proven, not assumed | NOT evaluated |
| `pause` / `resume` | native (Fake) | `implemented_unverified` live (guarded writers, P7-RUN-001c; route cases green in `backend/pkg/api/workflows_control_test.go`, worker pause/resume unit cases pending) | NOT evaluated | NOT evaluated |
| `step` (durability primitive) | native | native (durable steps) | NOT evaluated — `step` mapping is the open question | NOT evaluated |
| `sleep` (long-sleep) | native | native (derived `dagRequires`) | NOT evaluated | edge/request shape — durability unproven |
| `history` | native | PARTIAL (best-effort `waitUntil` logs, not a contract surface) | NOT evaluated | NOT evaluated |
| `capabilities()` | native (Fake advertises) | `implemented_unverified` (`worker/workflow/workflowCapabilities.ts`; advertisement + refusal cases in `worker/workflow/workflowCapabilities.test.ts` and `VibeWorkflow.test.ts`, CI-run) | required before any claim | required before any claim |
| at-least-once + idempotency | suite-proven on Fake | live path relies on step semantics; cross-provider unproven | must be proven per adapter | must be proven per adapter |

## Rules

- A `no` refuses at admission (C16); an `emulated` names the adapter and
  its tests; nothing is `native` without a green suite row.
- Cancellation rows distinguish requested / propagated / settled; pause
  rows distinguish accepted / durable / resumed.
- No active-Run migration between columns without a checkpoint/state
  design (D20 rule).
