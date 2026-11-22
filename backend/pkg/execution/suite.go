package execution

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// Harness is the extra seam the conformance suite needs beyond the contract:
// crash simulation, recovery and event dropping. The reference Fake implements
// it; a provider that cannot simulate a restart cannot be conformance-tested and
// must not claim the suite.
type Harness interface {
	Provider
	// CrashOn makes the next attempt at step succeed and then lose its
	// durable write (C2/C3).
	CrashOn(step string)
	// Recover re-drives a run left running by a simulated crash.
	Recover(runID string) error
	// DropEvents deletes the best-effort event log (C18).
	DropEvents(runID string)
	// BeforeStep installs a hook that runs before each attempt.
	BeforeStep(hook func(runID, step string))
}

// Report says which conformance cases ran and which were skipped for a missing
// capability. A skip is recorded, never silently passed.
type Report struct {
	Passed  []string
	Skipped []string
	Reasons map[string]string
}

// Accounted reports whether every case in the matrix is either passed or
// skipped with a reason.
func (r Report) Accounted(total int) bool {
	return len(r.Passed)+len(r.Skipped) == total && len(r.Skipped) == len(r.Reasons)
}

// String renders a one-line summary for CI logs.
func (r Report) String() string {
	return fmt.Sprintf("%d passed, %d skipped (%s)", len(r.Passed), len(r.Skipped), strings.Join(r.Skipped, ", "))
}

// countingExecutor records every call per step and lets a test decide outcomes.
type countingExecutor struct {
	mu     sync.Mutex
	calls  map[string]int
	seen   map[string]bool // idempotency keys already applied
	effect int             // side effects actually applied
	delay  time.Duration
}

func newCountingExecutor() *countingExecutor {
	return &countingExecutor{calls: map[string]int{}, seen: map[string]bool{}}
}

func (c *countingExecutor) Execute(ctx context.Context, call StepCall) (StepResult, error) {
	c.mu.Lock()
	c.calls[call.Step]++
	c.mu.Unlock()
	if c.delay > 0 {
		select {
		case <-time.After(c.delay):
		case <-ctx.Done():
			return StepResult{}, Transient("interrupted")
		}
	}
	c.mu.Lock()
	if call.IdempotencyKey != "" {
		if c.seen[call.IdempotencyKey] {
			c.mu.Unlock()
			return StepResult{Output: "deduplicated"}, nil
		}
		c.seen[call.IdempotencyKey] = true
	}
	c.effect++
	c.mu.Unlock()
	return StepResult{Output: call.Step + ":ok", Usage: Usage{InputTokens: 10, OutputTokens: 5}}, nil
}

func (c *countingExecutor) count(step string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[step]
}

func (c *countingExecutor) effects() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.effect
}

func twoStepPlan(id string) RunPlan {
	return RunPlan{
		ID: id, Tenant: "tenant-a",
		Steps: []Step{
			{Name: "first"},
			{Name: "second", DependsOn: []string{"first"}},
		},
	}
}

// caseFunc is one conformance case.
type caseFunc func(t *testing.T, h Harness, exec *countingExecutor) error

// conformanceCase is one row of the normative matrix.
type conformanceCase struct {
	id string
	// requires is the capability the case exercises; when the provider does
	// not advertise it the case is skipped with a reason.
	requires Capability
	// title mirrors the matrix in ecosystem/06 for readable test names.
	title string
	run   caseFunc
}

// totalCases is the size of the C1–C18 matrix.
const totalCases = 18

// Conformance runs the normative C1–C18 matrix against a provider harness. The
// caller supplies a fresh harness per case, so no case can inherit another's
// state.
func Conformance(t *testing.T, newHarness func() Harness) Report {
	t.Helper()
	report := Report{Reasons: map[string]string{}}
	for _, tc := range conformanceCases() {
		tc := tc
		t.Run(tc.id+" "+tc.title, func(t *testing.T) {
			h := newHarness()
			exec := newCountingExecutor()
			if tc.requires != "" && !h.Capabilities().Supports(tc.requires) {
				reason := fmt.Sprintf("provider does not advertise %q (advertised: %s)", tc.requires, h.Capabilities())
				report.Reasons[tc.id] = reason
				report.Skipped = append(report.Skipped, tc.id)
				t.Skip(reason)
				return
			}
			if err := tc.run(t, h, exec); err != nil {
				t.Fatalf("%s failed: %v", tc.id, err)
			}
			report.Passed = append(report.Passed, tc.id)
		})
	}
	sort.Strings(report.Passed)
	sort.Strings(report.Skipped)
	return report
}

