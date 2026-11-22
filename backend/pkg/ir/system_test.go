package ir

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureDir is the repository's canonical IR fixture set. Tests read the same
// bytes the docs gate validates (scripts/validate-system-ir.mjs), so the Go
// consumer and the gate cannot drift into two different answers.
const fixtureDir = "../../../docs/architecture/system-ir/examples"

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// TestLoadAcceptsTheFourCases is the first acceptance criterion: the compiler
// loads every IR v1 fixture (the four expressiveness cases) with no findings.
func TestLoadAcceptsTheFourCases(t *testing.T) {
	cases := []struct {
		file  string
		shape string
	}{
		{"app-builder.system.json", "agents"},
		{"m0-single-agent.system.json", "agents"},
		{"m1-team.system.json", "agents"},
		{"visual-workflow.system.json", "workflow"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			data := readFixture(t, tc.file)
			sys, problems := Load(data)
			if len(problems) > 0 {
				t.Fatalf("%s should be valid, got: %s", tc.file, strings.Join(problems, "; "))
			}
			if sys == nil {
				t.Fatal("expected a decoded system")
			}
			if sys.Kind != Kind || sys.APIVersion != APIVersion {
				t.Fatalf("unexpected envelope: %s/%s", sys.APIVersion, sys.Kind)
			}
			// The declared shape must match what the fixture claims to prove.
			hasWorkflow := sys.Spec.Workflow != nil
			if (tc.shape == "workflow") != hasWorkflow {
				t.Fatalf("%s: expected the %s shape (workflow=%v)", tc.file, tc.shape, hasWorkflow)
			}
			if tc.shape == "workflow" {
				kinds := map[string]bool{}
				for _, node := range sys.Spec.Workflow.Nodes {
					kinds[node.Kind] = true
				}
				for _, kind := range NodeKinds {
					if !kinds[kind] {
						t.Errorf("%s: workflow case does not exercise node kind %q", tc.file, kind)
					}
				}
			}
		})
	}
}

// TestCanonicalizeRoundTrips pins the second half of "the compiler can consume
// IR": canonical bytes are stable, and canonicalizing them again is a no-op (so
// a semantic diff never shows whitespace or key-order noise).
func TestCanonicalizeRoundTrips(t *testing.T) {
	for _, name := range []string{
		"app-builder.system.json",
		"m0-single-agent.system.json",
		"m1-team.system.json",
		"visual-workflow.system.json",
	} {
		t.Run(name, func(t *testing.T) {
			raw := readFixture(t, name)
			canonical, err := Canonicalize(raw)
			if err != nil {
				t.Fatalf("canonicalize: %v", err)
			}
			if string(canonical) != string(raw) {
				t.Fatalf("%s is not canonically serialized (Go and the docs gate disagree)", name)
			}
			again, err := Canonicalize(canonical)
			if err != nil {
				t.Fatalf("re-canonicalize: %v", err)
			}
			if string(again) != string(canonical) {
				t.Fatal("canonicalization is not idempotent")
			}
		})
	}
}

// mutate parses a fixture into a generic map, applies a mutation, and returns the
// re-encoded bytes — the same technique the docs gate uses for its probes.
func mutate(t *testing.T, name string, apply func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readFixture(t, name), &m); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}
	apply(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal mutated %s: %v", name, err)
	}
	return out
}

func spec(m map[string]any) map[string]any {
	return m["spec"].(map[string]any)
}

func agentsOf(m map[string]any) map[string]any {
	return spec(m)["agents"].(map[string]any)
}

