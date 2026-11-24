package plan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"backend/pkg/composition"
	"backend/pkg/execution"
	"backend/pkg/ir"
)

// CompilerVersion identifies this compiler. A behaviour change must move it, so
// a run can say which compiler produced its plan.
const CompilerVersion = "vibesdk-plan/0.1.0"

// PlanKind names which runtime the plan targets.
type PlanKind string

const (
	PlanWorkflow PlanKind = "workflow"
	PlanAgentic  PlanKind = "agentic"
)

// Code is a refusal code from the binding contract.
type Code string

const (
	CodeSnapshot   Code = "E_BIND_SNAPSHOT"
	CodeShape      Code = "E_BIND_SHAPE"
	CodeKind       Code = "E_BIND_KIND"
	CodeCapability Code = "E_BIND_CAPABILITY"
	CodePlan       Code = "E_BIND_PLAN"
)

// Finding is one refusal. A non-empty list means no plan.
type Finding struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
}

func (f Finding) String() string { return fmt.Sprintf("%s: %s", f.Code, f.Message) }

// Edge is the interpreter view of a dependency, with its condition carried
// verbatim: the compiler never evaluates a condition.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	When string `json:"when,omitempty"`
}

// Gate is one approval requirement, in the shared four-class vocabulary.
type Gate struct {
	On       string `json:"on"`
	Approver string `json:"approver"`
}

// Plan is the compiled artifact. It carries both views of one mapping:
// Steps/DependsOn for the execution provider, Edges/When for the interpreter.
type Plan struct {
	Kind            PlanKind               `json:"kind"`
	CompilerVersion string                 `json:"compilerVersion"`
	SnapshotID      string                 `json:"snapshotId"`
	SystemRef       string                 `json:"systemRef"`
	Tenant          string                 `json:"tenant"`
	Entry           []string               `json:"entry"`
	Steps           []execution.Step       `json:"steps"`
	Edges           []Edge                 `json:"edges"`
	Gates           []Gate                 `json:"gates"`
	Requirements    []execution.Capability `json:"requirements"`
	// OnError is carried verbatim from spec.routing.onError. It is deliberately
	// not translated into a gate: "escalate to a human on error" and "this class
	// needs human approval" are different mechanisms.
	OnError    string `json:"onError,omitempty"`
	PlanDigest string `json:"planDigest"`
}

// Canonical renders the plan with the repository canonical rules.
func (p *Plan) Canonical() ([]byte, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return ir.Canonicalize(raw)
}

// Digest is the plan content address: a plan has no clock, so the digest covers
// the whole document except the digest field itself.
func (p *Plan) Digest() (string, error) {
	copied := *p
	copied.PlanDigest = ""
	raw, err := json.Marshal(copied)
	if err != nil {
		return "", err
	}
	canonical, err := ir.Canonicalize(raw)
	if err != nil {
		return "", err
	}
	return composition.DigestBytes(canonical), nil
}

