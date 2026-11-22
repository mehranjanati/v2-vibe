package execution

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// State is one of the six run states plus `rejected`. Terminal states
// (succeeded, failed, cancelled, rejected) never transition again.
type State string

const (
	StateQueued    State = "queued"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
	StatePaused    State = "paused"
	// StateRejected is an admission refusal. It is deliberately distinct
	// from StateFailed: a capability gap is not a runtime failure.
	StateRejected State = "rejected"
)

// Terminal reports whether a state is final.
func (s State) Terminal() bool {
	switch s {
	case StateSucceeded, StateFailed, StateCancelled, StateRejected:
		return true
	default:
		return false
	}
}

// Valid reports whether the state is one the contract defines.
func (s State) Valid() bool {
	switch s {
	case StateQueued, StateRunning, StateSucceeded, StateFailed, StateCancelled, StatePaused, StateRejected:
		return true
	default:
		return false
	}
}

// ErrorClass is the fixed error taxonomy. A provider maps its own failures onto
// it; callers never parse provider-specific error text.
type ErrorClass string

const (
	// ErrTransient may succeed on retry within the step's budget.
	ErrTransient ErrorClass = "transient"
	// ErrPermanent will not succeed on retry.
	ErrPermanent ErrorClass = "permanent"
	// ErrPolicyDenied is a refusal that must be audited, not just counted.
	ErrPolicyDenied ErrorClass = "policy-denied"
	// ErrQuotaExceeded means a per-tenant cap stopped the run.
	ErrQuotaExceeded ErrorClass = "quota-exceeded"
	// ErrCancelled means an operator or publisher withdrew the run.
	ErrCancelled ErrorClass = "cancelled"
	// ErrAdmissionRefused means the plan never started.
	ErrAdmissionRefused ErrorClass = "admission-refused"
)

// Valid reports whether the class is part of the taxonomy.
func (c ErrorClass) Valid() bool {
	switch c {
	case ErrTransient, ErrPermanent, ErrPolicyDenied, ErrQuotaExceeded, ErrCancelled, ErrAdmissionRefused:
		return true
	default:
		return false
	}
}

// Retryable reports whether a class is worth another attempt.
func (c ErrorClass) Retryable() bool { return c == ErrTransient }

// Error is a contract error: a class plus a message.
type Error struct {
	Class ErrorClass
	// Msg explains the failure in one line.
	Msg string
	// RetryAfter is optional guidance for transient failures.
	RetryAfter time.Duration
}

func (e *Error) Error() string { return string(e.Class) + ": " + e.Msg }

// Classify returns the class of an error. Unknown errors are permanent: an
// unrecognised failure must never be retried forever.
func Classify(err error) ErrorClass {
	var pe *Error
	if errors.As(err, &pe) {
		return pe.Class
	}
	return ErrPermanent
}

// Transient wraps an error as retryable.
func Transient(format string, args ...interface{}) *Error {
	return &Error{Class: ErrTransient, Msg: fmt.Sprintf(format, args...)}
}

// Permanent wraps an error as non-retryable.
func Permanent(format string, args ...interface{}) *Error {
	return &Error{Class: ErrPermanent, Msg: fmt.Sprintf(format, args...)}
}

// Refused builds an admission refusal.
func Refused(format string, args ...interface{}) *Error {
	return &Error{Class: ErrAdmissionRefused, Msg: fmt.Sprintf(format, args...)}
}

// Cancelled builds a cancellation error for a step interrupted by cancel().
func Cancelled(format string, args ...interface{}) *Error {
	return &Error{Class: ErrCancelled, Msg: fmt.Sprintf(format, args...)}
}

// Capability is one optional guarantee a provider may advertise.
type Capability string

const (
	CapCancel    Capability = "cancel"
	CapPause     Capability = "pause"
	CapLongSleep Capability = "long-sleep"
	CapHistory   Capability = "history"
)

// Capabilities is a provider's advertised guarantees. Advertisement is a
// promise: a provider that says false must refuse the operation, and one that
// says true must pass the matching conformance case.
type Capabilities struct {
	Cancel    bool
	Pause     bool
	LongSleep bool
	History   bool
}

// Supports reports whether a capability was advertised.
func (c Capabilities) Supports(capability Capability) bool {
	switch capability {
	case CapCancel:
		return c.Cancel
	case CapPause:
		return c.Pause
	case CapLongSleep:
		return c.LongSleep
	case CapHistory:
		return c.History
	default:
		return false
	}
}

