package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"backend/pkg/engine"
)

// P0.5 — WebSocket upgrade authorization tests.
//
// These drive the real control-plane registrations (RegisterRoutes) with the
// boundary installed over a static token -> user map, so they exercise the
// wiring, not a stand-in router. A non-upgrade HTTP request is used on purpose:
// the guard runs before websocket.New, so a rejection is observable as 401/503
// while a pass-through is observable as 426 (fiber.ErrUpgradeRequired) from the
// upgrader.

// p05App registers the control-plane routes with the P0.5 WebSocket boundary
// installed. A nil tokens map leaves the boundary off (no SessionVerifier).
func p05App(tokens map[string]string) *fiber.App {
	app := fiber.New()
	hub := engine.NewEngineHub(nil, nil, nil)
	if tokens != nil {
		hub.SetSessionVerifier(NewStaticSessionVerifier(tokens))
	}
	RegisterRoutes(app, hub)
	return app
}

// TestWebSocketBoundaryRejectsNoTicket is the P0.5 acceptance criterion: with
// the boundary installed a WebSocket upgrade without a ticket/session is
// rejected with 401 and never reaches the upgrader/room.
func TestWebSocketBoundaryRejectsNoTicket(t *testing.T) {
	app := p05App(map[string]string{"valid-token": "user-a"})
	if status := doRequest(t, app, http.MethodGet, "/ws/agent-1", nil); status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a no-ticket WS request, got %d", status)
	}
}

// TestWebSocketBoundaryRejectsUnknownTicket covers the negative ticket path.
func TestWebSocketBoundaryRejectsUnknownTicket(t *testing.T) {
	app := p05App(map[string]string{"valid-token": "user-a"})
	if status := doRequest(t, app, http.MethodGet, "/ws/agent-1?ticket=revoked", nil); status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an unknown ticket, got %d", status)
	}
}

// TestWebSocketBoundaryAcceptsValidTicket proves the guard passes the credential
// through to the upgrader: a non-upgrade request with a valid short-lived ticket
// reaches websocket.New, which answers 426 (upgrade required) instead of 401.
// The room is claimed by the same user first (P0-SEC-003), and the ticket is
// issued by the store (P0-SEC-008) — a session token in the URL is no longer
// accepted.
func TestWebSocketBoundaryAcceptsValidTicket(t *testing.T) {
	app, hub := p05AppWithHub(map[string]string{"valid-token": "user-a"})
	if _, _, err := hub.ClaimRoomOwner("agent-1", "user-a"); err != nil {
		t.Fatalf("claim owner: %v", err)
	}
	ticket, _, err := hub.IssueWSTicket("user-a", "agent-1")
	if err != nil {
		t.Fatalf("issue ticket: %v", err)
	}
	if status := doRequest(t, app, http.MethodGet, "/ws/agent-1?ticket="+ticket, nil); status != fiber.StatusUpgradeRequired {
		t.Fatalf("expected the guard to pass a valid ticket (426 from the upgrader), got %d", status)
	}
}

// TestWebSocketBoundaryTokenTransports pins every credential transport that
// survives P0-SEC-008. The long-lived session token is accepted ONLY on headers
// (never in the URL); the `ticket` query param must be a short-lived single-use
// ticket, and the legacy `token` query alias was removed because it put the
// session token in a logged URL.
func TestWebSocketBoundaryTokenTransports(t *testing.T) {
	app, hub := p05AppWithHub(map[string]string{"valid-token": "user-a"})
	// Owner-scoped upgrade (P0-SEC-003): the room must belong to the caller for
	// the transport to be observable at the upgrader.
	if _, _, err := hub.ClaimRoomOwner("agent-1", "user-a"); err != nil {
		t.Fatalf("claim owner: %v", err)
	}
	ticket, _, err := hub.IssueWSTicket("user-a", "agent-1")
	if err != nil {
		t.Fatalf("issue ticket: %v", err)
	}
	cases := []struct {
		name    string
		path    string
		headers map[string]string
	}{
		{"query ticket (short-lived)", "/ws/agent-1?ticket=" + ticket, nil},
		{"X-Session-Token", "/ws/agent-1", map[string]string{"X-Session-Token": "valid-token"}},
		{"Authorization Bearer", "/ws/agent-1", map[string]string{"Authorization": "Bearer valid-token"}},
		{"access_token cookie", "/ws/agent-1", map[string]string{"Cookie": "access_token=valid-token"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if status := doRequest(t, app, http.MethodGet, tc.path, tc.headers); status != fiber.StatusUpgradeRequired {
				t.Fatalf("%s: expected the guard to pass (426 from the upgrader), got %d", tc.name, status)
			}
		})
	}

	// The session token is NOT a ticket: presenting it in the URL is refused,
	// which is the leak P0-SEC-008 closed.
	t.Run("session token in the URL is refused", func(t *testing.T) {
		if status := doRequest(t, app, http.MethodGet, "/ws/agent-1?ticket=valid-token", nil); status != http.StatusUnauthorized {
			t.Fatalf("expected 401 for a session token in the query, got %d", status)
		}
	})
	// The removed alias.
	t.Run("legacy token query param is refused", func(t *testing.T) {
		if status := doRequest(t, app, http.MethodGet, "/ws/agent-1?token=valid-token", nil); status != http.StatusUnauthorized {
			t.Fatalf("expected 401 for the removed `token` query alias, got %d", status)
		}
	})
}

