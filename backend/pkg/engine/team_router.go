package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"backend/pkg/agent"
	"backend/pkg/gateway"
	"backend/pkg/modelrouter"
	"backend/pkg/skills"
)

// ---------- P7 model routing on a live role (decision 3 / slice a) ----------
//
// P7.4 landed the gateway + modelrouter contracts with tests and zero
// consumers (audit F5). This is the first live consumer: the coder role of the
// team is built from a PINNED model policy when one is configured, and Eino's
// failover loop switches candidates through the router's policy instead of the
// role falling back to whatever the engine's default model is.
//
// Opt-in by design: with no policy configured the coder gets exactly the
// pre-P7 model (`roleModel`), so nothing about current behaviour changes until
// an operator pins a policy. A broken configuration degrades to that same role
// model rather than failing a run — routing is an optimisation, not a
// prerequisite.
//
// Deliberately NOT in this slice: MCP transport, registry/policy persistence in
// D1, and the gateway's own `Chat` dispatch path. The router only needs the
// registry to validate candidates and the factory to build them; persistence
// without a transport would be an untested store (audit F5).

const (
	// modelRouterPolicyEnv pins the policy the coder route uses
	// (`model-policy/<name>@<MAJOR.MINOR.PATCH>`).
	modelRouterPolicyEnv = "MODEL_ROUTER_POLICY"
	// modelRouterCandidatesEnv is the ordered candidate list of that policy,
	// comma-separated model ids (primary first, then fallbacks).
	modelRouterCandidatesEnv = "MODEL_ROUTER_CANDIDATES"
	// modelRouterProviderEnv is the registry provider id the candidates belong
	// to. One provider is enough for this slice: every candidate is built
	// through the engine's own endpoint.
	modelRouterProviderEnv = "MODEL_ROUTER_PROVIDER"
	// modelRouterDefaultProvider names that provider when unset.
	modelRouterDefaultProvider = "ai-gateway"
	// modelRouterContextWindowEnv declares the candidates' context window. The
	// registry refuses a model without a positive window, so this is the one
	// capacity number the slice must know; it is only a pre-flight ceiling.
	modelRouterContextWindowEnv = "MODEL_ROUTER_CONTEXT_WINDOW"
	// modelRouterDefaultContextWindow is the conservative default (128k-class).
	modelRouterDefaultContextWindow = 131072
)

// coderRouting is the coder role's resolved model plus the failover wiring that
// lets Eino switch candidates inside the pinned policy.
type coderRouting struct {
	Model    model.BaseChatModel
	Failover *adk.ModelFailoverConfig[*schema.Message]
	Policy   string
	Chosen   string
	// Degraded reports that a policy WAS configured but could not be used, so
	// the role fell back to the plain role model. The run continues; the reason
	// is logged and surfaced here for tests and operators.
	Degraded bool
	Reason   string
}

// coderRouting resolves the coder's model. No policy (or no candidates) means
// the pre-P7 behaviour; a policy means the router picks the initial candidate
// and supplies the failover config.
func (r *ProjectRoom) coderRouting(ctx context.Context, reg skills.Registry) coderRouting {
	if r.eng == nil {
		// runTeam is gated on an engine, so this is a guard for direct callers
		// (and tests) rather than a reachable production path.
		return coderRouting{Degraded: true, Reason: "no engine configured"}
	}
	fallback := r.roleModel(ctx, reg, skills.RoleCoder)

	policyRef := strings.TrimSpace(os.Getenv(modelRouterPolicyEnv))
	candidates := splitModelCandidates(os.Getenv(modelRouterCandidatesEnv))
	if policyRef == "" || len(candidates) == 0 {
		return coderRouting{Model: fallback}
	}

	router, err := r.buildCoderRouter(ctx, reg, policyRef, candidates)
	if err == nil {
		var initial model.BaseModel[*schema.Message]
		var chosen string
		initial, chosen, err = router.InitialModel(ctx)
		if err == nil {
			log.Printf("[room:%s] model routing: coder starts on %s under %s (%d candidates)",
				r.chatID, chosen, policyRef, len(candidates))
			return coderRouting{
				Model:    initial,
				Failover: router.FailoverConfig(),
				Policy:   policyRef,
				Chosen:   chosen,
			}
		}
	}

	log.Printf("[room:%s] model routing: %q unusable (%v); using the role model", r.chatID, policyRef, err)
	return coderRouting{Model: fallback, Policy: policyRef, Degraded: true, Reason: err.Error()}
}