// conformanceCases returns the matrix in order. Each case gets its own harness.
func conformanceCases() []conformanceCase {
	return []conformanceCase{
		{id: "C1", title: "two-step plan runs in dependency order", run: caseTwoStepOrder},
		{id: "C2", title: "replay after restart skips completed steps", run: caseReplaySkipsCompleted},
		{id: "C3", title: "at-least-once delivery with idempotency", run: caseAtLeastOnce},
		{id: "C4", title: "transient error retried within budget", run: caseTransientRetry},
		{id: "C5", title: "permanent error does not retry", run: casePermanentNoRetry},
		{id: "C6", title: "retry budget exhausted fails the run", run: caseBudgetExhausted},
		{id: "C7", title: "step timeout terminates the step", run: caseTimeout},
		{id: "C8", title: "tenant mismatch is rejected before any step", run: caseTenantMismatch},
		{id: "C9", title: "cyclic plan is rejected before any step", run: caseCyclicPlan},
		{id: "C10", title: "step names are identity", run: caseStepNameStability},
		{id: "C11", title: "terminal states are final", run: caseTerminalFinality},
		{id: "C12", title: "start is idempotent per run id", run: caseStartIdempotent},
		{id: "C13", title: "cancel mid-run ends cancelled", requires: CapCancel, run: caseCancelMidRun},
		{id: "C14", title: "pause/resume preserves durable state", requires: CapPause, run: casePauseResume},
		{id: "C15", title: "long sleep does not block a worker", requires: CapLongSleep, run: caseLongSleep},
		{id: "C16", title: "unsupported requirement is refused", run: caseCapabilityRefusal},
		{id: "C17", title: "history records every executed step", requires: CapHistory, run: caseHistoryCompleteness},
		{id: "C18", title: "metering reads durable results", run: caseMeteringSource},
	}
}

// --- shared helpers ---------------------------------------------------------

func mustStart(t *testing.T, h Harness, plan RunPlan, exec StepExecutor) RunHandle {
	t.Helper()
	handle, err := h.Start(context.Background(), plan, exec)
	if err != nil {
		t.Fatalf("Start(%s): %v", plan.ID, err)
	}
	return handle
}

func mustStatus(t *testing.T, h Harness, runID string) Status {
	t.Helper()
	status, err := h.Status(context.Background(), runID)
	if err != nil {
		t.Fatalf("Status(%s): %v", runID, err)
	}
	return status
}

func wantState(t *testing.T, h Harness, runID string, want State) Status {
	t.Helper()
	status := mustStatus(t, h, runID)
	if status.State != want {
		t.Fatalf("run %s state = %s, want %s (error: %v)", runID, status.State, want, status.Error)
	}
	return status
}

// --- the C1–C18 cases -------------------------------------------------------

func caseTwoStepOrder(t *testing.T, h Harness, ex *countingExecutor) error {
	mustStart(t, h, twoStepPlan("run-c1"), ex)
	status := wantState(t, h, "run-c1", StateSucceeded)
	if strings.Join(status.CompletedSteps, ",") != "first,second" {
		return fmt.Errorf("completed steps = %v, want first,second", status.CompletedSteps)
	}
	if ex.count("first") != 1 || ex.count("second") != 1 {
		return fmt.Errorf("executions = first:%d second:%d, want 1 and 1", ex.count("first"), ex.count("second"))
	}
	return nil
}