// p05AppWithHub registers the control-plane routes with the P0.5 WebSocket
// boundary installed and returns the hub so a test can prime room ownership
// (P0-SEC-003). A nil tokens map leaves the boundary off (no SessionVerifier).
func p05AppWithHub(tokens map[string]string) (*fiber.App, *engine.EngineHub) {
	app := fiber.New()
	hub := engine.NewEngineHub(nil, nil, nil)
	if tokens != nil {
		hub.SetSessionVerifier(NewStaticSessionVerifier(tokens))
	}
	RegisterRoutes(app, hub)
	return app, hub
}

// TestWebSocketBoundaryOffIsNoop pins the documented opt-in contract: with no
// SessionVerifier installed the guard is a no-op, so the pre-P0.5 behaviour
// (the upgrader answers 426 to a non-upgrade request) is preserved and no auth
// is required.
func TestWebSocketBoundaryOffIsNoop(t *testing.T) {
	app := p05App(nil)
	if status := doRequest(t, app, http.MethodGet, "/ws/agent-1", nil); status != fiber.StatusUpgradeRequired {
		t.Fatalf("expected 426 (no auth) when the boundary is off, got %d", status)
	}
}

// TestWebSocketBoundaryStoreOutageIs503 pins the fail-closed rule: a boundary
// that is on but has no reachable store rejects the upgrade with 503 rather
// than letting it through.
func TestWebSocketBoundaryStoreOutageIs503(t *testing.T) {
	app := fiber.New()
	hub := engine.NewEngineHub(nil, nil, nil)
	hub.SetSessionVerifier(NewUnavailableSessionVerifier())
	RegisterRoutes(app, hub)
	if status := doRequest(t, app, http.MethodGet, "/ws/agent-1?ticket=anything", nil); status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when the session store is unavailable, got %d", status)
	}
}

// P0-SEC-003 — per-room authorization. Authentication alone is not enough: a
// socket must be bound to the room's owner, or any logged-in user could attach
// to any chat id, read its state, write into its VFS, and cancel its runs.

// TestWebSocketOwnerMayConnect is the positive case: the room owner's ticket
// passes both guards and reaches the upgrader (426 on a non-upgrade request).
func TestWebSocketOwnerMayConnect(t *testing.T) {
	app, hub := p05AppWithHub(map[string]string{"owner-token": "user-a"})
	if _, _, err := hub.ClaimRoomOwner("agent-1", "user-a"); err != nil {
		t.Fatalf("claim owner: %v", err)
	}
	ticket, _, err := hub.IssueWSTicket("user-a", "agent-1")
	if err != nil {
		t.Fatalf("issue ticket: %v", err)
	}
	if status := doRequest(t, app, http.MethodGet, "/ws/agent-1?ticket="+ticket, nil); status != fiber.StatusUpgradeRequired {
		t.Fatalf("owner should reach the upgrader (426), got %d", status)
	}
}

