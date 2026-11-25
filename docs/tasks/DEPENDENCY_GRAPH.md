# Dependency Graph

P0-SEC-001 -> P0-SEC-002 -> P0-SEC-005
P0-SEC-001 -> P0-SEC-003
P0-SEC-001 -> P0-SEC-004 -> P1-AGT-003
P0-SEC-001 -> P0-SEC-006 (cost-gated deployment)
P0-SEC-002 -> P1-AGT-001 -> P1-AGT-002 -> P7-RUN-001
P6-COM-001 is research with no code dependency.

P0-SEC-006, P8, and P9 tasks stay backlog until their gates pass.
P0-SEC-001 is DONE (2026-11-22): boundary proven on a deployed plane,
fail-closed under store outage, plus the Worker session-token forwarding fix.
P1-AGT-001 is DONE (2026-11-22): identity + lifecycle + retirement contract.
P7-RUN-001 is DONE (contract P7-RUN-001a 2026-11-22; Fake + C1–C18 suite P7-RUN-001b;
live control surfaces P7-RUN-001c 2026-11-23; P7.12 route proof green locally
2026-11-24 with the worker cases run by CI).
P1-AGT-004 (MCP routes, SSE binding, generic credential store) is DONE 2026-11-24
(D8.2 SSE, D8.3 routes, D8.4 credential store). Its grants surface satisfies
ADI-CTX-003's stated dependency; the engine itself is DONE 2026-11-25 (`backend/pkg/activation`) and the next owner gate is a caller for it (ADI-CTX-004).

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

## Web3 workstreams (2026-11-25; W0 complete, the rest planned)

W0-WEB3-01 [DONE 2026-11-25, LayerZero architecture verified] -> W4-WEB3-01 (interop design).
W0-WEB3-02 [DONE 2026-11-25, Foundry vs Hardhat evidence, W3-TOOL PROPOSED]
-> W1-WEB3-02 (compilation design) -> W2-WEB3-01 (security gates) ->
W3-WEB3-01 (wallet/signing/deploy) -> W4-WEB3-01 and W5-WEB3-01 (operations)
-> W6-WEB3-01 (marketplace).
W0-WEB3-01 plus W0-WEB3-02 plus D22 (W3-IR and W3-VER accepted) ->
W0-WEB3-03 [DONE 2026-11-25, Contract IR v1 schema, fixtures and the sixth
docs gate] -> W1-WEB3-01 (generation scope) -> W1-WEB3-02.
Seven remain planned; the next Web3 step is W1-WEB3-01 (generation scope,
depends on the DONE schema) and W3-TOOL/W3-LZ acceptance.

## ADI workstreams (2026-11-24 audit: 25 DONE, 7 BLOCKED; evidence in `architecture/adi-implementation-evidence.md`)

ADI-FOUND-001 [DONE 2026-11-23, audit] -> ADI-FOUND-001a [DONE 2026-11-23, parity closed, `56da19a`] -> ADI-FOUND-002 [DONE 2026-11-23, lock contract] -> ADI-FOUND-003 [DONE 2026-11-23, resolver design] -> ADI-FOUND-003a [DONE 2026-11-23, `backend/pkg/composition`, `309af45`] -> ADI-FOUND-004 [DONE 2026-11-23, run-snapshot design] -> ADI-FOUND-004a [DONE 2026-11-23, migration 0017 + store + AdmitRun] -> ADI-FOUND-004b [DONE 2026-11-23, live guard + rollout switch] -> ADI-FOUND-004c [DONE 2026-11-24, producer resolve + persist + compile + start, `ad69c8b`]
ADI-FOUND-003a -> ADI-FOUND-005 [DONE 2026-11-23, system-to-run binding] -> ADI-FOUND-005a [DONE 2026-11-23, `backend/pkg/plan`]
ADI-FOUND-002 -> ADI-CTX-001 [DONE 2026-11-23, context-assembly] -> ADI-CTX-002 [DONE 2026-11-23, activation policy] -> ADI-CTX-003 -> ADI-CTX-004
ADI-CTX-001 -> ADI-MEM-001 [DONE 2026-11-23, memory-provider interface] -> ADI-MEM-002 [DONE 2026-11-23, absorbed into MEM-001] -> ADI-MEM-004 [DONE 2026-11-23, retrieval integration]
ADI-MEM-001 -> ADI-MEM-003 [DONE 2026-11-23, personalization profile] -> ADI-MEM-004
ADI-MEM-001 -> ADI-MEM-001a [DONE 2026-11-23, `backend/pkg/memory` + D1 adapter, PARTIAL: lexical only, no caller]
ADI-MEM-001 -> ADI-MEM-003 -> ADI-MEM-004
ADI-CTX-001 -> ADI-EVT-001 [DONE 2026-11-23, event envelope] -> ADI-EVT-002 [DONE 2026-11-23, outbox + inbox, DESIGN_ONLY] -> ADI-EVT-003 [DONE 2026-11-23, event-to-run, DESIGN_ONLY]
ADI-FOUND-004 -> ADI-EVAL-001 [DONE 2026-11-23, evaluation contract] -> ADI-EVAL-002 [DONE 2026-11-23, runner design] -> ADI-EVAL-002a [DONE 2026-11-23, `backend/pkg/evaluation`, PARTIAL: L1+L2 only]
ADI-EVAL-002 [DONE 2026-11-23, runner design] -> ADI-EVAL-003 [DONE 2026-11-23, candidate lifecycle] -> ADI-EVAL-004 [DONE 2026-11-23, promotion/rollback + audit]
ADI-FOUND-002 -> ADI-ECO-001 [BLOCKED] -> ADI-ECO-002 [BLOCKED] -> ADI-ECO-003 [BLOCKED] -> ADI-ECO-004 [BLOCKED]
P1-AGT-004 [DONE 2026-11-24: D8.2 + D8.3 + D8.4] -> ADI-CTX-003 [DONE 2026-11-25: eligibility core, pure library] -> ADI-CTX-004 [PREPARED, dependency satisfied]; P1-AGT-004 -> ADI-ECO-001 [BLOCKED]. ADI-EVT-002 blocked on P7-RUN-001 (DONE); ADI-ECO-003 blocked on O24/O25; ADI-ECO-002 blocked on O27.
First executable proof (single-agent adaptive composition) is gated by D8/D9 and is not in NEXT_EXECUTION. `P1-AGT-004` and `ADI-CTX-003` are DONE; the next executable step is the owner decision on O1 evidence (which also governs any activation caller); ADI-CTX-003 is PREPARED, not NEXT.