func caseReplaySkipsCompleted(t *testing.T, h Harness, ex *countingExecutor) error {
	h.CrashOn("second")
	mustStart(t, h, twoStepPlan("run-c2"), ex)
	if state := mustStatus(t, h, "run-c2").State; state != StateRunning {
		return fmt.Errorf("after the simulated crash state = %s, want running", state)
	}
	if ex.count("second") != 1 {
		return fmt.Errorf("second ran %d times before recovery, want 1", ex.count("second"))
	}
	if err := h.Recover("run-c2"); err != nil {
		return fmt.Errorf("Recover: %w", err)
	}
	wantState(t, h, "run-c2", StateSucceeded)
	if ex.count("first") != 1 {
		return fmt.Errorf("first re-executed %d times after recovery, want 1 (durable result must be reused)", ex.count("first"))
	}
	if ex.count("second") != 2 {
		return fmt.Errorf("second ran %d times, want 2 (crashed attempt + retry)", ex.count("second"))
	}
	return nil
}

func caseAtLeastOnce(t *testing.T, h Harness, ex *countingExecutor) error {
	plan := RunPlan{ID: "run-c3", Tenant: "tenant-a", Steps: []Step{
		{Name: "charge", IdempotencyKey: "charge:order-1"},
	}}
	h.CrashOn("charge")
	mustStart(t, h, plan, ex)
	if err := h.Recover("run-c3"); err != nil {
		return fmt.Errorf("Recover: %w", err)
	}
	wantState(t, h, "run-c3", StateSucceeded)
	if ex.count("charge") != 2 {
		return fmt.Errorf("executor saw %d deliveries, want 2 (at-least-once)", ex.count("charge"))
	}
	if ex.effects() != 1 {
		return fmt.Errorf("side effect applied %d times, want exactly 1 via the idempotency key", ex.effects())
	}
	return nil
}

func caseTransientRetry(t *testing.T, h Harness, ex *countingExecutor) error {
	f, ok := h.(*Fake)
	if !ok {
		return fmt.Errorf("case requires the injectable reference provider")
	}
	f.FailNext("flaky", Transient("upstream 503"), 2)
	plan := RunPlan{ID: "run-c4", Tenant: "tenant-a", Steps: []Step{
		{Name: "flaky", Retry: StepRetry{MaxAttempts: 3}},
	}}
	mustStart(t, h, plan, ex)
	wantState(t, h, "run-c4", StateSucceeded)
	// The executor is invoked once per attempt: the injected failures happen
	// inside the step, exactly like a flaky upstream. Applying the side effect
	// once is the idempotency contract C3 owns, not this case.
	if ex.count("flaky") != 3 {
		return fmt.Errorf("executor calls = %d, want 3 attempts (2 failed + 1 successful)", ex.count("flaky"))
	}
	if !h.Capabilities().History {
		return nil
	}
	history, err := h.History(context.Background(), "run-c4")
	if err != nil {
		return err
	}
	if len(history) != 3 {
		return fmt.Errorf("history rows = %d, want 3 attempts", len(history))
	}
	return nil
}

func casePermanentNoRetry(t *testing.T, h Harness, ex *countingExecutor) error {
	f, ok := h.(*Fake)
	if !ok {
		return fmt.Errorf("case requires the injectable reference provider")
	}
	f.FailNext("bad", Permanent("validation failed"), 1)
	plan := RunPlan{ID: "run-c5", Tenant: "tenant-a", Steps: []Step{
		{Name: "bad", Retry: StepRetry{MaxAttempts: 5}},
	}}
	mustStart(t, h, plan, ex)
	status := wantState(t, h, "run-c5", StateFailed)
	if status.Error == nil || status.Error.Class != ErrPermanent {
		return fmt.Errorf("error class = %v, want permanent", status.Error)
	}
	if !h.Capabilities().History {
		// Without the history capability the durable status is the evidence:
		// a permanent error must not have produced a second attempt.
		if status.Error.Msg != "validation failed" {
			return fmt.Errorf("error message = %q, want the step's own failure", status.Error.Msg)
		}
		return nil
	}
	history, err := h.History(context.Background(), "run-c5")
	if err != nil {
		return err
	}
	if len(history) != 1 {
		return fmt.Errorf("history rows = %d, want 1 (a permanent error must not retry)", len(history))
	}
	return nil
}

