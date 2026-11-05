package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"backend/pkg/cloudflare"
)

// ---------- P0.7 workflow tenant scope: persistence + audit ----------
//
// wfFakeD1 emulates the workflow_* / audit_logs statements the control plane
// issues and records every statement, so these tests pin both the SQL the
// tenant columns rely on and its outcome through the production
// cloudflare.D1Client.

type wfStatement struct {
	sql    string
	params []any
}

type wfFakeD1 struct {
	mu         sync.Mutex
	dags       map[string]map[string]any
	instances  map[string]map[string]any
	audits     []wfStatement
	statements []wfStatement
	outage     bool
}

func newWFFakeD1(t *testing.T) (*wfFakeD1, *cloudflare.D1Client) {
	t.Helper()
	f := &wfFakeD1{
		dags:      map[string]map[string]any{},
		instances: map[string]map[string]any{},
	}
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
func (f *wfFakeD1) apply(sql string, params []any) ([]map[string]any, bool) {
	f.statements = append(f.statements, wfStatement{sql: sql, params: params})
	if f.outage {
		return nil, true
	}
	workflowID := wfParam(params, 0)

	switch {
	case strings.HasPrefix(sql, "INSERT INTO workflow_dags"):
		row, ok := f.dags[workflowID]
		if !ok {
			row = map[string]any{
				"workflow_id": workflowID,
				"user_id":     nil,
				"created_at":  float64(1700000000),
			}
			f.dags[workflowID] = row
		}
		// Mirrors the statement's `NULLIF(?, '')` insert value and its
		// `COALESCE(workflow_dags.user_id, excluded.user_id)` conflict clause:
		// the first recorded owner sticks.
		if owner := wfParam(params, 3); owner != "" {
			if cur, _ := row["user_id"].(string); cur == "" {
				row["user_id"] = owner
			}
		}
		row["schema_version"] = params[1]
		row["dag_json"] = wfParam(params, 2)
		row["status"] = "generated"
		row["updated_at"] = float64(1700000001)

	case strings.HasPrefix(sql, "SELECT user_id FROM workflow_dags"):
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
		f.instances[wfParam(params, 0)] = map[string]any{
			"id":          wfParam(params, 0),
			"workflow_id": wfParam(params, 1),
			"user_id":     wfParam(params, 2),
			"input":       wfParam(params, 3),
		}

	case strings.HasPrefix(sql, "INSERT INTO audit_logs"):
		f.audits = append(f.audits, wfStatement{sql: sql, params: params})
	}
	return nil, false
}

func wfParam(params []any, i int) string {
	if i >= len(params) || params[i] == nil {
		return ""
	}
	s, _ := params[i].(string)
	return s
}

func (f *wfFakeD1) setOutage(outage bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outage = outage
}

func (f *wfFakeD1) statement(i int) wfStatement {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i < 0 || i >= len(f.statements) {
		return wfStatement{}
	}
	return f.statements[i]
}

func (f *wfFakeD1) statementCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.statements)
}

func (f *wfFakeD1) dag(workflowID string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dags[workflowID]
}

func (f *wfFakeD1) instance(id string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.instances[id]
}

func (f *wfFakeD1) auditStatements() []wfStatement {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]wfStatement(nil), f.audits...)
}

