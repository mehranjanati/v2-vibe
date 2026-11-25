package activation

import (
	"fmt"
	"sort"
	"strings"

	"backend/pkg/composition"
)

// defaultCandidateLimit mirrors the candidate bound the rest of the platform
// already uses (backend/pkg/toolsearch). A zero limit is not "unlimited": it is
// this bound, so a caller's omission cannot widen a run.
const defaultCandidateLimit = 8

// Decide is the whole engine: a pure function from the request to one decision
// record. It never reads a store, never mints a grant, never installs anything
// and never consults a clock of its own (decidedAt is injected).
func Decide(req Request) (Result, error) {
	ctx := req.Context
	if strings.TrimSpace(ctx.ContextID) == "" || strings.TrimSpace(ctx.SnapshotID) == "" || strings.TrimSpace(ctx.TenantKey) == "" {
		return Result{}, refusal(CodeContext, "contextId, snapshotId and tenantKey are required", nil)
	}
	limit := ctx.CandidateLimit
	if limit <= 0 {
		limit = defaultCandidateLimit
	}

	// The closure is the snapshot's declaration: the outermost set of the
	// invariant, and the only thing a policy or a candidate may draw from.
	closure := make(map[string]bool, len(req.Closure))
	for _, raw := range req.Closure {
		ref, err := composition.ParseRef(raw)
		if err != nil {
			return Result{}, policyErrorf("closure entry %q is not a pinned ref", raw)
		}
		closure[ref.String()] = true
	}
	if err := req.Policy.validate(closure); err != nil {
		return Result{}, err
	}

	// Grants arrive as an active snapshot, keyed by ref. A permission a rule
	// matches on must be in this set: a rule narrows a grant, never widens it.
	grants := make(map[string]map[string]bool, len(req.Grants))
	for _, grant := range req.Grants {
		ref, err := composition.ParseRef(grant.Ref)
		if err != nil {
			return Result{}, policyErrorf("grant ref %q is not a pinned ref", grant.Ref)
		}
		set := grants[ref.String()]
		if set == nil {
			set = map[string]bool{}
			grants[ref.String()] = set
		}
		for _, permission := range grant.Permissions {
			set[permission] = true
		}
	}

	candidates := append([]Facts(nil), req.Candidates...)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Ref < candidates[j].Ref })

	// Escalation is a security event: every offending ref is named and audited,
	// and the run is refused before a single decision is emitted.
	var escalation []AuditEvent
	seen := map[string]bool{}
	for _, candidate := range candidates {
		ref, err := composition.ParseRef(candidate.Ref)
		if err != nil {
			return Result{}, policyErrorf("candidate ref %q is not a pinned ref", candidate.Ref)
		}
		if seen[ref.String()] {
			return Result{}, refusal(CodeContext, fmt.Sprintf("candidate %q appears twice", ref.String()), nil)
		}
		seen[ref.String()] = true
		if !closure[ref.String()] {
			escalation = append(escalation, AuditEvent{
				Code:      CodeEscalation,
				TenantKey: ctx.TenantKey,
				RunID:     req.RunID,
				Ref:       ref.String(),
				Detail:    "candidate is outside the composition closure",
			})
		}
	}
	if len(escalation) > 0 {
		return Result{}, refusal(CodeEscalation, "one or more candidates are outside the composition closure", escalation)
	}

	decided, eligible, allowRules, requiredClasses := evaluate(candidates, grants, req)
	ordered := orderByRanking(eligible, req.Ranking)
	activated := make([]string, 0, len(ordered))
	for index, ref := range ordered {
		switch {
		case req.Policy.MaxCandidates > 0 && index >= req.Policy.MaxCandidates:
			decided = append(decided, Decision{
				Candidate: ref, Decision: DecisionSkipped, Reason: ReasonBudget, Stage: StageBudget,
			})
		case index >= limit:
			decided = append(decided, Decision{
				Candidate: ref, Decision: DecisionSkipped, Reason: ReasonBound, Stage: StageBound,
			})
		default:
			activated = append(activated, ref)
			decided = append(decided, Decision{
				Candidate: ref, Decision: DecisionActivated, RuleID: allowRules[ref],
				Stage: StageEligibility, ApprovalRequired: requiredClasses[ref],
			})
		}
	}

	record := Record{
		ActivationPolicyRef: req.Policy.Ref,
		CompositionDigest:   ctx.SnapshotID,
		ContextID:           ctx.ContextID,
		DecidedAt:           formatDecidedAt(req.DecidedAt),
		Decisions:           sortDecisions(decided),
		Ranking:             rankingInfo(req.Ranking),
		RunID:               req.RunID,
		TenantKey:           ctx.TenantKey,
	}
	sort.Strings(eligible)

	if len(activated) == 0 && req.Policy.MinCapabilities > 0 {
		err := refusal(CodeBudget, "eligibility left no capability while minCapabilities > 0", nil)
		err.Record = &record
		return Result{Record: record, Eligible: eligible}, err
	}
	return Result{Record: record, Activated: activated, Eligible: eligible}, nil
}
