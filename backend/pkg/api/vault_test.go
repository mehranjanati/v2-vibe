package api

// P1-AGT-004 (D8.4) — the credential store over HTTP.
//
// Same shape as the MCP route tests: real Fiber routes, an in-memory D1 fake,
// and the session boundary on, so the tenant always comes from the session.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"

	"backend/pkg/engine"
	"backend/pkg/vault"
)

// fakeVaultD1 implements vault.D1 by matching the store's statements.
type fakeVaultD1 struct {
	mu   sync.Mutex
	rows map[string]map[string]interface{} // tenant+"\x00"+name
}

func newFakeVaultD1() *fakeVaultD1 {
	return &fakeVaultD1{rows: map[string]map[string]interface{}{}}
}

func vaultKey(tenant, name string) string { return tenant + "\x00" + name }

func (f *fakeVaultD1) Exec(_ context.Context, sql string, args ...interface{}) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case strings.HasPrefix(sql, "INSERT INTO vault_secrets"):
		tenant, name, envelope := args[1].(string), args[2].(string), args[3].(string)
		row, exists := f.rows[vaultKey(tenant, name)]
		if !exists {
			row = map[string]interface{}{
				"id": args[0], "user_id": tenant, "name": name, "created_at": args[4],
			}
		}
		row["envelope"] = envelope
		row["updated_at"] = args[5]
		f.rows[vaultKey(tenant, name)] = row
	case strings.HasPrefix(sql, "DELETE FROM vault_secrets"):
		delete(f.rows, vaultKey(args[0].(string), args[1].(string)))
	}
	return true, nil
}

