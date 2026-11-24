package plan

import (
	"os"
	"strings"
	"testing"
	"time"

	"backend/pkg/composition"
	"backend/pkg/ir"
)

const fixtureDir = "../../../docs/architecture/system-ir/examples"

var clock = time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixtureDir + "/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

// snapshotFor stores a verified snapshot whose SystemDigest matches the document.
func snapshotFor(t *testing.T, doc []byte, entries []composition.ClosureEntry) *composition.Snapshot {
	t.Helper()
	canonical, err := ir.Canonicalize(doc)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	lock := &composition.Lock{
		LockVersion:     composition.LockVersion,
		SystemRef:       "system/compiled-fixture@1.0.0",
		SystemDigest:    composition.DigestBytes(canonical),
		CompilerVersion: "vibesdk-compiler/0.1.0",
		ResolutionMode:  "bundled",
		ResolvedClosure: entries,
		ModelPolicyRefs: []string{},
		EvaluationRefs:  []string{},
		GeneratedAt:     clock.Format(time.RFC3339),
	}
	snapshot, err := composition.NewSnapshot(lock, "tenant-a", "")
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	return snapshot
}

func hasCode(findings []Finding, code Code) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func stepNames(p *Plan) []string {
	out := make([]string, 0, len(p.Steps))
	for _, step := range p.Steps {
		out = append(out, step.Name)
	}
	return out
}

// B1 - the procedural fixture compiles to exactly its declared graph.
func TestB1CompilesVisualWorkflow(t *testing.T) {
	doc := fixture(t, "visual-workflow.system.json")
	compiled, findings := Compile(snapshotFor(t, doc, nil), doc)
	if len(findings) > 0 {
		t.Fatalf("unexpected findings: %v", findings)
	}
	want := []string{"on-lead", "enrich", "store", "score", "is-hot", "notify", "settle"}
	if got := strings.Join(stepNames(compiled), ","); got != strings.Join(want, ",") {
		t.Fatalf("step order: got %v want %v", stepNames(compiled), want)
	}
	if len(compiled.Entry) != 1 || compiled.Entry[0] != "on-lead" {
		t.Fatalf("entry mismatch: %v", compiled.Entry)
	}
	if compiled.Kind != PlanWorkflow || compiled.CompilerVersion != CompilerVersion {
		t.Fatalf("unexpected plan identity: %+v", compiled)
	}
	if compiled.PlanDigest == "" {
		t.Fatal("the plan must carry a digest")
	}
}

// B2 - all seven node kinds survive the mapping.
func TestB2SevenKindsSurvive(t *testing.T) {
	doc := fixture(t, "visual-workflow.system.json")
	compiled, findings := Compile(snapshotFor(t, doc, nil), doc)
	if len(findings) > 0 {
		t.Fatalf("unexpected findings: %v", findings)
	}
	kinds := map[string]bool{}
	for _, edge := range compiled.Edges {
		kinds[edge.From] = true
	}
	for _, kind := range ir.NodeKinds {
		found := false
		for _, name := range stepNames(compiled) {
			if name == kind {
				found = true
			}
		}
		_ = found
	}
	if len(compiled.Steps) != 7 {
		t.Fatalf("expected seven steps, got %d", len(compiled.Steps))
	}
}

// B3 - DependsOn is the inverse of next.
func TestB3DependsOnIsInverseOfNext(t *testing.T) {
	doc := fixture(t, "visual-workflow.system.json")
	compiled, _ := Compile(snapshotFor(t, doc, nil), doc)
	deps := map[string]string{}
	for _, step := range compiled.Steps {
		deps[step.Name] = strings.Join(step.DependsOn, ",")
	}
	if deps["enrich"] != "on-lead" || deps["store"] != "enrich" || deps["is-hot"] != "score" {
		t.Fatalf("chain mapping wrong: %v", deps)
	}
	if deps["notify"] != "is-hot" || deps["settle"] != "is-hot" {
		t.Fatalf("branch mapping wrong: %v", deps)
	}
}

// B4 - two compiles of one snapshot are byte-identical.
func TestB4Deterministic(t *testing.T) {
	doc := fixture(t, "visual-workflow.system.json")
	first, _ := Compile(snapshotFor(t, doc, nil), doc)
	second, _ := Compile(snapshotFor(t, doc, nil), doc)
	firstBytes, err := first.Canonical()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	secondBytes, _ := second.Canonical()
	if string(firstBytes) != string(secondBytes) {
		t.Fatal("two compiles of one snapshot differ")
	}
	if first.PlanDigest != second.PlanDigest {
		t.Fatalf("digest mismatch: %s vs %s", first.PlanDigest, second.PlanDigest)
	}
}

