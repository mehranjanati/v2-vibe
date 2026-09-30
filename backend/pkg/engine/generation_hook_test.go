package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// ---------- P1.3.1 / P1.3.2 — generation lineage hooks ----------
//
// These tests drive the REAL pipeline (fake AI Gateway from dual_model_test.go
// for planner + coder, emulated D1 from generation_test.go for the recorder)
// and assert the contract the History tab will read:
//
//   - one `generations` row per RUN — never one per stage (runTeam is nested
//     inside runDualModelPipeline, so a second record there would break the
//     parent chain),
//   - every row terminal when the run returns: a row still 'running' after the
//     run ends would never become a parent and would hang in the UI forever,
//   - the terminal status follows the run's REAL outcome: succeeded with the
//     run's verdict, failed on an error, cancelled when nothing was accepted.

// lineageRoom builds a room wired to the emulated D1 plus an in-memory Redis
// stand-in, so one whole pipeline run's record is observable without a live
// D1/Redis.
func lineageRoom(t *testing.T, chatID string) (*ProjectRoom, *fakeD1) {
	t.Helper()
	d1, client := newFakeD1(t)
	room := NewProjectRoom(chatID, nil, nil, nil)
	room.SetD1Client(client)
	room.genKV = newFakeGenerationKV()
	go room.Run()
	t.Cleanup(room.Stop)
	return room, d1
}

