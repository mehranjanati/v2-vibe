package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"backend/pkg/cloudflare"
	"backend/pkg/engine"
)

// guardedTestApp builds an app with the P0.3 boundary installed over a static
// token -> user map. The guarded routes are the real control-plane
// registrations (RegisterRoutes), so the tests exercise the wiring, not a
// stand-in router.
func guardedTestApp(tokens map[string]string) *fiber.App {
	app := fiber.New()
	hub := engine.NewEngineHub(nil, nil, nil)
	hub.SetSessionVerifier(NewStaticSessionVerifier(tokens))
	RegisterRoutes(app, hub)
	return app
}

func doRequest(t *testing.T, app *fiber.App, method, path string, headers map[string]string) int {
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
	return resp.StatusCode
}

// TestSessionBoundaryRejectsAnonymousCallers is the core P0.3 acceptance
// criterion: with the boundary installed, a mutating route without a session
// token answers 401 and never reaches its handler.
func TestSessionBoundaryRejectsAnonymousCallers(t *testing.T) {
	app := guardedTestApp(map[string]string{"valid-token": "user-a"})
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/agent/session"},
		{http.MethodGet, "/api/agent/agent-1/connect"},
		{http.MethodPost, "/api/projects/proj-1/deploy"},
		{http.MethodGet, "/api/projects/proj-1/files"},
		{http.MethodPost, "/api/projects/proj-1/github-export"},
		{http.MethodPost, "/api/workflows/trigger"},
		{http.MethodGet, "/api/workflows/wf-1"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			if status := doRequest(t, app, tc.method, tc.path, nil); status != http.StatusUnauthorized {
				t.Fatalf("expected 401 for an anonymous call, got %d", status)
			}
		})
	}
}

// TestSessionBoundaryRejectsUnknownToken covers the negative case where a
// token is presented but the store does not know it.
func TestSessionBoundaryRejectsUnknownToken(t *testing.T) {
	app := guardedTestApp(map[string]string{"valid-token": "user-a"})
	headers := map[string]string{"X-Session-Token": "revoked-token"}
	if status := doRequest(t, app, http.MethodPost, "/api/workflows/trigger", headers); status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an unknown token, got %d", status)
	}
}

// TestSessionBoundaryAcceptsValidToken proves the boundary is not a blanket
// deny: a known token passes the guard and the route handler runs.
func TestSessionBoundaryAcceptsValidToken(t *testing.T) {
	app := guardedTestApp(map[string]string{"valid-token": "user-a"})
	headers := map[string]string{"X-Session-Token": "valid-token"}
	if status := doRequest(t, app, http.MethodGet, "/api/projects/does-not-exist/files", headers); status != http.StatusOK {
		t.Fatalf("expected 200 for a valid token, got %d", status)
	}
}

// TestSessionTokenSources pins the three accepted transports.
func TestSessionTokenSources(t *testing.T) {
	app := guardedTestApp(map[string]string{"valid-token": "user-a"})
	cases := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"X-Session-Token header", map[string]string{"X-Session-Token": "valid-token"}, http.StatusOK},
		{"Authorization bearer", map[string]string{"Authorization": "Bearer valid-token"}, http.StatusOK},
		{"session cookie", map[string]string{"Cookie": "session=valid-token"}, http.StatusOK},
		{"wrong bearer", map[string]string{"Authorization": "Bearer nope"}, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if status := doRequest(t, app, http.MethodGet, "/api/projects/p/files", tc.headers); status != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, status)
			}
		})
	}
}

// TestSessionBoundaryFailsClosedWithoutStore pins the 503 fail-closed rule:
// an enforcement-enabled deployment whose session store is missing/unreachable
// must answer 503, never 200.
func TestSessionBoundaryFailsClosedWithoutStore(t *testing.T) {
	app := fiber.New()
	hub := engine.NewEngineHub(nil, nil, nil)
	hub.SetSessionVerifier(NewUnavailableSessionVerifier())
	RegisterRoutes(app, hub)

	headers := map[string]string{"X-Session-Token": "any-token"}
	if status := doRequest(t, app, http.MethodPost, "/api/workflows/trigger", headers); status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when the session store is unavailable, got %d", status)
	}
}

// TestSessionBoundaryPermissiveWhenUnconfigured pins the provisional state:
// with no verifier installed the guard is a no-op, so nothing regresses for a
// deployment that has not turned the boundary on yet.
func TestSessionBoundaryPermissiveWhenUnconfigured(t *testing.T) {
	app := newTestApp()
	if status := doRequest(t, app, http.MethodGet, "/api/projects/p/files", nil); status != http.StatusOK {
		t.Fatalf("expected 200 with the boundary unconfigured, got %d", status)
	}
}

// failingVerifier simulates an outage that is not a configuration error.
type failingVerifier struct{}

func (failingVerifier) VerifyUserID(context.Context, string) (string, error) {
	return "", errors.New("kv: http: connection refused")
}

