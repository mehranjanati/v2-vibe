package api

// P7.12 — route-level proof for the run-control surface (P7-RUN-001c).
//
// The contract these tests pin (docs/architecture/adr-execution-provider-contract.md,
// DEV_CHECKLIST P7.12):
//   - the three control endpoints sit behind the session guard and the same
//     workflow ownership guard as trigger/get (401 anonymous, 403 cross-tenant
//     + audit, 404 unknown/foreign instance, never an enumeration oracle);
//   - terminal states are final: no cancel/pause/resume after the fact (409);
//   - the durable state is written ONLY after the runtime accepted the action,
//     so a refusal (502) leaves the row describing reality;
//   - pause/resume are transition-guarded (running → paused, paused → running);
//   - the capability advertisement the caller must read is served and honest.
//
// Everything runs over the emulated D1 + the emulated Workflows API, so the
// production handler, guards, client and SQL run unmodified.

import (
	"net/http"
	"strings"
	"testing"
)

// controlRequest posts one control action with the given body.
func controlRequest(t *testing.T, env *workflowEnv, path, token, workflowID, instanceID string) (int, map[string]any) {
	t.Helper()
	return workflowRequest(t, env.app, http.MethodPost, path, token, map[string]any{
		"workflowId": workflowID,
		"instanceId": instanceID,
	})
}

// TestWorkflowControlRequiresSession: every control route — including the
// capability advertisement — answers 401 without a session and never reaches
// the runtime or the durable store.
func TestWorkflowControlRequiresSession(t *testing.T) {
	env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	env.d1.seedRun("inst-1", "wf-a", "pending")

	for _, path := range []string{
		"/api/workflows/cancel",
		"/api/workflows/pause",
		"/api/workflows/resume",
	} {
		status, _ := controlRequest(t, env, path, "", "wf-a", "inst-1")
		if status != http.StatusUnauthorized {
			t.Fatalf("anonymous %s: expected 401, got %d", path, status)
		}
	}
	if status, _ := workflowRequest(t, env.app, http.MethodGet, "/api/workflows/capabilities", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("anonymous capabilities: expected 401, got %d", status)
	}

	if actions := env.d1.controlActions(); len(actions) != 0 {
		t.Fatalf("an anonymous control request must not reach the runtime, got %v", actions)
	}
	if writes := env.d1.stateWritesSnapshot(); len(writes) != 0 {
		t.Fatalf("an anonymous control request must not write state, got %v", writes)
	}
}

// TestWorkflowCapabilitiesAdvertised: the endpoint a caller must read before
// relying on cancel/pause/history answers with the runtime's own advertisement.
func TestWorkflowCapabilitiesAdvertised(t *testing.T) {
	env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})

	status, body := workflowRequest(t, env.app, http.MethodGet, "/api/workflows/capabilities", "token-a", nil)
	if status != http.StatusOK {
		t.Fatalf("capabilities: expected 200, got %d (%v)", status, body)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities response carries no data object: %v", body)
	}
	for _, capability := range []string{"cancel", "pause", "longSleep", "history"} {
		if data[capability] != true {
			t.Fatalf("capability %q must be advertised as true, got %v", capability, data[capability])
		}
	}
}

// TestWorkflowControlCrossTenantDenied: another tenant's control attempt is
// refused before the instance is even loaded, and is audited like any other
// workflow access denial.
func TestWorkflowControlCrossTenantDenied(t *testing.T) {
	env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	env.d1.seedRun("inst-1", "wf-a", "running")

	status, _ := controlRequest(t, env, "/api/workflows/cancel", "token-b", "wf-a", "inst-1")
	if status != http.StatusForbidden {
		t.Fatalf("cross-tenant cancel: expected 403, got %d", status)
	}

	audits := env.d1.auditRows()
	if len(audits) != 1 || audits[0]["action"] != "workflow.access_denied" {
		t.Fatalf("expected one access-denial audit row, got %v", audits)
	}
	if actions := env.d1.controlActions(); len(actions) != 0 {
		t.Fatalf("a denied control must not reach the runtime, got %v", actions)
	}
	if writes := env.d1.stateWritesSnapshot(); len(writes) != 0 {
		t.Fatalf("a denied control must not write state, got %v", writes)
	}
}