// Missing returns the requirements the provider does not advertise, sorted.
func (c Capabilities) Missing(required []Capability) []Capability {
	var missing []Capability
	for _, r := range required {
		if !c.Supports(r) {
			missing = append(missing, r)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
	return missing
}

// String renders the advertisement for logs and history rows.
func (c Capabilities) String() string {
	return fmt.Sprintf("cancel=%t pause=%t long-sleep=%t history=%t", c.Cancel, c.Pause, c.LongSleep, c.History)
}

// Usage is the metering payload of one step. Metering reads durable step
// results, never best-effort events (C18).
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Add accumulates usage.
func (u *Usage) Add(other Usage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
}

// StepResult is what a step produced. Empty output is allowed; a failure must
// never be reported as an empty success.
type StepResult struct {
	Output string
	Usage  Usage
}

// StepRetry bounds retries for one step. MaxAttempts counts the first attempt.
type StepRetry struct {
	MaxAttempts int
	// Backoff is the base delay between attempts. The reference provider does
	// not sleep for it (tests must not be slow); a production provider honours
	// it.
	Backoff time.Duration
}

// Step is one durable unit of a plan.
type Step struct {
	// Name is the step identity and must be stable across replays.
	Name string
	// DependsOn lists step names that must succeed first.
	DependsOn []string
	// Retry bounds re-execution. Zero value means a single attempt.
	Retry StepRetry
	// Timeout bounds the step. Zero means no bound.
	Timeout time.Duration
	// IdempotencyKey names the argument/field that makes a re-delivery safe.
	IdempotencyKey string
}

// RunPlan is what to execute: an acyclic step graph for one tenant. The provider
// executes plans; it never parses, validates or repairs them.
type RunPlan struct {
	// ID identifies the plan (a workflow id today).
	ID string
	// Tenant is mandatory: a provider that cannot enforce it must refuse.
	Tenant string
	// Steps are the durable units.
	Steps []Step
	// Requirements lists the optional guarantees the plan depends on. A
	// provider that does not advertise one refuses the plan (C16).
	Requirements []Capability
}

// RunHandle is what start returns.
type RunHandle struct {
	RunID  string
	Tenant string
	PlanID string
	State  State
}

// StepCall is the context handed to a step executor.
type StepCall struct {
	RunID  string
	PlanID string
	Tenant string
	// Step is the stable step name.
	Step string
	// Attempt starts at 1 and increments on re-delivery.
	Attempt int
	// IdempotencyKey is copied from the step; a side-effecting executor
	// deduplicates on it.
	IdempotencyKey string
}

// StepEvent is one observability row. These are the best-effort tier: they may be
// lost and must never be used for billing or authorization.
type StepEvent struct {
	RunID      string
	Step       string
	Attempt    int
	StartedAt  time.Time
	FinishedAt time.Time
	// Outcome is "succeeded", "failed" or "skipped".
	Outcome string
	Error   string
	// Durable marks a row read from durable step results rather than the
	// best-effort event log.
	Durable bool
}

// Status is the durable run state plus its metered usage.
type Status struct {
	RunID      string
	PlanID     string
	Tenant     string
	State      State
	StartedAt  time.Time
	FinishedAt time.Time
	// Error is set when the run ended failed or rejected.
	Error *Error
	// Usage is the sum of durable step results (C18).
	Usage Usage
	// CompletedSteps lists steps with a durable result, in plan order.
	CompletedSteps []string
}

// StepExecutor runs one step. The provider owns durability; the executor owns
// the work. Any error it returns is classified by Classify.
type StepExecutor interface {
	Execute(ctx context.Context, call StepCall) (StepResult, error)
}

// StepExecutorFunc adapts a function to StepExecutor.
type StepExecutorFunc func(ctx context.Context, call StepCall) (StepResult, error)

// Execute implements StepExecutor.
func (f StepExecutorFunc) Execute(ctx context.Context, call StepCall) (StepResult, error) {
	return f(ctx, call)
}

// ValidatePlan applies the admission rules the runtime owns. It returns an
// admission refusal, never a repair: a provider must not guess what a broken plan
// meant, and a plan with a missing tenant is refused before any step runs (C8).
func ValidatePlan(plan RunPlan) error {
	if strings.TrimSpace(plan.Tenant) == "" {
		return Refused("plan %q has no tenant: a run without a tenant scope is not admissible", plan.ID)
	}
	if len(plan.Steps) == 0 {
		return Refused("plan %q has no steps", plan.ID)
	}
	seen := make(map[string]int, len(plan.Steps))
	for i, step := range plan.Steps {
		if strings.TrimSpace(step.Name) == "" {
			return Refused("plan %q step %d has no name (the name is the step identity)", plan.ID, i)
		}
		if prev, dup := seen[step.Name]; dup {
			return Refused("plan %q declares step %q twice (indexes %d and %d)", plan.ID, step.Name, prev, i)
		}
		seen[step.Name] = i
		if step.Retry.MaxAttempts < 0 {
			return Refused("plan %q step %q has a negative retry budget", plan.ID, step.Name)
		}
		if step.Timeout < 0 {
			return Refused("plan %q step %q has a negative timeout", plan.ID, step.Name)
		}
	}
	for _, step := range plan.Steps {
		for _, dep := range step.DependsOn {
			if _, ok := seen[dep]; !ok {
				return Refused("plan %q step %q depends on unknown step %q", plan.ID, step.Name, dep)
			}
			if dep == step.Name {
				return Refused("plan %q step %q depends on itself", plan.ID, step.Name)
			}
		}
	}
	if cycle := findCycle(plan); cycle != "" {
		return Refused("plan %q is cyclic: %s", plan.ID, cycle)
	}
	return nil
}

// Admit validates a plan and checks the provider's advertisement against the
// plan's requirements. It is the single admission gate both providers call, so
// C8, C9 and C16 cannot drift apart.
func Admit(plan RunPlan, caps Capabilities) error {
	if err := ValidatePlan(plan); err != nil {
		return err
	}
	if missing := caps.Missing(plan.Requirements); len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for _, m := range missing {
			names = append(names, string(m))
		}
		return Refused("plan %q requires %s, which this provider does not advertise (advertised: %s)",
			plan.ID, strings.Join(names, ", "), caps)
	}
	return nil
}

// findCycle returns a readable cycle path, or "" when the graph is acyclic.
func findCycle(plan RunPlan) string {
	graph := make(map[string][]string, len(plan.Steps))
	for _, step := range plan.Steps {
		graph[step.Name] = step.DependsOn
	}
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := make(map[string]int, len(graph))
	var stack []string
	var walk func(node string) string
	walk = func(node string) string {
		color[node] = grey
		stack = append(stack, node)
		for _, dep := range graph[node] {
			switch color[dep] {
			case grey:
				return strings.Join(append(stack, dep), " -> ")
			case white:
				if found := walk(dep); found != "" {
					return found
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[node] = black
		return ""
	}
	names := make([]string, 0, len(graph))
	for name := range graph {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if color[name] == white {
			if found := walk(name); found != "" {
				return found
			}
		}
	}
	return ""
}

// orderedSteps returns the steps in dependency order (stable for equal
// readiness), or an error when the graph is cyclic. Providers use it so step
// order is a property of the contract, not of a provider's implementation.
func orderedSteps(plan RunPlan) ([]Step, error) {
	byName := make(map[string]Step, len(plan.Steps))
	pending := make(map[string]int, len(plan.Steps))
	dependents := make(map[string][]string, len(plan.Steps))
	for _, step := range plan.Steps {
		byName[step.Name] = step
		pending[step.Name] = len(step.DependsOn)
		for _, dep := range step.DependsOn {
			dependents[dep] = append(dependents[dep], step.Name)
		}
	}
	var ready []string
	for name, count := range pending {
		if count == 0 {
			ready = append(ready, name)
		}
	}
	sort.Strings(ready)
	out := make([]Step, 0, len(plan.Steps))
	for len(ready) > 0 {
		name := ready[0]
		ready = ready[1:]
		out = append(out, byName[name])
		for _, dep := range dependents[name] {
			pending[dep]--
			if pending[dep] == 0 {
				ready = append(ready, dep)
				sort.Strings(ready)
			}
		}
	}
	if len(out) != len(plan.Steps) {
		return nil, Refused("plan %q is cyclic", plan.ID)
	}
	return out, nil
}