// TestSessionBoundaryStoreOutageIs503 covers a live-store failure (not the
// "unconfigured" sentinel) — it must also fail closed.
func TestSessionBoundaryStoreOutageIs503(t *testing.T) {
	app := fiber.New()
	hub := engine.NewEngineHub(nil, nil, nil)
	hub.SetSessionVerifier(failingVerifier{})
	RegisterRoutes(app, hub)

	headers := map[string]string{"X-Session-Token": "any-token"}
	if status := doRequest(t, app, http.MethodGet, "/api/projects/p/files", headers); status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on a store outage, got %d", status)
	}
}

// TestRequireOwnerScopesResource covers the reusable ownership primitive that
// P0.4/P0.7 build on: the caller may only touch its own resource.
func TestRequireOwnerScopesResource(t *testing.T) {
	owners := map[string]string{"a1": "user-a", "b1": "user-b"}
	app := fiber.New()
	app.Get("/own/:id",
		RequireSession(NewStaticSessionVerifier(map[string]string{"tok-a": "user-a"})),
		RequireOwner(func(c *fiber.Ctx) (string, bool) {
			owner, ok := owners[c.Params("id")]
			return owner, ok
		}),
		func(c *fiber.Ctx) error { return c.SendStatus(http.StatusOK) },
	)

	cases := []struct {
		name    string
		id      string
		headers map[string]string
		want    int
	}{
		{"anonymous", "a1", nil, http.StatusUnauthorized},
		{"own resource", "a1", map[string]string{"X-Session-Token": "tok-a"}, http.StatusOK},
		{"cross-user resource", "b1", map[string]string{"X-Session-Token": "tok-a"}, http.StatusForbidden},
		{"unknown resource", "zz", map[string]string{"X-Session-Token": "tok-a"}, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if status := doRequest(t, app, http.MethodGet, "/own/"+tc.id, tc.headers); status != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, status)
			}
		})
	}
}

// TestRequireOwnerWithoutIdentityFailsClosed ensures the ownership middleware
// is never a silent allow when it is wired without RequireSession.
func TestRequireOwnerWithoutIdentityFailsClosed(t *testing.T) {
	app := fiber.New()
	app.Get("/own", RequireOwner(func(*fiber.Ctx) (string, bool) { return "user-a", true }),
		func(c *fiber.Ctx) error { return c.SendStatus(http.StatusOK) })

	if status := doRequest(t, app, http.MethodGet, "/own", nil); status != http.StatusUnauthorized {
		t.Fatalf("expected 401 when no identity is attached, got %d", status)
	}
}

// TestCurrentIdentityExposesResolvedUser pins the contract downstream
// ownership scoping (P0.4/P0.7) relies on.
func TestCurrentIdentityExposesResolvedUser(t *testing.T) {
	app := fiber.New()
	app.Get("/whoami",
		RequireSession(NewStaticSessionVerifier(map[string]string{"tok-a": "user-a"})),
		func(c *fiber.Ctx) error {
			identity, ok := CurrentIdentity(c)
			if !ok {
				return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "no identity"})
			}
			return c.JSON(fiber.Map{"userId": identity.UserID, "source": identity.Source})
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("X-Session-Token", "tok-a")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := map[string]any{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["userId"] != "user-a" || body["source"] != "header" {
		t.Fatalf("unexpected identity payload: %v", body)
	}
}

// TestEdgeKVVerifierResolvesEdgeSession exercises the production verifier
// against an httptest-backed KV API: the Edge key contract
// (`session:token:<token>` -> userId) must resolve, a missing key must be an
// unknown session, and an unconfigured store must fail closed.
func TestEdgeKVVerifierResolvesEdgeSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/accounts/acct/storage/kv/namespaces/ns-1/values/session:token:good-token" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("user-42"))
	}))
	defer srv.Close()

	kv := cloudflare.NewKVClient("acct", "token")
	kv.SetBaseURL(srv.URL)

	verifier := NewEdgeKVVerifier(kv, "ns-1")
	ctx := context.Background()

	userID, err := verifier.VerifyUserID(ctx, "good-token")
	if err != nil || userID != "user-42" {
		t.Fatalf("expected user-42, got %q (err %v)", userID, err)
	}

	userID, err = verifier.VerifyUserID(ctx, "revoked-token")
	if err != nil || userID != "" {
		t.Fatalf("expected an unknown session, got %q (err %v)", userID, err)
	}

	if _, err := NewEdgeKVVerifier(kv, "").VerifyUserID(ctx, "good-token"); !errors.Is(err, engine.ErrSessionStoreUnavailable) {
		t.Fatalf("expected ErrSessionStoreUnavailable without a namespace, got %v", err)
	}
	if _, err := NewEdgeKVVerifier(nil, "ns-1").VerifyUserID(ctx, "good-token"); !errors.Is(err, engine.ErrSessionStoreUnavailable) {
		t.Fatalf("expected ErrSessionStoreUnavailable without a KV client, got %v", err)
	}
}
