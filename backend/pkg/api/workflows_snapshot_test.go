package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"backend/pkg/composition"
)

// ---------- ADI-FOUND-004b: snapshot binding on the live trigger path ----------
//
// These tests drive the real route registration over the emulated D1 and
// Cloudflare runtimes, so what they pin is the actual handler behaviour: a
// refused admission must leave no instance row and no Cloudflare run behind.

// snapshotStoreFake mirrors the append-only contract of composition/d1store.
type snapshotStoreFake struct {
	rows map[string]composition.Snapshot
}

func newSnapshotStoreFake() *snapshotStoreFake {
	return &snapshotStoreFake{rows: map[string]composition.Snapshot{}}
}

func (s *snapshotStoreFake) Persist(_ context.Context, snapshot composition.Snapshot) error {
	if existing, ok := s.rows[snapshot.SnapshotID]; ok {
		// Append-only, mirroring composition/d1store: identical content is a
		// no-op, and the same content under another tenant is the
		// first-writer-wins conflict the real adapter reports at write time.
		if existing.TenantKey != snapshot.TenantKey {
			return composition.ErrSnapshotTenant
		}
		return nil
	}
	s.rows[snapshot.SnapshotID] = snapshot
	return nil
}

func (s *snapshotStoreFake) Load(_ context.Context, snapshotID string) (*composition.Snapshot, error) {
	row, ok := s.rows[snapshotID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", composition.ErrSnapshotMissing, snapshotID)
	}
	return &row, nil
}

// testSnapshot builds a minimal but self-consistent lock for a tenant.
func testSnapshot(t *testing.T, tenant string) composition.Snapshot {
	t.Helper()
	lock := &composition.Lock{
		LockVersion:     composition.LockVersion,
		SystemRef:       "system/snapshot-test@1.0.0",
		SystemDigest:    "sha256:" + strings.Repeat("a", 64),
		CompilerVersion: "vibesdk-compiler/0.1.0",
		ResolutionMode:  "bundled",
		ResolvedClosure: []composition.ClosureEntry{},
		ModelPolicyRefs: []string{},
		EvaluationRefs:  []string{},
		GeneratedAt:     "2026-11-23T10:15:00Z",
	}
	snapshot, err := composition.NewSnapshot(lock, tenant, "")
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	if findings := composition.Verify(snapshot, snapshot.SnapshotID); len(findings) > 0 {
		t.Fatalf("the test snapshot must verify, got %v", findings)
	}
	return *snapshot
}

func newSnapshotEnv(t *testing.T, required bool) (*workflowEnv, *snapshotStoreFake) {
	t.Helper()
	env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	store := newSnapshotStoreFake()
	env.hub.SetSnapshotBinding(store, required)
	return env, store
}

func triggerCode(t *testing.T, body map[string]any) string {
	t.Helper()
	code, _ := body["code"].(string)
	return code
}

// 1. Binding on, no snapshot id: refused, and nothing durable exists afterwards.
func TestSnapshotGuardRefusesUnboundRun(t *testing.T) {
	env, _ := newSnapshotEnv(t, true)

	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		map[string]any{"workflowId": "wf-a"})
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%v)", status, body)
	}
	if got := triggerCode(t, body); got != string(composition.CodeSnapshotMissing) {
		t.Fatalf("expected E_SNAPSHOT_MISSING, got %q (%v)", got, body)
	}
	if rows := env.d1.instanceRows(); len(rows) != 0 {
		t.Fatalf("a refused run must not write an instance row: %v", rows)
	}
	if runs := env.d1.cloudflareRuns(); len(runs) != 0 {
		t.Fatalf("a refused run must not start a runtime instance: %v", runs)
	}
}

// 2. Soft mode (store installed, binding not required): unchanged behaviour.
func TestSnapshotGuardSoftModeKeepsUnboundRunsWorking(t *testing.T) {
	env, _ := newSnapshotEnv(t, false)

	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		map[string]any{"workflowId": "wf-a"})
	if status != http.StatusOK {
		t.Fatalf("soft mode must not change the trigger, got %d (%v)", status, body)
	}
	rows := env.d1.instanceRows()
	if len(rows) != 1 {
		t.Fatalf("expected one recorded run, got %d", len(rows))
	}
	if rows[0]["snapshot_id"] != "" {
		t.Fatalf("an unbound run must record an empty snapshot id, got %v", rows[0]["snapshot_id"])
	}
}

