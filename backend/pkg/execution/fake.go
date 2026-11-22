package execution

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Provider is the nine-operation contract every execution substrate implements.
//
//	start · status · cancel · pause · resume · step · sleep · history · capabilities
//
// `step` is not on this interface by design: it is the durability primitive the
// provider owns, and it is expressed here as the provider driving a StepExecutor.
// A caller never sees a step; it sees a run.
type Provider interface {
	// Capabilities advertises the optional guarantees this provider keeps.
	Capabilities() Capabilities
	// Start admits and starts a run. Starting an existing run id returns the
	// existing handle instead of a second execution (C12). An admission
	// refusal returns a handle in StateRejected — the refusal is a state, not
	// a Go error, so it stays observable.
	Start(ctx context.Context, plan RunPlan, exec StepExecutor) (RunHandle, error)
	// Status returns the durable run state.
	Status(ctx context.Context, runID string) (Status, error)
	// Cancel withdraws a run (capability: cancel).
	Cancel(ctx context.Context, runID, reason string) error
	// Pause holds a run at its next safe boundary (capability: pause).
	Pause(ctx context.Context, runID string) error
	// Resume continues a paused run without re-executing completed steps.
	Resume(ctx context.Context, runID string) error
	// Sleep waits without consuming a worker for the duration (capability:
	// long-sleep).
	Sleep(ctx context.Context, handle RunHandle, d time.Duration) error
	// History returns the best-effort step events. Never a billing source.
	History(ctx context.Context, runID string) ([]StepEvent, error)
}

// Fake is the reference in-process provider: deterministic, synchronous, with a
// durable step log and a best-effort event log kept apart on purpose. It exists
// to make the contract executable before a second real substrate does, and it is
// what the C1–C18 suite runs against by default.
//
// Determinism rules: no goroutines, no wall-clock waits (backoff and sleep are
// recorded, not slept), and every injection point is explicit.
type Fake struct {
	mu     sync.Mutex
	caps   Capabilities
	now    func() time.Time
	runs   map[string]*fakeRun
	events map[string][]StepEvent

	// failNext queues errors for a step name, consumed one per attempt.
	failNext map[string][]error
	// crashOn counts simulated crashes per step name.
	crashOn map[string]int
	// beforeStep runs before each attempt: a test uses it to cancel or pause
	// mid-run deterministically.
	beforeStep func(runID, step string)
	// sleeps records Sleep calls (C15): the fake never blocks.
	sleeps []time.Duration
}

type fakeRun struct {
	plan      RunPlan
	order     []Step
	state     State
	completed map[string]StepResult
	attempts  map[string]int
	usage     Usage
	err       *Error
	startedAt time.Time
	endedAt   time.Time
	cancel    bool
	pause     bool
	exec      StepExecutor
}

// NewFake builds the reference provider with every capability advertised.
func NewFake() *Fake {
	return &Fake{
		caps:     Capabilities{Cancel: true, Pause: true, LongSleep: true, History: true},
		now:      func() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) },
		runs:     map[string]*fakeRun{},
		events:   map[string][]StepEvent{},
		failNext: map[string][]error{},
		crashOn:  map[string]int{},
	}
}

// WithCapabilities replaces the advertisement (used to prove refusal paths).
func (f *Fake) WithCapabilities(caps Capabilities) *Fake {
	f.caps = caps
	return f
}

// Capabilities implements Provider.
func (f *Fake) Capabilities() Capabilities { return f.caps }

// FailNext queues n failures for one step, one per attempt.
func (f *Fake) FailNext(step string, err error, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := 0; i < n; i++ {
		f.failNext[step] = append(f.failNext[step], err)
	}
}

// CrashOn makes the next attempt at step succeed and then lose its durable
// write, simulating a restart (C2/C3).
func (f *Fake) CrashOn(step string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.crashOn[step]++
}

// BeforeStep installs a hook that runs before every attempt.
func (f *Fake) BeforeStep(hook func(runID, step string)) { f.beforeStep = hook }

// Sleeps returns the recorded sleep durations.
func (f *Fake) Sleeps() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.sleeps...)
}

// DropEvents deletes the best-effort event log, proving metering does not read it
// (C18).
func (f *Fake) DropEvents(runID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.events, runID)
}

