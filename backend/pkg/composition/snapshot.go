package composition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// DefaultRetentionClass is the retention policy a snapshot inherits unless the
// caller states otherwise (docs/architecture/run-snapshot.md §3.1).
const DefaultRetentionClass = "run"

// Snapshot is a stored, immutable lock: the durable value a run points at.
//
// The lock is a value the resolver produces with no database at all; the
// snapshot is that value made permanent. Keeping them apart is what lets the
// resolver stay testable and the run stay attributable.
type Snapshot struct {
	// SnapshotID is the lock digest. It is the primary key, so persisting an
	// unchanged closure is a no-op instead of a duplicate row.
	SnapshotID      string
	LockVersion     string
	SystemRef       string
	SystemDigest    string
	CompilerVersion string
	ResolutionMode  string
	// CanonicalLock is the whole lock document, including resolvedClosure.
	CanonicalLock string
	// TenantKey decides who may read the row. It is deliberately NOT part of the
	// digest: content identity must not depend on who happened to write it first.
	TenantKey string
	// ClosedAt is the lock's generatedAt, kept for audit only.
	ClosedAt      string
	RetentionClass string
}

var (
	// ErrSnapshotMissing means the run references a snapshot that is not stored.
	ErrSnapshotMissing = errors.New("composition: snapshot not found")
	// ErrSnapshotImmutable means a write tried to change an existing snapshot.
	ErrSnapshotImmutable = errors.New("composition: a stored snapshot cannot be rewritten")
	// ErrSnapshotInvalid means the stored document is not a usable lock.
	ErrSnapshotInvalid = errors.New("composition: stored snapshot is not a valid lock")
	// ErrSnapshotTenant means a write found the same content already recorded for
	// another tenant. Sharing is an open decision (run-snapshot.md §12), so the
	// conflict surfaces at write time instead of failing later at admission.
	ErrSnapshotTenant = errors.New("composition: snapshot is recorded for another tenant")
)

// SnapshotStore is the persistence seam. Implementations live outside this
// package (see composition/d1store), which is what keeps the resolver and these
// rules testable without a database.
type SnapshotStore interface {
	// Persist writes the snapshot if it does not exist. Identical content is a
	// no-op; different content under an existing id must return
	// ErrSnapshotImmutable. There is no update operation, by design.
	Persist(ctx context.Context, snapshot Snapshot) error
	// Load returns ErrSnapshotMissing when the id is unknown.
	Load(ctx context.Context, snapshotID string) (*Snapshot, error)
}

// NewSnapshot turns a lock into the record a run can reference.
func NewSnapshot(lock *Lock, tenantKey, retentionClass string) (*Snapshot, error) {
	if lock == nil {
		return nil, fmt.Errorf("composition: a snapshot needs a lock")
	}
	if strings.TrimSpace(tenantKey) == "" {
		return nil, fmt.Errorf("composition: a snapshot needs a tenant key")
	}
	canonical, err := lock.Canonical()
	if err != nil {
		return nil, err
	}
	digest, err := lock.Digest()
	if err != nil {
		return nil, err
	}
	if retentionClass == "" {
		retentionClass = DefaultRetentionClass
	}
	return &Snapshot{
		SnapshotID:      digest,
		LockVersion:     lock.LockVersion,
		SystemRef:       lock.SystemRef,
		SystemDigest:    lock.SystemDigest,
		CompilerVersion: lock.CompilerVersion,
		ResolutionMode:  lock.ResolutionMode,
		CanonicalLock:   string(canonical),
		TenantKey:       tenantKey,
		ClosedAt:        lock.GeneratedAt,
		RetentionClass:  retentionClass,
	}, nil
}

// Lock parses the stored document back into a lock.
func (s *Snapshot) Lock() (*Lock, error) {
	if s == nil || strings.TrimSpace(s.CanonicalLock) == "" {
		return nil, fmt.Errorf("%w: no canonical lock stored", ErrSnapshotInvalid)
	}
	var lock Lock
	if err := json.Unmarshal([]byte(s.CanonicalLock), &lock); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSnapshotInvalid, err)
	}
	return &lock, nil
}

