package composition

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// memStore is the reference semantics of a snapshot store: append-only, keyed by
// the lock digest, tenant-checked. The D1 adapter must behave the same way, and
// d1store has its own tests for that.
type memStore struct {
	rows  map[string]Snapshot
	writes int
}

func newMemStore() *memStore { return &memStore{rows: map[string]Snapshot{}} }

func (m *memStore) Persist(_ context.Context, snapshot Snapshot) error {
	m.writes++
	if existing, ok := m.rows[snapshot.SnapshotID]; ok {
		if existing.CanonicalLock != snapshot.CanonicalLock {
			return fmt.Errorf("%w: %s", ErrSnapshotImmutable, snapshot.SnapshotID)
		}
		if existing.TenantKey != snapshot.TenantKey {
			return fmt.Errorf("%w: %s", ErrSnapshotTenant, snapshot.SnapshotID)
		}
		return nil
	}
	m.rows[snapshot.SnapshotID] = snapshot
	return nil
}

func (m *memStore) Load(_ context.Context, snapshotID string) (*Snapshot, error) {
	row, ok := m.rows[snapshotID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSnapshotMissing, snapshotID)
	}
	copyRow := row
	return &copyRow, nil
}

func m1Lock(t *testing.T) *Lock {
	t.Helper()
	return resolveOK(t, realisticSource(t), fixture(t, "m1-team.system.json"))
}

// S1: the snapshot id is the lock digest, and the stored document round-trips.
func TestS1SnapshotIdentityIsTheLockDigest(t *testing.T) {
	lock := m1Lock(t)
	snapshot, err := NewSnapshot(lock, "tenant-a", "")
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	digest, _ := lock.Digest()
	if snapshot.SnapshotID != digest {
		t.Fatalf("snapshot id %q is not the lock digest %q", snapshot.SnapshotID, digest)
	}
	if snapshot.RetentionClass != DefaultRetentionClass {
		t.Fatalf("expected the default retention class, got %q", snapshot.RetentionClass)
	}
	if snapshot.ClosedAt != lock.GeneratedAt {
		t.Fatalf("closedAt should carry generatedAt, got %q", snapshot.ClosedAt)
	}
	restored, err := snapshot.Lock()
	if err != nil {
		t.Fatalf("parse stored lock: %v", err)
	}
	if len(restored.ResolvedClosure) != len(lock.ResolvedClosure) {
		t.Fatalf("closure length changed: %d vs %d", len(restored.ResolvedClosure), len(lock.ResolvedClosure))
	}
	if findings := Verify(snapshot, snapshot.SnapshotID); len(findings) > 0 {
		t.Fatalf("a fresh snapshot must verify, got %v", findings)
	}
}

