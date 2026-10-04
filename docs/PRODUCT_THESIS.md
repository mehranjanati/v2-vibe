# VibeSDK Product Thesis — Outcome-First Agentic Software Platform

> **Canonical product strategy.** This document defines what VibeSDK is, what it is not,
> who it serves, and how it wins. It is strategy, not status: for what is actually
> built today versus what is planned, read [`DEV_STATUS.md`](DEV_STATUS.md); for the
> ordered work, read [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md).
>
> **Status:** `current` — adopted 2026-10-01. Supersedes the implicit "AI App Builder"
> framing of the pre-2026-10 roadmap documents (preserved with historical task IDs,
> mapped into the new phase model in `DEV_CHECKLIST.md`). Last reviewed: 2026-10-01.

## Contents

| Section | Anchor |
|---|---|
| Problem | [`#problem`](#problem) |
| Target user | [`#target-user`](#target-user) |
| Outcome-first product model | [`#outcome-first-product-model`](#outcome-first-product-model) |
| What VibeSDK is | [`#what-vibesdk-is`](#what-vibesdk-is) |
| What VibeSDK is not | [`#what-vibesdk-is-not`](#what-vibesdk-is-not) |
| Core abstractions (canonical definitions) | [`#core-abstractions`](#core-abstractions) |
| Why graph is secondary | [`#why-graph-is-secondary`](#why-graph-is-secondary) |
| Why app builder is a capability | [`#why-app-builder-is-a-capability`](#why-app-builder-is-a-capability) |
| Agentic system model | [`#agentic-system-model`](#agentic-system-model) |
| Market entry | [`#market-entry`](#market-entry) |
| Reference systems | [`#reference-systems`](#reference-systems) |
| Competitive landscape | [`#competitive-landscape`](#competitive-landscape) |
| Moat hypotheses | [`#moat-hypotheses`](#moat-hypotheses) |
| Business model considerations | [`#business-model-considerations`](#business-model-considerations) |
| Strategic non-goals | [`#strategic-non-goals`](#strategic-non-goals) |

<a id="problem"></a>

## Problem

Building a production-grade AI agent system today means assembling the same
undifferentiated machinery every time: model access and routing, tool/MCP
wiring, memory and state, permissions and policy, durable execution with
retries and approvals, evaluation, observability, versioning, and deployment.
Frameworks provide pieces; runtimes provide primitives; app builders provide a
narrow output (a website). Nobody provides the **whole loop from a stated
outcome to an operating, governed, improvable agentic system**. Teams either
rebuild orchestration, runtime, and governance infrastructure from scratch, or
they accept a closed builder whose output cannot grow into a real system.

<a id="target-user"></a>

## Target user

The initial ICP is the builder who already feels this pain directly:

- Technical founders shipping an AI product without an orchestration team.
- AI engineers tasked with "put agents in production" inside a small product team.
- Small product teams and software agencies that deliver agentic systems to clients
  and need repeatability (templates, versioning, install/deploy) instead of
  one-off prompt craft.

Explicitly **not** the initial primary market: pure consumer no-code app
building, direct IDE replacement, or a generic enterprise platform sale before
product-market proof (see [Strategic non-goals](#strategic-non-goals)).

<a id="outcome-first-product-model"></a>

## Outcome-first product model

The primary user interaction is **Outcome → System synthesis**:

> Tell VibeSDK the outcome you want. VibeSDK synthesizes the agentic system
> needed to achieve it, executes it durably, evaluates the result, and
> continuously improves or repairs it.

The north-star flow is a pipeline, not a canvas:

```text
Intent → Outcome → System synthesis → Agents → Teams → Tools → Models →
Memory → Policies → ExecutionPlan / System IR → Durable runtime →
Evaluation → Observability → Repair → Deployment → Operations
```

The user defines an **outcome**, never a topology. Graph editing, JSON
configuration, and programmatic APIs are advanced representations of the same
underlying system — for inspection, debugging, and explicit control — not the
primary creation model. Beginner UX is outcome-first; advanced users get full
control over agents, models, tools, memory, policies, and execution.

<a id="what-vibesdk-is"></a>

## What VibeSDK is

VibeSDK is an **agentic software platform**: a platform for building,
composing, deploying, and operating AI agent systems from natural-language
outcomes. Concretely it is, in priority order:

1. An **outcome-to-system compiler** — parses intent, discovers capabilities,
   composes agents/teams/tools/models, and emits a validated executable plan.
2. An **agent system control plane** — identity, permissions, policy
   enforcement, quotas, audit, and lineage for agents and agent teams.
3. A **durable agent runtime** — events, retries, timeouts, approvals,
   schedules, idempotency, and observable execution state.
4. A **system state and lineage platform** — versioned systems, diffs,
   evaluations, and repair loops over runs.
5. A **marketplace substrate** — reusable, versioned, installable agents,
   teams, workflows, and full systems.

<a id="what-vibesdk-is-not"></a>

## What VibeSDK is not

- Not a Lovable / Replit / Bolt clone (browser IDE that emits a website).
- Not a Cursor / Claude Code / Codex replacement (a coding agent is a
  **capability inside** the platform and one of its hardest reference workloads).
- Not a generic chatbot builder.
- Not an n8n / Dify clone and not a graph-first workflow editor. Visual DAGs
  exist as an inspection/debugging representation (see
  [Why graph is secondary](#why-graph-is-secondary)).
- Not "serverless hosting with an AI feature." Cloudflare/serverless is the
  execution substrate; the product moat is the agentic control plane above it.

<a id="core-abstractions"></a>

## Core abstractions

These definitions are **canonical**: exactly one definition per concept across
the active docs. Every other document links here instead of redefining them.

| Concept | Canonical definition |
|---|---|
| **Outcome** | The user-visible business or technical result the user wants achieved. |
| **System** | A deployable agentic solution composed of agents, teams, tools, models, memory, policies, workflows, and runtime configuration. |
| **Team** | A coordinated set of agents with explicit delegation/composition semantics. |
| **Agent** | An executable role with model access, tools/capabilities, memory, policy, input/output contract, limits, and identity. |
| **Capability** | A capability available to the system, potentially exposed through one or more tools or MCP servers. |
| **Tool** | An executable interface an agent can invoke under explicit permissions. |
| **Model** | An LLM or model endpoint used according to routing/policy rules. |
| **Memory** | Persisted contextual state available according to scope and permissions. |
| **Policy** | Rules governing what an agent/system may do, where, when, and under which conditions. |
| **Workflow** | Durable execution logic involving events, sequencing, conditions, waits, retries, schedules, or approvals. |
| **ExecutionPlan** | The canonical intermediate representation used to translate an intended agentic system into validated executable steps. |
| **Run** | A concrete execution instance of a system or workflow. |
| **Evaluation** | A measurement of the quality, correctness, safety, cost, or outcome of a run. |
| **Lineage** | The traceable relationship between user intent, system version, plans, agents, files/artifacts, execution, evaluation, and outcomes. |

Product principles that follow from these abstractions:

- User defines outcome, not graph topology; system synthesis is the primary creation path.
- Natural language and programmatic configuration must coexist.
- Agent systems must be versionable and reproducible; execution must be observable and evaluable.
- Permissions must be runtime-enforced, not merely prompt-enforced.
- Model choice must be policy/routing driven and model-provider agnostic.
- Durable execution is a first-class runtime primitive.
- Marketplace artifacts are versioned and deployable; reference implementations validate the platform but must not hijack the roadmap.

<a id="why-graph-is-secondary"></a>

## Why graph is secondary

A graph is an **implementation and inspection representation** of an agentic
system, not the product abstraction. Users think in outcomes ("every lead gets
researched, qualified, and followed up within an hour"); only a minority can or
wants to think in nodes, edges, and retry policies. Making the graph primary
selects for workflow-engine buyers and excludes everyone who wants a result.

In VibeSDK the graph therefore occupies exactly two roles: (1) the compiled
form the runtime executes (the DAG the engine validates and runs), and (2) an
advanced view for inspection, debugging, and surgical editing. Today's
`WorkflowVisualizer` is correctly a read-only rendering of the generated DAG —
that is the right starting point; editing arrives as an advanced capability
(roadmap P6), never as the front door. The compiler owns topology; humans own
outcomes and constraints.

<a id="why-app-builder-is-a-capability"></a>

## Why app builder is a capability

Application generation remains a first-class capability of the platform — it is
the current reference workload and the proving ground for the team runtime,
lineage, and deploy paths. But apps are **outputs/artifacts of a system**,
not the platform's purpose. Framing the whole platform as an app builder caps
it at website output and surrenders every adjacent system (sales, support,
research, operations) to someone else's runtime.
<a id="agentic-system-model"></a>

## Agentic system model

The target architecture is layered so each layer can be reasoned about,
tested, and replaced independently. Cloudflare/serverless is the execution
substrate at the bottom; VibeSDK owns the agentic control-plane abstractions
above it:

```text
Outcome layer            user intent → validated outcome spec
Agentic compiler         outcome → ExecutionPlan / System IR (validation, policy checks)
Agent primitives         Agent / Model / Tool / Skill / Memory / Policy definitions, versioned manifests
Capability registry      discoverable, versioned capabilities (tools, MCP servers, node packages)
Model routing            task-, cost-, latency-, risk-aware model selection per role/step
Tool / MCP layer         least-privilege tool execution, credential references (never plaintext)
Memory / state           scoped persisted state (conversation, project, system)
Policy / permissions     runtime-enforced authorization for agents, tools, and users
Durable execution        events, retries, timeouts, approvals, schedules, idempotency
Evaluation / observability  run scoring, traces, cost accounting
Lineage / versioning     intent → system version → plan → run → evaluation → outcome traceability
Marketplace              publish / install / fork versioned agents, teams, systems
Deployment / runtime     Cloudflare primitives (Workers, Workflows, D1, KV, Pages) as substrate
```

Model-agnostic by design: models are swappable and routable based on task
type, quality, latency, cost, policy, risk, and execution context. No product
decision may hard-couple the platform to a single provider.

<a id="market-entry"></a>

## Market entry

Enter through builders who already operate agentic systems by hand and feel
the missing control plane: technical founders, AI engineers, small product
teams, and software agencies. Win them with: outcome-to-system synthesis that
actually works on day one for a narrow set of reference systems; durable
execution they can trust with customer-facing work; and lineage/evaluation
that lets them prove and improve results.

Do not enter through pure consumer no-code app building (crowded, low
willingness to pay for governance), direct IDE replacement (incumbent
distribution), or a generic enterprise platform sale before the reference
systems prove the loop.

<a id="reference-systems"></a>

## Reference systems

Three reference systems validate the platform end to end. Each must be
buildable as an outcome, executable durably, evaluable, and repairable.
They are validators of the platform — they must not become unrelated vertical
SaaS products unless explicitly decided later.

1. **AI Sales System** — lead intake → research → qualification → CRM update →
   outreach draft/send → human approval when required. Exercises: multi-agent
   delegation, external tools, human-in-the-loop, policy-gated sends.
2. **AI Support System** — ticket → classify → retrieve knowledge → resolve →
   escalate → update systems. Exercises: retrieval, classification routing,
   escalation policy, evaluation of resolution quality.
3. **AI Research System** — question → parallel research → fact checking →
   analysis → writing → review. Exercises: parallel teams, reviewer gates,
   multi-model routing, artifact lineage.


<a id="competitive-landscape"></a>

## Competitive landscape

Scope: product architecture, abstraction level, creation workflow,
extensibility, runtime, governance, and market positioning. Observed
capability is separated from strategic interpretation; no best/worst ranking;
claims below are sourced to the vendor's own current documentation or site,
checked 2026-10-01.

**AI coding agents.** Cursor's agent model is instructions + tools + model
per agent, with multi-agent Projects where a coordinator plans and delegates
([Cursor agent docs](https://cursor.com/docs/agent/overview)). This validates
the supervisor/delegation shape VibeSDK already implements with DeepAgent —
but Cursor's unit of value is code in an IDE, not a governed runnable system.
Claude Code and Codex-style agent infrastructure compete at the same layer:
excellent task execution, no platform story for deployment, permissions,
lineage, or marketplaces. Strategic read: converge-or-partner at the
tool/capability layer, differentiate at the system/control-plane layer.

**AI app builders.** Bolt ("create apps and websites by chatting with AI",
automatic model routing per task, built-in backend/hosting) and its peers
(Lovable, Replit) own the chat-to-website loop with hosting attached
([bolt.new](https://bolt.new/)). Their abstraction stops at the deployed
artifact; there is no agent-team model, no policy plane, no system
versioning. Strategic read: VibeSDK's app generation must stay competitive
as a *capability*, but the platform must not be judged as a builder — the
differentiation is everything that happens around and after the artifact.

**Workflow / agent builders.** n8n's AI Workflow Builder turns natural
language into node graphs (node selection, placement, configuration) with
refinement chat
([n8n docs](https://docs.n8n.io/build/ways-of-building-workflows/ai-workflow-builder)).
Dify positions as "platform for production-ready agentic workflows": visual
workflow studio, agents with skills/tools/knowledge, knowledge pipelines,
plugin marketplace, publish as app/API/tool
([dify.ai](https://dify.ai/)). Both are graph/visual-first creation models —
precisely the framing VibeSDK rejects as primary — and their governance
story is workflow-scoped, not system-scoped. Win on synthesis quality,
policy enforcement, and lineage, not on canvas features.
**Agent frameworks.** CrewAI (role-based agents, crews, control plane with
tracing/RBAC/audit, evaluation) and LangGraph (low-level stateful
orchestration primitives: graphs, nodes, edges, persistent memory, streaming,
human-in-the-loop; [langchain.com/langgraph](https://www.langchain.com/langgraph))
are developer frameworks, not platforms: the operator still owns runtime,
identity, deployment, and multi-tenancy. Strategic read: frameworks are
potential execution substrates or interop targets for the compiler, not the
product category. VibeSDK competes one layer up, at system synthesis and
operation.

**Agent runtimes / cloud execution.** Cloudflare's Agents platform (Agent
class with durable identity/state/sessions/routing/WebSockets/scheduling on
Durable Objects, plus Sandbox, Browser, MCP, AI Search, Workflows;
[developers.cloudflare.com/agents](https://developers.cloudflare.com/agents/))
is the execution substrate VibeSDK already builds on. Strategic read:
**substrate, not moat.** VibeSDK must track it closely, adopt its primitives
(Durable Objects sessions, Sandbox execution, Workflows) where they fit, and
keep all outcome/compiler/policy/lineage/marketplace value in its own layer
so the platform survives substrate evolution.

<a id="moat-hypotheses"></a>

## Moat hypotheses

Ranked by defensibility × difficulty. Each is a hypothesis to be validated by
reference-system evidence, not a claimed fact:

1. **Outcome-to-agent-system synthesis** — the compiler (intent → validated
   executable system) is the hardest part to replicate and the core UX.
2. **Universal ExecutionPlan / System IR** — one validated representation every
   creation path compiles to and every runtime executes.

3. **Runtime policy and permission enforcement** — agent/tool/user authorization
   in the execution path, not in prompts.
4. **System state and lineage** — traceability from intent to outcome across
   versions, runs, and evaluations.
5. **Execution → evaluation → repair loop** — measured runs that improve the
   system automatically or with targeted human input.
6. **System versioning and diff** — reproducible, reviewable, forkable systems.
7. **Durable execution semantics** — approvals, schedules, idempotency, and
   recovery as platform guarantees.
8. **Capability registry and extensibility** — versioned tools/MCP/skills with
   credential references and least-privilege wiring.
9. **Agent/team/system marketplace** — distribution and network effects on top
   of the registry.

Serverless execution cost/scale is an advantage, not a moat by itself.

<a id="business-model-considerations"></a>

## Business model considerations

Considerations only — pricing is not decided here:

- Charge where durable value accrues: execution (runs, with cost pass-through
  plus margin), evaluated outcomes, and marketplace take-rate — not raw model
  tokens, which commoditize.
- Self-hosted / BYO-cloud packaging matters to agencies and regulated teams;
  keep the control plane separable from any single substrate account.
- Usage limits and credits UI already exist in the product surface
  ([`usage-limits-ui.md`](usage-limits-ui.md)); align them to runs/systems
  rather than chat messages as the platform matures.

<a id="strategic-non-goals"></a>

## Strategic non-goals

- Becoming a general IDE or replacing the developer's editor.
- Becoming a vertical SaaS in sales, support, or research (reference systems
  prove the platform; productizing them is a separate decision).
- Owning foundation models or model hosting.
- Owning undifferentiated infrastructure (regions, cold starts, storage
  engines) — adopt the substrate, don't rebuild it.
- A visual-builder feature race with n8n/Dify/Retool on canvas capabilities.

