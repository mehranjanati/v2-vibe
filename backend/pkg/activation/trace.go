package activation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"backend/pkg/composition"
)

// Trace storage failures. They are named separately from the decision codes
// because they describe the *store*, not the decision: a record can be a
// perfectly good decision and still be refused by a store that already holds a
// different one for the same run.
var (
	// ErrTraceInvalid means the record violates the record contract (§5): a
	// decision that cannot be explained cannot be persisted, because storing it
	// would imply the question "why?" has an answer.
	ErrTraceInvalid = errors.New("activation: the decision record is not attributable")
	// ErrTraceMissing means the run has no stored trace.
	ErrTraceMissing = errors.New("activation: no decision trace is stored for this run")
	// ErrTraceImmutable means a different record already exists for the run. A
	// trace is append-only: a re-decision is a new run, not a rewritten row.
	ErrTraceImmutable = errors.New("activation: a stored decision trace cannot be rewritten")
	// ErrTraceTenant means the run id is already recorded for another tenant.
	ErrTraceTenant = errors.New("activation: the decision trace is recorded for another tenant")
	// ErrTraceCorrupt means a stored row no longer matches its own digest, so it
	// cannot be trusted as an explanation.
	ErrTraceCorrupt = errors.New("activation: the stored decision trace does not match its digest")
)

// TraceStore is the persistence seam for the decision record (ADI-CTX-004).
//
// It is an interface in the pure package for the same reason SnapshotStore is:
// the eligibility core keeps importing only `backend/pkg/ir`,
// `backend/pkg/composition` and the stdlib, while the adapter lives in
// activation/d1store. One record per run, append-only.
//
//   - Persist of an identical record is a no-op that returns nil; the first
//     write wins, including its `decidedAt`.
//   - Persist of a *different* record under an existing run id must return
//     ErrTraceImmutable, never overwrite.
//   - Persist of the same run id for another tenant must return ErrTraceTenant.
//   - Load of an unknown run returns ErrTraceMissing.
//   - Load of a row that no longer matches its digest returns ErrTraceCorrupt.
//
// Nothing in the platform calls this yet: no assembler supplies candidates
// (ADI-CTX-001) and no policy source exists, so the store is the seam a caller
// will use, not a capability surface.
type TraceStore interface {
	Persist(ctx context.Context, record Record) error
	Load(ctx context.Context, runID string) (*Record, error)
}

// ValidateRecord enforces the record contract of activation-policy.md §5 before
// anything is written. It refuses an unattributable decision — an `activated`
// without the rule that allowed it, or a `skipped`/`denied` without a reason —
// because such a record would make "why was it not used?" unanswerable while
// looking like an answer.
func ValidateRecord(record Record) error {
	fail := func(format string, args ...interface{}) error {
		return fmt.Errorf("%w: %s", ErrTraceInvalid, fmt.Sprintf(format, args...))
	}
	if strings.TrimSpace(record.RunID) == "" {
		return fail("runId is required: a trace that belongs to no run cannot be found again")
	}
	if strings.TrimSpace(record.TenantKey) == "" {
		return fail("tenantKey is required: a trace is never shared across tenants")
	}
	if strings.TrimSpace(record.ContextID) == "" {
		return fail("contextId is required: a decision that cites no context cannot be reproduced")
	}
	if strings.TrimSpace(record.CompositionDigest) == "" {
		return fail("compositionDigest is required: without the snapshot the decision has no closure")
	}
	if !policyRefRe.MatchString(record.ActivationPolicyRef) {
		return fail("activationPolicyRef %q is not activation-policy/<name>@MAJOR.MINOR.PATCH", record.ActivationPolicyRef)
	}
	if strings.TrimSpace(record.Ranking.Source) == "" {
		return fail("ranking.source is required; \"none\" is a value, empty is not")
	}
	if record.DecidedAt != "" {
		if _, err := time.Parse(time.RFC3339, record.DecidedAt); err != nil {
			return fail("decidedAt %q is not RFC 3339", record.DecidedAt)
		}
	}
	if !sort.SliceIsSorted(record.Decisions, func(i, j int) bool {
		return record.Decisions[i].Candidate < record.Decisions[j].Candidate
	}) {
		return fail("decisions must be sorted by candidate")
	}

	previous := ""
	for index, decision := range record.Decisions {
		where := fmt.Sprintf("decisions[%d] (%s)", index, decision.Candidate)
		if _, err := composition.ParseRef(decision.Candidate); err != nil {
			return fail("%s: candidate is not a pinned ref", where)
		}
		if decision.Candidate == previous {
			return fail("%s: appears twice; one entry per candidate", where)
		}
		previous = decision.Candidate
		if !oneOf(decision.Stage, StageEligibility, StageBudget, StageBound) {
			return fail("%s: stage %q is not eligibility|budget|bound", where, decision.Stage)
		}
		switch decision.Decision {
		case DecisionActivated:
			if decision.Stage != StageEligibility {
				return fail("%s: an activated entry is decided at eligibility, not %q", where, decision.Stage)
			}
			if strings.TrimSpace(decision.RuleID) == "" {
				return fail("%s: activated without a ruleId; every activation must cite the rule that allowed it", where)
			}
			if strings.TrimSpace(decision.Reason) != "" {
				return fail("%s: activated carries a reason; only non-activation entries explain a refusal", where)
			}
		case DecisionDenied:
			if decision.Stage != StageEligibility {
				return fail("%s: a denied entry is decided at eligibility, not %q", where, decision.Stage)
			}
			if strings.TrimSpace(decision.RuleID) == "" || strings.TrimSpace(decision.Reason) == "" {
				return fail("%s: denied must cite the deny rule and its reason", where)
			}
		case DecisionSkipped:
			if decision.Stage == StageEligibility && !oneOf(decision.Reason, ReasonNoAllow, ReasonBudget, ReasonBound) {
				return fail("%s: skipped reason %q is not %s|%s|%s", where, decision.Reason,
					ReasonNoAllow, ReasonBudget, ReasonBound)
			}
			if decision.Stage == StageBudget && decision.Reason != ReasonBudget {
				return fail("%s: stage budget requires reason %s", where, ReasonBudget)
			}
			if decision.Stage == StageBound && decision.Reason != ReasonBound {
				return fail("%s: stage bound requires reason %s", where, ReasonBound)
			}
			if strings.TrimSpace(decision.RuleID) != "" {
				return fail("%s: a skipped entry cites no rule; skipped means no allow matched", where)
			}
		default:
			return fail("%s: decision %q is not activated|skipped|denied", where, decision.Decision)
		}
		for _, class := range decision.ApprovalRequired {
			if !oneOf(class, ApprovalExternalMessage, ApprovalExternalWrite, ApprovalPayment, ApprovalHumanReview) {
				return fail("%s: approvalRequired carries %q, which is not an approval class", where, class)
			}
		}
	}
	return nil
}
