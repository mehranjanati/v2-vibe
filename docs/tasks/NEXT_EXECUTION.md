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

Next unblocked work: **`ADI-CTX-003` is DONE (2026-11-25)** — `backend/pkg/activation`,
the eligibility core as a pure `Decide()` (deny-overrides, default-deny,
E_ACTIVATION_ESCALATION + audit, budget-before-bound, ranking intersection,
canonical record), 12 test functions green incl. -race. It is a library: no
route, no store, no caller, no clock, so D8 stays untouched. The next prepared
task is `ADI-CTX-004` (persist the decision record); beyond that the live
options are:
(a) run the Cloudflare adapter against a deployed workflow to close P7.11's live
half — the harness is now one command (`go test -tags live ./pkg/execution/
-run TestCloudflareLive`), the adapter's endpoint/body shapes were corrected
against the published schema on 2026-11-25, and only a credential with
`Workers Scripts Write` is missing (both tokens on this host answer 401 /
code 9109), or
(b) a real WASM/container `Sandbox` behind `toolsearch.CodeMode`.

The P7.12 proof that used to sit between them is DONE (2026-11-24): the eight
route-test functions in `backend/pkg/api/workflows_control_test.go` are green
locally over the emulated D1 + Workflows API (401 incl. capabilities, 403 +
audit, 404 unknown/foreign, 409 on every terminal state, 502 with no write on a
runtime refusal, transition-guarded pause/resume, defaulted cancel reason), and
the cancel-finality + capability-refusal cases now exist in
`worker/workflow/VibeWorkflow.test.ts` — they run in CI because workerd cannot
start on macOS 12.7.6.

Done 2026-11-25 (P7.11 adapter, code + harness, live run still gated): reading the
published REST method schemas and cross-checking wrangler 4.90.0 (cloudflare SDK
5.2.0) showed the adapter had invented its lifecycle endpoints. It now creates
instances with `instance_id` plus a JSON-encoded `params` string, edits lifecycle
through `PATCH .../instances/{id}/status`, detects duplicates by the documented
code `10405`, and maps the three non-terminal schema statuses (`waiting`,
`waitingForPause`, `rollingBack`) to `running`. The stub rejects the old shapes,
and `cloudflare_live_test.go` (`-tags live`) is the honest proof: it refuses to
claim C1–C7/C10/C15/C17/C18 (workflow-body semantics), asserts zero HTTP calls for
admission refusals, and skips with a reason when the instance finishes before
pause/cancel can be observed. The live run is **not** done: both credentials on
this host are invalid (`.env` token → 401 code 10000; wrangler OAuth → code 9109),
so the test skips by design until a `Workers Scripts Write` token is supplied.

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

Done 2026-11-23 (run control): **`P7-RUN-001c`** — `capabilities`/`cancel`/`pause`/
`resume` on both planes (`backend/pkg/api`, `backend/pkg/engine`,
`backend/pkg/cloudflare`, `worker/workflow/`), terminal states made final in the
runtime's own UPDATEs so a terminated run cannot come back as `failed`, and a DAG
that needs an unadvertised guarantee refused before the first step. The Go half
builds and vets clean and `docs:check` passes with the four new routes; the worker
half is CI-verified only (workerd cannot start on this host).

Done 2026-11-23 (ADI-FOUND-001, audit only): `docs/architecture/ir-parity-audit.md` — a two-sided probe (ten documents through `ir.Load` and through the P7.1 schema) proved Go is the lenient side in 8 of 10 cases (name/node-id length, semver leading zeros, missing `outcome`, agents without topology, approval enum, `evaluation.dataset`, duplicate edges), that cycles and reachability are unenforced on both sides at the IR boundary, that `Canonicalize` accepts trailing content while `Load` rejects it, and that duplicate JSON keys are last-wins. No application code changed; probe files removed.

Done 2026-11-23 (ADI-FOUND-001a): the parity gaps are closed in both readers — 63-char caps, schema-grade semver, required `outcome`, agents-imply-topology, approval vocabulary, pinned `evaluation.dataset`, duplicate edges, **cycle detection**, **reachability as an error**, trailing content and duplicate keys in `Canonicalize`. Evidence: 13 named probes in `backend/pkg/ir/system_test.go`, 17 in the gate, and a shared positive control that guards against over-rejection; re-running the ADI-FOUND-001 matrix gives REJECTED 10/10 on both sides. No schema change, no version bump, no runtime change.