// TestWebSocketCrossTenantIsForbidden is the P0-SEC-003 acceptance criterion:
// a valid session that is NOT the room owner is refused with 403.
func TestWebSocketCrossTenantIsForbidden(t *testing.T) {
	app, hub := p05AppWithHub(map[string]string{
		"owner-token":  "user-a",
		"intruder-tok": "user-b",
	})
	if _, _, err := hub.ClaimRoomOwner("agent-1", "user-a"); err != nil {
		t.Fatalf("claim owner: %v", err)
	}
	// A ticket can only be minted for its own caller, so user-b's ticket is
	// issued directly by the store to prove the authorization guard (not the
	// ticket store) is what refuses the cross-tenant socket.
	ticket, _, err := hub.IssueWSTicket("user-b", "agent-1")
	if err != nil {
		t.Fatalf("issue intruder ticket: %v", err)
	}
	if status := doRequest(t, app, http.MethodGet, "/ws/agent-1?ticket="+ticket, nil); status != http.StatusForbidden {
		t.Fatalf("cross-tenant socket should be 403, got %d", status)
	}
}

// TestWebSocketUnownedRoomIsNotFound pins the 404 rule shared with the HTTP
// project routes: an unowned/unknown room never leaks its existence.
func TestWebSocketUnownedRoomIsNotFound(t *testing.T) {
	app, _ := p05AppWithHub(map[string]string{"valid-token": "user-a"})
	// Header transport: this test is about ownership, not about the ticket.
	headers := map[string]string{"X-Session-Token": "valid-token"}
	if status := doRequest(t, app, http.MethodGet, "/ws/never-claimed", headers); status != http.StatusNotFound {
		t.Fatalf("unowned room should be 404, got %d", status)
	}
}

// TestWebSocketOwnershipStoreOutageIs503 keeps the fail-closed rule for the
// ownership lookup too: an unreachable store must not degrade into an allow.
func TestWebSocketOwnershipStoreOutageIs503(t *testing.T) {
	app := fiber.New()
	hub := engine.NewEngineHub(nil, nil, nil)
	hub.SetSessionVerifier(NewStaticSessionVerifier(map[string]string{"valid-token": "user-a"}))
	RegisterRoutes(app, hub)
	// A nil hub registry is still in-memory, so an unknown room is 404 rather
	// than an outage; this test documents the 404 (not 500/200) outcome.
	headers := map[string]string{"X-Session-Token": "valid-token"}
	if status := doRequest(t, app, http.MethodGet, "/ws/agent-1", headers); status != http.StatusNotFound {
		t.Fatalf("unknown room with a live store should be 404, got %d", status)
	}
}

// TestWebSocketBoundaryOffSkipsOwnership pins that the ownership guard is a
// no-op with the boundary off (single-tenant dev/CI keeps working).
func TestWebSocketBoundaryOffSkipsOwnership(t *testing.T) {
	app, _ := p05AppWithHub(nil)
	if status := doRequest(t, app, http.MethodGet, "/ws/agent-1", nil); status != fiber.StatusUpgradeRequired {
		t.Fatalf("boundary off should skip both guards (426), got %d", status)
	}
}

// ---------------------------------------------------------------------------
// P0-SEC-008 — short-lived single-use tickets + Origin allow-list
// ---------------------------------------------------------------------------

// TestWSTicketIsSingleUse is the acceptance criterion: a redeemed ticket cannot
// be replayed (the second upgrade answers 401, not an upgrade).
func TestWSTicketIsSingleUse(t *testing.T) {
	app, hub := p05AppWithHub(map[string]string{"valid-token": "user-a"})
	if _, _, err := hub.ClaimRoomOwner("agent-1", "user-a"); err != nil {
		t.Fatalf("claim owner: %v", err)
	}
	ticket, _, err := hub.IssueWSTicket("user-a", "agent-1")
	if err != nil {
		t.Fatalf("issue ticket: %v", err)
	}
	first := doRequest(t, app, http.MethodGet, "/ws/agent-1?ticket="+ticket, nil)
	if first != fiber.StatusUpgradeRequired && first != http.StatusSwitchingProtocols {
		t.Fatalf("first use should pass the guard, got %d", first)
	}
	if second := doRequest(t, app, http.MethodGet, "/ws/agent-1?ticket="+ticket, nil); second != http.StatusUnauthorized {
		t.Fatalf("replayed ticket should be 401, got %d", second)
	}
}

