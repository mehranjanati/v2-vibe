package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ---------- Workflow DAG v2 (executable contract) ----------
//
// workflow.json v2 describes the app's backend logic as an EXECUTABLE DAG.
// Unlike v1 (visual-only), every node maps 1:1 to a durable Cloudflare
// Workflows step at runtime (Phase 2), so the schema carries real runtime
// semantics: per-type params, retry policies, timeouts, sleep durations and
// edge conditions. The Go control plane validates this file BEFORE it is
// synced to D1; an invalid workflow never fails a generation (salvage
// pattern) — the raw file stays in the VFS so the frontend can still
// render it.

const (
	WorkflowSchemaV2 = 2

	NodeTypeTrigger   = "trigger"
	NodeTypeHTTP      = "http"
	NodeTypeDB        = "db"
	NodeTypeAI        = "ai"
	NodeTypeEmail     = "email"
	NodeTypeCondition = "condition"
	NodeTypeSleep     = "sleep"
)

// validWorkflowNodeTypes is the closed set of executable node types.
var validWorkflowNodeTypes = map[string]struct{}{
	NodeTypeTrigger:   {},
	NodeTypeHTTP:      {},
	NodeTypeDB:        {},
	NodeTypeAI:        {},
	NodeTypeEmail:     {},
	NodeTypeCondition: {},
	NodeTypeSleep:     {},
}

// validHTTPMethods is the closed set of http.method values.
var validHTTPMethods = map[string]struct{}{
	"GET": {}, "POST": {}, "PUT": {}, "PATCH": {}, "DELETE": {},
}

// validDBOps is the closed set of db.op values.
var validDBOps = map[string]struct{}{
	"insert": {}, "upsert": {}, "select": {}, "update": {}, "delete": {},
}

// validConditionOps is the closed set for nodeParams.condOp and
// edge.condition.op.
var validConditionOps = map[string]struct{}{
	"eq": {}, "neq": {}, "gt": {}, "gte": {}, "lt": {}, "lte": {},
	"contains": {}, "truthy": {},
}

// validBackoffModes mirrors the Cloudflare Workflows retry.backoff values.
var validBackoffModes = map[string]struct{}{
	"constant": {}, "linear": {}, "exponential": {},
}

// workflowDurationUnits are the accepted duration-string suffixes
// (Workflows-compatible). The generation prompt documents exactly these.
var workflowDurationUnits = []string{
	"seconds", "second", "minutes", "minute", "hours", "hour",
	"days", "day", "weeks", "week",
}

// WorkflowV2Position is the visual node position (layout only; ignored by
// the runtime).
type WorkflowV2Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// WorkflowV2Retry is the per-node retry policy, translated 1:1 to
// step.do()'s retries option at runtime.
type WorkflowV2Retry struct {
	Limit   int    `json:"limit,omitempty"`
	Delay   string `json:"delay,omitempty"`
	Backoff string `json:"backoff,omitempty"`
}

