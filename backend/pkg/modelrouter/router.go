// Package modelrouter composes the VibeSDK model gateway (P7.4) with Eino's
// ADK failover primitive instead of reimplementing failover.
//
// Division of ownership:
//   - Eino owns the failover mechanics: when to switch, streaming-error
//     handling, replaying the last successful model first.
//   - This package owns the *decisions*: which model is next, whether it is
//     allowed to run at all (context window, cost budget, provider health),
//     and what was attempted.
//
// Consequence: the gateway keeps zero Eino types (its contract stays
// provider- and framework-neutral), while the runtime path gets a
// battle-tested streaming failover loop for free.
package modelrouter

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"backend/pkg/gateway"
)

// Factory builds an Eino chat model for one registry model id. It is supplied
// by the caller because credentials, base URLs and provider quirks belong to
// the infrastructure layer, not to routing.
type Factory func(ctx context.Context, modelID string) (model.BaseModel[*schema.Message], error)

// Router turns a pinned gateway policy into an Eino failover configuration.
// One Router serves one logical model call (or one agent role); it records
// attempts for tests and, later, for the run trace.
type Router struct {
	policy   gateway.Policy
	registry *gateway.Registry
	factory  Factory
	health   *gateway.Health

	mu       sync.Mutex
	tried    map[string]bool
	attempts []gateway.Attempt
}

// Config wires a Router.
type Config struct {
	// PolicyRef is `model-policy/name@MAJOR.MINOR.PATCH`; resolved at
	// construction so a bad reference fails before anything runs.
	PolicyRef string
	Registry  *gateway.Registry
	Factory   Factory
	// Health is optional; without it a failing provider is never skipped.
	Health *gateway.Health
}

// New resolves the policy and returns a Router.
func New(cfg Config) (*Router, error) {
	if cfg.Registry == nil {
		return nil, errors.New("modelrouter: registry is required")
	}
	if cfg.Factory == nil {
		return nil, errors.New("modelrouter: factory is required")
	}
	policy, ok := cfg.Registry.Policy(cfg.PolicyRef)
	if !ok {
		return nil, fmt.Errorf(
			"modelrouter: unknown or unpinned policy %q (expected model-policy/name@MAJOR.MINOR.PATCH)",
			cfg.PolicyRef,
		)
	}
	return &Router{
		policy: policy, registry: cfg.Registry, factory: cfg.Factory,
		health: cfg.Health, tried: make(map[string]bool),
	}, nil
}

// Policy exposes the resolved policy (inspection/tests).
func (r *Router) Policy() gateway.Policy { return r.policy }

// Attempts returns a copy of the routing decisions made so far.
func (r *Router) Attempts() []gateway.Attempt {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]gateway.Attempt, len(r.attempts))
	copy(out, r.attempts)
	return out
}

// InitialModel returns the first candidate that passes every guard. The agent
// starts here; Eino's failover only kicks in if this model fails.
func (r *Router) InitialModel(ctx context.Context) (model.BaseModel[*schema.Message], string, error) {
	if len(r.policy.Candidates) == 0 {
		return nil, "", fmt.Errorf("modelrouter: policy %s has no candidates", r.policy.Ref())
	}
	r.mu.Lock()
	r.tried = make(map[string]bool)
	r.mu.Unlock()

	// No messages yet: only health and registry membership can be judged.
	for _, id := range r.policy.Candidates {
		m, ok := r.registry.Model(id)
		if !ok {
			r.record(gateway.Attempt{ModelID: id, ErrorClass: gateway.ErrFatal, Error: "model not in registry"})
			continue
		}
		if r.health != nil && !r.health.Healthy(m.ProviderID) {
			r.record(gateway.Attempt{ModelID: id, ProviderID: m.ProviderID, ErrorClass: gateway.ErrRetryable, Error: "provider marked unhealthy"})
			continue
		}
		built, err := r.factory(ctx, id)
		if err != nil {
			r.record(gateway.Attempt{ModelID: id, ProviderID: m.ProviderID, ErrorClass: gateway.ErrFatal, Error: err.Error()})
			continue
		}
		r.markTried(id)
		return built, id, nil
	}
	return nil, "", fmt.Errorf("modelrouter: no candidate in %s passed the pre-flight guards", r.policy.Ref())
}

