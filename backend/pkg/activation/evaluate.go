package activation

import "time"

import "sort"

// evaluate runs stage A over candidates already known to be inside the
// closure. It returns the eligibility decisions, the (unsorted) eligible refs,
// the allow rule id each eligible ref carries, and the approval preconditions
// each carries. It never trims and never checks the boundary.
func evaluate(candidates []Facts, grants map[string]map[string]bool, req Request) ([]Decision, []string, map[string]string, map[string][]ApprovalClass) {
	decisions := make([]Decision, 0, len(candidates))
	eligible := make([]string, 0, len(candidates))
	allowRules := map[string]string{}
	requiredClasses := map[string][]ApprovalClass{}

	for _, candidate := range candidates {
		var denies, allows []Rule
		for _, rule := range req.Policy.Rules {
			if !rule.matches(candidate, grants[candidate.Ref]) {
				continue
			}
			if rule.Effect == EffectDeny {
				denies = append(denies, rule)
			} else {
				allows = append(allows, rule)
			}
		}
		if len(denies) > 0 {
			// Deny wins whenever both match; the tie-break (smallest rule id)
			// only makes the *citation* deterministic, not the outcome.
			sort.Slice(denies, func(i, j int) bool { return denies[i].ID < denies[j].ID })
			rule := denies[0]
			decisions = append(decisions, Decision{
				Candidate: candidate.Ref, Decision: DecisionDenied,
				RuleID: rule.ID, Reason: rule.Reason, Stage: StageEligibility,
			})
			continue
		}
		if len(allows) == 0 {
			// Default-deny: silence is never permission.
			decisions = append(decisions, Decision{
				Candidate: candidate.Ref, Decision: DecisionSkipped,
				Reason: ReasonNoAllow, Stage: StageEligibility,
			})
			continue
		}
		sort.Slice(allows, func(i, j int) bool { return allows[i].ID < allows[j].ID })
		eligible = append(eligible, candidate.Ref)
		allowRules[candidate.Ref] = allows[0].ID
		classes := append([]ApprovalClass(nil), candidate.Approvals...)
		for _, rule := range allows {
			classes = append(classes, rule.Match.Approvals...)
		}
		if candidate.RequiresHuman {
			classes = append(classes, ApprovalHumanReview)
		}
		if len(classes) > 0 {
			requiredClasses[candidate.Ref] = uniqueApprovals(classes)
		}
	}
	return decisions, eligible, allowRules, requiredClasses
}

// Request is the whole input of one decision. Rule ids, tenant, closure and
// grants are caller-supplied; the engine resolves or fetches nothing.
type Request struct {
	RunID      string   `json:"runId"`
	Context    Context  `json:"context"`
	Policy     Policy   `json:"policy"`
	Closure    []string `json:"closure"`
	Candidates []Facts  `json:"candidates"`
	Grants     []Grant  `json:"grants,omitempty"`
	Ranking    *Ranking `json:"ranking,omitempty"`
	// DecidedAt is the injected clock (a zero value renders no timestamp).
	// The package never calls time.Now: the clock always comes from the caller.
	DecidedAt time.Time `json:"-"`
}