// buildCoderRouter assembles the registry + router for the configured policy.
// Every candidate is built through the engine's own role-model factory, so
// credentials, base URL and provider quirks stay in the infrastructure layer.
func (r *ProjectRoom) buildCoderRouter(ctx context.Context, reg skills.Registry, policyRef string, candidates []string) (*modelrouter.Router, error) {
	if r.eng == nil {
		return nil, errors.New("no engine configured")
	}
	name, version, err := splitModelPolicyRef(policyRef)
	if err != nil {
		return nil, err
	}

	providerID := strings.TrimSpace(os.Getenv(modelRouterProviderEnv))
	if providerID == "" {
		providerID = modelRouterDefaultProvider
	}
	window := modelRouterDefaultContextWindow
	if raw := strings.TrimSpace(os.Getenv(modelRouterContextWindowEnv)); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n <= 0 {
			return nil, fmt.Errorf("context window %q is not a positive integer", raw)
		}
		window = n
	}
	registry := gateway.NewRegistry()
	if err := registry.AddProvider(&engineModelProvider{id: providerID}); err != nil {
		return nil, fmt.Errorf("provider %q: %w", providerID, err)
	}
	for _, id := range candidates {
		// Pricing stays unknown in this slice, and the policy carries no
		// BudgetUSD, so no cost guard is claimed. The context window is real:
		// the router refuses a candidate whose prompt + budget would not fit.
		if err := registry.AddModel(gateway.Model{
			ID:            id,
			ProviderID:    providerID,
			ContextWindow: window,
		}); err != nil {
			return nil, fmt.Errorf("candidate %q: %w", id, err)
		}
	}

	roleCfg, _ := reg.Config(skills.RoleCoder)
	if err := registry.AddPolicy(gateway.Policy{
		Name:        name,
		Version:     version,
		Temperature: roleCfg.Temperature,
		MaxTokens:   roleCfg.MaxTokens,
		Candidates:  candidates,
	}); err != nil {
		return nil, fmt.Errorf("policy %q: %w", policyRef, err)
	}

	factory := func(ctx context.Context, modelID string) (model.BaseModel[*schema.Message], error) {
		rm, err := r.eng.NewRoleModel(ctx, agent.RoleModelConfig{
			Model:       modelID,
			MaxTokens:   roleCfg.MaxTokens,
			Temperature: roleCfg.Temperature,
		})
		if err != nil {
			return nil, err
		}
		return rm, nil
	}
	return modelrouter.New(modelrouter.Config{
		PolicyRef: policyRef,
		Registry:  registry,
		Factory:   factory,
	})
}

// splitModelCandidates parses the comma-separated candidate list, dropping
// blanks and duplicates so an operator typo cannot create a phantom candidate.
func splitModelCandidates(raw string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, part := range strings.Split(raw, ",") {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// splitModelPolicyRef splits `model-policy/<name>@<version>`; the registry
// validates the same shape when the policy is added, so a bad ref fails before
// any model is built.
func splitModelPolicyRef(ref string) (name, version string, err error) {
	rest, ok := strings.CutPrefix(ref, "model-policy/")
	if !ok {
		return "", "", fmt.Errorf("model policy ref %q must look like model-policy/<name>@<MAJOR.MINOR.PATCH>", ref)
	}
	name, version, ok = strings.Cut(rest, "@")
	if !ok || name == "" || version == "" {
		return "", "", fmt.Errorf("model policy ref %q must look like model-policy/<name>@<MAJOR.MINOR.PATCH>", ref)
	}
	return name, version, nil
}

// engineModelProvider satisfies gateway.Provider for registry bookkeeping only.
// The router builds Eino models through the factory and never dispatches a
// gateway Chat call in this slice, so reaching Chat means the wiring was
// misread — an error, not a silent no-op.
type engineModelProvider struct{ id string }

func (p *engineModelProvider) ID() string { return p.id }

func (p *engineModelProvider) Chat(context.Context, gateway.ProviderRequest) (gateway.ProviderResponse, error) {
	return gateway.ProviderResponse{}, errors.New("engine provider: gateway dispatch is not wired in this slice")
}
