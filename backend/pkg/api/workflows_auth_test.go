package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"

	"backend/pkg/cloudflare"
	"backend/pkg/engine"
)

// ---------- P0.7 workflow tenant isolation ----------
//
// These tests drive the real route registrations (RegisterRoutes) with the
// P0.3 boundary on, over an emulated D1 REST API, so the guard wiring, the
// ownership SQL and the access-denial audit are exercised end to end rather
// than against a stand-in router. Acceptance criteria
// (docs/DEV_CHECKLIST.md P0.7): user B trigger/get on user A's workflow →
// 403 + audit; an unknown/unowned workflow → 404; an ownership-store outage
// → 503; with the boundary off nothing regresses.

// fakeWorkflowD1 is a minimal in-memory emulation of the D1 statements the
// workflow routes issue: the ownership lookup, the DAG load, the run insert
// and the audit insert. It answers the real D1 REST envelope, so the
// production cloudflare.D1Client runs unmodified.
type fakeWorkflowD1 struct {
	mu        sync.Mutex
	dags      map[string]map[string]any // workflow_id → row (user_id may be nil)
	instances []map[string]any          // workflow_instances inserts
	audits    []map[string]any          // audit_logs inserts
	cfRuns    []map[string]any          // Cloudflare Workflows instance creates
	// failOwnership makes the ownership SELECT fail, emulating a D1 outage.
	failOwnership bool
}

// newFakeWorkflowD1 starts the emulated D1 REST server and returns it plus a
// cloudflare.D1Client pointed at it.
func newFakeWorkflowD1(t *testing.T, dags map[string]map[string]any) (*fakeWorkflowD1, *cloudflare.D1Client) {
	t.Helper()
	f := &fakeWorkflowD1{dags: dags}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SQL    string `json:"sql"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		results, outage := f.apply(req.SQL, req.Params)
		f.mu.Unlock()
		if outage {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":7500,"message":"emulated D1 outage"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result":  []any{map[string]any{"results": results, "meta": map[string]any{}}},
		})
	}))
	t.Cleanup(srv.Close)

	client := cloudflare.NewD1Client("acct", "token", "db")
	client.SetBaseURL(srv.URL)
	return f, client
}

// apply executes one statement against the emulated tables (callers hold mu).
// The second return reports an emulated outage.
func (f *fakeWorkflowD1) apply(sql string, params []any) ([]map[string]any, bool) {
	workflowID := paramAt(params, 0)
	switch {
	case strings.HasPrefix(sql, "SELECT user_id FROM workflow_dags"):
		if f.failOwnership {
			return nil, true
		}
		row, ok := f.dags[workflowID]
		if !ok {
			return nil, false
		}
		return []map[string]any{{"user_id": row["user_id"]}}, false

	case strings.HasPrefix(sql, "SELECT workflow_id, schema_version, dag_json, status, user_id, updated_at FROM workflow_dags"):
		row, ok := f.dags[workflowID]
		if !ok {
			return nil, false
		}
		return []map[string]any{row}, false

	case strings.HasPrefix(sql, "INSERT INTO workflow_instances"):
		f.instances = append(f.instances, map[string]any{
			"id":          paramAt(params, 0),
			"workflow_id": paramAt(params, 1),
			"user_id":     paramAt(params, 2),
			"input":       paramAt(params, 3),
		})

	case strings.HasPrefix(sql, "INSERT INTO audit_logs"):
		f.audits = append(f.audits, map[string]any{
			"id":         paramAt(params, 0),
			"user_id":    paramAt(params, 1),
			"entity_id":  paramAt(params, 2),
			"action":     paramAt(params, 3),
			"new_values": paramAt(params, 4),
			"ip_address": paramAt(params, 5),
			"user_agent": paramAt(params, 6),
		})
	}
	return nil, false
}

// workflowCloudflareEmulator emulates POST …/workflows/{name}/instances so the
// trigger happy path completes; the created run is recorded on the fake.
func workflowCloudflareEmulator(t *testing.T, f *fakeWorkflowD1) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			InstanceID string         `json:"instance_id"`
			Params     map[string]any `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.cfRuns = append(f.cfRuns, map[string]any{"id": body.InstanceID, "params": body.Params})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{"id":"` + body.InstanceID + `","status":"queued"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func paramAt(params []any, i int) string {
	if i >= len(params) || params[i] == nil {
		return ""
	}
	s, _ := params[i].(string)
	return s
}

func (f *fakeWorkflowD1) auditRows() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.audits...)
}

