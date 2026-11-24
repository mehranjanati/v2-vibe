package d1store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"backend/pkg/composition"
)

// The D1 adapter must satisfy the store contract the resolver's admission path
// depends on.
var _ composition.SnapshotStore = (*Store)(nil)

// fakeQuerier emulates the two statements this store issues. It is deliberately
// literal: a real INSERT ... ON CONFLICT DO NOTHING and a real SELECT shape, so
// the test exercises the same SQL the database will see.
type fakeQuerier struct {
	rows map[string]map[string]interface{}
	sqls []string
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{rows: map[string]map[string]interface{}{}}
}

func (f *fakeQuerier) Exec(_ context.Context, sql string, args ...interface{}) (bool, error) {
	f.sqls = append(f.sqls, sql)
	if strings.Contains(sql, "INSERT INTO composition_snapshots") {
		if !strings.Contains(sql, "ON CONFLICT(snapshot_id) DO NOTHING") {
			return false, errors.New("insert is not conflict-safe")
		}
		id, _ := args[0].(string)
		if _, exists := f.rows[id]; exists {
			return true, nil // DO NOTHING
		}
		f.rows[id] = map[string]interface{}{
			"snapshot_id":      args[0],
			"lock_version":     args[1],
			"system_ref":       args[2],
			"system_digest":    args[3],
			"compiler_version": args[4],
			"resolution_mode":  args[5],
			"canonical_lock":   args[6],
			"tenant_key":       args[7],
			"closed_at":        args[8],
			"retention_class":  args[9],
		}
	}
	return true, nil
}

func (f *fakeQuerier) Query(_ context.Context, sql string, args ...interface{}) ([]map[string]interface{}, error) {
	f.sqls = append(f.sqls, sql)
	if len(args) == 0 {
		return nil, nil
	}
	id, _ := args[0].(string)
	if row, ok := f.rows[id]; ok {
		return []map[string]interface{}{row}, nil
	}
	return nil, nil
}

func sampleSnapshot(id string) composition.Snapshot {
	return composition.Snapshot{
		SnapshotID:      id,
		LockVersion:     composition.LockVersion,
		SystemRef:       "system/m1-qualify-then-draft@1.0.0",
		SystemDigest:    "sha256:abc",
		CompilerVersion: "vibesdk-compiler/0.1.0",
		ResolutionMode:  "bundled",
		CanonicalLock:   `{"lockVersion":"1.0.0"}`,
		TenantKey:       "tenant-a",
		ClosedAt:        "2026-11-23T10:15:00Z",
		RetentionClass:  composition.DefaultRetentionClass,
	}
}

// D1: persisting issues a conflict-safe insert and never an update.
func TestD1PersistIsAppendOnly(t *testing.T) {
	db := newFakeQuerier()
	store := New(db)
	if err := store.Persist(context.Background(), sampleSnapshot("sha256:one")); err != nil {
		t.Fatalf("persist: %v", err)
	}
	for _, sql := range db.sqls {
		if strings.Contains(strings.ToUpper(sql), "UPDATE") {
			t.Fatalf("the store issued an UPDATE: %s", sql)
		}
	}
	if len(db.rows) != 1 {
		t.Fatalf("expected one row, got %d", len(db.rows))
	}
}

// D2: a stored row round-trips, including the timestamp conversion.
func TestD2LoadRoundTrips(t *testing.T) {
	db := newFakeQuerier()
	store := New(db)
	original := sampleSnapshot("sha256:round-trip")
	if err := store.Persist(context.Background(), original); err != nil {
		t.Fatalf("persist: %v", err)
	}
	loaded, err := store.Load(context.Background(), original.SnapshotID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.CanonicalLock != original.CanonicalLock || loaded.TenantKey != original.TenantKey {
		t.Fatalf("content did not round-trip: %+v", loaded)
	}
	if loaded.ClosedAt != "2026-11-23T10:15:00Z" {
		t.Fatalf("closedAt did not round-trip through unix seconds: %q", loaded.ClosedAt)
	}
	if loaded.RetentionClass != composition.DefaultRetentionClass {
		t.Fatalf("unexpected retention class %q", loaded.RetentionClass)
	}
}

// D3: an unknown id is a missing snapshot, not an empty one.
func TestD3LoadUnknownIsMissing(t *testing.T) {
	store := New(newFakeQuerier())
	if _, err := store.Load(context.Background(), "sha256:nope"); !errors.Is(err, composition.ErrSnapshotMissing) {
		t.Fatalf("expected ErrSnapshotMissing, got %v", err)
	}
}

// D4: a second tenant cannot claim the same content silently.
func TestD4SecondTenantRefused(t *testing.T) {
	db := newFakeQuerier()
	store := New(db)
	first := sampleSnapshot("sha256:shared")
	if err := store.Persist(context.Background(), first); err != nil {
		t.Fatalf("persist first: %v", err)
	}
	second := first
	second.TenantKey = "tenant-b"
	if err := store.Persist(context.Background(), second); !errors.Is(err, composition.ErrSnapshotTenant) {
		t.Fatalf("expected ErrSnapshotTenant, got %v", err)
	}
}

// D5: the same id with different content is refused, never overwritten.
func TestD5DifferentContentRefused(t *testing.T) {
	db := newFakeQuerier()
	store := New(db)
	original := sampleSnapshot("sha256:collision")
	if err := store.Persist(context.Background(), original); err != nil {
		t.Fatalf("persist: %v", err)
	}
	tampered := original
	tampered.CanonicalLock = `{"lockVersion":"9.9.9"}`
	if err := store.Persist(context.Background(), tampered); !errors.Is(err, composition.ErrSnapshotImmutable) {
		t.Fatalf("expected ErrSnapshotImmutable, got %v", err)
	}
	stored, err := store.Load(context.Background(), original.SnapshotID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if stored.CanonicalLock != original.CanonicalLock {
		t.Fatal("the stored row was overwritten")
	}
}

// D6: an unconfigured store refuses instead of pretending.
func TestD6UnconfiguredStoreRefuses(t *testing.T) {
	store := New(nil)
	if err := store.Persist(context.Background(), sampleSnapshot("sha256:x")); err == nil {
		t.Fatal("persist with no database must fail")
	}
	if _, err := store.Load(context.Background(), "sha256:x"); err == nil {
		t.Fatal("load with no database must fail")
	}
}
