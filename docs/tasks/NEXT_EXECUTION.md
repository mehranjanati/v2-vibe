# Next Execution (solo-developer order)

1. ~~P0-SEC-001 Enforce control-plane session boundary rollout~~ — **DONE 2026-11-22**
   (deployed-plane proof: 200 health, 401 anonymous HTTP + WS, 503 fail-closed on
   store outage; Worker now forwards the Edge session token to the control plane).
2. P0-SEC-002 Verify tenant isolation across planes
3. P0-SEC-003 Verify WebSocket authorization
4. P0-SEC-004 Harden credential vault and revocation
5. P0-SEC-005 Verify workflow tenant isolation

Deferred: P0-SEC-006 (dedicated staging control plane) is **OBSOLETE** by D17 —
there is one control plane (production) plus a locally-deployed plane for
proofs, so no staging environment will be provisioned. Then P1-AGT-002 (IR
compiler consumer), P1-AGT-003 (MCP, blocked on the generic vault), P7-RUN-001,
and P6-COM-001 (DONE 2026-11-22, research only).

Done so far: P0-SEC-001 (2026-11-22, boundary proven on a deployed plane,
fail-closed under store outage, plus the Worker session-token forwarding fix),
P0-SEC-003 (per-room WebSocket authorization), P0-SEC-004 (vault audit: the
revocation claim is now true — provider revoke with a timeout and an honest
`providerRevoked`; the generic store is unwired, which is why P1-AGT-003 stays
blocked), P0-SEC-008 (short-lived single-use tickets + Origin allow-list),
P0-SEC-009 (Edge cookie name + cookie CSRF rule), P1-AGT-001 (agent identity +
lifecycle + retirement contract and its ADR), and P7-RUN-001a (nine-operation
execution provider contract, capability advertisement, error taxonomy, and the
C1-C18 conformance matrix).

Remaining P0 work: none. All P0 security tasks are DONE and P0-SEC-006 is
OBSOLETE by D17 (one control plane; the boundary is proven against production and
locally).

Done in P1: P1-AGT-001 (identity/lifecycle/retirement contract + ADR), P1-AGT-002
(backend/pkg/ir — the Go IR consumer: typed Load + invariants I1–I6, canonical
serialization, 4 fixtures + 6 negative probes in parity with the docs gate), and
P1-AGT-003 (MCP stdio transport: a real spawned server round-trip through the
trust gate, with the grantless call refusing to spawn at all).

Next unblocked work: **`P1-AGT-004`** is still gated by D8 (authenticated MCP
routes + credential store need O1 evidence). The open, unblocked options are:
(a) run the Cloudflare adapter against a deployed workflow (needs an account +
credentials) to close P7.11's live half, (b) teach `worker/workflow/VibeWorkflow.ts`
the missing contract surfaces (cancel / pause / `capabilities()`), or (c) a real
WASM/container `Sandbox` behind `toolsearch.CodeMode`.

Done 2026-11-23 (loaders + search): **`P7-SKL-001`** — `backend/pkg/agentskills`, the
agentskills.io loader on the D18 contract: frontmatter validation with
folder/name agreement, `ResolveTokenBudget`/`BodyTokenBudget` progressive
loading, on-demand resources with traversal refusal, `Community` scripts denied
until `Verified`, and a pure `Bind` that can only narrow the tenant's live grants
(grantless calls refuse before the transport runs). **`P7-OPI-001`** —
`backend/pkg/openapiimport`, one OpenAPI operation → one pinned Tool Manifest:
mandatory allow-list, method→authority mapping, idempotency from the addressed
resource, credentials forced to `vault:` refs and stripped from the input schema,
canonical deterministic output, idempotent `Merge`, and `Policy()` onto
`mcp.ToolPolicy` so an ungranted call is denied before any HTTP.
**`P7-RUN-001b`** — `backend/pkg/execution`, the execution-provider contract made
executable: contract types + admission (`ValidatePlan`/`Admit`), the reference
`Fake` provider with durable-vs-best-effort state separation, and the C1–C18
conformance suite (18/18 on the fake; capability skips reported, never passed).
The Cloudflare adapter maps the nine operations onto the Workflows REST API and
refuses what it cannot keep, but is not verified live (needs a deployed workflow).
**`P7-SCH-001`** — `backend/pkg/toolsearch`, `search_tools` beside the manifests:
a catalog built from the two loaders' output, deterministic weighted ranking,
hard bounds on candidates (8) and slice cost (600 estimated tokens), an honest
`Missed` result instead of a return-everything fallback, grant verification that
still hands execution to `mcp.Gate`, and a Code Mode spike that cannot run
without an injected sandbox (no `os/exec` path exists in the package).

P7-RUN-001b (fake + Cloudflare adapters and the conformance suite) needs an
execution environment; it is not blocked on design.
