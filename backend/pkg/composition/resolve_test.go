package composition

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/pkg/ir"
)

// The resolver is tested against the repository's real fixtures and example
// assets: the same bytes the docs gate validates. A test-only asset set would
// prove the code works on invented data.
const (
	fixtureDir  = "../../../docs/architecture/system-ir/examples"
	examplesDir = "../../../docs/architecture/manifest-schemas/examples"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/golden files")

var fixedNow = time.Date(2026, 11, 23, 10, 15, 0, 0, time.UTC)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func exampleSource(t *testing.T) *BundledSource {
	t.Helper()
	source, err := LoadBundledSource(os.DirFS(examplesDir), ".")
	if err != nil {
		t.Fatalf("load bundled examples: %v", err)
	}
	return source
}

func mutate(t *testing.T, name string, apply func(m map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(fixture(t, name), &doc); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}
	apply(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	return out
}

func specOf(m map[string]any) map[string]any { return m["spec"].(map[string]any) }

// stub builds a catalog asset: the reserved namespaces have no manifest schema,
// so they are supplied explicitly (composition-resolver.md §3).
func stub(ref, publisher string, refs ...string) Asset {
	parts := strings.SplitN(ref, "@", 2)
	name := strings.SplitN(parts[0], "/", 2)[1]
	body, _ := json.Marshal(map[string]any{
		"metadata": map[string]any{"name": name, "publisher": publisher, "version": parts[1]},
	})
	parsed, err := ParseRef(ref)
	if err != nil {
		panic(err)
	}
	return Asset{Ref: ref, Kind: parsed.Kind, Publisher: publisher, Bytes: body, Refs: refs}
}

func assetBytes(body map[string]any) []byte {
	out, _ := json.Marshal(body)
	return out
}

// realisticSource is the real example set plus the catalog stubs the M1 fixture
// needs. It proves two things at once: transitive edges through real manifests
// (agent -> skill -> tool) and derived modelPolicy/evaluation subsets.
func realisticSource(t *testing.T) AssetSource {
	t.Helper()
	base := exampleSource(t)
	assets := base.Assets()
	assets = append(assets,
		stub("agent/followup-drafter@0.9.0", "acme"),
		stub("agent/draft-reviewer@0.4.0", "acme"),
		stub("knowledge/icp-docs@5.0.0", "acme"),
		stub("model-policy/standard@1.0.0", "acme"),
		stub("model-policy/cheap@1.0.0", "acme"),
		stub("eval/lead-set@2.0.0", "acme"),
	)
	source, err := NewBundledSource(assets...)
	if err != nil {
		t.Fatalf("compose source: %v", err)
	}
	return source
}

func resolveOK(t *testing.T, source AssetSource, manifest []byte) *Lock {
	t.Helper()
	result := Resolve(context.Background(), Request{
		Manifest: manifest, Mode: ModeBundled, Source: source,
		CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow,
	})
	if len(result.Findings) > 0 {
		t.Fatalf("expected a lock, got findings: %v", result.Findings)
	}
	if result.Lock == nil {
		t.Fatal("expected a lock, got nil")
	}
	return result.Lock
}

func codes(findings []Finding) []Code {
	out := make([]Code, 0, len(findings))
	for _, finding := range findings {
		out = append(out, finding.Code)
	}
	return out
}

func hasCode(findings []Finding, code Code) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

// --- positive cases (composition-resolver.md §7) -----------------------------

// P1: a manifest with no capability references resolves to an empty closure, in
// real bundled mode, with a system digest that matches the canonical bytes.
func TestP1EmptyClosureFromRealBundledSet(t *testing.T) {
	raw := fixture(t, "visual-workflow.system.json")
	lock := resolveOK(t, exampleSource(t), raw)

	if len(lock.ResolvedClosure) != 0 {
		t.Fatalf("expected an empty closure, got %d entries", len(lock.ResolvedClosure))
	}
	if lock.SystemRef != "system/lead-intake-workflow@1.0.0" {
		t.Fatalf("unexpected systemRef %q", lock.SystemRef)
	}
	canonical, err := ir.Canonicalize(raw)
	if err != nil {
		t.Fatalf("canonicalize fixture: %v", err)
	}
	if lock.SystemDigest != DigestBytes(canonical) {
		t.Fatalf("systemDigest does not match the canonical fixture bytes")
	}
	if len(lock.ModelPolicyRefs) != 0 || len(lock.EvaluationRefs) != 0 {
		t.Fatalf("expected empty derived subsets, got %v / %v", lock.ModelPolicyRefs, lock.EvaluationRefs)
	}
}

// P2: the M1 fixture over the real manifests plus catalog stubs produces the
// full transitive closure, sorted, with derived subsets.
func TestP2M1TransitiveClosure(t *testing.T) {
	lock := resolveOK(t, realisticSource(t), fixture(t, "m1-team.system.json"))

	want := []string{
		"agent/draft-reviewer@0.4.0",
		"agent/followup-drafter@0.9.0",
		"agent/lead-qualifier@1.2.0",
		"eval/lead-set@2.0.0",
		"knowledge/icp-docs@5.0.0",
		"model-policy/cheap@1.0.0",
		"model-policy/standard@1.0.0",
		"skill/qualification-rubric@3.0.1",
		"tool/score-lead@1.0.0",
	}
	got := make([]string, 0, len(lock.ResolvedClosure))
	for _, entry := range lock.ResolvedClosure {
		got = append(got, entry.Ref)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("closure mismatch\n got: %v\nwant: %v", got, want)
	}

	for _, entry := range lock.ResolvedClosure {
		if entry.Digest == "" || !strings.HasPrefix(entry.Digest, DigestPrefix) {
			t.Errorf("%s has no digest", entry.Ref)
		}
		if entry.Publisher == "" {
			t.Errorf("%s lost its publisher", entry.Ref)
		}
	}

	// The skill is reached through the agent, and the tool through both the
	// agent and the skill: a shared dependency, not a cycle.
	tool := entryFor(t, lock, "tool/score-lead@1.0.0")
	if strings.Join(tool.RequestedBy, ",") != "agent/lead-qualifier@1.2.0,skill/qualification-rubric@3.0.1" {
		t.Fatalf("unexpected requestedBy for the tool: %v", tool.RequestedBy)
	}
	skill := entryFor(t, lock, "skill/qualification-rubric@3.0.1")
	if strings.Join(skill.RequestedBy, ",") != "agent/lead-qualifier@1.2.0" {
		t.Fatalf("unexpected requestedBy for the skill: %v", skill.RequestedBy)
	}

	// skill.spec.compatibility.agents is a compatibility statement, not an edge:
	// if it were an edge this closure would contain a cycle and fail.
	if len(lock.ModelPolicyRefs) != 2 || lock.ModelPolicyRefs[0] != "model-policy/cheap@1.0.0" {
		t.Fatalf("unexpected modelPolicyRefs: %v", lock.ModelPolicyRefs)
	}
	if len(lock.EvaluationRefs) != 1 || lock.EvaluationRefs[0] != "eval/lead-set@2.0.0" {
		t.Fatalf("unexpected evaluationRefs: %v", lock.EvaluationRefs)
	}
}

func entryFor(t *testing.T, lock *Lock, ref string) ClosureEntry {
	t.Helper()
	for _, entry := range lock.ResolvedClosure {
		if entry.Ref == ref {
			return entry
		}
	}
	t.Fatalf("closure has no entry for %s", ref)
	return ClosureEntry{}
}

// P3 is deliberately a negative in practice: the real bundled example set is NOT
// self-closing (the agent references knowledge/model-policy/eval catalogs that
// are not shipped), so a richer System must fail with E_MISSING rather than
// invent placeholders.
func TestP3RealBundledSetIsNotSelfClosing(t *testing.T) {
	result := Resolve(context.Background(), Request{
		Manifest: fixture(t, "m0-single-agent.system.json"), Mode: ModeBundled,
		Source: exampleSource(t), CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow,
	})
	if result.Lock != nil {
		t.Fatal("a partially resolvable manifest must not produce a lock")
	}
	if !hasCode(result.Findings, CodeMissing) {
		t.Fatalf("expected E_MISSING, got %v", codes(result.Findings))
	}
}

// P4: a diamond is not a cycle.
func TestP4DiamondResolvesOnce(t *testing.T) {
	source, err := NewBundledSource(
		stub("model-policy/diamond-leaf@1.0.0", "acme"),
		stub("agent/diamond-b@1.0.0", "acme", "model-policy/diamond-leaf@1.0.0"),
		stub("agent/diamond-c@1.0.0", "acme", "model-policy/diamond-leaf@1.0.0"),
	)
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	manifest := systemWithAgents(t, map[string]string{
		"coordinator": "agent/diamond-b@1.0.0",
		"worker":      "agent/diamond-c@1.0.0",
	})
	lock := resolveOK(t, source, manifest)
	leaf := entryFor(t, lock, "model-policy/diamond-leaf@1.0.0")
	if strings.Join(leaf.RequestedBy, ",") != "agent/diamond-b@1.0.0,agent/diamond-c@1.0.0" {
		t.Fatalf("diamond leaf should list both parents, got %v", leaf.RequestedBy)
	}
}

// P5: a transitive-only asset is attributed to the asset that pulled it in.
func TestP5TransitiveAttribution(t *testing.T) {
	source, err := NewBundledSource(
		stub("tool/transitive-t@1.0.0", "acme"),
		stub("skill/transitive-s@1.0.0", "acme", "tool/transitive-t@1.0.0"),
		stub("agent/transitive-a@1.0.0", "acme", "skill/transitive-s@1.0.0"),
	)
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	lock := resolveOK(t, source, systemWithAgents(t, map[string]string{"only": "agent/transitive-a@1.0.0"}))
	if got := strings.Join(entryFor(t, lock, "tool/transitive-t@1.0.0").RequestedBy, ","); got != "skill/transitive-s@1.0.0" {
		t.Fatalf("tool should be attributed to the skill, got %q", got)
	}
	if got := strings.Join(entryFor(t, lock, "agent/transitive-a@1.0.0").RequestedBy, ","); got != "system/parity-composition@1.0.0" {
		t.Fatalf("agent should be attributed to the system, got %q", got)
	}
}

// P6: with the clock injected, a second resolution is byte-identical.
func TestP6ReResolutionIsIdentical(t *testing.T) {
	raw := fixture(t, "m1-team.system.json")
	first := resolveOK(t, realisticSource(t), raw)
	second := resolveOK(t, realisticSource(t), raw)
	firstBytes, err := first.Canonical()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	secondBytes, err := second.Canonical()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	if string(firstBytes) != string(secondBytes) {
		t.Fatal("two resolutions of the same input differ")
	}
	firstDigest, _ := first.Digest()
	secondDigest, _ := second.Digest()
	if firstDigest != secondDigest {
		t.Fatalf("lock digests differ: %s vs %s", firstDigest, secondDigest)
	}
}

// systemWithAgents builds a minimal valid System manifest for synthetic sets.
func systemWithAgents(t *testing.T, agents map[string]string) []byte {
	t.Helper()
	agentDocs := map[string]any{}
	order := make([]string, 0, len(agents))
	for name := range agents {
		order = append(order, name)
	}
	// keep a deterministic order for readable failure output
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			if order[j] < order[i] {
				order[i], order[j] = order[j], order[i]
			}
		}
	}
	topology := make([]any, 0, len(order))
	for _, name := range order {
		agentDocs[name] = map[string]any{"ref": agents[name]}
		topology = append(topology, map[string]any{"run": name})
	}
	doc := map[string]any{
		"apiVersion": "vibesdk/v1",
		"kind":       "System",
		"metadata": map[string]any{
			"name": "parity-composition", "version": "1.0.0", "publisher": "acme", "license": "Apache-2.0",
		},
		"spec": map[string]any{
			"outcome":  "Prove the resolver on a synthetic closure",
			"agents":   agentDocs,
			"topology": topology,
		},
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal synthetic system: %v", err)
	}
	return out
}

