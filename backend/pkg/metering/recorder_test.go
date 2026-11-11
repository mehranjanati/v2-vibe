package metering

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/pkg/cloudflare"
)

// ---------- emulated D1 ----------
//
// fakeD1 applies the recorder's statements to in-memory rows and answers with
// the real D1 REST envelope, so the production cloudflare.D1Client — not a mock
// of it — is exercised end to end. Same approach as the workflow and generation
// tests in backend/pkg/engine.

type fakeRunRow struct {
	id, outcomeID, status, userID string
	input, output, errText        string
	startedAt, completedAt        int64 // 0 = SQL NULL
}

type fakeStepRow struct {
	id, runID, stepName, nodeType, status string
	attempt                               int
	output, errText                       string
	startedAt, completedAt                int64
}

type fakeD1 struct {
	mu    sync.Mutex
	runs  []*fakeRunRow
	steps []*fakeStepRow
}

func newFakeD1(t *testing.T) (*fakeD1, *cloudflare.D1Client) {
	t.Helper()
	f := &fakeD1{}
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
		results := f.apply(req.SQL, req.Params)
		f.mu.Unlock()
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
func (f *fakeD1) apply(sql string, params []any) []map[string]any {
	switch {
	case strings.HasPrefix(sql, "INSERT INTO outcome_runs"):
		f.runs = append(f.runs, &fakeRunRow{
			id:        strParam(params, 0),
			outcomeID: strParam(params, 1),
			status:    "pending",
			userID:    strParam(params, 2), // NULLIF(?, '') is applied by the fake
			input:     strParam(params, 3),
		})

	case strings.HasPrefix(sql, "UPDATE outcome_runs SET status = 'running'"):
		if run := f.findRun(strParam(params, 2)); run != nil {
			run.status = "running"
			run.startedAt = intParam(params, 0)
			if run.input == "" {
				run.input = strParam(params, 1)
			}
		}

	case strings.HasPrefix(sql, "UPDATE outcome_runs SET status = ?"):
		if run := f.findRun(strParam(params, 4)); run != nil {
			run.status = strParam(params, 0)
			if out := strParam(params, 1); out != "" {
				run.output = out
			}
			run.errText = strParam(params, 2)
			run.completedAt = intParam(params, 3)
		}

	case strings.HasPrefix(sql, "INSERT INTO outcome_run_steps") && strings.Contains(sql, "'skipped'"):
		f.steps = append(f.steps, &fakeStepRow{
			id:          strParam(params, 0),
			runID:       strParam(params, 1),
			stepName:    strParam(params, 2),
			nodeType:    strParam(params, 3),
			status:      "skipped",
			attempt:     intParam(params, 4),
			completedAt: intParam(params, 5),
		})

	case strings.HasPrefix(sql, "INSERT INTO outcome_run_steps"):
		f.steps = append(f.steps, &fakeStepRow{
			id:        strParam(params, 0),
			runID:     strParam(params, 1),
			stepName:  strParam(params, 2),
			nodeType:  strParam(params, 3),
			status:    "running",
			attempt:   intParam(params, 4),
			startedAt: intParam(params, 5),
		})

	case strings.HasPrefix(sql, "UPDATE outcome_run_steps SET status = ?"):
		if step := f.findStep(strParam(params, 4)); step != nil {
			step.status = strParam(params, 0)
			if out := strParam(params, 1); out != "" {
				step.output = out
			}
			step.errText = strParam(params, 2)
			step.completedAt = intParam(params, 3)
		}

	case strings.HasPrefix(sql, "SELECT status, COUNT(*) AS n"):
		return f.runUsage(params)

	case strings.HasPrefix(sql, "SELECT s.status AS status"):
		return f.stepUsage(params)
	}
	return nil
}

// runUsage emulates the run aggregation, including `user_id IS ?` semantics:
// a nil parameter matches unowned rows only (stored as "" by the fake).
func (f *fakeD1) runUsage(params []any) []map[string]any {
	outcomeID, owner := strParam(params, 0), strParam(params, 1)
	type agg struct {
		n        int
		duration int64
	}
	groups := map[string]*agg{}
	for _, run := range f.runs {
		if run.outcomeID != outcomeID || run.userID != owner {
			continue
		}
		g := groups[run.status]
		if g == nil {
			g = &agg{}
			groups[run.status] = g
		}
		g.n++
		if run.startedAt != 0 && run.completedAt != 0 && run.completedAt >= run.startedAt {
			g.duration += run.completedAt - run.startedAt
		}
	}
	out := make([]map[string]any, 0, len(groups))
	for status, g := range groups {
		out = append(out, map[string]any{"status": status, "n": float64(g.n), "duration_ms": float64(g.duration)})
	}
	return out
}

// stepUsage emulates the step aggregation joined onto the owning runs.
func (f *fakeD1) stepUsage(params []any) []map[string]any {
	outcomeID, owner := strParam(params, 0), strParam(params, 1)
	counts := map[string]int{}
	for _, step := range f.steps {
		run := f.findRun(step.runID)
		if run == nil || run.outcomeID != outcomeID || run.userID != owner {
			continue
		}
		counts[step.status]++
	}
	out := make([]map[string]any, 0, len(counts))
	for status, n := range counts {
		out = append(out, map[string]any{"status": status, "n": float64(n)})
	}
	return out
}

func (f *fakeD1) findRun(id string) *fakeRunRow {
	for _, run := range f.runs {
		if run.id == id {
			return run
		}
	}
	return nil
}

func (f *fakeD1) findStep(id string) *fakeStepRow {
	for _, step := range f.steps {
		if step.id == id {
			return step
		}
	}
	return nil
}

func (f *fakeD1) run(t *testing.T, id string) fakeRunRow {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if run := f.findRun(id); run != nil {
		return *run
	}
	t.Fatalf("no outcome_runs row for %s", id)
	return fakeRunRow{}
}

func (f *fakeD1) stepRows(runID string) []fakeStepRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeStepRow, 0, len(f.steps))
	for _, step := range f.steps {
		if step.runID == runID {
			out = append(out, *step)
		}
	}
	return out
}

