# Technical Debt and Gap Analysis — Evidence-Based (Canonical)

> **Status:** `adopted` 2026-11-21. Reality: `DEV_STATUS.md`. Backlog: `DEV_CHECKLIST.md`.

## Capability matrix (evidence, not aspiration)

- Go backend plus Fiber routes: IMPLEMENTED.
- React plus TypeScript SPA: IMPLEMENTED.
- Eino plus DeepAgent team plus plan-execute: IMPLEMENTED.
- Model gateway plus modelrouter plus coder consumer: PARTIAL.
- Skills packages plus role registry: PARTIAL (no versioned Skill registry).
- MCP trust boundary: PARTIAL (no wire transport, in-memory stores).
- Workflows interpreter plus engine: PARTIAL.
- Auth plus session boundary: PARTIAL (opt-in).
- Tenant isolation, vault revocation, quotas: PARTIAL (code exists, rollout pending).
- Metering recorder: PARTIAL (no billing).
- Git lineage plus History: PARTIAL (plan-execute path gap).
- Storage plus retrieval (Redis VFS, D1 lineage): PARTIAL.
- Manifests plus System IR: DESIGNED (docs gates only).
- Assistant/Research/Commerce/Cloud/Network surfaces: PLANNED/RESEARCH.
- WASM, containers, GPU, wasmCloud, chain: RESEARCH (no code).

## Conflicts and gaps

No separate IR format; no second version counter; provider leakage refused;
Cloudflare stays a provider; hybrid agent-plus-workflow needs an ADR;
marketplace commerce waits on demand plus controls.

## Dead or unused

Legacy controller trees and SpaceDO paths not imported by the live entry
point remain retired; document-only types without handlers are not live.