// --- test sources -------------------------------------------------------------

type countingSource struct {
	inner AssetSource
	calls int
}

func (c *countingSource) Fetch(ctx context.Context, ref string) (Asset, error) {
	c.calls++
	return c.inner.Fetch(ctx, ref)
}

type refMismatchSource struct{ asset Asset }

func (s refMismatchSource) Fetch(context.Context, string) (Asset, error) { return s.asset, nil }

type unavailableSource struct{}

func (unavailableSource) Fetch(context.Context, string) (Asset, error) {
	return Asset{}, ErrSourceUnavailable
}

type mutatingSource struct {
	asset  Asset
	mutate bool
	calls  int
}

func (s *mutatingSource) Fetch(context.Context, string) (Asset, error) {
	s.calls++
	if !s.mutate {
		return s.asset, nil
	}
	changed := s.asset
	changed.Bytes = assetBytes(map[string]any{"metadata": map[string]any{"name": "drifted", "publisher": "acme", "version": "1.0.0"}})
	return changed, nil
}

// --- negative cases ----------------------------------------------------------

// N1: an invalid manifest is refused before anything is fetched.
func TestN1InvalidManifestRefusedBeforeFetch(t *testing.T) {
	broken := mutate(t, "m1-team.system.json", func(m map[string]any) { delete(specOf(m), "topology") })
	source := &countingSource{inner: realisticSource(t)}
	result := Resolve(context.Background(), Request{
		Manifest: broken, Mode: ModeBundled, Source: source,
		CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow,
	})
	if !hasCode(result.Findings, CodeIR) {
		t.Fatalf("expected E_IR, got %v", codes(result.Findings))
	}
	if source.calls != 0 {
		t.Fatalf("expected zero source calls, got %d", source.calls)
	}
}

