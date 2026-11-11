// Package metering records durable outcome runs and their step logs in D1.
//
// This is the write half of the M1 metering slice (decision D6: durable runs
// are a metering key — never seats): one `outcome_runs` row per run, one
// `outcome_run_steps` row per node attempt, plus the derived Usage view the API
// reads. The HTTP contract these rows back is `OutcomeRun*` in
// `src/api-types.ts`; the schema is `outcome_runs` / `outcome_run_steps` in
// `worker/database/schema.ts` (migration `0014_outcome_runs.sql`).
//
// The lifecycle mirrors the workflow runtime the platform already runs
// (`worker/workflow/VibeWorkflow.ts`):
//
//	run:  pending → running → succeeded | failed | cancelled
//	step: running → succeeded | failed, or a single skipped row
//
// Timestamps are epoch milliseconds (the Worker's Date.now() convention and
// the control plane's normalizeTimestamp), never seconds.
//
// Everything here is best-effort, exactly like the generation lineage recorder
// (`backend/pkg/engine/generation.go`): a missing D1 client, an unmigrated
// table or a D1 outage returns an advisory error the caller logs — it must never
// fail the outcome itself. The Go control plane is the single writer of these
// tables; the runtime Worker never touches them.
package metering

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"backend/pkg/cloudflare"
)

// d1Timeout bounds every single D1 statement issued by the recorder.
const d1Timeout = 15 * time.Second

// ErrNoStore reports that the recorder has no D1 client attached.
// StartRun/StartStep still return a usable id alongside it so an outcome can
// run unrecorded instead of failing.
var ErrNoStore = errors.New("metering: no D1 client configured")

// RunStatus is the `outcome_runs.status` vocabulary (mirrors
// OUTCOME_RUN_STATUS_VALUES in worker/database/schema.ts).
type RunStatus string

// Terminal and in-flight run states.
const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// StepStatus is the `outcome_run_steps.status` vocabulary (mirrors
// OUTCOME_RUN_STEP_STATUS_VALUES in worker/database/schema.ts).
type StepStatus string

// Step states written by StartStep / FinishStep / SkipStep.
const (
	StepPending   StepStatus = "pending"
	StepRunning   StepStatus = "running"
	StepSucceeded StepStatus = "succeeded"
	StepFailed    StepStatus = "failed"
	StepSkipped   StepStatus = "skipped"
)

// NodeType is the `outcome_run_steps.node_type` vocabulary (mirrors
// OUTCOME_RUN_NODE_TYPE_VALUES in worker/database/schema.ts). The empty value
// is stored as SQL NULL.
type NodeType string

// Node types a plan step can execute.
const (
	NodeTrigger   NodeType = "trigger"
	NodeHTTP      NodeType = "http"
	NodeDB        NodeType = "db"
	NodeAI        NodeType = "ai"
	NodeEmail     NodeType = "email"
	NodeCondition NodeType = "condition"
	NodeSleep     NodeType = "sleep"
)

// D1 statements of the recorder. Kept as constants so the emulated D1 in
// recorder_test.go can mirror them exactly.
const (
	// insertRunSQL opens the run ('pending', no started_at). NULLIF keeps an
	// empty tenant as SQL NULL (unowned/legacy), matching the workflow writer.
	insertRunSQL = "INSERT INTO outcome_runs (id, outcome_id, status, user_id, input) VALUES (?, ?, 'pending', NULLIF(?, ''), ?)"
	// markRunningSQL stamps started_at once and keeps an input stored at
	// insert time (COALESCE), exactly like VibeWorkflow.markRunning.
	markRunningSQL = "UPDATE outcome_runs SET status = 'running', started_at = ?, input = COALESCE(input, ?), updated_at = CURRENT_TIMESTAMP WHERE id = ?"
	// finishRunSQL closes the run with its terminal status. COALESCE keeps an
	// output the runtime already wrote when the caller passes none.
	finishRunSQL = "UPDATE outcome_runs SET status = ?, output = COALESCE(?, output), error = ?, completed_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?"
	// insertStepSQL opens one step attempt in 'running' with its start stamp.
	insertStepSQL = "INSERT INTO outcome_run_steps (id, run_id, step_name, node_type, status, attempt, started_at) VALUES (?, ?, ?, ?, 'running', ?, ?)"
	// finishStepSQL closes one step attempt (succeeded | failed).
	finishStepSQL = "UPDATE outcome_run_steps SET status = ?, output = COALESCE(?, output), error = ?, completed_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?"
	// insertSkippedStepSQL records a pruned plan node: one row, no started_at,
	// mirroring VibeWorkflow.logSkipped.
	insertSkippedStepSQL = "INSERT INTO outcome_run_steps (id, run_id, step_name, node_type, status, attempt, completed_at) VALUES (?, ?, ?, ?, 'skipped', ?, ?)"
)