// TestWorkflowControlUnknownOrForeignInstanceIs404: an instance that does not
// belong to the addressed workflow is indistinguishable from a missing one.
func TestWorkflowControlUnknownOrForeignInstanceIs404(t *testing.T) {
	env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{
		"wf-a": generatedDagRow("user-a"),
		"wf-b": generatedDagRow("user-a"),
	})
	env.d1.seedRun("inst-foreign", "wf-b", "running")

	status, _ := controlRequest(t, env, "/api/workflows/cancel", "token-a", "wf-a", "inst-missing")
	if status != http.StatusNotFound {
		t.Fatalf("unknown instance: expected 404, got %d", status)
	}
	status, _ = controlRequest(t, env, "/api/workflows/cancel", "token-a", "wf-a", "inst-foreign")
	if status != http.StatusNotFound {
		t.Fatalf("an instance of another workflow: expected 404, got %d", status)
	}
	if actions := env.d1.controlActions(); len(actions) != 0 {
		t.Fatalf("a 404 must not reach the runtime, got %v", actions)
	}
}

// TestWorkflowControlTerminalStatesAreFinal: a finished run is never cancelled,
// paused or resumed after the fact (contract rule 5).
func TestWorkflowControlTerminalStatesAreFinal(t *testing.T) {
	for _, terminal := range []string{"succeeded", "failed", "cancelled"} {
		t.Run(terminal, func(t *testing.T) {
			env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
			env.d1.seedRun("inst-1", "wf-a", terminal)

			for _, path := range []string{
				"/api/workflows/cancel",
				"/api/workflows/pause",
				"/api/workflows/resume",
			} {
				status, _ := controlRequest(t, env, path, "token-a", "wf-a", "inst-1")
				if status != http.StatusConflict {
					t.Fatalf("%s on a %s run: expected 409, got %d", path, terminal, status)
				}
			}
			if actions := env.d1.controlActions(); len(actions) != 0 {
				t.Fatalf("a terminal run must not reach the runtime, got %v", actions)
			}
			if writes := env.d1.stateWritesSnapshot(); len(writes) != 0 {
				t.Fatalf("a terminal run must not be rewritten, got %v", writes)
			}
		})
	}
}

// TestWorkflowControlCancelRecordsOnlyAfterTheRuntimeAccepts is the sharpest
// half of the contract: acceptance writes `cancelled`, a runtime refusal leaves
// the row untouched and answers 502.
func TestWorkflowControlCancelRecordsOnlyAfterTheRuntimeAccepts(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
		env.d1.seedRun("inst-1", "wf-a", "running")

		status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/cancel", "token-a", map[string]any{
			"workflowId": "wf-a", "instanceId": "inst-1", "reason": "operator stopped it",
		})
		if status != http.StatusOK {
			t.Fatalf("cancel: expected 200, got %d (%v)", status, body)
		}
		actions := env.d1.controlActions()
		if len(actions) != 1 || actions[0]["action"] != "terminate" || actions[0]["instance_id"] != "inst-1" {
			t.Fatalf("expected one terminate for inst-1, got %v", actions)
		}
		if actions[0]["reason"] != "operator stopped it" {
			t.Fatalf("the operator's reason must reach the runtime, got %v", actions[0]["reason"])
		}
		writes := env.d1.stateWritesSnapshot()
		if len(writes) != 1 || writes[0]["status"] != "cancelled" || writes[0]["instance_id"] != "inst-1" {
			t.Fatalf("expected one cancelled write for inst-1, got %v", writes)
		}
		if reason, _ := writes[0]["reason"].(string); !strings.Contains(reason, "operator stopped it") {
			t.Fatalf("the recorded reason must be the operator's, got %q", reason)
		}
	})

	t.Run("refused by the runtime", func(t *testing.T) {
		env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
		env.d1.seedRun("inst-1", "wf-a", "running")
		env.d1.setFailControl(true)

		status, body := controlRequest(t, env, "/api/workflows/cancel", "token-a", "wf-a", "inst-1")
		if status != http.StatusBadGateway {
			t.Fatalf("a runtime refusal: expected 502, got %d (%v)", status, body)
		}
		if writes := env.d1.stateWritesSnapshot(); len(writes) != 0 {
			t.Fatalf("a refused cancel must leave the row describing reality, got %v", writes)
		}
	})
}

