# ADR — Reuse Eino's failover primitive for model routing (accepted)

> **Status:** `accepted` (2026-11-16). **Deciders:** platform architecture.
> **Related:** P7.4 ([`../DEV_CHECKLIST.md`](../DEV_CHECKLIST.md)),
> [`model-gateway.md`](model-gateway.md), O5 in
> [`../10-decisions-and-open-questions.md`](../10-decisions-and-open-questions.md).
> **Supersedes:** the implicit "gateway implements its own failover loop on the
> agent path" assumption in P7.4.

## Context

P7.4 delivered `backend/pkg/gateway`: pinned model policies, a candidate list,
pre-flight context-window and budget guards, provider health, and per-call cost
attribution. Its `Chat` method already implements a fallback loop for
**non-streaming** calls.

The runtime path is different: the agent talks to Eino chat models through the
ADK, and generation streams. Eino v0.9.21 (already pinned in `backend/go.mod`)
ships `adk.ModelFailoverConfig` — `MaxRetries`, `ShouldFailover`,
`GetFailoverModel` — including streaming-error failover and "try the last
successful model first". Writing a second failover loop on that path would mean
reimplementing streaming semantics that already exist and are tested upstream.

Constraint from the strategy: Eino is an implementation dependency, never the
canonical public model. Framework types must not leak into manifests, IR, or
the gateway's public contract.

## Options considered

### A. Compose: Eino owns failover mechanics, the gateway owns decisions *(chosen)*

`backend/pkg/modelrouter` adapts a `gateway.Policy` into an
`*adk.ModelFailoverConfig`: `GetFailoverModel` walks policy candidates, applies
the same guards the gateway applies, respects provider health, and returns the
next admissible model. `ShouldFailover` cancels on context cancellation and
otherwise defers to Eino.

- **Pro:** streaming failover is battle-tested upstream; our policy/guard/cost
  logic stays in one place; the gateway keeps zero Eino imports (the adapter
  lives in its own package).
- **Con:** two code paths for "choose the next model" (gateway.Chat for
  non-streaming, modelrouter for the agent path) — they share the *policy* and
  the *guard rules*, but not the loop.
- **Verdict:** chosen. The duplication is one loop, not two philosophies, and
  the guards are shared by construction.

### B. Gateway implements its own streaming failover *(rejected)*

- **Pro:** one loop for everything; no Eino coupling at all.
- **Con:** reimplements stream-partial-message handling, retry-after-partial
  semantics and cancellation races that Eino already covers with tests. High
  risk of subtle divergence on the exact path that matters most (long
  generation).
- **Verdict:** rejected — reimplementing a tested primitive to avoid an
  import we already depend on.

### C. Replace the gateway with Eino failover entirely *(rejected)*

- **Pro:** least code.
- **Con:** loses pinned policies, pre-flight budget/window guards, cost
  attribution and provider health — the things that make the platform
  operable. Also drags Eino types into the public contract.
- **Verdict:** rejected; this is the option that trades the product
  requirements for line count.

### D. Wait for an upstream "model gateway" component *(rejected)*

- **Pro:** no code now.
- **Con:** nothing in eino/eino-ext provides model registry, policy pinning,
  budget guards or cost attribution today (`eino-ext` ships model adapters
  only). Waiting blocks M1/M2 on an upstream roadmap we do not control.
- **Verdict:** rejected — no such component exists to wait for.

## Decision

Adopt option A. Ownership split is normative:

| Concern | Owner |
|---|---|
| When to switch models, streaming-error handling, last-successful-model replay | Eino ADK (`ModelFailoverConfig`) |
| Which model is next, and whether it may run at all (window, budget, health, registry membership) | `modelrouter` + `gateway` |
| Policy definition, pricing, attribution | `gateway` |
| Public contracts (manifests, IR, API types) | VibeSDK — no Eino types |

## Consequences

- **Positive:** the agent path gets streaming failover without new code risk;
  the gateway's public surface stays framework-neutral; the guard rules are
  shared, so a model refused by `gateway.Chat` is refused by the router too.
- **Negative / accepted:** two candidate-walk implementations must be kept
  behaviourally aligned; a change to guard rules touches both. Mitigation: both
  are covered by tests that assert the same outcomes (window refusal, budget
  refusal, unhealthy skip) — see `backend/pkg/modelrouter/router_test.go`.
- **Neutral:** nothing is wired into the live generation path yet; today the
  engine still builds per-role models directly, so generation behaviour is
  unchanged.

## Revisit triggers

1. Eino changes or removes `ModelFailoverConfig` in a pinned upgrade.
2. The two candidate-walk paths diverge observably (different model selected
   for the same policy and inputs).
3. A streaming failover bug is traced to upstream semantics we cannot adapt.
4. An upstream gateway/registry component appears that covers policies,
   budgets and attribution.

## Evidence

- Adapter + tests: `backend/pkg/modelrouter` (9 tests, green).
- Gateway contract: [`model-gateway.md`](model-gateway.md).
- Upstream primitive: `adk.ModelFailoverConfig` in
  `github.com/cloudwego/eino v0.9.21` (`adk/failover_chatmodel.go`).