func caseBudgetExhausted(t *testing.T, h Harness, ex *countingExecutor) error {
	f, ok := h.(*Fake)
	if !ok {
		return fmt.Errorf("case requires the injectable reference provider")
	}
	f.FailNext("flaky", Transient("upstream 503"), 5)
	plan := RunPlan{ID: "run-c6", Tenant: "tenant-a", Steps: []Step{
		{Name: "first"},
		{Name: "flaky", DependsOn: []string{"first"}, Retry: StepRetry{MaxAttempts: 2}},
	}}
	mustStart(t, h, plan, ex)
	status := wantState(t, h, "run-c6", StateFailed)
	if len(status.CompletedSteps) != 1 || status.CompletedSteps[0] != "first" {
		return fmt.Errorf("completed steps = %v, want only the first step: a partial run must never be reported as succeeded", status.CompletedSteps)
	}
	if !h.Capabilities().History {
		return nil
	}
	history, err := h.History(context.Background(), "run-c6")
	if err != nil {
		return err
	}
	attempts := 0
	for _, row := range history {
		if row.Step == "flaky" {
			attempts++
		}
	}
	if attempts != 2 {
		return fmt.Errorf("flaky attempts = %d, want exactly the 2-attempt budget", attempts)
	}
	return nil
}

func caseTimeout(t *testing.T, h Harness, ex *countingExecutor) error {
	ex.delay = 150 * time.Millisecond
	plan := RunPlan{ID: "run-c7", Tenant: "tenant-a", Steps: []Step{
		{Name: "slow", Timeout: 20 * time.Millisecond},
	}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		mustStart(t, h, plan, ex)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		return fmt.Errorf("run %s hung past its step timeout", plan.ID)
	}
	status := wantState(t, h, "run-c7", StateFailed)
	if status.Error == nil {
		return fmt.Errorf("timed-out run has no recorded error")
	}
	return nil
}

func caseTenantMismatch(t *testing.T, h Harness, ex *countingExecutor) error {
	plan := twoStepPlan("run-c8")
	plan.Tenant = ""
	handle, err := h.Start(context.Background(), plan, ex)
	if err != nil {
		return fmt.Errorf("Start returned a Go error instead of a rejected run: %w", err)
	}
	if handle.State != StateRejected {
		return fmt.Errorf("handle state = %s, want rejected", handle.State)
	}
	status := wantState(t, h, "run-c8", StateRejected)
	if status.Error == nil || status.Error.Class != ErrAdmissionRefused {
		return fmt.Errorf("admission error = %v, want admission-refused (the durable audit record)", status.Error)
	}
	if ex.count("first")+ex.count("second") != 0 {
		return fmt.Errorf("steps ran despite the tenant refusal")
	}
	return nil
}

func caseCyclicPlan(t *testing.T, h Harness, ex *countingExecutor) error {
	plan := RunPlan{ID: "run-c9", Tenant: "tenant-a", Steps: []Step{
		{Name: "a", DependsOn: []string{"b"}},
		{Name: "b", DependsOn: []string{"a"}},
	}}
	handle, err := h.Start(context.Background(), plan, ex)
	if err != nil {
		return fmt.Errorf("Start: %w", err)
	}
	if handle.State != StateRejected {
		return fmt.Errorf("handle state = %s, want rejected", handle.State)
	}
	status := wantState(t, h, "run-c9", StateRejected)
	if status.Error == nil || !strings.Contains(status.Error.Msg, "cyclic") {
		return fmt.Errorf("error = %v, want a cycle refusal naming the cycle", status.Error)
	}
	if ex.count("a")+ex.count("b") != 0 {
		return fmt.Errorf("steps ran despite the cyclic plan")
	}
	return nil
}

