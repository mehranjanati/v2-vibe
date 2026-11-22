package agentskills

import (
	"time"

	"backend/pkg/mcp"
)

// Binding is the pure result of intersecting a skill's requested tools with
// the tenant's live grants. It is the loader's entire contribution to
// authority: it reports what the Gate would already allow, and grants
// nothing itself.
type Binding struct {
	// Allowed lists the live grants that cover a requested ref.
	Allowed []mcp.Grant
	// Denied lists requested refs no live grant covers. A denied ref is not
	// an error — the skill may still run its own text — it is a capability
	// the caller must obtain through the normal grant flow.
	Denied []mcp.ServerRef
	// Advisory lists `allowed-tools` entries that were never pinned refs.
	// They are echoed for visibility and authorize nothing, ever.
	Advisory []string
}

// Allows reports whether a live grant covers exactly this pinned ref.
func (b Binding) Allows(ref mcp.ServerRef) bool {
	for _, g := range b.Allowed {
		if g.Ref == ref {
			return true
		}
	}
	return false
}

// Bind computes which of the skill's pinned tools the tenant may call right
// now. It is a filter over grants the caller already fetched for its own
// tenant, so it cannot cross tenants and cannot widen authority: a skill can
// only ever *narrow* what the grants already permit.
//
// The Gate remains the enforcement point — every allowed ref still has to
// pass Gate.Call with its grant ID, which re-checks registration, revocation,
// expiry, policy drift, approvals and idempotency.
func (s *Skill) Bind(grants []mcp.Grant, now time.Time) Binding {
	var out Binding
	out.Advisory = append(out.Advisory, s.UnpinnedTools...)
	for _, ref := range s.AllowedTools {
		matched := false
		for _, g := range grants {
			if g.Revoked || g.Expired(now) {
				continue
			}
			if g.Ref != ref {
				continue
			}
			out.Allowed = append(out.Allowed, g)
			matched = true
		}
		if !matched {
			out.Denied = append(out.Denied, ref)
		}
	}
	return out
}

// ScriptsAllowed reports whether the skill's scripts may be read at all. It
// mirrors Resource(KindScripts, ...) so a caller can explain a refusal before
// attempting it.
func (s *Skill) ScriptsAllowed() bool { return s.Trust != TrustCommunity }
