# Roadmap P0–P9 (Canonical, dependency-driven)

> **Status:** `adopted` 2026-11-21. History: `03-product-roadmap.md` (M0–M6,
> preserved). Backlog: `DEV_CHECKLIST.md`. Execution plan:
> `17-development-execution-plan.md`. Tasks: `tasks/MASTER_BACKLOG.json`.

- **P0 Stabilization and Security** — P0.3–P0.9 rollout, session/tenant/WebSocket/
  vault/workflow/lineage/deploy gates, missing security tests. Precedence: blockers first.
- **P1 General Agent Foundation** — identity, manifest, IR consumption, tools,
  skills, MCP runtime, provider abstraction, routing, memory, SDK, history.
- **P2 Assistant and Research** — chat APIs, user memory, search interface,
  research orchestration, citations, files, multimodal/voice interfaces, browser.
- **P3 Coder and Multi-Agent** — teams, delegation, parallel/durable runs,
  checkpoints, retry, scheduling, approvals, evaluation, observability.
- **P4 Studio and Developer Ecosystem** — NL plus visual builders, registries,
  CLI/SDK, manifests, versioning, contributions, marketplace foundations.
- **P5 Cloud** — managed deploy/execution, metering/billing, scaling, teams,
  monitoring, enterprise ops, SLOs.
- **P6 Commerce** — commerce model, UCP/AP2 adapters, merchants, discovery,
  cart/checkout, payments, orders, audit.
- **P7 Runtime Portability** — provider contract, local/Cloudflare providers,
  WASM PoC plus interfaces, wasmCloud evaluation, containers, GPU, conformance.
- **P8 Network** — node agent, registration/discovery, advertisements,
  scheduling, health, trust, verification, metering, settlement interface.
- **P9 Web3 Economy (optional)** — chain-agnostic settlement, wallets, escrow,
  incentives, reputation, staking evaluation, token research, legal review.

Rules: P0 first; no task without prerequisites; small verifiable tasks; research/
design/implementation/test/deploy labeled; P7–P9 stay backlog until gates pass.