Done 2026-11-23 (ADI-FOUND-002, design only): `docs/architecture/composition-lock.md` is now a normative contract — artifact field table plus forbidden fields, worked example from the M1 fixture, digest and canonical-byte rules built on the ADI-FOUND-001a hardening, three resolution modes and what each one actually proves, an eight-step deterministic resolver algorithm, an eight-code fail-closed failure taxonomy, runtime snapshot semantics, admission-time verification, a lockVersion/migration table, and nine negative acceptance tests handed to the next task. No code, no store, no route.

Done 2026-11-23 (ADI-FOUND-003, design only): `docs/architecture/composition-resolver.md` — the resolver as a pure function: `AssetSource` with six normative obligations, three mode adapters and what each honestly proves, eight-step algorithm with a sorted worklist, diamond-vs-cycle distinction, the `E_INCOMPATIBLE_VERSION` rule, determinism obligations plus six traps, an eleven-code precedence order, a 21-case test specification, and a package plan. The lock contract grew six failure codes to match.

Done 2026-11-23 (ADI-FOUND-003a, code): `backend/pkg/composition` resolves a validated System to an immutable lock in bundled mode — `ParseRef` reusing the IR grammar, `AssetSource`/`BundledSource`, edge extraction with the `skill.compatibility` exclusion, the secret denylist, `Canonical()`/`Digest()` through `ir.Canonicalize`, the eight-step algorithm with a sorted worklist, and eleven failure codes with precedence. 24 tests pass (P1-P6, N1-N12, D1-D3, G1) including `-race`, with a golden 9-entry M1 lock. Running it against real data corrected two premises: the bundled example set is not self-closing (P3 now asserts `E_MISSING`) and topology array order is semantic, so only object-key order may vary (D2).

Done 2026-11-23 (ADI-FOUND-004, design only): `docs/architecture/run-snapshot.md` — one snapshot per run, written before admission, identified by the lock digest, append-only, content-deduplicated; a ten-field stored record with no grants or secrets; the storage question measured on real data (the M1 golden lock is 2,930 bytes for 9 entries) and left open as O27; five admission checks with their codes; retention/GDPR rules; migration as a transformer by `snapshotId`, never an in-place re-resolution. It states plainly that `workflow_instances` and `generations` have **no** snapshot column today, so no run is snapshot-bound yet.

Done 2026-11-23 (ADI-FOUND-004a, storage + admission logic): `composition_snapshots` + nullable `snapshot_id` on `workflow_instances` (migration 0017, purely additive, no backfill), `composition.Snapshot`/`SnapshotStore`/`Verify`/`AdmitRun` with `E_DIGEST`/`E_SNAPSHOT_MISSING`/`E_TENANT`, and a D1 adapter whose insert is `ON CONFLICT DO NOTHING` with **no update path at all**. 13 new tests (7 snapshot, 6 store) pass; typecheck and eslint clean after the schema change.

Deliberately **not** claimed: live enforcement. No request carries a snapshot id yet, so an unbound run still starts. That is ADI-FOUND-004b.

Done 2026-11-23 (ADI-FOUND-004b, live wiring, opt-in): `EngineHub.SetSnapshotBinding` + `AdmitRunSnapshot` (no-op without a store, `SessionVerifier` idiom), optional `snapshotId` on the trigger admitted **before** `CreateWorkflowInstanceRow`, binding recorded as `NULLIF(?, '')`, codes mapped to 403/503/409, and `CONTROL_PLANE_REQUIRE_SNAPSHOT` read in `cmd/main.go` and documented in `DEPLOY_RENDER_CLOUDFLARE.md`. 7 route-level tests assert that a refused run leaves zero instance rows and zero runtime instances.

Two honest limits, both recorded in `architecture/run-snapshot.md` §13:
- **Nothing produces snapshots yet.** The run path has no System to resolve (workflow DAGs come from `workflow_dags`, not from IR), so enabling the switch today refuses every run. The missing link is the IR-to-run binding already listed as an open item in `ecosystem/06-runtime-and-execution-architecture.md`; a producer task cannot honestly be scheduled before it.
- `/api/outcomes/trigger` is not registered yet; when it lands it must admit exactly like the workflow trigger.

Done 2026-11-23 (ADI-CTX-001, design only): `docs/architecture/context-assembly.md` — assembly as a pure function from a normalized task/event plus the composition snapshot to one envelope: closed `kind` and `scope` vocabularies, the structural rule that knowledge, memory and profile can never be merged or relabelled, precedence preserved rather than resolved, the 8-candidate / 600-token / policy-budget bounds with a deterministic drop order (mandatory segments are refused, never truncated), honest determinism (explainable replay across a changing retrieval index, not a false claim of bit-identical replay), and six failure codes. The umbrella document keeps a summary plus pointer.

