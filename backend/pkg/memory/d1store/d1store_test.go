package d1store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"backend/pkg/memory"
)

// The adapter must satisfy the provider interface the platform depends on.
var _ memory.Provider = (*Store)(nil)

var clock = time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)

// fakeDB emulates the four statement shapes this store issues, so the tests
// exercise the real SQL strings and argument order without a database.
type fakeDB struct {
	rows []map[string]interface{}
	sqls []string
}

func newFakeDB() *fakeDB { return &fakeDB{} }

func (f *fakeDB) Exec(_ context.Context, sql string, args ...interface{}) (bool, error) {
	f.sqls = append(f.sqls, sql)
	switch {
	case strings.HasPrefix(sql, "INSERT INTO memory_records"):
		if !strings.Contains(sql, "ON CONFLICT(id) DO NOTHING") {
			return false, errors.New("insert is not conflict-safe")
		}
		id, _ := args[0].(string)
		for _, row := range f.rows {
			if row["id"] == id {
				return true, nil // DO NOTHING
			}
		}
		f.rows = append(f.rows, map[string]interface{}{
			"id": args[0], "tenant_key": args[1], "scope": args[2], "subject": args[3],
			"kind": args[4], "key": args[5], "text": args[6], "data": args[7],
			"source": args[8], "producer": args[9], "version": args[10],
			"created_at": args[11], "expires_at": args[12],
		})
		return true, nil
	case strings.HasPrefix(sql, "DELETE FROM memory_records"):
		kept := f.rows[:0:0]
		for _, row := range f.rows {
			if f.matchesDelete(row, sql, args) {
				continue
			}
			kept = append(kept, row)
		}
		f.rows = kept
		return true, nil
	}
	return true, nil
}

func (f *fakeDB) matchesDelete(row map[string]interface{}, sql string, args []interface{}) bool {
	if row["tenant_key"] != args[0] {
		return false
	}
	if strings.Contains(sql, "AND id = ?") {
		return row["id"] == args[1]
	}
	return row["scope"] == args[1] && row["subject"] == args[2]
}

func (f *fakeDB) Query(_ context.Context, sql string, args ...interface{}) ([]map[string]interface{}, error) {
	f.sqls = append(f.sqls, sql)
	switch {
	case strings.Contains(sql, "source = 'explicit'"):
		return f.filter(func(row map[string]interface{}) bool {
			return row["tenant_key"] == args[0] && row["scope"] == args[1] &&
				row["subject"] == args[2] && row["kind"] == args[3] &&
				row["key"] == args[4] && row["source"] == "explicit"
		}), nil
	case strings.Contains(sql, "AND id = ?"):
		return f.filter(func(row map[string]interface{}) bool {
			return row["tenant_key"] == args[0] && row["id"] == args[1]
		}), nil
	case strings.Contains(sql, "AND scope = ? AND subject = ?"):
		return f.filter(func(row map[string]interface{}) bool {
			return row["tenant_key"] == args[0] && row["scope"] == args[1] && row["subject"] == args[2]
		}), nil
	case strings.Contains(sql, "scope IN ("):
		return f.recall(sql, args), nil
	}
	return nil, fmt.Errorf("fakeDB: unhandled query %q", sql)
}