// N2: a transitive reference that does not exist fails closed.
func TestN2MissingTransitiveReference(t *testing.T) {
	source, err := NewBundledSource(stub("agent/gap-a@1.0.0", "acme", "tool/absent@9.9.9"))
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	result := Resolve(context.Background(), Request{
		Manifest: systemWithAgents(t, map[string]string{"only": "agent/gap-a@1.0.0"}), Mode: ModeBundled,
		Source: source, CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow,
	})
	if result.Lock != nil {
		t.Fatal("a missing asset must not produce a lock")
	}
	if !hasCode(result.Findings, CodeMissing) {
		t.Fatalf("expected E_MISSING, got %v", codes(result.Findings))
	}
}

// N3: non-exact references are refused by the grammar, before any fetch.
func TestN3NonExactReferencesAreGrammarErrors(t *testing.T) {
	for _, raw := range []string{
		"agent/x@latest", "agent/x@^1.0.0", "agent/x@1.x", "agent/x@1.0.0-rc.1",
		"agent/x@01.0.0", "agent/X@1.0.0", "agent/x", "Agent/x@1.0.0", "agent/x@1.0.0 ",
	} {
		if _, err := ParseRef(raw); err == nil {
			t.Errorf("ParseRef accepted %q", raw)
		}
	}
	if _, err := ParseRef("agent/x@1.0.0"); err != nil {
		t.Errorf("ParseRef rejected a valid reference: %v", err)
	}

	// The same rule applies to an edge declared by an asset.
	source, err := NewBundledSource(stub("agent/grammar-a@1.0.0", "acme", "tool/x@latest"))
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	result := Resolve(context.Background(), Request{
		Manifest: systemWithAgents(t, map[string]string{"only": "agent/grammar-a@1.0.0"}), Mode: ModeBundled,
		Source: source, CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow,
	})
	if !hasCode(result.Findings, CodeGrammar) {
		t.Fatalf("expected E_GRAMMAR, got %v", codes(result.Findings))
	}
}

