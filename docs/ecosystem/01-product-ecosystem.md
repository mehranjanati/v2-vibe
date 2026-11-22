# Product Ecosystem — Eight Independent Products (Canonical)

> **Status:** `adopted` 2026-11-21. Vision: `00-product-vision.md`.
> Boundaries: `02-product-boundaries.md`. Roadmap P0–P9: `03-product-roadmap.md`.
> Strategy apex: `12-agentic-platform-strategy.md`.
> Status labels: IMPLEMENTED / PARTIAL / DESIGNED / PLANNED / RESEARCH —
> defined in `18-glossary-and-architecture-terms.md`.

## Principle

Every product below is a distinct product with its own surface, API, SDK,
deployment model, release lifecycle, docs, and commercial offer. They stay
interoperable through shared platform contracts, never through duplication.

## 1. Vibe Assistant — General-purpose AI assistant (PLANNED)

Role: everyday conversations, reasoning, tool use, memory, voice, multimodal I/O.
Capabilities: conversational AI, model selection and routing, user memory,
tool calling, file understanding, multimodal in/out, voice, image understanding,
external integrations, delegation to specialist products.
Seed today: PARTIAL — chat UX plus plan-approval exists in the App Builder;
general delegation, voice, and memory scopes are DESIGNED/PLANNED.

## 2. Vibe Research — Deep research and knowledge intelligence (PLANNED)

Role: multi-step web investigation, source verification, synthesis, reports.
Capabilities: web search, browser research, deep-research orchestration,
parallel agents, verification, citations, document analysis, synthesis,
research memory, exportable reports.
Seed today: RESEARCH — no web-search provider, browser automation, or citation
model exists in the tree.

## 3. Vibe Coder — AI software engineering (PARTIAL)

Role: builds on current VibeSDK code generation.
Capabilities: coding agents, repo understanding, generation, review, debugging,
Git integration, PR generation, sandboxed execution, multi-agent engineering,
history and rollback.
Seed today: PARTIAL/IMPLEMENTED — Eino DeepAgent coordinator/coder/reviewer,
VFS tools, lineage, Pages deploy, GitHub export live. Missing: sandboxed
execution, PR flow, durable runs (see `16-technical-debt-and-gap-analysis.md`).

## 4. Vibe Commerce — Agentic commerce (RESEARCH)

Role: shopping, merchant, catalog, order, and payment flows over open protocols.
Capabilities: commerce agents, discovery, carts, checkout, orders, payment
authorization, human approval, UCP/AP2/A2A/MCP integration.
Seed today: RESEARCH — no commerce or payment code exists; no UCP/AP2
compliance claimed (`13-agentic-commerce.md`).

## 5. Vibe Studio — Agent creation environment (PARTIAL)

Role: create, configure, test, publish, and manage agents via NL, SDK/API/CLI,
and an optional advanced visual builder.
Capabilities: NL creation, manifests, System IR, visual builder, model/provider
choice, tool/skill assignment, memory config, permissions, testing, versioning,
publishing, import/export.
Seed today: PARTIAL — chat creation plus inspection-only visualizer live;
manifests and IR v1 DESIGNED as docs gates; registry/compiler PLANNED.

## 6. Vibe Runtime — Agent execution infrastructure (PARTIAL)

Role: portable execution for agents, tools, skills, workflows.
Capabilities: isolation, durable execution, checkpointing, pause/resume,
retries, cancellation, scheduling, triggers, quotas, history, tracing,
WASM/container/GPU providers.
Seed today: PARTIAL — Cloudflare Workflow interpreter plus Go engine paths
live; provider abstraction DESIGNED (`06-runtime-and-execution-architecture.md`).

## 7. Vibe Cloud — Managed agent infrastructure (PLANNED)

Role: commercial hosting — deploy, scale, monitor, operate without self-managing.
Capabilities: managed hosting/gateway/execution, deploys, scaling, metering,
billing, logs, teams/orgs, enterprise features, SLAs.
Seed today: PLANNED — only staging deploys plus a metering recorder seed exist.

## 8. Vibe Network — Distributed execution network (RESEARCH)

Role: future operator-run compute with verifiable, policy-controlled execution.
Capabilities: node registration/discovery, advertisements, scheduling, WASM and
container workloads, GPU discovery, health, trust/reputation, verification,
metering, settlement, optional chain integration, future token utility.
Seed today: RESEARCH — no node agent or settlement code exists.
