package mcp

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// versionRe pins Tool Manifest versions (MAJOR.MINOR.PATCH, no ranges).
var versionRe = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// ServerRef identifies a pinned MCP tool exposed by one registered server:
// `mcp/<server-name>@<MAJOR.MINOR.PATCH>`.
type ServerRef struct {
	// Server is the registry name of the MCP server (DNS-safe slug).
	Server string
	// Tool is the tool name as published by that server.
	Tool string
	// Version pins the Tool Manifest version the grant was reviewed against.
	Version string
}

var slugRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// Valid reports whether the ref is pinned and well-formed.
func (r ServerRef) Valid() bool {
	return slugRe.MatchString(r.Server) && r.Tool != "" && versionRe.MatchString(r.Version)
}

func (r ServerRef) String() string {
	return fmt.Sprintf("mcp/%s/%s@%s", r.Server, r.Tool, r.Version)
}

// SideEffect mirrors the Tool Manifest contract vocabulary.
type SideEffect string

const (
	SideEffectSafe         SideEffect = "safe"
	SideEffectExternal     SideEffect = "external"
	SideEffectIrreversible SideEffect = "irreversible"
)

// ApprovalClass mirrors the Tool Manifest approval vocabulary.
type ApprovalClass string

const (
	ApprovalExternalMessage ApprovalClass = "external-message"
	ApprovalExternalWrite   ApprovalClass = "external-write"
	ApprovalPayment         ApprovalClass = "payment"
	ApprovalHumanReview     ApprovalClass = "human-review"
)

// ServerRegistration records one MCP server the platform trusts enough to
// talk to. Registration is the weakest trust level: it authorizes
// discovery-time metadata reads only, never execution.
type ServerRegistration struct {
	// Name is the registry key (DNS-safe slug, immutable).
	Name string
	// Endpoint dials the server (transport address, resolved by the caller).
	Endpoint string
	// Tools lists the tool names the server advertises, recorded at
	// registration time for policy mapping. Discovery refreshes this list;
	// execution is unaffected either way (see grant.go).
	Tools []string
	// RegisteredBy is the human or policy that approved the registration.
	RegisteredBy string
	// RegisteredAt is the registration timestamp (UTC).
	RegisteredAt time.Time
}

// Transport dials MCP servers and invokes their tools. It is an interface
// so the first real transport (stdio/SSE, P7.x follow-up) lands behind the
// trust boundary without touching policy, grant, or audit code.
type Transport interface {
	// DiscoverTools returns the tool metadata the server currently
	// advertises. No grant is required and no execution happens here.
	DiscoverTools(server ServerRegistration) ([]DiscoveredTool, error)
	// InvokeTool executes one tool on the server. The adapter calls it only
	// after the trust checks in execution.go pass.
	InvokeTool(server ServerRegistration, toolName string, argsJSON string) (string, error)
}

// DiscoveredTool is read-only discovery metadata: enough to map a policy,
// never enough to execute.
type DiscoveredTool struct {
	Name        string
	Description string
	// SideEffect and Approvals are the server's *declared* values, refreshed
	// from discovery. Execution uses the approved Tool Manifest values, not
	// these — the registry never trusts a fresh declaration over review.
	SideEffect SideEffect
	Approvals  []ApprovalClass
}

// Registry is the in-memory trust root for MCP servers. Persistence (D1 +
// tenant isolation) is a P7.3 follow-up; the interface is shaped so the
// store can replace this without changing callers.
type Registry struct {
	mu      sync.RWMutex
	servers map[string]ServerRegistration
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{servers: make(map[string]ServerRegistration)}
}

// Register records a server. Registration authorizes discovery only.
func (r *Registry) Register(s ServerRegistration) error {
	if !slugRe.MatchString(s.Name) {
		return fmt.Errorf("mcp: register: server name %q must be a DNS-safe slug", s.Name)
	}
	if strings.TrimSpace(s.Endpoint) == "" {
		return fmt.Errorf("mcp: register: endpoint is required for server %q", s.Name)
	}
	if strings.TrimSpace(s.RegisteredBy) == "" {
		return fmt.Errorf("mcp: register: registered-by (human or policy) is required for server %q", s.Name)
	}
	s.RegisteredAt = s.RegisteredAt.UTC()
	if s.RegisteredAt.IsZero() {
		return fmt.Errorf("mcp: register: registered-at is required for server %q", s.Name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.servers[s.Name]; ok {
		return fmt.Errorf("mcp: register: server %q is already registered (names are immutable)", s.Name)
	}
	r.servers[s.Name] = s
	return nil
}

// Get returns a server registration by name.
func (r *Registry) Get(name string) (ServerRegistration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.servers[name]
	return s, ok
}

// List returns the server names in sorted order.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.servers))
	for name := range r.servers {
		out = append(out, name)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Registrations returns every server registration in name order. Callers get
// metadata only — registration authorizes discovery, never execution.
func (r *Registry) Registrations() []ServerRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ServerRegistration, 0, len(r.servers))
	for _, s := range r.servers {
		out = append(out, s)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Name < out[j-1].Name; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Remove unregisters a server. Existing grants against it keep failing
// closed (grant.go checks registration at every call), so removal is an
// immediate revocation path.
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.servers, name)
}