func (f *fakeDB) recall(sql string, args []interface{}) []map[string]interface{} {
	extra := 0
	if strings.Contains(sql, "AND kind = ?") {
		extra++
	}
	if strings.Contains(sql, "AND key = ?") {
		extra++
	}
	// args = tenant, subject, now, scopes..., [kind], [key], limit
	scopeCount := len(args) - 3 - extra - 1
	scopes := map[string]bool{}
	for _, raw := range args[3 : 3+scopeCount] {
		scopes[fmt.Sprintf("%v", raw)] = true
	}
	remaining := args[3+scopeCount:]
	now, _ := args[2].(int64)
	limit, _ := remaining[len(remaining)-1].(int)

	var out []map[string]interface{}
	for _, row := range f.rows {
		if row["tenant_key"] != args[0] || row["subject"] != args[1] {
			continue
		}
		if !scopes[fmt.Sprintf("%v", row["scope"])] {
			continue
		}
		if expires, ok := row["expires_at"].(int64); ok && expires <= now {
			continue
		}
		if strings.Contains(sql, "AND kind = ?") && row["kind"] != remaining[0] {
			continue
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, _ := out[i]["created_at"].(int64)
		right, _ := out[j]["created_at"].(int64)
		return left > right
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (f *fakeDB) filter(predicate func(map[string]interface{}) bool) []map[string]interface{} {
	var out []map[string]interface{}
	for _, row := range f.rows {
		if predicate(row) {
			out = append(out, row)
		}
	}
	return out
}

func fact(tenant, subject, text string, at time.Time) memory.Record {
	return memory.Record{
		TenantKey: tenant, Scope: memory.ScopeRun, Subject: subject, Kind: memory.KindFact, Text: text,
		Provenance: memory.Provenance{Source: memory.SourceObserved, Producer: "test", Version: "1.0.0"},
		CreatedAt:  at,
	}
}

func query(tenant string, scopes []memory.Scope, subject, text string) memory.Query {
	return memory.Query{
		TenantKey: tenant, CallerScope: memory.ScopeRun, Subject: subject, Scopes: scopes,
		Text: text, MaxItems: 10, MaxTokens: 1000, Now: clock,
	}
}

// M1 — a write then a read round-trips every field, including provenance and expiry.
func TestM1RoundTrip(t *testing.T) {
	db := newFakeDB()
	store := New(db)
	ctx := context.Background()
	expiry := clock.Add(24 * time.Hour)
	record := fact("tenant-a", "user-1", "user prefers dark mode", clock.Add(-time.Hour))
	record.ExpiresAt = &expiry
	id, err := store.Remember(ctx, record)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}

	// An expired record must not be recalled.
	expiredAt := clock.Add(-time.Minute)
	expired := fact("tenant-a", "user-1", "stale preference", clock.Add(-2*time.Hour))
	expired.ExpiresAt = &expiredAt
	if _, err := store.Remember(ctx, expired); err != nil {
		t.Fatalf("remember expired: %v", err)
	}

	items, err := store.Recall(ctx, query("tenant-a", []memory.Scope{memory.ScopeRun}, "user-1", "dark mode"))
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected exactly the unexpired record, got %d (%v)", len(items), items)
	}
	got := items[0]
	if got.ID != id || got.Text != record.Text || got.Provenance.Source != memory.SourceObserved || got.ExpiresAt == nil {
		t.Fatalf("round-trip lost fields: %+v", got)
	}
}

// M2 — the tenant wall: an empty tenant is refused, and a foreign id deletes nothing.
func TestM2TenantWall(t *testing.T) {
	db := newFakeDB()
	store := New(db)
	ctx := context.Background()

	if _, err := store.Recall(ctx, memory.Query{TenantKey: "", CallerScope: memory.ScopeRun, Subject: "u", Scopes: []memory.Scope{memory.ScopeRun}, MaxItems: 1, MaxTokens: 10, Now: clock}); !errors.Is(err, memory.ErrTenant) {
		t.Fatalf("expected ErrTenant for an empty tenant, got %v", err)
	}

	foreignID, err := store.Remember(ctx, fact("tenant-b", "user-1", "tenant b secret", clock.Add(-time.Hour)))
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	count, err := store.Forget(ctx, memory.ForgetRequest{TenantKey: "tenant-a", ID: foreignID})
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if count != 0 {
		t.Fatalf("a foreign id must delete nothing and report 0, got %d", count)
	}
	items, err := store.Recall(ctx, query("tenant-b", []memory.Scope{memory.ScopeRun}, "user-1", "secret"))
	if err != nil || len(items) != 1 {
		t.Fatalf("the record must survive a foreign forget: %v %v", items, err)
	}
}

// M3 — scope authority: a broader caller may not reach a narrower scope.
func TestM3ScopeAuthority(t *testing.T) {
	store := New(newFakeDB())
	query := memory.Query{
		TenantKey: "tenant-a", CallerScope: memory.ScopeTenant, Subject: "user-1",
		Scopes: []memory.Scope{memory.ScopeRun}, MaxItems: 1, MaxTokens: 10, Now: clock,
	}
	if _, err := store.Recall(context.Background(), query); !errors.Is(err, memory.ErrScope) {
		t.Fatalf("expected ErrScope, got %v", err)
	}
}

// M4 — an inferred value cannot overwrite an explicit one.
func TestM4InferredOverExplicitRefused(t *testing.T) {
	db := newFakeDB()
	store := New(db)
	ctx := context.Background()
	explicit := fact("tenant-a", "user-1", "formal", clock.Add(-2*time.Hour))
	explicit.Kind = memory.KindPreference
	explicit.Key = "preference.tone"
	explicit.Provenance.Source = memory.SourceExplicit
	if _, err := store.Remember(ctx, explicit); err != nil {
		t.Fatalf("remember explicit: %v", err)
	}

	inferred := explicit
	inferred.Provenance.Source = memory.SourceInferred
	inferred.Text = "casual"
	if _, err := store.Remember(ctx, inferred); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	if len(db.rows) != 1 {
		t.Fatalf("the inferred write must not be stored, rows=%d", len(db.rows))
	}
}

// M5 — a literal credential is refused on write.
func TestM5SecretRefused(t *testing.T) {
	store := New(newFakeDB())
	record := fact("tenant-a", "user-1", "crm access", clock)
	record.Data = map[string]any{"apiKey": "sk-live-51H8"}
	if _, err := store.Remember(context.Background(), record); !errors.Is(err, memory.ErrSecret) {
		t.Fatalf("expected ErrSecret, got %v", err)
	}

	vaulted := fact("tenant-a", "user-1", "crm access", clock)
	vaulted.Data = map[string]any{"apiKey": "vault:acme/crm"}
	if _, err := store.Remember(context.Background(), vaulted); err != nil {
		t.Fatalf("a vault reference must be accepted: %v", err)
	}
}

// M6 — a request beyond the advertised capability is refused, not clamped.
func TestM6BoundsRefused(t *testing.T) {
	store := New(newFakeDB())
	q := query("tenant-a", []memory.Scope{memory.ScopeRun}, "user-1", "")
	q.MaxItems = store.Capabilities().MaxItems + 1
	if _, err := store.Recall(context.Background(), q); !errors.Is(err, memory.ErrBounds) {
		t.Fatalf("expected ErrBounds, got %v", err)
	}
}

// M7 — ordering is total: score, then newest, then id.
func TestM7TotalOrdering(t *testing.T) {
	db := newFakeDB()
	store := New(db)
	ctx := context.Background()
	base := clock.Add(-10 * time.Hour)

	older := fact("tenant-a", "user-1", "alpha note", base)
	newer := fact("tenant-a", "user-1", "alpha note two", base.Add(2*time.Hour))
	tiedA := fact("tenant-a", "user-1", "alpha a", base.Add(time.Hour))
	tiedB := fact("tenant-a", "user-1", "alpha b", base.Add(time.Hour))
	tiedA.ID = "mem:aaa"
	tiedB.ID = "mem:bbb"
	// Pin the content ids so the expectation can name them; the store would
	// derive the same values on its own.
	older.ID = memory.ContentID(older)
	newer.ID = memory.ContentID(newer)
	for _, record := range []memory.Record{older, newer, tiedA, tiedB} {
		if _, err := store.Remember(ctx, record); err != nil {
			t.Fatalf("remember: %v", err)
		}
	}

	// An empty query scores everything zero, so the tie-break decides.
	items, err := store.Recall(ctx, query("tenant-a", []memory.Scope{memory.ScopeRun}, "user-1", ""))
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	var ids []string
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	want := []string{newer.ID, "mem:aaa", "mem:bbb", older.ID}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("ordering mismatch\n got %v\nwant %v", ids, want)
	}

	// A query term must float the matching record to the top.
	scored, err := store.Recall(ctx, query("tenant-a", []memory.Scope{memory.ScopeRun}, "user-1", "two"))
	if err != nil {
		t.Fatalf("recall scored: %v", err)
	}
	if len(scored) == 0 || scored[0].ID != newer.ID {
		t.Fatalf("the lexical match should rank first, got %+v", scored)
	}
}

