package activation

import (
	"regexp"
	"strings"

	"backend/pkg/composition"
	"backend/pkg/ir"
)

// policyRefRe pins the policy reference. A policy edit is a new version, exactly
// like a manifest, so a floating reference cannot exist.
var policyRefRe = regexp.MustCompile(`^activation-policy/[a-z0-9]([-a-z0-9]*[a-z0-9])?@` + ir.SemverPattern + `$`)

func oneOf[T ~string](value T, allowed ...T) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

// validate checks the policy before any candidate is considered. `closure` is
// the snapshot's declared set: a policy that allows something the composition
// never declared cannot be enforced, so it fails to load (E_ACTIVATION_POLICY).
func (p Policy) validate(closure map[string]bool) *Error {
	if !policyRefRe.MatchString(p.Ref) {
		return policyErrorf("policy ref %q is not activation-policy/<name>@MAJOR.MINOR.PATCH", p.Ref)
	}
	if p.MinCapabilities < 0 {
		return policyErrorf("minCapabilities cannot be negative")
	}
	if p.MaxCandidates < 0 {
		return policyErrorf("maxCandidates cannot be negative")
	}
	seen := map[string]bool{}
	for index, rule := range p.Rules {
		where := "rule[" + itoa(index) + "]"
		if strings.TrimSpace(rule.ID) == "" {
			return policyErrorf("%s: id is required (it is cited by the decision trace)", where)
		}
		if seen[rule.ID] {
			return policyErrorf("duplicate rule id %q", rule.ID)
		}
		seen[rule.ID] = true
		if !oneOf(rule.Effect, EffectAllow, EffectDeny) {
			return policyErrorf("rule %q: effect %q is not allow|deny", rule.ID, rule.Effect)
		}
		if rule.Effect == EffectDeny && strings.TrimSpace(rule.Reason) == "" {
			return policyErrorf("rule %q: a deny must carry a reason", rule.ID)
		}
		if err := rule.Match.validate(); err != nil {
			return policyErrorf("rule %q: %s", rule.ID, err.Error())
		}
		if rule.Effect != EffectAllow {
			continue
		}
		for _, ref := range rule.Match.Refs {
			if strings.HasSuffix(ref, "@*") {
				continue // a prefix names an identity, not a version
			}
			if !closure[ref] {
				return policyErrorf("rule %q allows %q, which the composition closure does not declare", rule.ID, ref)
			}
		}
	}
	return nil
}

// validate rejects unknown selector values: a typo must not silently match
// nothing (or everything).
func (m Match) validate() error {
	for _, kind := range m.Kinds {
		if !oneOf(kind, KindAgent, KindSkill, KindTool, KindKnowledge, KindModelPolicy, KindEval) {
			return &selectorError{"kinds", string(kind)}
		}
	}
	for _, ref := range m.Refs {
		if strings.HasSuffix(ref, "@*") {
			if _, err := composition.ParseRef(strings.TrimSuffix(ref, "@*") + "@0.0.0"); err != nil {
				return &selectorError{"refs", ref}
			}
			continue
		}
		if _, err := composition.ParseRef(ref); err != nil {
			return &selectorError{"refs", ref}
		}
	}
	for _, trust := range m.TrustLevels {
		if !oneOf(trust, TrustPrivate, TrustCommunity, TrustVerified) {
			return &selectorError{"trustLevels", string(trust)}
		}
	}
	for _, effect := range m.SideEffects {
		if !oneOf(effect, SideEffectSafe, SideEffectExternal, SideEffectIrreversible) {
			return &selectorError{"sideEffects", string(effect)}
		}
	}
	for _, approval := range m.Approvals {
		if !oneOf(approval, ApprovalExternalMessage, ApprovalExternalWrite, ApprovalPayment, ApprovalHumanReview) {
			return &selectorError{"approvals", string(approval)}
		}
	}
	for _, scope := range m.Scopes {
		if !oneOf(scope, ScopeRun, ScopeConversation, ScopeAgent, ScopeTeam, ScopeTenant) {
			return &selectorError{"scopes", string(scope)}
		}
	}
	for _, mode := range m.ResolutionModes {
		if !oneOf(mode, ModeRegistry, ModeBundled, ModeOfflineCache) {
			return &selectorError{"resolutionModes", string(mode)}
		}
	}
	for _, permission := range m.Permissions {
		if strings.TrimSpace(permission) == "" || !strings.Contains(permission, ":") {
			return &selectorError{"permissions", permission}
		}
	}
	return nil
}

type selectorError struct {
	field string
	value string
}

func (e *selectorError) Error() string {
	return "unknown selector value " + e.value + " in " + e.field
}

// itoa is a tiny helper so the package does not pull in strconv for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
