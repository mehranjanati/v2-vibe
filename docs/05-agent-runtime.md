# Agent Runtime Model (PROPOSED)

> **Scope:** how agents are modeled in the AI-Native Business OS. Layer
> context: [`04-system-architecture.md`](04-system-architecture.md). Wedge
> usage: [`02-mvp.md`](02-mvp.md). Function detail:
> [`06-functions-and-tools.md`](06-functions-and-tools.md). Live agent
> implementation today: [`MULTI_AGENT.md`](MULTI_AGENT.md) (Eino DeepAgent
> coordinator/coder/reviewer).
>
> **Status:** `proposed` — the model below is the target; the live system
> implements only the App Builder team today. Last reviewed: 2026-10-06.

## Principle

**Agents orchestrate capabilities; they do not contain arbitrary business
logic.** If a rule, calculation or action matters to the business, it lives
in a versioned, typed, testable function
([`06-functions-and-tools.md`](06-functions-and-tools.md)) — the agent
selects, sequences and supervises it. This keeps agent behavior reviewable
(plan + selections) and business behavior verifiable (typed functions with
tests and audit).

## Agent flow (target execution loop)

1. **User outcome** — the business result requested in natural language.
2. **Intent extraction** — classify and structure the request.
3. **Plan generation** — decompose into capability invocations with success
   criteria.
4. **Capability selection** — resolve named functions from the registry
   (metadata: typed IO, side-effect level, required approval).
5. **Permission check** — tenant + user + agent authorization per
   capability, enforced in the execution path, never only in the prompt
   ([`07-multitenancy-security.md`](07-multitenancy-security.md)).
6. **Execution** — durable steps on the runtime layer.
7. **Observation** — structured results feed the next plan step.
8. **Recovery/retry** — policy-driven retries; failure degrades to an
   explicit logged error, never silent loss.
9. **Human approval** — when a step has real external side effects or the
   tenant's policy requires it.
10. **Final outcome** — report against the original intent (the wedge's
    sales summary is the first instance).

## Relationship to the live Eino team

The live coordinator/coder/reviewer team
([`MULTI_AGENT.md`](MULTI_AGENT.md)) already proves steps 1–4 in the App
Builder domain: intent → plan (planner), delegation in dependency order
(coordinator), least-privilege execution and review (coder/reviewer). The
target model generalizes that loop from "write files" to "invoke business
capabilities", which is the minimal conceptual jump the wedge requires.
Eino remains the live orchestration substrate; Dify is an open alternative
(see the candidate register in
[`04-system-architecture.md`](04-system-architecture.md)) with a hard rule:
**orchestration vendors never own business data**.

## Agent composition rules

- One agent per coherent business role (e.g. lead-qualification agent,
  follow-up agent, reporting agent) — composed per outcome.
- Agents share context through plans and the data layer, not through
  hidden prompt state.
- Every agent run is observable end-to-end: intent, plan, selections,
  approvals, results — linked by the lineage discipline the platform
  already applies to generations.
- Agents are tenant-aware: tenant-specific configuration and behavior are
  inputs to the run, not forks of agent logic.

## Failure and escalation

- Missing capability → explicit error + human escalation.
- Conflicting guidance → project/tenant-local rules outrank generic
  behavior; the conflict is logged.
- Approval timeout → safe default (no side effect) + escalation.
