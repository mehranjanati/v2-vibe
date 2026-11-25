# Web3 and Network Economy (Canonical, optional)

> **Status:** `adopted` 2026-11-21; Web3 application direction added
> `proposed` 2026-11-25 (architecture only, no code). History:
> `11-aaas-web3-strategy.md` (preserved).

Web3 is an optional coordination layer for Vibe Network, never a product
prerequisite. Internal accounting independent of chain; settlement interfaces
chain-agnostic; no token in MVP; no speculative tokenomics; demand validated
first; legal/regulatory/tax/sanctions/securities review before any launch; no
fundraising claims on appreciation; viable without a token. Future scope:
wallets, settlement, usage accounting, escrow, staking evaluation, reputation,
governance, token-utility research.

## Web3 application direction (PROPOSED 2026-11-25, architecture only)

Alongside the settlement-layer role above, the platform gains an
**AI-native Web3 application direction**: describe, compose, generate,
validate, deploy, operate, and monetize on-chain systems through AI
workflows plus reusable ecosystem capabilities. Positioning:

- Platform identity stays a general-purpose agentic platform; Web3 is a
  strong product direction, not a rebrand.
- Commercial entry point: an AI-powered Smart Contract and Omnichain
  Application Studio (Contract Studio plus Web3 Operations).
- The platform is never a LayerZero-only dashboard, a Solidity-only code
  generator, a custody platform by default, or a single-chain platform.

Product boundaries (proposed, to reconcile with `02-product-boundaries.md`):

- Vibe Core: identity, tenancy, projects, contracts, policies, APIs, events.
- Vibe Agentic Platform: agents, teams, capabilities, tools, skills, MCP,
  memory, workflows.
- Vibe Contract Studio: contract specification, generation, compilation,
  testing, security analysis, deployment preparation.
- Vibe Web3 Operations: chain connectivity, wallet integration, transaction
  lifecycle, deployment records, monitoring, cross-chain operations.
- Vibe Marketplace: distribution of agents, skills, tools, contract
  templates, integrations, reusable capabilities.

Architecture (all PROPOSED, no code): Smart Contract IR
(`architecture/web3-smart-contract-ir.md`) separate from System IR;
EVM-first chain abstraction (`architecture/web3-chain-abstraction.md`);
LayerZero as a modular adapter (`architecture/web3-layerzero-interop.md`);
reproducible toolchain pipeline (`architecture/web3-toolchain-pipeline.md`);
wallet-first signing with mandatory invariants
(`architecture/web3-security-signing.md`). Decisions W3-IR, W3-EVM, W3-CHAIN,
W3-LZ, W3-TOOL, W3-SANDBOX, W3-SIGN, W3-VER are PROPOSED or OPEN in
`architecture/decision-status-register.md`; none is accepted. Commercial
MVP and monetization are outlined in
`ecosystem/14-product-and-business-model.md` (Web3 section, proposed).

## What Vibe Web3 is not

- Not a LayerZero-dependent platform: apps without omnichain routes carry
  zero LayerZero artifacts.
- Not a custody platform by default: wallet-first, managed signing only as
  an explicitly ADR-gated future option.
- Not a single-chain platform: EVM-first is an implementation order, not an
  architecture limit.
- Not an audit substitute: generated code plus passing tests never count as
  a security audit.