func (f *fakeWorkflowD1) instanceRows() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.instances...)
}

func (f *fakeWorkflowD1) setFailOwnership(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failOwnership = fail
}

// workflowEnv is one P0.7 test fixture: the real routes over emulated stores.
type workflowEnv struct {
	app *fiber.App
	hub *engine.EngineHub
	d1  *fakeWorkflowD1
}

// newWorkflowEnv builds the fixture. A nil tokens map leaves the P0.3 boundary
// off (verifier unset), which is what the "boundary off" cases pin.
func newWorkflowEnv(t *testing.T, tokens map[string]string, dags map[string]map[string]any) *workflowEnv {
	t.Helper()
	fake, d1 := newFakeWorkflowD1(t, dags)

	app := fiber.New()
	hub := engine.NewEngineHub(nil, nil, nil)
	hub.SetD1Client(d1)
	if tokens != nil {
		hub.SetSessionVerifier(NewStaticSessionVerifier(tokens))
	}
	cf := cloudflare.NewClient("acct", "token")
	cf.SetBaseURL(workflowCloudflareEmulator(t, fake))
	hub.SetCloudflareClient(cf)
	hub.SetWorkflowName("vibesdk-v2-workflows")
	RegisterRoutes(app, hub)

	return &workflowEnv{app: app, hub: hub, d1: fake}
}

// workflowRequest performs one request carrying the session token as the
// X-Session-Token header (one of the three P0.3 transports).
func workflowRequest(t *testing.T, app *fiber.App, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Session-Token", token)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s %s failed: %v", method, path, err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// generatedDagRow is one 'generated' workflow_dags row; owner may be nil to
// model a row written before P0.7 (or by an unowned room).
func generatedDagRow(owner any) map[string]any {
	return map[string]any{
		"workflow_id":    "wf-a",
		"schema_version": float64(2),
		"dag_json":       `{"schemaVersion":2,"nodes":[{"id":"trigger","type":"trigger"}],"edges":[]}`,
		"status":         "generated",
		"user_id":        owner,
		"updated_at":     float64(1700000000),
	}
}

// p07Tokens is the two-tenant token table the P0.7 cases share.
func p07Tokens() map[string]string {
	return map[string]string{"token-a": "user-a", "token-b": "user-b"}
}

// TestWorkflowTenantIsolationCrossUserDeniedAndAudited is the P0.7 acceptance
// criterion: user B's trigger/get on user A's workflow answers 403 and leaves
// an audit_logs row (action workflow.access_denied). A denied trigger records
// no run.
func TestWorkflowTenantIsolationCrossUserDeniedAndAudited(t *testing.T) {
	env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})

	cases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"trigger", http.MethodPost, "/api/workflows/trigger", map[string]any{"workflowId": "wf-a"}},
		{"get", http.MethodGet, "/api/workflows/wf-a", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, _ := workflowRequest(t, env.app, tc.method, tc.path, "token-b", tc.body)
			if status != http.StatusForbidden {
				t.Fatalf("expected 403 for a cross-tenant %s, got %d", tc.name, status)
			}
		})
	}

	audits := env.d1.auditRows()
	if len(audits) != len(cases) {
		t.Fatalf("expected one audit row per denied attempt, got %d: %v", len(audits), audits)
	}
	for _, row := range audits {
		if row["action"] != "workflow.access_denied" {
			t.Fatalf("unexpected audit action: %v", row)
		}
		if row["entity_id"] != "wf-a" || row["user_id"] != "user-b" {
			t.Fatalf("audit row must name the workflow and the caller: %v", row)
		}
		if detail, _ := row["new_values"].(string); !strings.Contains(detail, `"owner":"user-a"`) {
			t.Fatalf("audit detail must name the owner: %v", row)
		}
	}
	if got := len(env.d1.instanceRows()); got != 0 {
		t.Fatalf("a denied trigger must not record a run, got %d rows", got)
	}
}