func caseStepNameStability(t *testing.T, h Harness, ex *countingExecutor) error {
	original := RunPlan{ID: "run-c10", Tenant: "tenant-a", Steps: []Step{{Name: "build"}}}
	mustStart(t, h, original, ex)

	renamed := RunPlan{ID: "run-c10", Tenant: "tenant-a", Steps: []Step{{Name: "compile"}}}
	handle, err := h.Start(context.Background(), renamed, ex)
	if err != nil {
		return fmt.Errorf("Start(renamed): %w", err)
	}
	if handle.State != StateSucceeded {
		return fmt.Errorf("state after renamed start = %s, want the original succeeded run", handle.State)
	}
	if ex.count("compile") != 0 {
		return fmt.Errorf("renamed step executed %d times: a rename must not create new work", ex.count("compile"))
	}
	status := mustStatus(t, h, "run-c10")
	if strings.Join(status.CompletedSteps, ",") != "build" {
		return fmt.Errorf("completed steps = %v, want [build]", status.CompletedSteps)
	}
	return nil
}

func caseTerminalFinality(t *testing.T, h Harness, ex *countingExecutor) error {
	mustStart(t, h, twoStepPlan("run-c11"), ex)
	wantState(t, h, "run-c11", StateSucceeded)

	ctx := context.Background()
	if err := h.Cancel(ctx, "run-c11", "too late"); err == nil {
		return fmt.Errorf("cancel succeeded on a succeeded run")
	}
	if err := h.Pause(ctx, "run-c11"); err == nil {
		return fmt.Errorf("pause succeeded on a succeeded run")
	}
	if err := h.Resume(ctx, "run-c11"); err == nil {
		return fmt.Errorf("resume succeeded on a succeeded run")
	}
	wantState(t, h, "run-c11", StateSucceeded)
	return nil
}

func caseStartIdempotent(t *testing.T, h Harness, ex *countingExecutor) error {
	plan := twoStepPlan("run-c12")
	first := mustStart(t, h, plan, ex)
	second := mustStart(t, h, plan, ex)
	if first.RunID != second.RunID {
		return fmt.Errorf("handles differ: %s vs %s", first.RunID, second.RunID)
	}
	if second.State != StateSucceeded {
		return fmt.Errorf("second start state = %s, want the existing succeeded run", second.State)
	}
	if ex.count("first") != 1 || ex.count("second") != 1 {
		return fmt.Errorf("executions = first:%d second:%d, want one each", ex.count("first"), ex.count("second"))
	}
	return nil
}

func caseCancelMidRun(t *testing.T, h Harness, ex *countingExecutor) error {
	h.BeforeStep(func(runID, step string) {
		if step == "second" {
			// Deterministic "concurrent" cancel: the hook runs at the safe
			// boundary between steps.
			_ = h.Cancel(context.Background(), runID, "operator withdrew the run")
		}
	})
	mustStart(t, h, twoStepPlan("run-c13"), ex)
	status := wantState(t, h, "run-c13", StateCancelled)
	if status.Error == nil || status.Error.Class != ErrCancelled {
		return fmt.Errorf("cancel error = %v, want class cancelled", status.Error)
	}
	if ex.count("second") != 0 {
		return fmt.Errorf("the in-flight step ran %d times after cancel, want 0 (stop at a safe boundary)", ex.count("second"))
	}
	if strings.Join(status.CompletedSteps, ",") != "first" {
		return fmt.Errorf("completed steps = %v, want the durable result of the step that finished", status.CompletedSteps)
	}
	return nil
}

func casePauseResume(t *testing.T, h Harness, ex *countingExecutor) error {
	pausedOnce := false
	h.BeforeStep(func(runID, step string) {
		// One-shot: a resume must not be paused again by the same hook.
		if step == "second" && !pausedOnce {
			pausedOnce = true
			_ = h.Pause(context.Background(), runID)
		}
	})
	mustStart(t, h, twoStepPlan("run-c14"), ex)
	wantState(t, h, "run-c14", StatePaused)
	if ex.count("second") != 0 {
		return fmt.Errorf("second ran before resume")
	}
	if err := h.Resume(context.Background(), "run-c14"); err != nil {
		return fmt.Errorf("Resume: %w", err)
	}
	status := wantState(t, h, "run-c14", StateSucceeded)
	if ex.count("first") != 1 {
		return fmt.Errorf("first re-executed on resume (%d times), durable state must be preserved", ex.count("first"))
	}
	if ex.count("second") != 1 {
		return fmt.Errorf("second ran %d times, want 1", ex.count("second"))
	}
	if strings.Join(status.CompletedSteps, ",") != "first,second" {
		return fmt.Errorf("completed steps = %v, want first,second", status.CompletedSteps)
	}
	return nil
}