Done 2026-11-23 (ADI-CTX-002, design only): `docs/architecture/activation-policy.md` — eligibility (deterministic, narrowing only), ranking (probabilistic, ordering only), and the gate as call-time authority; the invariant `activated ⊆ ranked ⊆ eligible ⊆ candidates ⊆ closure` with a code per violated link; deny-overrides and default-deny; the rule that activation may require an approval but can never record one; a per-candidate decision record where every decision is attributable; and a closed selector vocabulary taken from `backend/pkg/mcp` rather than invented.

Done 2026-11-23 (ADI-MEM-001, design only): `docs/architecture/memory-provider.md` — three operations only (`Remember`/`Recall`/`Forget`; a correction is forget-plus-remember so provenance stays honest), the record shape with source/producer/version and optional expiry, the scope containment ladder with a read/write/forget authority matrix (tenant is a wall, not a filter; scopes may only narrow; no cross-subject reads), the `inferred` never-overwrites-`explicit` rule, the secret denylist on write, hard bounded recall with total ordering, retention where `Forget` must return the real count, and a `Capabilities` advertisement so a lexical provider says `SemanticRecall: false`. It records the evidence: the existing Redis index covers VFS *files* with a hash embedding, and there is no memory table — so nothing is replaced and no database is added.

Done 2026-11-23 (ADI-MEM-003, design only + ADI-MEM-002 closed as absorbed): `docs/architecture/personalization-profile.md` — the profile is explicitly **not authorization**; three key namespaces replace the umbrella's single ladder (`constraint.*` platform/tenant only, `default.*` beaten by an explicit user entry, `preference.*` where explicit beats every inferred value); inferred entries require evidence and confidence and are refused as `E_PROFILE_CONFLICT` against an explicit key; resolution yields one deterministic winner per key with a shadowed list kept in the trace, not the prompt. Its own recommendation is that **inferred preferences do not ship in the first slice** — proving they help needs an evaluation dataset. `ADI-MEM-002` (scope enforcement) was closed as delivered inside `memory-provider.md` §3-§7 rather than as a duplicate document.

Done 2026-11-23 (ADI-EVT-001, design only): `docs/architecture/event-envelope.md` — producer-assigned `eventId` with a materialised `dedupeKey`, closed category and type rules (type never carries PII), required tenant, `source` distinct from `subject`, **both** `occurredAt` and `receivedAt` so lateness survives, versions decoupled from the payload schema ref, at-least-once delivery with "a duplicate is a success, not an error", per-correlation best-effort ordering only, and an explicit refusal to define payloads (that would be a second manifest system). No broker, queue or table: Kafka/NATS/Temporal need measured need plus an ADR. The dangling reference in `context-assembly.md` §2 is now concrete.

Done 2026-11-23 (ADI-EVAL-001, design only): `docs/architecture/evaluation-contract.md` — evaluation judges a **snapshot** (so an asset update is a new subject), the five layers are closed with an honest automation matrix (L1 and L2 automated, L4 partly, L3 and end-to-end L5 with no pipeline), `not_evaluated` never counts as `pass`, composite scores are banned so a failing safety metric cannot be averaged away, gates fail closed with `blocked` propagating, and an LLM judge may inform L3 only. Five failure codes, ten tests, five open items (O20 datasets still open).

Done 2026-11-23 (ADI-EVAL-002, design only): `docs/architecture/benchmark-runner.md` — the batch job that turns a gate into a result artifact. Its central finding comes from the snapshot contract: a snapshot holds `systemDigest` and per-asset ref+digest but **no bytes**, so re-evaluation must be handed the System document plus an `AssetSource` and verify both; L2 therefore *verifies* the stored closure instead of re-resolving (no registry exists), which is a different check rather than a weaker one. Also fixes four exit codes (0 pass / 1 fail / 2 incomplete / 3 contract violation) so CI can distinguish worsening software from unmeasured software from a bad request, and per-case status so a case that could not run is never counted as a pass.

Done 2026-11-23 (ADI-EVAL-003, design only): `docs/architecture/candidate-lifecycle.md` — **a candidate is a snapshot, a stable composition is a pointer**, so the acceptance rule holds structurally: there is no write path to a stable composition. Closed state machine with a guard per transition, promotion as compare-and-set on `(tenantKey, systemRef, environment)` with `expectedCurrentSnapshotId` (concurrent promotions surface `E_CAND_CONFLICT` instead of silently losing one), evidence valid only for `(snapshotId, dataset digest, evaluatorVersion)`, un-promoted candidates expire, and promotion changes only the default for new runs.