// TestWorkflowTenantIsolationOwnerAllowed proves the guard is not a blanket
// deny: the owner reads the DAG (200) and triggers a run (200), and the run
// row carries the workflow's tenant.
func TestWorkflowTenantIsolationOwnerAllowed(t *testing.T) {
	env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})

	status, body := workflowRequest(t, env.app, http.MethodGet, "/api/workflows/wf-a", "token-a", nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200 for the owner, got %d (%v)", status, body)
	}
	data, _ := body["data"].(map[string]any)
	workflowMap, _ := data["workflow"].(map[string]any)
	if workflowMap["workflowId"] != "wf-a" {
		t.Fatalf("unexpected workflow payload: %v", body)
	}

	status, body = workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a", map[string]any{"workflowId": "wf-a"})
	if status != http.StatusOK {
		t.Fatalf("expected 200 for the owner's trigger, got %d (%v)", status, body)
	}
	instances := env.d1.instanceRows()
	if len(instances) != 1 {
		t.Fatalf("expected exactly one recorded run, got %d", len(instances))
	}
	if instances[0]["user_id"] != "user-a" || instances[0]["workflow_id"] != "wf-a" {
		t.Fatalf("the run must carry the workflow's tenant: %v", instances[0])
	}
	if audits := env.d1.auditRows(); len(audits) != 0 {
		t.Fatalf("an allowed access must not be audited as a denial: %v", audits)
	}
}

// TestWorkflowTenantIsolationUnknownWorkflowIs404 covers the 404 contract: a
// workflow with no workflow_dags row is not found for anyone and is not
// audited as a cross-tenant attempt (the caller learns nothing about it).
func TestWorkflowTenantIsolationUnknownWorkflowIs404(t *testing.T) {
	env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{})

	cases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"trigger", http.MethodPost, "/api/workflows/trigger", map[string]any{"workflowId": "wf-a"}},
		{"get", http.MethodGet, "/api/workflows/wf-a", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, _ := workflowRequest(t, env.app, tc.method, tc.path, "token-a", tc.body)
			if status != http.StatusNotFound {
				t.Fatalf("expected 404 for an unknown workflow, got %d", status)
			}
		})
	}
	if audits := env.d1.auditRows(); len(audits) != 0 {
		t.Fatalf("an unknown workflow is not a cross-tenant attempt: %v", audits)
	}
	if got := len(env.d1.instanceRows()); got != 0 {
		t.Fatalf("no run may be recorded for an unknown workflow, got %d", got)
	}
}

// TestWorkflowTenantIsolationUnownedWorkflowIs404 pins the unowned case: a row
// with a NULL tenant and no room ownership record is refused for everyone
// (never silently readable), exactly like an unowned room in P0.4.
func TestWorkflowTenantIsolationUnownedWorkflowIs404(t *testing.T) {
	env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow(nil)})

	status, _ := workflowRequest(t, env.app, http.MethodGet, "/api/workflows/wf-a", "token-a", nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 for an unowned workflow, got %d", status)
	}
}

