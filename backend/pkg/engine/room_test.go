package engine

import (
	"encoding/json"
	"testing"

	"backend/pkg/llm"
)

func TestDecodeChatHistory(t *testing.T) {
	entry := func(role, content string) string {
		b, _ := json.Marshal(map[string]string{"role": role, "content": content})
		return string(b)
	}

	t.Run("parses well-formed entries in order", func(t *testing.T) {
		in := []string{
			entry("user", "first"),
			entry("assistant", "reply one"),
			entry("user", "second"),
		}
		got := decodeChatHistory(in)
		want := []llm.ChatMessage{
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "reply one"},
			{Role: "user", Content: "second"},
		}
		if len(got) != len(want) {
			t.Fatalf("len = %d, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("skips malformed and blank entries", func(t *testing.T) {
		in := []string{
			"not-json",
			entry("", "no-role"),
			entry("user", ""),
			entry("user", "kept"),
			"",
		}
		got := decodeChatHistory(in)
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1 (only valid entry kept)", len(got))
		}
		if got[0] != (llm.ChatMessage{Role: "user", Content: "kept"}) {
			t.Errorf("got %+v, want the single valid entry", got[0])
		}
	})

	t.Run("empty input yields empty (non-nil) slice", func(t *testing.T) {
		if got := decodeChatHistory(nil); got == nil || len(got) != 0 {
			t.Errorf("got %#v, want empty non-nil slice", got)
		}
	})
}
