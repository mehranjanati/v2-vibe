# MVP Definition — Agent Builder (PROPOSED)

> **Scope:** the Agent Builder MVP (M0) of the Agentic Platform vision. ICP
> and problem hypotheses: [`01-problem-and-icp.md`](01-problem-and-icp.md)
> (must validate before build). Vision:
> [`00-product-vision.md`](00-product-vision.md). Roadmap position: phase M0
> in [`03-product-roadmap.md`](03-product-roadmap.md). Canonical strategy:
> [`12-agentic-platform-strategy.md`](12-agentic-platform-strategy.md).
> Agent/function/tenant
> models: [`05-agent-runtime.md`](05-agent-runtime.md) ·
> [`06-functions-and-tools.md`](06-functions-and-tools.md) ·
> [`07-multitenancy-security.md`](07-multitenancy-security.md).
>
> **Status:** `planned` — Agent Builder scope; nothing in this scope is
> built yet beyond what the live App Builder already provides. The prior AI
> Sales wedge is retained below as an optional reference template, not the
> MVP. Last
> reviewed: 2026-11-15.

## The MVP (one usable Agent Builder)

> **"Describe the agent you want in natural language; inspect and refine it
> visually; run it governed and auditable."**

Natural-language agent creation + visual inspection/editing on one shared
system representation, single-agent execution, basic tools, basic model
selection, basic memory, versioning, execution history, auth + tenant
isolation, import/export.

## Rule

