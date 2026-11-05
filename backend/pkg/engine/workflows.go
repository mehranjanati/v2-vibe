package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

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
	UserID        string      // Owning user id (P0.7); "" when the row is unowned
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
		"SELECT workflow_id, schema_version, dag_json, status, user_id, updated_at FROM workflow_dags WHERE workflow_id = ? LIMIT 1",
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
		UserID:        wfStr(row, "user_id"),
		UpdatedAt:     wfAny(row, "updated_at"),
	}, nil
}

// ---------- Workflow tenant scope (P0.7) ----------
//
// A workflow's tenant lives in workflow_dags.user_id, written by the DAG
// upsert from the room's P0.4 owner claim (see (*ProjectRoom).persistWorkflowDag).
// The route guard in backend/pkg/api resolves it through WorkflowOwner below;
// rows written before the column existed fall back to the same claim in the
// Redis `room:owner:` registry, so an upgrade does not orphan existing
// workflows. A row that records no owner at all is unowned → 404 (never leak
// another tenant's existence), exactly like an unowned room in P0.4.

// RoomOwnerLookup resolves the P0.4 ownership record of a chat/project id.
// It is satisfied by (*EngineHub).RoomOwner. A non-nil error means the
// ownership store is unreachable; callers must fail closed (503) rather than
// treat it as "unowned".
type RoomOwnerLookup func(chatID string) (owner string, claimed bool, err error)

// WorkflowOwner resolves the user id that owns workflowID.
//
// Resolution order:
//  1. workflow_dags.user_id (authoritative; written at DAG upsert);
//  2. the P0.4 room ownership record of the chat the workflow id names
//     (migration fallback for rows written before the column existed);
//  3. no owner recorded → (found=false) → the route answers 404.
//
// A workflow that has no workflow_dags row is reported as not found, so a
// caller never learns whether another tenant's workflow exists. D1 and the
// ownership registry are both fail-closed: any lookup error is returned and
// the guard answers 503.
func WorkflowOwner(ctx context.Context, d1 *cloudflare.D1Client, workflowID string, roomOwner RoomOwnerLookup) (string, bool, error) {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return "", false, nil
	}
	if d1 == nil {
		return "", false, fmt.Errorf("d1: workflow ownership store is not configured")
	}

	rows, err := d1.Query(ctx,
		"SELECT user_id FROM workflow_dags WHERE workflow_id = ? LIMIT 1", workflowID)
	if err != nil {
		return "", false, fmt.Errorf("d1: load workflow_dags user_id: %w", err)
	}
	if len(rows) == 0 {
		return "", false, nil // no such workflow → 404, existence never leaked
	}
	if recorded := strings.TrimSpace(wfStr(rows[0], "user_id")); recorded != "" {
		return recorded, true, nil
	}

	if roomOwner == nil {
		return "", false, nil
	}
	owner, claimed, err := roomOwner(workflowID)
	if err != nil {
		return "", false, err
	}
	if !claimed || strings.TrimSpace(owner) == "" {
		return "", false, nil // unowned → 404
	}
	return strings.TrimSpace(owner), true, nil
}

// WorkflowAccessDenial is one rejected cross-tenant workflow access attempt
// (the P0.7 audit record). Operation is the HTTP method of the attempt.
type WorkflowAccessDenial struct {
	WorkflowID string
	CallerID   string
	OwnerID    string
	Operation  string
	IPAddress  string
	UserAgent  string
}

// workflowAccessDeniedAction is the audit_logs.action of a rejected
// cross-tenant workflow access (P0.7).
const workflowAccessDeniedAction = "workflow.access_denied"

// RecordWorkflowAccessDenial appends one cross-tenant workflow access attempt
// to D1 audit_logs (entity_type 'workflow', entity_id = workflow id,
// action 'workflow.access_denied', new_values = {operation, owner, caller}).
// The caller id is the audit subject (user_id); when the caller is unknown to
// D1 the insert is rejected by the FK and reported as an error.
func RecordWorkflowAccessDenial(ctx context.Context, d1 *cloudflare.D1Client, denial WorkflowAccessDenial) error {
	if d1 == nil {
		return fmt.Errorf("d1: audit store is not configured")
	}
	detail, err := json.Marshal(map[string]string{
		"operation": denial.Operation,
		"owner":     denial.OwnerID,
		"caller":    denial.CallerID,
	})
	if err != nil {
		return fmt.Errorf("d1: marshal workflow access denial: %w", err)
	}
	ok, err := d1.Exec(ctx,
		"INSERT INTO audit_logs (id, user_id, entity_type, entity_id, action, new_values, ip_address, user_agent, created_at) "+
			"VALUES (?, ?, 'workflow', ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), CURRENT_TIMESTAMP)",
		uuid.NewString(), denial.CallerID, denial.WorkflowID, workflowAccessDeniedAction, string(detail),
		denial.IPAddress, denial.UserAgent)
	if err != nil || !ok {
		return fmt.Errorf("d1: insert audit_logs: %v", err)
	}
	return nil
}

// CreateWorkflowInstanceRow inserts the pending workflow_instances row a
// run starts from. The runtime worker loads this row by id (workflow_id →
// workflow_dags FK) when the Cloudflare event fires. ownerID is the run's
// tenant (P0.7) — the DAG's owner, copied here so the runtime can refuse to
// execute a run whose tenant no longer matches the DAG it points at; an
// empty ownerID is stored as SQL NULL (unowned, pre-P0.7 behaviour).
func CreateWorkflowInstanceRow(ctx context.Context, d1 *cloudflare.D1Client, workflowID, instanceID, ownerID, inputJSON string) error {
	ok, err := d1.Exec(ctx,
		"INSERT INTO workflow_instances (id, workflow_id, status, user_id, input, created_at) VALUES (?, ?, 'pending', NULLIF(?, ''), ?, CURRENT_TIMESTAMP)",
		instanceID, workflowID, ownerID, inputJSON)
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