// M8 — Forget reports the real count and is idempotent.
func TestM8ForgetCounts(t *testing.T) {
	db := newFakeDB()
	store := New(db)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := store.Remember(ctx, fact("tenant-a", "user-9", fmt.Sprintf("note %d", i), clock.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatalf("remember: %v", err)
		}
	}
	request := memory.ForgetRequest{TenantKey: "tenant-a", Scope: memory.ScopeRun, Subject: "user-9"}
	count, err := store.Forget(ctx, request)
	if err != nil || count != 2 {
		t.Fatalf("expected 2 deleted, got %d (%v)", count, err)
	}
	again, err := store.Forget(ctx, request)
	if err != nil || again != 0 {
		t.Fatalf("forget must be idempotent, got %d (%v)", again, err)
	}
}

// M9 — a store that cannot honour expiry refuses an expiring write.
func TestM9RetentionRefused(t *testing.T) {
	store := NewWithoutTTL(newFakeDB())
	expiry := clock.Add(time.Hour)
	record := fact("tenant-a", "user-1", "temporary", clock)
	record.ExpiresAt = &expiry
	if _, err := store.Remember(context.Background(), record); !errors.Is(err, memory.ErrRetention) {
		t.Fatalf("expected ErrRetention, got %v", err)
	}
}

// M10 — the provider is honest about being lexical.
func TestM10CapabilitiesAreHonest(t *testing.T) {
	semantic := New(newFakeDB()).Capabilities()
	if semantic.SemanticRecall {
		t.Fatal("the D1 adapter is lexical and must say so")
	}
	if !semantic.SupportsTTL {
		t.Fatal("the default store can honour expires_at")
	}
	if NewWithoutTTL(newFakeDB()).Capabilities().SupportsTTL {
		t.Fatal("the no-TTL store must not advertise TTL support")
	}
}

// A retried write of the same content is idempotent instead of duplicating.
func TestRememberIsIdempotent(t *testing.T) {
	db := newFakeDB()
	store := New(db)
	record := fact("tenant-a", "user-1", "same content", clock)
	first, err := store.Remember(context.Background(), record)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	second, err := store.Remember(context.Background(), record)
	if err != nil {
		t.Fatalf("remember again: %v", err)
	}
	if first != second || len(db.rows) != 1 {
		t.Fatalf("expected one row and one id, got %d rows (%s vs %s)", len(db.rows), first, second)
	}
}