func (f *fakeVaultD1) Query(_ context.Context, sql string, args ...interface{}) ([]map[string]interface{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tenant := args[0].(string)
	switch {
	case strings.HasPrefix(sql, "SELECT envelope"):
		row, ok := f.rows[vaultKey(tenant, args[1].(string))]
		if !ok {
			return nil, nil
		}
		return []map[string]interface{}{{"envelope": row["envelope"]}}, nil
	case strings.HasPrefix(sql, "SELECT name,"):
		if len(args) > 1 {
			row, ok := f.rows[vaultKey(tenant, args[1].(string))]
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

// installTestVault attaches a store backed by the in-memory D1 fake.
func installTestVault(t *testing.T, hub *engine.EngineHub) (*vault.Store, *fakeVaultD1) {
	t.Helper()
	sealer, err := vault.NewSealer("test-vault-secret")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	d1 := newFakeVaultD1()
	store, err := vault.NewStore(d1, sealer)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	hub.SetVaultStore(store)
	return store, d1
}

func TestVaultRoutesRequireSession(t *testing.T) {
	t.Setenv("VAULT_SECRET", "")
	t.Setenv("JWT_SECRET", "")
	f := newMCPFixture(t, p07Tokens())
	installTestVault(t, f.hub)

	routes := []struct {
		method string
		path   string
		body   any
	}{
		{"GET", "/api/vault/status", nil},
		{"GET", "/api/vault/secrets", nil},
		{"POST", "/api/vault/secrets", map[string]any{"name": "crm/token", "value": "s3cret"}},
		{"DELETE", "/api/vault/secrets/x?name=crm/token", nil},
	}
	for _, route := range routes {
		status, _ := workflowRequest(t, f.app, route.method, route.path, "", route.body)
		if status != fiber.StatusUnauthorized {
			t.Fatalf("anonymous %s %s: expected 401, got %d", route.method, route.path, status)
		}
	}
}

func TestVaultStoresWithoutEchoingTheValue(t *testing.T) {
	f := newMCPFixture(t, p07Tokens())
	_, d1 := installTestVault(t, f.hub)

	status, body := workflowRequest(t, f.app, "POST", "/api/vault/secrets", "token-a", map[string]any{
		"name": "crm/token", "value": "s3cret",
	})
	if status != fiber.StatusCreated {
		t.Fatalf("put secret: expected 201, got %d (%v)", status, body)
	}
	data, _ := body["data"].(map[string]any)
	if ref, _ := data["ref"].(string); ref != "vault:crm/token" {
		t.Fatalf("the response must state the ref, got %v", data["ref"])
	}
	if strings.Contains(fmt.Sprint(body), "s3cret") {
		t.Fatalf("the value must never be echoed: %v", body)
	}
	stored, _ := d1.rows[vaultKey("user-a", "crm/token")]["envelope"].(string)
	if stored == "" || strings.Contains(stored, "s3cret") {
		t.Fatalf("D1 must hold an envelope, not the value: %q", stored)
	}

	status, body = workflowRequest(t, f.app, "GET", "/api/vault/secrets", "token-a", nil)
	if status != fiber.StatusOK {
		t.Fatalf("list secrets: expected 200, got %d (%v)", status, body)
	}
	if items, _ := body["data"].([]any); len(items) != 1 {
		t.Fatalf("expected one secret, got %v", body["data"])
	}

	status, body = workflowRequest(t, f.app, "GET", "/api/vault/status", "token-a", nil)
	if status != fiber.StatusOK {
		t.Fatalf("vault status: expected 200, got %d (%v)", status, body)
	}
	statusData, _ := body["data"].(map[string]any)
	if statusData["exists"] != true || statusData["mode"] != "server-encrypted" {
		t.Fatalf("status must describe the server-side store honestly: %v", statusData)
	}
	if count, _ := statusData["itemCount"].(float64); int(count) != 1 {
		t.Fatalf("expected itemCount 1, got %v", statusData["itemCount"])
	}
}

func TestVaultIsTenantScoped(t *testing.T) {
	f := newMCPFixture(t, p07Tokens())
	installTestVault(t, f.hub)

	if status, body := workflowRequest(t, f.app, "POST", "/api/vault/secrets", "token-a", map[string]any{
		"name": "crm/token", "value": "s3cret",
	}); status != fiber.StatusCreated {
		t.Fatalf("put as user-a: expected 201, got %d (%v)", status, body)
	}

	status, body := workflowRequest(t, f.app, "GET", "/api/vault/secrets", "token-b", nil)
	if status != fiber.StatusOK {
		t.Fatalf("list as user-b: expected 200, got %d (%v)", status, body)
	}
	if items, _ := body["data"].([]any); len(items) != 0 {
		t.Fatalf("user-b must not see user-a's secrets: %v", body["data"])
	}

	// Deleting another tenant's name is a no-op, not a cross-tenant delete.
	if status, body := workflowRequest(t, f.app, "DELETE", "/api/vault/secrets/x?name=crm/token", "token-b", nil); status != fiber.StatusOK {
		t.Fatalf("a no-op delete answers success, got %d (%v)", status, body)
	}
	status, body = workflowRequest(t, f.app, "GET", "/api/vault/secrets", "token-a", nil)
	if status != fiber.StatusOK {
		t.Fatalf("list as user-a: expected 200, got %d (%v)", status, body)
	}
	if items, _ := body["data"].([]any); len(items) != 1 {
		t.Fatalf("user-a's secret must survive another tenant's delete: %v", body["data"])
	}
}

func TestVaultUnavailableWithoutKeyMaterial(t *testing.T) {
	t.Setenv("VAULT_SECRET", "")
	t.Setenv("JWT_SECRET", "")
	f := newMCPFixture(t, p07Tokens())

	status, body := workflowRequest(t, f.app, "GET", "/api/vault/status", "token-a", nil)
	if status != fiber.StatusOK {
		t.Fatalf("status without a key: expected 200, got %d (%v)", status, body)
	}
	data, _ := body["data"].(map[string]any)
	if data["exists"] != false || data["configured"] != false {
		t.Fatalf("status must say the store is not configured: %v", data)
	}

	status, body = workflowRequest(t, f.app, "POST", "/api/vault/secrets", "token-a", map[string]any{
		"name": "crm/token", "value": "s3cret",
	})
	if status != fiber.StatusServiceUnavailable {
		t.Fatalf("writing without a key: expected 503, got %d (%v)", status, body)
	}
}

func TestVaultRejectsUnsafeNames(t *testing.T) {
	f := newMCPFixture(t, p07Tokens())
	installTestVault(t, f.hub)

	status, body := workflowRequest(t, f.app, "POST", "/api/vault/secrets", "token-a", map[string]any{
		"name": "../etc/passwd", "value": "s3cret",
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("unsafe name: expected 400, got %d (%v)", status, body)
	}
	if status, body := workflowRequest(t, f.app, "POST", "/api/vault/secrets", "token-a", map[string]any{
		"name": "crm/token",
	}); status != fiber.StatusBadRequest {
		t.Fatalf("missing value: expected 400, got %d (%v)", status, body)
	}
}
