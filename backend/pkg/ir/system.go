// Package ir is the Go consumer of the Canonical System IR v1 (P1-AGT-002).
//
// IR v1 *is* the `kind: System` manifest `spec` (docs/architecture/system-ir.md):
// one representation, one version counter, no parallel format. This package is
// the compiler-side reader the docs gate already describes:
//
//   - Load decodes a System document into typed structs and rejects anything the
//     contract forbids (unknown fields, wrong apiVersion/kind, missing identity,
//     and the IR invariants I2–I6).
//   - Canonicalize re-serializes a document byte-for-byte the way the repository
//     stores it (sorted keys, 2-space indent, one trailing newline), so a diff is
//     semantic and a round-trip is stable.
//
// Parity with the gate: scripts/validate-system-ir.mjs implements the same
// invariants over the same fixtures. The two must agree — when one changes, the
// other changes in the same commit, or the platform has two answers to "is this
// IR valid?".
//
// Scope: a *reader and validator*, not a compiler. Capability resolution against
// a live registry, policy validation and deployment planning are P7.3/P7.4.
// JSON-Schema validation stays the docs gate's job; here a typed decode with
// DisallowUnknownFields gives the `additionalProperties: false` guarantee for the
// fields the compiler consumes.
package ir

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

// APIVersion is the only envelope version IR v1 accepts.
const APIVersion = "vibesdk/v1"

// Kind is the only document kind this package treats as IR.
const Kind = "System"

// NodeKinds mirrors WORKFLOW_NODE_TYPES in worker/workflow/VibeWorkflow.ts — the
// seven procedural node kinds IR v1 can express.
var NodeKinds = []string{"trigger", "http", "db", "ai", "email", "condition", "sleep"}

// Limits and vocabularies mirror docs/architecture/manifest-schemas/system.schema.json.
// They are exported so a caller can explain a refusal without repeating the numbers.
const (
	// MaxSlugLength is the schema's maxLength for metadata.name and workflow node ids.
	MaxSlugLength = 63
	// MaxOutcomeLength is the schema's maxLength for spec.outcome.
	MaxOutcomeLength = 1000
	// MaxDescriptionLength, MaxPublisherLength, MaxLicenseLength and MaxLabelLength
	// mirror the metadata caps in the same schema.
	MaxDescriptionLength = 500
	MaxPublisherLength   = 120
	MaxLicenseLength     = 80
	MaxLabelLength       = 200
	// MaxConditionLength is the schema's maxLength for topology/workflow conditions.
	MaxConditionLength = 200
	// MaxReplicas is the schema's maximum for spec.deployment.replicas.
	MaxReplicas = 100
)

// ApprovalClasses is the schema's approvals[].on enum. An approval for a class
// outside this list would never bind, so it is refused instead of ignored.
var ApprovalClasses = []string{"external-message", "external-write", "payment", "human-review"}

// RoutingOnError is the schema's spec.routing.onError enum.
var RoutingOnError = []string{"fail-run", "escalate-human", "skip-step"}

// DeploymentEnvironments is the schema's spec.deployment.environment enum.
var DeploymentEnvironments = []string{"development", "staging", "production"}

// SemverPattern is the schema's exact-version grammar: no leading zeros, no
// ranges, no `latest`. Go and the JSON Schema must agree on it (ADI-FOUND-001).
const SemverPattern = `(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)`

var (
	slugRe     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	agentRefRe = regexp.MustCompile(`^agent/[a-z0-9]([-a-z0-9]*[a-z0-9])?@` + SemverPattern + `$`)
	anyRefRe   = regexp.MustCompile(`^(agent|skill|tool|model-policy|knowledge|eval)/[a-z0-9]([-a-z0-9]*[a-z0-9])?@` + SemverPattern + `$`)
	semverRe   = regexp.MustCompile(`^` + SemverPattern + `$`)
	approverRe = regexp.MustCompile(`^(role|user|group):[A-Za-z0-9_.-]+$`)
)

// System is one IR v1 document.
type System struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
}

