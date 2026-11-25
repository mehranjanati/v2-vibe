# Web3 Security and Signing — Authority Boundaries (PROPOSED, NORMATIVE RULES)

> **Status:** `proposed` 2026-11-25. No wallet, signer, or deployment code
> exists. The invariants below are normative for all future Web3 tasks.
> Companions: `web3-chain-abstraction.md`, `web3-layerzero-interop.md`,
> `web3-security-gates.md` (G0–G10 gates plus the verdict model; invariant 5 is
> enforced structurally there — signing refuses a deployment whose verdict is not
> `gates_passed`, and no gate may set an audit status),
> `web3-signing-and-deployment.md` (**the lifecycle this document's invariants are
> implemented in** — nine stages with distinct owners, the `TxIntent` digest binding,
> approvals, broadcast/confirm, and the deployment record).
> Decision: `adr-web3-signing-authority.md` (PROPOSED).

## Mandatory invariants

1. Agents never receive raw private keys by default.
2. Private keys, seeds, and signing secrets never enter prompts, agent
   memory, ordinary logs, or generated source.
3. Build, simulate, policy check, approve, sign, broadcast, and confirm are
   distinct lifecycle stages with distinct owners.
4. Mainnet deployment and high-impact transactions require explicit approval
   under configurable policy.
5. Passing generation or tests is never labelled an audit; passing tests
   never marks a contract production-ready.
6. Every sensitive operation produces an auditable record (who, what,
   policy version, approval refs, hashes — never secret material).
7. Tenants are isolated across wallets, signing sessions, deployment
   permissions, and secrets.

## Boundaries

- Wallet-first by default: non-custodial connection (for example
  WalletConnect-style standards; OPEN: pin the standard after evaluation).
- External and hardware signers are first-class; any managed signer is an
  explicitly optional future capability with its own ADR and custody review.
- Signing boundary: the only component that holds signing authority. Agents
  submit approval requests; the boundary enforces policy, collects human or
  multisig/timelock approval, signs, and broadcasts. No bypass path.
- Approval workflows: role plus policy based; testnet policy may auto-approve
  low-risk operations explicitly, mainnet never does by default.
- Multisig and timelock integration points are declared per deployment
  profile before mainnet use.

## Threat model (initial, to be expanded in the ADR)

- Malicious or buggy generated code and supply-chain dependencies.
- Prompt injection and compromised MCP/tool content reaching the pipeline.
- RPC manipulation, chain reorganization, transaction replay/replacement.
- Cross-chain misconfiguration (wrong peers, weak DVN sets, executor trust).
- Secret exfiltration via logs, artifacts, or error payloads.

## Risk classification

- Standard (testnet, low value): policy-checked, logged, reversible steps
  may proceed with recorded approval.
- High (mainnet, value movement, upgrades, cross-chain enablement):
  explicit human or multisig approval, simulation evidence, and a rollback
  or pause plan are required before signing.

## Open items

- Wallet standard pin and session-scoping rules.
- Managed-signer custody decision (deferred, ADR-gated).
- Approval-policy schema and multisig/timelock defaults.
- Secret-store choice and rotation story (ties to O24).
