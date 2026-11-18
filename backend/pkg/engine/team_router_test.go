package engine

import (
	"context"
	"strings"
	"testing"

	"backend/pkg/agent"
	"backend/pkg/skills"
)

// routerTestRoom is a room with a real (offline) eino engine: NewEngine builds
// the provider client without any network call, which is what the role-model
// factory needs.
func routerTestRoom(t *testing.T) *ProjectRoom {
	t.Helper()
	eng, err := agent.NewEngine(context.Background(), agent.EngineConfig{
		BaseURL:       "http://127.0.0.1:1",
		APIKey:        "test",
		Model:         "engine-default",
		MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return NewProjectRoom("router-room", nil, nil, eng)
}

func TestCoderRoutingWithoutPolicyKeepsRoleModel(t *testing.T) {
	t.Setenv(modelRouterPolicyEnv, "")
	t.Setenv(modelRouterCandidatesEnv, "")

	room := routerTestRoom(t)
	got := room.coderRouting(context.Background(), skills.NewRegistry())
	if got.Degraded {
		t.Fatalf("no policy is not a degradation: %s", got.Reason)
	}
	if got.Model == nil {
		t.Fatal("a model is always required")
	}
	if got.Failover != nil {
		t.Error("no policy must mean no failover config (pre-P7 behaviour)")
	}
	if got.Policy != "" || got.Chosen != "" {
		t.Errorf("unexpected routing metadata: policy=%q chosen=%q", got.Policy, got.Chosen)
	}
}

func TestCoderRoutingWithPinnedPolicy(t *testing.T) {
	t.Setenv(modelRouterPolicyEnv, "model-policy/coder@1.0.0")
	// Duplicates and blanks must not create phantom candidates.
	t.Setenv(modelRouterCandidatesEnv, " llama-3.3-70b , llama-3.1-8b ,llama-3.3-70b,")

	room := routerTestRoom(t)
	got := room.coderRouting(context.Background(), skills.NewRegistry())
	if got.Degraded {
		t.Fatalf("pinned policy degraded: %s", got.Reason)
	}
	if got.Policy != "model-policy/coder@1.0.0" {
		t.Errorf("policy = %q", got.Policy)
	}
	if got.Chosen != "llama-3.3-70b" {
		t.Errorf("router started on %q, want the first candidate", got.Chosen)
	}
	if got.Model == nil {
		t.Fatal("router returned no model")
	}
	if got.Failover == nil {
		t.Fatal("a pinned policy must supply the failover config")
	}
	if got.Failover.MaxRetries != 1 {
		t.Errorf("MaxRetries = %d, want 1 (two candidates = one failover)", got.Failover.MaxRetries)
	}
	if got.Failover.GetFailoverModel == nil || got.Failover.ShouldFailover == nil {
		t.Error("failover hooks are not wired")
	}
}

func TestCoderRoutingDegradesOnBadPolicyRef(t *testing.T) {
	// Missing the `model-policy/` prefix: the registry validates the reference
	// shape, so this must degrade instead of failing a run.
	t.Setenv(modelRouterPolicyEnv, "coder@1.0.0")
	t.Setenv(modelRouterCandidatesEnv, "model-a,model-b")

	room := routerTestRoom(t)
	got := room.coderRouting(context.Background(), skills.NewRegistry())
	if !got.Degraded {
		t.Fatal("a malformed policy ref must degrade")
	}
	if got.Failover != nil {
		t.Error("a degraded route must not hand out a failover config")
	}
	if got.Model == nil {
		t.Error("a degraded route must still yield the role model")
	}
	if !strings.Contains(got.Reason, "model-policy/") {
		t.Errorf("reason %q should name the expected reference shape", got.Reason)
	}
}

func TestCoderRoutingWithoutEngine(t *testing.T) {
	t.Setenv(modelRouterPolicyEnv, "model-policy/coder@1.0.0")
	t.Setenv(modelRouterCandidatesEnv, "model-a")

	room := NewProjectRoom("router-no-engine", nil, nil, nil)
	got := room.coderRouting(context.Background(), skills.NewRegistry())
	if !got.Degraded || got.Model != nil {
		t.Fatalf("no engine must degrade without a model, got %+v", got)
	}
}

func TestCoderRoutingDegradesOnBadContextWindow(t *testing.T) {
	t.Setenv(modelRouterPolicyEnv, "model-policy/coder@1.0.0")
	t.Setenv(modelRouterCandidatesEnv, "model-a")
	t.Setenv(modelRouterContextWindowEnv, "wide")

	room := routerTestRoom(t)
	got := room.coderRouting(context.Background(), skills.NewRegistry())
	if !got.Degraded {
		t.Fatal("an unusable context window must degrade")
	}
	if !strings.Contains(got.Reason, "context window") {
		t.Errorf("reason %q should name the context window", got.Reason)
	}
	if got.Model == nil {
		t.Error("a degraded route must still yield the role model")
	}
}

func TestSplitModelCandidates(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{" , ,", nil},
		{"a", []string{"a"}},
		{" a , b ,a", []string{"a", "b"}},
		{"a,b,c", []string{"a", "b", "c"}},
	}
	for _, tc := range cases {
		got := splitModelCandidates(tc.raw)
		if len(got) != len(tc.want) {
			t.Fatalf("splitModelCandidates(%q) = %v, want %v", tc.raw, got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("splitModelCandidates(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		}
	}
}

func TestSplitModelPolicyRef(t *testing.T) {
	name, version, err := splitModelPolicyRef("model-policy/coder-fast@2.1.0")
	if err != nil || name != "coder-fast" || version != "2.1.0" {
		t.Fatalf("split = (%q, %q, %v)", name, version, err)
	}
	for _, bad := range []string{"", "coder@1.0.0", "model-policy/coder", "model-policy/@1.0.0"} {
		if _, _, err := splitModelPolicyRef(bad); err == nil {
			t.Errorf("splitModelPolicyRef(%q) should fail", bad)
		}
	}
}