func (f *fakeD1) stepByID(t *testing.T, id string) fakeStepRow {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if step := f.findStep(id); step != nil {
		return *step
	}
	t.Fatalf("no outcome_run_steps row for %s", id)
	return fakeStepRow{}
}

func strParam(params []any, i int) string {
	if i < 0 || i >= len(params) || params[i] == nil {
		return ""
	}
	if s, ok := params[i].(string); ok {
		return s
	}
	return ""
}

func intParam(params []any, i int) int64 {
	if i < 0 || i >= len(params) || params[i] == nil {
		return 0
	}
	if n, ok := params[i].(float64); ok {
		return int64(n)
	}
	return 0
}

// testRecorder wires a recorder to the emulated D1 with a controllable clock.
func testRecorder(t *testing.T) (*Recorder, *fakeD1, func(int64)) {
	t.Helper()
	f, d1 := newFakeD1(t)
	rec := NewRecorder(d1)
	clock := int64(1_700_000_000_000)
	rec.now = func() time.Time { return time.UnixMilli(clock) }
	return rec, f, func(ms int64) { clock = ms }
}

// ---------- tests ----------

func TestStartRunRecordsPendingTenantScopedRow(t *testing.T) {
	rec, f, _ := testRecorder(t)
	ctx := context.Background()

	id, err := rec.StartRun(ctx, StartRunParams{
		OutcomeID: "out-1",
		OwnerID:   "user-a",
		Input:     json.RawMessage(`{"lead":"ada"}`),
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if id == "" {
		t.Fatal("StartRun returned an empty id")
	}
	run := f.run(t, id)
	if run.status != string(RunPending) {
		t.Errorf("status = %q, want %q", run.status, RunPending)
	}
	if run.outcomeID != "out-1" || run.userID != "user-a" {
		t.Errorf("row scope = (%q, %q), want (out-1, user-a)", run.outcomeID, run.userID)
	}
	if run.input != `{"lead":"ada"}` {
		t.Errorf("input = %q, want the trigger payload", run.input)
	}
	if run.startedAt != 0 || run.completedAt != 0 {
		t.Errorf("pending run must not stamp timestamps, got started=%d completed=%d", run.startedAt, run.completedAt)
	}

	// An explicit id is honored (caller-side correlation).
	fixed, err := rec.StartRun(ctx, StartRunParams{OutcomeID: "out-1", RunID: "run-fixed"})
	if err != nil || fixed != "run-fixed" {
		t.Fatalf("explicit RunID = (%q, %v), want (run-fixed, nil)", fixed, err)
	}
	// An empty owner is stored as SQL NULL (unowned), never as "".
	if unowned := f.run(t, fixed); unowned.userID != "" {
		t.Errorf("unowned row user_id = %q, want NULL/empty", unowned.userID)
	}
}

func TestStartRunRequiresOutcomeID(t *testing.T) {
	rec, _, _ := testRecorder(t)
	if _, err := rec.StartRun(context.Background(), StartRunParams{}); err == nil {
		t.Fatal("StartRun with no outcome id: want error, got nil")
	}
}

func TestRecorderWithoutStoreReturnsIDAndErrNoStore(t *testing.T) {
	rec := NewRecorder(nil)
	ctx := context.Background()
	if rec.Enabled() {
		t.Fatal("Enabled() = true with no D1 client")
	}

	id, err := rec.StartRun(ctx, StartRunParams{OutcomeID: "out-1"})
	if !errors.Is(err, ErrNoStore) {
		t.Fatalf("StartRun error = %v, want ErrNoStore", err)
	}
	if id == "" {
		t.Fatal("StartRun must still return a usable id when recording is disabled")
	}

	if err := rec.MarkRunning(ctx, id, nil); !errors.Is(err, ErrNoStore) {
		t.Errorf("MarkRunning error = %v, want ErrNoStore", err)
	}
	if err := rec.FinishRun(ctx, FinishRunParams{RunID: id, Status: RunSucceeded}); !errors.Is(err, ErrNoStore) {
		t.Errorf("FinishRun error = %v, want ErrNoStore", err)
	}
	if err := rec.CancelRun(ctx, id, "stop"); !errors.Is(err, ErrNoStore) {
		t.Errorf("CancelRun error = %v, want ErrNoStore", err)
	}

	stepID, err := rec.StartStep(ctx, StepStartParams{RunID: id, StepName: "qualify_lead"})
	if !errors.Is(err, ErrNoStore) || stepID == "" {
		t.Errorf("StartStep = (%q, %v), want (non-empty id, ErrNoStore)", stepID, err)
	}
	if err := rec.FinishStep(ctx, StepFinishParams{StepID: stepID, Status: StepSucceeded}); !errors.Is(err, ErrNoStore) {
		t.Errorf("FinishStep error = %v, want ErrNoStore", err)
	}
	if _, err := rec.SkipStep(ctx, StepStartParams{RunID: id, StepName: "send_followup"}); !errors.Is(err, ErrNoStore) {
		t.Errorf("SkipStep error = %v, want ErrNoStore", err)
	}
	if _, err := rec.Usage(ctx, "out-1", ""); !errors.Is(err, ErrNoStore) {
		t.Errorf("Usage error = %v, want ErrNoStore", err)
	}
}

func TestRunLifecycleStampsAndMetersDuration(t *testing.T) {
	rec, f, setClock := testRecorder(t)
	ctx := context.Background()
	base := int64(1_700_000_000_000)

	id, err := rec.StartRun(ctx, StartRunParams{OutcomeID: "out-1", OwnerID: "user-a"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	setClock(base + 1_000)
	if err := rec.MarkRunning(ctx, id, json.RawMessage(`{"lead":"ada"}`)); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	// A re-delivered trigger must not overwrite the first payload.
	if err := rec.MarkRunning(ctx, id, json.RawMessage(`{"lead":"eve"}`)); err != nil {
		t.Fatalf("MarkRunning (retry): %v", err)
	}

	setClock(base + 2_500)
	if err := rec.FinishRun(ctx, FinishRunParams{
		RunID:  id,
		Status: RunSucceeded,
		Output: json.RawMessage(`{"qualified":true}`),
	}); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	run := f.run(t, id)
	if run.status != string(RunSucceeded) {
		t.Errorf("status = %q, want %q", run.status, RunSucceeded)
	}
	if run.startedAt != base+1_000 || run.completedAt != base+2_500 {
		t.Errorf("timestamps = (%d, %d), want (%d, %d)", run.startedAt, run.completedAt, base+1_000, base+2_500)
	}
	if run.input != `{"lead":"ada"}` {
		t.Errorf("input = %q, want the first payload (COALESCE)", run.input)
	}
	if run.output != `{"qualified":true}` {
		t.Errorf("output = %q, want the run result", run.output)
	}
	if run.errText != "" {
		t.Errorf("error = %q, want SQL NULL", run.errText)
	}

	usage, err := rec.Usage(ctx, "out-1", "user-a")
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if usage.Runs != 1 || usage.Succeeded != 1 {
		t.Errorf("usage runs = (%d, %d), want (1 run, 1 succeeded)", usage.Runs, usage.Succeeded)
	}
	if usage.DurationMS != 1_500 {
		t.Errorf("duration = %d ms, want 1500", usage.DurationMS)
	}
}

func TestCancelRunRecordsCancelledStatus(t *testing.T) {
	rec, f, _ := testRecorder(t)
	ctx := context.Background()

	id, err := rec.StartRun(ctx, StartRunParams{OutcomeID: "out-1", OwnerID: "user-a"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := rec.CancelRun(ctx, id, "user stopped the run"); err != nil {
		t.Fatalf("CancelRun: %v", err)
	}

	run := f.run(t, id)
	if run.status != string(RunCancelled) {
		t.Errorf("status = %q, want %q", run.status, RunCancelled)
	}
	if run.errText != "user stopped the run" {
		t.Errorf("error = %q, want the cancellation reason", run.errText)
	}
}

func TestStepLifecycleAndUsage(t *testing.T) {
	rec, f, setClock := testRecorder(t)
	ctx := context.Background()
	base := int64(1_700_000_000_000)

	runID, err := rec.StartRun(ctx, StartRunParams{OutcomeID: "out-1", OwnerID: "user-a"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	// 1) succeeded step with a result payload
	setClock(base + 100)
	stepID, err := rec.StartStep(ctx, StepStartParams{
		RunID:    runID,
		StepName: "qualify_lead",
		NodeType: NodeAI,
		Attempt:  0,
	})
	if err != nil {
		t.Fatalf("StartStep: %v", err)
	}
	started := f.stepByID(t, stepID)
	if started.status != string(StepRunning) || started.startedAt != base+100 {
		t.Errorf("started row = (%q, %d), want (%q, %d)", started.status, started.startedAt, StepRunning, base+100)
	}
	if started.nodeType != string(NodeAI) {
		t.Errorf("node type = %q, want %q", started.nodeType, NodeAI)
	}

	setClock(base + 900)
	if err := rec.FinishStep(ctx, StepFinishParams{
		StepID: stepID,
		Status: StepSucceeded,
		Output: json.RawMessage(`{"score":91}`),
	}); err != nil {
		t.Fatalf("FinishStep: %v", err)
	}
	done := f.stepByID(t, stepID)
	if done.status != string(StepSucceeded) || done.completedAt != base+900 {
		t.Errorf("finished row = (%q, %d), want (%q, %d)", done.status, done.completedAt, StepSucceeded, base+900)
	}
	if done.output != `{"score":91}` {
		t.Errorf("output = %q, want the step result", done.output)
	}

	// 2) failed retry attempt: a second row, not a mutation of the first
	failedID, err := rec.StartStep(ctx, StepStartParams{
		RunID:    runID,
		StepName: "crm_writeback",
		NodeType: NodeHTTP,
		Attempt:  1,
	})
	if err != nil {
		t.Fatalf("StartStep (retry): %v", err)
	}
	if err := rec.FinishStep(ctx, StepFinishParams{
		StepID: failedID,
		Status: StepFailed,
		Error:  "crm 502",
	}); err != nil {
		t.Fatalf("FinishStep (failed): %v", err)
	}
	if failed := f.stepByID(t, failedID); failed.attempt != 1 || failed.errText != "crm 502" {
		t.Errorf("failed row = (attempt %d, error %q), want (1, crm 502)", failed.attempt, failed.errText)
	}

	// 3) pruned node: one skipped row with completed_at and no started_at
	skippedID, err := rec.SkipStep(ctx, StepStartParams{
		RunID:    runID,
		StepName: "send_followup",
		NodeType: NodeEmail,
	})
	if err != nil {
		t.Fatalf("SkipStep: %v", err)
	}
	skipped := f.stepByID(t, skippedID)
	if skipped.status != string(StepSkipped) {
		t.Errorf("skipped status = %q, want %q", skipped.status, StepSkipped)
	}
	if skipped.startedAt != 0 || skipped.completedAt == 0 {
		t.Errorf("skipped timestamps = (%d, %d), want (0, non-zero)", skipped.startedAt, skipped.completedAt)
	}

	if got := len(f.stepRows(runID)); got != 3 {
		t.Errorf("step rows = %d, want 3 (one per attempt plus the skipped node)", got)
	}

	usage, err := rec.Usage(ctx, "out-1", "user-a")
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if usage.Steps != 3 || usage.StepsSucceeded != 1 || usage.StepsFailed != 1 || usage.StepsSkipped != 1 {
		t.Errorf("step usage = %+v, want 3 steps (1 succeeded, 1 failed, 1 skipped)", usage)
	}
}

func TestUsageIsTenantScoped(t *testing.T) {
	rec, _, _ := testRecorder(t)
	ctx := context.Background()

	for _, owner := range []string{"user-a", "user-a", "user-b"} {
		if _, err := rec.StartRun(ctx, StartRunParams{OutcomeID: "out-1", OwnerID: owner}); err != nil {
			t.Fatalf("StartRun(%s): %v", owner, err)
		}
	}
	if _, err := rec.StartRun(ctx, StartRunParams{OutcomeID: "out-1"}); err != nil {
		t.Fatalf("StartRun(unowned): %v", err)
	}

	scoped, err := rec.Usage(ctx, "out-1", "user-a")
	if err != nil {
		t.Fatalf("Usage(user-a): %v", err)
	}
	if scoped.Runs != 2 {
		t.Errorf("user-a runs = %d, want 2 (user-b and unowned rows must not leak)", scoped.Runs)
	}

	unowned, err := rec.Usage(ctx, "out-1", "")
	if err != nil {
		t.Fatalf("Usage(unowned): %v", err)
	}
	if unowned.Runs != 1 {
		t.Errorf("unowned runs = %d, want 1 (an empty owner must not mean \"all tenants\")", unowned.Runs)
	}
}

func TestFinishRunRejectsNonTerminalStatus(t *testing.T) {
	rec, _, _ := testRecorder(t)
	ctx := context.Background()
	for _, status := range []RunStatus{RunPending, RunRunning, RunStatus("done")} {
		err := rec.FinishRun(ctx, FinishRunParams{RunID: "run-1", Status: status})
		if err == nil {
			t.Errorf("FinishRun(%q): want error, got nil", status)
		}
	}
}

func TestFinishStepRejectsSkipped(t *testing.T) {
	rec, _, _ := testRecorder(t)
	err := rec.FinishStep(context.Background(), StepFinishParams{StepID: "step-1", Status: StepSkipped})
	if err == nil {
		t.Fatal("FinishStep(skipped): want error, got nil (SkipStep records pruned nodes)")
	}
	if !strings.Contains(err.Error(), "SkipStep") {
		t.Errorf("error = %v, want it to point at SkipStep", err)
	}
}

func TestStepParamsAreValidated(t *testing.T) {
	rec, _, _ := testRecorder(t)
	ctx := context.Background()
	if _, err := rec.StartStep(ctx, StepStartParams{RunID: "run-1"}); err == nil {
		t.Error("StartStep without a step name: want error, got nil")
	}
	if _, err := rec.StartStep(ctx, StepStartParams{StepName: "qualify_lead"}); err == nil {
		t.Error("StartStep without a run id: want error, got nil")
	}
	if _, err := rec.SkipStep(ctx, StepStartParams{RunID: "run-1"}); err == nil {
		t.Error("SkipStep without a step name: want error, got nil")
	}
	if err := rec.FinishStep(ctx, StepFinishParams{Status: StepSucceeded}); err == nil {
		t.Error("FinishStep without a step id: want error, got nil")
	}
	if _, err := rec.Usage(ctx, " ", "user-a"); err == nil {
		t.Error("Usage without an outcome id: want error, got nil")
	}
}
