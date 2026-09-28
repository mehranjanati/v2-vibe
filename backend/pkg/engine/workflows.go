package engine

import (
	"context"
	"fmt"

	"backend/pkg/cloudflare"
)

// ---------- Workflow run persistence (workflow_* D1 tables) ----------
//
// The runtime worker (worker/workflow/VibeWorkflow.ts) owns execution: it
// advances workflow_instances running → succeeded|failed and writes the
// per-step log rows into workflow_step_logs. The control plane only:
//
//   - creates the pending workflow_instances row (POST /api/workflows/trigger)
//   - reads the DAG, instances and per-step logs back (GET /api/workflows/:id)
//   - records the startup failure when the Cloudflare Workflow cannot be
//     created (e.g. bad DAG or the runtime worker is not deployed)
//
// D1 columns arrive as interface{} (numbers decode as float64, strings as
// string), so rows are normalized through small typed getters.

// WorkflowDagRow is one workflow_dags row read back from D1.
type WorkflowDagRow struct {
	WorkflowID    string
	SchemaVersion int
	DagJSON       string
	Status        string
	UpdatedAt     interface{} // epoch seconds (D1 CURRENT_TIMESTAMP)
}

// WorkflowInstanceRow is one workflow_instances row read back from D1.
type WorkflowInstanceRow struct {
	ID          string
	WorkflowID  string
	Status      string
	Input       string
	Output      string
	Error       string
	StartedAt   interface{} // epoch ms (runtime worker Date.now())
	CompletedAt interface{} // epoch ms (runtime worker Date.now())
	CreatedAt   interface{} // epoch seconds (D1 CURRENT_TIMESTAMP)
}

// WorkflowStepRow is one workflow_step_logs row read back from D1.
type WorkflowStepRow struct {
	ID          string
	InstanceID  string
	StepName    string
	NodeType    string
	Status      string
	Attempt     int
	Input       string
	Output      string
	Error       string
	StartedAt   interface{} // epoch ms (runtime worker Date.now())
	CompletedAt interface{} // epoch ms (runtime worker Date.now())
}