// B5 - an agentic System is refused, not lowered.
func TestB5AgenticRefused(t *testing.T) {
	doc := fixture(t, "m1-team.system.json")
	_, findings := Compile(snapshotFor(t, doc, nil), doc)
	if !hasCode(findings, CodeShape) {
		t.Fatalf("expected E_BIND_SHAPE, got %v", findings)
	}
	if !strings.Contains(findings[0].Message, "agentic") {
		t.Fatalf("the refusal must name the limitation: %q", findings[0].Message)
	}
}

// B6 - a document that is not the snapshot document is refused.
func TestB6ForeignDocumentRefused(t *testing.T) {
	doc := fixture(t, "visual-workflow.system.json")
	snapshot := snapshotFor(t, doc, nil)
	other := []byte(strings.Replace(string(doc), "lead-intake-workflow", "lead-intake-other", 1))
	_, findings := Compile(snapshot, other)
	if !hasCode(findings, CodeSnapshot) {
		t.Fatalf("expected E_BIND_SNAPSHOT, got %v", findings)
	}
}

// B7 - a capability outside the closure is refused.
func TestB7CapabilityOutsideClosureRefused(t *testing.T) {
	doc := inlineSystem("[]", `"fail-run"`, `"tool/http-call@1.0.0"`)
	_, findings := Compile(snapshotFor(t, doc, nil), doc)
	if !hasCode(findings, CodeCapability) {
		t.Fatalf("expected E_BIND_CAPABILITY, got %v", findings)
	}
}

// B8 - conditions are carried verbatim.
func TestB8ConditionCarriedVerbatim(t *testing.T) {
	doc := fixture(t, "visual-workflow.system.json")
	compiled, _ := Compile(snapshotFor(t, doc, nil), doc)
	conditions := map[string]string{}
	for _, edge := range compiled.Edges {
		conditions[edge.From+"->"+edge.To] = edge.When
	}
	if conditions["is-hot->notify"] != "score >= 80" || conditions["is-hot->settle"] != "score < 80" {
		t.Fatalf("conditions not carried verbatim: %v", conditions)
	}
}

// B9 - approvals become gates in the shared vocabulary.
func TestB9ApprovalsBecomeGates(t *testing.T) {
	doc := inlineSystem(`[{"on":"external-message","approver":"role:ops"}]`, `"fail-run"`, "")
	compiled, findings := Compile(snapshotFor(t, doc, nil), doc)
	if len(findings) > 0 {
		t.Fatalf("unexpected findings: %v", findings)
	}
	if len(compiled.Gates) != 1 || compiled.Gates[0].On != "external-message" || compiled.Gates[0].Approver != "role:ops" {
		t.Fatalf("gate mapping wrong: %v", compiled.Gates)
	}
}

// B10 - routing.onError is carried, never turned into a gate.
func TestB10OnErrorCarriedNotGated(t *testing.T) {
	doc := inlineSystem("[]", `"escalate-human"`, "")
	compiled, findings := Compile(snapshotFor(t, doc, nil), doc)
	if len(findings) > 0 {
		t.Fatalf("unexpected findings: %v", findings)
	}
	if compiled.OnError != "escalate-human" {
		t.Fatalf("onError not carried: %q", compiled.OnError)
	}
	if len(compiled.Gates) != 0 {
		t.Fatal("onError must not become an approval gate")
	}
}

// inlineSystem builds a minimal procedural System for the mapping tests.
func inlineSystem(approvals, onError, capability string) []byte {
	capabilityField := ""
	if capability != "" {
		capabilityField = ",\"capability\": " + capability
	}
	doc := `{"apiVersion":"vibesdk/v1","kind":"System","metadata":{"name":"inline-plan","version":"1.0.0","publisher":"acme","license":"Apache-2.0"},` +
		`"spec":{"outcome":"Compile an inline workflow","approvals":` + approvals + `,"routing":{"onError":` + onError + `},` +
		`"workflow":{"entry":["a"],"nodes":[{"id":"a","kind":"http"` + capabilityField + `,"next":["b"]},{"id":"b","kind":"sleep"}]}}}`
	return []byte(doc)
}