// Compile turns a verified snapshot plus its System document into a plan.
func Compile(snapshot *composition.Snapshot, systemDocument []byte) (*Plan, []Finding) {
	fail := func(code Code, format string, args ...any) (*Plan, []Finding) {
		return nil, []Finding{{Code: code, Message: fmt.Sprintf(format, args...)}}
	}

	// 1. The subject must be a stored, verified snapshot.
	if snapshot == nil {
		return fail(CodeSnapshot, "no snapshot supplied")
	}
	if findings := composition.Verify(snapshot, snapshot.SnapshotID); len(findings) > 0 {
		return fail(CodeSnapshot, "snapshot fails verification: %s", findings[0].String())
	}

	// 2. The document must be the snapshot document.
	if len(systemDocument) == 0 {
		return fail(CodeSnapshot, "no system document supplied; the snapshot stores only its digest")
	}
	canonicalDoc, err := ir.Canonicalize(systemDocument)
	if err != nil {
		return fail(CodeSnapshot, "system document is not canonically serializable: %v", err)
	}
	if digest := composition.DigestBytes(canonicalDoc); digest != snapshot.SystemDigest {
		return fail(CodeSnapshot, "system document digest %s does not match the snapshot digest %s", digest, snapshot.SystemDigest)
	}
	sys, problems := ir.Load(systemDocument)
	if len(problems) > 0 {
		return fail(CodeSnapshot, "the snapshot document is not a valid System: %s", strings.Join(problems, "; "))
	}

	// 3. Only the procedural shape is compilable today.
	if sys.Spec.Workflow == nil {
		return fail(CodeShape, "the System is agentic (agents+topology); no runtime node type runs an agent, so it is refused rather than lowered")
	}
	workflow := sys.Spec.Workflow
	if len(workflow.Nodes) == 0 {
		return fail(CodePlan, "the workflow declares no nodes")
	}

	closure := map[string]bool{}
	if lock, err := snapshot.Lock(); err == nil {
		for _, entry := range lock.ResolvedClosure {
			closure[entry.Ref] = true
		}
	}

	// 4. Nodes: known kinds, capabilities inside the closure, known edges.
	known := map[string]bool{}
	for _, node := range workflow.Nodes {
		known[node.ID] = true
	}
	kindSet := map[string]bool{}
	for _, kind := range ir.NodeKinds {
		kindSet[kind] = true
	}
	for _, node := range workflow.Nodes {
		if !kindSet[node.Kind] {
			return fail(CodeKind, "node %q has an unknown kind %q", node.ID, node.Kind)
		}
		if node.Capability != "" && !closure[node.Capability] {
			return fail(CodeCapability, "node %q references %q, which is not in the snapshot closure", node.ID, node.Capability)
		}
		for _, next := range node.Next {
			if !known[next] {
				return fail(CodePlan, "node %q points to unknown node %q", node.ID, next)
			}
		}
	}

	// 5. One mapping, two consumers.
	whenByID := map[string]string{}
	for _, node := range workflow.Nodes {
		whenByID[node.ID] = node.When
	}
	predecessors := map[string][]string{}
	var edges []Edge
	requirements := map[execution.Capability]bool{}
	for _, node := range workflow.Nodes {
		for _, next := range node.Next {
			predecessors[next] = append(predecessors[next], node.ID)
			edges = append(edges, Edge{From: node.ID, To: next, When: whenByID[next]})
		}
		if node.Kind == "sleep" {
			requirements[execution.CapLongSleep] = true
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})

	steps, ordered := buildSteps(workflow.Nodes, predecessors)
	if len(ordered) != len(workflow.Nodes) {
		return fail(CodePlan, "the workflow graph is cyclic; a plan must be acyclic")
	}

	gates := make([]Gate, 0, len(sys.Spec.Approvals))
	for _, approval := range sys.Spec.Approvals {
		gates = append(gates, Gate{On: approval.On, Approver: approval.Approver})
	}
	sort.Slice(gates, func(i, j int) bool {
		if gates[i].On != gates[j].On {
			return gates[i].On < gates[j].On
		}
		return gates[i].Approver < gates[j].Approver
	})

	capabilityList := make([]execution.Capability, 0, len(requirements))
	for capability := range requirements {
		capabilityList = append(capabilityList, capability)
	}
	sort.Slice(capabilityList, func(i, j int) bool { return capabilityList[i] < capabilityList[j] })

	onError := ""
	if sys.Spec.Routing != nil {
		onError = sys.Spec.Routing.OnError
	}

	compiled := &Plan{
		Kind:            PlanWorkflow,
		CompilerVersion: CompilerVersion,
		SnapshotID:      snapshot.SnapshotID,
		SystemRef:       snapshot.SystemRef,
		Tenant:          snapshot.TenantKey,
		Entry:           append([]string{}, workflow.Entry...),
		Steps:           steps,
		Edges:           edges,
		Gates:           gates,
		Requirements:    capabilityList,
		OnError:         onError,
	}
	digest, err := compiled.Digest()
	if err != nil {
		return fail(CodePlan, "plan digest: %v", err)
	}
	compiled.PlanDigest = digest
	return compiled, nil
}

// buildSteps emits steps in a deterministic topological order: Kahn algorithm
// with the smallest ready node id chosen first, so two compiles of one snapshot
// agree byte for byte.
func buildSteps(nodes []ir.Node, predecessors map[string][]string) ([]execution.Step, []string) {
	remaining := map[string][]string{}
	var ready []string
	for _, node := range nodes {
		deps := sortedCopy(predecessors[node.ID])
		remaining[node.ID] = deps
		if len(deps) == 0 {
			ready = append(ready, node.ID)
		}
	}
	sort.Strings(ready)
	var order []string
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		order = append(order, id)
		for _, node := range nodes {
			deps := remaining[node.ID]
			if len(deps) == 0 {
				continue
			}
			filtered := make([]string, 0, len(deps))
			for _, dep := range deps {
				if dep != id {
					filtered = append(filtered, dep)
				}
			}
			remaining[node.ID] = filtered
			if len(filtered) == 0 && !containsString(order, node.ID) && !containsString(ready, node.ID) {
				ready = append(ready, node.ID)
			}
		}
		sort.Strings(ready)
	}
	steps := make([]execution.Step, 0, len(order))
	for _, id := range order {
		steps = append(steps, execution.Step{Name: id, DependsOn: sortedCopy(predecessors[id])})
	}
	return steps, order
}

func sortedCopy(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := append([]string{}, values...)
	sort.Strings(out)
	return out
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