// N4: an asset that exists only at another version is missing, not "close enough".
func TestN4WrongVersionIsMissing(t *testing.T) {
	source, err := NewBundledSource(stub("agent/versioned-a@2.0.0", "acme"))
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	result := Resolve(context.Background(), Request{
		Manifest: systemWithAgents(t, map[string]string{"only": "agent/versioned-a@1.0.0"}), Mode: ModeBundled,
		Source: source, CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow,
	})
	if !hasCode(result.Findings, CodeMissing) {
		t.Fatalf("expected E_MISSING, got %v", codes(result.Findings))
	}
}

// N5: a source that redirects is refused.
func TestN5RefMismatch(t *testing.T) {
	result := Resolve(context.Background(), Request{
		Manifest: systemWithAgents(t, map[string]string{"only": "agent/redirect-a@1.0.0"}), Mode: ModeBundled,
		Source: refMismatchSource{asset: stub("agent/other-a@1.0.0", "acme")},
		CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow,
	})
	if !hasCode(result.Findings, CodeRefMismatch) {
		t.Fatalf("expected E_REF_MISMATCH, got %v", codes(result.Findings))
	}
}

// N6: provenance is copied, never invented.
func TestN6UnsignedAsset(t *testing.T) {
	source, err := NewBundledSource(stub("agent/unsigned-a@1.0.0", ""))
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	result := Resolve(context.Background(), Request{
		Manifest: systemWithAgents(t, map[string]string{"only": "agent/unsigned-a@1.0.0"}), Mode: ModeBundled,
		Source: source, CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow,
	})
	if !hasCode(result.Findings, CodeAssetUnsigned) {
		t.Fatalf("expected E_ASSET_UNSIGNED, got %v", codes(result.Findings))
	}
}

