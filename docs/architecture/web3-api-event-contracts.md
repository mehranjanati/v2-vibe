# Web3 API and Event Contracts — Extension Plan (PROPOSED, NO ROUTES ADDED)

> **Status:** `proposed` 2026-11-25. No route, handler, table, or migration
> exists or is added by this document. It reserves names and rules so future
> API work stays consistent with the two-plane auth model and the event
> envelope. Route truth stays in `POSTMAN_COLLECTION_README.md` section 7;
> envelope truth stays in `architecture/event-envelope.md`.

## Placement rule

Control-plane routes own sessions, rooms, generation, deploys, and workflow
runs; the light Worker owns D1 auth plus GitHub export; unknown `/api`
paths answer JSON 404 or 503, never SPA HTML. Web3 routes follow the same
split: chain reads, generation jobs, and deployment orchestration live on
the control plane behind per-route session checks; wallet-session binding
and user-facing status live on the edge plane. No blanket `/api` auth gate
is introduced.

## Candidate API domains (names only, schemas after W3 acceptance)

Chains and networks; wallet connections; contract specifications;
generation jobs; compilation jobs plus artifacts; test and security-analysis
reports; deployments; transactions; contract instances; contract reads and
writes; cross-chain applications; routes plus configurations; messages plus
delivery attempts; monitoring plus alerts; approvals plus signing requests.

## Candidate events (names only, envelope rules apply)

`contract.specification.created`, `contract.generation.started`,
`contract.generation.completed`, `contract.compilation.completed`,
`contract.tests.completed`, `contract.security_analysis.completed`,
`contract.deployment.requested`, `contract.deployment.approved`,
`contract.deployment.completed`, `transaction.simulated`,
`transaction.approval_required`, `transaction.signed`,
`transaction.submitted`, `transaction.confirmed`, `transaction.failed`,
`crosschain.message.sent`, `crosschain.message.verified`,
`crosschain.message.delivered`, `crosschain.message.failed`.

Every event carries tenant scope, correlation ID, idempotency key, and
provider-native metadata alongside the normalized payload. Ordering is
best-effort unless a stream contract says otherwise; replay replays facts,
never re-signs.

## SDK boundary (planned, not implemented)

Future client surface groups: core plus auth context; systems and agents;
contract specs, generation, compilation, tests, security reports; chains
plus wallets; transaction simulation plus lifecycle; deployments plus
instances; events plus streaming; interop plus LayerZero operations.
Framework-independent; React-independent; generated versus handwritten
split decided when the first route ships.
