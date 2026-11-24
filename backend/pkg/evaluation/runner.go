package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"backend/pkg/composition"
	"backend/pkg/ir"
)

// Evaluate runs the requested layers against one stored snapshot.
//
// It refuses the run (Findings, exit 3) when the subject cannot be verified, and
// otherwise reports a verdict per requested layer. A layer with no evidence is
// `not_evaluated`, never `pass` - evaluation-contract.md §3.
func Evaluate(ctx context.Context, req Request, store composition.SnapshotStore) Result {
	result := Result{
		EvaluatorVersion: req.EvaluatorVersion,
		SnapshotID:       req.SnapshotID,
		Budget:           req.Budget,
		Judge:            req.Judge,
		Datasets:         sortedCopy(req.Datasets),
		GeneratedAt:      formatClock(req.Now),
	}
	refuse := func(code Code, format string, args ...any) Result {
		result.Findings = append(result.Findings, Finding{Code: code, Message: fmt.Sprintf(format, args...)})
		result.Verdict = VerdictBlocked
		return result
	}

	// 1. Request contract.
	if req.Now.IsZero() {
		return refuse(CodeSubject, "now must come from the injected clock")
	}
	if strings.TrimSpace(req.EvaluatorVersion) == "" {
		return refuse(CodeSubject, "evaluatorVersion is required")
	}
	if len(req.Layers) == 0 {
		return refuse(CodeSubject, "at least one layer must be requested")
	}
	for _, layer := range req.Layers {
		if !ValidLayer(layer) {
			return refuse(CodeSubject, "unknown layer %q", layer)
		}
	}
	if req.Judge != nil {
		if req.Judge.Provider == "" || req.Judge.Model == "" || req.Judge.Version == "" {
			return refuse(CodeJudge, "a judge must declare provider, model and version")
		}
		for _, layer := range req.Layers {
			if layer == LayerSafety {
				return refuse(CodeJudge, "an LLM judge may inform L3 only; L5 must be decided by facts")
			}
		}
	}

	// 2. Subject: a stored, verified snapshot. Nothing else is evaluable.
	if store == nil {
		return refuse(CodeSubject, "no snapshot store is installed, so the subject cannot be read")
	}
	snapshot, err := store.Load(ctx, req.SnapshotID)
	if err != nil {
		if errors.Is(err, composition.ErrSnapshotMissing) {
			return refuse(CodeSubject, "snapshot %q is not stored", req.SnapshotID)
		}
		return refuse(CodeSubject, "snapshot %q could not be read: %v", req.SnapshotID, err)
	}
	if findings := composition.Verify(snapshot, req.SnapshotID); len(findings) > 0 {
		return refuse(CodeSubject, "snapshot %q fails verification: %s", req.SnapshotID, findings[0].String())
	}
	result.SystemRef = snapshot.SystemRef
	result.SystemDigest = snapshot.SystemDigest
	// The document the caller supplied must be the one the snapshot describes:
	// that is a subject check, not a layer finding (benchmark-runner.md §2).
	if len(req.SystemDocument) > 0 {
		canonical, err := ir.Canonicalize(req.SystemDocument)
		if err != nil {
			return refuse(CodeSubject, "system document is not canonically serializable: %v", err)
		}
		if digest := composition.DigestBytes(canonical); digest != snapshot.SystemDigest {
			return refuse(CodeSubject, "system document digest %s does not match the snapshot's %s", digest, snapshot.SystemDigest)
		}
	}
	lock, err := snapshot.Lock()
	if err != nil {
		return refuse(CodeSubject, "snapshot %q has no readable lock: %v", req.SnapshotID, err)
	}

	// 3. Layers, in the closed reporting order.
	requested := map[Layer]bool{}
	for _, layer := range req.Layers {
		requested[layer] = true
	}
	var layers []LayerResult
	for _, layer := range allLayers {
		if !requested[layer] {
			continue
		}
		if !req.Budget.Deadline.IsZero() && req.Now.After(req.Budget.Deadline) {
			layers = append(layers, LayerResult{Layer: layer, Verdict: VerdictNotEvaluated, Reason: "budget: the injected deadline passed before this layer ran"})
			continue
		}
		layers = append(layers, evaluateLayer(layer, req, snapshot, lock))
	}
	result.Layers = layers
	result.Verdict = aggregate(layers)
	return result
}

func evaluateLayer(layer Layer, req Request, snapshot *composition.Snapshot, lock *composition.Lock) LayerResult {
	switch layer {
	case LayerStructural:
		return evaluateStructural(req)
	case LayerCapability:
		return evaluateCapability(lock)
	case LayerBehavioral:
		if len(req.Datasets) == 0 {
			return LayerResult{Layer: layer, Verdict: VerdictNotEvaluated, Reason: "no dataset supplied, so behavior cannot be measured"}
		}
		for _, dataset := range req.Datasets {
			if _, err := composition.ParseRef(dataset); err != nil {
				return LayerResult{Layer: layer, Verdict: VerdictBlocked, Reason: fmt.Sprintf("E_EVAL_DATASET: %q is not a pinned eval ref", dataset)}
			}
		}
		return LayerResult{Layer: layer, Verdict: VerdictNotEvaluated, Reason: "the runner does not execute cases yet: no run path accepts a snapshot"}
	case LayerOperational:
		return LayerResult{Layer: layer, Verdict: VerdictNotEvaluated, Reason: "no provider selected: conformance needs a target provider"}
	case LayerSafety:
		return LayerResult{Layer: layer, Verdict: VerdictNotEvaluated, Reason: "no asset source supplied, so the safety checks cannot read manifests"}
	default:
		return LayerResult{Layer: layer, Verdict: VerdictBlocked, Reason: "unknown layer"}
	}
}

