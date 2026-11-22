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

var (
	slugRe     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	agentRefRe = regexp.MustCompile(`^agent/[a-z0-9]([-a-z0-9]*[a-z0-9])?@\d+\.\d+\.\d+$`)
	anyRefRe   = regexp.MustCompile(`^(agent|skill|tool|model-policy|knowledge|eval)/[a-z0-9]([-a-z0-9]*[a-z0-9])?@\d+\.\d+\.\d+$`)
	semverRe   = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
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

	// Envelope.
	if sys.APIVersion != APIVersion {
		fail("apiVersion must be %q (got %q)", APIVersion, sys.APIVersion)
	}
	if sys.Kind != Kind {
		fail("kind must be %q (got %q)", Kind, sys.Kind)
	}
	if !slugRe.MatchString(sys.Metadata.Name) {
		fail("metadata.name %q must be a lowercase slug of at most 63 chars", sys.Metadata.Name)
	}
	if !semverRe.MatchString(sys.Metadata.Version) {
		fail("metadata.version %q must be exact semver", sys.Metadata.Version)
	}
	if strings.TrimSpace(sys.Metadata.Publisher) == "" {
		fail("metadata.publisher is required")
	}
	if strings.TrimSpace(sys.Metadata.License) == "" {
		fail("metadata.license is required")
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

	// I3 - agent slots are pinned Agent refs; delegation targets exist.
	names := make([]string, 0, len(spec.Agents))
	for name := range spec.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		agent := spec.Agents[name]
		if !slugRe.MatchString(name) {
			fail("agent key %q is not a slug", name)
		}
		if !agentRefRe.MatchString(agent.Ref) {
			fail("agent %q ref %q must be agent/name@MAJOR.MINOR.PATCH", name, agent.Ref)
		}
		for _, target := range agent.DelegatesTo {
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

	// I5 - workflow graph integrity.
	if spec.Workflow != nil {
		ids := map[string]bool{}
		for i, node := range spec.Workflow.Nodes {
			if ids[node.ID] {
				fail("workflow node id %q duplicated (index %d)", node.ID, i)
			}
			ids[node.ID] = true
			if !isNodeKind(node.Kind) {
				fail("workflow node %q has unknown kind %q", node.ID, node.Kind)
			}
			if node.Capability != "" && !anyRefRe.MatchString(node.Capability) {
				fail("workflow node %q capability %q must be a pinned ref", node.ID, node.Capability)
			}
		}
		for _, node := range spec.Workflow.Nodes {
			for _, next := range node.Next {
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
	}

	// I6 - approvals use a typed approver.
	for i, approval := range spec.Approvals {
		if !approverRe.MatchString(approval.Approver) {
			fail("approvals[%d].approver must be role:/user:/group:", i)
		}
	}
	return problems
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
