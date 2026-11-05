package api

import (
	"net/http"
	"testing"

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

// TestWebSocketBoundaryAcceptsValidTicket proves the guard passes a valid
// ticket through to the upgrader: a non-upgrade request with a valid ticket
// reaches websocket.New, which answers 426 (upgrade required) instead of 401.
func TestWebSocketBoundaryAcceptsValidTicket(t *testing.T) {
	app := p05App(map[string]string{"valid-token": "user-a"})
	if status := doRequest(t, app, http.MethodGet, "/ws/agent-1?ticket=valid-token", nil); status != fiber.StatusUpgradeRequired {
		t.Fatalf("expected the guard to pass a valid ticket (426 from the upgrader), got %d", status)
	}
}

// TestWebSocketBoundaryTokenTransports pins every transport the guard accepts:
// the `ticket`/`token` query params (the browser path) plus the HTTP header and
// cookie transports shared with RequireSession.
func TestWebSocketBoundaryTokenTransports(t *testing.T) {
	app := p05App(map[string]string{"valid-token": "user-a"})
	cases := []struct {
		name    string
		path    string
		headers map[string]string
	}{
		{"query ticket", "/ws/agent-1?ticket=valid-token", nil},
		{"query token", "/ws/agent-1?token=valid-token", nil},
		{"X-Session-Token", "/ws/agent-1", map[string]string{"X-Session-Token": "valid-token"}},
		{"Authorization Bearer", "/ws/agent-1", map[string]string{"Authorization": "Bearer valid-token"}},
		{"session cookie", "/ws/agent-1", map[string]string{"Cookie": "session=valid-token"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if status := doRequest(t, app, http.MethodGet, tc.path, tc.headers); status != fiber.StatusUpgradeRequired {
				t.Fatalf("%s: expected the guard to pass (426 from the upgrader), got %d", tc.name, status)
			}
		})
	}
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
