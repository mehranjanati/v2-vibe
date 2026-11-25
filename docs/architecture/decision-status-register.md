# Architecture Decision Status Register (single index — 2026-11-24)

> **Purpose:** one authoritative status per decision; the log it indexes
> stays in `10-decisions-and-open-questions.md` /
> `ecosystem/10-decisions-and-open-questions.md`. Statuses: `ACCEPTED` |
> `PROPOSED` | `RESEARCH_REQUIRED` | `EXPERIMENTAL` | `DEFERRED` |
> `SUPERSEDED`. **Rule:** research is never quoted as accepted; INDEX rows
> point at the record, they do not replace it.

| ID | Decision | Status | Record |
|---|---|---|---|
| D7 | Agentic Platform identity; one IR | ACCEPTED | `10-decisions-and-open-questions.md` |
| D8/D8.1/D8.2/D8.3/D8.4 | M1 freeze on O1; MCP transports, the guarded route surface and the server-side credential store | ACCEPTED (M1 scope still frozen) | `10-decisions-and-open-questions.md`, `DEV_CHECKLIST.md` |
| D9 | M0 slice v0 scope | ACCEPTED | `10-decisions-and-open-questions.md`, `02-mvp.md` |
| D10 | Product independence | ACCEPTED | `ecosystem/10-decisions-and-open-questions.md` |
| D11 | Shared contracts normative | ACCEPTED | `ecosystem/10-decisions-and-open-questions.md` |
| D12 | Cloudflare is a provider | ACCEPTED | `ecosystem/10-decisions-and-open-questions.md` |
| D13 | wasmCloud evaluation-only | ACCEPTED (unchanged) | `ecosystem/10-decisions-and-open-questions.md` |
| D14 | No token/chain MVP | ACCEPTED | `ecosystem/10-decisions-and-open-questions.md` |
| D15 | Agent identity/lifecycle | ACCEPTED | `architecture/adr-agent-identity-lifecycle.md` |
| D16 | Execution provider contract (9 ops, C1–C18) | ACCEPTED (unchanged) | `architecture/adr-execution-provider-contract.md` |
| D17 | One control plane, no staging | ACCEPTED | `ecosystem/10-decisions-and-open-questions.md` |
| D18 | Unified integrations (one Gate) | ACCEPTED | `architecture/adr-unified-integrations.md` |
| D19 | ADI as shared capability | PROPOSED | `architecture/adr-adaptive-intelligence-boundaries.md` |
| D20 | Multi-provider execution lifecycle | ACCEPTED (2026-11-24, docs only) | `architecture/adr-multi-provider-execution.md` |
| D21 | WASM + Component execution strategy | ACCEPTED (2026-11-24, docs only) | `architecture/adr-wasm-component-strategy.md` |
| Eino-substrate | Eino is orchestration, not governance | ACCEPTED (2026-11-24, docs only) | `architecture/adr-eino-orchestration-substrate.md` |
| Agentic-exec | agent node vs topology executor vs refuse | PROPOSED | `architecture/adr-agentic-execution.md` |
| WASM-compat | Eino↔wasmCloud compat | RESEARCH_REQUIRED | `architecture/eino-wasm-compatibility.md` (+ `wasm-poc-spec.md`, `wasm-runtime-matrix.md`) |

## Open questions carried (still unresolved)

O1 (ICP/WTP evidence, gates D8) · O14/O22/O23 (commerce) · O15 (WASM
interface choice — D21 narrows, PoC decides) · O24 (vault rotation) · O25
(registry trust) · O26/O26a/O29 (lock digest/cycles) · O27 (registry shape)
· O28 (eval datasets) · O9–O13 per the strategy log.
