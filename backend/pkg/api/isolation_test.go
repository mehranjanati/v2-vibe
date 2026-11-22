package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// P0-SEC-002 — cross-tenant isolation matrix.
//
// The other guard tests each own one family (auth_test → projects/agent,
// workflows_auth_test → workflows, ws_auth_test → the socket). The History API
// (generations list/diff/rollback) had no automated cross-tenant case: it was
// proven only by a manual production run. This file covers it and pins the whole
// matrix in one place so a future route cannot quietly ship with "authenticated
// but unscoped" behaviour.
//
// Contract asserted for every guarded resource family, with the boundary on and
// the resource owned by user A:
//
//	user B (valid session, wrong tenant) → 403
//	unknown/unowned resource             → 404 (existence is never leaked)
//	user A (owner)                       → anything except 401/403
//
// It runs against the real registrations (RegisterRoutes) over a static
// token → user map, so it exercises the wiring, not a stand-in router.

// doRawRequest performs a request and returns the status plus the body, for the
// few assertions that need more than a status code.
func doRawRequest(t *testing.T, app *fiber.App, method, path string, headers map[string]string) struct {
	status int
	body   string
} {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s %s failed: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return struct {
		status int
		body   string
	}{status: resp.StatusCode, body: string(raw)}
}

// guardedResourceCases is the isolation surface of the control plane. Keep it in
// step with the guarded registrations in routes.go.
func guardedResourceCases() []struct {
	name   string
	method string
	path   string
} {
	return []struct {
		name   string
		method string
		path   string
	}{
		{"projects files", http.MethodGet, "/api/projects/res-1/files"},
		{"projects deploy", http.MethodPost, "/api/projects/res-1/deploy"},
		{"projects github-export", http.MethodPost, "/api/projects/res-1/github-export"},
		{"agent connect", http.MethodGet, "/api/agent/res-1/connect"},
		{"generations list", http.MethodGet, "/api/generations/res-1"},
		{"generations diff", http.MethodGet, "/api/generations/res-1/gen-1/diff"},
		{"generations rollback", http.MethodPost, "/api/generations/res-1/gen-1/rollback"},
	}
}

// TestCrossTenantReadsAndWritesAreDenied is the P0-SEC-002 acceptance criterion:
// every guarded family denies a foreign tenant with 403 and an unknown resource
// with 404, while the owner is not denied.
func TestCrossTenantReadsAndWritesAreDenied(t *testing.T) {
	app, hub := p05AppWithHub(map[string]string{
		"token-a": "user-a",
		"token-b": "user-b",
	})
	if _, _, err := hub.ClaimRoomOwner("res-1", "user-a"); err != nil {
		t.Fatalf("claim owner: %v", err)
	}
	asA := map[string]string{"X-Session-Token": "token-a"}
	asB := map[string]string{"X-Session-Token": "token-b"}

	for _, tc := range guardedResourceCases() {
		t.Run(tc.name, func(t *testing.T) {
			// Foreign tenant: 403 (not 404 — the resource exists; the caller is
			// simply not its owner).
			if status := doRequest(t, app, tc.method, tc.path, asB); status != http.StatusForbidden {
				t.Errorf("cross-tenant %s %s: expected 403, got %d", tc.method, tc.path, status)
			}

			// Unknown resource: 404, never 403, so another tenant's ids are not
			// enumerable.
			unknown := strings.Replace(tc.path, "res-1", "never-owned", 1)
			if status := doRequest(t, app, tc.method, unknown, asB); status != http.StatusNotFound {
				t.Errorf("unknown resource %s %s: expected 404, got %d", tc.method, unknown, status)
			}

			// Anonymous: 401 (the session guard, not the owner guard).
			if status := doRequest(t, app, tc.method, tc.path, nil); status != http.StatusUnauthorized {
				t.Errorf("anonymous %s %s: expected 401, got %d", tc.method, tc.path, status)
			}

			// Owner: the guard must not deny them. The handler's own status
			// depends on the stubbed dependencies (nil D1/CF in this harness), so
			// only "not denied by the boundary" is asserted.
			if status := doRequest(t, app, tc.method, tc.path, asA); status == http.StatusUnauthorized || status == http.StatusForbidden {
				t.Errorf("owner %s %s: guard should not deny the owner, got %d", tc.method, tc.path, status)
			}
		})
	}
}

// TestGenerationRollbackDeniedDoesNotMutate pins that the History rollback is
// refused BEFORE any mutation: a foreign tenant gets 403, and the owner's
// resource is still reachable afterwards (so the refusal did not consume or
// corrupt it).
func TestGenerationRollbackDeniedDoesNotMutate(t *testing.T) {
	app, hub := p05AppWithHub(map[string]string{
		"token-a": "user-a",
		"token-b": "user-b",
	})
	if _, _, err := hub.ClaimRoomOwner("chat-9", "user-a"); err != nil {
		t.Fatalf("claim owner: %v", err)
	}

	if status := doRequest(t, app, http.MethodPost, "/api/generations/chat-9/gen-1/rollback", map[string]string{"X-Session-Token": "token-b"}); status != http.StatusForbidden {
		t.Fatalf("foreign rollback: expected 403, got %d", status)
	}
	// The owner still sees their own history resource afterward.
	if status := doRequest(t, app, http.MethodGet, "/api/generations/chat-9", map[string]string{"X-Session-Token": "token-a"}); status == http.StatusForbidden || status == http.StatusUnauthorized {
		t.Fatalf("owner history after a denied rollback: expected not-denied, got %d", status)
	}
}

// TestUnguardedLegacyAgentEndpointMintsNoTicket documents the one control-plane
// route that is deliberately unguarded (POST /api/agent only echoes a fresh id
// and never touches a room). The isolation property that matters: with the
// boundary on it must NOT hand out a WebSocket credential, because it has no
// identity to bind one to.
func TestUnguardedLegacyAgentEndpointMintsNoTicket(t *testing.T) {
	app, hub := p05AppWithHub(map[string]string{"token-a": "user-a"})
	_ = hub

	res := doRawRequest(t, app, http.MethodPost, "/api/agent", map[string]string{"X-Session-Token": "token-a"})
	if res.status != http.StatusOK {
		t.Fatalf("POST /api/agent: expected 200, got %d", res.status)
	}
	if strings.Contains(res.body, "ticket=") {
		t.Fatalf("unguarded /api/agent must not mint a WebSocket ticket, body: %s", res.body)
	}
	if !strings.Contains(res.body, "websocketUrl") {
		t.Fatalf("expected the legacy NDJSON payload, got: %s", res.body)
	}
}