// N7: a literal credential in an asset is refused; vault: references are fine.
func TestN7LiteralCredentialRefused(t *testing.T) {
	leaky := Asset{
		Ref: "tool/leaky@1.0.0", Kind: KindTool, Publisher: "acme",
		Bytes: assetBytes(map[string]any{
			"metadata": map[string]any{"name": "leaky", "publisher": "acme", "version": "1.0.0"},
			"spec":     map[string]any{"apiKey": "sk-live-51H8"},
		}),
	}
	source, err := NewBundledSource(leaky)
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	result := Resolve(context.Background(), Request{
		Manifest: systemWithAgents(t, map[string]string{"only": "agent/leaky-a@1.0.0"}), Mode: ModeBundled,
		Source: source, CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow,
	})
	// The agent itself is absent, so E_MISSING appears first; the credential case
	// is asserted directly against the scanner.
	if key, bad := ScanForSecret(leaky.Bytes); !bad || key != "apiKey" {
		t.Fatalf("expected the scanner to catch apiKey, got %q (found=%v)", key, bad)
	}

	vaulted := Asset{
		Ref: "tool/vaulted@1.0.0", Kind: KindTool, Publisher: "acme",
		Bytes: assetBytes(map[string]any{
			"metadata": map[string]any{"name": "vaulted", "publisher": "acme", "version": "1.0.0"},
			"spec":     map[string]any{"apiKey": "vault:acme/crm", "secrets": []any{map[string]any{"name": "key", "vault": "vault:acme/crm"}}},
		}),
	}
	if key, bad := ScanForSecret(vaulted.Bytes); bad {
		t.Fatalf("vault: references must be accepted, scanner flagged %q", key)
	}

	withLeak, err := NewBundledSource(stub("agent/leak-parent@1.0.0", "acme", "tool/leaky@1.0.0"), leaky)
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	leakResult := Resolve(context.Background(), Request{
		Manifest: systemWithAgents(t, map[string]string{"only": "agent/leak-parent@1.0.0"}), Mode: ModeBundled,
		Source: withLeak, CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow,
	})
	if !hasCode(leakResult.Findings, CodeSecretInAsset) {
		t.Fatalf("expected E_SECRET_IN_ASSET, got %v", codes(leakResult.Findings))
	}
	_ = result
}

// N8: a source whose content drifts cannot be silently reused. E_CONFLICT_DIGEST
// itself is defensive (reachable only through a source that mutates inside one
// resolution); the guarantee this test pins is the observable one: changed bytes
// produce a different lock, never the same one.
func TestN8ChangedContentChangesTheLock(t *testing.T) {
	source := &mutatingSource{asset: stub("agent/drift-a@1.0.0", "acme")}
	manifest := systemWithAgents(t, map[string]string{"only": "agent/drift-a@1.0.0"})
	stable := resolveOK(t, source, manifest)
	stableDigest, err := stable.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	source.mutate = true
	drifted := resolveOK(t, source, manifest)
	driftedDigest, err := drifted.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if stableDigest == driftedDigest {
		t.Fatal("a content change must change the lock digest")
	}
}

