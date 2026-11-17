package mcp

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// vaultRefRe matches secret references (`vault:<path>`), the only secret
// form the trust slice accepts. Literal values are rejected: secrets live
// in the vault, never in grants, args, or audit events.
var vaultRefRe = regexp.MustCompile(`^vault:[a-z0-9][a-z0-9/_.-]*$`)

// ToolPolicy maps one server tool into the platform permission model. The
// grant (grant.go) records a snapshot of the policy it was reviewed against;
// execution enforces the snapshot, and drift forces re-grant (see execution.go).
type ToolPolicy struct {
	// Server and Tool name the MCP server tool this policy maps.
	Server string
	Tool   string
	// ToolManifestVersion pins the reviewed Tool Manifest version.
	ToolManifestVersion string
	// Permissions are the platform `resource:action` grants (e.g.
	// `leads:read`). Execution never consults model text for authority.
	Permissions []string
	// SideEffect is the reviewed side-effect level.
	SideEffect SideEffect
	// Approvals lists the approval classes required before execution.
	Approvals []ApprovalClass
	// Timeout bounds a single invocation; zero means the adapter default.
	TimeoutSeconds int
	// MaxRetries bounds transparent retries (idempotent tools only).
	MaxRetries int
	// IdempotencyKey names the arg field that deduplicates business writes.
	// Empty for side-effect-free tools.
	IdempotencyKey string
	// Secrets lists `vault:` references the invocation may resolve.
	Secrets []string
	// RequiresHuman is true when policy alone can never authorize this tool
	// (e.g. payments): every call needs a live human grant.
	RequiresHuman bool
}

func (p ToolPolicy) key() string { return p.Server + "/" + p.Tool }

// Valid reports whether the policy is reviewable and enforceable.
func (p ToolPolicy) Valid() error {
	if !slugRe.MatchString(p.Server) {
		return fmt.Errorf("mcp: policy: server %q must be a DNS-safe slug", p.Server)
	}
	if strings.TrimSpace(p.Tool) == "" {
		return fmt.Errorf("mcp: policy: tool name is required")
	}
	if !versionRe.MatchString(p.ToolManifestVersion) {
		return fmt.Errorf("mcp: policy: tool manifest version %q must be pinned MAJOR.MINOR.PATCH", p.ToolManifestVersion)
	}
	switch p.SideEffect {
	case SideEffectSafe, SideEffectExternal, SideEffectIrreversible:
	default:
		return fmt.Errorf("mcp: policy: side effect %q must be safe, external or irreversible", p.SideEffect)
	}
	for _, s := range p.Secrets {
		if !vaultRefRe.MatchString(s) {
			return fmt.Errorf("mcp: policy: secret %q must be a vault: reference, never a literal", s)
		}
	}
	if p.SideEffect == SideEffectIrreversible && len(p.Approvals) == 0 {
		return fmt.Errorf("mcp: policy: irreversible tools require at least one approval class")
	}
	if p.TimeoutSeconds < 0 {
		return fmt.Errorf("mcp: policy: timeout must not be negative")
	}
	if p.MaxRetries < 0 {
		return fmt.Errorf("mcp: policy: max retries must not be negative")
	}
	return nil
}

// PolicyStore maps server tools to their reviewed policies. Persistence is
// a follow-up; the interface keeps the caller side stable.
type PolicyStore struct {
	mu       sync.RWMutex
	policies map[string]ToolPolicy
}

// NewPolicyStore returns an empty policy store.
func NewPolicyStore() *PolicyStore {
	return &PolicyStore{policies: make(map[string]ToolPolicy)}
}

// Put records a reviewed policy, replacing any previous one. Replacing a
// policy does not touch existing grants — their snapshots keep enforcing
// the version they were reviewed against until they are re-granted.
func (s *PolicyStore) Put(p ToolPolicy) error {
	if err := p.Valid(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies[p.key()] = p
	return nil
}

// Get returns the current policy for a server tool.
func (s *PolicyStore) Get(server, tool string) (ToolPolicy, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.policies[server+"/"+tool]
	return p, ok
}

// Remove drops a policy. Existing grants are unaffected — they carry their
// own snapshot — but no new grant can be issued until a policy is recorded.
func (s *PolicyStore) Remove(server, tool string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.policies, server+"/"+tool)
}
