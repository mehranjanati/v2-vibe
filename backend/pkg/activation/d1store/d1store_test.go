package d1store

// ADI-CTX-004 — the D1 adapter against the activation.TraceStore contract.
//
// T1-T10 below are the store half: round trip, idempotent replay, immutability,
// the tenant wall, missing rows, corruption, a refusal record, the injected
// clock, validation-before-write and a missing database. The fake querier
// emulates the two literal statements, so the SQL strings and argument order are
// exercised, not mocked away.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"backend/pkg/activation"
)

// The adapter must satisfy the seam the activation core declares.
var _ activation.TraceStore = (*Store)(nil)

var decidedAt = time.Date(2026, 11, 25, 9, 30, 0, 0, time.UTC)

// fakeQuerier emulates INSERT ... ON CONFLICT DO NOTHING plus the SELECT shape.
type fakeQuerier struct {
	rows   map[string]map[string]interface{}
	sqls   []string
	failOn string
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{rows: map[string]map[string]interface{}{}}
}

func (f *fakeQuerier) Exec(_ context.Context, sql string, args ...interface{}) (bool, error) {
	f.sqls = append(f.sqls, sql)
	if f.failOn != "" && strings.Contains(sql, f.failOn) {
		return false, errors.New("injected failure")
	}
	if !strings.Contains(sql, "INSERT INTO activation_decision_traces") {
		return false, errors.New("unexpected statement: " + sql)
	}
	if !strings.Contains(sql, "ON CONFLICT(run_id) DO NOTHING") {
		return false, errors.New("insert is not conflict-safe")
	}
	runID, _ := args[0].(string)
	if _, exists := f.rows[runID]; exists {
		return true, nil // DO NOTHING
	}
	f.rows[runID] = map[string]interface{}{
		"run_id":                args[0],
		"tenant_key":            args[1],
		"context_id":            args[2],
		"composition_digest":    args[3],
		"activation_policy_ref": args[4],
		"ranking_source":        args[5],
		"ranking_version":       args[6],
		"decided_at":            args[7],
		"trace_digest":          args[8],
		"record":                args[9],
	}
	return true, nil
}

func (f *fakeQuerier) Query(_ context.Context, sql string, args ...interface{}) ([]map[string]interface{}, error) {
	f.sqls = append(f.sqls, sql)
	if f.failOn != "" && strings.Contains(sql, f.failOn) {
		return nil, errors.New("injected failure")
	}
	if !strings.Contains(sql, "FROM activation_decision_traces") {
		return nil, errors.New("unexpected statement: " + sql)
	}
	if len(args) != 1 {
		return nil, errors.New("load must bind exactly the run id")
	}
	runID, _ := args[0].(string)
	if row, ok := f.rows[runID]; ok {
		return []map[string]interface{}{row}, nil
	}
	return nil, nil
}

// record builds the docs/architecture/activation-policy.md §5 example: one
// activated, one budget-skipped and one denied candidate, in candidate order.
func record() activation.Record {
	return activation.Record{
		ActivationPolicyRef: "activation-policy/tenant-default@3.0.0",
		CompositionDigest:   "sha256:deadbeef",
		ContextID:           "ctx:sha256:00",
		DecidedAt:           decidedAt.UTC().Format(time.RFC3339),
		Decisions: []activation.Decision{
			{Candidate: "tool/refund@1.0.0", Decision: activation.DecisionDenied,
				RuleID: "deny-irreversible", Reason: "tenant forbids irreversible side effects", Stage: activation.StageEligibility},
			{Candidate: "tool/score-lead@1.0.0", Decision: activation.DecisionActivated,
				RuleID: "allow-safe-writes", Stage: activation.StageEligibility,
				ApprovalRequired: []activation.ApprovalClass{activation.ApprovalPayment}},
			{Candidate: "tool/send-email@2.1.0", Decision: activation.DecisionSkipped,
				Reason: activation.ReasonBudget, Stage: activation.StageBudget},
		},
		Ranking:   activation.Ranking{Source: "provider-v1", Version: "1.2.0"},
		RunID:     "run-1",
		TenantKey: "tenant-a",
	}
}