Done 2026-11-23 (ADI-EVAL-004, design only): `docs/architecture/promotion-and-rollback.md` — promotion is a four-condition conjunction (passing gate for the target environment, required approvals, unexpired candidate, compare-and-set), approvals bind to the evaluation artifact digest and expire with the candidate, rollout runs on environment pointers because no traffic-splitting layer exists, and **rollback is split from emergency pin**: rollback targets a snapshot that previously held this pointer and needs no new evaluation, while emergency pin targets any verified snapshot with a named decision and marks the pointer until cleared. Every pointer movement writes exactly one `audit_logs` row, and a movement that cannot be audited does not happen.

Done 2026-11-23 (ADI-EVT-002, design only): `docs/architecture/durable-event-delivery.md` — a transactional **outbox** written in the same transaction as the fact it describes, a durable **inbox** whose unique `(tenant, dedupeKey)` insert *is* the dedupe, and a dispatcher whose conditional-`UPDATE` claim is safe because D1 is single-threaded. Grounded in the repository's own limits (`CF_LIMITS.md` §8): rows at 2 MB, statements at 100 KB/100 binds, queries at 30 s, and 500 MB shared on the current Free plan — which is why the capacity arithmetic (~7 weeks at 10k events/day) makes sweeping a correctness rule, and why inbox retention must outlive the retry window or a duplicate becomes a second execution. Loop protection via `causationId` depth and per-correlation backpressure. No broker; the honest threshold that would justify Cloudflare Queues is recorded as a future ADR trigger.

The evaluation/evolution line is complete on paper (EVAL-001 through EVAL-004), and events now have both semantics and mechanism. Done 2026-11-23 (ADI-EVT-003, design only): `docs/architecture/event-to-activation.md` — the last mile from a delivered envelope to a run. Subscriptions live in the **data layer**, not the IR (a `spec.subscriptions` block would need an ADR and would disturb D7), so an event-triggered run resolves through the **stable pointer** and therefore uses exactly what production uses. `triggerable` is a property of the binding, not of the event; the eligibility filter is deterministic and model-free; run de-duplication is **per binding** via a database-unique `(binding_id, dedupe_key)` - the inbox marks *received*, never *started*. The event can supply input, correlation and context; it cannot choose capabilities, set the tenant, satisfy an approval, bypass the gate or smuggle a composition, and the payload is a **mandatory** context segment so an over-budget payload refuses instead of running without its input.

Done 2026-11-23 (ADI-MEM-004, design only): `docs/architecture/context-retrieval.md` - retrieval order mirrors the reverse of assembly drop order (profile, memory, knowledge) so a cheap least-droppable source is never crowded out; one bounded query, no LLM expansion in v1; `providerKind` recorded so the current lexical `HashEmbedding` knowledge provider is **labelled lexical** rather than implying semantic recall; bounds enforced twice; out-of-scope items refused (`E_CONTEXT_SCOPE`) instead of filtered; degradation for optional providers.

**Every ADI design task that can be closed without an open decision is now closed (37/45).** What remains is deliberately blocked, not pending:

- `ADI-CTX-003` is DONE 2026-11-25 (the eligibility core exists as a pure library); `ADI-CTX-004` (persist the record) is the remaining trace piece. Neither has a caller yet: no run today carries a snapshot or an envelope, so the engine stays a library until a caller is decided.
- `ADI-ECO-001..004` stay blocked on the registry storage shape (O27), provenance depth (O25) and vault rotation (O24). Opening them without those decisions would be the speculative infrastructure the mission forbids.
- `P1-AGT-004` is DONE 2026-11-24 (D8.2/D8.3/D8.4); `ADI-CTX-003` is DONE 2026-11-25 on top of it. The next owner gate is a caller for activation (ADI-CTX-004 + a run that carries an envelope).

Done 2026-11-23 (ADI-MEM-001a, first memory code): `backend/pkg/memory` (types, Provider, validation, scope ladder, total ordering, lexical score, content ids, secret scan) plus `backend/pkg/memory/d1store` (D1 adapter over a two-method Querier, `ON CONFLICT DO NOTHING`, bounded recall, real forget counts, `NewWithoutTTL` so the refusal path is reachable) and additive migration `0018` with a `memory_records` table. 11 tests green; `go vet`, the whole backend suite, `typecheck` and `eslint` all clean. The storage decision is recorded in `memory-provider.md` §10: **D1 with `expires_at`**, revisited only on a measured latency problem. Honest limits: lexical only (`SemanticRecall: false`), no route, no agent caller, no embedding model.