// S2: the same closure persisted twice is one row, not two.
func TestS2DedupeByContent(t *testing.T) {
	store := newMemStore()
	first, _ := NewSnapshot(m1Lock(t), "tenant-a", "")
	second, _ := NewSnapshot(m1Lock(t), "tenant-a", "")
	if first.SnapshotID != second.SnapshotID {
		t.Fatalf("re-resolving the same closure produced different ids: %s vs %s", first.SnapshotID, second.SnapshotID)
	}
	if err := store.Persist(context.Background(), *first); err != nil {
		t.Fatalf("persist first: %v", err)
	}
	if err := store.Persist(context.Background(), *second); err != nil {
		t.Fatalf("persist second: %v", err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("expected one row, got %d", len(store.rows))
	}
}

// S3: an existing snapshot cannot be rewritten.
func TestS3StoredSnapshotIsImmutable(t *testing.T) {
	store := newMemStore()
	snapshot, _ := NewSnapshot(m1Lock(t), "tenant-a", "")
	if err := store.Persist(context.Background(), *snapshot); err != nil {
		t.Fatalf("persist: %v", err)
	}
	tampered := *snapshot
	tampered.CanonicalLock = strings.Replace(snapshot.CanonicalLock, "\"lockVersion\": \"1.0.0\"", "\"lockVersion\": \"9.9.9\"", 1)
	if tampered.CanonicalLock == snapshot.CanonicalLock {
		t.Fatal("the tamper did not change the document")
	}
	if err := store.Persist(context.Background(), tampered); !errors.Is(err, ErrSnapshotImmutable) {
		t.Fatalf("expected ErrSnapshotImmutable, got %v", err)
	}
}

// S4: tampering with the stored document is caught by the digest chain.
func TestS4VerifyCatchesTampering(t *testing.T) {
	snapshot, _ := NewSnapshot(m1Lock(t), "tenant-a", "")

	tampered := *snapshot
	tampered.CanonicalLock = strings.Replace(snapshot.CanonicalLock, "sha256:", "sha256:0", 1)
	findings := Verify(&tampered, tampered.SnapshotID)
	if !hasCode(findings, CodeDigest) {
		t.Fatalf("expected E_DIGEST, got %v", codes(findings))
	}

	wrongID := *snapshot
	if findings := Verify(&wrongID, "sha256:deadbeef"); !hasCode(findings, CodeDigest) {
		t.Fatalf("expected E_DIGEST for an id mismatch, got %v", codes(findings))
	}

	if findings := Verify(nil, ""); !hasCode(findings, CodeSnapshotMissing) {
		t.Fatalf("expected E_SNAPSHOT_MISSING for a nil snapshot, got %v", codes(findings))
	}
}

// S5: admission refuses a run that carries no snapshot.
func TestS5AdmissionRefusesUnboundRun(t *testing.T) {
	store := newMemStore()
	_, findings := AdmitRun(context.Background(), store, "", "tenant-a")
	if !hasCode(findings, CodeSnapshotMissing) {
		t.Fatalf("expected E_SNAPSHOT_MISSING, got %v", codes(findings))
	}
	_, findings = AdmitRun(context.Background(), store, "sha256:unknown", "tenant-a")
	if !hasCode(findings, CodeSnapshotMissing) {
		t.Fatalf("expected E_SNAPSHOT_MISSING for an unknown id, got %v", codes(findings))
	}
}

// S6: a snapshot recorded for one tenant cannot be read by another.
func TestS6CrossTenantAdmissionRefused(t *testing.T) {
	store := newMemStore()
	snapshot, _ := NewSnapshot(m1Lock(t), "tenant-a", "")
	if err := store.Persist(context.Background(), *snapshot); err != nil {
		t.Fatalf("persist: %v", err)
	}
	_, findings := AdmitRun(context.Background(), store, snapshot.SnapshotID, "tenant-b")
	if !hasCode(findings, CodeTenant) {
		t.Fatalf("expected E_TENANT, got %v", codes(findings))
	}
	admitted, findings := AdmitRun(context.Background(), store, snapshot.SnapshotID, "tenant-a")
	if len(findings) > 0 {
		t.Fatalf("the owning tenant must be admitted, got %v", findings)
	}
	if admitted.SnapshotID != snapshot.SnapshotID {
		t.Fatalf("admitted the wrong snapshot")
	}
}

// S7: the acceptance criterion that matters operationally - a completed run keeps
// its closure after the underlying asset changes.
func TestS7CompletedRunKeepsItsClosure(t *testing.T) {
	store := newMemStore()
	before, _ := NewSnapshot(m1Lock(t), "tenant-a", "")
	if err := store.Persist(context.Background(), *before); err != nil {
		t.Fatalf("persist before: %v", err)
	}

	// The world moves on: a new asset version exists, so a new resolution differs.
	base := exampleSource(t)
	assets := base.Assets()
	for i := range assets {
		if assets[i].Ref == "tool/score-lead@1.0.0" {
			assets[i].Bytes = assetBytes(map[string]any{
				"metadata": map[string]any{"name": "score-lead", "publisher": "acme", "version": "2.0.0"},
				"spec":     map[string]any{"description": "changed"},
			})
		}
	}
	assets = append(assets,
		stub("agent/followup-drafter@0.9.0", "acme"), stub("agent/draft-reviewer@0.4.0", "acme"),
		stub("knowledge/icp-docs@5.0.0", "acme"), stub("model-policy/standard@1.0.0", "acme"),
		stub("model-policy/cheap@1.0.0", "acme"), stub("eval/lead-set@2.0.0", "acme"))
	changedSource, err := NewBundledSource(assets...)
	if err != nil {
		t.Fatalf("compose changed source: %v", err)
	}
	after := resolveOK(t, changedSource, fixture(t, "m1-team.system.json"))
	afterSnapshot, err := NewSnapshot(after, "tenant-a", "")
	if err != nil {
		t.Fatalf("NewSnapshot after: %v", err)
	}
	if afterSnapshot.SnapshotID == before.SnapshotID {
		t.Fatal("a changed asset must produce a different snapshot")
	}

	// The original run still reads exactly what it started with.
	admitted, findings := AdmitRun(context.Background(), store, before.SnapshotID, "tenant-a")
	if len(findings) > 0 {
		t.Fatalf("the original snapshot must still admit, got %v", findings)
	}
	if string(admitted.CanonicalLock) != string(before.CanonicalLock) {
		t.Fatal("the stored snapshot changed underneath the run")
	}
	restored, err := admitted.Lock()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, entry := range restored.ResolvedClosure {
		if entry.Ref == "tool/score-lead@1.0.0" {
			if !strings.HasPrefix(entry.Digest, DigestPrefix) {
				t.Fatalf("closure entry lost its digest: %+v", entry)
			}
			return
		}
	}
	t.Fatal("the original closure no longer contains the tool it started with")
}