// WorkflowV2Params is the option bag attached to a node. The executable
// subset depends on node.type; Validate enforces the per-type required
// fields documented in the generation prompt. Body is `any` on purpose: an
// HTTP request body is a JSON object while an email body is a string.
type WorkflowV2Params struct {
	// http
	URL     string            `json:"url,omitempty"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    any               `json:"body,omitempty"`

	// db
	Table string         `json:"table,omitempty"`
	Op    string         `json:"op,omitempty"`
	Where map[string]any `json:"where,omitempty"`
	Data  map[string]any `json:"data,omitempty"`

	// ai
	Model  string `json:"model,omitempty"`
	Prompt string `json:"prompt,omitempty"`
	System string `json:"system,omitempty"`

	// email
	To      string `json:"to,omitempty"`
	Subject string `json:"subject,omitempty"`

	// condition (node-level branch; edge conditions use WorkflowV2Condition)
	CondOp string `json:"condOp,omitempty"`
	LHS    string `json:"lhs,omitempty"`
	RHS    string `json:"rhs,omitempty"`

	// sleep
	Duration string `json:"duration,omitempty"`
}

// WorkflowV2Node is a single DAG node. id/retry/timeout map to the
// step.do() call shape at runtime (step name = node id). Params is a
// pointer so an absent or null "params" decodes safely.
type WorkflowV2Node struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Label    string              `json:"label,omitempty"`
	Params   *WorkflowV2Params   `json:"params,omitempty"`
	Position *WorkflowV2Position `json:"position,omitempty"`
	Retry    *WorkflowV2Retry    `json:"retry,omitempty"`
	Timeout  string              `json:"timeout,omitempty"`
}

// WorkflowV2Condition is an optional edge guard (evaluated at runtime
// between steps, outside step.do).
type WorkflowV2Condition struct {
	Op  string `json:"op"`
	LHS string `json:"lhs"`
	RHS string `json:"rhs,omitempty"`
}

// WorkflowV2Edge is a directed dependency between two nodes. Edge ids are
// optional: the runtime synthesizes them when absent.
type WorkflowV2Edge struct {
	ID        string               `json:"id,omitempty"`
	Source    string               `json:"source"`
	Target    string               `json:"target"`
	Label     string               `json:"label,omitempty"`
	Condition *WorkflowV2Condition `json:"condition,omitempty"`
}

// WorkflowV2 is the top-level DAG document.
type WorkflowV2 struct {
	SchemaVersion int              `json:"schemaVersion"`
	Nodes         []WorkflowV2Node `json:"nodes"`
	Edges         []WorkflowV2Edge `json:"edges"`
}

// ParseWorkflowV2 decodes the raw workflow.json payload into a WorkflowV2.
// Following the plan parser's tolerant pattern, the payload may be wrapped
// in markdown fences or prose; the outermost {...} span is retried on a
// direct-decode failure. It does NOT validate semantic contracts — call
// Validate for that.
func ParseWorkflowV2(raw string) (WorkflowV2, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return WorkflowV2{}, errors.New("engine: workflow payload is empty")
	}

	var wf WorkflowV2
	if err := json.Unmarshal([]byte(trimmed), &wf); err != nil {
		start, end, ok := workflowOuterJSONObject(trimmed)
		if !ok {
			return WorkflowV2{}, fmt.Errorf("engine: workflow payload contains no JSON object: %w", err)
		}
		if err := json.Unmarshal([]byte(trimmed[start:end]), &wf); err != nil {
			return WorkflowV2{}, fmt.Errorf("engine: workflow JSON is malformed: %w", err)
		}
	}
	return wf, nil
}

// workflowOuterJSONObject returns the byte span [start,end) of the
// outermost {...} object in s. Local copy of backend/agent's tolerant-parse
// helper so the engine package stays dependency-light.
func workflowOuterJSONObject(s string) (int, int, bool) {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return 0, 0, false
	}
	return start, end + 1, true
}

// validWorkflowDuration loosely accepts Workflows-style duration strings:
// a numeric prefix plus one of the documented unit suffixes ("5 minutes",
// "30 seconds", ...). Rejects garbage like "soon" or "5".
func validWorkflowDuration(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, unit := range workflowDurationUnits {
		if !strings.HasSuffix(s, unit) {
			continue
		}
		num := strings.TrimSpace(strings.TrimSuffix(s, unit))
		if num == "" {
			return false
		}
		for i := 0; i < len(num); i++ {
			if num[i] < '0' || num[i] > '9' {
				return false
			}
		}
		return true
	}
	return false
}

// validateNodeParams enforces the per-type required params documented in
// the generation prompt. Cosmetic fields (labels, positions, edge ids) are
// intentionally NOT errors: they have runtime/frontend fallbacks.
func validateNodeParams(i int, n WorkflowV2Node) error {
	var errs []error
	params := WorkflowV2Params{}
	if n.Params != nil {
		params = *n.Params
	}
	ref := fmt.Sprintf("node %d (%s %s)", i, n.Type, n.ID)

	if n.Type == NodeTypeHTTP {
		url := strings.TrimSpace(params.URL)
		if url == "" {
			errs = append(errs, fmt.Errorf("%s: params.url is required for node.type %q", ref, NodeTypeHTTP))
		} else if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
			errs = append(errs, fmt.Errorf("%s: params.url %q must be an absolute http(s) URL", ref, url))
		}
		method := strings.ToUpper(strings.TrimSpace(params.Method))
		if method == "" {
			errs = append(errs, fmt.Errorf("%s: params.method is required for node.type %q", ref, NodeTypeHTTP))
		} else if _, ok := validHTTPMethods[method]; !ok {
			errs = append(errs, fmt.Errorf("%s: params.method %q is not one of GET|POST|PUT|PATCH|DELETE", ref, params.Method))
		}
	} else if n.Type == NodeTypeDB {
		if strings.TrimSpace(params.Table) == "" {
			errs = append(errs, fmt.Errorf("%s: params.table is required for node.type %q", ref, NodeTypeDB))
		}
		if strings.TrimSpace(params.Op) == "" {
			errs = append(errs, fmt.Errorf("%s: params.op is required for node.type %q", ref, NodeTypeDB))
		} else if _, ok := validDBOps[params.Op]; !ok {
			errs = append(errs, fmt.Errorf("%s: params.op %q is not one of insert|upsert|select|update|delete", ref, params.Op))
		}
	} else if n.Type == NodeTypeAI {
		if strings.TrimSpace(params.Model) == "" {
			errs = append(errs, fmt.Errorf("%s: params.model is required for node.type %q", ref, NodeTypeAI))
		}
		if strings.TrimSpace(params.Prompt) == "" {
			errs = append(errs, fmt.Errorf("%s: params.prompt is required for node.type %q", ref, NodeTypeAI))
		}
	} else if n.Type == NodeTypeEmail {
		if !strings.Contains(params.To, "@") {
			errs = append(errs, fmt.Errorf("%s: params.to must be an email address for node.type %q", ref, NodeTypeEmail))
		}
		if strings.TrimSpace(params.Subject) == "" {
			errs = append(errs, fmt.Errorf("%s: params.subject is required for node.type %q", ref, NodeTypeEmail))
		}
	} else if n.Type == NodeTypeCondition {
		if _, ok := validConditionOps[params.CondOp]; !ok {
			errs = append(errs, fmt.Errorf("%s: params.condOp %q is not one of eq|neq|gt|gte|lt|lte|contains|truthy", ref, params.CondOp))
		}
		if strings.TrimSpace(params.LHS) == "" {
			errs = append(errs, fmt.Errorf("%s: params.lhs is required for node.type %q", ref, NodeTypeCondition))
		}
		if params.CondOp != "truthy" && strings.TrimSpace(params.RHS) == "" {
			errs = append(errs, fmt.Errorf("%s: params.rhs is required for condOp %q (omit only for truthy)", ref, params.CondOp))
		}
	} else if n.Type == NodeTypeSleep {
		if !validWorkflowDuration(params.Duration) {
			errs = append(errs, fmt.Errorf("%s: params.duration %q must be a duration like \"5 minutes\" (number + seconds/minutes/hours/days/weeks suffix)", ref, params.Duration))
		}
	}

	return errors.Join(errs...)
}

// Validate reports every contract violation in one joined error:
//   - schemaVersion must be 2
//   - node ids: non-empty, unique; types in the closed set; per-type params
//   - exactly one trigger node and every edge references real nodes
//   - no self-loops, no cycles (the runtime topo-sorts the DAG)
//
// Cosmetic fields (labels, positions, edge ids) are not validated.
func (wf WorkflowV2) Validate() error {
	var errs []error

	if wf.SchemaVersion != WorkflowSchemaV2 {
		errs = append(errs, fmt.Errorf("schemaVersion must be %d (got %d)", WorkflowSchemaV2, wf.SchemaVersion))
	}
	if len(wf.Nodes) == 0 {
		errs = append(errs, errors.New("workflow has no nodes"))
	}

	ids := make(map[string]int, len(wf.Nodes))
	triggers := 0
	for i, n := range wf.Nodes {
		if strings.TrimSpace(n.ID) == "" {
			errs = append(errs, fmt.Errorf("node %d: id is empty", i))
		} else if prev, dup := ids[n.ID]; dup {
			errs = append(errs, fmt.Errorf("node %d: id %q is duplicated (first at node %d)", i, n.ID, prev))
		} else {
			ids[n.ID] = i
		}
		if _, ok := validWorkflowNodeTypes[n.Type]; !ok {
			errs = append(errs, fmt.Errorf("node %d (%s): type %q is not supported (want trigger|http|db|ai|email|condition|sleep)", i, n.ID, n.Type))
		}
		if n.Type == NodeTypeTrigger {
			triggers++
		}
		if verr := validateNodeParams(i, n); verr != nil {
			errs = append(errs, verr)
		}
		if n.Retry != nil {
			if n.Retry.Limit < 0 {
				errs = append(errs, fmt.Errorf("node %d (%s): retry.limit %d must be >= 0", i, n.ID, n.Retry.Limit))
			}
			if n.Retry.Limit > 10000 {
				errs = append(errs, fmt.Errorf("node %d (%s): retry.limit %d exceeds the Workflows maximum of 10000", i, n.ID, n.Retry.Limit))
			}
			if n.Retry.Backoff != "" {
				if _, ok := validBackoffModes[n.Retry.Backoff]; !ok {
					errs = append(errs, fmt.Errorf("node %d (%s): retry.backoff %q is not one of constant|linear|exponential", i, n.ID, n.Retry.Backoff))
				}
			}
		}
		if n.Timeout != "" && !validWorkflowDuration(n.Timeout) {
			errs = append(errs, fmt.Errorf("node %d (%s): timeout %q must be a duration like \"30 seconds\"", i, n.ID, n.Timeout))
		}
	}
	if triggers != 1 {
		if triggers == 0 {
			errs = append(errs, errors.New("workflow must contain exactly one trigger node (found none)"))
		} else {
			errs = append(errs, fmt.Errorf("workflow must contain exactly one trigger node (found %d)", triggers))
		}
	}

	for i, e := range wf.Edges {
		if _, ok := ids[e.Source]; !ok {
			errs = append(errs, fmt.Errorf("edge %d: source %q references an unknown node", i, e.Source))
		}
		if _, ok := ids[e.Target]; !ok {
			errs = append(errs, fmt.Errorf("edge %d: target %q references an unknown node", i, e.Target))
		}
		if e.Source != "" && e.Source == e.Target {
			errs = append(errs, fmt.Errorf("edge %d: self-loop %q -> %q", i, e.Source, e.Target))
		}
		if e.Condition != nil {
			cond := e.Condition
			if _, ok := validConditionOps[cond.Op]; !ok {
				errs = append(errs, fmt.Errorf("edge %d: condition.op %q is not one of eq|neq|gt|gte|lt|lte|contains|truthy", i, cond.Op))
			}
			if strings.TrimSpace(cond.LHS) == "" {
				errs = append(errs, fmt.Errorf("edge %d: condition.lhs is empty", i))
			}
			if cond.Op != "truthy" && strings.TrimSpace(cond.RHS) == "" {
				errs = append(errs, fmt.Errorf("edge %d: condition.rhs is required for op %q", i, cond.Op))
			}
		}
	}

	if len(wf.Edges) > 0 && hasWorkflowCycle(wf) {
		errs = append(errs, errors.New("workflow contains a cycle; edges must form a DAG"))
	}

	return errors.Join(errs...)
}

// hasWorkflowCycle reports whether the edge graph contains a cycle using
// Kahn's algorithm. The runtime executes the DAG topologically, so a cycle
// would deadlock an instance. Invalid references are tolerated (they are
// reported separately by Validate).
func hasWorkflowCycle(wf WorkflowV2) bool {
	indeg := make(map[string]int, len(wf.Nodes))
	succ := make(map[string][]string, len(wf.Nodes))
	for _, n := range wf.Nodes {
		indeg[n.ID] = 0
		succ[n.ID] = []string{}
	}
	for _, e := range wf.Edges {
		indeg[e.Target]++
		succ[e.Source] = append(succ[e.Source], e.Target)
	}

	queue := []string{}
	for _, n := range wf.Nodes {
		if indeg[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}
	visited := 0
	for qi := 0; qi < len(queue); qi++ {
		id := queue[qi]
		visited++
		for _, next := range succ[id] {
			indeg[next]--
			if indeg[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	return visited != len(wf.Nodes)
}
