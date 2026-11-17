package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CallerContext attributes a gateway call. Every field is required for
// accounting; the gateway never infers attribution from prompt text.
type CallerContext struct {
	Tenant string
	Agent  string
	RunID  string
}

// Gateway routes policy-resolved calls to providers. It is the only component
// that knows about providers, prices and budgets.
type Gateway struct {
	registry *Registry
	secrets  SecretResolver
	usage    UsageSink
	health   *Health
	now      func() time.Time
}

// Config wires a Gateway.
type Config struct {
	Registry *Registry
	// Secrets resolves `vault:` references. Required: a gateway without a
	// resolver cannot authenticate and must not silently call with no key.
	Secrets SecretResolver
	// Usage receives one record per successful call. Optional.
	Usage UsageSink
	// Health tracks provider failures. Optional.
	Health *Health
	// Now overrides the clock (tests).
	Now func() time.Time
}

// New builds a Gateway.
func New(cfg Config) (*Gateway, error) {
	if cfg.Registry == nil {
		return nil, fmt.Errorf("gateway: registry is required")
	}
	if cfg.Secrets == nil {
		return nil, fmt.Errorf("gateway: a SecretResolver is required (keys are vault references, never literals)")
	}
	health := cfg.Health
	if health == nil {
		health = NewHealth()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Gateway{registry: cfg.Registry, secrets: cfg.Secrets, usage: cfg.Usage, health: health, now: now}, nil
}

// estimatePromptTokens is a deterministic pre-flight estimate (chars/4, the
// conventional English heuristic). It exists to fail closed on obvious
// overshoot; the provider's own usage numbers are what get recorded.
func estimatePromptTokens(msgs []Message) int {
	chars := 0
	for _, m := range msgs {
		chars += len(m.Role) + len(m.Content) + 4
	}
	return (chars + 3) / 4
}

// Chat resolves the policy, applies the pre-flight guards, then walks the
// candidate list until one succeeds.
func (g *Gateway) Chat(ctx context.Context, caller CallerContext, req ChatRequest) (ChatResponse, error) {
	if strings.TrimSpace(caller.Tenant) == "" {
		return ChatResponse{}, fmt.Errorf("gateway: tenant is required for attribution")
	}
	policy, ok := g.registry.Policy(req.PolicyRef)
	if !ok {
		return ChatResponse{}, fmt.Errorf(
			"gateway: unknown or unpinned policy %q (expected model-policy/name@MAJOR.MINOR.PATCH)", req.PolicyRef)
	}
	if len(req.Messages) == 0 {
		return ChatResponse{}, fmt.Errorf("gateway: no messages supplied")
	}

	maxTokens := policy.MaxTokens
	if req.MaxTokens > 0 && req.MaxTokens < maxTokens {
		maxTokens = req.MaxTokens
	}
	budget := policy.BudgetUSD
	if req.BudgetUSD > 0 && (budget == 0 || req.BudgetUSD < budget) {
		budget = req.BudgetUSD
	}
	promptTokens := estimatePromptTokens(req.Messages)

	var attempts []Attempt
	fallbacksUsed := 0

	for _, candidateID := range policy.Candidates {
		model, ok := g.registry.Model(candidateID)
		if !ok {
			attempts = append(attempts, Attempt{ModelID: candidateID, ErrorClass: ErrFatal, Error: "model not in registry"})
			fallbacksUsed++
			continue
		}
		if !g.health.Healthy(model.ProviderID) {
			attempts = append(attempts, Attempt{
				ModelID: model.ID, ProviderID: model.ProviderID, ErrorClass: ErrRetryable,
				Error: "provider marked unhealthy",
			})
			fallbacksUsed++
			continue
		}
		// Guard 1: prompt plus completion must fit the context window.
		if model.ContextWindow > 0 && promptTokens+maxTokens > model.ContextWindow {
			attempts = append(attempts, Attempt{
				ModelID: model.ID, ProviderID: model.ProviderID, ErrorClass: ErrBudget,
				Error: fmt.Sprintf("context window: %d prompt + %d completion > %d",
					promptTokens, maxTokens, model.ContextWindow),
			})
			fallbacksUsed++
			continue
		}
		// Guard 2: the worst-case cost of this call must fit the budget.
		if budget > 0 {
			worst := model.EstimateCost(Usage{PromptTokens: promptTokens, CompletionTokens: maxTokens})
			if worst > budget {
				attempts = append(attempts, Attempt{
					ModelID: model.ID, ProviderID: model.ProviderID, ErrorClass: ErrBudget,
					Error: fmt.Sprintf("estimated worst-case $%.6f exceeds budget $%.6f", worst, budget),
				})
				fallbacksUsed++
				continue
			}
		}

		provider, ok := g.registry.Provider(model.ProviderID)
		if !ok {
			attempts = append(attempts, Attempt{
				ModelID: model.ID, ProviderID: model.ProviderID, ErrorClass: ErrFatal,
				Error: "provider not registered",
			})
			fallbacksUsed++
			continue
		}

		apiKey := ""
		if hp, isHTTP := provider.(*HTTPProvider); isHTTP && hp.SecretRef != "" {
			key, err := g.secrets.Resolve(hp.SecretRef)
			if err != nil {
				attempts = append(attempts, Attempt{
					ModelID: model.ID, ProviderID: model.ProviderID, ErrorClass: ErrFatal,
					Error: "vault resolve failed",
				})
				fallbacksUsed++
				continue
			}
			apiKey = key
		}

		resp, err := provider.Chat(ctx, ProviderRequest{
			ModelID: model.ID, Messages: req.Messages, MaxTokens: maxTokens,
			Temperature: policy.Temperature, APIKey: apiKey,
		})
		if err != nil {
			class := ClassOf(err)
			attempts = append(attempts, Attempt{
				ModelID: model.ID, ProviderID: model.ProviderID, ErrorClass: class, Error: err.Error(),
			})
			if class == ErrRetryable {
				g.health.Failed(model.ProviderID)
			}
			// Fatal and budget failures also move to the next candidate: the
			// caller asked for a policy, not for a provider.
			fallbacksUsed++
			continue
		}

		g.health.Succeeded(model.ProviderID)
		cost := model.EstimateCost(resp.Usage)
		if g.usage != nil {
			g.usage.Record(Event{
				PolicyRef: req.PolicyRef, ModelID: model.ID, ProviderID: model.ProviderID,
				Usage: resp.Usage, CostUSD: cost,
				Agent: caller.Agent, Tenant: caller.Tenant, RunID: caller.RunID,
				FallbacksUsed: fallbacksUsed, Attempts: attempts,
			})
		}
		return ChatResponse{
			Content: resp.Content, ModelID: model.ID, ProviderID: model.ProviderID,
			Usage: resp.Usage, CostUSD: cost, FallbacksUsed: fallbacksUsed,
		}, nil
	}

	return ChatResponse{}, &ProviderError{
		Provider: "gateway", Class: ErrFatal,
		Message: fmt.Sprintf("no candidate in %s could serve the call (%d attempted)", policy.Ref(), len(attempts)),
		Err:     fmt.Errorf("attempts: %v", attempts),
	}
}
