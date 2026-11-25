package activation

import "strings"

// matches applies one rule to one candidate. Every field present must match;
// lists are OR within themselves; permissions are a subset check, because a
// rule may narrow the grant and never widen it.
func (r Rule) matches(facts Facts, granted map[string]bool) bool {
	match := r.Match
	if len(match.Kinds) > 0 && !containsKind(match.Kinds, facts.Kind) {
		return false
	}
	if len(match.Refs) > 0 && !containsRef(match.Refs, facts.Ref) {
		return false
	}
	if len(match.Publishers) > 0 && !containsString(match.Publishers, facts.Publisher) {
		return false
	}
	if len(match.TrustLevels) > 0 && !containsTrust(match.TrustLevels, facts.TrustLevel) {
		return false
	}
	if len(match.SideEffects) > 0 && !containsSideEffect(match.SideEffects, facts.SideEffect) {
		return false
	}
	for _, permission := range match.Permissions {
		if !granted[permission] {
			return false
		}
	}
	for _, approval := range match.Approvals {
		if !containsApproval(facts.Approvals, approval) {
			return false
		}
	}
	if match.RequiresHuman != nil && *match.RequiresHuman != facts.RequiresHuman {
		return false
	}
	if len(match.Scopes) > 0 && !containsScope(match.Scopes, facts.Scope) {
		return false
	}
	if len(match.ResolutionModes) > 0 && !containsMode(match.ResolutionModes, facts.ResolutionMode) {
		return false
	}
	return true
}

// containsRef accepts an exact ref or a `kind/name@*` prefix. A prefix names one
// identity, never a version range: `ParseRef` has already refused ranges.
func containsRef(allowed []string, ref string) bool {
	for _, candidate := range allowed {
		if candidate == ref {
			return true
		}
		if strings.HasSuffix(candidate, "@*") && strings.TrimSuffix(candidate, "@*") == identity(ref) {
			return true
		}
	}
	return false
}

func identity(ref string) string {
	if at := strings.LastIndex(ref, "@"); at > 0 {
		return ref[:at]
	}
	return ref
}

func containsKind(list []Kind, value Kind) bool              { return oneOf(value, list...) }
func containsTrust(list []TrustLevel, value TrustLevel) bool { return oneOf(value, list...) }
func containsSideEffect(list []SideEffect, value SideEffect) bool {
	return oneOf(value, list...)
}
func containsScope(list []Scope, value Scope) bool { return oneOf(value, list...) }
func containsMode(list []ResolutionMode, value ResolutionMode) bool {
	return oneOf(value, list...)
}
func containsString(list []string, value string) bool {
	for _, candidate := range list {
		if candidate == value {
			return true
		}
	}
	return false
}
func containsApproval(list []ApprovalClass, value ApprovalClass) bool {
	return oneOf(value, list...)
}