// FailoverConfig returns the Eino failover configuration for this policy.
// MaxRetries is bounded by the remaining candidates so the loop cannot run
// longer than the policy allows.
func (r *Router) FailoverConfig() *adk.ModelFailoverConfig[*schema.Message] {
	remaining := len(r.policy.Candidates) - 1
	if remaining < 0 {
		remaining = 0
	}
	return &adk.ModelFailoverConfig[*schema.Message]{
		MaxRetries:       uint(remaining),
		ShouldFailover:   DefaultShouldFailover,
		GetFailoverModel: r.getFailoverModel,
	}
}

// DefaultShouldFailover is the routing policy for switch decisions:
// cancellation is never retried (the caller asked us to stop), and any other
// error is a candidate for the next model.
func DefaultShouldFailover(ctx context.Context, _ *schema.Message, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	return err != nil
}

// getFailoverModel implements adk.ModelFailoverConfig.GetFailoverModel.
// It walks the policy candidates in order, skipping models already tried in
// this call and any whose pre-flight guards fail, and returns the first
// admissible one.
func (r *Router) getFailoverModel(ctx context.Context, fc *adk.FailoverContext[*schema.Message]) (
	model.BaseModel[*schema.Message], []*schema.Message, error) {
	if fc == nil {
		return nil, nil, errors.New("modelrouter: failover context is nil")
	}
	promptTokens := estimateTokens(fc.InputMessages)

	for _, id := range r.policy.Candidates {
		if r.alreadyTried(id) {
			continue
		}
		m, ok := r.registry.Model(id)
		if !ok {
			r.record(gateway.Attempt{ModelID: id, ErrorClass: gateway.ErrFatal, Error: "model not in registry"})
			r.markTried(id)
			continue
		}
		if r.health != nil && !r.health.Healthy(m.ProviderID) {
			r.record(gateway.Attempt{ModelID: id, ProviderID: m.ProviderID, ErrorClass: gateway.ErrRetryable, Error: "provider marked unhealthy"})
			r.markTried(id)
			continue
		}
		if verdict := r.guard(m, promptTokens); verdict != "" {
			r.record(gateway.Attempt{ModelID: id, ProviderID: m.ProviderID, ErrorClass: gateway.ErrBudget, Error: verdict})
			r.markTried(id)
			continue
		}
		built, err := r.factory(ctx, id)
		if err != nil {
			r.record(gateway.Attempt{ModelID: id, ProviderID: m.ProviderID, ErrorClass: gateway.ErrFatal, Error: err.Error()})
			r.markTried(id)
			continue
		}
		r.markTried(id)
		// nil input messages keep the original conversation: Eino documents
		// that nil means "use the original input".
		return built, nil, nil
	}

	return nil, nil, fmt.Errorf(
		"modelrouter: policy %s exhausted its candidates after %d attempt(s)",
		r.policy.Ref(), fc.FailoverAttempt,
	)
}

// guard applies the same two pre-flight checks the gateway applies in
// gateway.Chat, so a model that would be refused there is never dispatched
// from here either.
func (r *Router) guard(m gateway.Model, promptTokens int) string {
	if m.ContextWindow > 0 && promptTokens+r.policy.MaxTokens > m.ContextWindow {
		return fmt.Sprintf(
			"context window: %d prompt + %d completion > %d",
			promptTokens, r.policy.MaxTokens, m.ContextWindow)
	}
	if r.policy.BudgetUSD > 0 {
		worst := m.EstimateCost(gateway.Usage{PromptTokens: promptTokens, CompletionTokens: r.policy.MaxTokens})
		if worst > r.policy.BudgetUSD {
			return fmt.Sprintf("estimated worst-case $%.6f exceeds budget $%.6f", worst, r.policy.BudgetUSD)
		}
	}
	return ""
}

// estimateTokens mirrors the gateway's documented heuristic (chars/4). It is
// a pre-flight guard only; providers report the tokens that actually ran.
func estimateTokens(msgs []*schema.Message) int {
	chars := 0
	for _, m := range msgs {
		if m == nil {
			continue
		}
		chars += len(m.Content) + len(string(m.Role)) + 4
		for _, part := range m.MultiContent {
			chars += len(part.Text)
		}
	}
	return (chars + 3) / 4
}

func (r *Router) record(a gateway.Attempt) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempts = append(r.attempts, a)
}

func (r *Router) markTried(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tried[id] = true
}

func (r *Router) alreadyTried(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tried[id]
}