// Start implements Provider.
func (f *Fake) Start(ctx context.Context, plan RunPlan, exec StepExecutor) (RunHandle, error) {
	f.mu.Lock()
	if run, ok := f.runs[plan.ID]; ok {
		handle := RunHandle{RunID: plan.ID, Tenant: run.plan.Tenant, PlanID: run.plan.ID, State: run.state}
		f.mu.Unlock()
		return handle, nil
	}
	run := &fakeRun{
		plan: plan, state: StateQueued, completed: map[string]StepResult{},
		attempts: map[string]int{}, startedAt: f.now(), exec: exec,
	}
	f.runs[plan.ID] = run
	if err := Admit(plan, f.caps); err != nil {
		run.err = asContractError(err)
		run.state = StateRejected
		run.endedAt = f.now()
		f.mu.Unlock()
		return RunHandle{RunID: plan.ID, Tenant: plan.Tenant, PlanID: plan.ID, State: StateRejected}, nil
	}
	order, err := orderedSteps(plan)
	if err != nil {
		run.err = asContractError(err)
		run.state = StateRejected
		run.endedAt = f.now()
		f.mu.Unlock()
		return RunHandle{RunID: plan.ID, Tenant: plan.Tenant, PlanID: plan.ID, State: StateRejected}, nil
	}
	run.order = order
	f.mu.Unlock()

	if driveErr := f.drive(ctx, plan.ID, run); driveErr != nil {
		return RunHandle{RunID: plan.ID, Tenant: plan.Tenant, PlanID: plan.ID, State: run.state}, driveErr
	}
	return RunHandle{RunID: plan.ID, Tenant: plan.Tenant, PlanID: plan.ID, State: run.state}, nil
}

// Recover re-drives a run left running by a simulated crash. It is a test hook,
// not part of the contract: a real substrate recovers on its own.
func (f *Fake) Recover(runID string) error {
	f.mu.Lock()
	run, ok := f.runs[runID]
	f.mu.Unlock()
	if !ok {
		return Permanent("run %q is unknown", runID)
	}
	return f.drive(context.Background(), runID, run)
}

// drive executes the plan from the first unfinished step. Completed steps are
// skipped because their durable result is already recorded (C2).
func (f *Fake) drive(ctx context.Context, runID string, run *fakeRun) error {
	f.mu.Lock()
	if run.state.Terminal() {
		f.mu.Unlock()
		return nil
	}
	run.state = StateRunning
	f.mu.Unlock()

	for _, step := range run.order {
		f.mu.Lock()
		if run.cancel {
			run.state = StateCancelled
			run.err = Cancelled("run %q cancelled before step %q", runID, step.Name)
			run.endedAt = f.now()
			f.mu.Unlock()
			return nil
		}
		if run.pause {
			run.state = StatePaused
			f.mu.Unlock()
			return nil
		}
		if _, done := run.completed[step.Name]; done {
			f.mu.Unlock()
			continue
		}
		hook := f.beforeStep
		f.mu.Unlock()

		if hook != nil {
			hook(runID, step.Name)
			continueOrPause := f.applyHookState(runID, run)
			if !continueOrPause {
				return nil
			}
		}
		stop, err := f.runStep(ctx, runID, run, step)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// A run only succeeds if the loop actually finished it: a crash, a
	// cancellation or a failure left the state elsewhere and must not be
	// overwritten by the happy path.
	if run.state == StateRunning {
		run.state = StateSucceeded
		run.endedAt = f.now()
	}
	return nil
}

// applyHookState turns a hook-triggered cancel/pause into a state transition and
// reports whether the drive loop may continue.
func (f *Fake) applyHookState(runID string, run *fakeRun) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if run.cancel {
		run.state = StateCancelled
		run.err = Cancelled("run %q cancelled mid-run", runID)
		run.endedAt = f.now()
		return false
	}
	if run.pause {
		run.state = StatePaused
		return false
	}
	return true
}

// runStep executes one step with its retry budget, timeout and crash injection.
func (f *Fake) runStep(ctx context.Context, runID string, run *fakeRun, step Step) (bool, error) {
	maxAttempts := step.Retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	for {
		f.mu.Lock()
		run.attempts[step.Name]++
		attempt := run.attempts[step.Name]
		exec := run.exec
		var queued error
		if q := f.failNext[step.Name]; len(q) > 0 {
			queued = q[0]
			f.failNext[step.Name] = q[1:]
		}
		started := f.now()
		f.mu.Unlock()

		call := StepCall{
			RunID: runID, PlanID: run.plan.ID, Tenant: run.plan.Tenant,
			Step: step.Name, Attempt: attempt, IdempotencyKey: step.IdempotencyKey,
		}
		stepCtx := ctx
		cancel := func() {}
		if step.Timeout > 0 {
			stepCtx, cancel = context.WithTimeout(ctx, step.Timeout)
		}
		var result StepResult
		var err error
		if exec != nil {
			result, err = exec.Execute(stepCtx, call)
		}
		cancel()
		if queued != nil {
			err = queued
		}
		if err == nil && stepCtx.Err() != nil {
			err = Transient("step %q timed out after %s", step.Name, step.Timeout)
		}
		if ctx.Err() != nil {
			err = Cancelled("run %q cancelled during step %q", runID, step.Name)
		}

		finished := f.now()
		if err == nil {
			f.mu.Lock()
			if f.crashOn[step.Name] > 0 {
				// The work happened, the durable write did not: the
				// at-least-once window C3 exists for.
				f.crashOn[step.Name]--
				f.events[runID] = append(f.events[runID], StepEvent{
					RunID: runID, Step: step.Name, Attempt: attempt,
					StartedAt: started, FinishedAt: finished, Outcome: "failed",
					Error: "simulated crash before durable write",
				})
				f.mu.Unlock()
				return true, nil
			}
			run.completed[step.Name] = result
			run.usage.Add(result.Usage)
			f.events[runID] = append(f.events[runID], StepEvent{
				RunID: runID, Step: step.Name, Attempt: attempt,
				StartedAt: started, FinishedAt: finished, Outcome: "succeeded", Durable: true,
			})
			f.mu.Unlock()
			return false, nil
		}

		class := Classify(err)
		f.mu.Lock()
		f.events[runID] = append(f.events[runID], StepEvent{
			RunID: runID, Step: step.Name, Attempt: attempt,
			StartedAt: started, FinishedAt: finished, Outcome: "failed", Error: err.Error(),
		})
		if class == ErrCancelled {
			run.state = StateCancelled
			run.err = asContractError(err)
			run.endedAt = f.now()
			f.mu.Unlock()
			return true, nil
		}
		if class.Retryable() && attempt < maxAttempts {
			f.mu.Unlock()
			continue
		}
		run.state = StateFailed
		run.err = asContractError(err)
		run.endedAt = f.now()
		f.mu.Unlock()
		return true, nil
	}
}