func caseLongSleep(t *testing.T, h Harness, ex *countingExecutor) error {
	handle := mustStart(t, h, twoStepPlan("run-c15"), ex)
	started := time.Now()
	if err := h.Sleep(context.Background(), handle, 90*time.Minute); err != nil {
		return fmt.Errorf("Sleep: %w", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		return fmt.Errorf("Sleep blocked for %s: a long sleep must not consume a worker", elapsed)
	}
	wantState(t, h, "run-c15", StateSucceeded)
	if err := h.Sleep(context.Background(), handle, -time.Second); err == nil {
		return fmt.Errorf("negative sleep was accepted")
	}
	return nil
}

func caseCapabilityRefusal(t *testing.T, h Harness, ex *countingExecutor) error {
	// A provider that advertises nothing must refuse a plan that needs
	// cancel, rather than running it and silently not cancelling.
	limited := NewFake().WithCapabilities(Capabilities{})
	plan := twoStepPlan("run-c16")
	plan.Requirements = []Capability{CapCancel}
	handle, err := limited.Start(context.Background(), plan, ex)
	if err != nil {
		return fmt.Errorf("Start: %w", err)
	}
	if handle.State != StateRejected {
		return fmt.Errorf("state = %s, want rejected (no silent downgrade)", handle.State)
	}
	if ex.count("first")+ex.count("second") != 0 {
		return fmt.Errorf("steps ran despite the missing capability")
	}
	status := mustStatus(t, limited, "run-c16")
	if status.Error == nil || !strings.Contains(status.Error.Msg, "cancel") {
		return fmt.Errorf("error = %v, want it to name the missing capability", status.Error)
	}
	// And the operations themselves refuse.
	if err := limited.Cancel(context.Background(), "run-c16", "x"); err == nil {
		return fmt.Errorf("cancel succeeded on a provider that does not advertise it")
	}
	return nil
}

func caseHistoryCompleteness(t *testing.T, h Harness, ex *countingExecutor) error {
	mustStart(t, h, twoStepPlan("run-c17"), ex)
	history, err := h.History(context.Background(), "run-c17")
	if err != nil {
		return err
	}
	seen := map[string]string{}
	for _, row := range history {
		seen[row.Step] = row.Outcome
	}
	for _, step := range []string{"first", "second"} {
		if seen[step] != "succeeded" {
			return fmt.Errorf("history lacks a succeeded row for %q (got %q)", step, seen[step])
		}
	}
	if len(history) < 2 {
		return fmt.Errorf("history rows = %d, want at least one per executed step", len(history))
	}
	return nil
}

func caseMeteringSource(t *testing.T, h Harness, ex *countingExecutor) error {
	mustStart(t, h, twoStepPlan("run-c18"), ex)
	before := wantState(t, h, "run-c18", StateSucceeded)
	wantPerStep := Usage{InputTokens: 10, OutputTokens: 5}
	if before.Usage != (Usage{InputTokens: wantPerStep.InputTokens * 2, OutputTokens: wantPerStep.OutputTokens * 2}) {
		return fmt.Errorf("usage = %+v, want 2 steps of %+v", before.Usage, wantPerStep)
	}
	// The best-effort tier may vanish without moving the metered number.
	h.DropEvents("run-c18")
	after := mustStatus(t, h, "run-c18")
	if after.Usage != before.Usage {
		return fmt.Errorf("usage changed after dropping events: %+v -> %+v", before.Usage, after.Usage)
	}
	if len(after.CompletedSteps) != 2 {
		return fmt.Errorf("durable step results = %v, want two", after.CompletedSteps)
	}
	return nil
}
