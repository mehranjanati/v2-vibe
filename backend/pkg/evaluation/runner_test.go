package evaluation

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"backend/pkg/composition"
	"backend/pkg/ir"
)

const fixturePath = "../../../docs/architecture/system-ir/examples/m1-team.system.json"

var clock = time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)

func systemDocument(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

// store is the minimal SnapshotStore the runner needs.
type store map[string]composition.Snapshot

func (s store) Persist(_ context.Context, snapshot composition.Snapshot) error {
	s[snapshot.SnapshotID] = snapshot
	return nil
}

func (s store) Load(_ context.Context, id string) (*composition.Snapshot, error) {
	row, ok := s[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", composition.ErrSnapshotMissing, id)
	}
	return &row, nil
}

func entry(ref, digest string, requestedBy ...string) composition.ClosureEntry {
	kind := strings.SplitN(ref, "/", 2)[0]
	return composition.ClosureEntry{Ref: ref, Kind: kind, Publisher: "acme", Digest: digest, RequestedBy: requestedBy}
}

const goodDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// snapshotFor stores a verified snapshot whose SystemDigest matches the document.
func snapshotFor(t *testing.T, doc []byte, entries []composition.ClosureEntry) (store, string) {
	t.Helper()
	canonical, err := ir.Canonicalize(doc)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	lock := &composition.Lock{
		LockVersion:     composition.LockVersion,
		SystemRef:       "system/m1-qualify-then-draft@1.0.0",
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
	if findings := composition.Verify(snapshot, snapshot.SnapshotID); len(findings) > 0 {
		t.Fatalf("the test snapshot must verify: %v", findings)
	}
	return store{snapshot.SnapshotID: *snapshot}, snapshot.SnapshotID
}

func request(doc []byte, snapshotID string, layers ...Layer) Request {
	return Request{
		SnapshotID: snapshotID, SystemDocument: doc, Layers: layers,
		Now: clock, EvaluatorVersion: "vibesdk-eval/0.1.0",
	}
}

func layerOf(t *testing.T, result Result, layer Layer) LayerResult {
	t.Helper()
	for _, got := range result.Layers {
		if got.Layer == layer {
			return got
		}
	}
	t.Fatalf("result has no layer %s", layer)
	return LayerResult{}
}

// E1 - a clean subject passes L1 and L2.
func TestE1StructuralAndCapabilityPass(t *testing.T) {
	doc := systemDocument(t)
	st, id := snapshotFor(t, doc, []composition.ClosureEntry{entry("agent/lead-qualifier@1.2.0", goodDigest)})
	result := Evaluate(context.Background(), request(doc, id, LayerStructural, LayerCapability), st)
	if len(result.Findings) > 0 {
		t.Fatalf("unexpected findings: %v", result.Findings)
	}
	if got := layerOf(t, result, LayerStructural).Verdict; got != VerdictPass {
		t.Fatalf("L1 = %s", got)
	}
	if got := layerOf(t, result, LayerCapability).Verdict; got != VerdictPass {
		t.Fatalf("L2 = %s (%s)", got, layerOf(t, result, LayerCapability).Reason)
	}
	if result.Verdict != VerdictPass || ExitCode(result) != 0 {
		t.Fatalf("verdict %s exit %d", result.Verdict, ExitCode(result))
	}
}

// E2 - a document that is not the snapshot's is a subject refusal.
func TestE2ForeignDocumentRefused(t *testing.T) {
	doc := systemDocument(t)
	st, id := snapshotFor(t, doc, nil)
	other := []byte(strings.Replace(string(doc), "m1-qualify-then-draft", "m1-qualify-other", 1))
	result := Evaluate(context.Background(), request(other, id, LayerStructural), st)
	if len(result.Findings) == 0 || result.Findings[0].Code != CodeSubject {
		t.Fatalf("expected E_EVAL_SUBJECT, got %v", result.Findings)
	}
	if ExitCode(result) != 3 {
		t.Fatalf("expected exit 3, got %d", ExitCode(result))
	}
}

// E3 - a missing snapshot refuses the run.
func TestE3MissingSnapshotRefused(t *testing.T) {
	doc := systemDocument(t)
	st, _ := snapshotFor(t, doc, nil)
	result := Evaluate(context.Background(), request(doc, "sha256:missing", LayerStructural), st)
	if len(result.Findings) == 0 || result.Findings[0].Code != CodeSubject || ExitCode(result) != 3 {
		t.Fatalf("expected E_EVAL_SUBJECT/exit 3, got %v exit %d", result.Findings, ExitCode(result))
	}
}

// E4 - a stored lock whose closure carries a non-pinned ref fails L2.
//
// A digest without its algorithm prefix cannot reach here through a stored
// snapshot: composition.Verify rejects it first. The digest check inside
// evaluateCapability is therefore defence in depth, exercised directly below.
func TestE4FloatingRefFailsCapability(t *testing.T) {
	doc := systemDocument(t)
	st, id := snapshotFor(t, doc, []composition.ClosureEntry{entry("agent/lead-qualifier@latest", goodDigest)})
	result := Evaluate(context.Background(), request(doc, id, LayerCapability), st)
	if got := layerOf(t, result, LayerCapability).Verdict; got != VerdictFail {
		t.Fatalf("expected L2 fail, got %s", got)
	}
	if result.Verdict != VerdictFail || ExitCode(result) != 1 {
		t.Fatalf("verdict %s exit %d", result.Verdict, ExitCode(result))
	}
}

// E4b - the digest-prefix check itself, called on an unverified lock.
func TestE4bUnprefixedDigestFailsCapability(t *testing.T) {
	lock := &composition.Lock{ResolvedClosure: []composition.ClosureEntry{entry("agent/lead-qualifier@1.2.0", "deadbeef")}}
	layer := evaluateCapability(lock)
	if layer.Verdict != VerdictFail || !strings.Contains(layer.Reason, "algorithm prefix") {
		t.Fatalf("expected an algorithm-prefix failure, got %s / %q", layer.Verdict, layer.Reason)
	}
}

// E5 - one identity at two versions fails L2.
func TestE5IncompatibleVersionsFail(t *testing.T) {
	doc := systemDocument(t)
	st, id := snapshotFor(t, doc, []composition.ClosureEntry{
		entry("tool/http-call@1.2.0", goodDigest),
		entry("tool/http-call@2.0.0", goodDigest),
	})
	result := Evaluate(context.Background(), request(doc, id, LayerCapability), st)
	layer := layerOf(t, result, LayerCapability)
	if layer.Verdict != VerdictFail || !strings.Contains(layer.Reason, "both") {
		t.Fatalf("expected an incompatible-version failure, got %s / %q", layer.Verdict, layer.Reason)
	}
}

// E6 - a closure cycle fails L2 with its path.
func TestE6CycleFailsWithPath(t *testing.T) {
	doc := systemDocument(t)
	st, id := snapshotFor(t, doc, []composition.ClosureEntry{
		entry("agent/cyc-a@1.0.0", goodDigest, "agent/cyc-b@1.0.0"),
		entry("agent/cyc-b@1.0.0", goodDigest, "agent/cyc-a@1.0.0"),
	})
	result := Evaluate(context.Background(), request(doc, id, LayerCapability), st)
	layer := layerOf(t, result, LayerCapability)
	if layer.Verdict != VerdictFail || !strings.Contains(layer.Reason, " -> ") {
		t.Fatalf("expected a cycle with a path, got %s / %q", layer.Verdict, layer.Reason)
	}
}

// E7 - a layer with no evidence is not_evaluated, and never pass.
func TestE7BehavioralWithoutDatasetIsNotEvaluated(t *testing.T) {
	doc := systemDocument(t)
	st, id := snapshotFor(t, doc, nil)
	result := Evaluate(context.Background(), request(doc, id, LayerStructural, LayerBehavioral), st)
	if got := layerOf(t, result, LayerBehavioral).Verdict; got != VerdictNotEvaluated {
		t.Fatalf("expected not_evaluated, got %s", got)
	}
	if result.Verdict != VerdictNotEvaluated || ExitCode(result) != 2 {
		t.Fatalf("a run with unevaluated layers must not be a pass: %s exit %d", result.Verdict, ExitCode(result))
	}
}

// E8 - an LLM judge may never decide L5.
func TestE8JudgeOnSafetyRefused(t *testing.T) {
	doc := systemDocument(t)
	st, id := snapshotFor(t, doc, nil)
	req := request(doc, id, LayerSafety)
	req.Judge = &Judge{Provider: "acme", Model: "judge-1", Version: "1.0.0"}
	result := Evaluate(context.Background(), req, st)
	if len(result.Findings) == 0 || result.Findings[0].Code != CodeJudge {
		t.Fatalf("expected E_EVAL_JUDGE, got %v", result.Findings)
	}
	if ExitCode(result) != 3 {
		t.Fatalf("expected exit 3, got %d", ExitCode(result))
	}
}

// E9 - the artifact digest excludes the clock.
func TestE9DigestExcludesClock(t *testing.T) {
	doc := systemDocument(t)
	st, id := snapshotFor(t, doc, []composition.ClosureEntry{entry("agent/lead-qualifier@1.2.0", goodDigest)})
	first := Evaluate(context.Background(), request(doc, id, LayerStructural, LayerCapability), st)
	second := Evaluate(context.Background(), request(doc, id, LayerStructural, LayerCapability), st)
	firstDigest, err := first.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	secondDigest, _ := second.Digest()
	if firstDigest != secondDigest {
		t.Fatalf("two identical runs differ: %s vs %s", firstDigest, secondDigest)
	}

	later := request(doc, id, LayerStructural, LayerCapability)
	later.Now = clock.Add(2 * time.Hour)
	third := Evaluate(context.Background(), later, st)
	thirdDigest, _ := third.Digest()
	if thirdDigest != firstDigest {
		t.Fatal("the clock must not change the artifact digest")
	}
}

// E10 - an expired injected deadline leaves the remaining layers unevaluated.
func TestE10DeadlineMarksRemainingNotEvaluated(t *testing.T) {
	doc := systemDocument(t)
	st, id := snapshotFor(t, doc, nil)
	req := request(doc, id, LayerStructural, LayerCapability)
	req.Budget = Budget{Deadline: clock.Add(-time.Minute)}
	result := Evaluate(context.Background(), req, st)
	for _, layer := range result.Layers {
		if layer.Verdict != VerdictNotEvaluated || !strings.Contains(layer.Reason, "budget") {
			t.Fatalf("expected not_evaluated/budget for %s, got %s / %q", layer.Layer, layer.Verdict, layer.Reason)
		}
	}
	if ExitCode(result) != 2 {
		t.Fatalf("expected exit 2, got %d", ExitCode(result))
	}
}

// E11 - L1 without the document is blocked, not passed.
func TestE11MissingDocumentBlocksL1(t *testing.T) {
	doc := systemDocument(t)
	st, id := snapshotFor(t, doc, nil)
	req := request(nil, id, LayerStructural)
	result := Evaluate(context.Background(), req, st)
	if got := layerOf(t, result, LayerStructural).Verdict; got != VerdictBlocked {
		t.Fatalf("expected blocked, got %s", got)
	}
	if ExitCode(result) != 2 {
		t.Fatalf("expected exit 2, got %d", ExitCode(result))
	}
}

// E12 - a floating dataset ref blocks the behavioral layer.
func TestE12UnpinnedDatasetBlocks(t *testing.T) {
	doc := systemDocument(t)
	st, id := snapshotFor(t, doc, nil)
	req := request(doc, id, LayerBehavioral)
	req.Datasets = []string{"eval/lead-set@latest"}
	result := Evaluate(context.Background(), req, st)
	layer := layerOf(t, result, LayerBehavioral)
	if layer.Verdict != VerdictBlocked || !strings.Contains(layer.Reason, "E_EVAL_DATASET") {
		t.Fatalf("expected a blocked layer naming E_EVAL_DATASET, got %s / %q", layer.Verdict, layer.Reason)
	}
}
