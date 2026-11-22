// Package execution is the P7-RUN-001b implementation of the execution-provider
// contract (ADR: docs/architecture/adr-execution-provider-contract.md,
// contract text: ecosystem/06-runtime-and-execution-architecture.md).
//
// The contract exists so "durable", "retried" and "cancelled" mean one thing
// across Cloudflare Workflows, a local runner and a future container/WASM
// runtime. The rules it imposes are:
//
//   - `step` is the only durability primitive. Work outside a step may be
//     re-executed after a restart.
//   - Delivery is at-least-once. A step may run twice; side-effecting steps need
//     an idempotency key. Nothing here claims exactly-once.
//   - Step names are identity. A renamed step is new work.
//   - Terminal states are final; a retry is a new run.
//   - Providers advertise capabilities, and an unsupported requirement is
//     refused at admission (`Rejected`), never silently downgraded.
//   - `Rejected` is not `Failed`: admission refusal is not a runtime failure.
//
// This package contains the contract types, the admission rules, a deterministic
// in-process provider used as the reference implementation, the C1–C18
// conformance suite, and a Cloudflare Workflows adapter. Nothing is wired to the
// live chat path: the workflow interpreter keeps running as before (D8).
package execution
