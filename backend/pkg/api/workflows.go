package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"backend/pkg/cloudflare"
	"backend/pkg/composition"
	"backend/pkg/engine"
	"backend/pkg/plan"
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
	// SnapshotID optionally binds the run to a stored composition snapshot
	// (ADI-FOUND-004b). Empty means unbound, which is allowed until
	// CONTROL_PLANE_REQUIRE_SNAPSHOT makes binding mandatory.
	SnapshotID string `json:"snapshotId,omitempty"`
	// System optionally carries the declarative System manifest the run is
	// produced from (ADI-FOUND-004c). When present the handler resolves it
	// against the bundled source, persists the resulting snapshot and
	// compiles the plan before anything durable is written; the response
	// names the snapshot the run is bound to. Sending it together with
	// snapshotId is refused: binding to a stored snapshot and producing a
	// new one from a document are different intents, and silently picking
	// one would hide which composition the run actually carries.
	System json.RawMessage `json:"system,omitempty"`
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
//
// Tenant scope (P0.7): the route runs workflowGuard (routes.go) after the
// session boundary, so only the workflow's owner reaches this handler; the
// recorded run copies the DAG's owner (dag.UserID) as its tenant.
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
		// P0.7 + P0-SEC-005: the run carries the tenant workflowGuard resolved
		// and authorized, so the runtime can always compare it against the DAG.
		// Legacy rows (workflow_dags.user_id NULL) are authorized through the
		// room-ownership fallback — stamping the raw dag.UserID there would
		// record a NULL tenant and make the runtime's mismatch check vacuous.
		runTenant := dag.UserID
		if resolved, ok := c.Locals(workflowOwnerLocals).(string); ok && resolved != "" {
			runTenant = resolved
		}
		// ADI-FOUND-004b: admit the composition snapshot before any row exists. In
		// soft mode (store installed, binding not required) an unbound run is still
		// allowed, so this changes nothing until the rollout switch is flipped.
		snapshotID := strings.TrimSpace(req.SnapshotID)
		if len(req.System) > 0 && snapshotID != "" {
			// ADI-FOUND-004c: binding and producing are different intents.
			// Silently picking one would hide which composition the run
			// actually carries, so the ambiguity is refused instead.
			return c.Status(400).JSON(fiber.Map{
				"success": false,
				"error":   "pass either snapshotId or system, not both: binding to a stored snapshot and producing one from a document are different intents",
			})
		}
		// ADI-FOUND-004c: the snapshot producer. An inline System document is
		// resolved → persisted → compiled before anything durable is written,
		// so CONTROL_PLANE_REQUIRE_SNAPSHOT is usable without a separate
		// producer service: the run starts bound to the snapshot it names, and
		// the plan identity below is recomputed, never stored as a blob.
		planKind, planDigest, planCompiler := "", "", ""
		if len(req.System) > 0 {
			id, kind, digest, compiler, fail := produceRunSnapshot(ctx, hub, runTenant, req.System)
			if fail != nil {
				return renderTriggerRefusal(c, *fail)
			}
			snapshotID, planKind, planDigest, planCompiler = id, kind, digest, compiler
		}
		if _, findings := hub.AdmitRunSnapshot(ctx, snapshotID, runTenant); len(findings) > 0 {
			status := fiber.StatusConflict
			switch findings[0].Code {
			case composition.CodeTenant:
				status = fiber.StatusForbidden
			case composition.CodeSource:
				status = fiber.StatusServiceUnavailable
			}
			return c.Status(status).JSON(fiber.Map{
				"success": false,
				"error":   findings[0].Message,
				"code":    string(findings[0].Code),
			})
		}

		if err := engine.CreateWorkflowInstanceRow(ctx, d1, workflowID, instanceID, runTenant, string(inputJSON), snapshotID, planKind, planDigest, planCompiler); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": "failed to record the workflow run"})
		}

		if _, cerr := cf.CreateWorkflowInstance(ctx, workflowName, instanceID, req.Input); cerr != nil {
			_ = engine.MarkWorkflowInstanceFailed(ctx, d1, instanceID, "failed to start execution: "+cerr.Error())
			return c.Status(502).JSON(fiber.Map{"success": false, "error": "failed to start workflow execution"})
		}

		run := fiber.Map{
			"workflowId": workflowID,
			"instanceId": instanceID,
			"status":     "pending",
		}
		if snapshotID != "" {
			run["snapshotId"] = snapshotID
		}
		if planKind != "" {
			run["planKind"] = planKind
			run["planDigest"] = planDigest
			run["compilerVersion"] = planCompiler
		}
		return c.JSON(fiber.Map{
			"success": true,
			"data":    run,
		})
	}
}

