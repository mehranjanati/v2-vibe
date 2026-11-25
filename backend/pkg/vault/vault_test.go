package vault

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// P1-AGT-004 / D8.4 — the credential store's contract.
//
// The D1 fake below matches the real SQL by prefix (the pattern the repo uses
// for every D1 fake), so these tests exercise the statements the store issues,
// not a hand-waved interface.

const testTenant = "user-a"

type fakeD1 struct {
	rows map[string]map[string]interface{} // tenant+"\x00"+name
	fail bool
}

func newFakeD1() *fakeD1 { return &fakeD1{rows: map[string]map[string]interface{}{}} }

func key(tenant, name string) string { return tenant + "\x00" + name }

func (f *fakeD1) Exec(_ context.Context, sql string, args ...interface{}) (bool, error) {
	if f.fail {
		return false, errors.New("d1 down")
	}
	switch {
	case strings.HasPrefix(sql, "INSERT INTO vault_secrets"):
		tenant, name, envelope := args[1].(string), args[2].(string), args[3].(string)
		created, updated := args[4].(int64), args[5].(int64)
		row, exists := f.rows[key(tenant, name)]
		if !exists {
			row = map[string]interface{}{"id": args[0], "user_id": tenant, "name": name, "created_at": created}
		}
		row["envelope"] = envelope
		row["updated_at"] = updated
		f.rows[key(tenant, name)] = row
	case strings.HasPrefix(sql, "DELETE FROM vault_secrets"):
		delete(f.rows, key(args[0].(string), args[1].(string)))
	}
	return true, nil
}

func (f *fakeD1) Query(_ context.Context, sql string, args ...interface{}) ([]map[string]interface{}, error) {
	if f.fail {
		return nil, errors.New("d1 down")
	}
	tenant := args[0].(string)
	switch {
	case strings.HasPrefix(sql, "SELECT envelope"):
		row, ok := f.rows[key(tenant, args[1].(string))]
		if !ok {
			return nil, nil
		}
		return []map[string]interface{}{{"envelope": row["envelope"]}}, nil
	case strings.HasPrefix(sql, "SELECT name,"):
		if len(args) > 1 {
			row, ok := f.rows[key(tenant, args[1].(string))]
			if !ok {
				return nil, nil
			}
			return []map[string]interface{}{row}, nil
		}
		var out []map[string]interface{}
		for _, row := range f.rows {
			if row["user_id"] == tenant {
				out = append(out, row)
			}
		}
		return out, nil
	}
	return nil, nil
}

func testStore(t *testing.T) (*Store, *fakeD1) {
	t.Helper()
	sealer, err := NewSealer("test-secret")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	d1 := newFakeD1()
	store, err := NewStore(d1, sealer)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	store.SetClock(func() time.Time { return time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC) })
	return store, d1
}

func TestSealerBindsTheEnvelopeToTenantAndName(t *testing.T) {
	sealer, err := NewSealer("test-secret")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}

	envelope, err := sealer.Seal("user-a", "crm/token", "s3cret")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if !strings.HasPrefix(envelope, EnvelopePrefix) {
		t.Fatalf("sealed value must carry the %q marker, got %q", EnvelopePrefix, envelope)
	}
	again, _ := sealer.Seal("user-a", "crm/token", "s3cret")
	if again == envelope {
		t.Fatal("a fresh IV per seal is what keeps identical secrets distinct")
	}

	if value, err := sealer.Open("user-a", "crm/token", envelope); err != nil || value != "s3cret" {
		t.Fatalf("Open: got %q, %v", value, err)
	}
	if _, err := sealer.Open("user-b", "crm/token", envelope); err == nil {
		t.Fatal("another tenant's envelope must not open (AAD binds the tenant)")
	}
	if _, err := sealer.Open("user-a", "crm/other", envelope); err == nil {
		t.Fatal("a renamed envelope must not open (AAD binds the name)")
	}

	tampered := envelope[:len(envelope)-2] + "AA"
	if _, err := sealer.Open("user-a", "crm/token", tampered); err == nil {
		t.Fatal("a tampered envelope must fail, never decrypt to garbage")
	}
	if _, err := sealer.Open("user-a", "crm/token", "plaintext"); err == nil {
		t.Fatal("a value that is not an envelope must be refused")
	}

	if _, err := NewSealer(""); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("an empty secret must be ErrNotConfigured, got %v", err)
	}
}