// TestWorkflowControlPauseAndResumeAreTransitionGuarded: pause only from
// running, resume only from paused — the durable row can never claim a state
// the runtime was not asked to enter.
func TestWorkflowControlPauseAndResumeAreTransitionGuarded(t *testing.T) {
	t.Run("pause requires running", func(t *testing.T) {
		env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
		env.d1.seedRun("inst-1", "wf-a", "pending")

		if status, _ := controlRequest(t, env, "/api/workflows/pause", "token-a", "wf-a", "inst-1"); status != http.StatusConflict {
			t.Fatalf("pause on a pending run: expected 409, got %d", status)
		}
		if actions := env.d1.controlActions(); len(actions) != 0 {
			t.Fatalf("an invalid pause must not reach the runtime, got %v", actions)
		}
	})

	t.Run("pause then resume", func(t *testing.T) {
		env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
		env.d1.seedRun("inst-1", "wf-a", "running")

		if status, body := controlRequest(t, env, "/api/workflows/pause", "token-a", "wf-a", "inst-1"); status != http.StatusOK {
			t.Fatalf("pause on a running run: expected 200, got %d (%v)", status, body)
		}
		// The fake applied the guarded write, so the row is paused now — which
		// is exactly what resume must require.
		if status, body := controlRequest(t, env, "/api/workflows/resume", "token-a", "wf-a", "inst-1"); status != http.StatusOK {
			t.Fatalf("resume on a paused run: expected 200, got %d (%v)", status, body)
		}

		actions := env.d1.controlActions()
		if len(actions) != 2 || actions[0]["action"] != "pause" || actions[1]["action"] != "resume" {
			t.Fatalf("expected pause then resume, got %v", actions)
		}
		writes := env.d1.stateWritesSnapshot()
		if len(writes) != 2 || writes[0]["status"] != "paused" || writes[1]["status"] != "running" {
			t.Fatalf("expected paused then running writes, got %v", writes)
		}
	})

	t.Run("resume requires paused", func(t *testing.T) {
		env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
		env.d1.seedRun("inst-1", "wf-a", "running")

		if status, _ := controlRequest(t, env, "/api/workflows/resume", "token-a", "wf-a", "inst-1"); status != http.StatusConflict {
			t.Fatalf("resume on a running run: expected 409, got %d", status)
		}
		if actions := env.d1.controlActions(); len(actions) != 0 {
			t.Fatalf("an invalid resume must not reach the runtime, got %v", actions)
		}
	})
}

// TestWorkflowControlDefaultsTheCancelReason pins the operator-facing default:
// an empty reason still records something a human can act on.
func TestWorkflowControlDefaultsTheCancelReason(t *testing.T) {
	env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	env.d1.seedRun("inst-1", "wf-a", "pending")

	if status, body := controlRequest(t, env, "/api/workflows/cancel", "token-a", "wf-a", "inst-1"); status != http.StatusOK {
		t.Fatalf("cancel without a reason: expected 200, got %d (%v)", status, body)
	}
	writes := env.d1.stateWritesSnapshot()
	if len(writes) != 1 {
		t.Fatalf("expected one cancelled write, got %v", writes)
	}
	if reason, _ := writes[0]["reason"].(string); !strings.Contains(reason, "cancelled by operator") {
		t.Fatalf("an empty reason must default honestly, got %q", reason)
	}
}
