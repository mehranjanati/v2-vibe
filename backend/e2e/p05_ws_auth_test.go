//go:build e2e

// P0.5 rollout verification — WebSocket upgrade authorization.
//
// Self-contained proof that the /ws/:id upgrade runs the control-plane session
// boundary before the room is touched. It drives the SAME wiring production
// uses — api.RegisterRoutes + an installed engine.SessionVerifier — over a real
// TCP listener and dials it with a real WebSocket client, so the handshake
// accept/reject is observed exactly as a browser would see it (a rejected
// upgrade is a non-101 HTTP response, never an open socket).
//
// No credentials or external services are needed (nil Redis/LLM/Cloudflare
// clients are fine: the guard decides before any room work runs).
//
// Run: go test -tags e2e ./e2e/ -run P05 -v
package e2e

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gorilla/websocket"

	"backend/pkg/api"
	"backend/pkg/engine"
)

// p05Listen starts app on a random loopback port and returns its ws:// base
// URL, shutting the app down when the test ends.
func p05Listen(t *testing.T, app *fiber.App) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	return "ws://" + ln.Addr().String()
}

// p05Dial performs a WebSocket handshake and returns the HTTP status the server
// answered with (101 on a successful upgrade) plus the live connection (nil on
// rejection).
func p05Dial(t *testing.T, base, path string) (int, *websocket.Conn) {
	t.Helper()
	conn, resp, err := websocket.DefaultDialer.Dial(base+path, nil)
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	if err != nil && resp == nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	return status, conn
}

// TestP05WebSocketAuthorization proves the P0.5 acceptance criterion end to end:
// no ticket -> handshake rejected (401, no socket); unknown ticket -> rejected;
// valid ticket -> 101 upgrade with the agent handshake payloads; and the
// boundary-off deployment keeps the pre-P0.5 behaviour (upgrade without a
// ticket).
func TestP05WebSocketAuthorization(t *testing.T) {
	app := fiber.New()
	hub := engine.NewEngineHub(nil, nil, nil)
	hub.SetSessionVerifier(api.NewStaticSessionVerifier(map[string]string{"p05-valid": "user-a"}))
	api.RegisterRoutes(app, hub)
	base := p05Listen(t, app)

	// --- no ticket -> 401, no upgrade ---
	status, conn := p05Dial(t, base, "/ws/p05-room")
	if status != http.StatusUnauthorized {
		t.Fatalf("no-ticket connect: expected 401, got %d", status)
	}
	if conn != nil {
		conn.Close()
		t.Fatal("no-ticket connect: expected NO upgrade, but the handshake succeeded")
	}
	t.Log("no-ticket connect rejected with 401 (no upgrade)")

	// --- unknown ticket -> 401, no upgrade ---
	status, conn = p05Dial(t, base, "/ws/p05-room?ticket=p05-does-not-exist")
	if status != http.StatusUnauthorized {
		t.Fatalf("unknown-ticket connect: expected 401, got %d", status)
	}
	if conn != nil {
		conn.Close()
		t.Fatal("unknown-ticket connect: expected NO upgrade")
	}
	t.Log("unknown-ticket connect rejected with 401 (no upgrade)")

	// --- valid ticket -> 101 upgrade + handshake payloads ---
	status, conn = p05Dial(t, base, "/ws/p05-room?ticket=p05-valid")
	if status != http.StatusSwitchingProtocols || conn == nil {
		t.Fatalf("valid-ticket connect: expected 101 upgrade, got status=%d conn=%v", status, conn != nil)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	sawConnected := false
	for i := 0; i < 2; i++ {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("reading handshake payload %d: %v", i, err)
		}
		if strings.Contains(string(msg), "agent_connected") {
			sawConnected = true
		}
	}
	conn.Close()
	if !sawConnected {
		t.Fatal("valid-ticket connect: did not receive the agent_connected handshake payload")
	}
	t.Log("valid ticket upgraded (101) and delivered the agent_connected handshake")

	// --- boundary off -> no-ticket upgrade succeeds (pre-P0.5 behaviour) ---
	offApp := fiber.New()
	offHub := engine.NewEngineHub(nil, nil, nil)
	api.RegisterRoutes(offApp, offHub)
	offBase := p05Listen(t, offApp)
	status, conn = p05Dial(t, offBase, "/ws/p05-open")
	if status != http.StatusSwitchingProtocols || conn == nil {
		t.Fatalf("boundary off: expected 101 upgrade without a ticket, got status=%d conn=%v", status, conn != nil)
	}
	conn.Close()
	t.Log("boundary off preserved the pre-P0.5 behaviour (upgrade without a ticket)")
}
