// Package d1store is the D1-backed memory provider (ADI-MEM-001a).
//
// It depends on a two-method interface rather than on the Cloudflare client, so
// cloudflare.D1Client satisfies it structurally and tests satisfy it with a
// map. The provider is lexical and says so: Capabilities.SemanticRecall is
// false, because the only honest alternative is a measured semantic upgrade
// (see docs/architecture/memory-provider.md §10).
package d1store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"backend/pkg/memory"
)

// Querier is the subset of the D1 client this store needs.
type Querier interface {
	Query(ctx context.Context, sql string, args ...interface{}) ([]map[string]interface{}, error)
	Exec(ctx context.Context, sql string, args ...interface{}) (bool, error)
}

// Store persists memory records in D1.
type Store struct {
	DB Querier
	// ttl records whether this database can honour expires_at. The D1 table has
	// the column, so the default store can; NewWithoutTTL exists so the refusal
	// path is reachable and tested rather than assumed.
	ttl bool
}

// New builds a store that can honour expiry.
func New(db Querier) *Store { return &Store{DB: db, ttl: true} }

// NewWithoutTTL builds a store that must refuse expiring writes.
func NewWithoutTTL(db Querier) *Store { return &Store{DB: db} }

// Capabilities is honest about what this provider does.
func (s *Store) Capabilities() memory.Capabilities {
	return memory.Capabilities{
		SemanticRecall: false, // lexical scoring, not embeddings
		SupportsTTL:    s.ttl,
		MaxItems:       memory.DefaultMaxItems,
		MaxTokens:      memory.DefaultMaxTokens,
	}
}

const insertSQL = `INSERT INTO memory_records
(id, tenant_key, scope, subject, kind, key, text, data, source, producer, version, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ? , ?, ?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING`

const selectRecallPrefix = `SELECT id, tenant_key, scope, subject, kind, key, text, data, source, producer, version, created_at, expires_at FROM memory_records`

// Remember stores a record and returns its id. Repeating the same content is a
// no-op with the same id (content-addressed), which is what makes a retried
// write safe.
func (s *Store) Remember(ctx context.Context, record memory.Record) (string, error) {
	if s == nil || s.DB == nil {
		return "", errors.New("memory/d1store: no database configured")
	}
	if err := memory.ValidateRecord(record); err != nil {
		return "", err
	}
	if record.ExpiresAt != nil && !s.ttl {
		return "", fmt.Errorf("%w: this store cannot honour expires_at", memory.ErrRetention)
	}
	if record.Provenance.Source == memory.SourceInferred && record.Key != "" {
		explicit, err := s.hasExplicit(ctx, record)
		if err != nil {
			return "", err
		}
		if explicit {
			return "", fmt.Errorf("%w: %s/%s/%s already has an explicit value", memory.ErrConflict, record.Scope, record.Subject, record.Key)
		}
	}

	id := record.ID
	if id == "" {
		id = memory.ContentID(record)
	}
	var data any
	if len(record.Data) > 0 {
		encoded, err := json.Marshal(record.Data)
		if err != nil {
			return "", fmt.Errorf("memory/d1store: encode data: %w", err)
		}
		data = string(encoded)
	}
	var expiresAt any
	if record.ExpiresAt != nil {
		expiresAt = record.ExpiresAt.Unix()
	}
	if _, err := s.DB.Exec(ctx, insertSQL,
		id, record.TenantKey, string(record.Scope), record.Subject, record.Kind, record.Key,
		record.Text, data, string(record.Provenance.Source), record.Provenance.Producer,
		record.Provenance.Version, record.CreatedAt.Unix(), expiresAt,
	); err != nil {
		return "", fmt.Errorf("memory/d1store: remember: %w", err)
	}
	return id, nil
}

// hasExplicit reports whether an explicit record already owns (scope, subject, key).
func (s *Store) hasExplicit(ctx context.Context, record memory.Record) (bool, error) {
	rows, err := s.DB.Query(ctx, selectRecallPrefix+
		" WHERE tenant_key = ? AND scope = ? AND subject = ? AND kind = ? AND key = ? AND source = 'explicit' LIMIT 1",
		record.TenantKey, string(record.Scope), record.Subject, record.Kind, record.Key)
	if err != nil {
		return false, fmt.Errorf("memory/d1store: check explicit: %w", err)
	}
	return len(rows) > 0, nil
}