// N9: one identity may appear at exactly one version in a closure.
func TestN9IncompatibleVersionsRefused(t *testing.T) {
	source, err := NewBundledSource(
		stub("tool/http-call@1.2.0", "acme"),
		stub("tool/http-call@2.0.0", "acme"),
		stub("agent/inc-a@1.0.0", "acme", "tool/http-call@1.2.0"),
		stub("agent/inc-b@1.0.0", "acme", "tool/http-call@2.0.0"),
	)
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	result := Resolve(context.Background(), Request{
		Manifest: systemWithAgents(t, map[string]string{
			"first":  "agent/inc-a@1.0.0",
			"second": "agent/inc-b@1.0.0",
		}), Mode: ModeBundled,
		Source: source, CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow,
	})
	if result.Lock != nil {
		t.Fatal("an incompatible closure must not produce a lock")
	}
	if !hasCode(result.Findings, CodeIncompatibleVersion) {
		t.Fatalf("expected E_INCOMPATIBLE_VERSION, got %v", codes(result.Findings))
	}
}

// N10: a dependency cycle is refused with its path.
func TestN10CycleRefusedWithPath(t *testing.T) {
	source, err := NewBundledSource(
		stub("agent/cyc-a@1.0.0", "acme", "skill/cyc-s@1.0.0"),
		stub("skill/cyc-s@1.0.0", "acme", "agent/cyc-a@1.0.0"),
	)
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	result := Resolve(context.Background(), Request{
		Manifest: systemWithAgents(t, map[string]string{"only": "agent/cyc-a@1.0.0"}), Mode: ModeBundled,
		Source: source, CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow,
	})
	if !hasCode(result.Findings, CodeCycle) {
		t.Fatalf("expected E_CYCLE, got %v", codes(result.Findings))
	}
	var cycle Finding
	for _, finding := range result.Findings {
		if finding.Code == CodeCycle {
			cycle = finding
		}
	}
	if !strings.Contains(cycle.Message, " -> ") {
		t.Fatalf("cycle finding should carry a path, got %q", cycle.Message)
	}
}

// N11: the mode is a closed vocabulary.
func TestN11UnknownModeRefused(t *testing.T) {
	result := Resolve(context.Background(), Request{
		Manifest: fixture(t, "m1-team.system.json"), Mode: Mode("guess"),
		Source: exampleSource(t), CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow,
	})
	if !hasCode(result.Findings, CodeMode) {
		t.Fatalf("expected E_MODE, got %v", codes(result.Findings))
	}
}

// N12: infrastructure failure is distinct from "the asset does not exist".
func TestN12SourceUnavailableIsNotMissing(t *testing.T) {
	result := Resolve(context.Background(), Request{
		Manifest: fixture(t, "m1-team.system.json"), Mode: ModeRegistry,
		Source: unavailableSource{}, CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow,
	})
	if !hasCode(result.Findings, CodeSource) {
		t.Fatalf("expected E_SOURCE, got %v", codes(result.Findings))
	}
	if hasCode(result.Findings, CodeMissing) {
		t.Fatal("a transient source failure must not be reported as a missing asset")
	}
}

// --- determinism -------------------------------------------------------------

// D1: repeated resolution never depends on map iteration order.
func TestD1HundredResolutionsAreIdentical(t *testing.T) {
	raw := fixture(t, "m1-team.system.json")
	var want string
	for i := 0; i < 100; i++ {
		lock := resolveOK(t, realisticSource(t), raw)
		canonical, err := lock.Canonical()
		if err != nil {
			t.Fatalf("canonical: %v", err)
		}
		if i == 0 {
			want = string(canonical)
			continue
		}
		if string(canonical) != want {
			t.Fatalf("resolution %d differs from the first", i)
		}
	}
}