// wfStr reads a string column (""); nil and non-strings become "".
func wfStr(row map[string]interface{}, key string) string {
	v, ok := row[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// wfInt reads a numeric column (D1 numbers decode as float64).
func wfInt(row map[string]interface{}, key string) int {
	v, ok := row[key]
	if !ok || v == nil {
		return 0
	}
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

// wfAny returns the raw column value (nil when the column is SQL NULL).
func wfAny(row map[string]interface{}, key string) interface{} {
	v, _ := row[key]
	return v
}

// LoadWorkflowDag fetches the workflow_dags row for workflowID; nil when the
// workflow does not exist.
func LoadWorkflowDag(ctx context.Context, d1 *cloudflare.D1Client, workflowID string) (*WorkflowDagRow, error) {
	rows, err := d1.Query(ctx,
		"SELECT workflow_id, schema_version, dag_json, status, updated_at FROM workflow_dags WHERE workflow_id = ? LIMIT 1",
		workflowID)
	if err != nil {
		return nil, fmt.Errorf("d1: load workflow_dags: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	row := rows[0]
	return &WorkflowDagRow{
		WorkflowID:    wfStr(row, "workflow_id"),
		SchemaVersion: wfInt(row, "schema_version"),
		DagJSON:       wfStr(row, "dag_json"),
		Status:        wfStr(row, "status"),
		UpdatedAt:     wfAny(row, "updated_at"),
	}, nil
}

// CreateWorkflowInstanceRow inserts the pending workflow_instances row a
// run starts from. The runtime worker loads this row by id (workflow_id →
// workflow_dags FK) when the Cloudflare event fires.
func CreateWorkflowInstanceRow(ctx context.Context, d1 *cloudflare.D1Client, workflowID, instanceID, inputJSON string) error {
	ok, err := d1.Exec(ctx,
		"INSERT INTO workflow_instances (id, workflow_id, status, input, created_at) VALUES (?, ?, 'pending', ?, CURRENT_TIMESTAMP)",
		instanceID, workflowID, inputJSON)
	if err != nil || !ok {
		return fmt.Errorf("d1: insert workflow_instances: %v", err)
	}
	return nil
}

// MarkWorkflowInstanceFailed records a startup failure (the Cloudflare
// Workflow could not be created) on a still-pending instance row.
func MarkWorkflowInstanceFailed(ctx context.Context, d1 *cloudflare.D1Client, instanceID, message string) error {
	ok, err := d1.Exec(ctx,
		"UPDATE workflow_instances SET status = 'failed', error = ?, completed_at = COALESCE(completed_at, CURRENT_TIMESTAMP), updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = 'pending'",
		message, instanceID)
	if err != nil || !ok {
		return fmt.Errorf("d1: fail workflow_instances: %v", err)
	}
	return nil
}

// LoadWorkflowInstance fetches one instance row scoped to its workflow;
// nil when the run does not exist.
func LoadWorkflowInstance(ctx context.Context, d1 *cloudflare.D1Client, workflowID, instanceID string) (*WorkflowInstanceRow, error) {
	rows, err := d1.Query(ctx,
		"SELECT id, workflow_id, status, input, output, error, started_at, completed_at, created_at FROM workflow_instances WHERE workflow_id = ? AND id = ? LIMIT 1",
		workflowID, instanceID)
	if err != nil {
		return nil, fmt.Errorf("d1: load workflow_instances: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return instanceRowFrom(rows[0]), nil
}

// ListWorkflowInstances returns the workflow's most recent runs, newest
// first, capped at limit (1..100).
func ListWorkflowInstances(ctx context.Context, d1 *cloudflare.D1Client, workflowID string, limit int) ([]WorkflowInstanceRow, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := d1.Query(ctx,
		"SELECT id, workflow_id, status, input, output, error, started_at, completed_at, created_at FROM workflow_instances WHERE workflow_id = ? ORDER BY created_at DESC, id DESC LIMIT ?",
		workflowID, limit)
	if err != nil {
		return nil, fmt.Errorf("d1: list workflow_instances: %w", err)
	}
	out := make([]WorkflowInstanceRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, *instanceRowFrom(row))
	}
	return out, nil
}

// instanceRowFrom normalizes one workflow_instances D1 row.
func instanceRowFrom(row map[string]interface{}) *WorkflowInstanceRow {
	return &WorkflowInstanceRow{
		ID:          wfStr(row, "id"),
		WorkflowID:  wfStr(row, "workflow_id"),
		Status:      wfStr(row, "status"),
		Input:       wfStr(row, "input"),
		Output:      wfStr(row, "output"),
		Error:       wfStr(row, "error"),
		StartedAt:   wfAny(row, "started_at"),
		CompletedAt: wfAny(row, "completed_at"),
		CreatedAt:   wfAny(row, "created_at"),
	}
}

// LoadInstanceStepLogs returns the per-step log rows of one run in
// execution order. Each DAG node logs one row per attempt (running →
// succeeded | failed; skipped rows are written for condition-pruned nodes).
func LoadInstanceStepLogs(ctx context.Context, d1 *cloudflare.D1Client, instanceID string) ([]WorkflowStepRow, error) {
	rows, err := d1.Query(ctx,
		"SELECT id, instance_id, step_name, node_type, status, attempt, input, output, error, started_at, completed_at FROM workflow_step_logs WHERE instance_id = ? ORDER BY started_at ASC, created_at ASC",
		instanceID)
	if err != nil {
		return nil, fmt.Errorf("d1: load workflow_step_logs: %w", err)
	}
	out := make([]WorkflowStepRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, WorkflowStepRow{
			ID:          wfStr(row, "id"),
			InstanceID:  wfStr(row, "instance_id"),
			StepName:    wfStr(row, "step_name"),
			NodeType:    wfStr(row, "node_type"),
			Status:      wfStr(row, "status"),
			Attempt:     wfInt(row, "attempt"),
			Input:       wfStr(row, "input"),
			Output:      wfStr(row, "output"),
			Error:       wfStr(row, "error"),
			StartedAt:   wfAny(row, "started_at"),
			CompletedAt: wfAny(row, "completed_at"),
		})
	}
	return out, nil
}

// MapCloudflareStatus translates a Cloudflare Workflows instance status
// (queued | running | paused | errored | terminated | complete) into the
// workflow_instances vocabulary (pending | running | succeeded | failed |
// cancelled) used by the runtime worker. Unknown statuses pass through.
func MapCloudflareStatus(status string) string {
	switch status {
	case "queued":
		return "pending"
	case "paused":
		return "running"
	case "errored":
		return "failed"
	case "complete":
		return "succeeded"
	case "terminated":
		return "cancelled"
	}
	return status
}
