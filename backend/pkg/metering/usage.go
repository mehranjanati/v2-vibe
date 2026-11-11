package metering

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// D1 aggregation statements of the Usage view. `user_id IS ?` is a NULL-safe
// equality in SQLite: a nil parameter matches only unowned rows and a string
// matches exactly that tenant, so a scoped read can never spill across tenants.
const (
	usageRunsSQL = "SELECT status, COUNT(*) AS n, " +
		"COALESCE(SUM(CASE WHEN started_at IS NOT NULL AND completed_at IS NOT NULL AND completed_at >= started_at " +
		"THEN completed_at - started_at ELSE 0 END), 0) AS duration_ms " +
		"FROM outcome_runs WHERE outcome_id = ? AND user_id IS ? GROUP BY status"
	usageStepsSQL = "SELECT s.status AS status, COUNT(*) AS n " +
		"FROM outcome_run_steps s JOIN outcome_runs r ON r.id = s.run_id " +
		"WHERE r.outcome_id = ? AND r.user_id IS ? GROUP BY s.status"
)

// Usage is the metering view of one outcome: how many durable runs and steps
// executed, how they ended, and the accumulated wall-clock duration.
//
// It is derived from the recorded rows — no extra columns — so it reports
// counts and duration only. Token/cost counters need their own migration and
// are deliberately not invented here.
type Usage struct {
	OutcomeID string

	Runs      int
	Pending   int
	Running   int
	Succeeded int
	Failed    int
	Cancelled int

	Steps          int
	StepsSucceeded int
	StepsFailed    int
	StepsSkipped   int

	// DurationMS is the summed wall-clock duration of runs that both started
	// and finished (epoch-ms timestamps, so the sum is in milliseconds).
	DurationMS int64
}

// Usage aggregates ownerID's runs of outcomeID.
//
// Authorization belongs to the caller (the outcome routes resolve ownership
// before they read); metering only enforces the tenant filter as
// defence-in-depth. An empty ownerID aggregates unowned runs only — never every
// tenant's.
func (r *Recorder) Usage(ctx context.Context, outcomeID, ownerID string) (Usage, error) {
	outcomeID = strings.TrimSpace(outcomeID)
	if outcomeID == "" {
		return Usage{}, errors.New("metering: Usage: outcome id is required")
	}
	out := Usage{OutcomeID: outcomeID}
	if !r.Enabled() {
		return out, ErrNoStore
	}

	runCtx, cancelRuns := writeCtx(ctx)
	rows, err := r.d1.Query(runCtx, usageRunsSQL, outcomeID, nullable(ownerID))
	cancelRuns()
	if err != nil {
		return out, fmt.Errorf("metering: run usage: %w", err)
	}
	for _, row := range rows {
		n := intAt(row, "n")
		switch RunStatus(strAt(row, "status")) {
		case RunPending:
			out.Pending += n
		case RunRunning:
			out.Running += n
		case RunSucceeded:
			out.Succeeded += n
		case RunFailed:
			out.Failed += n
		case RunCancelled:
			out.Cancelled += n
		}
		out.Runs += n
		out.DurationMS += int64At(row, "duration_ms")
	}

	stepCtx, cancelSteps := writeCtx(ctx)
	stepRows, err := r.d1.Query(stepCtx, usageStepsSQL, outcomeID, nullable(ownerID))
	cancelSteps()
	if err != nil {
		return out, fmt.Errorf("metering: step usage: %w", err)
	}
	for _, row := range stepRows {
		n := intAt(row, "n")
		switch StepStatus(strAt(row, "status")) {
		case StepSucceeded:
			out.StepsSucceeded += n
		case StepFailed:
			out.StepsFailed += n
		case StepSkipped:
			out.StepsSkipped += n
		}
		out.Steps += n
	}
	return out, nil
}

// strAt reads a text column ("" when missing/nil/non-string). D1 returns text
// columns as JSON strings.
func strAt(row map[string]any, key string) string {
	v, ok := row[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// intAt reads a numeric column as int (D1 decodes numbers as float64).
func intAt(row map[string]any, key string) int {
	return int(int64At(row, key))
}

// int64At reads a numeric column as int64 (D1 decodes numbers as float64).
func int64At(row map[string]any, key string) int64 {
	v, ok := row[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}