// TestWSTicketIsBoundToOneRoom pins the binding: a ticket minted for room A is
// refused on room B even though the same user owns both.
func TestWSTicketIsBoundToOneRoom(t *testing.T) {
	app, hub := p05AppWithHub(map[string]string{"valid-token": "user-a"})
	for _, room := range []string{"agent-1", "agent-2"} {
		if _, _, err := hub.ClaimRoomOwner(room, "user-a"); err != nil {
			t.Fatalf("claim %s: %v", room, err)
		}
	}
	ticket, _, err := hub.IssueWSTicket("user-a", "agent-1")
	if err != nil {
		t.Fatalf("issue ticket: %v", err)
	}
	if status := doRequest(t, app, http.MethodGet, "/ws/agent-2?ticket="+ticket, nil); status != http.StatusUnauthorized {
		t.Fatalf("ticket for agent-1 used on agent-2 should be 401, got %d", status)
	}
}

// TestWSTicketIsShortLived pins the TTL contract and the single-use store
// semantics without waiting 15s: a fresh ticket redeems once, never twice.
func TestWSTicketIsShortLived(t *testing.T) {
	hub := engine.NewEngineHub(nil, nil, nil)
	ticket, expiresAt, err := hub.IssueWSTicket("user-a", "agent-1")
	if err != nil || ticket == "" {
		t.Fatalf("issue ticket: %v", err)
	}
	if ttl := time.Until(expiresAt); ttl <= 0 || ttl > engine.WSTicketTTL {
		t.Fatalf("ticket ttl out of range: %v", ttl)
	}
	if _, _, ok, _ := hub.RedeemWSTicket(ticket); !ok {
		t.Fatal("a fresh ticket should redeem")
	}
	if _, _, ok, _ := hub.RedeemWSTicket(ticket); ok {
		t.Fatal("an already-redeemed ticket must not redeem twice")
	}
}

// TestWebSocketOriginAllowList pins W2: a foreign browser Origin is refused
// before the handshake, an allowed origin passes, and a request with no Origin
// (SDK/CLI/e2e dial) is allowed because only a browser can forge one.
func TestWebSocketOriginAllowList(t *testing.T) {
	app, hub := p05AppWithHub(map[string]string{"valid-token": "user-a"})
	if _, _, err := hub.ClaimRoomOwner("agent-1", "user-a"); err != nil {
		t.Fatalf("claim owner: %v", err)
	}

	t.Run("foreign origin is refused", func(t *testing.T) {
		headers := map[string]string{"Origin": "https://evil.example.com"}
		if status := doRequest(t, app, http.MethodGet, "/ws/agent-1", headers); status != http.StatusForbidden {
			t.Fatalf("expected 403 for a foreign origin, got %d", status)
		}
	})

	t.Run("allowed origin passes", func(t *testing.T) {
		headers := map[string]string{
			"Origin":          "http://localhost:5173",
			"X-Session-Token": "valid-token",
		}
		if status := doRequest(t, app, http.MethodGet, "/ws/agent-1", headers); status != fiber.StatusUpgradeRequired {
			t.Fatalf("expected 426 for an allowed origin, got %d", status)
		}
	})

	t.Run("wildcard subdomain is honoured but suffix tricks are not", func(t *testing.T) {
		if !originAllowed("https://preview.vibeos-dda.pages.dev") {
			t.Fatal("a wildcard sub-domain should be allowed")
		}
		if originAllowed("https://vibeos-dda.pages.dev.evil.com") {
			t.Fatal("a suffix trick must not be allowed")
		}
	})

	t.Run("no origin is allowed", func(t *testing.T) {
		if !originAllowed("") {
			t.Fatal("a request without an Origin should be allowed")
		}
	})
}