Done 2026-11-23 (ADI-EVAL-002a, second code task): `backend/pkg/evaluation` implements the runner — `Evaluate`, `ExitCode` (0 pass / 1 fail / 2 incomplete / 3 contract violation), `Canonical` and `Digest` with the clock excluded. **L1** re-runs `ir.Load` on the supplied System document, **L2** verifies the stored closure (exact pins, one version per identity, cycles over `requestedBy`), and **L3/L4/L5 report `not_evaluated` with a precise reason** rather than pretending. 13 tests green; the whole backend suite, `go vet` and `docs:check` clean. Two contract refinements came out of it: the document/snapshot digest match is a **subject** check (refusal, exit 3) rather than an L1 finding, and the injected absolute `deadline` replaces a wall-clock read so budget exhaustion is reproducible.

Done 2026-11-23 (ADI-FOUND-005, design only): `docs/architecture/ir-to-run-binding.md` closes the open item the runtime architecture has carried since the provider contract. **The procedural shape compiles 1:1 today** — the IR node kinds are the same seven the DAG interpreter runs, and `next[]` maps onto edges — while **the agentic shape is refused with `E_BIND_SHAPE`** rather than lowered into a chain that would discard delegation and topology order. The document fixes the compile contract, the field-by-field mapping (including `when` carried verbatim and approvals mapping onto the four-class vocabulary), deterministic Kahn ordering, three additive run fields with the argument that a plan is recomputed rather than stored, and five refusal codes.

Done 2026-11-23 (ADI-FOUND-005a, code): `backend/pkg/plan` compiles a verified snapshot plus its System document into a plan — Steps/DependsOn for the provider and Edges/When for the interpreter from one mapping, deterministic Kahn ordering, five refusal codes, and agentic Systems refused by design. 10 tests green; vet and the full backend suite clean. The tests forced one real correction: the IR's `when` belongs to the **target** node, so the condition had been read from the wrong side.

Done 2026-11-24 (the snapshot producer, `ADI-FOUND-004c`): the flow the
binding design named — resolve → persist → compile → start — is wired on the
live trigger path. `POST /api/workflows/trigger` accepts an optional inline
`system` document; the handler resolves it against the bundled catalog
(`backend/pkg/composition/bundle`, the vetted example manifests embedded with
`go:embed`, parity-tested against the docs originals), persists the snapshot
and compiles the plan before anything durable is written, then admits the
fresh snapshot through the same `AdmitRunSnapshot` guard as a caller-supplied
id. Migration `0019` adds the three plan columns of `ir-to-run-binding.md`
(`plan_kind`, `plan_digest`, `compiler_version` on `workflow_instances`),
written only when a plan was compiled. 12 route tests + 2 bundle tests green
(`-race` clean); vet, the full backend suite, `typecheck`, `lint` and
`docs:check` clean. Honest limits: the bundled catalog is the example set, so
a System referencing anything outside it refuses with `E_MISSING` until a
registry exists (ADI-ECO-002, O27); the zero-ref procedural example System is
the end-to-end proof; binding and producing are different intents and are
refused together; `CONTROL_PLANE_REQUIRE_SNAPSHOT` is now satisfiable and
stays off by default.