// D2: the order in which agents are written in the manifest cannot leak into the
// lock (resolution sorts; discovery order is irrelevant).
func TestD2DeclarationOrderDoesNotLeak(t *testing.T) {
	source, err := NewBundledSource(
		stub("agent/order-a@1.0.0", "acme"),
		stub("agent/order-b@1.0.0", "acme"),
	)
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	template := `{"apiVersion":"vibesdk/v1","kind":"System","metadata":{"name":"order-test","version":"1.0.0","publisher":"acme","license":"Apache-2.0"},` +
		`"spec":{"outcome":"Prove order independence","agents":{%s},"topology":[%s]}}`
	// Only the OBJECT key order varies. Topology is an array, so its order is
	// semantic content and must change the digest — that is covered by P6/G1,
	// not here.
	topology := `{"run":"a"},{"run":"b"}`
	forward := []byte(fmt.Sprintf(template,
		`"a":{"ref":"agent/order-a@1.0.0"},"b":{"ref":"agent/order-b@1.0.0"}`, topology))
	reversed := []byte(fmt.Sprintf(template,
		`"b":{"ref":"agent/order-b@1.0.0"},"a":{"ref":"agent/order-a@1.0.0"}`, topology))

	firstLock := resolveOK(t, source, forward)
	secondLock := resolveOK(t, source, reversed)
	first, _ := firstLock.Canonical()
	second, _ := secondLock.Canonical()
	if string(first) != string(second) {
		t.Fatal("declaration order changed the lock")
	}
}

// D3: the resolver is re-entrant: concurrent resolutions agree.
func TestD3ConcurrentResolutionsAgree(t *testing.T) {
	raw := fixture(t, "m1-team.system.json")
	source := realisticSource(t)
	var wg sync.WaitGroup
	digests := make([]string, 16)
	for i := range digests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result := Resolve(context.Background(), Request{
				Manifest: raw, Mode: ModeBundled, Source: source,
				CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow,
			})
			if len(result.Findings) > 0 {
				digests[i] = "findings: " + strings.Join(codeStrings(result.Findings), "; ")
				return
			}
			digest, err := result.Lock.Digest()
			if err != nil {
				digests[i] = "error: " + err.Error()
				return
			}
			digests[i] = digest
		}(i)
	}
	wg.Wait()
	for i := 1; i < len(digests); i++ {
		if digests[i] != digests[0] {
			t.Fatalf("goroutine %d disagreed: %s vs %s", i, digests[i], digests[0])
		}
	}
}

func codeStrings(findings []Finding) []string {
	out := make([]string, 0, len(findings))
	for _, finding := range findings {
		out = append(out, finding.String())
	}
	return out
}

// --- golden file -------------------------------------------------------------

// G1: the canonical lock for the M1 fixture is a deliberate diff. Run with
// -update to rewrite it after an intended shape change.
func TestG1GoldenLock(t *testing.T) {
	lock := resolveOK(t, realisticSource(t), fixture(t, "m1-team.system.json"))
	canonical, err := lock.Canonical()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	golden := filepath.Join("testdata", "m1-team.lock.json")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(golden, canonical, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create it): %v", err)
	}
	if string(canonical) != string(want) {
		t.Fatalf("lock differs from the golden file\n got:\n%s\nwant:\n%s", canonical, want)
	}
}

// Assert the resolver never imports the clock it is supposed to be given.
func TestGeneratedAtComesFromTheRequest(t *testing.T) {
	lock := resolveOK(t, realisticSource(t), fixture(t, "m1-team.system.json"))
	if lock.GeneratedAt != "2026-11-23T10:15:00Z" {
		t.Fatalf("generatedAt should come from the injected clock, got %q", lock.GeneratedAt)
	}
	other := Resolve(context.Background(), Request{
		Manifest: fixture(t, "m1-team.system.json"), Mode: ModeBundled, Source: realisticSource(t),
		CompilerVersion: "vibesdk-compiler/0.1.0", Now: fixedNow.Add(48 * time.Hour),
	})
	if len(other.Findings) > 0 {
		t.Fatalf("unexpected findings: %v", other.Findings)
	}
	first, _ := lock.Digest()
	second, _ := other.Lock.Digest()
	if first != second {
		t.Fatal("generatedAt must not affect the lock digest")
	}
}

// ErrNotAnAsset must be identifiable so a bundled directory can contain a System.
func TestSystemManifestIsNotAnAsset(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(examplesDir, "inbound-sales.system.json"))
	if err != nil {
		t.Fatalf("read system example: %v", err)
	}
	if _, err := ParseAsset(data); !errors.Is(err, ErrNotAnAsset) {
		t.Fatalf("expected ErrNotAnAsset, got %v", err)
	}
}