// 3. An unknown snapshot id is refused, not silently downgraded to unbound.
func TestSnapshotGuardUnknownSnapshotRefused(t *testing.T) {
	env, _ := newSnapshotEnv(t, true)

	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		map[string]any{"workflowId": "wf-a", "snapshotId": "sha256:missing"})
	if status != http.StatusConflict || triggerCode(t, body) != string(composition.CodeSnapshotMissing) {
		t.Fatalf("expected 409 E_SNAPSHOT_MISSING, got %d (%v)", status, body)
	}
	if rows := env.d1.instanceRows(); len(rows) != 0 {
		t.Fatalf("no run may be recorded: %v", rows)
	}
}

// 4. A tampered snapshot is refused by the digest chain.
func TestSnapshotGuardTamperedSnapshotRefused(t *testing.T) {
	env, store := newSnapshotEnv(t, true)
	snapshot := testSnapshot(t, "user-a")
	if err := store.Persist(context.Background(), snapshot); err != nil {
		t.Fatalf("persist: %v", err)
	}
	row := store.rows[snapshot.SnapshotID]
	row.CanonicalLock = strings.Replace(row.CanonicalLock, "vibesdk-compiler/0.1.0", "vibesdk-compiler/9.9.9", 1)
	store.rows[snapshot.SnapshotID] = row

	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		map[string]any{"workflowId": "wf-a", "snapshotId": snapshot.SnapshotID})
	if status != http.StatusConflict || triggerCode(t, body) != string(composition.CodeDigest) {
		t.Fatalf("expected 409 E_DIGEST, got %d (%v)", status, body)
	}
	if rows := env.d1.instanceRows(); len(rows) != 0 {
		t.Fatalf("no run may be recorded: %v", rows)
	}
}

// 5. Another tenant's snapshot is refused with 403 and writes nothing.
func TestSnapshotGuardCrossTenantRefused(t *testing.T) {
	env, store := newSnapshotEnv(t, true)
	snapshot := testSnapshot(t, "user-b")
	if err := store.Persist(context.Background(), snapshot); err != nil {
		t.Fatalf("persist: %v", err)
	}

	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		map[string]any{"workflowId": "wf-a", "snapshotId": snapshot.SnapshotID})
	if status != http.StatusForbidden || triggerCode(t, body) != string(composition.CodeTenant) {
		t.Fatalf("expected 403 E_TENANT, got %d (%v)", status, body)
	}
	if rows := env.d1.instanceRows(); len(rows) != 0 {
		t.Fatalf("a cross-tenant refusal must not record a run: %v", rows)
	}
}

// 6. A valid snapshot is admitted, recorded on the run, and echoed back.
func TestSnapshotGuardBindsValidRun(t *testing.T) {
	env, store := newSnapshotEnv(t, true)
	snapshot := testSnapshot(t, "user-a")
	if err := store.Persist(context.Background(), snapshot); err != nil {
		t.Fatalf("persist: %v", err)
	}

	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		map[string]any{"workflowId": "wf-a", "snapshotId": snapshot.SnapshotID})
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (%v)", status, body)
	}
	rows := env.d1.instanceRows()
	if len(rows) != 1 {
		t.Fatalf("expected one recorded run, got %d", len(rows))
	}
	if rows[0]["snapshot_id"] != snapshot.SnapshotID {
		t.Fatalf("the run must record its snapshot, got %v", rows[0]["snapshot_id"])
	}
	data, _ := body["data"].(map[string]any)
	if data["snapshotId"] != snapshot.SnapshotID {
		t.Fatalf("the response must echo the binding: %v", body)
	}
	if runs := env.d1.cloudflareRuns(); len(runs) != 1 {
		t.Fatalf("a bound run must start exactly one runtime instance, got %d", len(runs))
	}
}

// 7. Binding required but no store installed is a configuration failure, not an
// implicit allow.
func TestSnapshotGuardRequiredWithoutStoreRefuses(t *testing.T) {
	env := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	env.hub.SetSnapshotBinding(nil, true)

	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		map[string]any{"workflowId": "wf-a", "snapshotId": "sha256:whatever"})
	if status != http.StatusConflict || triggerCode(t, body) != string(composition.CodeSnapshotMissing) {
		t.Fatalf("expected 409 E_SNAPSHOT_MISSING, got %d (%v)", status, body)
	}
	if rows := env.d1.instanceRows(); len(rows) != 0 {
		t.Fatalf("no run may be recorded: %v", rows)
	}
}