// Status implements Provider.
func (f *Fake) Status(_ context.Context, runID string) (Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	run, ok := f.runs[runID]
	if !ok {
		return Status{}, Permanent("run %q is unknown", runID)
	}
	status := Status{
		RunID: runID, PlanID: run.plan.ID, Tenant: run.plan.Tenant, State: run.state,
		StartedAt: run.startedAt, FinishedAt: run.endedAt, Error: run.err, Usage: run.usage,
	}
	for _, step := range run.order {
		if _, done := run.completed[step.Name]; done {
			status.CompletedSteps = append(status.CompletedSteps, step.Name)
		}
	}
	return status, nil
}

// Cancel implements Provider.
func (f *Fake) Cancel(_ context.Context, runID, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	run, ok := f.runs[runID]
	if !ok {
		return Permanent("run %q is unknown", runID)
	}
	if !f.caps.Cancel {
		return &Error{Class: ErrAdmissionRefused, Msg: "cancel is not advertised by this provider"}
	}
	if run.state.Terminal() {
		return Permanent("run %q is %s: terminal states are final", runID, run.state)
	}
	if reason == "" {
		reason = "no reason given"
	}
	run.cancel = true
	if run.state == StatePaused || run.state == StateQueued {
		run.state = StateCancelled
		run.err = Cancelled("run %q cancelled: %s", runID, reason)
		run.endedAt = f.now()
	}
	return nil
}

// Pause implements Provider.
func (f *Fake) Pause(_ context.Context, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	run, ok := f.runs[runID]
	if !ok {
		return Permanent("run %q is unknown", runID)
	}
	if !f.caps.Pause {
		return &Error{Class: ErrAdmissionRefused, Msg: "pause is not advertised by this provider"}
	}
	if run.state.Terminal() {
		return Permanent("run %q is %s: terminal states are final", runID, run.state)
	}
	run.pause = true
	return nil
}

// Resume implements Provider.
func (f *Fake) Resume(ctx context.Context, runID string) error {
	f.mu.Lock()
	run, ok := f.runs[runID]
	if !ok {
		f.mu.Unlock()
		return Permanent("run %q is unknown", runID)
	}
	if !f.caps.Pause {
		f.mu.Unlock()
		return &Error{Class: ErrAdmissionRefused, Msg: "resume is not advertised by this provider"}
	}
	if run.state != StatePaused {
		state := run.state
		f.mu.Unlock()
		return Permanent("run %q is %s, not paused", runID, state)
	}
	run.pause = false
	run.state = StateRunning
	f.mu.Unlock()
	return f.drive(ctx, runID, run)
}

// Sleep implements Provider. The fake records the duration instead of blocking,
// which is what "does not consume a worker" looks like in a test.
func (f *Fake) Sleep(_ context.Context, handle RunHandle, d time.Duration) error {
	if !f.caps.LongSleep {
		return &Error{Class: ErrAdmissionRefused, Msg: "long-sleep is not advertised by this provider"}
	}
	if d < 0 {
		return Permanent("sleep duration must not be negative")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sleeps = append(f.sleeps, d)
	return nil
}

// History implements Provider.
func (f *Fake) History(_ context.Context, runID string) ([]StepEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.runs[runID]; !ok {
		return nil, Permanent("run %q is unknown", runID)
	}
	if !f.caps.History {
		return nil, &Error{Class: ErrAdmissionRefused, Msg: "history is not advertised by this provider"}
	}
	return append([]StepEvent(nil), f.events[runID]...), nil
}

// asContractError converts any error into the taxonomy's shape.
func asContractError(err error) *Error {
	var pe *Error
	if errors.As(err, &pe) {
		return pe
	}
	return Permanent("%s", err.Error())
}
