package composition

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"backend/pkg/ir"
)

// Mode is how the closure was resolved. The mode is recorded on the lock because
// it qualifies what the lock proves (docs/architecture/composition-resolver.md §3).
type Mode string

const (
	ModeRegistry     Mode = "registry"
	ModeBundled      Mode = "bundled"
	ModeOfflineCache Mode = "offline-cache"
)

// Code is a failure code from the lock contract's taxonomy.
type Code string

const (
	CodeIR                  Code = "E_IR"
	CodeMode                Code = "E_MODE"
	CodeGrammar             Code = "E_GRAMMAR"
	CodeRefMismatch         Code = "E_REF_MISMATCH"
	CodeAssetUnsigned       Code = "E_ASSET_UNSIGNED"
	CodeAssetInvalid        Code = "E_ASSET_INVALID"
	CodeSecretInAsset       Code = "E_SECRET_IN_ASSET"
	CodeMissing             Code = "E_MISSING"
	CodeSource              Code = "E_SOURCE"
	CodeConflictDigest      Code = "E_CONFLICT_DIGEST"
	CodeIncompatibleVersion Code = "E_INCOMPATIBLE_VERSION"
	CodeCycle               Code = "E_CYCLE"

	// Admission-side codes (docs/architecture/run-snapshot.md §10). They are
	// returned by Verify/AdmitRun rather than Resolve, and sit last in the
	// precedence list because they describe a stored snapshot, not a resolution.
	CodeDigest          Code = "E_DIGEST"
	CodeSnapshotMissing Code = "E_SNAPSHOT_MISSING"
	CodeTenant          Code = "E_TENANT"
)

// precedence orders findings for stable reporting: the first code that can make
// every later statement meaningless is reported first (§6 of the design).
var precedence = []Code{
	CodeIR, CodeMode, CodeGrammar, CodeRefMismatch, CodeAssetUnsigned,
	CodeAssetInvalid, CodeSecretInAsset, CodeMissing, CodeSource,
	CodeConflictDigest, CodeIncompatibleVersion, CodeCycle,
	CodeSnapshotMissing, CodeDigest, CodeTenant,
}

// Finding is one refusal. A non-empty list means no lock was produced.
type Finding struct {
	Code    Code
	Ref     string
	Message string
}

func (f Finding) String() string {
	if f.Ref == "" {
		return fmt.Sprintf("%s: %s", f.Code, f.Message)
	}
	return fmt.Sprintf("%s [%s]: %s", f.Code, f.Ref, f.Message)
}

// Request is everything the resolver is allowed to look at.
type Request struct {
	Manifest        []byte
	Mode            Mode
	Source          AssetSource
	CompilerVersion string
	// Now is injected: the resolver never calls time.Now(), so a re-resolution
	// with the same inputs is byte-identical (determinism obligation 4).
	Now time.Time
}

// Result is a lock or the reasons there is none. Never both.
type Result struct {
	Lock     *Lock
	Findings []Finding
}

