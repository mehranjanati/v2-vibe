# Agentic Commerce — UCP, AP2, A2A, MCP (Canonical)

> **Status:** `adopted` 2026-11-21. Product: `ecosystem/01-product-ecosystem.md`.
> Protocol research log (P6-COM-001): verify section below (2026-11-22).

MCP connects tools and context (clients, discovery, permission controls);
A2A carries delegation between agents and external platforms; UCP is the
dedicated commerce adapter (discovery, cart, checkout, merchants — validate
the official spec before implementation); AP2 governs payment authorization
with limits, human approval, auditability, merchant requirements;
OpenAI-compatible APIs keep model interop without provider lock-in; WASM/WIT
evaluated for portable components. Each adapter independently versioned with
conformance tests; no compliance claimed without passing tests. Commerce
agents enforce approval policies for every payment side effect.


## Protocol research log — P6-COM-001 (2026-11-22)

**Scope:** verified facts about the two official specifications, the adapter
boundary they create, and what Vibe must *not* claim. No adapter code exists in
this repository; `Vibe Commerce` stays `RESEARCH` until the gates below pass.
Sources read 2026-11-22: the UCP specification repository
(`Universal-Commerce-Protocol/ucp`, Apache-2.0) and the AP2 specification
repository (`google-agentic-commerce/AP2`).

### UCP — Universal Commerce Protocol (RESEARCH)

- **What it is:** an open standard for agentic commerce covering discovery →
  cart → checkout → order management across consumer surfaces. The business
  remains **Merchant of Record** and keeps its own business logic.
- **Actors:** *Business* (seller), *Buyer* (end user), *Host* (agent app, super
  app, or browser), *Embedded Context* (business UI in an iframe/webview).
- **Versioning is date-based, not semver.** The spec's own error example reads
  `Protocol version 2026-01-12 is not supported. This business supports versions
  2026-01-11 and 2026-01-23.` Our manifests use semver
  (`architecture/manifest-schemas.md`), so an adapter maps an external
  date-version to an internal adapter version — the two schemes must never be
  presented as equivalent.
- **Discovery and negotiation are two steps.** The business advertises its
  current protocol version plus profiles for older versions; the platform selects
  **one exact version**; the business then derives the active capability set from
  the intersection. The profile document lives at `/.well-known/ucp` and is
  cacheable by both parties.
- **Capability names are reverse-domain:** `{reverse-domain}.{service}.{capability}`
  (e.g. `dev.ucp.shopping.checkout`, `dev.ucp.shopping.fulfillment`,
  `dev.ucp.common.identity_linking`). Each capability MUST declare a `schema` URL
  whose origin matches its namespace authority — and the spec states plainly that
  this proves **provenance, not trust**, the same rule our MCP slice already
  applies (discovery grants nothing).
- **Transports:** REST, MCP, and an *Embedded Protocol* (JSON-RPC 2.0 over an
  iframe/webview channel); A2A is documented as a business integration path. UCP
  therefore binds MCP/A2A rather than replacing them.
- **Two-layer error model:** application errors travel inside the `result`
  envelope (`ucp.status`) while transport failures use JSON-RPC/HTTP codes.
  `continue_url` lets an agent hand the buyer back to the normal web flow, and
  message signatures plus `Content-Digest` cover tamper evidence.
- **Idempotency is mandatory for writes** — reusing a key with a different
  payload is a `409`.

### AP2 — Agentic Payment Protocol (RESEARCH)

- **What it is:** `v0.2`, securing *agent-performed payments*. It is explicitly a
  **security feature inside a commerce protocol**, not one itself: catalog APIs,
  checkout updates, and role-to-role commerce calls are out of scope, and it is
  designed to compose with UCP.
- **Five roles:** *Shopping Agent* (discovery, checkout, purchase — expected to
  be agentic), *Credential Provider* (issues/scopes payment credentials),
  *Merchant* (verifies the agent may buy these items; owns inventory, pricing and
  discount integrity), *Merchant Payment Processor* (verifies the credential is
  scoped to this checkout), *Trusted Surface* (captures informed consent — **MUST
  be non-agentic**).
- **Two linked artifacts, not three.** A **Checkout Mandate** plus Checkout
  Receipt JWT (what is purchased) and a **Payment Mandate** plus Payment Receipt
  JWT (the payment). Mandates are *open* (with constraints) or *closed*, secured
  as **SD-JWT** (selective disclosure), with other verifiable-credential formats
  left as an extension point.
- **Two human flows:** *Human Present* ("direct") and *Human Not Present*
  ("autonomous") — the latter is exactly where our approval policies and
  irreversible-side-effect rules must bind.
- **Dispute evidence is a hash chain:** the checkout receipt reference must match
  the closed Checkout Mandate hash, the payment mandate is verified against the
  checkout's `checkout_hash`, and the payment receipt reference must match the
  closed Payment Mandate hash.
- **Extension points:** mandate constraints (type + schema + evaluation
  algorithm), checkout object, payment instruments, VDC formats.


### Adapter boundary this creates for Vibe

| Layer | Owner | Vibe's obligation |
|---|---|---|
| Discovery, cart, checkout, order | UCP adapter (Vibe Commerce) | Implement one pinned date-version; never imply another works |
| Payment authorization and mandates | AP2 adapter | Preserve mandate constraints; require approval for `Human Not Present`; retain receipts for audit |
| Delegation to external agents | A2A adapter | Typed calls carrying tenant/user/agent identity |
| Tool and context connectivity | MCP adapter (already gated) | Reuse the existing trust boundary; UCP/MCP discovery still grants nothing |
| Internal system definitions | System IR / manifests | Unchanged — external protocol versions never leak into IR fields |

### What Vibe must NOT claim

- No UCP or AP2 compliance, conformance, or certification until our own tests run
  against the pinned versions.
- No "supported versions" list beyond the one version we actually exercised.
- No payment-rail, custody, or settlement claims: AP2 secures *authorization
  evidence*; money movement stays with the credential provider/processor.
- No commerce capability listed in any marketplace before permission mapping,
  approval policy, and audit are enforced.

### Ordered gates before any commerce adapter code

1. Pin one UCP protocol version and one AP2 version; record both here with the
   verification date.
2. Map UCP capability permissions onto the existing Tool Manifest vocabulary
   (side-effect level, approvals, `vault:` secret refs) instead of inventing a
   second permission language — the rule the MCP slice already follows.
3. Decide the approval model for `Human Not Present` flows: which side-effect
   classes may never auto-approve.
4. Build a stub business server plus a stub credential provider for conformance
   tests; no live merchant or payment credentials in CI.
5. Only then admit `P6-COM-*` implementation tasks.

### Open questions carried forward

- **O14 (partially answered):** the published shapes are date-versioned UCP and
  `AP2 v0.2`; which exact UCP date-version we pin — and whether we also need the
  Embedded Protocol binding — stays open.
- **O22 (new):** does Vibe Commerce act as a *Host* (embedding business UIs via
  the Embedded Protocol) or as a *Platform* calling REST/MCP only? That answer
  decides whether we must ship iframe CSP and `credentialless` handling.
- **O23 (new):** who is the Credential Provider for the managed Cloud offering,
  and does any managed payment path create money-transmission or licensing
  obligations that need legal review before launch?