// TestUpsertWorkflowDagStampsTenantFirstWriteWins pins the DAG ownership write:
// the upsert records the owner, a later regeneration refreshes the DAG without
// transferring the tenant, and an unowned generation stores SQL NULL.
func TestUpsertWorkflowDagStampsTenantFirstWriteWins(t *testing.T) {
	f, d1 := newWFFakeD1(t)
	ctx := context.Background()

	if err := d1.UpsertWorkflowDag(ctx, "wf-a", 2, `{"v":1}`, "user-a"); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	first := f.statement(0)
	if !strings.Contains(first.sql, "user_id") || !strings.Contains(first.sql, "NULLIF(?, '')") {
		t.Fatalf("the upsert must write user_id (NULLIF for the unowned case): %s", first.sql)
	}
	if !strings.Contains(first.sql, "user_id = COALESCE(workflow_dags.user_id, excluded.user_id)") {
		t.Fatalf("a later upsert must keep the first recorded owner: %s", first.sql)
	}
	if got := wfParam(first.params, 3); got != "user-a" {
		t.Fatalf("expected the owner as the fourth parameter, got %q", got)
	}

	if err := d1.UpsertWorkflowDag(ctx, "wf-a", 2, `{"v":2}`, "user-b"); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	row, err := LoadWorkflowDag(ctx, d1, "wf-a")
	if err != nil {
		t.Fatalf("load dag: %v", err)
	}
	if row == nil {
		t.Fatal("expected the DAG row to exist")
	}
	if row.UserID != "user-a" {
		t.Fatalf("a regeneration must not transfer the tenant, got %q", row.UserID)
	}
	if row.DagJSON != `{"v":2}` {
		t.Fatalf("the regeneration must still refresh the DAG, got %q", row.DagJSON)
	}

	if err := d1.UpsertWorkflowDag(ctx, "wf-b", 2, `{"v":1}`, ""); err != nil {
		t.Fatalf("unowned upsert: %v", err)
	}
	unowned, err := LoadWorkflowDag(ctx, d1, "wf-b")
	if err != nil {
		t.Fatalf("load unowned dag: %v", err)
	}
	if unowned.UserID != "" {
		t.Fatalf("an empty owner must be stored as NULL, got %q", unowned.UserID)
	}
	if _, found, err := WorkflowOwner(ctx, d1, "wf-b", nil); err != nil || found {
		t.Fatalf("an unowned workflow must not resolve to an owner: found=%v err=%v", found, err)
	}
}

// TestCreateWorkflowInstanceRowCarriesTenant pins that a recorded run copies
// the workflow's tenant (the value VibeWorkflow compares against the DAG).
func TestCreateWorkflowInstanceRowCarriesTenant(t *testing.T) {
	f, d1 := newWFFakeD1(t)
	ctx := context.Background()

	if err := CreateWorkflowInstanceRow(ctx, d1, "wf-a", "inst-1", "user-a", `{"amount":1}`); err != nil {
		t.Fatalf("record run: %v", err)
	}
	stmt := f.statement(0)
	if !strings.Contains(stmt.sql, "user_id") || !strings.Contains(stmt.sql, "NULLIF(?, '')") {
		t.Fatalf("the run insert must carry the tenant: %s", stmt.sql)
	}
	inst := f.instance("inst-1")
	if inst["user_id"] != "user-a" || inst["workflow_id"] != "wf-a" || inst["input"] != `{"amount":1}` {
		t.Fatalf("unexpected run row: %v", inst)
	}

	// An unowned workflow records a NULL tenant (pre-P0.7 behaviour).
	if err := CreateWorkflowInstanceRow(ctx, d1, "wf-b", "inst-2", "", `{}`); err != nil {
		t.Fatalf("record unowned run: %v", err)
	}
	if got := f.instance("inst-2")["user_id"]; got != "" {
		t.Fatalf("expected an empty tenant for an unowned run, got %v", got)
	}
}