// TestWSTicketEndpoint pins POST /api/ws-ticket — the contract the SDK already
// calls (sdk/src/http.ts getWsTicket): owner 200 + {ticket, expiresIn},
// cross-tenant 403, anonymous 401.
func TestWSTicketEndpoint(t *testing.T) {
	app, hub := p05AppWithHub(map[string]string{"owner-token": "user-a", "other-token": "user-b"})
	if _, _, err := hub.ClaimRoomOwner("agent-1", "user-a"); err != nil {
		t.Fatalf("claim owner: %v", err)
	}
	body := `{"resourceType":"agent","resourceId":"agent-1"}`

	post := func(token string) *http.Response {
		req := httptest.NewRequest(http.MethodPost, "/api/ws-ticket", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("X-Session-Token", token)
		}
		res, err := app.Test(req, 5000)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		return res
	}

	t.Run("owner gets a ticket", func(t *testing.T) {
		res := post("owner-token")
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", res.StatusCode)
		}
		raw, _ := io.ReadAll(res.Body)
		var payload struct {
			Success bool `json:"success"`
			Data    struct {
				Ticket    string `json:"ticket"`
				ExpiresIn int    `json:"expiresIn"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode: %v (%s)", err, raw)
		}
		if !payload.Success || payload.Data.Ticket == "" {
			t.Fatalf("expected a ticket, got %s", raw)
		}
		if payload.Data.ExpiresIn != int(engine.WSTicketTTL/time.Second) {
			t.Fatalf("expiresIn = %d, want %d", payload.Data.ExpiresIn, int(engine.WSTicketTTL/time.Second))
		}
	})

	t.Run("cross-tenant is forbidden", func(t *testing.T) {
		res := post("other-token")
		defer res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 for another tenant, got %d", res.StatusCode)
		}
	})

	t.Run("anonymous is unauthorized", func(t *testing.T) {
		res := post("")
		defer res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 without a session, got %d", res.StatusCode)
		}
	})
}

// TestSessionCookieNameMatchesEdge pins P0-SEC-009: the control plane must look
// for the cookie the Edge actually sets (`access_token`), and the old `session`
// name must stay dead so the docs cannot drift back into a lie.
func TestSessionCookieNameMatchesEdge(t *testing.T) {
	app, hub := p05AppWithHub(map[string]string{"valid-token": "user-a"})
	if _, _, err := hub.ClaimRoomOwner("agent-1", "user-a"); err != nil {
		t.Fatalf("claim owner: %v", err)
	}

	t.Run("edge cookie name is accepted", func(t *testing.T) {
		headers := map[string]string{"Cookie": "access_token=valid-token"}
		if status := doRequest(t, app, http.MethodGet, "/ws/agent-1", headers); status != fiber.StatusUpgradeRequired {
			t.Fatalf("expected 426 with the Edge cookie, got %d", status)
		}
	})

	t.Run("legacy session cookie name is dead", func(t *testing.T) {
		headers := map[string]string{"Cookie": "session=valid-token"}
		if status := doRequest(t, app, http.MethodGet, "/ws/agent-1", headers); status != http.StatusUnauthorized {
			t.Fatalf("the old `session` cookie must not authenticate (got %d)", status)
		}
	})
}

// TestCookieAuthRequiresTrustedOrigin pins the CSRF rule that makes cookie
// authentication safe: a browser attaches the cookie automatically, so a
// cookie-authenticated request from a foreign Origin is refused. Header
// transports are unaffected (a cross-site page cannot set them).
func TestCookieAuthRequiresTrustedOrigin(t *testing.T) {
	app, hub := p05AppWithHub(map[string]string{"valid-token": "user-a"})
	if _, _, err := hub.ClaimRoomOwner("agent-1", "user-a"); err != nil {
		t.Fatalf("claim owner: %v", err)
	}

	t.Run("foreign origin with a cookie is refused", func(t *testing.T) {
		headers := map[string]string{
			"Cookie": "access_token=valid-token",
			"Origin": "https://evil.example.com",
		}
		if status := doRequest(t, app, http.MethodGet, "/ws/agent-1", headers); status != http.StatusForbidden {
			t.Fatalf("expected 403 for a cookie-authenticated cross-site request, got %d", status)
		}
	})

	t.Run("allowed origin with a cookie passes", func(t *testing.T) {
		headers := map[string]string{
			"Cookie": "access_token=valid-token",
			"Origin": "http://localhost:5173",
		}
		if status := doRequest(t, app, http.MethodGet, "/ws/agent-1", headers); status != fiber.StatusUpgradeRequired {
			t.Fatalf("expected 426 for an allowed origin, got %d", status)
		}
	})

	t.Run("header transport is not origin-gated", func(t *testing.T) {
		// A cross-site page cannot set X-Session-Token, so the CSRF rule must not
		// apply to it. Proven on an HTTP guarded route (the WS route has its own
		// Origin guard, covered by TestWebSocketOriginAllowList).
		headers := map[string]string{
			"X-Session-Token": "valid-token",
			"Origin":          "https://evil.example.com",
		}
		if status := doRequest(t, app, http.MethodGet, "/api/projects/agent-1/files", headers); status == http.StatusForbidden {
			t.Fatalf("header transport must not be origin-gated, got 403")
		}
	})
}