func TestStoreRoundTripKeepsThePlaintextOutOfD1(t *testing.T) {
	store, d1 := testStore(t)
	ctx := context.Background()

	secret, err := store.Put(ctx, testTenant, "crm/token", "s3cret")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if secret.Name != "crm/token" || secret.CreatedAt.IsZero() {
		t.Fatalf("unexpected metadata: %+v", secret)
	}
	if strings.Contains(d1.rows[key(testTenant, "crm/token")]["envelope"].(string), "s3cret") {
		t.Fatal("the stored value must be an envelope, never plaintext")
	}

	if value, err := store.Get(ctx, testTenant, "crm/token"); err != nil || value != "s3cret" {
		t.Fatalf("Get: got %q, %v", value, err)
	}
	if _, err := store.Get(ctx, "user-b", "crm/token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another tenant must see ErrNotFound, got %v", err)
	}

	// Overwriting keeps one row and moves only the updated timestamp.
	first, _ := store.Put(ctx, testTenant, "crm/token", "rotated")
	if !first.CreatedAt.Equal(secret.CreatedAt) {
		t.Fatal("an upsert must keep the original createdAt")
	}
	if value, _ := store.Get(ctx, testTenant, "crm/token"); value != "rotated" {
		t.Fatalf("the rotated value must win, got %q", value)
	}

	list, err := store.List(ctx, testTenant)
	if err != nil || len(list) != 1 || list[0].Name != "crm/token" {
		t.Fatalf("List: got %+v, %v", list, err)
	}
	other, err := store.List(ctx, "user-b")
	if err != nil {
		t.Fatalf("a tenant with no secrets lists nothing, got %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("listing another tenant must not leak rows: %+v", other)
	}
}

func TestStoreResolvesRefsAtCallTimeOnly(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()
	if _, err := store.Put(ctx, testTenant, "crm/token", "s3cret"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	values, err := store.Resolve(ctx, testTenant, []string{"vault:crm/token"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if values["vault:crm/token"] != "s3cret" {
		t.Fatalf("Resolve must return the plaintext for its tenant, got %+v", values)
	}

	if _, err := store.Resolve(ctx, testTenant, []string{"vault:missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown ref must be ErrNotFound, got %v", err)
	}
	if _, err := store.Resolve(ctx, testTenant, []string{"crm/token"}); !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("a non-vault ref must be ErrInvalidRef, got %v", err)
	}
	if _, err := store.Resolve(ctx, "user-b", []string{"vault:crm/token"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another tenant's ref must be ErrNotFound, got %v", err)
	}
}

func TestStoreFailsClosed(t *testing.T) {
	ctx := context.Background()

	if _, err := NewStore(nil, nil); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("a store without D1 or a sealer must be ErrNotConfigured, got %v", err)
	}
	sealer, _ := NewSealer("test-secret")
	if _, err := NewStore(nil, sealer); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("a store without D1 must be ErrNotConfigured, got %v", err)
	}

	store, d1 := testStore(t)
	d1.fail = true
	if _, err := store.Put(ctx, testTenant, "crm/token", "s3cret"); err == nil {
		t.Fatal("a D1 failure must surface, never look like success")
	}
	if _, err := store.Get(ctx, testTenant, "crm/token"); err == nil {
		t.Fatal("a read failure must surface")
	}

	if err := store.Delete(ctx, testTenant, "bad name"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("an unsafe name must be ErrInvalidName, got %v", err)
	}
	if _, err := store.Put(ctx, testTenant, "crm/token", "   "); err == nil {
		t.Fatal("an empty value must be refused")
	}
	if _, err := store.Put(ctx, "", "crm/token", "s3cret"); err == nil {
		t.Fatal("a tenantless write must be refused")
	}
}

func TestParseRefIsStrict(t *testing.T) {
	if name, err := ParseRef("vault:crm/token"); err != nil || name != "crm/token" {
		t.Fatalf("ParseRef: got %q, %v", name, err)
	}
	for _, ref := range []string{"crm/token", "vault:", "vault:bad name", "vault:../etc/passwd"} {
		if _, err := ParseRef(ref); !errors.Is(err, ErrInvalidRef) {
			t.Fatalf("ParseRef(%q) must be ErrInvalidRef, got %v", ref, err)
		}
	}
}