// TestWorkflowOwnerResolution pins the resolution order the route guard relies
// on: the D1 column wins, a legacy row falls back to the P0.4 room ownership
// record, a missing workflow never falls back (404 without leaking), and both
// the missing store and an outage fail closed.
func TestWorkflowOwnerResolution(t *testing.T) {
	f, d1 := newWFFakeD1(t)
	ctx := context.Background()

	if err := d1.UpsertWorkflowDag(ctx, "wf-owned", 2, `{}`, "user-a"); err != nil {
		t.Fatalf("seed owned dag: %v", err)
	}
	if err := d1.UpsertWorkflowDag(ctx, "wf-legacy", 2, `{}`, ""); err != nil {
		t.Fatalf("seed legacy dag: %v", err)
	}

	fallbackCalls := 0
	rooms := func(chatID string) (string, bool, error) {
		fallbackCalls++
		if chatID == "wf-legacy" {
			return "user-a", true, nil
		}
		return "", false, nil
	}

	// 1. workflow_dags.user_id is authoritative — the registry is not consulted.
	owner, found, err := WorkflowOwner(ctx, d1, "wf-owned", rooms)
	if err != nil || !found || owner != "user-a" {
		t.Fatalf("expected the recorded column to win: owner=%q found=%v err=%v", owner, found, err)
	}
	if fallbackCalls != 0 {
		t.Fatalf("the room registry must not be consulted when the column records an owner (%d calls)", fallbackCalls)
	}

	// 2. A legacy row (NULL tenant) is scoped by the room ownership record.
	owner, found, err = WorkflowOwner(ctx, d1, "wf-legacy", rooms)
	if err != nil || !found || owner != "user-a" {
		t.Fatalf("expected the room fallback to resolve: owner=%q found=%v err=%v", owner, found, err)
	}
	if fallbackCalls != 1 {
		t.Fatalf("expected exactly one fallback lookup, got %d", fallbackCalls)
	}

	// 3. No column and no fallback configured → unowned.
	if owner, found, err = WorkflowOwner(ctx, d1, "wf-legacy", nil); err != nil || found || owner != "" {
		t.Fatalf("expected an unowned result without a fallback: owner=%q found=%v err=%v", owner, found, err)
	}

	// 4. An unknown workflow is not found and never consults the registry.
	fallbackCalls = 0
	if owner, found, err = WorkflowOwner(ctx, d1, "wf-missing", rooms); err != nil || found || owner != "" {
		t.Fatalf("expected an unknown workflow to stay not-found: owner=%q found=%v err=%v", owner, found, err)
	}
	if fallbackCalls != 0 {
		t.Fatalf("an unknown workflow must not reach the ownership registry (%d calls)", fallbackCalls)
	}

	// 5. No ownership store configured → error (the guard answers 503).
	if _, _, err = WorkflowOwner(ctx, nil, "wf-owned", rooms); err == nil {
		t.Fatal("expected an error when D1 is not configured")
	}

	// 6. Ownership-store outage → error, never "unowned".
	f.setOutage(true)
	if _, _, err = WorkflowOwner(ctx, d1, "wf-owned", rooms); err == nil {
		t.Fatal("expected an error during a D1 outage")
	}
}

// TestRecordWorkflowAccessDenialWritesAuditRow pins the audit record the P0.7
// acceptance criterion requires next to the 403.
func TestRecordWorkflowAccessDenialWritesAuditRow(t *testing.T) {
	f, d1 := newWFFakeD1(t)

	err := RecordWorkflowAccessDenial(context.Background(), d1, WorkflowAccessDenial{
		WorkflowID: "wf-a",
		CallerID:   "user-b",
		OwnerID:    "user-a",
		Operation:  "POST",
		IPAddress:  "203.0.113.7",
		UserAgent:  "p07-test",
	})
	if err != nil {
		t.Fatalf("record denial: %v", err)
	}

	audits := f.auditStatements()
	if len(audits) != 1 {
		t.Fatalf("expected exactly one audit row, got %d", len(audits))
	}
	stmt := audits[0]
	if !strings.Contains(stmt.sql, "INSERT INTO audit_logs") || !strings.Contains(stmt.sql, "'workflow'") {
		t.Fatalf("unexpected audit statement: %s", stmt.sql)
	}
	if got := wfParam(stmt.params, 1); got != "user-b" {
		t.Fatalf("the audit subject must be the caller, got %q", got)
	}
	if got := wfParam(stmt.params, 2); got != "wf-a" {
		t.Fatalf("the audited entity must be the workflow, got %q", got)
	}
	if got := wfParam(stmt.params, 3); got != "workflow.access_denied" {
		t.Fatalf("unexpected audit action: %q", got)
	}
	detail := wfParam(stmt.params, 4)
	if !strings.Contains(detail, `"owner":"user-a"`) || !strings.Contains(detail, `"operation":"POST"`) {
		t.Fatalf("audit detail must name the owner and the operation: %s", detail)
	}
	if f.statementCount() != 1 {
		t.Fatalf("a denial audit writes exactly one statement, got %d", f.statementCount())
	}

	// No audit store configured → error (never a silent skip).
	if err := RecordWorkflowAccessDenial(context.Background(), nil, WorkflowAccessDenial{}); err == nil {
		t.Fatal("expected an error when the audit store is not configured")
	}
}