// Recall returns bounded, ordered results. It fetches a bounded candidate window,
// scores lexically in Go, applies the total order and then enforces the caller's
// item and token budget.
func (s *Store) Recall(ctx context.Context, query memory.Query) ([]memory.Item, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("memory/d1store: no database configured")
	}
	if err := memory.ValidateQuery(query, s.Capabilities()); err != nil {
		return nil, err
	}

	candidates := query.MaxItems * 4
	if candidates > 200 {
		candidates = 200
	}
	args := []interface{}{query.TenantKey, query.Subject, query.Now.Unix()}
	sql := selectRecallPrefix + " WHERE tenant_key = ? AND subject = ? AND (expires_at IS NULL OR expires_at > ?)"
	sql += " AND scope IN (" + placeholders(len(query.Scopes)) + ")"
	for _, scope := range query.Scopes {
		args = append(args, string(scope))
	}
	if query.Kind != "" {
		sql += " AND kind = ?"
		args = append(args, query.Kind)
	}
	if query.Key != "" {
		sql += " AND key = ?"
		args = append(args, query.Key)
	}
	sql += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, candidates)

	rows, err := s.DB.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("memory/d1store: recall: %w", err)
	}

	items := make([]memory.Item, 0, len(rows))
	for _, row := range rows {
		record, err := recordFrom(row)
		if err != nil {
			return nil, err
		}
		items = append(items, memory.Item{
			Record: record,
			Score:  memory.Score(query.Text, record.Text),
			Tokens: memory.EstimateTokens(record.Text),
		})
	}
	memory.SortItems(items)

	kept := make([]memory.Item, 0, len(items))
	tokens := 0
	for _, item := range items {
		if len(kept) >= query.MaxItems {
			break
		}
		if tokens+item.Tokens > query.MaxTokens {
			continue // skip rather than exceed the budget; a smaller later item may still fit
		}
		tokens += item.Tokens
		kept = append(kept, item)
	}
	return kept, nil
}

// Forget removes by id, or by scope+subject, and returns the real count.
//
// A foreign id and a missing id are both "0" on purpose: reporting a different
// answer for a record that exists in another tenant would confirm its existence,
// which is the leak the tenant wall exists to prevent.
func (s *Store) Forget(ctx context.Context, request memory.ForgetRequest) (int, error) {
	if s == nil || s.DB == nil {
		return 0, errors.New("memory/d1store: no database configured")
	}
	if strings.TrimSpace(request.TenantKey) == "" {
		return 0, fmt.Errorf("%w: tenantKey is required", memory.ErrTenant)
	}

	var selectSQL, deleteSQL string
	var args []interface{}
	switch {
	case strings.TrimSpace(request.ID) != "":
		selectSQL = selectRecallPrefix + " WHERE tenant_key = ? AND id = ?"
		deleteSQL = "DELETE FROM memory_records WHERE tenant_key = ? AND id = ?"
		args = []interface{}{request.TenantKey, request.ID}
	case request.Scope != "" && strings.TrimSpace(request.Subject) != "":
		selectSQL = selectRecallPrefix + " WHERE tenant_key = ? AND scope = ? AND subject = ?"
		deleteSQL = "DELETE FROM memory_records WHERE tenant_key = ? AND scope = ? AND subject = ?"
		args = []interface{}{request.TenantKey, string(request.Scope), request.Subject}
	default:
		return 0, fmt.Errorf("%w: forget needs an id, or a scope and subject", memory.ErrInvalid)
	}

	rows, err := s.DB.Query(ctx, selectSQL, args...)
	if err != nil {
		return 0, fmt.Errorf("memory/d1store: forget lookup: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	if _, err := s.DB.Exec(ctx, deleteSQL, args...); err != nil {
		return 0, fmt.Errorf("memory/d1store: forget: %w", err)
	}
	return len(rows), nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func recordFrom(row map[string]interface{}) (memory.Record, error) {
	record := memory.Record{
		ID:        asString(row["id"]),
		TenantKey: asString(row["tenant_key"]),
		Scope:     memory.Scope(asString(row["scope"])),
		Subject:   asString(row["subject"]),
		Kind:      asString(row["kind"]),
		Key:       asString(row["key"]),
		Text:      asString(row["text"]),
		Provenance: memory.Provenance{
			Source:   memory.Source(asString(row["source"])),
			Producer: asString(row["producer"]),
			Version:  asString(row["version"]),
		},
		CreatedAt: asTime(row["created_at"]),
		ExpiresAt: asTimePtr(row["expires_at"]),
	}
	if raw := asString(row["data"]); raw != "" {
		if err := json.Unmarshal([]byte(raw), &record.Data); err != nil {
			return memory.Record{}, fmt.Errorf("memory/d1store: row %s has undecodable data: %w", record.ID, err)
		}
	}
	return record, nil
}

func asString(value interface{}) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprintf("%v", value)
}

func asTime(value interface{}) time.Time {
	if parsed := asTimePtr(value); parsed != nil {
		return *parsed
	}
	return time.Time{}
}

func asTimePtr(value interface{}) *time.Time {
	switch typed := value.(type) {
	case nil:
		return nil
	case int64:
		at := time.Unix(typed, 0).UTC()
		return &at
	case int:
		at := time.Unix(int64(typed), 0).UTC()
		return &at
	case float64:
		at := time.Unix(int64(typed), 0).UTC()
		return &at
	case string:
		if typed == "" {
			return nil
		}
		at, err := time.Parse(time.RFC3339, typed)
		if err != nil {
			return nil
		}
		return &at
	default:
		return nil
	}
}

// SortedScopes is a small helper for callers that build a query from a set.
func SortedScopes(scopes []memory.Scope) []memory.Scope {
	out := append([]memory.Scope{}, scopes...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
