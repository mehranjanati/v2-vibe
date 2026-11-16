package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"backend/pkg/engine"
)

// TestCreateSessionRoomQuota pins the P0.8 HTTP contract: with the per-user cap
// at 1, a second POST /api/agent/session answers 429 ROOM_QUOTA_EXCEEDED and
// creates no room, while another user still gets theirs.
func TestCreateSessionRoomQuota(t *testing.T) {
	app, hub := guardedTestAppWithHub(map[string]string{
		"token-a": "user-a",
		"token-b": "user-b",
	})
	hub.SetLifecyclePolicy(engine.RoomLifecyclePolicy{MaxRoomsPerUser: 1, SweepInterval: time.Minute})

	post := func(token string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/agent/session", nil)
		req.Header.Set("X-Session-Token", token)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("POST /api/agent/session failed: %v", err)
		}
		defer resp.Body.Close()
		body := map[string]any{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("invalid JSON response: %v", err)
		}
		return resp.StatusCode, body
	}

	status, body := post("token-a")
	if status != http.StatusOK {
		t.Fatalf("first session: status=%d body=%v, want 200", status, body)
	}
	if got := hub.RoomCount(); got != 1 {
		t.Fatalf("after first session: %d rooms, want 1", got)
	}

	status, body = post("token-a")
	if status != http.StatusTooManyRequests {
		t.Fatalf("second session: status=%d body=%v, want 429", status, body)
	}
	if body["code"] != "ROOM_QUOTA_EXCEEDED" {
		t.Fatalf("second session: code=%v, want ROOM_QUOTA_EXCEEDED", body["code"])
	}
	if body["success"] != false {
		t.Fatalf("second session: success=%v, want false", body["success"])
	}
	if msg, _ := body["error"].(string); msg == "" {
		t.Fatalf("second session: missing error message in %v", body)
	}
	if got := hub.RoomCount(); got != 1 {
		t.Fatalf("refused session created a room: %d rooms, want 1", got)
	}

	// The cap is per user: another caller still gets a session.
	status, body = post("token-b")
	if status != http.StatusOK {
		t.Fatalf("user-b session: status=%d body=%v, want 200", status, body)
	}
	if got := hub.RoomCount(); got != 2 {
		t.Fatalf("after user-b session: %d rooms, want 2", got)
	}
}
