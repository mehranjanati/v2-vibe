//go:build e2e

// P0.3 rollout verification — control-plane session boundary.
//
// This is the reproducible proof that turning CONTROL_PLANE_REQUIRE_SESSION on
// (backend/cmd/main.go) installs a real session boundary over the seven
// guarded control-plane routes (backend/pkg/api/routes.go). It exercises the
// SAME wiring production uses — RegisterRoutes + an installed
// engine.SessionVerifier + api.EdgeKVVerifier reading `session:token:<token>`
// through cloudflare.KVClient — and asserts the full 401 / 503 / pass / 403
// matrix across all seven routes.
//
// Two KV backends are supported:
//
//   - emu  (default): a faithful in-process Cloudflare KV emulator
//     (GET/PUT/DELETE .../values/<key>) so the whole flow is provable without
//     credentials; the only difference from real mode is the API base URL.
//   - real: the actual Edge VibecoderStore namespace (wrangler.v2.jsonc
//     kv_namespaces[0].id). Selected with VIBE_P03_KV=real, which requires
//     CLOUDFLARE_ACCOUNT_ID + CLOUDFLARE_API_TOKEN (Account → Workers KV →
//     Read+Write) + VIBECODER_STORE_KV_ID. The harness seeds one throwaway
//     `session:token:<nonce>` key and deletes it on exit.
//
// Run (emulator):        go test -tags e2e ./e2e/ -run P03 -v
// Run (real KV):
//
//	VIBE_P03_KV=real CLOUDFLARE_ACCOUNT_ID=… CLOUDFLARE_API_TOKEN=… \
//	  VIBECODER_STORE_KV_ID=56d249930a4447ee92493a7ff061a3ae \
//	  go test -tags e2e ./e2e/ -run P03 -v
//
// The 403 case is proven on the RequireOwner primitive here; the seven live
// routes now install the equivalent ownership guard (P0.4, ownerGuard +
// RequireResourceOwner), so a cross-user 403 also occurs on the real project
// routes when the resource is owned (see backend/pkg/api/auth_test.go).
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"backend/pkg/api"
	"backend/pkg/cloudflare"
	"backend/pkg/engine"
)

// p03Guard401 / p03Guard503 are the exact JSON `error` strings the boundary
// emits (backend/pkg/api/auth.go). Matching on them lets a handler-level 503
// (e.g. workflows without D1) be told apart from a boundary rejection.
const (
	p03Guard401 = "authentication required"
	p03Guard503 = "session store unavailable"
)

type p03Route struct {
	name   string
	method string
	path   string
}

// p03GuardedRoutes mirrors the seven guarded routes registered at
// backend/pkg/api/routes.go:302-321.
func p03GuardedRoutes() []p03Route {
	return []p03Route{
		{"POST /api/agent/session", http.MethodPost, "/api/agent/session"},
		{"GET /api/agent/:id/connect", http.MethodGet, "/api/agent/agent-1/connect"},
		{"POST /api/projects/:id/deploy", http.MethodPost, "/api/projects/proj-1/deploy"},
		{"GET /api/projects/:id/files", http.MethodGet, "/api/projects/proj-1/files"},
		{"POST /api/projects/:id/github-export", http.MethodPost, "/api/projects/proj-1/github-export"},
		{"POST /api/workflows/trigger", http.MethodPost, "/api/workflows/trigger"},
		{"GET /api/workflows/:workflowId", http.MethodGet, "/api/workflows/wf-1"},
	}
}

// p03KVBackend abstracts where `session:token:<token>` lives: the real Edge
// namespace (mode "real") or the in-process emulator (mode "emu").
type p03KVBackend struct {
	mode        string
	kv          *cloudflare.KVClient
	namespaceID string
	close       func()
}

func p03SetupKVBackend(t *testing.T) *p03KVBackend {
	t.Helper()
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("VIBE_P03_KV")))
	if mode == "" {
		mode = "emu"
	}
	switch mode {
	case "real":
		account := strings.TrimSpace(os.Getenv("CLOUDFLARE_ACCOUNT_ID"))
		token := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN"))
		ns := strings.TrimSpace(os.Getenv("VIBECODER_STORE_KV_ID"))
		if account == "" || token == "" || ns == "" {
			t.Fatalf("VIBE_P03_KV=real needs CLOUDFLARE_ACCOUNT_ID, CLOUDFLARE_API_TOKEN and " +
				"VIBECODER_STORE_KV_ID (the Edge VibecoderStore namespace id, wrangler.v2.jsonc kv_namespaces[0].id)")
		}
		return &p03KVBackend{
			mode:        "real",
			kv:          cloudflare.NewKVClient(account, token),
			namespaceID: ns,
			close:       func() {},
		}
	case "emu":
		srv, _ := p03NewKVEmulator()
		kv := cloudflare.NewKVClient("p03-acct", "p03-token")
		kv.SetBaseURL(srv.URL)
		return &p03KVBackend{mode: "emu", kv: kv, namespaceID: "p03-ns", close: srv.Close}
	default:
		t.Fatalf("VIBE_P03_KV must be \"emu\" or \"real\", got %q", mode)
		return nil
	}
}