Do NOT build all major platform phases in parallel. Multi-agent systems,
durable runtime depth, developer ecosystem breadth, operations, marketplace,
and the factory are destination phases (decision D7 in
[`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md)).
The MVP delivers one usable Agent Builder for the initial segments.

## Must-have (M0 scope)

| Capability | Platform concept | Live seed today |
|---|---|---|
| Natural-language agent creation | Natural Language Studio | Chat UX + plan approval already live |
| Visual inspection + basic editing | Visual Builder (advanced) | Workflow visualizer (inspection only); editing is new |
| Single-agent execution | Agent layer ([`05-agent-runtime.md`](05-agent-runtime.md)) | Eino DeepAgent coordinator/coder/reviewer team (App Builder domain) |
| Basic tools + model selection | Capability + model layers | VFS/REST toolset live; registry + gateway are new; built-in Tools + starter Skills catalog in [`06-functions-and-tools.md`](06-functions-and-tools.md) |
| Basic memory | Memory layer | Redis VFS/state exists; scoped memory is new |
| Versioning + import/export | System state | Internal-Git lineage direction exists; manifests are new |
| Execution history | Observability | Generation lineage recorder exists |
| Human approval where needed | Human-in-the-loop | Plan-approval gate exists; capability-level approvals are new |
| Auth + tenant isolation | Tenancy | D1 auth + opt-in session boundary live; full isolation per P0 |

## Must-NOT build (M0 non-goals)

- Multi-agent teams and delegation (phase M1).
- Durable runtime depth: schedules, triggers, webhooks, pause/resume
  (phase M2).
- Public API/SDK/CLI/OpenAPI/manifest specs as stable contracts (phase M3).
- Advanced evaluation, regression, cost optimization (phase M4).
- Registries with third-party supply, marketplace, revenue sharing (phase M5).
- Autonomous generation of whole systems (phase M6).
- Full accounting/inventory ERP; full marketplace or plugin ecosystem.
- Two parallel agent models (NL vs visual) — one Canonical System IR only.

## Acceptance criteria (M0 exit gate)

1. A user creates an agent through natural language — no node design.
2. The same agent can be inspected and edited in the visual editor; both
   interfaces use the same canonical system representation.
3. The agent executes at least one useful task end to end.
4. Every tool call is permission-checked and auditable with
   tenant/agent/identity and verdict.
5. Runs have traceable identity and status in execution history.
6. Tenant isolation tests pass.
7. No critical security boundary is left unverified (launch gates in
   [`07-multitenancy-security.md`](07-multitenancy-security.md)).
8. Failure behavior: a failed/missing capability degrades to an explicit,
   logged error with human escalation — never silent loss.

## M0 vertical slice v0 — the smallest increment that proves the loop (ADOPTED as D9)

> **Status:** `adopted` by **D9** (2026-11-18) — scope fixed, **not built**.
> Implementation sequences after the P0 rollout, because acceptance criterion 2
> (governance) cannot be met with the boundary off. Decision record and the four
> settled sub-decisions:
> [`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md).
> The slice's acceptance criteria are also the measured boundary of D8's
> "in scope while frozen" clause.

### The slice in one sentence

> A signed-in user asks, in natural language, for a small app; a **single**
> agent plans and writes it; the run is **governed** (session + tenant boundary
> on, every write attributed and audited); the run is **recorded** as a
> lineage row plus one internal Git revision; and the same rows are readable in
> the History tab, where the user can inspect the diff and roll back.

What it proves: the whole loop from intent to a versioned, auditable,
rollback-able result — using only code that already exists on `main`. It is a
**demo of the platform loop**, not yet the Agent Builder product.

### Explicitly in scope (all of it already built — the slice is wiring + proof)

| Step | Mechanism that exists | What the slice adds |
|---|---|---|
| 1. NL request + plan approval | chat UX + plan gate (`backend/pkg/engine/dual_model.go`) | a scripted demo path and a recorded verdict |
| 2. Single-agent execution | single-coder step loop; the P1.8 gate already routes small plans here (`backend/pkg/engine/team_gate.go`) | assert the gate chose single-coder for the demo plan |
| 3. Governed run | `guard` + `ownerGuardFor` (`backend/pkg/api/routes.go`), session boundary (`backend/pkg/api/auth.go`) | **boundary ON** in staging, with a second user proving 403/404 |
| 4. Attributed writes | `RecordWriteAuthor` intake + `file_written` audits (P1.3.3) | nothing — verify the rows |
| 5. Revision | `commitGeneration` → one commit per run (P1.3.5) + pre-commit gate (P1.3.6) | nothing — verify `commit_sha` on 100% of runs |
| 6. History | `GET /api/generations/:chatId[/:genId/diff]` + rollback (P1.4) and the History tab (P1.5) | a manual browser pass |
| 7. Failure behavior | explicit error events, no silent loss | assert one induced failure degrades loudly |

### Explicitly out of scope (and why)

- **No agent authoring** (defining a new agent, its tools/skills). That is the
  Agent Builder proper and needs the manifest/capability surfaces; the slice
  uses the existing generator agent as *the* agent. Adding it here would make
  the slice an M0+ build, not a slice.
- **No multi-agent composition, no delegation, no new routing** — frozen by D8.
  The demo must work on the single-coder path; if it only works with the team,
  the slice fails its own test.
- **No MCP transport, no P2 catalog, no marketplace, no evaluation datasets.**
- **No visual editing** of the produced app beyond the existing inspection
  surfaces (workflow visualizer + Monaco read).

### Acceptance criteria (the slice's own gate, on staging)

1. **Loop**: one NL request produces files, one lineage row with
   `status=succeeded`, and a non-empty `commit_sha`.
2. **Governance**: with the boundary on — anonymous request → 401; a second
   user reading the first user's history/diff/rollback → 403 or 404; the owner →
   200. This is the M0 exit gate item that is currently **red** and the whole
   reason the slice needs staging.
3. **Attribution**: every file row in the diff carries an author; the audits
   contain `file_written` and `generation_finished`.
4. **Visibility**: the History tab lists both runs with their verdicts and
   commit SHAs; the diff matches the changed files; a rollback returns a new
   commit and a restored working set.
5. **Rollback safety**: history is not rewritten — the pre-rollback commit is
   still resolvable.
6. **Failure loud, not silent**: a forced failure (e.g. provider error) yields an
   explicit error event and a `failed`/`cancelled` lineage row, never a blank
   success.
7. **Reproducible evidence**: the two runs, the SHAs, the 403/404 proofs and the
   rollback SHA are pasted into `DEV_STATUS.md` with the date.

### What this slice deliberately does NOT prove

It does not prove product-market fit, does not exercise the Agent Builder's
authoring story, and does not touch M1 composition. A green slice means "the
platform loop works for a real user under real governance" — no more, and no
claims beyond it.

### Decisions (settled 2026-11-18, D9)

| # | Question | Decision |
|---|---|---|
| 1 | Is "the agent = the existing generator agent" acceptable for v0? | **Yes** — the generator is the v0 agent; user-defined agent authoring is the next slice. |
| 2 | Does the slice require the boundary ON? | **Yes, mandatory.** A boundary-off demo would re-create the "advisory security" state the audit flagged (F3); the slice is therefore blocked on the P0 rollout. |
| 3 | Is the History tab the "visual inspection" of M0's exit criteria for now? | **For v0 only.** M0 stays incomplete until visual *editing* exists — the tab is inspection, not editing. |
| 4 | Who is the demo tenant/user pair for the 403 proof? | A **throwaway account on staging**, never production. |

### Cost and sequencing

No new dependencies, no schema change, no new route. The only new artifacts are
a demo script and the recorded evidence — everything else is turning on what
already exists and proving it. **Sequencing: after the P0 rollout (ب)**, because
criterion 2 cannot be met without a staging boundary; running it before would
produce a green demo that quietly skips the one gate it was built to close.



AI Sales (lead qualification + follow-up + CRM write-back + weekly summary)
is preserved as an optional reference template/example that exercises the
Agent Builder — never as the mandatory first market product. If built as a
demo, it must satisfy the same representation, permission, audit, and
approval gates above.

## Dependencies

- Engineering P0 (identity/tenancy/security) — checklist P0.3–P0.9 remain
  the gating foundation ([`DEV_CHECKLIST.md`](DEV_CHECKLIST.md)).
- Capability model minimum ([`06-functions-and-tools.md`](06-functions-and-tools.md)):
  typed tools/functions with tenant context, audit and approval hooks.
- Durable execution minimum (engineering P3 slice).
- Measured against builder activation and run success, never seats
  or MAU (decision D6, preserved). Pricing follows the AaaS direction —
  system value plus
  metered run cost — and is finalized only when O6 evidence exists
  ([`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md),
  [`11-aaas-web3-strategy.md`](11-aaas-web3-strategy.md)).

## What remains intentionally unbuilt

Everything in phases M1–M6 of
[`03-product-roadmap.md`](03-product-roadmap.md), and every technology
candidate marked open in
[`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md).
