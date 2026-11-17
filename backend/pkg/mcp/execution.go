package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// CallRequest is one MCP tool invocation request coming from an agent.
type CallRequest struct {
	// Identity is the tenant-scoped caller. Required.
	Identity Identity
	// Ref names the pinned MCP tool.
	Ref ServerRef
	// GrantID names the grant the caller believes authorizes this call.
	// Execution verifies it; believing is not authorizing.
	GrantID string
	// ArgsJSON is the model-supplied argument object.
	ArgsJSON string
	// ApprovalRef records a live approval reference when policy demands one.
	ApprovalRef string
}

// CallResult is the outcome of an allowed call.
type CallResult struct {
	Output string
	// GrantID echoes the grant that authorized the call.
	GrantID string
	// IdempotencyKey is the deduplication key recorded for this call.
	IdempotencyKey string
}

// DeniedError describes a refused call. It always carries the verdict so a
// caller can map it to a 403-style response without re-deriving the reason.
type DeniedError struct {
	Verdict string
}

func (e *DeniedError) Error() string { return "mcp: call denied: " + e.Verdict }

// Gate is the trust boundary: the only path from an agent to an MCP server.
// It owns discovery, grant enforcement, idempotency, and audit.
type Gate struct {
	registry *Registry
	policies *PolicyStore
	grants   *Store
	audit    AuditSink
	now      func() time.Time

	// idempotency remembers completed keys per tenant.
	mu          sync.Mutex
	idempotency map[string]string // tenant + key -> recorded output
}

// GateConfig wires a Gate.
type GateConfig struct {
	Registry *Registry
	Policies *PolicyStore
	Grants   *Store
	Audit    AuditSink
	// Now overrides the clock (tests). Defaults to time.Now.
	Now func() time.Time
}

// NewGate builds the trust boundary. Every dependency is required: a Gate
// without an audit sink or a policy store cannot enforce anything.
func NewGate(cfg GateConfig) (*Gate, error) {
	if cfg.Registry == nil || cfg.Policies == nil || cfg.Grants == nil || cfg.Audit == nil {
		return nil, fmt.Errorf("mcp: gate: registry, policies, grants and audit are all required")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Gate{
		registry:    cfg.Registry,
		policies:    cfg.Policies,
		grants:      cfg.Grants,
		audit:       cfg.Audit,
		now:         now,
		idempotency: make(map[string]string),
	}, nil
}

// DiscoveryResult is read-only metadata: no grant, no execution.
type DiscoveryResult struct {
	Server string
	Tools  []DiscoveredTool
	// Note states, in one line, that discovery grants nothing. It is surfaced
	// to callers so the boundary is visible in logs and UIs.
	Note string
}

// Discover lists a registered server's tools. Registration alone is enough to
// read metadata; nothing here authorizes execution.
func (g *Gate) Discover(id Identity, server string, tr Transport) (DiscoveryResult, error) {
	if !id.Valid() {
		return DiscoveryResult{}, fmt.Errorf("mcp: discover: tenant is required")
	}
	reg, ok := g.registry.Get(server)
	if !ok {
		return DiscoveryResult{}, fmt.Errorf("mcp: discover: server %q is not registered", server)
	}
	tools, err := tr.DiscoverTools(reg)
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("mcp: discover: %s: %w", server, err)
	}
	return DiscoveryResult{
		Server: server,
		Tools:  tools,
		Note: "discovery returns metadata only; each tool needs an explicit " +
			"tenant-scoped grant before it can execute",
	}, nil
}