Next steps, in dependency order (2026-11-24 audit):
1. **`P1-AGT-004`** — **DONE 2026-11-24** (D8.2/D8.3/D8.4): SSE binding (`backend/pkg/mcp/sse.go`, 7 tests), session-guarded routes (`backend/pkg/api/mcp.go`, 7 route tests, 6 routes) and the generic credential store (`backend/pkg/vault` + 4 `/api/vault/*` routes, migration `0020`, 10 tests) with call-time resolution into the MCP transport. `ADI-CTX-003`'s grants dependency is satisfied; starting it is the next owner gate.
2. **`ADI-CTX-003`** — **DONE 2026-11-25**: `backend/pkg/activation`, the eligibility core as a pure `Decide()` (deny-overrides, default-deny, closure invariant with E_ACTIVATION_ESCALATION + audit, budget-before-bound, ranking intersection, canonical record with decidedAt excluded). 12 test functions incl. -race; the import scan proves no `backend/pkg/mcp`/exec/SQL in non-test code, so D8 is untouched. Evidence matrix: `architecture/adi-implementation-evidence.md`.
3. **`ADI-CTX-004`** — **DONE 2026-11-25** (store; no caller): `activation.TraceStore` + `ValidateRecord` (`backend/pkg/activation/trace.go`), the D1 adapter `backend/pkg/activation/d1store` and additive migration `0021` (`activation_decision_traces`): one append-only row per run, replay is a no-op, a different record for the same run is refused, a corrupt row is refused on read, and an unattributable decision never reaches D1. 4 trace tests + 11 store tests green with `-race`. Nothing calls it: no assembler (ADI-CTX-001) and no policy source exist, so no row is produced yet.
4. **Agentic execution** — PROPOSED ADR `architecture/adr-agentic-execution.md` (options: new `agent` node kind vs topology-aware executor vs keep refusing; lowering rejected by default). Deliberately not presumed; never NEXT without a decision.
5. **`ADI-ECO-001..004`** — BLOCKED on O24/O25/O27 decisions; the bundled catalog is the honest stand-in until the registry lands.

History (superseded): ADI-FOUND-001a was originally scoped as validation tightening under the existing contracts — no new capability, so D8 was unaffected, and ADI-FOUND-002 was made to depend on it because a lock cannot be deterministic over two different validity answers. That reasoning held; the work is now DONE.

Stash triage (2026-11-25, both stashes dropped after review): two long-lived stashes
were inspected before they could rot. `docs-checkpoint-interim` (18 files, docs plus
`go.mod`/`go.sum`) and `preexisting-worktree` (18 files, code plus docs) both branched
from `9ee23fa`, which is an ancestor of `main`. Method: for every changed file the
stash's added lines (>= 45 chars) were tested for presence verbatim on `main`, and the
code changes were confirmed by symbol — P0.8 `SetLifecyclePolicy`/`EnsureRoom`/
`StartJanitor`, P1.3.3 `takeGenerationIntake`/`genForked`/`generationCloseSQL`,
`go-git` in `backend/go.mod`, the `code?: string` error field in
`src/services/controlPlaneClient.ts`, and the `ROOM_QUOTA_EXCEEDED` path all exist on
`main`. Result: 100% of the code and >= 95% of the doc lines already landed; every
remaining line is superseded wording (old `2026-11-14` headers, `planned` statuses that
later became `verified`). Nothing was recovered because nothing was lost. The two
snapshots are archived as pushed branches `wip/docs-checkpoint-interim` and
`wip/preexisting-worktree` for anyone who wants to diff them; the local stash entries
are gone.


ADI note (2026-11-23, superseded 2026-11-24): the 23 ADI-FOUND/CTX/MEM/EVT/EVAL/ECO tasks were PROPOSED design work, all BLOCKED on dependencies/decisions (lock/resolver, grants surface, durable idempotency, registry/provenance). None enters NEXT_EXECUTION until unblocked; D8/D9 gates unchanged.

## Web3 track (parallel, MVP-independent, no code)

`W0-WEB3-01` **DONE 2026-11-25** — official LayerZero V2 docs read and
recorded (five layers, channel plus nonce model, OApp peer rules, OFT
debit/credit, X-of-Y-of-N verification, native plus ZRO fees); O32 answered
for architecture, SDK versions and target chains still open.

`W0-WEB3-02` **DONE 2026-11-25** — Foundry and Hardhat 3 compared from
official docs; recommendation (Foundry primary, Hardhat 3 compatibility path)
recorded PROPOSED in `architecture/adr-web3-toolchain.md`; O31 answered for
capability comparison, release pins still open.

Next in the Web3 track: `W0-WEB3-03` **DONE 2026-11-25** — owner decision D22
accepted `W3-IR` and `W3-VER`, and Contract IR v1 shipped as schema plus two
canonical fixtures plus the sixth docs validator (`scripts/validate-contract-ir.mjs`,
eight negative probes; `bun run docs:check` runs six gates).

