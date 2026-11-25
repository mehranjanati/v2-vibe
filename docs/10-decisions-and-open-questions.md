> **Ecosystem consolidation (2026-11-21):** this file is preserved with its
> decisions D1–D9 intact. New ecosystem decisions D10–D14 appended at the end
> (product-independence rule, one-ecosystem contract set, Cloudflare-as-provider,
> wasmCloud non-production, no-token MVP). New open questions O14–O20 appended
> alongside O1–O13 (all unresolved).


# Decisions & Open Questions (AGENTIC PLATFORM)

> **Scope:** the decision log for the VibeSDK Agentic Platform strategy
> reframe, and every unresolved question with its evaluation criteria.
> Vision: [`00-product-vision.md`](00-product-vision.md). Canonical strategy:
> [`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md).
> Register of
> technology candidates: [`04-system-architecture.md`](04-system-architecture.md).
>
> **Status:** `adopted` — decisions D1–D9 are strategy direction adopted
> 2026-10-06 through 2026-11-18 (documentation only; no implementation).
> D1–D6 history is preserved verbatim below; D7 records the Agentic Platform
> shift, D8 the M1 scope freeze and D9 the M0 slice scope. Open questions O1–O8
> are explicitly **unresolved — none may be treated as finalized**, plus new
> platform opens below. Last
> reviewed: 2026-11-18.

## Adopted decisions (strategy direction)

| ID | Decision | Rationale / consequence |
|---|---|---|
| D1 | **Wedge = AI Sales (lead qualification + follow-up + CRM update + reporting).** Supersedes the "no vertical SaaS in sales" clause of [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md) strategic non-goals. | Sales impact is the fastest measurable/validatable revenue outcome and seeds CRM → automation → Business OS. The clause is revised by this strategy, not silently dropped. |
| D2 | **ERP + full AI suite + Marketplace + broad Automation = destination architecture, NOT MVP scope.** | Prevents the parallel build-out failure mode; the roadmap is dependency-ordered. |
| D3 | **Outcome-first interaction: users request outcomes; workflow/node design stays an inspection surface.** | Continues the existing thesis; also rules out a visual workflow editor in the MVP. |
| D4 | **Multi-tenant by design; tenancy is a launch requirement of the Agent Builder (M0), not later hardening.** | Product contract for engineering P0. |
| D5 | **Vendor replaceability; no orchestration vendor owns business data.** | Dify (if adopted) orchestrates only; the data layer stays platform-owned. |
| D6 | **AaaS-only; legacy SaaS ends; Web3 is next plan (M5–M6).** No new seat-based SaaS features; every capability ships as a versioned manifest; Web3 enters only via the marketplace trust and settlement gates. Strategy: [`11-aaas-web3-strategy.md`](11-aaas-web3-strategy.md); ADR: [`architecture/adr-aaas-only-end-of-saas.md`](architecture/adr-aaas-only-end-of-saas.md). | Kills margin inversion and commoditization; preserves platform focus; defers wallet, gas, and token risk until proven demand. |
| D7 | **Agentic Platform as primary product identity (2026-11-15).** VibeSDK is an Open Agentic Platform: Natural Language First, Visual Builder for developers, one Canonical System IR across NL/Visual/SDK/CLI/API, Open Core + Managed Cloud direction. Supersedes the Business OS / AI Sales wedge positioning as product identity only (D1 history preserved; AI Sales retained as optional reference template). Retires the M0-M5 wedge roadmap in favor of the M0-M6 platform roadmap; P0-P6 engineering IDs unchanged. Canonical strategy: [`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md). | The wedge proved too narrow as identity; the platform (builder / teams / runtime / ecosystem / ops / marketplace / factory) is the durable product. One IR prevents two divergent agent models. |
| D8 | **M1 scope freeze until O1 evidence (2026-11-18).** While O1 is unresolved, **no new** team / routing / coordination capability is admitted: multi-agent composition beyond the existing primitive, routing breadth beyond the already-wired coder slice, MCP transport, and the P2 capability catalog all stay closed. In scope while frozen: the P0 rollout (the M0 launch gate), the M0 product surface, and finishing slices that are already open. | D7 makes the M0 Agent Builder the only sellable increment, and [`02-mvp.md`](02-mvp.md) lists multi-agent teams and delegation as must-NOT-build for M0; O1 requires ICP/problem/WTP evidence before any M1 scope lock. Without this freeze the "one narrow primitive" reading of M0 silently turns into M1 product work on an unvalidated ICP — the parallel build-out failure [`03-product-roadmap.md`](03-product-roadmap.md) forbids. Lifting it is itself a decision and needs O1 evidence recorded here. Scope of the freeze in the backlog: [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md) header rule. |
| D8.1 | **MCP transport PoC admitted as an owner-directed amendment to D8 (2026-11-22).** `P1-AGT-003` was executed on explicit owner instruction, which is what lifts a freeze ("lifting it is itself a decision"). Numbered as an amendment so it sits next to the freeze it narrows and cannot collide with the ecosystem series in [`ecosystem/10-decisions-and-open-questions.md`](ecosystem/10-decisions-and-open-questions.md) (D10–D17). Scope is deliberately narrow: the stdio `Transport` adapter plus five tests against a spawned stub server, **behind** the existing trust boundary. No route, no agent caller, no registry/grant persistence, no UI — so **no user-facing MCP capability is enabled** and D8's product-level intent (no unvalidated M1 capability surface) is unchanged. What remains closed by D8 until O1 evidence: authenticated MCP routes, the SSE binding, the generic credential store, and any agent run that can call an MCP tool — all tracked as `P1-AGT-004`. | Without recording this, the repository would hold code that contradicts its own decision log — the exact drift the docs gates exist to prevent. The exception is bounded and reversible (delete the adapter and the boundary is untouched); the freeze on *enabled* capability stays. |
| D8.2 | **MCP SSE transport binding admitted as a second owner-directed amendment to D8 (2026-11-24).** Scope is exactly one binding behind the existing `Transport` interface: `backend/pkg/mcp/sse.go` (HTTP+SSE stream, `initialize` handshake, one deadline per exchange, caller-supplied headers, no redirects, `https` only except a loopback host) plus six transport tests against a stub HTTP+SSE server — including "a grantless call never dials". Like D8.1 it enables **no user-facing capability**: no route, no UI, no agent caller, no registry/grant persistence, and **no credential store** — the adapter never resolves a secret, it only sends headers the caller already resolved. What stays closed until O1 evidence: authenticated MCP routes (discover/grant/revoke/execute), the generic credential store, and any agent run that can call an MCP tool (all still `P1-AGT-004`). | The SSE binding was a design row with no code — the "contract nothing implements" failure D8 prevents on the *capability* side and D8.1 already admits on the *transport* side. Two bindings on one interface are what make the gate transport-agnostic instead of stdio-shaped, and the freeze on enabled capability is unchanged; the amendment is reversible by deleting the adapter, exactly like the stdio PoC. |
| D8.3 | **MCP route surface admitted as a third owner-directed amendment to D8 (2026-11-24).** Scope: six session-guarded routes — `GET /api/mcp/servers`, `GET /api/mcp/servers/:server/tools`, `GET`/`POST /api/mcp/grants`, `POST /api/mcp/grants/:grantId/revoke`, `POST /api/mcp/call` — that call `mcp.Gate` with the tenant taken from the session, never from the body. Deliberately narrower than `P1-AGT-004` as originally written: the SSE binding is the **only routable transport** (a `stdio://` registration answers 409 instead of spawning a process), there is **no registration route** (registration is platform configuration; persistence stays with P7.6), and the **generic credential store stays closed** — a policy listing `vault:` references is refused with 503 before a transport is built. Grants are issued only against a reviewed `ToolPolicy` looked up server-side, so a body cannot widen permissions; registry/policy/grant stores remain in memory. | "Prove it or do not claim it": the Gate had no HTTP surface, so the boundary existed only in unit tests and every MCP capability claim (including ADI-CTX-003's dependency on a grants surface) rested on code nothing could call. This admits the smallest surface that can be exercised end to end while keeping D8's product intent — no registration, no credential, no process spawn — and is reversible by deleting the routes. |
| D8.4 | **Generic credential store admitted as a fourth owner-directed amendment to D8 (2026-11-24).** Scope: one server-encrypted store (`vault_secrets`, one row per tenant+name, migration `0020`) written through four session-guarded routes (`GET /api/vault/status`, `GET`/`POST /api/vault/secrets`, `DELETE /api/vault/secrets/:name`). Values are AES-256-GCM envelopes (`enc:v1:`) with the **same key derivation** as `worker/light/tokenVault.ts` and AAD binding `vault:<tenant>:<name>`, so both planes speak one format and one review covers both. No route returns a value; resolution happens only inside a granted MCP call and becomes the transport's `Authorization: Bearer …` header (one ref per policy — two refs are refused because no per-secret header mapping exists). The SPA's zero-knowledge `/api/vault/{config,setup,reset}` contract is deliberately **not** adopted. | "A server secret is stored as a `vault:` ref and resolved only at call time" cannot be satisfied by a store nothing can reach; this is the smallest design that keeps plaintext out of D1, out of responses and out of logs, reuses the already-reviewed envelope construction, and leaves the Edge GitHub vault untouched. Reversible: delete the routes and drop the table. M1's team/routing/catalog scope stays frozen. |
| D9 | **M0 vertical slice v0 scope adopted (2026-11-18).** The first executable M0 increment is the slice in [`02-mvp.md`](02-mvp.md): NL request → **single** agent → governed run → lineage row + one internal Git revision → History/diff/rollback, and its four open sub-decisions are settled as: (1) the agent **is** the existing generator — authoring is the next slice; (2) the session/tenant boundary is **mandatory**, so the slice is blocked on the P0 rollout (no boundary-off demo); (3) the History tab counts as M0's "visual inspection" **for v0 only** — M0 stays incomplete until editing exists; (4) the 403 proof uses a **throwaway account on staging, never production**. | Answers O13 and gives D8's "in scope" clause a measured boundary: the slice's seven acceptance criteria are the scope test, so "is this in scope?" stops being a judgement call. Scope only — nothing is built, no new dependency, no schema change, no new route; the code already on `main` is what the slice exercises. It is built **after** the P0 rollout, because its governance criterion cannot be met with the boundary off. |

## Open questions (unresolved — do not claim as decided)

| ID | Question | Evaluation criteria / notes |
|---|---|---|
| O1 | Final ICP and problem validation evidence. | Phase M0 exit gate ([`01-problem-and-icp.md`](01-problem-and-icp.md)) — interviews + WTP evidence before any M1 build scope lock. |
| O2 | Frontend (SvelteKit vs live React SPA), identity (Appwrite vs live D1 auth), database (TiDB vs D1/Redis), generated-app builder (Bolt/bolt.diy vs live pipeline), function runtime (Bun where appropriate). | Each needs a trade-off ADR against the live implementation; rewrite only with explicit migration cost/benefit. Candidates are recorded, not chosen ([`04-system-architecture.md`](04-system-architecture.md)). |
| O3 | Tenant data-model granularity (shared schema + tenant column by default; when is a tenant-specific model justified?). | Guiding constraint: never assume every tenant needs a custom database ([`07-multitenancy-security.md`](07-multitenancy-security.md)). |
| O4 | Durable execution + gateway choice (Rivet or equivalent vs live Cloudflare Workflows; FastAPI vs Rivet gateway vs live Go Fiber). | Decide together with the runtime interface boundary; P3 checklist items are the first slice. |
| O5 | AI orchestration (continue Eino-first vs adopt Dify for parts). | Boundary rule: Eino is an implementation dependency, never the canonical public Agent model; agents must remain composable either way; pilot evidence from M0 decides. Model routing/failover is settled as a *composition* (Eino failover primitive + VibeSDK policy/guards): [`architecture/adr-model-failover-composition.md`](architecture/adr-model-failover-composition.md). |
| O6 | Pricing / willingness-to-pay for the platform. | Hypotheses in [`01-problem-and-icp.md`](01-problem-and-icp.md); price only after M0 evidence. Runs + evaluated outcomes + marketplace take-rate direction (D6); never seats or raw tokens. |
| O7 | Web3 chain selection (which L2, custody, gas abstraction). | Gated to M5; needs ADR with cost, latency, and vendor-neutrality criteria. No chain is chosen by this strategy. |
| O8 | Web3 settlement currency (fiat vs stablecoin mix, escrow design). | Gated to M5 pilot; needs ADR with regulatory and treasury review. No token in scope. |

## Conflicts resolved by reframes

0. **Business OS wedge identity vs Agentic Platform identity** — resolved by D7 (2026-11-15): the platform (M0-M6) is the product identity; the AI Sales wedge is preserved as decision history (D1) and an optional reference template, never the mandatory first product.
1. **Vertical-SaaS non-goal vs sales wedge** — resolved by D1 (explicit
   supersession, recorded above).
2. **Builder-first ICP vs business-buyer wedge** — resolved: the wedge
   buyer is the business ICP from M0; the builder ICP remains the long-term
   platform/ecosystem audience ([`PRODUCT_THESIS.md`](PRODUCT_THESIS.md)).
3. **Proposed candidate stack vs live stack** — resolved by the register in
   [`04-system-architecture.md`](04-system-architecture.md): live stack is
   truth, candidates are options pending ADRs; no rewrite is decided.
4. **Legacy SaaS model vs outcome-first AaaS positioning** — resolved by D6:
   seat-based SaaS is retired (no seat metrics, no new standalone SaaS pages)
   and Web3 is deferred to the M5/M6 trust and settlement gates
   ([`11-aaas-web3-strategy.md`](11-aaas-web3-strategy.md)).

## Rule

Any future ADR that resolves O1–O12 must update this log and the linked
documents in the same change — unresolved questions must never be quoted
as finalized decisions.

## New platform opens (D7 — unresolved)

| ID | Question | Evaluation criteria / notes |
|---|---|---|
| O9 | Canonical System IR schema + versioning/compat/migration strategy. | Minimal viable schema tested against four cases (App Builder, single agent, multi-agent team, visual workflow) before expansion; ADR with alternatives and trade-offs. Tracked as P7.2 in [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md); IR v1 contract + ADR landed 2026-11-16 ([`architecture/system-ir.md`](architecture/system-ir.md), [`architecture/adr-system-ir-v1.md`](architecture/adr-system-ir-v1.md)), schema enforcement awaits the compiler. |
| O10 | Agent/Tool/Skill/System manifest specs + compatibility rules. | Must stay consistent with the live agent model and capability registry direction; validation + import/export required. Tracked as P7.1 in [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md); schemas + spec landed 2026-11-16 ([`architecture/manifest-schemas.md`](architecture/manifest-schemas.md)), enforcement awaits the compiler. |
| O11 | Runtime Adapter interface + Run State Machine + event/checkpoint contracts. | At-least-once + idempotency explicit; never claim exactly-once without proof; Workflows limits analyzed first. Tracked as P7.5 (+ P7.7 trace) in [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md). |
| O12 | Open-core boundary + license + managed-cloud packaging. | No component assumed open source; license analysis before code; self-hosting strategy required. |
| O13 | ~~**M0 vertical slice v0 scope**~~ — **resolved by D9 (2026-11-18)**, kept for traceability. | Scope: [`02-mvp.md`](02-mvp.md) "M0 vertical slice v0". D9 settled the four sub-decisions (generator as the v0 agent; boundary mandatory; History tab = visual inspection for v0 only; throwaway staging account for the 403 proof). **Not built**: implementation sequences after the P0 rollout, because the governance criterion cannot be met with the boundary off. |