// TestWorkflowTenantIsolationFallsBackToRoomOwnership covers the migration
// path: a DAG row without a tenant (written before P0.7) is scoped by the
// P0.4 room ownership record for the chat the workflow id names.
func TestWorkflowTenantIsolationFallsBackToRoomOwnership(t *testing.T) {
	env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow(nil)})
	if _, ok, err := env.hub.ClaimRoomOwner("wf-a", "user-a"); err != nil || !ok {
		t.Fatalf("claim room ownership: ok=%v err=%v", ok, err)
	}

	if status, _ := workflowRequest(t, env.app, http.MethodGet, "/api/workflows/wf-a", "token-a", nil); status != http.StatusOK {
		t.Fatalf("expected 200 for the recorded room owner, got %d", status)
	}
	if status, _ := workflowRequest(t, env.app, http.MethodGet, "/api/workflows/wf-a", "token-b", nil); status != http.StatusForbidden {
		t.Fatalf("expected 403 for the other tenant, got %d", status)
	}
	if audits := env.d1.auditRows(); len(audits) != 1 {
		t.Fatalf("expected the fallback denial to be audited once, got %d", len(audits))
	}
}

// TestWorkflowTenantIsolationOwnerStoreUnavailableIs503 pins the fail-closed
// rule: an unreachable workflow ownership store must answer 503, never allow.
func TestWorkflowTenantIsolationOwnerStoreUnavailableIs503(t *testing.T) {
	env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	env.d1.setFailOwnership(true)

	status, _ := workflowRequest(t, env.app, http.MethodGet, "/api/workflows/wf-a", "token-a", nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 during an ownership-store outage, got %d", status)
	}
}

// TestWorkflowTenantIsolationUnconfiguredStoreIs503 covers the deployment
// where the boundary is on but no D1 client is attached: the workflow routes
// fail closed instead of serving an unscoped DAG.
func TestWorkflowTenantIsolationUnconfiguredStoreIs503(t *testing.T) {
	app := fiber.New()
	hub := engine.NewEngineHub(nil, nil, nil)
	hub.SetSessionVerifier(NewStaticSessionVerifier(p07Tokens()))
	RegisterRoutes(app, hub)

	status, _ := workflowRequest(t, app, http.MethodGet, "/api/workflows/wf-a", "token-a", nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without an ownership store, got %d", status)
	}
}

// TestWorkflowTenantIsolationUnresolvableTargetIs404 pins the guard's refusal
// to run without a target, and that the boundary-off deployment keeps the
// handler's own 400 for the same request (opt-in behaviour preserved).
func TestWorkflowTenantIsolationUnresolvableTargetIs404(t *testing.T) {
	guarded := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	status, _ := workflowRequest(t, guarded.app, http.MethodPost, "/api/workflows/trigger", "token-a", map[string]any{})
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 for a trigger without a resolvable workflow, got %d", status)
	}
	if got := len(guarded.d1.instanceRows()); got != 0 {
		t.Fatalf("no run may be recorded without a resolved workflow, got %d", got)
	}

	boundaryOff := newWorkflowEnv(t, nil, map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	status, _ = workflowRequest(t, boundaryOff.app, http.MethodPost, "/api/workflows/trigger", "", map[string]any{})
	if status != http.StatusBadRequest {
		t.Fatalf("expected the handler's 400 with the boundary off, got %d", status)
	}
}

// TestWorkflowTenantIsolationBoundaryOffIsPermissive pins the opt-in rollout:
// with no SessionVerifier installed the workflow routes behave exactly as
// before P0.7 (the guard is a no-op), so a single-tenant deployment that has
// not turned the boundary on does not regress.
func TestWorkflowTenantIsolationBoundaryOffIsPermissive(t *testing.T) {
	env := newWorkflowEnv(t, nil, map[string]map[string]any{"wf-a": generatedDagRow(nil)})

	status, _ := workflowRequest(t, env.app, http.MethodGet, "/api/workflows/wf-a", "", nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200 with the boundary off, got %d", status)
	}
	if audits := env.d1.auditRows(); len(audits) != 0 {
		t.Fatalf("no audit may be written with the boundary off: %v", audits)
	}
}