`W1-WEB3-01` **DONE 2026-11-25** (design only, no generator) —
`architecture/web3-solidity-generation-scope.md` settles the three acceptance
items. Scope: eight in-scope kinds mapped onto existing Contract IR v1 fields
(escrow/vault, treasury splitter, fixed-supply ERC-20, role-minted ERC-20,
ERC-721, ERC-1155, access-controlled registry, UUPS registry) and eleven named
exclusions with reasons (AMM/lending math, governance, bridge/OApp generation,
diamonds/4626/4337, oracles, Yul and assembly, formal verification, tokenomics,
non-EVM, fee-on-transfer/rebasing/permit in v1, gas golfing); a proxy needs
`risksAcknowledged: true` and every payable function's policy must be one of four
authority shapes. Pinned set: `@openzeppelin/contracts` **5.6.1** (MIT) — tarball
SHA-256 `bd202ef9a2fd09f6005edc18437b4d3090841d0f676c01c814ee9608581d2e63`, npm
shasum plus SHA-512 integrity, and **17 allowed entry points with digests computed
locally** (`shasum -a 256`) and observed pragmas (`^0.8.20`; UUPS at `^0.8.22`),
governed by six pin rules (closure resolved inside the tarball, digest mismatch is
a hard failure, upgrades are deliberate). Templates: IR skeleton plus acceptance
list plus NatSpec outline plus module-set pin — never generated source or framework
scaffolding; identity `publisher/tpl-<kind>@semver`; lifecycle
`draft → reviewed → pinned → deprecated` with pin-means-frozen,
deprecation-never-deletes, no-`latest`, security-change-is-major, and template
version independent of IR version. Toolchain stays **unbound** (`W3-TOOL` is still
PROPOSED): no template names Foundry or Hardhat, and `go.mod`/`package.json` were
not touched.

`W1-WEB3-02` **DONE 2026-11-25** (design only, no compiler or sandbox added) —
`architecture/web3-compilation-and-artifacts.md`. Compiler pin: solc
**0.8.39+commit.ce47f2c8**, per-platform SHA-256/keccak from the official
`list.json` (the macosx-amd64 binary was downloaded and hashed locally and matched
`6134d77c…ef51`), plus the npm `solc` channel. Settings profile: `evmVersion` always
comes from the IR and defaults to `cancun` — measured trap: 0.8.39 defaults to
`osaka`, which the IR enum forbids; optimizer explicit; `bytecodeHash: ipfs` by
default, `appendCBOR: false` only with `bytecodeHash: none` (0.8.39 rejects the
combination); `libraries` empty. Twelve reproducibility rules (R1–R12), four of them
measured rather than assumed: the same content under `Contract.sol`,
`src/Contract.sol` and `./Contract.sol` yields **three different bytecodes**; the
same sources under `ozpkg/package/` vs `vendor/oz-5.6.1/` yields different bytecodes
because metadata records resolved paths — the **vendor root name is a build input**;
content-supplied sources are working-directory independent while an absolute
remapping target breaks under `--base-path`; and solc metadata carries **no
timestamp**, so no `SOURCE_DATE_EPOCH` workaround is needed. Artifact manifest
`vibesdk-artifact/v1` defined (contract ref + irDigest + templateRef, `build.id` from
the canonical Standard JSON input, sourceDigest, closureDigest, full compiler
identity, settings, module pin, digest-addressed outputs incl. metadata IPFS CID and
storage layout, `reproducibility.verified` only after a double build in two
independent instances, `sandbox.keysPresent: false`), with timestamps/hosts/user ids
deliberately excluded because they belong to lineage records keyed by `build.id`.
Sandbox boundary fixed: inputs (IR, content sources, digest-verified tarball,
profile, digest-verified compiler) → outputs (artifacts, manifest, secret-free log);
network, keys, deployment, control-plane writes and out-of-workspace filesystem
denied. No framework is bound (`W3-TOOL` stays PROPOSED).

`W2-WEB3-01` **DONE 2026-11-25** (design only, no gate implemented) —
`architecture/web3-security-gates.md`. Eleven gates G0–G10, each with an objective
**Pass** and **Fail** list and, where the IR has the field, derived from it: the IR
`tests[].kind` enum (`unit`, `integration`, `fuzz`, `invariant`, `static-analysis`,
`simulation`) maps 1:1 onto G2–G7, so the required set is machine-readable. G0 scope
admission, G1 build reproducibility (consumes the W1-WEB3-02 manifest), G2 unit,
G3 integration, G4 fuzz, G5 invariant, G6 static analysis, G7 simulation preview,
G8 provenance and traceability, G9 deployment policy and approvals, G10 secret and
key hygiene; required-versus-optional is fixed per risk class (standard versus high,
the classes the signing doc already defines).

