package toolsearch

import (
	"time"

	"backend/pkg/mcp"
)

// GrantID returns the live grant that authorizes this match for the caller's
// tenant, or ("", false) when there is none.
//
// This is the whole of search's authority: it can tell a caller which grant to
// use, and it can tell a caller there is no grant. It cannot issue, widen or
// bypass one — the Gate re-checks registration, revocation, expiry, policy
// drift, approvals and idempotency on every call, so a stale answer here fails
// closed there.
func (m Match) GrantID(grants []mcp.Grant, now time.Time) (string, bool) {
	// Sort-free scan: a tenant has few grants for one ref, and the first
	// match is enough because a live grant is a live grant.
	for _, g := range grants {
		if g.Revoked || g.Expired(now) {
			continue
		}
		if g.Ref != m.Candidate.Ref {
			continue
		}
		return g.ID, true
	}
	return "", false
}

// Verified is one match after a grant check.
type Verified struct {
	Match
	// Granted is true when a live grant covers the candidate.
	Granted bool
	// GrantID echoes the grant to pass to Gate.Call.
	GrantID string
}

// Verify checks every match against the caller's grants. Callers are expected to
// render only verified matches as callable: an unverified candidate is still a
// useful hint ("this exists, ask for access"), but it must never be presented as
// callable, or the model will hallucinate a way around the gate.
func Verify(matches []Match, grants []mcp.Grant, now time.Time) []Verified {
	out := make([]Verified, 0, len(matches))
	for _, m := range matches {
		id, ok := m.GrantID(grants, now)
		out = append(out, Verified{Match: m, Granted: ok, GrantID: id})
	}
	return out
}

// CallRequest builds the request a caller would hand to mcp.Gate for a verified
// match. It refuses an ungranted match instead of producing a request that could
// only fail: the error is the answer.
func (v Verified) CallRequest(id mcp.Identity, argsJSON string) (mcp.CallRequest, error) {
	if !v.Granted || v.GrantID == "" {
		return mcp.CallRequest{}, &mcp.DeniedError{Verdict: "search surfaced " + v.Candidate.Pinned() + " without a live grant: request access instead of calling"}
	}
	return mcp.CallRequest{
		Identity: id,
		Ref:      v.Candidate.Ref,
		GrantID:  v.GrantID,
		ArgsJSON: argsJSON,
	}, nil
}
