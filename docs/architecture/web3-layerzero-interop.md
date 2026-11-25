# LayerZero Interop — Modular Cross-Chain Messaging (PROPOSED, RESEARCH-GATED)

> **Status:** `proposed` 2026-11-25. No LayerZero code, config, or dependency
> exists in the tree. Protocol architecture below is **verified against the
> official V2 documentation on 2026-11-25** (sources table at the end); the
> adapter design itself is still PROPOSED and unimplemented.
> Companions: `web3-chain-abstraction.md`, `web3-security-signing.md`.
> Decision: `adr-web3-layerzero-adapter.md` (PROPOSED). Task: `W0-WEB3-01`
> (DONE 2026-11-25, research plus documentation only).

## Verified protocol architecture (2026-11-25)

Official wording: LayerZero is an omnichain interoperability protocol that
provides "a stable, immutable interface for crosschain messaging", built so
that separating interface, verification, and execution into independent
layers allows composable crosschain architectures "without compromising
security" (`layerzero-protocol-architecture`, read 2026-11-25).

Five independent layers, each with one job:

1. **Business Logic Interface (OApp)** — the application contract that defines
   custom payload bytes and calls the protocol.
2. **Protocol Interface (Endpoint)** — the stable entry point every OApp
   integrates with.
3. **Configurable Protocol Libraries (Message Libraries)** — per-channel send,
   receive, and read libraries; "push" messaging uses Send/Receive ULN
   workflows, "pull" messaging (request/response) uses `lzRead`.
4. **DVNs (Decentralized Verifier Networks)** — independent verifiers whose
   attestations satisfy the channel's verification configuration.
5. **Executors** — worker services that deliver verified messages to the
   destination application.

Channel model (verified): communication is per OApp-to-OApp channel with
channel identification, **channel-specific nonce tracking**, message packet
generation, and per-channel configuration. Verification is expressed as
X-of-Y-of-N coordination (the docs illustrate a 2-of-2-of-2 and a
1-of-2-of-3 arrangement), so the security threshold is an explicit,
reviewable configuration — not an implied default.

Message lifecycle (verified entrypoints): `send(...)` on the source OApp →
transport and routing through Endpoint, libraries, DVNs, Executor →
`lzReceive(...)` on the destination OApp, which must confirm the sender is
the configured peer.

Fee handling is part of the OApp contract surface: quote, collect, and refund
**native and ZRO fees** (`oapp-reference`, read 2026-11-25). Consequence for
Vibe: a cross-chain route has a real per-message cost in two asset types;
quoting must be surfaced to the user before approval, never absorbed
silently.

Troubleshooting surfaces documented by the provider and therefore required in
our adapter design: debugging messages, **chain reorgs, and nonce recovery**.
Reorg plus nonce recovery means "sent" is not "delivered" and delivery is not
automatically reversible — matching the retry/recovery stance below.

## OApp and OFT (verified scope)

**OApp** (Omnichain Application) is the standard for any contract that sends
and receives crosschain messages; every OApp implementation must handle
message sending, message receiving, fee handling, peer management, and
channel management plus security (`oapp-reference`, read 2026-11-25).

Deployment facts (verified): an OApp is deployed on each chain it operates on;
initialization integrates the **local Endpoint V2 address** via constructor or
initializer, which also makes the OApp an Endpoint delegate so it can call
Endpoint security methods; peers are then registered **directionally** with
`setPeer(eid, peerAddress)` on each side, and the receiving OApp checks
`peers[srcEid] == origin.sender` before processing. Trust is directional by
construction.

**OFT** (Omnichain Fungible Token) enables one fungible token across many
chains while preserving a **single global supply**; the transfer debits the
source chain (burn or lock in escrow) and credits the destination chain (mint
or unlock) after the message is delivered, with a shared-decimals /
decimal-conversion-rate rule to avoid dust drift
(`oft-reference`, read 2026-11-25). The docs distinguish **Direct** (the token
contract itself is the OApp) from **Adapter** (a separate adapter contract
holds the existing token) deployment patterns.

Vibe reading: OApp is the right template for arbitrary cross-chain business
logic; OFT is the right template when the asset must keep one global supply.
Both are candidate marketplace templates, not platform features. Vibe does not
claim OApp/OFT conformance until the adapter's own tests run against a pinned
version.

## Position

LayerZero is an interoperability provider behind a generic interface, never
a dependency of System IR, the core API, or any non-omnichain app. An
application without cross-chain routes ships zero LayerZero artifacts.

## Generic interoperability interface (planned)

`sendMessage(route, payload, options) · estimateFee(route, options) ·
getMessageStatus(id) · retryMessage(id) · listRoutes(app)`.
Provider adapters (starting with `layerzero-v2`) implement it; the app only
sees routes, payloads, and delivery states (`created · in-flight ·
verified · delivered · failed`).

## Interop manifest (planned, separate from Contract IR)

Per omnichain app: app id, source and destination chain refs, peer addresses
(trust relationships declared explicitly), message routes, per-route security
config, executor/relayer choice, gas policy, retry policy, and event
subscriptions. LayerZero-specific fields (Endpoint IDs, DVN lists, executor
settings, option blobs) live only in the `layerzero-v2` section — never in
the generic core.

## Lifecycle and operations

Source send → transport and routing (Endpoint, message libraries, DVNs) →
destination receive. The platform normalizes provider callbacks into platform
events (candidate names only; the envelope rule stays with `event-envelope.md`):
`crosschain.message.sent`, `crosschain.message.verified`,
`crosschain.message.delivered`, `crosschain.message.failed`. Failed delivery
retries through the adapter with explicit policy; the docs never promise
automatic reversal of executed destination effects, and the provider itself
documents chain reorgs plus nonce recovery as first-class failure modes — so
our status model must distinguish "sent", "verified", "delivered", and
"needs manual recovery" without collapsing them. Monitoring keeps
provider-native payloads alongside the normalized event.

## Security rules (normative for any adapter)

- Peer and DVN configuration are reviewable deployment settings; AI output
  may propose them but may never silently weaken them.
- The verification threshold is explicit, never inherited: X-of-Y-of-N is
  recorded per channel, and a weaker-than-required set is refused rather than
  accepted as a default.
- Directional peering is mandatory on both sides; a one-sided `setPeer` is an
  incomplete deployment, not a working route.
- Fee quoting (native plus ZRO) is shown before approval; a route whose fee
  cannot be quoted is not submitted.
- Mainnet cross-chain enablement needs the same approval flow as mainnet
  deployment (see `web3-security-signing.md`).
- Trust assumptions (who verifies, who executes, what finality is assumed)
  are recorded per route before mainnet use.

## Sources (read 2026-11-25)

| Source | URL | What it established |
|---|---|---|
| Protocol architecture | `https://docs.layerzero.network/v2/concepts/layerzero-protocol-architecture` | Five independent layers; channel model; nonce tracking; X-of-Y-of-N verification |
| OApp reference | `https://docs.layerzero.network/v2/concepts/technical-reference/oapp-reference` | OApp responsibilities; Endpoint integration; directional `setPeer`; `send`/`lzReceive`; native plus ZRO fees |
| OFT reference | `https://docs.layerzero.network/v2/concepts/technical-reference/oft-reference` | Single global supply; debit/credit flow; shared decimals; Direct vs Adapter |

Still to pin before adapter code: the exact SDK package versions and the
supported-chain list for the chains we actually target (O32 stays partially
open for versions and chains; architecture is no longer open).