// waitForPlanGate blocks until the B7 gate is open (or the deadline passes),
// then runs act. Polling the gate instead of sleeping a fixed amount keeps
// these tests independent of how long the fake planner takes to answer.
func waitForPlanGate(t *testing.T, room *ProjectRoom, act func()) {
	t.Helper()
	go func() {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			room.planMu.Lock()
			pending := room.planApproval != nil
			room.planMu.Unlock()
			if pending {
				act()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
}

// TestRunDualModelPipelineRecordsGeneration (P1.3.2): a completed run opens
// exactly ONE row, closes it 'succeeded' with the run's verdict and stores the
// diff rows of the files the run touched.
func TestRunDualModelPipelineRecordsGeneration(t *testing.T) {
	srv, _ := dualPipelineServer(t, dualPlanFixture,
		"window.App = { name: 'todo' };\n",
		"<html><body>app</body></html>\n")
	dualPipelineEnv(t, srv.URL)

	room, d1 := lineageRoom(t, "lineage-dual")
	room.UpsertFile("public/index.html", "<html>old</html>\n", "")
	approveLater(room)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := room.runDualModelPipeline(ctx, "build a todo app"); err != nil {
		t.Fatalf("runDualModelPipeline: %v", err)
	}

	rows := d1.genRows()
	if len(rows) != 1 {
		t.Fatalf("generations rows = %d, want exactly 1 per run (nested stages must not open their own)", len(rows))
	}
	g := rows[0]
	if g.chatID != "lineage-dual" {
		t.Errorf("chat_id = %q, want %q", g.chatID, "lineage-dual")
	}
	if g.status != "succeeded" {
		t.Errorf("status = %q, want succeeded", g.status)
	}
	if g.verdict != "done" {
		t.Errorf("verdict = %q, want done (the single-coder path has no reviewer stage)", g.verdict)
	}
	if g.parent != "" {
		t.Errorf("parent = %q, want empty for the chat's first generation", g.parent)
	}

	ops := map[string]string{}
	for _, row := range d1.fileRows() {
		if row.genID != g.id {
			t.Errorf("file row %q belongs to generation %q, want %q", row.path, row.genID, g.id)
		}
		ops[row.path] = row.op
	}
	if len(ops) != 2 || ops["public/js/app.js"] != "create" || ops["public/index.html"] != "modify" {
		t.Errorf("diff rows = %v, want app.js create + index.html modify", ops)
	}

	// The audit trail is written on a detached goroutine: waiting for it also
	// keeps the writer from racing the server teardown.
	d1.waitForAudits(t, 1)
}

// TestRunDualModelPipelineFailedRunClosesRecord (P1.3.2): a run that dies
// before its explicit Finish (here a degenerate greenfield plan) must not
// leave a 'running' row behind.
func TestRunDualModelPipelineFailedRunClosesRecord(t *testing.T) {
	srv, _ := dualPipelineServer(t, dualEmptyPlanFixture)
	dualPipelineEnv(t, srv.URL)

	room, d1 := lineageRoom(t, "lineage-dual-failed")
	approveLater(room)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := room.runDualModelPipeline(ctx, "build something"); err == nil {
		t.Fatal("a degenerate greenfield plan must fail the pipeline")
	}

	rows := d1.genRows()
	if len(rows) != 1 {
		t.Fatalf("generations rows = %d, want 1", len(rows))
	}
	if rows[0].status != "failed" || rows[0].verdict != "error" {
		t.Errorf("row = %+v, want status failed + verdict error", rows[0])
	}
}

// TestRunDualModelPipelineRejectedPlanClosesRecord (P1.3.2): a plan the user
// rejects is not a failure — no file was ever accepted, so the row closes
// 'cancelled' with a NULL verdict.
func TestRunDualModelPipelineRejectedPlanClosesRecord(t *testing.T) {
	srv, _ := dualPipelineServer(t, dualPlanFixture)
	dualPipelineEnv(t, srv.URL)

	room, d1 := lineageRoom(t, "lineage-dual-rejected")
	waitForPlanGate(t, room, room.RejectPlan)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := room.runDualModelPipeline(ctx, "build a todo app")
	if !errors.Is(err, errPlanRejected) {
		t.Fatalf("err = %v, want errPlanRejected", err)
	}

	rows := d1.genRows()
	if len(rows) != 1 {
		t.Fatalf("generations rows = %d, want 1", len(rows))
	}
	if rows[0].status != "cancelled" || rows[0].verdict != "" {
		t.Errorf("row = %+v, want status cancelled + NULL verdict", rows[0])
	}

	// The trail records WHY the run closed (and waiting for it keeps the
	// detached audit writer from racing the D1 server teardown).
	audits := d1.waitForAudits(t, 1)
	if audits[0].action != "generation_finished" || !strings.Contains(audits[0].detail, "\"cancelled\"") {
		t.Errorf("audit = %+v, want a generation_finished row carrying the cancelled status", audits[0])
	}
}

// TestRunDualModelPipelineCancelledRunClosesRecord (P1.3.2): a run cancelled
// while parked on the plan gate still closes its row. The close goes through
// lineageCtx (context.WithoutCancel), so a dead run context cannot leave a
// 'running' row behind.
func TestRunDualModelPipelineCancelledRunClosesRecord(t *testing.T) {
	srv, _ := dualPipelineServer(t, dualPlanFixture)
	dualPipelineEnv(t, srv.URL)

	room, d1 := lineageRoom(t, "lineage-dual-cancelled")

	ctx, cancel := context.WithCancel(context.Background())
	waitForPlanGate(t, room, cancel)

	if err := room.runDualModelPipeline(ctx, "build a todo app"); err == nil {
		t.Fatal("a cancelled run must return the cancellation")
	}

	rows := d1.genRows()
	if len(rows) != 1 {
		t.Fatalf("generations rows = %d, want 1", len(rows))
	}
	if rows[0].status != "cancelled" || rows[0].verdict != "" {
		t.Errorf("row = %+v, want status cancelled + NULL verdict", rows[0])
	}

	// A cancelled run still lands its trail (lineageCtx survives the caller's
	// cancellation) — asserted by waiting, not sleeping.
	audits := d1.waitForAudits(t, 1)
	if audits[0].action != "generation_finished" || !strings.Contains(audits[0].detail, "\"cancelled\"") {
		t.Errorf("audit = %+v, want a generation_finished row carrying the cancelled status", audits[0])
	}
}