// evaluateStructural re-runs the IR reader on the supplied System document and
// compares its canonical digest with the one the snapshot recorded.
func evaluateStructural(req Request) LayerResult {
	layer := LayerStructural
	if len(req.SystemDocument) == 0 {
		return LayerResult{Layer: layer, Verdict: VerdictBlocked, Reason: "no system document supplied: L1 needs the bytes, the snapshot only stores their digest"}
	}
	if _, problems := ir.Load(req.SystemDocument); len(problems) > 0 {
		return LayerResult{Layer: layer, Verdict: VerdictFail, Reason: "invalid IR: " + strings.Join(problems, "; ")}
	}
	// The digest match is already enforced as a subject check; L1 judges validity.
	return LayerResult{Layer: layer, Verdict: VerdictPass, Metrics: []Metric{{
		Name: "invariant_violations", Direction: "lower-better", Value: 0, Threshold: 0, Passed: true,
	}}}
}

// evaluateCapability verifies the stored closure rather than re-resolving it:
// there is no registry, and verification still catches tampering, floating refs,
// incompatible versions and cycles in the artifact a run points at.
func evaluateCapability(lock *composition.Lock) LayerResult {
	layer := LayerCapability
	var problems []string
	versions := map[string]string{}
	edges := map[string][]string{}
	known := map[string]bool{}
	for _, entry := range lock.ResolvedClosure {
		known[entry.Ref] = true
	}
	for _, entry := range lock.ResolvedClosure {
		parsed, err := composition.ParseRef(entry.Ref)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s is not an exact pinned ref: %v", entry.Ref, err))
			continue
		}
		if !strings.HasPrefix(entry.Digest, composition.DigestPrefix) {
			problems = append(problems, fmt.Sprintf("%s carries no digest algorithm prefix", entry.Ref))
		}
		identity := parsed.Identity()
		if existing, seen := versions[identity]; seen && existing != parsed.Version {
			problems = append(problems, fmt.Sprintf("%s appears at both %s and %s", identity, existing, parsed.Version))
		}
		versions[identity] = parsed.Version
		for _, parent := range entry.RequestedBy {
			if known[parent] {
				edges[parent] = append(edges[parent], entry.Ref)
			}
		}
	}
	if path := findCycle(edges); path != "" {
		problems = append(problems, "closure contains a cycle: "+path)
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return LayerResult{Layer: layer, Verdict: VerdictFail, Reason: strings.Join(problems, "; ")}
	}
	return LayerResult{Layer: layer, Verdict: VerdictPass, Metrics: []Metric{{
		Name: "closure_defects", Direction: "lower-better", Value: 0, Threshold: 0, Passed: true,
	}}}
}

// findCycle uses the same colouring as backend/pkg/ir and the docs gate.
func findCycle(edges map[string][]string) string {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := map[string]int{}
	var stack []string
	var found string
	var visit func(string) bool
	visit = func(id string) bool {
		color[id] = grey
		stack = append(stack, id)
		for _, next := range edges[id] {
			switch color[next] {
			case grey:
				path := append([]string{}, stack...)
				for i, node := range path {
					if node == next {
						path = append(path[i:], next)
						break
					}
				}
				found = strings.Join(path, " -> ")
				return true
			case white:
				if visit(next) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = black
		return false
	}
	ids := make([]string, 0, len(edges))
	for id := range edges {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if color[id] == white && visit(id) {
			return found
		}
	}
	return ""
}

// aggregate: any fail wins, then blocked, then not_evaluated, then pass.
func aggregate(layers []LayerResult) Verdict {
	verdict := VerdictPass
	for _, layer := range layers {
		switch layer.Verdict {
		case VerdictFail:
			return VerdictFail
		case VerdictBlocked:
			verdict = VerdictBlocked
		case VerdictNotEvaluated:
			if verdict == VerdictPass {
				verdict = VerdictNotEvaluated
			}
		}
	}
	return verdict
}

// ExitCode maps the result to the four CI-meaningful codes of benchmark-runner.md §7.
func ExitCode(result Result) int {
	if len(result.Findings) > 0 {
		return 3
	}
	switch result.Verdict {
	case VerdictPass:
		return 0
	case VerdictFail:
		return 1
	default:
		return 2
	}
}

// Canonical renders the artifact with the repository canonical rules.
func (r *Result) Canonical() ([]byte, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return ir.Canonicalize(raw)
}

// Digest is the artifact's content address with the clock excluded, so two runs
// of the same request compare equal.
func (r *Result) Digest() (string, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", err
	}
	delete(fields, "generatedAt")
	stripped, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	canonical, err := ir.Canonicalize(stripped)
	if err != nil {
		return "", err
	}
	return composition.DigestBytes(canonical), nil
}

func formatClock(now time.Time) string {
	if now.IsZero() {
		return ""
	}
	return now.UTC().Format(time.RFC3339)
}

func sortedCopy(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := append([]string{}, values...)
	sort.Strings(out)
	return out
}
