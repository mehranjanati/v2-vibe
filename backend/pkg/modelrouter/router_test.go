package modelrouter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"backend/pkg/gateway"
)

// fakeModel is a do-nothing chat model: the router only builds them, it never
// calls them (Eino's failover loop does).
type fakeModel struct{ id string }

func (f *fakeModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return &schema.Message{Content: f.id}, nil
}

func (f *fakeModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("not implemented in test")
}

// noopProvider satisfies gateway.Provider; the router never dispatches.
type noopProvider struct{ id string }

func (n *noopProvider) ID() string { return n.id }
func (n *noopProvider) Chat(context.Context, gateway.ProviderRequest) (gateway.ProviderResponse, error) {
	return gateway.ProviderResponse{}, errors.New("not used in router tests")
}

// buildRegistry registers a cheap small model and an expensive large one.
func buildRegistry(t *testing.T, policy gateway.Policy) *gateway.Registry {
	t.Helper()
	r := gateway.NewRegistry()
	if err := r.AddProvider(&noopProvider{id: "p1"}); err != nil {
		t.Fatalf("AddProvider p1: %v", err)
	}
	if err := r.AddProvider(&noopProvider{id: "p2"}); err != nil {
		t.Fatalf("AddProvider p2: %v", err)
	}
	if err := r.AddModel(gateway.Model{
		ID: "small", ProviderID: "p1", ContextWindow: 4096,
		PriceInPerMTok: 0.10, PriceOutPerMTok: 0.40,
	}); err != nil {
		t.Fatalf("AddModel small: %v", err)
	}
	if err := r.AddModel(gateway.Model{
		ID: "large", ProviderID: "p2", ContextWindow: 128000,
		PriceInPerMTok: 3.00, PriceOutPerMTok: 9.00,
	}); err != nil {
		t.Fatalf("AddModel large: %v", err)
	}
	if err := r.AddPolicy(policy); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}
	return r
}

func factoryFor(built *[]string) Factory {
	return func(_ context.Context, modelID string) (model.BaseModel[*schema.Message], error) {
		*built = append(*built, modelID)
		return &fakeModel{id: modelID}, nil
	}
}

