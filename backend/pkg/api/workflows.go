package api

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"backend/pkg/cloudflare"
	"backend/pkg/engine"
)

// ---------- Workflow run endpoints ----------
//
// POST /api/workflows/trigger — validate the generated DAG, record the run
//   (workflow_instances 'pending' row), and start the Cloudflare Workflow
//   execution (wrangler.v2.jsonc [[workflows]] → VibeWorkflow runtime).
// GET  /api/workflows/:workflowId — the DAG plus its runs; with
//   ?instanceId=<id> the single run and its per-step logs
//   (workflow_step_logs rows written by the runtime worker).

// workflowTriggerRequest is the POST /api/workflows/trigger body.
type workflowTriggerRequest struct {
	WorkflowID string                 `json:"workflowId"`
	Input      map[string]interface{} `json:"input"`
}

// workflowInstanceLimit caps the run list of GET /api/workflows/:workflowId.
const workflowInstanceLimit = 20

// handleWorkflowTrigger implements POST /api/workflows/trigger.
//
// Sequence: (1) the workflow DAG must exist in workflow_dags and be in the
// 'generated' state; (2) a 'pending' workflow_instances row is inserted so
// the runtime worker can find the run when the Cloudflare event fires;
// (3) the Cloudflare Workflow instance is created via the REST API with the
// same instance id. If (3) fails the row is marked 'failed' so GET reflects
// reality instead of leaving a zombie 'pending' run.
func handleWorkflowTrigger(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		d1 := hub.D1Client()
		if d1 == nil {
			return c.Status(503).JSON(fiber.Map{"success": false, "error": "workflow persistence (D1) is not configured"})
		}
		cf := hub.CloudflareClient()
		workflowName := hub.WorkflowName()
		if cf == nil || workflowName == "" {
			return c.Status(503).JSON(fiber.Map{"success": false, "error": "workflow execution (Cloudflare) is not configured"})
		}

		var req workflowTriggerRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid JSON body"})
		}
		workflowID := strings.TrimSpace(req.WorkflowID)
		if workflowID == "" {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "workflowId is required"})
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		dag, err := engine.LoadWorkflowDag(ctx, d1, workflowID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": "database error"})
		}
		if dag == nil {
			return c.Status(404).JSON(fiber.Map{"success": false, "error": "workflow not found"})
		}
		if dag.Status != "generated" {
			return c.Status(409).JSON(fiber.Map{"success": false, "error": "workflow is not runnable (status: " + dag.Status + ")"})
		}

		instanceID := uuid.NewString()
		inputJSON, jerr := json.Marshal(req.Input)
		if jerr != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "input is not JSON-serializable"})
		}
		if err := engine.CreateWorkflowInstanceRow(ctx, d1, workflowID, instanceID, string(inputJSON)); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": "failed to record the workflow run"})
		}

		if _, cerr := cf.CreateWorkflowInstance(ctx, workflowName, instanceID, req.Input); cerr != nil {
			_ = engine.MarkWorkflowInstanceFailed(ctx, d1, instanceID, "failed to start execution: "+cerr.Error())
			return c.Status(502).JSON(fiber.Map{"success": false, "error": "failed to start workflow execution"})
		}

		return c.JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"workflowId": workflowID,
				"instanceId": instanceID,
				"status":     "pending",
			},
		})
	}
}

// handleWorkflowGet implements GET /api/workflows/:workflowId. Without a
// query it returns the DAG plus the workflow's most recent runs; with
// ?instanceId=<id> it returns that run and its per-step logs. While a run
// is still pending/running the live Cloudflare status is merged in
// best-effort (the runtime worker writes the terminal states itself).
func handleWorkflowGet(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		workflowID := c.Params("workflowId")
		if strings.TrimSpace(workflowID) == "" {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "missing workflow id"})
		}
		d1 := hub.D1Client()
		if d1 == nil {
			return c.Status(503).JSON(fiber.Map{"success": false, "error": "workflow persistence (D1) is not configured"})
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		dag, err := engine.LoadWorkflowDag(ctx, d1, workflowID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": "database error"})
		}
		if dag == nil {
			return c.Status(404).JSON(fiber.Map{"success": false, "error": "workflow not found"})
		}

		// Best-effort parse of the stored DAG so the frontend can render it
		// without re-parsing the VFS copy; malformed payloads come back as
		// the raw string instead of failing the whole request.
		workflowMap := fiber.Map{
			"workflowId":    dag.WorkflowID,
			"schemaVersion": dag.SchemaVersion,
			"status":        dag.Status,
			"updatedAt":     normalizeTimestamp(dag.UpdatedAt),
			"dag":           decodeJSONField(dag.DagJSON),
		}

		if instanceID := c.Query("instanceId"); strings.TrimSpace(instanceID) != "" {
			return handleWorkflowGetInstance(c, hub, d1, ctx, workflowMap, workflowID, instanceID)
		}

		rows, err := engine.ListWorkflowInstances(ctx, d1, workflowID, workflowInstanceLimit)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": "database error"})
		}
		instances := []fiber.Map{}
		for _, row := range rows {
			instances = append(instances, buildInstanceMap(row))
		}
		return c.JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"workflow":  workflowMap,
				"instances": instances,
			},
		})
	}
}

