package mcp

import (
	"fmt"
	"strings"
)

// Identity is the tenant-scoped principal that owns a grant or a call.
// The adapter never reads tenant/user identity from tool arguments —
// arguments are model-controlled and therefore untrusted — so every entry
// point requires an explicit Identity (see grant.go, execution.go).
type Identity struct {
	// Tenant is the mandatory tenant scope. Empty means "no tenant", and
	// any grant or execution with an empty tenant is rejected.
	Tenant string
	// User identifies the human on whose behalf the agent acts, if known.
	User string
	// Agent identifies the calling agent (manifest name), if known.
	Agent string
}

// Valid reports whether the identity can own a grant or a call.
func (i Identity) Valid() bool {
	return strings.TrimSpace(i.Tenant) != ""
}

func (i Identity) String() string {
	return fmt.Sprintf("tenant=%s user=%s agent=%s", i.Tenant, i.User, i.Agent)
}
