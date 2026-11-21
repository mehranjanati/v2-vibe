package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestConnectAgentEnvelope pins the GET /api/agent/:id/connect contract: the
// body is the standard envelope and AgentConnectionData ({agentId,
// websocketUrl}) lives under `data`. Both clients read `data` —
// src/lib/api-client.ts#connectToAgent and sdk/src/client.ts#connect — so a
// flat body silently breaks reopening an existing chat: the SPA throws
// "Failed to connect to agent", never opens the WebSocket, and the preview and
// History views stay empty (found live on production 2026-11-21).
func TestConnectAgentEnvelope(t *testing.T) {
	app, hub := guardedTestAppWithHub(map[string]string{"token-a": "user-a"})

	// The route runs ownerGuard, which answers 404 for an unowned chat, so the
	// caller must own it first (exactly the state a reopened chat is in).
	if _, _, err := hub.ClaimRoomOwner("chat-1", "user-a"); err != nil {
		t.Fatalf("ClaimRoomOwner: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agent/chat-1/connect", nil)
	req.Header.Set("X-Session-Token", "token-a")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("GET /api/agent/chat-1/connect failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var body struct {
		Success bool `json:"success"`
		Data    *struct {
			AgentID      string `json:"agentId"`
			WebSocketURL string `json:"websocketUrl"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if !body.Success {
		t.Errorf("success=false, want true")
	}
	if body.Data == nil {
		t.Fatalf("missing `data` envelope: the SPA and the SDK read data.agentId/data.websocketUrl")
	}
	if body.Data.AgentID != "chat-1" {
		t.Errorf("data.agentId=%q, want chat-1", body.Data.AgentID)
	}
	if !strings.Contains(body.Data.WebSocketURL, "/ws/chat-1") {
		t.Errorf("data.websocketUrl=%q, want it to address /ws/chat-1", body.Data.WebSocketURL)
	}
	if got := hub.RoomCount(); got != 1 {
		t.Errorf("rooms=%d, want 1 (connect must ensure the room the upgrade targets)", got)
	}
}
