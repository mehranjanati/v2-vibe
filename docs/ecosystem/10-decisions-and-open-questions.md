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
  Consequences and owners: P1-AGT-004 (DONE 2026-11-24) finished the MCP surface; P7-SKL-001
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

- **D22 — Smart Contract IR accepted (2026-11-25, docs plus schema only).**
  Adopt a versioned, canonical, **target-independent** Smart Contract
  Intermediate Representation with a **generic semantic core**
  (`spec.abstract`) and **explicit target-specific extensions**
  (`spec.targets` per VM, `spec.extensions` per vendor). EVM/Solidity is the
  initial implementation target. Contract IR stays **separate from System
  IR**, while System IR can compose and reference on-chain contract
  capabilities. Generated source is derived from the IR and must stay
  traceable to its specification, toolchain, and artifacts. Contract:
  `architecture/web3-smart-contract-ir.md`; schema plus fixtures:
  `architecture/contract-ir/`; ADRs:
  `architecture/adr-web3-ir-separation.md`,
  `architecture/adr-web3-contract-ir-versioning.md`; gate:
  `scripts/validate-contract-ir.mjs` (sixth docs validator, eight negative
  probes). Consequences: `W0-WEB3-03` DONE; no generator, compiler, or
  registry exists, and nothing on the live path changes.

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


**D19 — Adaptive Intelligence as shared capability (PROPOSED 2026-11-23).** ADI (compose/activate/personalize/evaluate/evolve) lives on existing contracts; no ninth product, no new IR, no new manifest kind in v1, no new runtime. Needs owner acceptance: `architecture/adr-adaptive-intelligence-boundaries.md`.

**D20 — Multi-provider execution lifecycle (ACCEPTED 2026-11-24, docs only).** Five layers with one job each (Control Plane authority; Eino orchestration; D16 provider lifecycle; placement; substrate/capability execution). D16 operations, states, error taxonomy and C1–C18 unchanged; no live Run migration without a checkpoint design. Record: `architecture/adr-multi-provider-execution.md`. Eino-substrate boundary accepted the same day: `architecture/adr-eino-orchestration-substrate.md`.

**D21 — WASM and Component execution strategy (ACCEPTED 2026-11-24, docs only).** WASM Components are sandboxed tool execution (never registry/grants/auth); Tool Manifest ↔ WIT stay associated but separate; `mcp.Gate` stays sole authority; full-Eino-in-WASM is RESEARCH_REQUIRED (Model B experiment, Model C preferred path). Record: `architecture/adr-wasm-component-strategy.md`. Index: `architecture/decision-status-register.md`.

**O15 — WASM interface choice (narrowed 2026-11-24, still open).** D21 fixes the roles (WIT = ABI, Manifest = identity, Gate = authority) but the exact WIT surface stays open until the PoC executes: `architecture/wasm-poc-spec.md` (WASM-01..15, all `NOT_RUN`), scoreboard `architecture/wasm-runtime-matrix.md`, feasibility `architecture/eino-wasm-compatibility.md`. No interface is pinned before measured evidence.


**O26 — Composition Lock exact shape (partially answered 2026-11-23).** The artifact shape, digest *format* (`<algorithm>:<hex>`), resolution modes, failure taxonomy and snapshot semantics are now normative in `architecture/composition-lock.md` (ADI-FOUND-002, design only). Still open: the digest algorithm pin (SHA-256 proposed), whether the lock digest covers `compilerVersion` (**O26a**), the registry storage shape (**O27**), and capability-cycle policy (**O29**). **O27 — Memory/personalization store shape (open).** Provider interfaces first; no new store without ADR. **O28 — Evaluation dataset catalog (open).** O20 stays open; no datasets invented here. **O29 — Capability-cycle policy (opened 2026-11-23).** Workflow cycles are refused outright (I5, ADI-FOUND-001a), but a capability graph cycle (`skill → tool → skill`, e.g. pagination) may be legitimate. v1 rejects and reports the path; whether recursion should be classified and allowed needs a real use case. Contract: `architecture/composition-lock.md` §17.

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
propagates or tombstones locally. Unblock: the publish-flow design (no task owns it yet; ADI-ECO-003),
before any `Verified` claim depends on cached provenance.

## Web3 open questions (all unresolved except where noted, opened 2026-11-25)

O30 wallet standard pin and session scoping (which connection standard, what
permissions a session grants, how revocation propagates). **O31 toolchain
comparison — answered 2026-11-25 by `W0-WEB3-02`**: Foundry and Hardhat 3
capabilities verified from official docs; recommendation (Foundry primary,
Hardhat 3 compatibility path) recorded as PROPOSED in
`architecture/adr-web3-toolchain.md`; still open are release pins, Hardhat
invariant coverage, scanner shortlist, and sandbox implementation. **O32
LayerZero documentation verification — answered 2026-11-25 by `W0-WEB3-01`**
for protocol architecture (five layers, channel plus nonce model, OApp, OFT,
verification threshold, fee assets); still open: SDK package versions and the
supported-chain list for target chains. O33 verifier default (which
source-verification service per chain profile). O34 managed-signer custody
(deferred; needs custody review plus ADR before any design). Remaining opens
gate their own implementation tasks only; none gates current P0–P7 work.