// Recorder writes outcome runs + step logs to D1. It is stateless apart from
// the client and the clock, so one recorder per process (or per request) is
// enough: metering.NewRecorder(hub.D1Client()).
type Recorder struct {
	d1 *cloudflare.D1Client
	// now overrides the clock in tests; nil means time.Now.
	now func() time.Time
}

// NewRecorder returns a recorder backed by d1. A nil client is valid: every
// write then returns ErrNoStore and the outcome still runs unrecorded.
func NewRecorder(d1 *cloudflare.D1Client) *Recorder {
	return &Recorder{d1: d1}
}

// Enabled reports whether the recorder can write (a D1 client is attached).
func (r *Recorder) Enabled() bool {
	return r != nil && r.d1 != nil
}

// nowMillis is the epoch-millisecond clock shared by every timestamp column.
func (r *Recorder) nowMillis() int64 {
	if r != nil && r.now != nil {
		return r.now().UnixMilli()
	}
	return time.Now().UnixMilli()
}

// writeCtx derives a bounded context that survives the caller's cancellation:
// the run row is a best-effort side write, so a cancelled outcome must still
// close its row (mirrors lineageCtx in backend/pkg/engine/generation.go).
func writeCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), d1Timeout)
}

// nullable maps "" to SQL NULL (user_id, error and node_type are nullable).
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullableNodeType maps an unknown/empty node type to SQL NULL instead of
// inventing a vocabulary value the schema enum does not allow.
func nullableNodeType(t NodeType) any {
	if t == "" {
		return nil
	}
	return string(t)
}

// jsonOrNil returns the raw JSON text for a payload column, or nil (SQL NULL)
// when the payload is empty. Callers pass json.RawMessage so the recorder never
// re-marshals a payload it did not build.
func jsonOrNil(raw json.RawMessage) any {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	return string(trimmed)
}

// requireRunID rejects empty run ids with a message naming the caller.
func requireRunID(op, runID string) error {
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("metering: %s: run id is required", op)
	}
	return nil
}

// StartRunParams opens one outcome run.
type StartRunParams struct {
	// OutcomeID is the outcome_definitions row the run executes (required).
	OutcomeID string
	// OwnerID is the run's tenant; "" stores SQL NULL (unowned/legacy).
	OwnerID string
	// RunID is optional: generated when empty, honored when supplied so a
	// caller can correlate its own identifiers.
	RunID string
	// Input is the trigger payload (optional); nil/empty stores SQL NULL.
	Input json.RawMessage
}

// StartRun inserts the 'pending' outcome_runs row and returns its id.
//
// The id is always usable: when D1 is not configured the caller receives a
// generated id plus ErrNoStore so the outcome can still run unrecorded.
func (r *Recorder) StartRun(ctx context.Context, p StartRunParams) (string, error) {
	if strings.TrimSpace(p.OutcomeID) == "" {
		return "", errors.New("metering: StartRun: outcome id is required")
	}
	runID := strings.TrimSpace(p.RunID)
	if runID == "" {
		runID = uuid.NewString()
	}
	if !r.Enabled() {
		return runID, ErrNoStore
	}

	dctx, cancel := writeCtx(ctx)
	defer cancel()
	if _, err := r.d1.Exec(dctx, insertRunSQL, runID, p.OutcomeID, p.OwnerID, jsonOrNil(p.Input)); err != nil {
		return runID, fmt.Errorf("metering: insert outcome run: %w", err)
	}
	return runID, nil
}

// MarkRunning advances the run to 'running' and stamps started_at. input is
// stored only when the row has none yet (COALESCE), matching the runtime's
// markRunning — re-delivered triggers must not overwrite the first payload.
func (r *Recorder) MarkRunning(ctx context.Context, runID string, input json.RawMessage) error {
	if err := requireRunID("MarkRunning", runID); err != nil {
		return err
	}
	if !r.Enabled() {
		return ErrNoStore
	}

	dctx, cancel := writeCtx(ctx)
	defer cancel()
	if _, err := r.d1.Exec(dctx, markRunningSQL, r.nowMillis(), jsonOrNil(input), runID); err != nil {
		return fmt.Errorf("metering: mark outcome run running: %w", err)
	}
	return nil
}

// FinishRunParams closes one run. Status must be terminal.
type FinishRunParams struct {
	RunID  string
	Status RunStatus
	// Output is the run's result payload (optional).
	Output json.RawMessage
	// Error is the failure/escalation message ("" stores SQL NULL).
	Error string
}