// handleWorkflowGetInstance serves the ?instanceId= branch of
// GET /api/workflows/:workflowId: one run plus its per-step logs.
func handleWorkflowGetInstance(c *fiber.Ctx, hub *engine.EngineHub, d1 *cloudflare.D1Client, ctx context.Context, workflowMap fiber.Map, workflowID, instanceID string) error {
	instance, err := engine.LoadWorkflowInstance(ctx, d1, workflowID, instanceID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": "database error"})
	}
	if instance == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "workflow instance not found"})
	}
	stepRows, serr := engine.LoadInstanceStepLogs(ctx, d1, instanceID)
	if serr != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": "database error"})
	}

	instanceMap := buildInstanceMap(*instance)
	instanceMap["stepCount"] = len(stepRows)
	steps := []fiber.Map{}
	for _, row := range stepRows {
		steps = append(steps, buildStepMap(row))
	}

	// Live status refresh: while the run is still pending/running in D1,
	// merge the Cloudflare runtime status (errored/complete/terminated) so
	// callers see progress between runtime D1 writes.
	if cf := hub.CloudflareClient(); cf != nil && hub.WorkflowName() != "" {
		if live, lerr := cf.GetWorkflowInstance(ctx, hub.WorkflowName(), instanceID); lerr == nil && live != nil {
			status := ""
			if s, ok := live["status"].(string); ok {
				status = s
			}
			if status != "" {
				if instance.Status == "pending" || instance.Status == "running" {
					if mapped := engine.MapCloudflareStatus(status); mapped != "pending" && mapped != "running" {
						instanceMap["status"] = mapped
					}
				}
				instanceMap["cloudflareStatus"] = status
			}
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"workflow": workflowMap,
			"instance": instanceMap,
			"steps":    steps,
		},
	})
}

// buildInstanceMap renders one workflow_instances row as the API object.
func buildInstanceMap(row engine.WorkflowInstanceRow) fiber.Map {
	return fiber.Map{
		"id":          row.ID,
		"workflowId":  row.WorkflowID,
		"status":      row.Status,
		"input":       decodeJSONField(row.Input),
		"output":      decodeJSONField(row.Output),
		"error":       nullableString(row.Error),
		"startedAt":   normalizeTimestamp(row.StartedAt),
		"completedAt": normalizeTimestamp(row.CompletedAt),
		"createdAt":   normalizeTimestamp(row.CreatedAt),
	}
}

// buildStepMap renders one workflow_step_logs row as the API object.
func buildStepMap(row engine.WorkflowStepRow) fiber.Map {
	return fiber.Map{
		"id":          row.ID,
		"stepName":    row.StepName,
		"nodeType":    nullableString(row.NodeType),
		"status":      row.Status,
		"attempt":     row.Attempt,
		"input":       decodeJSONField(row.Input),
		"output":      decodeJSONField(row.Output),
		"error":       nullableString(row.Error),
		"startedAt":   normalizeTimestamp(row.StartedAt),
		"completedAt": normalizeTimestamp(row.CompletedAt),
	}
}

// decodeJSONField parses a stored JSON column into its object/array value.
// Empty or non-JSON text (e.g. a scalar payload) is passed through as the
// raw string; SQL NULL becomes JSON null.
func decodeJSONField(raw string) interface{} {
	if strings.TrimSpace(raw) == "" || raw == "null" {
		return nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &obj); err == nil {
		return obj
	}
	var arr []interface{}
	if err := json.Unmarshal([]byte(raw), &arr); err == nil {
		return arr
	}
	return raw
}

// nullableString renders an empty text column as JSON null.
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// normalizeTimestamp converts D1 CURRENT_TIMESTAMP epoch-seconds values to
// epoch milliseconds to match the runtime worker's Date.now() convention
// (started_at/completed_at are written in ms). Values >= 1e12 are already
// in ms and pass through; nil passes through as null.
func normalizeTimestamp(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	f, ok := v.(float64)
	if !ok {
		return v
	}
	if f > 0 && f < 1e12 {
		return f * 1000
	}
	return f
}