The no-audit rule is **structural**, not a disclaimer: the verdict vocabulary is
`not_run | failed | passed | waived | not_applicable` with a mandatory `bound` per
passing gate and deliberately no `safe` value; `gates_passed` is true only when every
required gate passed (a waiver never counts); there is no `production_ready` field;
the pipeline **may never write** `security.auditStatus` (IR enum
`none|reviewed|audited`) and G8 **fails** an `audited` claim with no resolvable
external record; and a forbidden-label list bans “audited/secure/safe/production-ready”
plus “verified” used as a synonym for audited (`source_verified` is reserved for
on-chain source verification, with its own label and link).

Evidence: one digest-addressed artefact per gate (`admission.json`, the manifest,
`test/integration/fuzz/invariant-report.json`, `static-analysis.sarif` +
`dispositions.json`, `simulation.json`, `provenance.json`, `policy-decision.json`,
`secret-scan.json`), canonical serialization, mandatory reference to the artifact
digest under test, never-overwrite retention, and a verdict that references artefacts
by digest so tampering breaks the reference instead of changing the story.

`W3-WEB3-01` **DONE 2026-11-25** (design only, no wallet or signer added) —
`architecture/web3-signing-and-deployment.md`. One lifecycle as nine stages
(intent → simulate → policy → approve → sign → broadcast → confirm → record →
verify), each with an **owner** and a **refuses-to-run** condition, so the boundaries
are authorization boundaries rather than function calls: the control plane builds,
simulates, checks policy and broadcasts but never approves or signs; the custody
holder approves and signs but never broadcasts or records; no component may perform
two of approve/sign/broadcast.

The three acceptance criteria are structural. **Distinct owners:** an actors table
with an explicit "may not" column plus the no-two-stages rule above. **Mainnet
approval mandatory:** a policy allow verdict is necessary but never sufficient —
`riskClass: high` also needs simulation evidence and a rollback/pause plan, testnet
auto-approval is limited to explicitly configured low-risk operation kinds and is
recorded with the policy version, and no config value makes mainnet self-approving.
**Agent keys refused:** the agent tool surface has no method that accepts or returns
key material, `TxIntent` has no key/seed/signature field, the agent may only propose
an intent, and the platform never asks a user to paste a key, mnemonic or keystore.

Also defined: the `vibesdk-txintent/v1` object with a canonical `intentDigest`
(approval binds to the digest, one chain, one nonce, one expiry — necessary because
EIP-712 explicitly leaves replay protection to the application), wallet/session rules
citing standards verified Final on 2026-11-25 (EIP-1193, EIP-6963, ERC-4361, EIP-712,
ERC-55 checksummed addresses), approval single-use and self-approval refusal, the
signing boundary that rebuilds the payload from the intent and verifies the recovered
address, broadcast idempotency by `(chainId, from, nonce)` with `included ≠ finalized`,
the append-only `vibesdk-deployment/v1` record (`source_verified` is explicitly not an
audit), tenancy/audit events, and a twelve-row failure/recovery matrix where a retry
never widens authority.

That closes the Web3 design chain through the signing boundary. Still awaiting owner
acceptance: `W3-TOOL` (Foundry primary, Hardhat 3 compatibility — evidence recorded),
`W3-LZ` (LayerZero architecture verified, adapter unimplemented), and the open items
this document defers (remote/QR wallet pin, managed signer **O34**, final
approval-policy schema, multisig verification method, per-chain confirmations, verifier
default). None of these blocks or reorders the P0–P7 engineering line above.

ADI audit (2026-11-24, HEAD `ad69c8b`; updated 2026-11-25): 32 ADI task IDs — 28 DONE (18 design-only DONE + 10 code DONE: resolver `backend/pkg/composition`, snapshot store + `Verify`/`AdmitRun`, live binding `ADI-FOUND-004b`, producer `ADI-FOUND-004c`, plan compiler `backend/pkg/plan`, lexical memory `backend/pkg/memory`, L1+L2 evaluation `backend/pkg/evaluation`, activation core `backend/pkg/activation`, decision trace `backend/pkg/activation/trace.go` + `d1store`), 4 BLOCKED (`ADI-ECO-001..004`). Evidence matrix: `architecture/adi-implementation-evidence.md` (every capability linked to file:line code, migration, test names, local `go test ./...` green + CI `go-test`, commit SHAs). Agentic execution is a PROPOSED ADR (`architecture/adr-agentic-execution.md`), not an implementation. `P1-AGT-004` DONE 2026-11-24 (D8.2/D8.3/D8.4); `ADI-CTX-003` DONE 2026-11-25; `ADI-CTX-004` DONE 2026-11-25 (store only — no caller, so no trace is produced on the live path). D8/D9 gates unchanged.