// p03KVStore is the in-process Cloudflare KV emulator: a map guarded by a
// mutex that answers the same REST contract KVClient speaks.
type p03KVStore struct {
	mu   sync.Mutex
	data map[string]string
}

func p03NewKVEmulator() (*httptest.Server, *p03KVStore) {
	store := &p03KVStore{data: map[string]string{}}
	return httptest.NewServer(http.HandlerFunc(store.serveHTTP)), store
}

func (s *p03KVStore) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	const marker = "/values/"
	idx := strings.Index(r.URL.EscapedPath(), marker)
	if idx < 0 {
		http.NotFound(w, r)
		return
	}
	key, err := url.PathUnescape(r.URL.EscapedPath()[idx+len(marker):])
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		v, ok := s.data[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, v)
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		s.data[key] = string(body)
		_, _ = io.WriteString(w, `{"success":true}`)
	case http.MethodDelete:
		if _, ok := s.data[key]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(s.data, key)
		_, _ = io.WriteString(w, `{"success":true}`)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// p03BuildApp mirrors the opt-in branch in backend/cmd/main.go: installing a
// SessionVerifier on the hub turns on the guards in RegisterRoutes; nil deps
// (Redis/LLM/Cloudflare) are irrelevant to the boundary. An emulated D1 is
// attached so the workflow tenant guard (P0.7) can resolve ownership
// (unowned → 404) instead of failing closed (503) on a missing store.
func p03BuildApp(verifier engine.SessionVerifier) *fiber.App {
	return p03BuildAppWithD1(verifier, true)
}

// p03BuildAppWithoutD1 wires the fail-closed deployment: the boundary is on
// but the workflow ownership store is not configured, so the workflow routes
// answer 503 rather than serving an unscoped DAG.
func p03BuildAppWithoutD1(verifier engine.SessionVerifier) *fiber.App {
	return p03BuildAppWithD1(verifier, false)
}

func p03BuildAppWithD1(verifier engine.SessionVerifier, withD1 bool) *fiber.App {
	app := fiber.New()
	hub := engine.NewEngineHub(nil, nil, nil)
	hub.SetSessionVerifier(verifier)
	if withD1 {
		hub.SetD1Client(p03D1Client())
	}
	api.RegisterRoutes(app, hub)
	return app
}