// T1: what goes in comes out — identity, ranking version, every explanation and
// the injected clock (stored as a unix second, read back as RFC 3339).
func TestTraceRoundTrip(t *testing.T) {
	db := newFakeQuerier()
	store := New(db)
	want := record()
	if err := store.Persist(context.Background(), want); err != nil {
		t.Fatalf("persist: %v", err)
	}
	got, err := store.Load(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.RunID != want.RunID || got.TenantKey != want.TenantKey || got.ContextID != want.ContextID {
		t.Fatalf("identity drifted: %+v", got)
	}
	if got.CompositionDigest != want.CompositionDigest || got.ActivationPolicyRef != want.ActivationPolicyRef {
		t.Fatalf("snapshot or policy drifted: %+v", got)
	}
	if got.Ranking.Source != want.Ranking.Source || got.Ranking.Version != want.Ranking.Version {
		t.Fatalf("ranking provider/version must survive storage: %+v", got.Ranking)
	}
	if got.DecidedAt != want.DecidedAt {
		t.Fatalf("decidedAt: want %q, got %q", want.DecidedAt, got.DecidedAt)
	}
	if len(got.Decisions) != len(want.Decisions) {
		t.Fatalf("decisions: want %d, got %d", len(want.Decisions), len(got.Decisions))
	}
	for i := range want.Decisions {
		if got.Decisions[i].Candidate != want.Decisions[i].Candidate ||
			got.Decisions[i].Decision != want.Decisions[i].Decision ||
			got.Decisions[i].RuleID != want.Decisions[i].RuleID ||
			got.Decisions[i].Reason != want.Decisions[i].Reason ||
			got.Decisions[i].Stage != want.Decisions[i].Stage ||
			len(got.Decisions[i].ApprovalRequired) != len(want.Decisions[i].ApprovalRequired) {
			t.Fatalf("decision %d drifted: want %+v, got %+v", i, want.Decisions[i], got.Decisions[i])
		}
	}
	if stored := db.rows["run-1"]["decided_at"]; stored != decidedAt.Unix() {
		t.Fatalf("decided_at must be stored as a unix second, got %v", stored)
	}
}

// T2: the same decision twice is one row, and the first write keeps its clock.
func TestTracePersistIsIdempotent(t *testing.T) {
	db := newFakeQuerier()
	store := New(db)
	first := record()
	if err := store.Persist(context.Background(), first); err != nil {
		t.Fatalf("first persist: %v", err)
	}
	replay := record()
	replay.DecidedAt = decidedAt.Add(time.Hour).UTC().Format(time.RFC3339)
	if err := store.Persist(context.Background(), replay); err != nil {
		t.Fatalf("a replay of the same decision must be a no-op: %v", err)
	}
	if len(db.rows) != 1 {
		t.Fatalf("expected one row, got %d", len(db.rows))
	}
	got, err := store.Load(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.DecidedAt != first.DecidedAt {
		t.Fatalf("the first write must keep its decidedAt: want %q, got %q", first.DecidedAt, got.DecidedAt)
	}
}

// T3: a different decision under the same run is refused, never overwritten.
func TestTraceIsImmutablePerRun(t *testing.T) {
	db := newFakeQuerier()
	store := New(db)
	if err := store.Persist(context.Background(), record()); err != nil {
		t.Fatalf("persist: %v", err)
	}
	changed := record()
	changed.ActivationPolicyRef = "activation-policy/tenant-default@3.0.1"
	err := store.Persist(context.Background(), changed)
	if !errors.Is(err, activation.ErrTraceImmutable) {
		t.Fatalf("expected ErrTraceImmutable, got %v", err)
	}
	got, loadErr := store.Load(context.Background(), "run-1")
	if loadErr != nil {
		t.Fatalf("load after refusal: %v", loadErr)
	}
	if got.ActivationPolicyRef != "activation-policy/tenant-default@3.0.0" {
		t.Fatalf("the refusal must not rewrite the stored policy ref: %q", got.ActivationPolicyRef)
	}
}

// T4: a run id already recorded by another tenant is refused, not shared.
func TestTraceRefusesAnotherTenant(t *testing.T) {
	db := newFakeQuerier()
	store := New(db)
	if err := store.Persist(context.Background(), record()); err != nil {
		t.Fatalf("persist: %v", err)
	}
	foreign := record()
	foreign.TenantKey = "tenant-b"
	if err := store.Persist(context.Background(), foreign); !errors.Is(err, activation.ErrTraceTenant) {
		t.Fatalf("expected ErrTraceTenant, got %v", err)
	}
}

// T5: an unknown or unnamed run answers ErrTraceMissing, never an empty record.
func TestTraceMissingRun(t *testing.T) {
	store := New(newFakeQuerier())
	if _, err := store.Load(context.Background(), "run-404"); !errors.Is(err, activation.ErrTraceMissing) {
		t.Fatalf("expected ErrTraceMissing, got %v", err)
	}
	if _, err := store.Load(context.Background(), "  "); !errors.Is(err, activation.ErrTraceMissing) {
		t.Fatalf("expected ErrTraceMissing for a blank run id, got %v", err)
	}
}

// T6: a stored row that no longer matches its digest is refused, not served.
func TestTraceCorruptionIsRefused(t *testing.T) {
	t.Run("record edited under a stale digest", func(t *testing.T) {
		db := newFakeQuerier()
		store := New(db)
		if err := store.Persist(context.Background(), record()); err != nil {
			t.Fatalf("persist: %v", err)
		}
		row := db.rows["run-1"]
		row["record"] = strings.Replace(row["record"].(string), "tenant forbids irreversible side effects", "tenant permits it", 1)
		if _, err := store.Load(context.Background(), "run-1"); !errors.Is(err, activation.ErrTraceCorrupt) {
			t.Fatalf("expected ErrTraceCorrupt, got %v", err)
		}
	})
	t.Run("identity columns disagree with the record", func(t *testing.T) {
		db := newFakeQuerier()
		store := New(db)
		if err := store.Persist(context.Background(), record()); err != nil {
			t.Fatalf("persist: %v", err)
		}
		db.rows["run-1"]["run_id"] = "run-other"
		if _, err := store.Load(context.Background(), "run-1"); !errors.Is(err, activation.ErrTraceCorrupt) {
			t.Fatalf("expected ErrTraceCorrupt, got %v", err)
		}
	})
}

// T7: a refusal is storable — "why did it stop" deserves the same trace.
func TestTracePersistsARefusalRecord(t *testing.T) {
	refusal := record()
	refusal.Decisions = []activation.Decision{
		{Candidate: "tool/score-lead@1.0.0", Decision: activation.DecisionSkipped,
			Reason: activation.ReasonNoAllow, Stage: activation.StageEligibility},
	}
	refusal.Ranking = activation.Ranking{Source: "none"}
	db := newFakeQuerier()
	store := New(db)
	if err := store.Persist(context.Background(), refusal); err != nil {
		t.Fatalf("a refusal record must persist: %v", err)
	}
	got, err := store.Load(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Ranking.Source != "none" {
		t.Fatalf("the absence of a ranking provider must survive as \"none\", got %q", got.Ranking.Source)
	}
	if len(got.Decisions) != 1 || got.Decisions[0].Reason != activation.ReasonNoAllow {
		t.Fatalf("the default-deny explanation was lost: %+v", got.Decisions)
	}
}

// T8: no injected clock means no timestamp, not an invented one.
func TestTraceWithoutAClock(t *testing.T) {
	timeless := record()
	timeless.DecidedAt = ""
	db := newFakeQuerier()
	store := New(db)
	if err := store.Persist(context.Background(), timeless); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if stored := db.rows["run-1"]["decided_at"]; stored != nil {
		t.Fatalf("a zero clock must store NULL, got %v", stored)
	}
	got, err := store.Load(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.DecidedAt != "" {
		t.Fatalf("a zero clock must read back empty, got %q", got.DecidedAt)
	}
}

// T9: validation happens before the store is touched — an unattributable record
// never reaches D1, so a broken caller cannot leave a partial explanation.
func TestTraceValidatesBeforeWriting(t *testing.T) {
	invalid := record()
	invalid.Decisions[1].RuleID = "" // activated without the rule that allowed it
	db := newFakeQuerier()
	store := New(db)
	err := store.Persist(context.Background(), invalid)
	if !errors.Is(err, activation.ErrTraceInvalid) {
		t.Fatalf("expected ErrTraceInvalid, got %v", err)
	}
	if len(db.sqls) != 0 {
		t.Fatalf("no statement may run for an invalid record, saw %v", db.sqls)
	}
}

// T10: a store without a database refuses instead of panicking.
func TestTraceRequiresADatabase(t *testing.T) {
	store := New(nil)
	if err := store.Persist(context.Background(), record()); err == nil {
		t.Fatal("expected an error from a store with no database")
	}
	if _, err := store.Load(context.Background(), "run-1"); err == nil {
		t.Fatal("expected an error from a store with no database")
	}
}

// T11: a D1 failure is reported with the run named, and never swallowed.
func TestTraceStoreFailuresAreLoud(t *testing.T) {
	db := newFakeQuerier()
	db.failOn = "INSERT INTO activation_decision_traces"
	err := New(db).Persist(context.Background(), record())
	if err == nil || !strings.Contains(err.Error(), "run-1") {
		t.Fatalf("expected an error naming the run, got %v", err)
	}

	reader := newFakeQuerier()
	reader.failOn = "FROM activation_decision_traces"
	if _, err := New(reader).Load(context.Background(), "run-1"); err == nil {
		t.Fatal("expected a load error when D1 fails")
	}
}