func TestInitialModelPicksFirstHealthyCandidate(t *testing.T) {
	r := buildRegistry(t, gateway.Policy{
		Name: "standard", Version: "1.0.0", MaxTokens: 512,
		Candidates: []string{"small", "large"},
	})
	var built []string
	router, err := New(Config{PolicyRef: "model-policy/standard@1.0.0", Registry: r, Factory: factoryFor(&built)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m, id, err := router.InitialModel(context.Background())
	if err != nil {
		t.Fatalf("InitialModel: %v", err)
	}
	if m == nil || id != "small" {
		t.Fatalf("want the first candidate, got %q", id)
	}
	if len(built) != 1 || built[0] != "small" {
		t.Fatalf("factory must build exactly the chosen model, got %v", built)
	}
}

func TestUnknownPolicyFailsAtConstruction(t *testing.T) {
	r := buildRegistry(t, gateway.Policy{
		Name: "standard", Version: "1.0.0", MaxTokens: 512, Candidates: []string{"small"},
	})
	var built []string
	if _, err := New(Config{PolicyRef: "model-policy/standard@latest", Registry: r, Factory: factoryFor(&built)}); err == nil {
		t.Fatal("a floating policy ref must be rejected")
	}
	if _, err := New(Config{PolicyRef: "model-policy/missing@1.0.0", Registry: r, Factory: factoryFor(&built)}); err == nil {
		t.Fatal("an unknown policy must be rejected")
	}
	if _, err := New(Config{PolicyRef: "model-policy/standard@1.0.0", Registry: r}); err == nil {
		t.Fatal("a router without a model factory must be rejected")
	}
}

func TestFailoverConfigBoundsRetriesByCandidates(t *testing.T) {
	r := buildRegistry(t, gateway.Policy{
		Name: "standard", Version: "1.0.0", MaxTokens: 512,
		Candidates: []string{"small", "large"},
	})
	var built []string
	router, _ := New(Config{PolicyRef: "model-policy/standard@1.0.0", Registry: r, Factory: factoryFor(&built)})
	cfg := router.FailoverConfig()
	if cfg.MaxRetries != 1 {
		t.Fatalf("MaxRetries must be candidates-1, got %d", cfg.MaxRetries)
	}
	if cfg.ShouldFailover == nil || cfg.GetFailoverModel == nil {
		t.Fatal("both failover callbacks must be set (adk rejects a partial config)")
	}
}

func TestGetFailoverModelWalksToNextCandidate(t *testing.T) {
	r := buildRegistry(t, gateway.Policy{
		Name: "standard", Version: "1.0.0", MaxTokens: 512,
		Candidates: []string{"small", "large"},
	})
	var built []string
	router, _ := New(Config{PolicyRef: "model-policy/standard@1.0.0", Registry: r, Factory: factoryFor(&built)})
	if _, _, err := router.InitialModel(context.Background()); err != nil {
		t.Fatalf("InitialModel: %v", err)
	}

	m, msgs, err := router.getFailoverModel(context.Background(), &adk.FailoverContext[*schema.Message]{FailoverAttempt: 1})
	if err != nil {
		t.Fatalf("getFailoverModel: %v", err)
	}
	if m == nil || msgs != nil {
		t.Fatalf("nil messages must be returned to keep the original input, got %v", msgs)
	}
	if got := built[len(built)-1]; got != "large" {
		t.Fatalf("failover must select the next candidate, built %v", built)
	}
}

func TestGetFailoverModelSkipsUnhealthyCandidate(t *testing.T) {
	r := buildRegistry(t, gateway.Policy{
		Name: "guarded", Version: "1.0.0", MaxTokens: 512,
		Candidates: []string{"small", "large"},
	})
	var built []string
	health := gateway.NewHealth()
	health.Threshold = 1
	health.Failed("p1") // small's provider is sick

	router, _ := New(Config{
		PolicyRef: "model-policy/guarded@1.0.0", Registry: r,
		Factory: factoryFor(&built), Health: health,
	})
	_, id, err := router.InitialModel(context.Background())
	if err != nil || id != "large" {
		t.Fatalf("want the healthy candidate, got %q (%v)", id, err)
	}
	attempts := router.Attempts()
	if len(attempts) != 1 || attempts[0].ModelID != "small" || attempts[0].ErrorClass != gateway.ErrRetryable {
		t.Fatalf("the skip must be recorded as a retryable attempt, got %+v", attempts)
	}
}

func TestGetFailoverModelAppliesContextWindowGuard(t *testing.T) {
	r := buildRegistry(t, gateway.Policy{
		Name: "wide", Version: "1.0.0", MaxTokens: 1000,
		Candidates: []string{"small", "large"},
	})
	var built []string
	router, _ := New(Config{PolicyRef: "model-policy/wide@1.0.0", Registry: r, Factory: factoryFor(&built)})

	// ~8000 prompt tokens cannot fit 4096 plus 1000 completion.
	big := []*schema.Message{{Role: schema.User, Content: strings.Repeat("word ", 8000)}}
	m, _, err := router.getFailoverModel(context.Background(), &adk.FailoverContext[*schema.Message]{
		FailoverAttempt: 1, InputMessages: big,
	})
	if err != nil || m == nil {
		t.Fatalf("getFailoverModel: %v", err)
	}
	if got := built[len(built)-1]; got != "large" {
		t.Fatalf("the small model must be refused by the window guard, built %v", built)
	}
	found := false
	for _, a := range router.Attempts() {
		if a.ModelID == "small" && a.ErrorClass == gateway.ErrBudget {
			found = true
		}
	}
	if !found {
		t.Fatalf("the window refusal must be recorded as a budget attempt, got %+v", router.Attempts())
	}
}

func TestBudgetGuardRefusesBeforeBuilding(t *testing.T) {
	r := gateway.NewRegistry()
	if err := r.AddProvider(&noopProvider{id: "p1"}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	if err := r.AddModel(gateway.Model{
		ID: "pricey", ProviderID: "p1", ContextWindow: 100000,
		PriceInPerMTok: 100.0, PriceOutPerMTok: 100.0,
	}); err != nil {
		t.Fatalf("AddModel: %v", err)
	}
	if err := r.AddPolicy(gateway.Policy{
		Name: "tight", Version: "1.0.0", MaxTokens: 1000, BudgetUSD: 0.000001,
		Candidates: []string{"pricey"},
	}); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}
	var built []string
	router, _ := New(Config{PolicyRef: "model-policy/tight@1.0.0", Registry: r, Factory: factoryFor(&built)})

	_, _, err := router.getFailoverModel(context.Background(), &adk.FailoverContext[*schema.Message]{
		FailoverAttempt: 1, InputMessages: []*schema.Message{{Role: schema.User, Content: "hello"}},
	})
	if err == nil {
		t.Fatal("a candidate that cannot fit the budget must not be returned")
	}
	if len(built) != 0 {
		t.Fatalf("a budget-refused model must never be built, got %v", built)
	}
}

func TestExhaustedCandidatesReturnError(t *testing.T) {
	r := buildRegistry(t, gateway.Policy{
		Name: "single", Version: "1.0.0", MaxTokens: 256, Candidates: []string{"small"},
	})
	var built []string
	router, _ := New(Config{PolicyRef: "model-policy/single@1.0.0", Registry: r, Factory: factoryFor(&built)})
	if _, _, err := router.InitialModel(context.Background()); err != nil {
		t.Fatalf("InitialModel: %v", err)
	}
	if _, _, err := router.getFailoverModel(context.Background(), &adk.FailoverContext[*schema.Message]{FailoverAttempt: 1}); err == nil {
		t.Fatal("an exhausted policy must return an error instead of looping")
	}
}

func TestDefaultShouldFailoverPolicy(t *testing.T) {
	if DefaultShouldFailover(context.Background(), nil, nil) {
		t.Fatal("no error must not trigger failover")
	}
	if !DefaultShouldFailover(context.Background(), nil, errors.New("boom")) {
		t.Fatal("an error must trigger failover")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if DefaultShouldFailover(ctx, nil, errors.New("boom")) {
		t.Fatal("a cancelled context must never trigger failover")
	}
}