// Resolve turns a validated System manifest plus an asset source into a lock.
func Resolve(ctx context.Context, req Request) Result {
	var findings []Finding
	fail := func(code Code, ref, format string, args ...any) {
		findings = append(findings, Finding{Code: code, Ref: ref, Message: fmt.Sprintf(format, args...)})
	}

	// 1. ADMIT the manifest. Nothing is fetched if it is not a valid System.
	sys, problems := ir.Load(req.Manifest)
	if len(problems) > 0 {
		for _, problem := range problems {
			fail(CodeIR, "", "%s", problem)
		}
		return Result{Findings: sortFindings(findings)}
	}
	canonicalSystem, err := ir.Canonicalize(req.Manifest)
	if err != nil {
		fail(CodeIR, "", "manifest is not canonically serializable: %v", err)
		return Result{Findings: sortFindings(findings)}
	}

	// 2. MODE.
	if !validMode(req.Mode) {
		fail(CodeMode, "", "resolution mode %q is not one of %s|%s|%s", req.Mode, ModeRegistry, ModeBundled, ModeOfflineCache)
		return Result{Findings: sortFindings(findings)}
	}
	if req.Source == nil {
		fail(CodeSource, "", "no asset source was provided for mode %q", req.Mode)
		return Result{Findings: sortFindings(findings)}
	}
	if strings.TrimSpace(req.CompilerVersion) == "" {
		fail(CodeMode, "", "compilerVersion is required: a lock without it cannot explain a behaviour change")
		return Result{Findings: sortFindings(findings)}
	}

	systemRef := SystemRef(sys.Metadata.Name, sys.Metadata.Version)

	// 3. COLLECT direct references in a fixed order.
	direct := collectDirectRefs(sys)

	// 4. WALK the closure with a sorted worklist: fetch order must be
	// reproducible, not dependent on map iteration.
	parents := make(map[string]map[string]bool, len(direct))
	assetByRef := map[string]Asset{}
	digestByRef := map[string]string{}
	identityVersion := map[string]string{}
	for _, ref := range direct {
		addParent(parents, ref, systemRef)
	}
	worklist := sortedCopy(direct)

	for len(worklist) > 0 {
		ref := worklist[0]
		worklist = worklist[1:]
		if _, done := assetByRef[ref]; done {
			continue
		}

		parsed, err := ParseRef(ref)
		if err != nil {
			fail(CodeGrammar, ref, "%v", err)
			continue
		}

		asset, err := req.Source.Fetch(ctx, ref)
		if err != nil {
			if errors.Is(err, ErrAssetNotFound) {
				fail(CodeMissing, ref, "no asset exists at the pinned version")
			} else {
				fail(CodeSource, ref, "source could not answer: %v", err)
			}
			continue
		}
		if asset.Ref != ref {
			fail(CodeRefMismatch, ref, "source returned %q for a request for %q", asset.Ref, ref)
			continue
		}
		if strings.TrimSpace(asset.Publisher) == "" {
			fail(CodeAssetUnsigned, ref, "asset carries no metadata.publisher; provenance is copied, never invented")
			continue
		}
		if key, bad := ScanForSecret(asset.Bytes); bad {
			fail(CodeSecretInAsset, ref, "field %q carries a literal credential; use a vault: reference", key)
			continue
		}
		canonicalAsset, err := ir.Canonicalize(asset.Bytes)
		if err != nil {
			fail(CodeAssetInvalid, ref, "asset bytes are not canonicalizable JSON: %v", err)
			continue
		}

		digest := DigestBytes(canonicalAsset)
		if previous, seen := digestByRef[ref]; seen && previous != digest {
			fail(CodeConflictDigest, ref, "source returned different content for the same reference (%s vs %s)", previous, digest)
			continue
		}
		if version, seen := identityVersion[parsed.Identity()]; seen && version != parsed.Version {
			fail(CodeIncompatibleVersion, ref, "%s is already pinned at %s in this closure; v1 allows one version per identity", parsed.Identity(), version)
			continue
		}

		identityVersion[parsed.Identity()] = parsed.Version
		digestByRef[ref] = digest
		assetByRef[ref] = asset

		for _, child := range asset.Refs {
			addParent(parents, child, ref)
			worklist = insertSorted(worklist, child)
		}
	}

	// 5. GRAPH: cycles are refused; a shared dependency (diamond) is not a cycle.
	edges := make(map[string][]string, len(assetByRef))
	for ref, asset := range assetByRef {
		var children []string
		for _, child := range asset.Refs {
			if _, resolved := assetByRef[child]; resolved {
				children = append(children, child)
			}
		}
		edges[ref] = sortedCopy(children)
	}
	if path := findCycle(edges); path != "" {
		fail(CodeCycle, "", "closure contains a cycle; dependencies must form a DAG (via %s)", path)
	}

	if len(findings) > 0 {
		return Result{Findings: sortFindings(findings)}
	}

	// 6. EMIT. Sorted closure, sorted parents, derived subsets — deterministic by construction.
	refs := make([]string, 0, len(assetByRef))
	for ref := range assetByRef {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	closure := make([]ClosureEntry, 0, len(refs))
	modelPolicyRefs := []string{}
	evaluationRefs := []string{}
	for _, ref := range refs {
		asset := assetByRef[ref]
		closure = append(closure, ClosureEntry{
			Ref:         ref,
			Kind:        asset.Kind,
			Publisher:   asset.Publisher,
			Digest:      digestByRef[ref],
			RequestedBy: sortedKeys(parents[ref]),
		})
		switch asset.Kind {
		case KindModelPolicy:
			modelPolicyRefs = append(modelPolicyRefs, ref)
		case KindEval:
			evaluationRefs = append(evaluationRefs, ref)
		}
	}

	lock := &Lock{
		LockVersion:     LockVersion,
		SystemRef:       systemRef,
		SystemDigest:    DigestBytes(canonicalSystem),
		CompilerVersion: req.CompilerVersion,
		ResolutionMode:  string(req.Mode),
		ResolvedClosure: closure,
		ModelPolicyRefs: modelPolicyRefs,
		EvaluationRefs:  evaluationRefs,
		GeneratedAt:     req.Now.UTC().Format(time.RFC3339),
	}
	return Result{Lock: lock}
}

// collectDirectRefs lists the manifest's own references in the fixed order the
// design names: agents (keys sorted), workflow capabilities (document order),
// then the evaluation dataset. Topology order is deliberately not used.
func collectDirectRefs(sys *ir.System) []string {
	var refs []string
	agentKeys := make([]string, 0, len(sys.Spec.Agents))
	for name := range sys.Spec.Agents {
		agentKeys = append(agentKeys, name)
	}
	sort.Strings(agentKeys)
	for _, name := range agentKeys {
		refs = append(refs, sys.Spec.Agents[name].Ref)
	}
	if sys.Spec.Workflow != nil {
		for _, node := range sys.Spec.Workflow.Nodes {
			if node.Capability != "" {
				refs = append(refs, node.Capability)
			}
		}
	}
	if sys.Spec.Evaluation != nil && sys.Spec.Evaluation.Dataset != "" {
		refs = append(refs, sys.Spec.Evaluation.Dataset)
	}
	return refs
}

// findCycle returns a readable path or "" when the graph is acyclic. Same
// colouring as ir.findCycle and scripts/validate-system-ir.mjs.
func findCycle(edges map[string][]string) string {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := make(map[string]int, len(edges))
	var stack []string
	var found string

	var visit func(id string) bool
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

// sortFindings orders findings by precedence, then by reference, so the same
// broken input always reports the same thing first.
func sortFindings(findings []Finding) []Finding {
	rank := make(map[Code]int, len(precedence))
	for i, code := range precedence {
		rank[code] = i
	}
	out := append([]Finding{}, findings...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, okI := rank[out[i].Code]
		rj, okJ := rank[out[j].Code]
		if !okI {
			ri = len(rank)
		}
		if !okJ {
			rj = len(rank)
		}
		if ri != rj {
			return ri < rj
		}
		if out[i].Ref != out[j].Ref {
			return out[i].Ref < out[j].Ref
		}
		return out[i].Message < out[j].Message
	})
	return out
}

func validMode(mode Mode) bool {
	switch mode {
	case ModeRegistry, ModeBundled, ModeOfflineCache:
		return true
	default:
		return false
	}
}

func addParent(parents map[string]map[string]bool, ref, parent string) {
	set, ok := parents[ref]
	if !ok {
		set = map[string]bool{}
		parents[ref] = set
	}
	set[parent] = true
}

func insertSorted(values []string, value string) []string {
	if len(values) == 0 {
		return []string{value}
	}
	i := sort.SearchStrings(values, value)
	if i < len(values) && values[i] == value {
		return values
	}
	values = append(values, "")
	copy(values[i+1:], values[i:])
	values[i] = value
	return values
}

func sortedKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return []string{}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