// Call is the only execution path. It fails closed on every check and writes
// an audit event for both allowed and denied attempts.
func (g *Gate) Call(req CallRequest, tr Transport) (CallResult, error) {
	now := g.now().UTC()
	event := g.baseEvent(req, now)

	deny := func(verdict string) (CallResult, error) {
		event.Decision = DecisionDenied
		event.Verdict = verdict
		g.audit.Append(event)
		return CallResult{}, &DeniedError{Verdict: verdict}
	}

	// 1. Tenant identity is mandatory and comes from the caller, not args.
	if !req.Identity.Valid() {
		return deny("no tenant identity on the call")
	}
	// 2. The ref must be pinned and well-formed.
	if !req.Ref.Valid() {
		return deny("ref must be mcp/server/tool@MAJOR.MINOR.PATCH")
	}
	// 3. The server must still be registered (removal revokes immediately).
	reg, ok := g.registry.Get(req.Ref.Server)
	if !ok {
		return deny("server " + req.Ref.Server + " is not registered")
	}
	// 4. A grant ID is required; discovery never implies one.
	if req.GrantID == "" {
		return deny("no grant: discovery does not authorize execution")
	}
	grant, ok := g.grants.lookup(req.Identity, req.GrantID)
	if !ok {
		return deny("grant not found for this tenant")
	}
	event.GrantID = grant.ID
	// 5. Revoked / expired grants fail closed.
	if grant.revokedInternal() {
		return deny("grant revoked")
	}
	if grant.Expired(now) {
		return deny("grant expired")
	}
	// 6. The grant must cover exactly this server/tool/version.
	if grant.Ref.Server != req.Ref.Server || grant.Ref.Tool != req.Ref.Tool || grant.Ref.Version != req.Ref.Version {
		return deny("grant does not cover ref " + req.Ref.String())
	}
	// 7. The reviewed snapshot must still match the current policy: drift
	//    forces a re-grant instead of silently widening authority.
	current, ok := g.policies.Get(req.Ref.Server, req.Ref.Tool)
	if !ok {
		return deny("no policy for " + req.Ref.String())
	}
	if !samePolicy(grant.Policy, current) {
		return deny("policy drifted since the grant was issued; re-grant required")
	}
	// 8. Tools that need a live human approval must have one.
	if current.RequiresHuman {
		if !grant.HumanApproved {
			return deny("tool requires live human approval")
		}
		if req.ApprovalRef == "" {
			return deny("tool requires an approval reference on every call")
		}
		event.ApprovalRef = req.ApprovalRef
	}
	// 9. Irreversible tools must carry an idempotency key from args.
	idemKey := ""
	if current.IdempotencyKey != "" {
		key, err := lookupStringField(req.ArgsJSON, current.IdempotencyKey)
		if err != nil {
			return deny("idempotency key " + current.IdempotencyKey + " missing from args")
		}
		idemKey = key
	}
	event.IdempotencyKey = idemKey

	// 10. Idempotent replay: return the recorded result, do not re-execute.
	if idemKey != "" {
		if out, seen := g.replay(req.Identity.Tenant, idemKey); seen {
			event.Decision = DecisionAllowed
			event.Verdict = "idempotent replay: already executed"
			g.audit.Append(event)
			return CallResult{Output: out, GrantID: grant.ID, IdempotencyKey: idemKey}, nil
		}
	}

	// 11. Execute. A transport error is not an authorization decision, but it
	//     is still audited as a denied attempt with its own verdict.
	out, err := tr.InvokeTool(reg, req.Ref.Tool, req.ArgsJSON)
	if err != nil {
		event.Decision = DecisionDenied
		event.Verdict = "transport error: " + err.Error()
		g.audit.Append(event)
		return CallResult{}, fmt.Errorf("mcp: call %s: %w", req.Ref.String(), err)
	}

	if idemKey != "" {
		g.remember(req.Identity.Tenant, idemKey, out)
	}
	event.Decision = DecisionAllowed
	event.Verdict = "all trust checks passed"
	g.audit.Append(event)
	return CallResult{Output: out, GrantID: grant.ID, IdempotencyKey: idemKey}, nil
}

func (g *Gate) baseEvent(req CallRequest, now time.Time) AuditEvent {
	return AuditEvent{
		At: now, Tenant: req.Identity.Tenant, User: req.Identity.User, Agent: req.Identity.Agent,
		Server: req.Ref.Server, Tool: req.Ref.Tool, Version: req.Ref.Version,
	}
}

func (g *Gate) replay(tenant, key string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out, ok := g.idempotency[tenant+"\x00"+key]
	return out, ok
}

func (g *Gate) remember(tenant, key, out string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.idempotency[tenant+"\x00"+key] = out
}

// samePolicy compares the authority-bearing fields of two policies. Text and
// metadata differences do not widen authority; permissions, side effect,
// approvals, secrets, version and the human requirement do.
func samePolicy(a, b ToolPolicy) bool {
	if a.ToolManifestVersion != b.ToolManifestVersion || a.SideEffect != b.SideEffect {
		return false
	}
	if a.RequiresHuman != b.RequiresHuman || a.IdempotencyKey != b.IdempotencyKey {
		return false
	}
	if fmt.Sprint(sortedStrings(a.Permissions)) != fmt.Sprint(sortedStrings(b.Permissions)) {
		return false
	}
	if fmt.Sprint(sortedStrings(a.Secrets)) != fmt.Sprint(sortedStrings(b.Secrets)) {
		return false
	}
	return fmt.Sprint(sortedApprovals(a.Approvals)) == fmt.Sprint(sortedApprovals(b.Approvals))
}

// lookupStringField resolves a dotted argument path (e.g. `lead.id`) and
// returns it as a non-empty string. A dotted path keeps the Tool Manifest
// idempotency contract honest for nested payloads.
func lookupStringField(argsJSON, field string) (string, error) {
	var root map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &root); err != nil {
		return "", fmt.Errorf("args are not a JSON object: %w", err)
	}
	var current any = root
	for _, segment := range strings.Split(field, ".") {
		obj, ok := current.(map[string]any)
		if !ok {
			return "", fmt.Errorf("field %q is not reachable (segment %q)", field, segment)
		}
		next, ok := obj[segment]
		if !ok {
			return "", fmt.Errorf("field %q absent (segment %q)", field, segment)
		}
		current = next
	}
	s, ok := current.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("field %q must be a non-empty string", field)
	}
	return s, nil
}
