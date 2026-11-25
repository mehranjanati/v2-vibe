# Web3 Stack + Infrastructure Readiness (MVP-independent track, 2026-11-25)

> **Status:** `proposed`. This is a readiness checklist, not a build order:
> nothing here starts Web3 implementation before the MVP gates, and nothing
> here changes runtime code, bindings, dependencies, or the deploy config.
> MVP truth stays in `docs/02-mvp.md`; Web3 phases stay in
> `docs/ecosystem/03-product-roadmap.md` (W0–W6 inside P9).

## Goal

From now on, every infra decision keeps a door open for the post-MVP Web3
track: EVM toolchain in an isolated sandbox, RPC access behind an adapter,
wallet-first signing outside the control plane, deployment records in D1,
monitoring events on the envelope. No chain code ships until W-gates pass.

## Readiness checklist (no code until W-gates pass)

1. Toolchain sandbox image: pinned `solc` plus **Foundry as the primary
   framework with Hardhat 3 as the compatibility path** (evidence in
   `architecture/web3-toolchain-pipeline.md`, decision `W3-TOOL` PROPOSED —
   owner acceptance still required before the image names a default),
   scanners, and artifact capture; runs as an untrusted worker with no
   secrets and no network egress by default.
2. RPC posture: provider endpoints only via env/secrets, never code; adapter
   interface drafted (W3-CHAIN) with failover plus health probes plus a
   confirmation-count policy per chain profile.
3. Signing posture: wallet-first; private keys never in prompts, memory,
   logs, or artifacts; approval flow designed (W3-SIGN) before any testnet key.
4. Data posture: deployment records plus signing audit extend the D1 lineage
   pattern; no new store without an ADR (O27 family).
5. Local dev parity: `docker-compose.yml` gains no chain service until a W
   task owns it; until then the readiness proof is docs plus interface
   drafts, never a running node in the default stack.
6. CI posture: Web3 gates run the same way current gates do — docs checks
   plus Go/TS suites — with stub adapters first (no live RPC, no live keys).

## What explicitly does NOT happen now

No `solc`/Foundry/Hardhat dependency, no RPC client, no wallet SDK, no
bridge SDK, no new binding, no new route, no new table, no mainnet anything.
