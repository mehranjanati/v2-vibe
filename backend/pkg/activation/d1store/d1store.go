// Package d1store is the D1-backed activation.TraceStore (ADI-CTX-004).
//
// It depends on a two-method interface rather than on the Cloudflare client, so
// cloudflare.D1Client satisfies it structurally and tests satisfy it with a map.
// The package owns one row per run: the whole decision record, plus the
// identity columns that make the row findable and a digest that lets the row
// detect its own corruption.
//
// There is deliberately no update path. A trace is append-only: a re-decision is
// a new run with its own record, exactly like a snapshot is a new snapshot.
package d1store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"backend/pkg/activation"
)

// Querier is the subset of the D1 client this store needs.
type Querier interface {
	Query(ctx context.Context, sql string, args ...interface{}) ([]map[string]interface{}, error)
	Exec(ctx context.Context, sql string, args ...interface{}) (bool, error)
}

// Store persists activation decision traces in D1.
type Store struct {
	DB Querier
}

// New builds a store over an existing D1 client.
func New(db Querier) *Store { return &Store{DB: db} }

// insertSQL relies on the primary key for idempotency: a replay of the same
// decision resolves to the same digest, so persisting it twice is a no-op
// rather than a duplicate row (the first write keeps its `decided_at`).
const insertSQL = `INSERT INTO activation_decision_traces
(run_id, tenant_key, context_id, composition_digest, activation_policy_ref, ranking_source, ranking_version, decided_at, trace_digest, record)
VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)
ON CONFLICT(run_id) DO NOTHING`

const selectSQL = `SELECT run_id, tenant_key, trace_digest, record FROM activation_decision_traces WHERE run_id = ?`

// Persist writes the record if the run has none and verifies that what is
// stored afterwards is the same decision. A different record for the same run is
// refused (ErrTraceImmutable) instead of overwritten, and another tenant's run id
// is refused (ErrTraceTenant) instead of shared.
func (s *Store) Persist(ctx context.Context, record activation.Record) error {
	if s == nil || s.DB == nil {
		return errors.New("activation/d1store: no database configured")
	}
	if err := activation.ValidateRecord(record); err != nil {
		return err
	}
	digest, err := record.Digest()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("activation/d1store: encode record: %w", err)
	}
	var decidedAt interface{}
	if record.DecidedAt != "" {
		parsed, err := time.Parse(time.RFC3339, record.DecidedAt)
		if err != nil {
			return fmt.Errorf("activation/d1store: decidedAt %q is not RFC 3339: %w", record.DecidedAt, err)
		}
		decidedAt = parsed.Unix()
	}
	if _, err := s.DB.Exec(ctx, insertSQL,
		record.RunID, record.TenantKey, record.ContextID, record.CompositionDigest,
		record.ActivationPolicyRef, record.Ranking.Source, record.Ranking.Version,
		decidedAt, digest, string(payload),
	); err != nil {
		return fmt.Errorf("activation/d1store: persist %s: %w", record.RunID, err)
	}

	stored, storedDigest, err := s.load(ctx, record.RunID)
	if err != nil {
		return err
	}
	if stored.TenantKey != record.TenantKey {
		return fmt.Errorf("%w: %s was first recorded by tenant %q", activation.ErrTraceTenant, record.RunID, stored.TenantKey)
	}
	if storedDigest != digest {
		return fmt.Errorf("%w: %s already holds a different decision", activation.ErrTraceImmutable, record.RunID)
	}
	return nil
}

// Load reads the record for a run.
func (s *Store) Load(ctx context.Context, runID string) (*activation.Record, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("activation/d1store: no database configured")
	}
	record, _, err := s.load(ctx, runID)
	return record, err
}

func (s *Store) load(ctx context.Context, runID string) (*activation.Record, string, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, "", fmt.Errorf("%w: runId is required", activation.ErrTraceMissing)
	}
	rows, err := s.DB.Query(ctx, selectSQL, runID)
	if err != nil {
		return nil, "", fmt.Errorf("activation/d1store: load %s: %w", runID, err)
	}
	if len(rows) == 0 {
		return nil, "", fmt.Errorf("%w: %s", activation.ErrTraceMissing, runID)
	}
	row := rows[0]
	raw := asString(row["record"])
	var record activation.Record
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return nil, "", fmt.Errorf("%w: row %s carries an undecodable record: %v", activation.ErrTraceCorrupt, runID, err)
	}
	storedDigest := asString(row["trace_digest"])
	computed, err := record.Digest()
	if err != nil {
		return nil, "", fmt.Errorf("activation/d1store: digest %s: %w", runID, err)
	}
	if computed != storedDigest {
		// The row no longer explains anything: refuse it rather than serve a
		// record whose identity columns and decisions disagree.
		return nil, "", fmt.Errorf("%w: %s stores %q but its record hashes to %q",
			activation.ErrTraceCorrupt, runID, storedDigest, computed)
	}
	if record.RunID != asString(row["run_id"]) || record.TenantKey != asString(row["tenant_key"]) {
		return nil, "", fmt.Errorf("%w: row %s identity columns disagree with the record", activation.ErrTraceCorrupt, runID)
	}
	return &record, storedDigest, nil
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
