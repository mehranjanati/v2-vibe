# Decisions and Open Questions — Ecosystem Delta (Canonical)

> **Status:** `adopted` 2026-11-21. Full log: `10-decisions-and-open-questions.md`.

## New adopted decisions

- **D10** Product independence: eight products evolve independently through contracts.
- **D11** One-ecosystem contract set: the twelve shared contracts are normative.
- **D12** Cloudflare as one provider, never the platform identity.
- **D13** wasmCloud stays out of production until contract plus PoC exist.
- **D14** No token or chain dependency in any MVP.
- **D15** Agent identity, lifecycle, and retirement (P1-AGT-001, 2026-11-22):
  three distinct identities (definition / version / instance) with `kind/name` as
  the uniqueness key, `name` immutable once published, lifecycle state in the data
  layer with audited transitions, and retirement as a tombstone that keeps
  existing pins resolvable while blocking new pins and new runs. Six states:
  `draft · published · active · paused · retired · obsolete`. ADR:
  `architecture/adr-agent-identity-lifecycle.md`.
- **D16** Execution provider contract (P7-RUN-001a, 2026-11-22): nine operations
  (`start · status · cancel · pause · resume · step · sleep · history ·
  capabilities`), `step` as the only durability primitive, at-least-once delivery
  with mandatory idempotency keys, six run states plus a separate `rejected` for
  admission refusals, capability advertisement that refuses rather than silently
  downgrades, a fixed error taxonomy, and durable-state vs best-effort-events
  tiers (metering reads durable results only). No provider ships without the
  C1–C18 conformance suite. ADR:
  `architecture/adr-execution-provider-contract.md`.
- **D17** **No dedicated staging environment (2026-11-22).** Vibe runs one control
- **D18** **Unified integrations on one contract (2026-11-22).** MCP, Skills,
  OpenAPI imports, builtins and future Component artifacts all converge on one
  Tool Manifest vocabulary and one execution gate (`mcp.Gate`) behind
  `backend/pkg/mcp` — each kind lands as a **loader**, never as a new engine or
  a new auth plane (ADR: `architecture/adr-unified-integrations.md`). The
  research behind it is `ecosystem/19-integration-research.md` (proposals 1–7).
  Consequences and owners: P1-AGT-004 finishes the MCP surface; P7-SKL-001
  (Skill Loader, agentskills.io frontmatter + token budget + `allowed-tools`),
  P7-OPI-001 (OpenAPI Importer, owner + version pin) and P7-SCH-001 (dynamic
  `search_tools`) are the next loader tasks; WASM Components stay sandboxed
  execution (D13 unchanged), not a protocol; the Edge carries registry caches
  and `Code Mode`-style publication (D12 unchanged), never grants.
  plane: production, plus a locally-deployed plane for security proofs
  (`docker-compose.yml`, boundary on, real Edge KV id). A second Render
  environment was costed (paid Key Value required) and declined — the security
  value was in *the proof matrix*, not in a second host, and the matrix already
  runs against production and locally. Consequences: `render.yaml` is the only
  blueprint (`render.staging.yaml` was removed with this decision); the
  `deploy-staging.yml` workflow keeps pointing at the production control plane as
  a Worker preview; anything provable only on a separate host stays explicitly
  **unproven**. Resolves O21.

## New open questions (all unresolved)

O14 commerce protocol versions; O15 WASM interface choice; O16 GPU provider
model; O17 node trust economics; O18 marketplace liability; O19 model-license
matrix; O20 Assistant/Research evaluation datasets.

**O14 status (2026-11-22):** partially answered by the P6-COM-001 research log in
`13-agentic-commerce.md` — UCP publishes date-based protocol versions and AP2 is
at `v0.2`; the exact UCP version we pin, and whether the Embedded Protocol
binding is needed, remain open. Two new questions were opened by that research:
**O22** (is Vibe Commerce a UCP *Host* or a *Platform*?) and **O23** (who is the
Credential Provider, and is legal review needed for managed payment flows?).


**O21 — dedicated staging environment — RESOLVED by D17 (2026-11-22).**
No staging environment is provisioned. `render.yaml` declares one web service plus
one Key Value, and a Render workspace allows one Free Key Value, so a second
environment needed a paid instance; that spend was declined because the security
proof is a matrix (401 / 403 / 404 / 503 / 200), not a second host, and it runs
against production and against a locally-deployed plane (`docker-compose.yml`).
Kept for traceability: the O21 investigation is what produced the local
deployed-plane proof.

**O24 — credential-vault key rotation (opened 2026-11-22).** Stored provider
tokens are sealed with AES-256-GCM under a key derived from `JWT_SECRET`, so
rotating that secret makes every existing token unreadable and forces every user
to re-authorise. That is a safe failure (fail closed, no plaintext leak) but it
must be a documented operational decision: choose between accepting mass
re-authorisation, adding a key-version header plus a dual-read window, or moving
to a dedicated vault key binding. Unblock: an ops decision plus a migration plan;
no code change is authorised before that. Also open: whether the generic
user-secrets store (`worker/services/secrets/`, currently unreachable) becomes the
one vault or stays a reference implementation.

**O25 — registry trust depth (opened 2026-11-23).** Vibe mirrors the official
MCP Registry read-only (proposal 4 of `ecosystem/19-integration-research.md`),
but "how good is that mirror's word?" stays open: whether D1 stores full
`server.json` documents or hashes, how often the cache refreshes, whether
package digests are pinned, and whether a revoked/renamed upstream server
propagates or tombstones locally. Unblock: the `P1-AGT-004` publish-flow design,
before any `Verified` claim depends on cached provenance.