// triggerRefusal is one shaped trigger failure: the HTTP status, the stable
// machine code and the human message. code may be empty for infrastructure
// failures that have no composition vocabulary.
type triggerRefusal struct {
	status int
	code   string
	err    string
}

// renderTriggerRefusal writes a refusal in the exact shape every snapshot
// refusal has used since ADI-FOUND-004b: {"success":false,"error":…,"code":…}.
func renderTriggerRefusal(c *fiber.Ctx, refusal triggerRefusal) error {
	body := fiber.Map{"success": false, "error": refusal.err}
	if refusal.code != "" {
		body["code"] = refusal.code
	}
	return c.Status(refusal.status).JSON(body)
}

// produceRunSnapshot is the snapshot producer (ADI-FOUND-004c): resolve an
// inline System document against the bundled source, persist the resulting
// snapshot and compile its plan — the first three steps of the resolve →
// persist → compile → start flow; "start" is the trigger that follows.
//
// Ordering rules this implements (docs/architecture/run-snapshot.md §2, §6):
//
//   - the snapshot row exists before the run is admitted (the caller admits
//     right after this returns, against the id produced here);
//   - identity is the lock digest, so an unchanged closure re-persists as a
//     no-op and two tenants cannot share a snapshot (first-writer-wins);
//   - a compile refusal leaves no run behind. The snapshot itself stays when
//     resolution succeeded: it is valid, tenant-attributed content that a
//     future runtime (the agentic ADR) may run, and nothing references it.
//
// An empty tenant is refused: a snapshot nobody can attribute cannot be
// admitted later, so producing it would create unreadable content.
func produceRunSnapshot(ctx context.Context, hub *engine.EngineHub, tenant string, systemDoc []byte) (snapshotID, planKind, planDigest, compilerVersion string, refused *triggerRefusal) {
	store := hub.SnapshotStore()
	if store == nil {
		return "", "", "", "", &triggerRefusal{
			status: fiber.StatusServiceUnavailable,
			err:    "snapshot persistence (D1) is not configured; a System document cannot be produced into a snapshot",
		}
	}
	source := hub.CompositionSource()
	if source == nil {
		return "", "", "", "", &triggerRefusal{
			status: fiber.StatusServiceUnavailable,
			err:    "composition source is not configured; a System document cannot be resolved",
		}
	}
	if strings.TrimSpace(tenant) == "" {
		return "", "", "", "", &triggerRefusal{
			status: fiber.StatusBadRequest,
			err:    "the run's tenant is unknown; a composition snapshot must be attributable to a tenant",
		}
	}

	result := composition.Resolve(ctx, composition.Request{
		Manifest:        systemDoc,
		Mode:            composition.ModeBundled,
		Source:          source,
		CompilerVersion: composition.CompilerVersion,
		Now:             time.Now().UTC(),
	})
	if len(result.Findings) > 0 {
		status := fiber.StatusConflict
		switch result.Findings[0].Code {
		case composition.CodeIR:
			// The document is not a valid System manifest: the caller's fault.
			status = fiber.StatusBadRequest
		case composition.CodeSource:
			status = fiber.StatusServiceUnavailable
		}
		return "", "", "", "", &triggerRefusal{
			status: status,
			code:   string(result.Findings[0].Code),
			err:    result.Findings[0].Message,
		}
	}

	snapshot, err := composition.NewSnapshot(result.Lock, tenant, "")
	if err != nil {
		return "", "", "", "", &triggerRefusal{status: fiber.StatusInternalServerError, err: err.Error()}
	}
	if err := store.Persist(ctx, *snapshot); err != nil {
		if errors.Is(err, composition.ErrSnapshotTenant) {
			return "", "", "", "", &triggerRefusal{
				status: fiber.StatusConflict,
				code:   string(composition.CodeTenant),
				err:    "an identical composition is already recorded for another tenant (first-writer-wins)",
			}
		}
		return "", "", "", "", &triggerRefusal{
			status: fiber.StatusServiceUnavailable,
			code:   string(composition.CodeSource),
			err:    "failed to record the composition snapshot",
		}
	}

	compiled, findings := plan.Compile(snapshot, systemDoc)
	if len(findings) > 0 {
		return "", "", "", "", &triggerRefusal{
			status: fiber.StatusConflict,
			code:   string(findings[0].Code),
			err:    findings[0].Message,
		}
	}
	return snapshot.SnapshotID, string(compiled.Kind), compiled.PlanDigest, compiled.CompilerVersion, nil
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

// ---------- Run control endpoints (P7-RUN-001c) ----------
//
// POST /api/workflows/cancel   — terminate a run (capability: cancel)
// POST /api/workflows/pause    — hold a run at its next safe boundary
// POST /api/workflows/resume   — continue a paused run
// GET  /api/workflows/capabilities — what the deployed runtime advertises
//
// All three control routes carry the same guard chain as trigger: the session
// boundary first, then workflowGuard, which resolves the addressed workflow's
// tenant from the JSON body's workflowId. The instance is then loaded *scoped to
// that workflow*, so a run id from another workflow (or tenant) is a 404 and
// never a cross-tenant control channel.

// workflowControlRequest is the POST /api/workflows/{cancel,pause,resume} body.
type workflowControlRequest struct {
	WorkflowID string `json:"workflowId"`
	InstanceID string `json:"instanceId"`
	Reason     string `json:"reason"`
}

// handleWorkflowControl implements the three control actions with one handler, so
// their guards, scoping and finality rules cannot drift.
func handleWorkflowControl(hub *engine.EngineHub, action string) fiber.Handler {
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

		var req workflowControlRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid JSON body"})
		}
		workflowID := strings.TrimSpace(req.WorkflowID)
		instanceID := strings.TrimSpace(req.InstanceID)
		if workflowID == "" || instanceID == "" {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "workflowId and instanceId are required"})
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Scoped load: a run belonging to another workflow is indistinguishable
		// from a missing one (no enumeration across tenants).
		instance, err := engine.LoadWorkflowInstance(ctx, d1, workflowID, instanceID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": "database error"})
		}
		if instance == nil {
			return c.Status(404).JSON(fiber.Map{"success": false, "error": "workflow instance not found"})
		}

		// Terminal states are final (contract rule 5): a finished run is never
		// cancelled, paused or resumed after the fact.
		if engine.WorkflowInstanceTerminal(instance.Status) {
			return c.Status(409).JSON(fiber.Map{
				"success": false,
				"error":   "run is already " + instance.Status + "; terminal states are final",
			})
		}

		var actionErr error
		switch action {
		case "cancel":
			actionErr = cf.TerminateWorkflowInstance(ctx, workflowName, instanceID, req.Reason)
		case "pause":
			if instance.Status != "running" {
				return c.Status(409).JSON(fiber.Map{"success": false, "error": "only a running run can be paused (status: " + instance.Status + ")"})
			}
			actionErr = cf.PauseWorkflowInstance(ctx, workflowName, instanceID)
		case "resume":
			if instance.Status != "paused" {
				return c.Status(409).JSON(fiber.Map{"success": false, "error": "only a paused run can be resumed (status: " + instance.Status + ")"})
			}
			actionErr = cf.ResumeWorkflowInstance(ctx, workflowName, instanceID)
		default:
			return c.Status(500).JSON(fiber.Map{"success": false, "error": "unknown workflow control action"})
		}
		if actionErr != nil {
			// The runtime refused or the API failed: nothing was recorded, so the
			// durable row still describes reality.
			log.Printf("[api] workflow %s failed for %s: %v", action, instanceID, actionErr)
			return c.Status(502).JSON(fiber.Map{"success": false, "error": "workflow runtime refused the " + action})
		}

		status := instance.Status
		switch action {
		case "cancel":
			status = "cancelled"
			if err := engine.MarkWorkflowInstanceCancelled(ctx, d1, instanceID, req.Reason); err != nil {
				log.Printf("[api] record cancel for %s: %v", instanceID, err)
			}
		case "pause":
			status = "paused"
			if err := engine.MarkWorkflowInstancePaused(ctx, d1, instanceID); err != nil {
				log.Printf("[api] record pause for %s: %v", instanceID, err)
			}
		case "resume":
			status = "running"
			if err := engine.MarkWorkflowInstanceResumed(ctx, d1, instanceID); err != nil {
				log.Printf("[api] record resume for %s: %v", instanceID, err)
			}
		}

		return c.JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"action":     action,
				"workflowId": workflowID,
				"instanceId": instanceID,
				"status":     status,
			},
		})
	}
}

// handleWorkflowCapabilities serves GET /api/workflows/capabilities: the
// advertisement a caller must read before relying on cancel, pause or history.
func handleWorkflowCapabilities(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		caps := engine.WorkflowCapabilities()
		return c.JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"cancel":    caps.Cancel,
				"pause":     caps.Pause,
				"longSleep": caps.LongSleep,
				"history":   caps.History,
			},
		})
	}
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