// TestLoadRejectsNegativeProbes is the second acceptance criterion: every probe
// the docs gate defines must be refused by the Go consumer too. If Go accepted a
// probe the gate rejects, the platform would ship a compiler that accepts IR the
// contract forbids.
func TestLoadRejectsNegativeProbes(t *testing.T) {
	base := "m1-team.system.json"
	probes := []struct {
		name   string
		file   string
		mutate func(m map[string]any)
		want   string
	}{
		{
			name:   "runtime status",
			file:   base,
			mutate: func(m map[string]any) { agentsOf(m)["coordinator"].(map[string]any)["status"] = "running" },
			want:   "status",
		},
		{
			name:   "telemetry block",
			file:   base,
			mutate: func(m map[string]any) { spec(m)["telemetry"] = map[string]any{"cost": 0.02} },
			want:   "telemetry",
		},
		{
			name: "unpinned agent ref",
			file: base,
			mutate: func(m map[string]any) {
				agentsOf(m)["drafter"].(map[string]any)["ref"] = "agent/followup-drafter@latest"
			},
			want: "must be agent/name@MAJOR.MINOR.PATCH",
		},
		{
			name: "unknown delegation target",
			file: base,
			mutate: func(m map[string]any) {
				agentsOf(m)["coordinator"].(map[string]any)["delegatesTo"] = []any{"ghost"}
			},
			want: "delegates to unknown agent",
		},
		{
			name: "both execution shapes",
			file: base,
			mutate: func(m map[string]any) {
				spec(m)["workflow"] = map[string]any{"nodes": []any{map[string]any{"id": "only", "kind": "sleep"}}}
			},
			want: "exactly one execution shape",
		},
		{
			name: "dangling workflow edge",
			file: "visual-workflow.system.json",
			mutate: func(m map[string]any) {
				wf := spec(m)["workflow"].(map[string]any)
				nodes := wf["nodes"].([]any)
				nodes[0].(map[string]any)["next"] = []any{"does-not-exist"}
			},
			want: "points to unknown node",
		},
	}

	for _, probe := range probes {
		t.Run(probe.name, func(t *testing.T) {
			data := mutate(t, probe.file, probe.mutate)
			sys, problems := Load(data)
			if len(problems) == 0 {
				t.Fatalf("probe %q was accepted (IR v1 must reject it); decoded: %+v", probe.name, sys)
			}
			if !strings.Contains(strings.Join(problems, "; "), probe.want) {
				t.Fatalf("probe %q was rejected for the wrong reason: %v (wanted %q)", probe.name, problems, probe.want)
			}
		})
	}
}

// TestLoadRejectsNeitherShape pins the other half of I1: a system with no
// execution shape at all is not a system.
func TestLoadRejectsNeitherShape(t *testing.T) {
	data := mutate(t, "m1-team.system.json", func(m map[string]any) {
		delete(spec(m), "agents")
	})
	_, problems := Load(data)
	if len(problems) == 0 {
		t.Fatal("a system with neither agents nor workflow must be rejected")
	}
	if !strings.Contains(strings.Join(problems, "; "), "neither") {
		t.Fatalf("wrong reason: %v", problems)
	}
}

// TestLoadedSystemExposesPinnedClosure shows what the *compiler* gets from the
// loader: every agent slot resolves to an exact semver ref, so a capability
// closure can be resolved without guessing. That is the whole point of consuming
// the IR in Go rather than re-parsing JSON at each call site.
func TestLoadedSystemExposesPinnedClosure(t *testing.T) {
	sys, problems := Load(readFixture(t, "m1-team.system.json"))
	if len(problems) > 0 {
		t.Fatalf("fixture should be valid: %v", problems)
	}
	if len(sys.Spec.Agents) != 3 {
		t.Fatalf("expected the three agent slots, got %d", len(sys.Spec.Agents))
	}
	for name, agent := range sys.Spec.Agents {
		parts := strings.SplitN(agent.Ref, "@", 2)
		if len(parts) != 2 || !semverRe.MatchString(parts[1]) {
			t.Fatalf("agent %q ref %q is not pinned to exact semver", name, agent.Ref)
		}
	}
	// The coordinator declares its delegation, so the closure is walkable.
	if got := sys.Spec.Agents["coordinator"].DelegatesTo; len(got) != 1 || got[0] != "drafter" {
		t.Fatalf("expected coordinator -> drafter delegation, got %v", got)
	}
	// Desired deployment state is readable without touching runtime state.
	if sys.Spec.Deployment == nil || sys.Spec.Deployment.Environment != "staging" {
		t.Fatalf("expected deployment.environment=staging, got %+v", sys.Spec.Deployment)
	}
}

// TestCanonicalizeRejectsBrokenJSON keeps the helper honest: it must fail loudly
// rather than emit a plausible-looking document for unparseable input.
func TestCanonicalizeRejectsBrokenJSON(t *testing.T) {
	if _, err := Canonicalize([]byte("{not json")); err == nil {
		t.Fatal("expected an error for unparseable input")
	}
}