// FinishRun closes the run with its terminal status, output and error message.
// It is idempotent (one UPDATE keyed by run id), so a retried close cannot
// duplicate rows.
func (r *Recorder) FinishRun(ctx context.Context, p FinishRunParams) error {
	if err := requireRunID("FinishRun", p.RunID); err != nil {
		return err
	}
	switch p.Status {
	case RunSucceeded, RunFailed, RunCancelled:
	default:
		return fmt.Errorf("metering: FinishRun: status %q is not terminal", p.Status)
	}
	if !r.Enabled() {
		return ErrNoStore
	}

	dctx, cancel := writeCtx(ctx)
	defer cancel()
	if _, err := r.d1.Exec(dctx, finishRunSQL,
		string(p.Status), jsonOrNil(p.Output), nullable(p.Error), r.nowMillis(), p.RunID); err != nil {
		return fmt.Errorf("metering: finish outcome run: %w", err)
	}
	return nil
}

// CancelRun is FinishRun with RunCancelled: the run was aborted before an
// outcome existed (user cancel/stop). reason is stored as the run error.
func (r *Recorder) CancelRun(ctx context.Context, runID, reason string) error {
	return r.FinishRun(ctx, FinishRunParams{RunID: runID, Status: RunCancelled, Error: reason})
}

// StepStartParams opens one step attempt (or records a skipped step).
type StepStartParams struct {
	// RunID is the outcome run the step belongs to (required).
	RunID string
	// StepID is optional: generated when empty, honored when supplied.
	StepID string
	// StepName is the plan node id / step name (required).
	StepName string
	// NodeType is optional; an empty value stores SQL NULL.
	NodeType NodeType
	// Attempt is 0-based, matching the runtime worker's step logs.
	Attempt int
}

// StartStep appends the 'running' step row and returns its id. One row per
// attempt: a retry calls StartStep again instead of mutating the failed row.
func (r *Recorder) StartStep(ctx context.Context, p StepStartParams) (string, error) {
	stepID, err := validateStepStart("StartStep", p)
	if err != nil {
		return "", err
	}
	if !r.Enabled() {
		return stepID, ErrNoStore
	}

	dctx, cancel := writeCtx(ctx)
	defer cancel()
	if _, err := r.d1.Exec(dctx, insertStepSQL,
		stepID, p.RunID, p.StepName, nullableNodeType(p.NodeType), p.Attempt, r.nowMillis()); err != nil {
		return stepID, fmt.Errorf("metering: insert outcome run step: %w", err)
	}
	return stepID, nil
}

// SkipStep records a node the plan pruned: one 'skipped' row with completed_at
// and no started_at, exactly like the runtime's logSkipped.
func (r *Recorder) SkipStep(ctx context.Context, p StepStartParams) (string, error) {
	stepID, err := validateStepStart("SkipStep", p)
	if err != nil {
		return "", err
	}
	if !r.Enabled() {
		return stepID, ErrNoStore
	}

	dctx, cancel := writeCtx(ctx)
	defer cancel()
	if _, err := r.d1.Exec(dctx, insertSkippedStepSQL,
		stepID, p.RunID, p.StepName, nullableNodeType(p.NodeType), p.Attempt, r.nowMillis()); err != nil {
		return stepID, fmt.Errorf("metering: insert skipped outcome run step: %w", err)
	}
	return stepID, nil
}

// StepFinishParams closes one step attempt.
type StepFinishParams struct {
	StepID string
	// Status must be StepSucceeded or StepFailed. A pruned node is recorded
	// with SkipStep instead (it never starts, so it has no started_at).
	Status StepStatus
	// Output is the step result payload (optional).
	Output json.RawMessage
	// Error is the failure message ("" stores SQL NULL).
	Error string
}

// FinishStep closes one step attempt with its result. It is idempotent (one
// UPDATE keyed by step id).
func (r *Recorder) FinishStep(ctx context.Context, p StepFinishParams) error {
	if strings.TrimSpace(p.StepID) == "" {
		return errors.New("metering: FinishStep: step id is required")
	}
	switch p.Status {
	case StepSucceeded, StepFailed:
	default:
		return fmt.Errorf("metering: FinishStep: status %q must be %q or %q (use SkipStep for pruned nodes)",
			p.Status, StepSucceeded, StepFailed)
	}
	if !r.Enabled() {
		return ErrNoStore
	}

	dctx, cancel := writeCtx(ctx)
	defer cancel()
	if _, err := r.d1.Exec(dctx, finishStepSQL,
		string(p.Status), jsonOrNil(p.Output), nullable(p.Error), r.nowMillis(), p.StepID); err != nil {
		return fmt.Errorf("metering: finish outcome run step: %w", err)
	}
	return nil
}

// validateStepStart resolves the step id (generated when empty) and enforces
// the required fields shared by StartStep and SkipStep.
func validateStepStart(op string, p StepStartParams) (string, error) {
	if err := requireRunID(op, p.RunID); err != nil {
		return "", err
	}
	if strings.TrimSpace(p.StepName) == "" {
		return "", fmt.Errorf("metering: %s: step name is required", op)
	}
	stepID := strings.TrimSpace(p.StepID)
	if stepID == "" {
		stepID = uuid.NewString()
	}
	return stepID, nil
}
