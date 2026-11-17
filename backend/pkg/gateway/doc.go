// Package gateway is the P7.4 model gateway: one contract in front of every
// model provider, so no Agent, manifest, or prompt ever names a provider.
//
// What it owns:
//   - a model registry (id, provider, context window, price) and a
//     model-policy registry (pinned `model-policy/name@MAJOR.MINOR.PATCH`);
//   - routing over an ordered candidate list with fallback on retryable
//     provider failures;
//   - guards that fail closed before money is spent: context-window fit,
//     max-token budget, per-call cost budget;
//   - usage and cost attribution, and provider health tracking.
//
// What it deliberately does NOT do (v1): streaming, prompt caching, LoRA
// adapters, and provider-specific parameter translation beyond the
// OpenAI-compatible shape. Agents reference a policy; the gateway resolves it.
//
// Secrets: providers receive a `vault:` reference and resolve it through a
// SecretResolver at call time. No key is stored in the registry, a policy, a
// manifest, or a log line.
package gateway
