# Market and Competition — Canonical Reference

> **Status:** `adopted` 2026-11-24. Vision: `00-product-vision.md`.
> Products: `01-product-ecosystem.md`. Business model: `14-product-and-business-model.md`.
> Strategy apex: `12-agentic-platform-strategy.md`.
> Status labels: IMPLEMENTED / PARTIAL / DESIGNED / PLANNED / RESEARCH —
> defined in `18-glossary-and-architecture-terms.md`.
>
> **Reading rule:** all market figures are ranges with named sources and
> access dates, never single-point claims. Definitions of "agent market"
> differ across firms; the base-vs-bull spread below is the honest
> representation. Re-verify before any pricing, fundraising, or
> partnership use. Last verified: 2026-11-24.

## 1. What Vibe is (one paragraph)

Vibe is an outcome-first Agent-as-a-Service ecosystem: from a stated
outcome to an operating, governed, improvable agentic system. The
canonical pipeline is Intent → System Manifest → IR Validation →
Capability Resolution → Composition Lock → Context Assembly →
Activation → Policy Enforcement → Execution → Evaluation →
Controlled Evolution. Users buy operating systems (agents + skills +
tools + memory + policy + evaluation + versioning + execution), not
chatbots or websites. Open core under MIT; commercial engine is
Vibe Cloud (runs + evaluated outcomes + marketplace take-rate —
never seats, never raw tokens, D6).

## 2. 2027 market figures (sourced ranges)

### 2.1 Total AI agents

| Case | 2027 size | Source | Accessed |
|---|---|---|---|
| Base | **~$44B** | Axis Intelligence, AI Agents Trends 2027 | 2026-11-24 |
| Bull (Gartner-referenced) | **~$65B** | Axis citing Gartner | 2026-11-24 |
| Consensus CAGR | **~40-45%** | Mordor / Grand View / Fortune BI / Precedence | 2026-11-24 |

Axis frames the spread as "$44B Base Case vs Gartner's $65B".
Use the range, never one end alone.

### 2.2 Total AI spend context (why infra matters)

| Figure | 2027 | Source | Accessed |
|---|---|---|---|
| Global AI spending | **~$4.0-4.05T** (+47.5% YoY) | Gartner press release 2026-11-12 | 2026-11-24 |
| Infra share of AI spend | **59% (~$2.4T)** | Gartner press release 2026-11-12 | 2026-11-24 |

Implication: the 2027 money wave sits in the layer Vibe
Runtime/Cloud occupies, not in model weights alone.

### 2.3 Addressable segments for Vibe products

| Segment | 2027 estimate | Vibe product | Source |
|---|---|---|---|
| AI coding assistants/tools | **~$8B** (from ~$2.3B in 2025) | Coder + Studio | AI Kick Start synthesis of market.us + Sacra, 2026-06-19 |
| Conversational AI | **~$18B** (VMR) to **~$57B** (Juniper, broader incl. services) | Assistant | VMR via PRNewswire; Juniper Research |
| US AI-originated retail sales (agentic commerce GMV) | **~$74.7B**, 52.7% CAGR 2026-2031 | Commerce (take-rate 1-3% = ~$0.7-2.2B platform revenue) | GlobeNewswire 2026-11-24 |
| Enterprise knowledge/search + research | **~$12-15B** | Research | Directional; pin before fundraising use |
| Managed agent infra/hosting | Tens of $B (slice of ~$2.4T AI infra) | Cloud | Gartner-derived; runs-metered hosting |
| Decentralized compute | **~$5-8B** | Network (future) | Directional; Akash/io.net-analogous |

### 2.4 TAM / SAM / SOM framing (2027)

- **TAM** (spend agents touch): **~$150-200B**.
- **SAM** (serviceable with open-core + Cloud + adapters): **~$5-10B**.
- **SOM** (year-one with good P0-P5 execution): **tens of $M ARR**
  via Coder/Studio wedge, Research upsell.

## 3. Business-model comparables

| Player | Model | Closest Vibe analog | Why Vibe differs |
|---|---|---|---|
| Replit / Lovable / Bolt | Outcome-first builder + hosting + credits | Coder + Cloud | Website output only; no System IR, lineage, evaluation loop |
| Devin / Cursor / Copilot Workspace | Coding agent, per-seat or usage | Coder | Code only; no multi-product platform |
| Perplexity / Glean / Coveo | Research subscription | Research | Answers, not auditable multi-agent orchestration |
| Sierra / Decagon / Intercom Fin | Per-resolution outcome pricing | Pricing model | Vertical support; not a general platform |
| Stripe / Shopify + agents | Commerce rails + take-rate | Commerce (UCP/AP2) | Rails; not protocol-independent orchestration |
| Render / Fly.io / Vercel | Managed hosting + usage | Cloud | Hosts code; not durable agent runs with approvals |
| WordPress-Automattic / GitLab | Open core + managed cloud + marketplace | Whole-Vibe shape | Closest structural analog, pre-AI era |

## 4. Technical-stack comparables (by layer)

| Layer | Players | Vibe position |
|---|---|---|
| Orchestration frameworks | LangGraph, CrewAI, AutoGen, OpenAI Agents SDK, Claude Agent SDK | Above them: Eino is substrate; value is IR + Lock + Activation + Evaluation |
| Durable execution | Temporal, Inngest, Trigger.dev, CF Workflows/Agents, Bedrock AgentCore, Vertex Agents | C1-C18 provider contract + conformance; portable AND agent-aware |
| Tool/context | MCP, LangChain Tools, Composio, Toolhouse | MCP as adapter + trust boundary (mcp.Gate) |
| Memory/context | Mem0, Zep, LangMem, Supermemory | Scope ladder (run/conversation/agent/team/tenant) + provenance |
| Model gateway | LiteLLM, OpenRouter, Portkey, CF AI Gateway | Policy-aware gateway + router, not just failover |
| Marketplace/extensions | Poe bots, Flowise templates, HF Spaces | Signed artifacts + retirement/tombstone + evaluation gates |
| Decentralized compute | Gensyn, Bittensor, Akash, io.net, Render Network | Verifiable execution, chain-agnostic; no token precondition (D14) |

## 5. Differentiation (five moats)

1. **One System IR + Composition Lock** — immutable snapshot of the whole
   system that run, evaluation, and rollback all reference.
2. **Runtime-enforced policy, not prompt-based** — approvals,
   side-effect classes, Human-Not-Present rules, deny-overrides in the Gate.
3. **Provider-portable durable execution with conformance (C1-C18).**
4. **Closed Evaluation-to-Evolution loop** — no composite scores, CAS
   promotion, rollback vs emergency pin.
5. **Protocol-agnostic + self-hostable + no token** — no model, chain,
   or cloud lock-in.

## 6. Growth potential and risks (honest)

**Base case** (good P0-P5 execution): Coder + Studio wedge for
agencies/SMB → single-digit $M ARR in 18-24 months, low CAC via open
core + marketplace.

**Bull case** (two gates clear): Commerce via UCP/AP2 + Network
verifiable execution → $100M+ ARR path (take-rate + compute metering).

**Top 3 risks:** (1) incumbent distribution power — offset with
open-source distribution; (2) protocol immaturity — Commerce stays
RESEARCH until pin + conformance; (3) execution spread — P0-P5 must
stay sequential, not parallel.

**Standing recommendation:** wedge = Coder + Studio for agencies;
Research as upsell; Commerce + Network as R&D until gates pass;
Cloud metering correct from day one.