// p03D1Client emulates D1 for the rollout proof: no workflow rows exist, so
// every workflow lookup resolves as unowned (→ 404 without leaking).
func p03D1Client() *cloudflare.D1Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"result":[{"results":[],"meta":{}}]}`))
	}))
	d1 := cloudflare.NewD1Client("p03-acct", "p03-token", "p03-db")
	d1.SetBaseURL(srv.URL)
	return d1
}

type p03Result struct {
	status int
	body   string
}

func p03Do(t *testing.T, app *fiber.App, method, path string, headers map[string]string) p03Result {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req, 20000)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return p03Result{status: resp.StatusCode, body: string(raw)}
}

// p03GuardError classifies a response as a boundary rejection ("401"/"503")
// or a pass ("") by matching the guard's JSON error string.
func p03GuardError(r p03Result) string {
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal([]byte(r.body), &body)
	switch {
	case r.status == http.StatusUnauthorized && body.Error == p03Guard401:
		return "401"
	case r.status == http.StatusServiceUnavailable && body.Error == p03Guard503:
		return "503"
	default:
		return ""
	}
}

// p03OwnerApp wires RequireSession + RequireOwner over the real verifier, an
// isolated proof of the ownership primitive (the live routes install the
// equivalent ownerGuard since P0.4); it yields 403 for a cross-user resource
// resolved through the same Edge-KV identity.
func p03OwnerApp(verifier engine.SessionVerifier) *fiber.App {
	app := fiber.New()
	app.Get("/own/:id",
		api.RequireSession(verifier),
		api.RequireOwner(func(c *fiber.Ctx) (string, bool) {
			switch c.Params("id") {
			case "mine":
				id, ok := api.CurrentIdentity(c)
				return id.UserID, ok
			case "theirs":
				return "someone-else", true
			default:
				return "", false // unknown/unowned resource -> 404
			}
		}),
		func(c *fiber.Ctx) error { return c.SendStatus(http.StatusOK) },
	)
	return app
}

// TestP03SessionBoundaryRollout is the acceptance proof: 401 (anonymous and
// unknown token), 503 (store unavailable), valid-token pass-through, and 403
// (owner scoping) across all seven guarded routes.
func TestP03SessionBoundaryRollout(t *testing.T) {
	backend := p03SetupKVBackend(t)
	defer backend.close()
	t.Logf("KV backend: %s (namespace %s)", backend.mode, backend.namespaceID)

	ctx := context.Background()
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())
	token := "p03-rollout-" + nonce
	userID := "p03-user-" + nonce
	key := "session:token:" + token

	if err := backend.kv.Put(ctx, backend.namespaceID, key, userID); err != nil {
		t.Fatalf("seed %s in %s KV: %v", key, backend.mode, err)
	}
	defer func() {
		if err := backend.kv.Delete(ctx, backend.namespaceID, key); err != nil {
			t.Errorf("cleanup %s in %s KV: %v", key, backend.mode, err)
		}
	}()

	verifier := api.NewEdgeKVVerifier(backend.kv, backend.namespaceID)
	if got, err := verifier.VerifyUserID(ctx, token); err != nil || got != userID {
		t.Fatalf("verifier failed to resolve the seeded token: got %q err %v", got, err)
	}
	t.Logf("verifier resolved %s -> %s", key, userID)

	routes := p03GuardedRoutes()
	guarded := p03BuildApp(verifier)
	unavailable := p03BuildAppWithoutD1(api.NewUnavailableSessionVerifier())

	// --- 401: anonymous (no token) on every guarded route ---
	t.Run("anonymous_401", func(t *testing.T) {
		for _, rt := range routes {
			r := p03Do(t, guarded, rt.method, rt.path, nil)
			if got := p03GuardError(r); got != "401" {
				t.Errorf("%s: expected guard 401, got status=%d body=%s", rt.name, r.status, r.body)
			}
		}
		t.Log("all 7 guarded routes reject anonymous callers with 401")
	})

	// --- 401: unknown token ---
	t.Run("unknown_token_401", func(t *testing.T) {
		h := map[string]string{"X-Session-Token": "p03-does-not-exist"}
		for _, rt := range routes {
			r := p03Do(t, guarded, rt.method, rt.path, h)
			if got := p03GuardError(r); got != "401" {
				t.Errorf("%s: expected guard 401 for an unknown token, got status=%d body=%s", rt.name, r.status, r.body)
			}
		}
		t.Log("all 7 guarded routes reject an unknown token with 401")
	})

	// --- 503: boundary on, session store unavailable/misconfigured ---
	// Exactly what main.go installs when CONTROL_PLANE_REQUIRE_SESSION is on
	// but the Edge store is not configured/unreachable: fail closed.
	t.Run("store_unavailable_503", func(t *testing.T) {
		h := map[string]string{"X-Session-Token": token}
		for _, rt := range routes {
			r := p03Do(t, unavailable, rt.method, rt.path, h)
			if got := p03GuardError(r); got != "503" {
				t.Errorf("%s: expected guard 503, got status=%d body=%s", rt.name, r.status, r.body)
			}
		}
		t.Log("all 7 guarded routes fail closed with 503 when the session store is unavailable")
	})

	// --- valid token: the guard passes and the handler runs ---
	t.Run("valid_token_passes", func(t *testing.T) {
		h := map[string]string{"X-Session-Token": token}
		for _, rt := range routes {
			r := p03Do(t, guarded, rt.method, rt.path, h)
			if got := p03GuardError(r); got != "" {
				t.Errorf("%s: guard rejected a valid token (%s): status=%d body=%s", rt.name, got, r.status, r.body)
				continue
			}
			t.Logf("%-34s guard passed -> handler status %d", rt.name, r.status)
		}
	})

	// --- three accepted token transports ---
	t.Run("token_transports", func(t *testing.T) {
		cases := []struct {
			name    string
			headers map[string]string
		}{
			{"X-Session-Token", map[string]string{"X-Session-Token": token}},
			{"Authorization: Bearer", map[string]string{"Authorization": "Bearer " + token}},
			{"session cookie", map[string]string{"Cookie": "session=" + token}},
		}
		for _, tc := range cases {
			r := p03Do(t, guarded, http.MethodGet, "/api/projects/p03/files", tc.headers)
			if got := p03GuardError(r); got != "" {
				t.Errorf("%s: valid token rejected (%s): status=%d body=%s", tc.name, got, r.status, r.body)
			} else {
				t.Logf("%-22s guard passed -> handler status %d", tc.name, r.status)
			}
		}
	})

	// --- 403: ownership scoping (RequireOwner primitive the routes use in P0.4) ---
	t.Run("owner_scoping", func(t *testing.T) {
		ownerApp := p03OwnerApp(verifier)
		if r := p03Do(t, ownerApp, http.MethodGet, "/own/mine", map[string]string{"X-Session-Token": token}); r.status != http.StatusOK {
			t.Errorf("own resource: expected 200, got %d (%s)", r.status, r.body)
		}
		if r := p03Do(t, ownerApp, http.MethodGet, "/own/theirs", map[string]string{"X-Session-Token": token}); r.status != http.StatusForbidden {
			t.Errorf("cross-user resource: expected 403, got %d (%s)", r.status, r.body)
		}
		if r := p03Do(t, ownerApp, http.MethodGet, "/own/ghost", map[string]string{"X-Session-Token": token}); r.status != http.StatusNotFound {
			t.Errorf("unknown resource: expected 404, got %d (%s)", r.status, r.body)
		}
		if r := p03Do(t, ownerApp, http.MethodGet, "/own/mine", nil); r.status != http.StatusUnauthorized {
			t.Errorf("anonymous owner access: expected 401, got %d (%s)", r.status, r.body)
		}
		t.Log("owner middleware: own=200, cross-user=403, unknown=404, anonymous=401")
	})
}
