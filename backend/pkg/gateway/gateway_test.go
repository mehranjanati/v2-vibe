package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeProvider is the mock provider used alongside the real HTTP adapter.
type fakeProvider struct {
	id      string
	failFor int // number of calls to fail before succeeding
	class   ErrorClass
	calls   int
	content string
	usage   Usage
}

func (f *fakeProvider) ID() string { return f.id }

func (f *fakeProvider) Chat(_ context.Context, req ProviderRequest) (ProviderResponse, error) {
	f.calls++
	if f.calls <= f.failFor {
		return ProviderResponse{}, &ProviderError{Provider: f.id, Class: f.class, Message: "injected failure"}
	}
	content := f.content
	if content == "" {
		content = "ok:" + req.ModelID
	}
	usage := f.usage
	if usage.TotalTokens == 0 {
		usage = Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
	}
	return ProviderResponse{Content: content, Usage: usage}, nil
}

// vault is a trivial SecretResolver: it records requests and returns a value.
type vault struct {
	values map[string]string
	asked  []string
}

func (v *vault) Resolve(ref string) (string, error) {
	v.asked = append(v.asked, ref)
	if val, ok := v.values[ref]; ok {
		return val, nil
	}
	return "", http.ErrNoCookie
}

// testRegistry builds a two-model, one-or-two-provider registry: a cheap small
// model and (optionally) a larger backup model on the second provider.
func testRegistry(t *testing.T, primary, fallback Provider) *Registry {
	t.Helper()
	r := NewRegistry()
	if err := r.AddProvider(primary); err != nil {
		t.Fatalf("AddProvider primary: %v", err)
	}
	if err := r.AddModel(Model{
		ID: "small", ProviderID: primary.ID(), ContextWindow: 4096,
		PriceInPerMTok: 0.10, PriceOutPerMTok: 0.40,
	}); err != nil {
		t.Fatalf("AddModel small: %v", err)
	}
	if fallback != nil {
		if err := r.AddProvider(fallback); err != nil {
			t.Fatalf("AddProvider fallback: %v", err)
		}
		if err := r.AddModel(Model{
			ID: "backup", ProviderID: fallback.ID(), ContextWindow: 128000,
			PriceInPerMTok: 1.00, PriceOutPerMTok: 2.00,
		}); err != nil {
			t.Fatalf("AddModel backup: %v", err)
		}
	}
	return r
}

