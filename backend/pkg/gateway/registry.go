package gateway

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

var (
	slugRe    = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	versionRe = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	policyRe  = regexp.MustCompile(`^model-policy/([a-z0-9]([-a-z0-9]*[a-z0-9])?)@(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	vaultRe   = regexp.MustCompile(`^vault:[a-z0-9][a-z0-9/_.-]*$`)
)

// Model is one entry in the model registry. Pricing is USD per 1M tokens, so
// cost is a multiplication, not a guess; zero pricing is legal (self-hosted).
type Model struct {
	// ID is the provider-facing model identifier (e.g. a Workers AI model id).
	ID string
	// ProviderID is the registry key of the provider that serves it.
	ProviderID string
	// ContextWindow is the total token capacity (prompt + completion).
	ContextWindow int
	// PriceInPerMTok and PriceOutPerMTok are USD per million tokens.
	PriceInPerMTok  float64
	PriceOutPerMTok float64
}

// EstimateCost returns the USD cost of a usage record under this model's
// price table.
func (m Model) EstimateCost(u Usage) float64 {
	return float64(u.PromptTokens)/1e6*m.PriceInPerMTok +
		float64(u.CompletionTokens)/1e6*m.PriceOutPerMTok
}

// Policy is a pinned, versioned model policy — the only thing an Agent
// manifest may reference (`model-policy/standard@1.0.0`).
type Policy struct {
	Name    string
	Version string
	// Temperature applies to every candidate unless a call overrides it.
	Temperature float64
	// MaxTokens is the ceiling for a single call in this policy.
	MaxTokens int
	// BudgetUSD caps the estimated cost of one call.
	BudgetUSD float64
	// Candidates are tried in order: primary first, then fallbacks.
	Candidates []string // model IDs from the registry
}

// Ref renders the manifest reference.
func (p Policy) Ref() string { return "model-policy/" + p.Name + "@" + p.Version }

// Registry holds providers, models and policies. It is the only place where
// provider-specific knowledge lives.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	models    map[string]Model
	policies  map[string]Policy
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		models:    make(map[string]Model),
		policies:  make(map[string]Policy),
	}
}

// AddProvider registers a provider adapter.
func (r *Registry) AddProvider(p Provider) error {
	if p == nil {
		return fmt.Errorf("gateway: provider is nil")
	}
	id := p.ID()
	if !slugRe.MatchString(id) {
		return fmt.Errorf("gateway: provider id %q must be a DNS-safe slug", id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[id]; ok {
		return fmt.Errorf("gateway: provider %q already registered", id)
	}
	r.providers[id] = p
	return nil
}

// AddModel registers a model. The provider must already exist.
func (r *Registry) AddModel(m Model) error {
	if strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("gateway: model id is required")
	}
	if m.ContextWindow <= 0 {
		return fmt.Errorf("gateway: model %q needs a positive context window", m.ID)
	}
	if m.PriceInPerMTok < 0 || m.PriceOutPerMTok < 0 {
		return fmt.Errorf("gateway: model %q price must not be negative", m.ID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[m.ProviderID]; !ok {
		return fmt.Errorf("gateway: model %q references unknown provider %q", m.ID, m.ProviderID)
	}
	if _, ok := r.models[m.ID]; ok {
		return fmt.Errorf("gateway: model %q already registered", m.ID)
	}
	r.models[m.ID] = m
	return nil
}

// AddPolicy registers a pinned policy. Candidates must exist and be distinct.
func (r *Registry) AddPolicy(p Policy) error {
	if !slugRe.MatchString(p.Name) {
		return fmt.Errorf("gateway: policy name %q must be a DNS-safe slug", p.Name)
	}
	if !versionRe.MatchString(p.Version) {
		return fmt.Errorf("gateway: policy version %q must be pinned MAJOR.MINOR.PATCH", p.Version)
	}
	if len(p.Candidates) == 0 {
		return fmt.Errorf("gateway: policy %s needs at least one candidate", p.Ref())
	}
	if p.MaxTokens <= 0 {
		return fmt.Errorf("gateway: policy %s needs a positive MaxTokens ceiling", p.Ref())
	}
	seen := make(map[string]bool, len(p.Candidates))
	for _, c := range p.Candidates {
		if seen[c] {
			return fmt.Errorf("gateway: policy %s lists candidate %q twice", p.Ref(), c)
		}
		seen[c] = true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range p.Candidates {
		if _, ok := r.models[c]; !ok {
			return fmt.Errorf("gateway: policy %s references unknown model %q", p.Ref(), c)
		}
	}
	if _, ok := r.policies[p.Ref()]; ok {
		return fmt.Errorf("gateway: policy %s already registered (versions are immutable)", p.Ref())
	}
	r.policies[p.Ref()] = p
	return nil
}

// Policy resolves a `model-policy/name@x.y.z` reference.
func (r *Registry) Policy(ref string) (Policy, bool) {
	if !policyRe.MatchString(ref) {
		return Policy{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.policies[ref]
	return p, ok
}

// Model returns a model entry.
func (r *Registry) Model(id string) (Model, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.models[id]
	return m, ok
}

// Provider returns a provider adapter.
func (r *Registry) Provider(id string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	return p, ok
}

// Policies lists registered policy refs in sorted order (audit/inspection).
func (r *Registry) Policies() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.policies))
	for ref := range r.policies {
		out = append(out, ref)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
