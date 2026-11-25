package mcp

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// P1-AGT-004 (D8.3) — the route-facing surface.
//
// The Gate remains the only authority: it checks identity, grants, policy drift,
// approvals and idempotency. This type exists so an HTTP handler cannot assemble
// a Gate that skips a reviewed store, and so the wire rule (which bindings a
// route may use) lives in one place instead of in every handler.

// TransportFactory builds the transport for one registered server. The second
// argument carries the credential headers the caller resolved from `vault:` refs
// (nil when the policy needs none). A field so a test can inject a stub, and so
// production has exactly one rule.
type TransportFactory func(ServerRegistration, map[string]string) (Transport, error)

// Surface wires the gate and the reviewed stores the MCP routes read.
type Surface struct {
	Registry  *Registry
	Policies  *PolicyStore
	Grants    *Store
	Audit     *MemorySink
	Gate      *Gate
	Transport TransportFactory
	// Now is the surface clock for grants and calls (tests pin it).
	Now func() time.Time
}

// NewSurface builds an empty surface with the production transport rule.
func NewSurface(now func() time.Time) (*Surface, error) {
	s := &Surface{
		Registry:  NewRegistry(),
		Policies:  NewPolicyStore(),
		Grants:    NewStore(),
		Audit:     NewMemorySink(),
		Transport: SSEOnlyTransport,
		Now:       now,
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	gate, err := NewGate(GateConfig{
		Registry: s.Registry,
		Policies: s.Policies,
		Grants:   s.Grants,
		Audit:    s.Audit,
		Now:      s.Now,
	})
	if err != nil {
		return nil, err
	}
	s.Gate = gate
	return s, nil
}

// SSEOnlyTransport is the D8.3 wire rule for the route surface: an HTTP request
// may reach an `http(s)` MCP server (the SSE binding) and nothing else.
//
// A `stdio://` registration would mean an HTTP request spawns a local process;
// that is not what D8.2/D8.3 admit. The stdio binding stays a PoC used directly
// by code (and by its own tests), never by a route.
func SSEOnlyTransport(server ServerRegistration, headers map[string]string) (Transport, error) {
	raw := strings.TrimSpace(server.Endpoint)
	parsed, err := url.Parse(raw)
	if err != nil || raw == "" {
		return nil, fmt.Errorf("mcp: server %q endpoint is not a URL", server.Name)
	}
	switch parsed.Scheme {
	case "http", "https":
		return &SSETransport{Endpoint: raw, Headers: headers}, nil
	default:
		return nil, fmt.Errorf(
			"mcp: server %q endpoint scheme %q is not routable (the route surface uses the SSE binding only)",
			server.Name, parsed.Scheme,
		)
	}
}
