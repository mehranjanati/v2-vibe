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
