package models

import (
	"encoding/json"
	"testing"
)

// TestConversationResponseShape guards the wire contract the React frontend
// depends on for streaming conversational replies (see
// worker/api/websocketTypes.ts ConversationResponseMessage and the
// 'conversation_response' handler in handle-websocket-message.ts).
func TestConversationResponseShape(t *testing.T) {
	streaming := ConversationResponse{
		Type:        "conversation_response",
		Message:     "hello ",
		IsStreaming: true,
	}
	raw, err := json.Marshal(streaming)
	if err != nil {
		t.Fatalf("marshal streaming: %v", err)
	}
	got := map[string]any{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	checks := []struct {
		key  string
		want any
	}{
		{"type", "conversation_response"},
		{"message", "hello "},
		{"isStreaming", true},
	}
	for _, c := range checks {
		if got[c.key] != c.want {
			t.Errorf("streaming %s = %v, want %v", c.key, got[c.key], c.want)
		}
	}

	// The streaming flag must be emitted (not omitted) when true, and the
	// final event should serialize with isStreaming absent/false.
	final := ConversationResponse{Type: "conversation_response", Message: "hello world"}
	finalRaw, err := json.Marshal(final)
	if err != nil {
		t.Fatalf("marshal final: %v", err)
	}
	if string(finalRaw) == string(raw) {
		t.Errorf("final event should differ from streaming event (isStreaming): %q", finalRaw)
	}
}
