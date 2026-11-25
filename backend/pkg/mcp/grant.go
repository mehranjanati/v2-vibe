package mcp

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Grant is the single unit of execution authority for one MCP tool. It is
// tenant-scoped, content-addressed, and carries the policy snapshot it was
// reviewed against, so later policy edits cannot silently widen a grant.
type Grant struct {
	// ID is the grant identifier, `g_` + 26 lowercase alphanumerics.
	ID string
	// Tenant scopes the grant. A grant never crosses tenants.
	Tenant string
	// Ref pins the server tool and the reviewed manifest version.
	Ref ServerRef
	// Policy is the snapshot of the reviewed ToolPolicy the grant permits.
	// Execution enforces this snapshot, not the store's current policy.
	Policy ToolPolicy
	// GrantedBy is the human or policy that approved the grant.
	GrantedBy string
	// HumanApproved is true when a live human approved (vs a standing
	// tenant policy). Tools with RequiresHuman never execute without it.
	HumanApproved bool
	// CreatedAt is the grant timestamp (UTC).
	CreatedAt time.Time
	// ExpiresAt, when set, bounds the grant lifetime. The zero value means
	// the grant lives until revoked.
	ExpiresAt time.Time
	// Revoked is true once revoked; revocation is permanent and immediate.
	Revoked bool
	// RevokedAt and RevokedBy record the revocation, when it happens.
	RevokedAt time.Time
	RevokedBy string
}

// Expired reports whether the grant has passed its expiry.
func (g Grant) Expired(now time.Time) bool {
	return !g.ExpiresAt.IsZero() && !now.Before(g.ExpiresAt)
}

// revokedInternal reports whether the grant must no longer authorize calls.
func (g Grant) revokedInternal() bool { return g.Revoked }

// Store keeps tenant-scoped grants in memory. Persistence (D1, tenant
// isolation enforced in the store, not the caller) is a P7.3 follow-up; the
// Store API is shaped for it: every method takes the caller Identity and
// never infers scope from arguments.
type Store struct {
	mu     sync.RWMutex
	grants map[string]Grant // key: tenant + "\x00" + grant ID
}

func key(tenant, id string) string { return tenant + "\x00" + id }

// NewStore returns an empty grant store.
func NewStore() *Store {
	return &Store{grants: make(map[string]Grant)}
}

var grantIDChars = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

// newGrantID derives a deterministic-looking local ID. Uniqueness across
// restarts is the persistent store's job; this keeps the trust slice
// testable without a clock or RNG dependency.
var grantCounter = 0

func nextGrantID() string {
	grantCounter++
	n := grantCounter
	var b [26]rune
	for i := 25; i >= 0; i-- {
		b[i] = grantIDChars[n%len(grantIDChars)]
		n /= len(grantIDChars)
	}
	return "g_" + string(b[:])
}

// Issue records a grant for id after validating the tenant, the ref, and
// the policy snapshot. It returns the grant or a descriptive error.
func (s *Store) Issue(id Identity, ref ServerRef, policy ToolPolicy, grantedBy string, humanApproved bool, expiresAt time.Time, now time.Time) (Grant, error) {
	if !id.Valid() {
		return Grant{}, fmt.Errorf("mcp: grant: tenant is required (identity: %s)", id)
	}
	if !ref.Valid() {
		return Grant{}, fmt.Errorf("mcp: grant: ref %q must be mcp/server/tool@MAJOR.MINOR.PATCH", ref)
	}
	if err := policy.Valid(); err != nil {
		return Grant{}, err
	}
	if policy.Server != ref.Server || policy.Tool != ref.Tool || policy.ToolManifestVersion != ref.Version {
		return Grant{}, fmt.Errorf("mcp: grant: policy snapshot does not match ref %s (server/tool/version must agree)", ref)
	}
	if strings.TrimSpace(grantedBy) == "" {
		return Grant{}, fmt.Errorf("mcp: grant: granted-by (human or policy) is required")
	}
	if policy.RequiresHuman && !humanApproved {
		return Grant{}, fmt.Errorf("mcp: grant: tool %s requires live human approval; a standing policy is not enough", ref)
	}
	now = now.UTC()
	if !expiresAt.IsZero() {
		expiresAt = expiresAt.UTC()
		if !now.Before(expiresAt) {
			return Grant{}, fmt.Errorf("mcp: grant: expiry must be in the future")
		}
	}
	g := Grant{
		ID: nextGrantID(), Tenant: id.Tenant, Ref: ref, Policy: policy,
		GrantedBy: grantedBy, HumanApproved: humanApproved,
		CreatedAt: now, ExpiresAt: expiresAt,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants[key(id.Tenant, g.ID)] = g
	return g, nil
}

// Revoke permanently disables a grant. Unknown IDs are a no-op error the
// audit sink still records (see execution.go).
func (s *Store) Revoke(id Identity, grantID, revokedBy string, now time.Time) error {
	if !id.Valid() {
		return fmt.Errorf("mcp: revoke: tenant is required")
	}
	if strings.TrimSpace(revokedBy) == "" {
		return fmt.Errorf("mcp: revoke: revoked-by is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.grants[key(id.Tenant, grantID)]
	if !ok {
		return fmt.Errorf("mcp: revoke: unknown grant %q for tenant %q", grantID, id.Tenant)
	}
	if g.Revoked {
		return fmt.Errorf("mcp: revoke: grant %q is already revoked", grantID)
	}
	g.Revoked = true
	g.RevokedAt = now.UTC()
	g.RevokedBy = revokedBy
	s.grants[key(id.Tenant, grantID)] = g
	return nil
}

// lookup returns the grant only if it belongs to the identity's tenant.
func (s *Store) lookup(id Identity, grantID string) (Grant, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.grants[key(id.Tenant, grantID)]
	return g, ok
}

// Get returns one of the tenant's grants. Unknown or another tenant's id is
// (zero, false) — the same wall `lookup` enforces for execution, exposed so a
// route can refuse before dialing (e.g. a tool whose policy needs the vault).
func (s *Store) Get(id Identity, grantID string) (Grant, bool) {
	if !id.Valid() {
		return Grant{}, false
	}
	return s.lookup(id, grantID)
}

// List returns the tenant's grants in ID order, including revoked ones
// (audit history, not a live set).
func (s *Store) List(id Identity) []Grant {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Grant
	for k, g := range s.grants {
		if strings.HasPrefix(k, id.Tenant+"\x00") {
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
