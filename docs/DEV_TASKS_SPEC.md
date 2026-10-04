# DEV TASKS SPEC — Task-Spec Quality Contract

> Canonical definition of what a development task specification must contain.
> The checklist says *what*; the spec files say *how + with what test*.
> `docs/DEV_STATUS.md` is the entry point; `docs/DEV_CHECKLIST.md` is the
> single backlog; product strategy is [`PRODUCT_THESIS.md`](PRODUCT_THESIS.md).
> Status vocabulary: `planned` | `in_progress` | `implemented_unverified` |
> `verified` | `blocked` | `obsolete` — `implemented_unverified` means code
> exists but verification evidence is incomplete (it is not `verified`).
>
> Last reviewed: 2026-10-01.

## Required fields (every task spec carries all of them)

| # | Field | What it must answer |
|---|---|---|
| 1 | `id` | Stable historical ID (never renumbered; mapped via the checklist map). |
| 2 | `phase` | Target P0–P6 phase (via the checklist map, not the legacy prefix). |
| 3 | `title` | One line, implementation-sized, no vague verbs ("improve agents" ✗). |
| 4 | `status` | One of the six vocabulary states — no `done`/`complete`/`finished`. |
| 5 | `goal` | The concrete end state in one or two sentences. |
| 6 | `why` | Platform reading: which thesis capability this serves and why now. |
| 7 | `dependencies` | Named task IDs that must land first (or "none"). |
| 8 | `affected files` | Repo-relative paths when known (`backend/...`, `worker/...`, `src/...`, `migrations/...`). |
| 9 | `implementation notes` | The exact functions/routes/tables/contracts to touch, with resolvable `file:line` refs. |
| 10 | `acceptance criteria` | Observable, testable conditions — including negative cases (403s, rejections, fallbacks). |
| 11 | `verification` | Exact commands: Go (`cd backend && go vet ./... && go test ./...`, new `*_test.go` next to code), TS (`bun run typecheck && bun run lint && bun run build`), docs (`bun run docs:check` when docs change). |
| 12 | `unblock condition` | Required iff `blocked`: the named decision/resource/event that unblocks, plus what is verified in the meantime. |

Quality rule: a developer must be able to pick a task from
`DEV_CHECKLIST.md` and implement it without prior chat history. If the spec
cannot be followed cold, the spec — not the developer — is incomplete.

## Spec index (target phases → files)

| Target phase | Spec file | Legacy content it carries |
|---|---|---|
| P0 Foundation (P0.3–P0.9) | `DEV_CHECKLIST.md` (P0 block owns the acceptance tests; no separate spec file — implement per the file targets + tests in each P0 task) | Security/tenancy/identity foundation |
| P1 Agent Primitive (lineage/author/budgets) | `DEV_SPEC_P1A.md` (P1.0–P1.5 incl. internal-Git substrate now mapped to P5) | Lineage + Git substrate + History API |
| P1 Agent Primitive (reviewer/gate/claim/identified) | `DEV_SPEC_P1B.md` (P1.6–P1.10; compiler-bound items map to P2, workbench items to P4 — see checklist) | Reviewer + gate + claim + identified generation |
| P1 Agent Primitive (capability registry) | `DEV_SPEC_P2.md` (P2.1–P2.6 node catalog = first registry content) | Node-as-package catalog |
| P3 Durable Runtime + P6 Factory (repair loop) | `DEV_SPEC_P3P4.md` (P3.1–P3.4 suspense/RFT/circuit → P6 repair; P4.1/P4.2 sandbox → P4 workbench) | Suspense + circuit + sandbox |
| P5 Systems (reference system) | `DEV_SPEC_P5.md` (P5.1–P5.6 App Runtime reframed as first reference system) | Per-app backend |
| P4 Workbench + P3 Runtime (canvas reframed) | `DEV_SPEC_P6.md` (P6.1–P6.8; graph is inspection surface per thesis) | Workflow canvas waves |

## General test contract (all phases)

- Go: `cd backend && go vet ./... && go test ./...` green; new tests in `*_test.go` next to code. (`./...`, not `./pkg/...` — `backend/agent` has tests and the planner/gate live there; `backend/e2e` self-skips unless `VIBE_E2E=1`.)
- TS: from root, `bun run typecheck && bun run lint && bun run build` green.
- DB: local migrations only after **P1.0.0** (Node ≥ 22 via `bun --bun` + `--config wrangler.v2.jsonc` + DB name `v2-vibe`).
- Verified baseline (measured 2026-09-28): `go vet ./...` clean, `go test ./...` green (incl. `backend/agent`; `backend/e2e` skips without `VIBE_E2E=1`), `typecheck` green, `lint` 0 errors / 3 warnings, `build` green (~7s), `docs:check` green.
- Tick rule: no green tests, no tick (checklist rule 1).

## Docs-maintenance backlog (closed)

> Output of the `docs/` review (2026-09-24). 11 tasks with priority, dependencies, target files, acceptance criteria, tests.
>
> **Reference:** `docs/DOCS_AUDIT_BACKLOG.md`
> **Status (2026-09-28):** all 16 tasks done (T1 … T16) — backlog closed. (T16: three periodic maintenance routines — routes / platform numbers / bindings — became machine gates in `bun run docs:check`.)

