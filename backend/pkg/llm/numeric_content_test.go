package llm

import (
	"context"
	"strings"
	"testing"
)

// TestEmitChunkNumericContent reproduces the Workers AI quirk where single
// numeric tokens stream as bare JSON numbers ("content":8). Before the fix
// the whole payload failed to unmarshal and every digit of generated code
// was silently dropped (charset="UTF-", <h class=..., price: null).
func TestEmitChunkNumericContent(t *testing.T) {
	c := NewClient(Config{})
	ch := make(chan StreamChunk, 16)
	fr := ""

	frames := []string{
		`{"choices":[{"delta":{"content":"UTF-"}}]}`,
		`{"choices":[{"delta":{"content":8}}]}`,
		`{"choices":[{"delta":{"content":" h"}}]}`,
		`{"choices":[{"delta":{"content":1}}]}`,
		`{"choices":[{"delta":{"content":" price: "}}]}`,
		`{"choices":[{"delta":{"content":450}}]}`,
		`{"choices":[{"delta":{"content":null}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}
	for _, f := range frames {
		if !c.emitChunk(context.Background(), f, ch, &fr) {
			t.Fatalf("emitChunk stopped early on %s", f)
		}
	}
	close(ch)

	var got strings.Builder
	for cc := range ch {
		if cc.Err != nil {
			t.Fatalf("unexpected error chunk: %v", cc.Err)
		}
		got.WriteString(cc.Content)
	}
	if want := "UTF-8 h1 price: 450"; got.String() != want {
		t.Fatalf("content = %q, want %q", got.String(), want)
	}
	if fr != "stop" {
		t.Fatalf("finish reason = %q, want stop", fr)
	}
}

// TestCoerceDeltaContent covers the raw-JSON-to-text coercion directly.
func TestCoerceDeltaContent(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`"UTF-"`, "UTF-"},
		{`8`, "8"},
		{`450`, "450"},
		{`4.5`, "4.5"},
		{`"hello"`, "hello"},
		{`"line\nbreak"`, "line\nbreak"},
		{`null`, ""},
		{``, ""},
		{`true`, "true"},
	}
	for _, tc := range cases {
		if got := coerceDeltaContent([]byte(tc.raw)); got != tc.want {
			t.Errorf("coerceDeltaContent(%s) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// TestReadSSENumericStreamEndToEnd feeds a full SSE body mixing string and
// numeric deltas through readSSE and asserts no digits are lost.
func TestReadSSENumericStreamEndToEnd(t *testing.T) {
	c := NewClient(Config{})
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"scale=\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":1}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\".\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":0}}]}\n\n" +
		"data: [DONE]\n\n"
	ch := make(chan StreamChunk, 8)
	go c.readSSE(context.Background(), &fakeReadCloser{data: body}, ch)
	var out strings.Builder
	for cc := range ch {
		if cc.Err != nil {
			t.Fatalf("unexpected error chunk: %v", cc.Err)
		}
		out.WriteString(cc.Content)
	}
	if got, want := out.String(), "scale=1.0"; got != want {
		t.Fatalf("stream = %q, want %q", got, want)
	}
}
