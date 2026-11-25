package activation

// ADI-CTX-004 — the persisted decision record against its own contract.
//
// Two things are proved here: a record the engine actually emits is storable,
// and a record that cannot explain itself is refused *before* any store sees it.

import (
	"errors"
	"testing"
	"time"
)

// traceBaseRecord is a decided record that carries all three outcomes
// (activated, skipped/budget, denied), so every mutation below has a target.
func traceBaseRecord(t *testing.T) Record {
	t.Helper()
	request := baseRequest()
	request.Policy.MaxCandidates = 1
	result, err := Decide(request)
	if err != nil {
		t.Fatalf("base decision failed: %v", err)
	}
	return result.Record
}

// TestTraceAcceptsWhatTheEngineEmits is the positive control: the record of a
// real decision, and the record of a real refusal, both satisfy the store
// contract. A validator that rejected its own engine's output would be useless.
func TestTraceAcceptsWhatTheEngineEmits(t *testing.T) {
	result, err := Decide(baseRequest())
	if err != nil {
		t.Fatalf("base request must decide cleanly: %v", err)
	}
	if err := ValidateRecord(result.Record); err != nil {
		t.Fatalf("a real decision record must be storable: %v", err)
	}
	if err := ValidateRecord(traceBaseRecord(t)); err != nil {
		t.Fatalf("a record with skipped entries must be storable: %v", err)
	}

	budget := baseRequest()
	budget.Policy.MinCapabilities = 1
	budget.Policy.Rules = nil // nothing allows: default-deny leaves eligibility empty
	_, refusalErr := Decide(budget)
	var activationErr *Error
	if !errors.As(refusalErr, &activationErr) || activationErr.Record == nil {
		t.Fatalf("expected a budget refusal carrying its record, got %v", refusalErr)
	}
	if err := ValidateRecord(*activationErr.Record); err != nil {
		t.Fatalf("a refusal record must be storable too — \"why did it stop\" deserves the same trace: %v", err)
	}
}

// TestTraceRefusesUnattributableDecisions is the point of the contract: a stored
// record is an explanation, so an entry that explains nothing is refused.
func TestTraceRefusesUnattributableDecisions(t *testing.T) {
	base := func() Record { return traceBaseRecord(t) }
	cases := []struct {
		name   string
		mutate func(*Record)
	}{
		{"activated without a ruleId", func(r *Record) {
			for i := range r.Decisions {
				if r.Decisions[i].Decision == DecisionActivated {
					r.Decisions[i].RuleID = ""
				}
			}
		}},
		{"activated carrying a reason", func(r *Record) {
			for i := range r.Decisions {
				if r.Decisions[i].Decision == DecisionActivated {
					r.Decisions[i].Reason = "because"
				}
			}
		}},
		{"skipped without a reason", func(r *Record) {
			for i := range r.Decisions {
				if r.Decisions[i].Decision == DecisionSkipped {
					r.Decisions[i].Reason = ""
				}
			}
		}},
		{"skipped with an open-ended reason", func(r *Record) {
			for i := range r.Decisions {
				if r.Decisions[i].Decision == DecisionSkipped {
					r.Decisions[i].Reason = "not today"
				}
			}
		}},
		{"skipped citing a rule", func(r *Record) {
			for i := range r.Decisions {
				if r.Decisions[i].Decision == DecisionSkipped {
					r.Decisions[i].RuleID = "allow-tools"
				}
			}
		}},
		{"denied without its rule", func(r *Record) {
			for i := range r.Decisions {
				if r.Decisions[i].Decision == DecisionDenied {
					r.Decisions[i].RuleID = ""
				}
			}
		}},
		{"unknown decision kind", func(r *Record) {
			r.Decisions[0].Decision = "maybe"
		}},
		{"unknown stage", func(r *Record) {
			r.Decisions[0].Stage = "later"
		}},
		{"budget stage without a budget reason", func(r *Record) {
			r.Decisions[0].Decision = DecisionSkipped
			r.Decisions[0].RuleID = ""
			r.Decisions[0].Stage = StageBudget
			r.Decisions[0].Reason = ReasonBound
		}},
		{"duplicate candidate", func(r *Record) {
			r.Decisions = append(r.Decisions, r.Decisions[0])
		}},
		{"unsorted decisions", func(r *Record) {
			if len(r.Decisions) < 2 {
				t.Fatalf("the base record needs two decisions to test ordering")
			}
			r.Decisions[0], r.Decisions[1] = r.Decisions[1], r.Decisions[0]
		}},
		{"unpinned candidate", func(r *Record) {
			r.Decisions[0].Candidate = "tool/lookup"
		}},
		{"unknown approval class", func(r *Record) {
			for i := range r.Decisions {
				if len(r.Decisions[i].ApprovalRequired) > 0 {
					r.Decisions[i].ApprovalRequired = []ApprovalClass{"rubber-stamp"}
					return
				}
			}
			r.Decisions[0].ApprovalRequired = []ApprovalClass{"rubber-stamp"}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			record := base()
			testCase.mutate(&record)
			err := ValidateRecord(record)
			if !errors.Is(err, ErrTraceInvalid) {
				t.Fatalf("expected ErrTraceInvalid, got %v", err)
			}
		})
	}
}

// TestTraceRefusesUnknownIdentity: a record whose run, tenant, context, snapshot
// or policy is unnameable cannot be found again, so it is refused.
func TestTraceRefusesUnknownIdentity(t *testing.T) {
	base := func() Record { return traceBaseRecord(t) }
	cases := map[string]func(*Record){
		"runId":             func(r *Record) { r.RunID = " " },
		"tenantKey":         func(r *Record) { r.TenantKey = "" },
		"contextId":         func(r *Record) { r.ContextID = "" },
		"compositionDigest": func(r *Record) { r.CompositionDigest = "" },
		"activationPolicyRef": func(r *Record) {
			r.ActivationPolicyRef = "activation-policy/test" // floating, not pinned
		},
		"ranking.source": func(r *Record) { r.Ranking.Source = "" },
		"decidedAt":      func(r *Record) { r.DecidedAt = "yesterday" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			record := base()
			mutate(&record)
			if err := ValidateRecord(record); !errors.Is(err, ErrTraceInvalid) {
				t.Fatalf("expected ErrTraceInvalid for %s, got %v", name, err)
			}
		})
	}
}

// TestTraceDigestIgnoresTheClock pins the one allowance of the record: two runs
// of the same inputs at different times are the same decision, so their digest
// is equal, while a changed decision is a different artifact.
func TestTraceDigestIgnoresTheClock(t *testing.T) {
	first, err := Decide(baseRequest())
	if err != nil {
		t.Fatalf("first decision failed: %v", err)
	}
	later := baseRequest()
	later.DecidedAt = testTime.Add(48 * time.Hour)
	second, err := Decide(later)
	if err != nil {
		t.Fatalf("second decision failed: %v", err)
	}
	firstDigest, err := first.Record.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	secondDigest, err := second.Record.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if firstDigest != secondDigest {
		t.Fatalf("decidedAt must be excluded from the digest: %s != %s", firstDigest, secondDigest)
	}

	changed := baseRequest()
	changed.Policy.MaxCandidates = 1
	third, _ := Decide(changed)
	thirdDigest, err := third.Record.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if thirdDigest == firstDigest {
		t.Fatal("a changed decision must have a different digest")
	}
}
