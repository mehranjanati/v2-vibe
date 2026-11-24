// Package d1store is the D1-backed implementation of composition.SnapshotStore.
//
// It deliberately depends on a two-method interface rather than on the
// Cloudflare client: cloudflare.D1Client satisfies Querier, and tests satisfy it
// with a map. That keeps this package free of Cloudflare types and free of
// network calls in tests.
//
// The store has no update operation. A snapshot is append-only by construction:
// there is no code path here that can rewrite a stored closure
// (docs/architecture/run-snapshot.md §5).
package d1store

import (
	"context"
	"fmt"
	"time"

	"backend/pkg/composition"
)

// Querier is the subset of the D1 client this store needs.
type Querier interface {
	Query(ctx context.Context, sql string, args ...interface{}) ([]map[string]interface{}, error)
	Exec(ctx context.Context, sql string, args ...interface{}) (bool, error)
}

// Store persists composition snapshots in D1.
type Store struct {
	DB Querier
}

// New builds a store over an existing D1 client.
func New(db Querier) *Store { return &Store{DB: db} }

// insertSQL relies on the primary key for deduplication: the same closure
// resolves to the same digest, so persisting it twice is a no-op rather than a
// duplicate row.
const insertSQL = `INSERT INTO composition_snapshots
(snapshot_id, lock_version, system_ref, system_digest, compiler_version, resolution_mode, canonical_lock, tenant_key, closed_at, retention_class)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(snapshot_id) DO NOTHING`

const selectSQL = `SELECT snapshot_id, lock_version, system_ref, system_digest, compiler_version, resolution_mode, canonical_lock, tenant_key, closed_at, retention_class
FROM composition_snapshots WHERE snapshot_id = ?`

// Persist writes the snapshot if it is new and verifies that what is stored now
// matches what was asked for. If the row already exists with different content
// (or under another tenant) it refuses instead of overwriting.
func (s *Store) Persist(ctx context.Context, snapshot composition.Snapshot) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("composition/d1store: no database configured")
	}
	var closedAt interface{}
	if snapshot.ClosedAt != "" {
		parsed, err := time.Parse(time.RFC3339, snapshot.ClosedAt)
		if err != nil {
			return fmt.Errorf("composition/d1store: closedAt %q is not RFC 3339: %w", snapshot.ClosedAt, err)
		}
		closedAt = parsed.Unix()
	}
	retention := snapshot.RetentionClass
	if retention == "" {
		retention = composition.DefaultRetentionClass
	}
	if _, err := s.DB.Exec(ctx, insertSQL,
		snapshot.SnapshotID, snapshot.LockVersion, snapshot.SystemRef, snapshot.SystemDigest,
		snapshot.CompilerVersion, snapshot.ResolutionMode, snapshot.CanonicalLock,
		snapshot.TenantKey, closedAt, retention,
	); err != nil {
		return fmt.Errorf("composition/d1store: persist %s: %w", snapshot.SnapshotID, err)
	}

	stored, err := s.Load(ctx, snapshot.SnapshotID)
	if err != nil {
		return err
	}
	if stored.CanonicalLock != snapshot.CanonicalLock {
		// Same id, different document: the id would be lying about the content.
		return fmt.Errorf("%w: %s already exists with different content", composition.ErrSnapshotImmutable, snapshot.SnapshotID)
	}
	if stored.TenantKey != snapshot.TenantKey {
		// First writer wins. Returning an error instead of silently sharing is
		// deliberate: a second tenant's runs would later be refused at admission
		// with E_TENANT, so the failure has to surface at write time.
		return fmt.Errorf("%w: %s was first recorded by tenant %q; cross-tenant sharing is an open decision (run-snapshot.md §12)",
			composition.ErrSnapshotTenant, snapshot.SnapshotID, stored.TenantKey)
	}
	return nil
}

// Load reads a snapshot by id.
func (s *Store) Load(ctx context.Context, snapshotID string) (*composition.Snapshot, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("composition/d1store: no database configured")
	}
	rows, err := s.DB.Query(ctx, selectSQL, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("composition/d1store: load %s: %w", snapshotID, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: %s", composition.ErrSnapshotMissing, snapshotID)
	}
	row := rows[0]
	return &composition.Snapshot{
		SnapshotID:      asString(row["snapshot_id"]),
		LockVersion:     asString(row["lock_version"]),
		SystemRef:       asString(row["system_ref"]),
		SystemDigest:    asString(row["system_digest"]),
		CompilerVersion: asString(row["compiler_version"]),
		ResolutionMode:  asString(row["resolution_mode"]),
		CanonicalLock:   asString(row["canonical_lock"]),
		TenantKey:       asString(row["tenant_key"]),
		ClosedAt:        asRFC3339(row["closed_at"]),
		RetentionClass:  asString(row["retention_class"]),
	}, nil
}

func asString(value interface{}) string {
	if value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return fmt.Sprintf("%v", value)
	}
	return text
}

func asRFC3339(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case int64:
		return time.Unix(typed, 0).UTC().Format(time.RFC3339)
	case int:
		return time.Unix(int64(typed), 0).UTC().Format(time.RFC3339)
	case float64:
		return time.Unix(int64(typed), 0).UTC().Format(time.RFC3339)
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", typed)
	}
}