// Verify recomputes the digest chain of a stored snapshot.
//
// It is the only enforcement point (docs/architecture/run-snapshot.md §6): a
// mismatch is a refusal, never a degraded run. Everything it needs is inside the
// snapshot, so admission never has to consult a registry.
func Verify(snapshot *Snapshot, wantSnapshotID string) []Finding {
	if snapshot == nil {
		return []Finding{{Code: CodeSnapshotMissing, Message: "the run references a snapshot that is not stored"}}
	}
	var findings []Finding
	fail := func(code Code, ref, format string, args ...any) {
		findings = append(findings, Finding{Code: code, Ref: ref, Message: fmt.Sprintf(format, args...)})
	}

	if wantSnapshotID != "" && snapshot.SnapshotID != wantSnapshotID {
		fail(CodeDigest, snapshot.SnapshotID, "stored snapshot id %q does not match the run's reference %q", snapshot.SnapshotID, wantSnapshotID)
	}

	lock, err := snapshot.Lock()
	if err != nil {
		fail(CodeDigest, snapshot.SnapshotID, "%v", err)
		return findings
	}
	recomputed, err := lock.Digest()
	if err != nil {
		fail(CodeDigest, snapshot.SnapshotID, "recomputed digest failed: %v", err)
		return findings
	}
	if recomputed != snapshot.SnapshotID {
		fail(CodeDigest, snapshot.SnapshotID, "recomputed lock digest %q does not match the stored id", recomputed)
	}
	if lock.LockVersion != snapshot.LockVersion {
		fail(CodeDigest, snapshot.SnapshotID, "stored lockVersion %q disagrees with the document %q", snapshot.LockVersion, lock.LockVersion)
	}
	if !strings.HasPrefix(snapshot.SystemDigest, DigestPrefix) {
		fail(CodeDigest, snapshot.SnapshotID, "systemDigest %q carries no algorithm prefix", snapshot.SystemDigest)
	}
	for _, entry := range lock.ResolvedClosure {
		if !strings.HasPrefix(entry.Digest, DigestPrefix) {
			fail(CodeDigest, entry.Ref, "closure entry carries no algorithm prefix (%q)", entry.Digest)
		}
	}
	return findings
}

// AdmitRun is what admission calls before a provider is asked to start a run.
//
// It exists so the three refusals that must happen before the first durable step
// - missing snapshot, wrong tenant, broken digest chain - are one call with one
// result shape, rather than three checks duplicated at each entry point.
func AdmitRun(ctx context.Context, store SnapshotStore, snapshotID, tenantKey string) (*Snapshot, []Finding) {
	if strings.TrimSpace(snapshotID) == "" {
		return nil, []Finding{{Code: CodeSnapshotMissing, Message: "the run carries no snapshot id; a run cannot start unbound"}}
	}
	if store == nil {
		return nil, []Finding{{Code: CodeSnapshotMissing, Message: "no snapshot store is installed, so the run's snapshot cannot be read"}}
	}
	snapshot, err := store.Load(ctx, snapshotID)
	if err != nil {
		if errors.Is(err, ErrSnapshotMissing) {
			return nil, []Finding{{Code: CodeSnapshotMissing, Ref: snapshotID, Message: "no snapshot is stored for this run"}}
		}
		return nil, []Finding{{Code: CodeSource, Ref: snapshotID, Message: fmt.Sprintf("snapshot store could not answer: %v", err)}}
	}
	if snapshot.TenantKey != tenantKey {
		return nil, []Finding{{Code: CodeTenant, Ref: snapshotID, Message: fmt.Sprintf("snapshot belongs to tenant %q, not %q", snapshot.TenantKey, tenantKey)}}
	}
	if findings := Verify(snapshot, snapshotID); len(findings) > 0 {
		return nil, sortFindings(findings)
	}
	return snapshot, nil
}
