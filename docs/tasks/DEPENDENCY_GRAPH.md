# Dependency Graph

P0-SEC-001 -> P0-SEC-002 -> P0-SEC-005
P0-SEC-001 -> P0-SEC-003
P0-SEC-001 -> P0-SEC-004 -> P1-AGT-003
P0-SEC-001 -> P0-SEC-006 (cost-gated deployment)
P0-SEC-002 -> P1-AGT-001 -> P1-AGT-002 -> P7-RUN-001
P6-COM-001 is research with no code dependency.

P0-SEC-006, P7-RUN-001, P8, and P9 tasks stay backlog until their gates pass.
P0-SEC-001 is DONE (2026-11-22): boundary proven on a deployed plane,
fail-closed under store outage, plus the Worker session-token forwarding fix.
P1-AGT-001 is DONE (2026-11-22): identity + lifecycle + retirement contract.
P7-RUN-001 is IN_PROGRESS: the contract (P7-RUN-001a) landed 2026-11-22; the
adapters plus the C1-C18 suite (P7-RUN-001b) stay blocked on P1-AGT-002 only for
binding the RunPlan payload to the System IR — the interface itself is
plan-agnostic and does not need it.

## Historical ID mapping (DEV_CHECKLIST to ecosystem backlog)

| Ecosystem ID | Historical ID(s) | Relation |
|---|---|---|
| P0-SEC-001 | P0.3 | Rollout of the existing boundary, not a new mechanism |
| P0-SEC-002 | P0.4 plus P0.7 slice | Ownership plus workflow tenant scope verification |
| P0-SEC-003 | P0.5 | WebSocket gate verification |
| P0-SEC-004 | P0.6 | Vault and revocation audit |
| P0-SEC-005 | P0.7 | Workflow isolation proof |
| P1-AGT-001 | P7.1 slice | Agent identity over the manifest contract |
| P1-AGT-002 | P7.2 slice | First compiler consumer of the IR |
| P1-AGT-003 | P7.3 slice | Live transport behind the trust boundary (blocked on vault) |
| P7-RUN-001 | P7.5 slice | Provider contract plus conformance |
| P6-COM-001 | None (new research) | UCP and AP2 spec review only, no code |

Historical IDs keep their meaning; ecosystem IDs track the rollout and
verification work on top. No task is marked DONE without repo evidence.

## ADI workstreams (PROPOSED 2026-11-23, design-only, all BLOCKED)

ADI-FOUND-001 [DONE 2026-11-23, audit] -> ADI-FOUND-001a [DONE 2026-11-23, parity closed] -> ADI-FOUND-002 [DONE 2026-11-23, lock contract] -> ADI-FOUND-003 [DONE 2026-11-23, resolver design] -> ADI-FOUND-003a [DONE 2026-11-23, backend/pkg/composition] -> ADI-FOUND-004 [DONE 2026-11-23, run-snapshot design] -> ADI-FOUND-004a [DONE 2026-11-23, migration + store + AdmitRun] -> ADI-FOUND-004b [DONE 2026-11-23, live guard + rollout switch]
ADI-FOUND-003a -> ADI-FOUND-005 [DONE 2026-11-23, system-to-run binding] -> ADI-FOUND-005a [DONE 2026-11-23, backend/pkg/plan] -> (producer: resolve + persist + compile + start)
ADI-FOUND-002 -> ADI-CTX-001 [DONE 2026-11-23, context-assembly] -> ADI-CTX-002 [DONE 2026-11-23, activation policy] -> ADI-CTX-003 -> ADI-CTX-004
ADI-CTX-001 -> ADI-MEM-001 [DONE 2026-11-23, memory-provider interface] -> ADI-MEM-002 [DONE 2026-11-23, absorbed into MEM-001] -> ADI-MEM-004 [DONE 2026-11-23, retrieval integration]
ADI-MEM-001 -> ADI-MEM-003 [DONE 2026-11-23, personalization profile] -> ADI-MEM-004
ADI-MEM-001 -> ADI-MEM-001a [DONE 2026-11-23, backend/pkg/memory + D1 adapter]
ADI-MEM-001 -> ADI-MEM-003 -> ADI-MEM-004
ADI-CTX-001 -> ADI-EVT-001 [DONE 2026-11-23, event envelope] -> ADI-EVT-002 [DONE 2026-11-23, outbox + inbox] -> ADI-EVT-003 [DONE 2026-11-23, event-to-run]
ADI-FOUND-004 -> ADI-EVAL-001 [DONE 2026-11-23, evaluation contract] -> ADI-EVAL-002 [DONE 2026-11-23, runner design] -> ADI-EVAL-002a [DONE 2026-11-23, backend/pkg/evaluation]
ADI-EVAL-002 [DONE 2026-11-23, runner design] -> ADI-EVAL-003 [DONE 2026-11-23, candidate lifecycle] -> ADI-EVAL-004 [DONE 2026-11-23, promotion/rollback + audit]
ADI-FOUND-002 -> ADI-ECO-001 -> ADI-ECO-002 -> ADI-ECO-003 -> ADI-ECO-004
ADI-CTX-003 blocked on P1-AGT-004; ADI-EVT-002 blocked on P7-RUN-001; ADI-ECO-003 blocked on O24/O25.
First executable proof (single-agent adaptive composition) is gated by D8/D9 and is not in NEXT_EXECUTION.
