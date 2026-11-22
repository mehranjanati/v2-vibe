package execution

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestFakePassesTheFullMatrix is the deliverable: the reference provider passes
// all eighteen cases, none skipped.
func TestFakePassesTheFullMatrix(t *testing.T) {
	report := Conformance(t, func() Harness { return NewFake() })
	t.Logf("conformance: %s", report)
	if !report.Accounted(totalCases) {
		t.Fatalf("report accounts for %d of %d cases (%d passed, %d skipped): %v",
			len(report.Passed)+len(report.Skipped), totalCases, len(report.Passed), len(report.Skipped), report.Skipped)
	}
	if len(report.Skipped) != 0 {
		t.Fatalf("the reference provider must pass every case, skipped: %v", report.Skipped)
	}
	for _, id := range []string{"C1", "C7", "C13", "C18"} {
		if !contains(report.Passed, id) {
			t.Errorf("%s did not run", id)
		}
	}
}

// TestPartialProviderSkipsWithReason proves a capability gap is reported, never
// silently passed: a provider advertising nothing runs 14 cases and reports the
// four capability-gated ones with a reason.
func TestPartialProviderSkipsWithReason(t *testing.T) {
	report := Conformance(t, func() Harness { return NewFake().WithCapabilities(Capabilities{}) })
	t.Logf("partial conformance: %s", report)
	if !report.Accounted(totalCases) {
		t.Fatalf("report accounts for fewer than %d cases", totalCases)
	}
	want := []string{"C13", "C14", "C15", "C17"}
	if strings.Join(report.Skipped, ",") != strings.Join(want, ",") {
		t.Errorf("skipped = %v, want %v", report.Skipped, want)
	}
	for _, id := range want {
		if !strings.Contains(report.Reasons[id], "does not advertise") {
			t.Errorf("%s skip reason = %q, want a capability explanation", id, report.Reasons[id])
		}
	}
}

// TestValidatePlanIsTheAdmissionGate covers the rules the runtime owns, so a
// provider never has to guess what a broken plan meant.
func TestValidatePlanIsTheAdmissionGate(t *testing.T) {
	cases := []struct {
		name string
		plan RunPlan
		want string
	}{
		{"no-tenant", RunPlan{ID: "p", Steps: []Step{{Name: "a"}}}, "no tenant"},
		{"no-steps", RunPlan{ID: "p", Tenant: "t"}, "no steps"},
		{"no-name", RunPlan{ID: "p", Tenant: "t", Steps: []Step{{}}}, "no name"},
		{"duplicate", RunPlan{ID: "p", Tenant: "t", Steps: []Step{{Name: "a"}, {Name: "a"}}}, "declares step"},
		{"unknown-dep", RunPlan{ID: "p", Tenant: "t", Steps: []Step{{Name: "a", DependsOn: []string{"ghost"}}}}, "unknown step"},
		{"self-dep", RunPlan{ID: "p", Tenant: "t", Steps: []Step{{Name: "a", DependsOn: []string{"a"}}}}, "depends on itself"},
		{"cycle", RunPlan{ID: "p", Tenant: "t", Steps: []Step{
			{Name: "a", DependsOn: []string{"b"}}, {Name: "b", DependsOn: []string{"a"}},
		}}, "cyclic"},
		{"negative-retry", RunPlan{ID: "p", Tenant: "t", Steps: []Step{{Name: "a", Retry: StepRetry{MaxAttempts: -1}}}}, "negative retry"},
		{"negative-timeout", RunPlan{ID: "p", Tenant: "t", Steps: []Step{{Name: "a", Timeout: -time.Second}}}, "negative timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePlan(tc.plan)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
			if Classify(err) != ErrAdmissionRefused {
				t.Errorf("class = %s, want admission-refused", Classify(err))
			}
		})
	}

	valid := twoStepPlan("ok")
	if err := ValidatePlan(valid); err != nil {
		t.Fatalf("ValidatePlan(valid) = %v", err)
	}
	if err := Admit(valid, Capabilities{}); err != nil {
		t.Fatalf("Admit without requirements should pass: %v", err)
	}
	valid.Requirements = []Capability{CapCancel, CapPause}
	if err := Admit(valid, Capabilities{Cancel: true}); err == nil || !strings.Contains(err.Error(), "pause") {
		t.Fatalf("Admit err = %v, want it to name pause", err)
	}
}

// TestOrderedStepsIsDependencyOrder proves providers share one ordering rule.
func TestOrderedStepsIsDependencyOrder(t *testing.T) {
	plan := RunPlan{ID: "p", Tenant: "t", Steps: []Step{
		{Name: "deploy", DependsOn: []string{"build", "test"}},
		{Name: "test", DependsOn: []string{"build"}},
		{Name: "build"},
		{Name: "lint"},
	}}
	ordered, err := orderedSteps(plan)
	if err != nil {
		t.Fatalf("orderedSteps: %v", err)
	}
	names := make([]string, 0, len(ordered))
	for _, step := range ordered {
		names = append(names, step.Name)
	}
	if got := strings.Join(names, ","); got != "build,lint,test,deploy" {
		t.Errorf("order = %s, want build,lint,test,deploy (stable for equal readiness)", got)
	}
}

// TestClassifyDefaultsToPermanent proves an unknown error is never retried
// forever.
func TestClassifyDefaultsToPermanent(t *testing.T) {
	if got := Classify(context.Canceled); got != ErrPermanent {
		t.Errorf("Classify(context.Canceled) = %s, want permanent (explicit classes only)", got)
	}
	if got := Classify(Transient("x")); got != ErrTransient {
		t.Errorf("Classify(Transient) = %s", got)
	}
	if !ErrTransient.Retryable() || ErrPermanent.Retryable() {
		t.Error("retryable flags are wrong")
	}
}

func contains(set []string, want string) bool {
	for _, s := range set {
		if s == want {
			return true
		}
	}
	return false
}