func TestPolicyMustBePinnedAndKnown(t *testing.T) {
	p := &fakeProvider{id: "p1"}
	r := testRegistry(t, p, nil)
	g, err := New(Config{Registry: r, Secrets: &vault{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := g.Chat(context.Background(), CallerContext{Tenant: "t1"}, ChatRequest{
		PolicyRef: "model-policy/standard@latest", Messages: []Message{{Role: "user", Content: "hi"}},
	}); err == nil {
		t.Fatal("a floating policy ref must be rejected")
	}
	if _, err := g.Chat(context.Background(), CallerContext{Tenant: "t1"}, ChatRequest{
		PolicyRef: "model-policy/standard@1.0.0", Messages: []Message{{Role: "user", Content: "hi"}},
	}); err == nil {
		t.Fatal("an unregistered policy must be rejected")
	}
}

func TestTenantIsRequired(t *testing.T) {
	p := &fakeProvider{id: "p1"}
	r := testRegistry(t, p, nil)
	g, _ := New(Config{Registry: r, Secrets: &vault{}})
	if _, err := g.Chat(context.Background(), CallerContext{}, ChatRequest{
		PolicyRef: "model-policy/x@1.0.0", Messages: []Message{{Role: "user", Content: "hi"}},
	}); err == nil || !strings.Contains(err.Error(), "tenant") {
		t.Fatalf("missing tenant must be rejected, got %v", err)
	}
}

func TestNoResolverIsRejectedAtConstruction(t *testing.T) {
	if _, err := New(Config{Registry: NewRegistry()}); err == nil {
		t.Fatal("a gateway without a SecretResolver must not be constructed")
	}
}

func TestChatResolvesPolicyAndAttributesUsage(t *testing.T) {
	p := &fakeProvider{id: "p1", usage: Usage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500}}
	r := testRegistry(t, p, nil)
	if err := r.AddPolicy(Policy{
		Name: "standard", Version: "1.0.0", Temperature: 0.2, MaxTokens: 256,
		Candidates: []string{"small"},
	}); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}
	usage := &MemoryUsage{}
	g, _ := New(Config{Registry: r, Secrets: &vault{}, Usage: usage})

	resp, err := g.Chat(context.Background(),
		CallerContext{Tenant: "tenant-a", Agent: "lead-qualifier", RunID: "run-1"},
		ChatRequest{PolicyRef: "model-policy/standard@1.0.0", Messages: []Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.ModelID != "small" || resp.ProviderID != "p1" {
		t.Fatalf("response must name what ran, got %+v", resp)
	}
	// 1000 prompt @ $0.10/1M + 500 completion @ $0.40/1M = 0.0001 + 0.0002
	want := 0.0003
	if resp.CostUSD < want-1e-9 || resp.CostUSD > want+1e-9 {
		t.Fatalf("cost estimate = %v, want %v", resp.CostUSD, want)
	}
	if len(usage.Events) != 1 {
		t.Fatalf("want one usage record, got %d", len(usage.Events))
	}
	e := usage.Events[0]
	if e.Tenant != "tenant-a" || e.Agent != "lead-qualifier" || e.RunID != "run-1" {
		t.Fatalf("usage must carry attribution, got %+v", e)
	}
	if e.PolicyRef != "model-policy/standard@1.0.0" || e.CostUSD != resp.CostUSD {
		t.Fatalf("usage must carry policy + cost, got %+v", e)
	}
}

func TestFallbackOnRetryableFailure(t *testing.T) {
	primary := &fakeProvider{id: "p1", failFor: 1, class: ErrRetryable}
	backup := &fakeProvider{id: "p2"}
	r := testRegistry(t, primary, backup)
	if err := r.AddPolicy(Policy{
		Name: "resilient", Version: "1.0.0", MaxTokens: 128,
		Candidates: []string{"small", "backup"},
	}); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}
	usage := &MemoryUsage{}
	g, _ := New(Config{Registry: r, Secrets: &vault{}, Usage: usage})

	resp, err := g.Chat(context.Background(), CallerContext{Tenant: "t"},
		ChatRequest{PolicyRef: "model-policy/resilient@1.0.0", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.ModelID != "backup" || resp.ProviderID != "p2" {
		t.Fatalf("fallback must run the next candidate, got %+v", resp)
	}
	if resp.FallbacksUsed != 1 {
		t.Fatalf("want 1 fallback, got %d", resp.FallbacksUsed)
	}
	if len(usage.Events) != 1 || len(usage.Events[0].Attempts) != 1 {
		t.Fatalf("usage must record the failed attempt, got %+v", usage.Events)
	}
	if usage.Events[0].Attempts[0].ErrorClass != ErrRetryable {
		t.Fatalf("failed attempt must keep its class, got %+v", usage.Events[0].Attempts[0])
	}
}

// TestHTTPProviderRoundTrip proves a second, real-adapter provider works end
// to end: an OpenAI-compatible server is stood up locally and the gateway
// routes to it without knowing what is behind it.
func TestHTTPProviderRoundTrip(t *testing.T) {
	var sawAuth, sawModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		sawAuth = req.Header.Get("Authorization")
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		sawModel = body.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"from-http"}}],
			"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`))
	}))
	defer srv.Close()

	httpProvider := &HTTPProvider{Name: "openai-compatible", BaseURL: srv.URL, SecretRef: "vault:llm/openai"}
	secrets := &vault{values: map[string]string{"vault:llm/openai": "sk-from-vault"}}

	r := NewRegistry()
	if err := r.AddProvider(httpProvider); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	if err := r.AddModel(Model{ID: "http-model", ProviderID: "openai-compatible", ContextWindow: 8192}); err != nil {
		t.Fatalf("AddModel: %v", err)
	}
	if err := r.AddPolicy(Policy{
		Name: "http", Version: "1.0.0", MaxTokens: 64, Candidates: []string{"http-model"},
	}); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	g, _ := New(Config{Registry: r, Secrets: secrets})
	resp, err := g.Chat(context.Background(), CallerContext{Tenant: "t"},
		ChatRequest{PolicyRef: "model-policy/http@1.0.0", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "from-http" || resp.Usage.TotalTokens != 10 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if sawModel != "http-model" {
		t.Fatalf("provider saw model %q", sawModel)
	}
	if sawAuth != "Bearer sk-from-vault" {
		t.Fatalf("the key must come from the resolver, saw %q", sawAuth)
	}
	if len(secrets.asked) != 1 || secrets.asked[0] != "vault:llm/openai" {
		t.Fatalf("resolver must be asked for the vault ref, got %v", secrets.asked)
	}
}

func TestContextWindowGuardFailsClosedAndFallsBack(t *testing.T) {
	primary := &fakeProvider{id: "p1"}
	backup := &fakeProvider{id: "p2"}
	r := testRegistry(t, primary, backup)
	if err := r.AddPolicy(Policy{
		Name: "wide", Version: "1.0.0", MaxTokens: 2000,
		Candidates: []string{"small", "backup"}, // small window is 4096
	}); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}
	g, _ := New(Config{Registry: r, Secrets: &vault{}})

	// ~8000 prompt tokens + 2000 completion cannot fit 4096, but fits 128000.
	resp, err := g.Chat(context.Background(), CallerContext{Tenant: "t"},
		ChatRequest{PolicyRef: "model-policy/wide@1.0.0", Messages: []Message{{Role: "user", Content: strings.Repeat("word ", 8000)}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.ModelID != "backup" || resp.FallbacksUsed != 1 {
		t.Fatalf("window guard must skip the small model, got %+v", resp)
	}
	if primary.calls != 0 {
		t.Fatal("a model that cannot fit the window must never be dispatched")
	}
}

func TestCostBudgetGuardFailsClosed(t *testing.T) {
	p := &fakeProvider{id: "p1"}
	r := testRegistry(t, p, nil)
	if err := r.AddPolicy(Policy{
		Name: "cheap", Version: "1.0.0", MaxTokens: 1000, BudgetUSD: 0.00001,
		Candidates: []string{"small"},
	}); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}
	g, _ := New(Config{Registry: r, Secrets: &vault{}})

	_, err := g.Chat(context.Background(), CallerContext{Tenant: "t"},
		ChatRequest{PolicyRef: "model-policy/cheap@1.0.0", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("a call whose worst-case cost exceeds the budget must be refused")
	}
	if p.calls != 0 {
		t.Fatal("a budget-refused call must never reach the provider")
	}
	if !strings.Contains(err.Error(), "budget") {
		t.Fatalf("error must explain the budget refusal, got %v", err)
	}
}

func TestPerCallOverridesCannotWidenPolicy(t *testing.T) {
	p := &fakeProvider{id: "p1"}
	r := testRegistry(t, p, nil)
	if err := r.AddPolicy(Policy{
		Name: "bounded", Version: "1.0.0", MaxTokens: 100, Candidates: []string{"small"},
	}); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}
	g, _ := New(Config{Registry: r, Secrets: &vault{}})

	// A caller may ask for less, but never for more than the policy ceiling.
	resp, err := g.Chat(context.Background(), CallerContext{Tenant: "t"}, ChatRequest{
		PolicyRef: "model-policy/bounded@1.0.0", MaxTokens: 1_000_000,
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.ModelID != "small" {
		t.Fatalf("unexpected model %q", resp.ModelID)
	}
}

func TestUnhealthyProviderIsSkipped(t *testing.T) {
	primary := &fakeProvider{id: "p1", failFor: 5, class: ErrRetryable}
	backup := &fakeProvider{id: "p2"}
	r := testRegistry(t, primary, backup)
	if err := r.AddPolicy(Policy{
		Name: "guarded", Version: "1.0.0", MaxTokens: 64,
		Candidates: []string{"small", "backup"},
	}); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}
	health := NewHealth()
	health.Threshold = 1
	g, _ := New(Config{Registry: r, Secrets: &vault{}, Health: health})

	// First call: primary fails, backup serves, health marks p1 unhealthy.
	if _, err := g.Chat(context.Background(), CallerContext{Tenant: "t"},
		ChatRequest{PolicyRef: "model-policy/guarded@1.0.0", Messages: []Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("first Chat: %v", err)
	}
	if health.FailureCount("p1") != 1 {
		t.Fatalf("p1 must be marked failed, got %d", health.FailureCount("p1"))
	}
	before := primary.calls
	// Second call: p1 must be skipped without a dispatch attempt.
	if _, err := g.Chat(context.Background(), CallerContext{Tenant: "t"},
		ChatRequest{PolicyRef: "model-policy/guarded@1.0.0", Messages: []Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("second Chat: %v", err)
	}
	if primary.calls != before {
		t.Fatal("an unhealthy provider must not be dispatched to")
	}
}

func TestNoCandidateSurfacesAttempts(t *testing.T) {
	p := &fakeProvider{id: "p1", failFor: 10, class: ErrFatal}
	r := testRegistry(t, p, nil)
	if err := r.AddPolicy(Policy{
		Name: "doomed", Version: "1.0.0", MaxTokens: 64, Candidates: []string{"small"},
	}); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}
	g, _ := New(Config{Registry: r, Secrets: &vault{}})
	_, err := g.Chat(context.Background(), CallerContext{Tenant: "t"},
		ChatRequest{PolicyRef: "model-policy/doomed@1.0.0", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("a call with no viable candidate must fail")
	}
	if !strings.Contains(err.Error(), "attempts") {
		t.Fatalf("the failure must list attempts, got %v", err)
	}
}

func TestPolicyRegistryRejectsFloatingAndUnknown(t *testing.T) {
	p := &fakeProvider{id: "p1"}
	r := testRegistry(t, p, nil)
	if err := r.AddPolicy(Policy{Name: "x", Version: "latest", MaxTokens: 10, Candidates: []string{"small"}}); err == nil {
		t.Fatal("an unpinned policy version must be rejected")
	}
	if err := r.AddPolicy(Policy{Name: "y", Version: "1.0.0", MaxTokens: 10, Candidates: []string{"ghost"}}); err == nil {
		t.Fatal("a policy referencing an unknown model must be rejected")
	}
	if err := r.AddModel(Model{ID: "orphan", ProviderID: "nope", ContextWindow: 100}); err == nil {
		t.Fatal("a model referencing an unknown provider must be rejected")
	}
}