// Metadata is the envelope identity (definition + version identity).
type Metadata struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Publisher   string            `json:"publisher"`
	License     string            `json:"license"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// Spec is the IR payload: exactly one execution shape (agents+topology XOR
// workflow) plus desired-state policy.
type Spec struct {
	Outcome    string           `json:"outcome,omitempty"`
	Agents     map[string]Agent `json:"agents,omitempty"`
	Topology   []Step           `json:"topology,omitempty"`
	Workflow   *Workflow        `json:"workflow,omitempty"`
	Routing    *Routing         `json:"routing,omitempty"`
	Deployment *Deployment      `json:"deployment,omitempty"`
	Evaluation *Evaluation      `json:"evaluation,omitempty"`
	Approvals  []Approval       `json:"approvals,omitempty"`
}

// Agent is one slot in an agentic system: a pinned Agent ref plus delegation.
type Agent struct {
	Ref         string   `json:"ref"`
	DelegatesTo []string `json:"delegatesTo,omitempty"`
}

// Step is one agent's position in the run order.
type Step struct {
	Run   string `json:"run"`
	After string `json:"after,omitempty"`
	On    string `json:"on,omitempty"`
	When  string `json:"when,omitempty"`
}

// Workflow is the procedural shape.
type Workflow struct {
	Entry []string `json:"entry,omitempty"`
	Nodes []Node   `json:"nodes"`
}

// Node is one procedural step.
type Node struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Next       []string `json:"next,omitempty"`
	Capability string   `json:"capability,omitempty"`
	// When is the edge condition for this step (e.g. "score >= 80").
	When string `json:"when,omitempty"`
}

// Routing is the run-level error and parallelism policy.
type Routing struct {
	OnError  string `json:"onError,omitempty"`
	Parallel *bool  `json:"parallel,omitempty"`
}

// Deployment is desired deployment state (never runtime state).
type Deployment struct {
	Environment string `json:"environment,omitempty"`
	Replicas    *int   `json:"replicas,omitempty"`
}

// Evaluation is the scoring contract.
type Evaluation struct {
	Dataset    string             `json:"dataset"`
	Thresholds map[string]float64 `json:"thresholds,omitempty"`
}

// Approval gates one class of side effect behind a typed approver.
type Approval struct {
	On       string `json:"on"`
	Approver string `json:"approver"`
}

// runtimeOnlyKeys are the field names IR v1 refuses anywhere in the document:
// the IR is desired state, runs live in the data layer (invariant I2).
var runtimeOnlyKeys = map[string]bool{
	"status": true, "state": true, "health": true, "lastRun": true,
	"runId": true, "metrics": true, "telemetry": true, "logs": true,
}

// Load decodes and validates one IR document. It returns the typed system plus
// every violation found (nil means valid). A document with any violation is not
// usable: callers must refuse it rather than compile a partial system.
func Load(data []byte) (*System, []string) {
	var problems []string
	fail := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	// Field-level: unknown fields are rejected, which is how the schema's
	// `additionalProperties: false` reaches the compiler. This is also what
	// rejects a stray `status`/`telemetry` at a known position.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var sys System
	if err := dec.Decode(&sys); err != nil {
		fail("not a valid System document: %v", err)
		return nil, problems
	}
	if dec.More() {
		fail("trailing content after the System document")
	}
	if key, dup := firstDuplicateKey(data); dup {
		fail("duplicate JSON key %q (a shadowed key is not a canonical document)", key)
	}

	// Envelope.
	if sys.APIVersion != APIVersion {
		fail("apiVersion must be %q (got %q)", APIVersion, sys.APIVersion)
	}
	if sys.Kind != Kind {
		fail("kind must be %q (got %q)", Kind, sys.Kind)
	}
	if problem := slugProblem("metadata.name", sys.Metadata.Name); problem != "" {
		fail("%s", problem)
	}
	if !semverRe.MatchString(sys.Metadata.Version) {
		fail("metadata.version %q must be exact semver (%s)", sys.Metadata.Version, SemverPattern)
	}
	if strings.TrimSpace(sys.Metadata.Publisher) == "" {
		fail("metadata.publisher is required")
	} else if len(sys.Metadata.Publisher) > MaxPublisherLength {
		fail("metadata.publisher exceeds %d characters", MaxPublisherLength)
	}
	if strings.TrimSpace(sys.Metadata.License) == "" {
		fail("metadata.license is required")
	} else if len(sys.Metadata.License) > MaxLicenseLength {
		fail("metadata.license exceeds %d characters", MaxLicenseLength)
	}
	if len(sys.Metadata.Description) > MaxDescriptionLength {
		fail("metadata.description exceeds %d characters", MaxDescriptionLength)
	}
	for _, key := range sortedKeys(sys.Metadata.Labels) {
		if len(sys.Metadata.Labels[key]) > MaxLabelLength {
			fail("metadata.labels[%q] exceeds %d characters", key, MaxLabelLength)
		}
	}

	// I2 — desired state only. The typed decode already rejects the known
	// runtime keys as unknown fields; this walk also catches them where the
	// document carries free-form values (labels, thresholds).
	var walk func(any, string)
	walk = func(node any, at string) {
		switch v := node.(type) {
		case map[string]any:
			for key, value := range v {
				if runtimeOnlyKeys[key] {
					fail("%s.%s carries runtime state %q (the IR is desired state; runs live in the data layer)", at, key, key)
				}
				walk(value, at+"."+key)
			}
		case []any:
			for i, item := range v {
				walk(item, fmt.Sprintf("%s[%d]", at, i))
			}
		}
	}
	var generic any
	if err := json.Unmarshal(data, &generic); err == nil {
		walk(generic, "$")
	}

	problems = append(problems, checkInvariants(&sys)...)
	return &sys, problems
}

// checkInvariants implements I1 and I3-I6 (I2 is the walk in Load).
func checkInvariants(sys *System) []string {
	var problems []string
	fail := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	spec := sys.Spec
	hasAgents := len(spec.Agents) > 0
	hasWorkflow := spec.Workflow != nil

	// I1 - exactly one execution shape.
	if hasAgents && hasWorkflow {
		fail("declares both 'agents' and 'workflow' (IR v1 allows exactly one execution shape)")
	}
	if !hasAgents && !hasWorkflow {
		fail("declares neither 'agents' nor 'workflow' (IR v1 needs one execution shape)")
	}
	// The schema's oneOf requires outcome in both branches: a system with no
	// stated outcome cannot be evaluated or attributed (ADI-FOUND-001 P-A3).
	if strings.TrimSpace(spec.Outcome) == "" {
		fail("spec.outcome is required (outcome-first: a system must state the outcome it delivers)")
	} else if len(spec.Outcome) > MaxOutcomeLength {
		fail("spec.outcome exceeds %d characters", MaxOutcomeLength)
	}
	// The schema requires topology with agents (branch 1 of oneOf). Without it the
	// runtime would invent a run order - a semantic decision outside the contract.
	if hasAgents && len(spec.Topology) == 0 {
		fail("spec.agents declared without spec.topology (IR v1 requires an explicit run order)")
	}

	// I3 - agent slots are pinned Agent refs; delegation targets exist.
	names := make([]string, 0, len(spec.Agents))
	for name := range spec.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		agent := spec.Agents[name]
		if problem := slugProblem(fmt.Sprintf("agent key %q", name), name); problem != "" {
			fail("%s", problem)
		}
		if !agentRefRe.MatchString(agent.Ref) {
			fail("agent %q ref %q must be agent/name@MAJOR.MINOR.PATCH", name, agent.Ref)
		}
		seenTargets := map[string]bool{}
		for _, target := range agent.DelegatesTo {
			if seenTargets[target] {
				fail("agent %q delegates to %q more than once", name, target)
			}
			seenTargets[target] = true
			if _, ok := spec.Agents[target]; !ok {
				fail("agent %q delegates to unknown agent %q", name, target)
			}
			if target == name {
				fail("agent %q delegates to itself", name)
			}
		}
	}

	// I4 - topology references declared agents, one step per agent.
	seen := map[string]bool{}
	for i, step := range spec.Topology {
		if _, ok := spec.Agents[step.Run]; !ok {
			fail("topology[%d] runs unknown agent %q", i, step.Run)
		}
		if seen[step.Run] {
			fail("topology[%d] runs %q twice (v1: one step per agent)", i, step.Run)
		}
		seen[step.Run] = true
		if step.After != "" {
			if _, ok := spec.Agents[step.After]; !ok {
				fail("topology[%d] after unknown agent %q", i, step.After)
			}
		}
	}

	// I5 - workflow graph integrity: unique ids, known kinds, resolvable edges,
	// no duplicate edges, no cycles, every node reachable from the entry set.
	if spec.Workflow != nil {
		ids := map[string]bool{}
		for i, node := range spec.Workflow.Nodes {
			if ids[node.ID] {
				fail("workflow node id %q duplicated (index %d)", node.ID, i)
			}
			ids[node.ID] = true
			if problem := slugProblem(fmt.Sprintf("workflow node id %q", node.ID), node.ID); problem != "" {
				fail("%s", problem)
			}
			if !isNodeKind(node.Kind) {
				fail("workflow node %q has unknown kind %q", node.ID, node.Kind)
			}
			if node.Capability != "" && !anyRefRe.MatchString(node.Capability) {
				fail("workflow node %q capability %q must be a pinned ref", node.ID, node.Capability)
			}
			if len(node.When) > MaxConditionLength {
				fail("workflow node %q when exceeds %d characters", node.ID, MaxConditionLength)
			}
		}
		edges := map[string]map[string]bool{}
		for _, node := range spec.Workflow.Nodes {
			edges[node.ID] = map[string]bool{}
			for _, next := range node.Next {
				if edges[node.ID][next] {
					fail("workflow node %q lists %q more than once in next", node.ID, next)
				}
				edges[node.ID][next] = true
				if !ids[next] {
					fail("workflow node %q points to unknown node %q", node.ID, next)
				}
			}
		}
		for _, entry := range spec.Workflow.Entry {
			if !ids[entry] {
				fail("workflow entry %q does not exist", entry)
			}
		}
		// A cycle makes the DAG unexecutable. The runtime refuses it too
		// (VibeWorkflow / workflowschema.go); refusing it here means a cyclic
		// composition can never be resolved or locked in the first place.
		if path := findCycle(spec.Workflow.Nodes, edges); path != "" {
			fail("workflow contains a cycle; edges must form a DAG (via %s)", path)
		}
		// Reachability: a declared step that can never run is a modelling bug.
		// Entry defaults to the first node, exactly as the docs gate does.
		if len(spec.Workflow.Nodes) > 0 {
			roots := spec.Workflow.Entry
			if len(roots) == 0 {
				roots = []string{spec.Workflow.Nodes[0].ID}
			}
			reachable := map[string]bool{}
			queue := append([]string{}, roots...)
			for len(queue) > 0 {
				id := queue[0]
				queue = queue[1:]
				if reachable[id] {
					continue
				}
				reachable[id] = true
				for next := range edges[id] {
					queue = append(queue, next)
				}
			}
			for _, node := range spec.Workflow.Nodes {
				if !reachable[node.ID] {
					fail("workflow node %q is unreachable from the entry set", node.ID)
				}
			}
		}
	}

	// I6 - approvals use the shared class vocabulary and a typed approver.
	for i, approval := range spec.Approvals {
		if !contains(ApprovalClasses, approval.On) {
			fail("approvals[%d].on %q must be one of %s (an approval for an unknown class never binds)",
				i, approval.On, strings.Join(ApprovalClasses, "|"))
		}
		if !approverRe.MatchString(approval.Approver) {
			fail("approvals[%d].approver must be role:/user:/group:", i)
		}
	}

	// Desired deployment state stays inside the schema's enums and bounds.
	if spec.Deployment != nil {
		if spec.Deployment.Environment != "" && !contains(DeploymentEnvironments, spec.Deployment.Environment) {
			fail("deployment.environment %q must be one of %s", spec.Deployment.Environment,
				strings.Join(DeploymentEnvironments, "|"))
		}
		if spec.Deployment.Replicas != nil && (*spec.Deployment.Replicas < 0 || *spec.Deployment.Replicas > MaxReplicas) {
			fail("deployment.replicas must be between 0 and %d", MaxReplicas)
		}
	}

	// Routing policy is a closed vocabulary: an unknown onError has no defined
	// meaning at runtime, so it is refused rather than defaulted.
	if spec.Routing != nil && spec.Routing.OnError != "" && !contains(RoutingOnError, spec.Routing.OnError) {
		fail("routing.onError %q must be one of %s", spec.Routing.OnError, strings.Join(RoutingOnError, "|"))
	}

	// An evaluation block is a promise to score against a dataset. The schema
	// pins that dataset as a ref, so an ad-hoc string is not enough.
	if spec.Evaluation != nil {
		if !anyRefRe.MatchString(spec.Evaluation.Dataset) {
			fail("evaluation.dataset %q must be a pinned ref (e.g. eval/name@1.0.0)", spec.Evaluation.Dataset)
		}
		for _, key := range sortedKeys(spec.Evaluation.Thresholds) {
			value := spec.Evaluation.Thresholds[key]
			if value < 0 || value > 1 {
				fail("evaluation.thresholds[%q] must be between 0 and 1", key)
			}
		}
	}

	// Topology conditions carry the same length bound as workflow conditions.
	for i, step := range spec.Topology {
		if len(step.On) > MaxConditionLength {
			fail("topology[%d].on exceeds %d characters", i, MaxConditionLength)
		}
		if len(step.When) > MaxConditionLength {
			fail("topology[%d].when exceeds %d characters", i, MaxConditionLength)
		}
	}
	return problems
}

// slugProblem reports a slug-grammar or length violation for an identity field.
// Length is checked first so a 64+ character name is reported as a length
// problem, not as a generic pattern failure (ADI-FOUND-001 N1/N2).
func slugProblem(field, value string) string {
	if len(value) > MaxSlugLength {
		return fmt.Sprintf("%s is %d characters; the maximum is %d", field, len(value), MaxSlugLength)
	}
	if !slugRe.MatchString(value) {
		return fmt.Sprintf("%s is not a lowercase slug (^[a-z0-9]([-a-z0-9]*[a-z0-9])?$)", field)
	}
	return ""
}

// findCycle returns a readable cycle path (a -> b -> a) or "" when the graph is
// acyclic. Edges to unknown nodes are ignored here: they are reported separately.
func findCycle(nodes []Node, edges map[string]map[string]bool) string {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := make(map[string]int, len(nodes))
	var stack []string
	var found string

	var visit func(id string) bool
	visit = func(id string) bool {
		color[id] = grey
		stack = append(stack, id)
		for next := range edges[id] {
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

	for i := range nodes {
		id := nodes[i].ID
		if color[id] == white && visit(id) {
			return found
		}
	}
	return ""
}

// firstDuplicateKey reports the first object key that appears twice in the same
// JSON object. `encoding/json` silently keeps the last one, which would let a
// hand-edited document shadow `kind`, `apiVersion` or a `ref` without any tool
// noticing (ADI-FOUND-001 P-A11). Maps are the only place duplicates can hide.
func firstDuplicateKey(data []byte) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	type frame struct {
		object    bool
		expectKey bool
		keys      map[string]bool
	}
	var stack []*frame

	// completeValue marks the value finished for the enclosing container, so the
	// next string in an object is a key again.
	completeValue := func() {
		if len(stack) == 0 {
			return
		}
		if top := stack[len(stack)-1]; top.object {
			top.expectKey = true
		}
	}

	for {
		tok, err := dec.Token()
		if err != nil {
			return "", false
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{':
				stack = append(stack, &frame{object: true, expectKey: true, keys: map[string]bool{}})
			case '[':
				stack = append(stack, &frame{})
			case '}', ']':
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				completeValue()
			}
			continue
		}
		if key, ok := tok.(string); ok && len(stack) > 0 {
			top := stack[len(stack)-1]
			if top.object && top.expectKey {
				top.expectKey = false
				if top.keys[key] {
					return key, true
				}
				top.keys[key] = true
				continue
			}
		}
		completeValue()
	}
}

// sortedKeys returns the map's keys in a deterministic order so validation
// messages do not depend on Go's map iteration order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// contains reports whether a closed vocabulary includes a value.
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func isNodeKind(kind string) bool {
	for _, k := range NodeKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Canonicalize re-serializes a document the way the repository stores it:
// recursively sorted keys, 2-space indent, exactly one trailing newline. Numbers
// keep their literal form, so a round-trip cannot silently rewrite them.
func Canonicalize(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, fmt.Errorf("canonicalize: %w", err)
	}
	// One document, nothing after it. `Load` enforces this too; if Canonicalize
	// did not, two byte streams sharing a prefix would canonicalize identically
	// and a content digest could not tell them apart (ADI-FOUND-001 P-A10).
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("canonicalize: trailing content after the document")
	}
	if key, dup := firstDuplicateKey(data); dup {
		return nil, fmt.Errorf("canonicalize: duplicate JSON key %q", key)
	}
	out, err := canonicalEncode(generic)
	if err != nil {
		return nil, fmt.Errorf("canonicalize: %w", err)
	}
	// json.Encoder already appends a newline; normalize to exactly one.
	return append(bytes.TrimRight(out, "\n"), byte(10)), nil
}

// canonicalEncode is Canonicalize's writer: HTML escaping is DISABLED because the
// fixtures carry IR conditions such as "score >= 80" and a canonical form that
// rewrote them to \u003e would never round-trip against the stored bytes.
func canonicalEncode(generic any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(generic); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
