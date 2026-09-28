package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"backend/pkg/llm"
	"backend/pkg/models"
)

// newTestRoom wires a ProjectRoom against a fake LLM HTTP server (no
// Redis, no engine) with a running Run loop so broadcasts drain.
func newTestRoom(t *testing.T, srvURL string) *ProjectRoom {
	t.Helper()
	cfg := llm.Config{GatewayURL: srvURL, APIKey: "test", Timeout: 15 * time.Second}
	r := NewProjectRoom("llmtest-"+time.Now().Format("150405.000"), nil, llm.NewClient(cfg), nil)
	go r.Run()
	t.Cleanup(r.Stop)
	return r
}

// sseServer spins an httptest SSE endpoint. handlers is consumed per
// request (the last handler repeats for extra requests).
func sseServer(t *testing.T, handlers ...http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		n := int(hits.Add(1))
		var h http.HandlerFunc
		if n <= len(handlers) {
			h = handlers[n-1]
		} else {
			h = handlers[len(handlers)-1]
		}
		w.Header().Set("Content-Type", "text/event-stream")
		h(w, req)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// frame builds a properly JSON-escaped SSE data frame (newlines in the
// LLM content must be escaped, not raw).
func frame(content string) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"delta": map[string]any{"content": content},
		}},
	})
	return "data: " + string(b) + "\n\n"
}

// abortNow flushes whatever was written then hard-kills the connection
// (unexpected EOF on the client side).
func abortNow(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	panic(http.ErrAbortHandler)
}

const doneFrame = "data: [DONE]\n\n"

func keysOf(m map[string]*models.FileEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestStreamLLMRRetriesWhenConnectionLostBeforeOutput: an attempt that
// dies before producing any content is retried automatically (up to 3).
func TestStreamLLMRRetriesWhenConnectionLostBeforeOutput(t *testing.T) {
	srv, hits := sseServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			// Attempt 1: an empty delta frame, then a hard abort
			// (unexpected EOF on the client side).
			_, _ = w.Write([]byte(frame("")))
			abortNow(w)
		},
		func(w http.ResponseWriter, _ *http.Request) {
			// Attempt 2: full successful stream.
			_, _ = w.Write([]byte(frame("full ") + frame("output") + doneFrame))
		},
	)
	r := newTestRoom(t, srv.URL)

	full, finish, err := r.streamLLMRaw(context.Background(), "sys",
		[]llm.ChatMessage{{Role: "user", Content: "hi"}}, 1024, func(string) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if full != "full output" {
		t.Fatalf("content = %q", full)
	}
	if finish != "" {
		t.Fatalf("finish = %q", finish)
	}
	if hits.Load() != 2 {
		t.Fatalf("expected 2 requests, got %d", hits.Load())
	}
}

// TestStreamLLMRawKeepsPartialOnMidContentDrop: a drop AFTER content has
// streamed must keep the partial output (finish=connection_lost) instead
// of failing — no retry (it would duplicate).
func TestStreamLLMRawKeepsPartialOnMidContentDrop(t *testing.T) {
	srv, hits := sseServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(frame("Hello")))
		abortNow(w)
	})
	r := newTestRoom(t, srv.URL)

	full, finish, err := r.streamLLMRaw(context.Background(), "sys",
		[]llm.ChatMessage{{Role: "user", Content: "hi"}}, 1024, func(string) {})
	if err != nil {
		t.Fatalf("mid-content drop must not error: %v", err)
	}
	if full != "Hello" {
		t.Fatalf("content = %q, want Hello", full)
	}
	if finish != "connection_lost" {
		t.Fatalf("finish = %q, want connection_lost", finish)
	}
	if hits.Load() != 1 {
		t.Fatalf("mid-content drop must not retry; hits=%d", hits.Load())
	}
}

// TestStreamAndApplyFilesWritesVFS: a full fence stream through
// streamAndApplyFiles lands in the VFS via file events.
func TestStreamAndApplyFilesWritesVFS(t *testing.T) {
	doc := "intro text\n```public/index.html\n<html><script src=\"js/main.js\"></script></html>\n```\n" +
		"```public/js/main.js\nApp.init();\n```\n"
	srv, _ := sseServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(frame("intro ") + frame(doc) + doneFrame))
	})
	r := newTestRoom(t, srv.URL)
	r.filesWritten.Store(0)

	_, truncated, err := r.streamAndApplyFiles(context.Background(), "sys",
		[]llm.ChatMessage{{Role: "user", Content: "build"}}, 1024)
	if err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if len(truncated) != 0 {
		t.Fatalf("unexpected truncated: %v", truncated)
	}
	vfs := r.GetVFS()
	if _, ok := vfs["public/index.html"]; !ok {
		t.Fatalf("index.html missing from VFS: %v", keysOf(vfs))
	}
	if _, ok := vfs["public/js/main.js"]; !ok {
		t.Fatalf("main.js missing from VFS: %v", keysOf(vfs))
	}
	if r.filesWritten.Load() != 2 {
		t.Fatalf("filesWritten = %d, want 2", r.filesWritten.Load())
	}
	if missing := missingReferencedAssets(vfs); len(missing) != 0 {
		t.Fatalf("unexpected missing refs: %v", missing)
	}
}

// TestFillMissingReferencedFilesStreamsGaps: pre-existing index.html that
// references two missing files; one gap-fill pass must generate them.
func TestFillMissingReferencedFilesStreamsGaps(t *testing.T) {
	var lastBody atomic.Value
	srv, _ := sseServer(t, func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		lastBody.Store(string(body))
		_, _ = w.Write([]byte(frame(
			"```public/js/main.js\nApp.init();\n```\n"+
				"```public/styles.css\nbody{color:red}\n```\n") + doneFrame))
	})
	r := newTestRoom(t, srv.URL)
	r.filesWritten.Store(0)
	r.UpsertFile("public/index.html",
		"<html><head><link rel=\"stylesheet\" href=\"styles.css\"></head>"+
			"<body><script src=\"js/main.js\"></script></body></html>")

	r.fillMissingReferencedFiles(context.Background())

	vfs := r.GetVFS()
	if _, ok := vfs["public/js/main.js"]; !ok {
		t.Fatalf("gap-fill did not create main.js: %v", keysOf(vfs))
	}
	if _, ok := vfs["public/styles.css"]; !ok {
		t.Fatalf("gap-fill did not create styles.css: %v", keysOf(vfs))
	}
	if missing := missingReferencedAssets(vfs); len(missing) != 0 {
		t.Fatalf("still missing after gap-fill: %v", missing)
	}
	if body, _ := lastBody.Load().(string); !strings.Contains(body, "public/js/main.js") {
		t.Fatalf("gap-fill prompt did not mention missing files: %q", body)
	}
}

// TestStreamLLMRawForwardsAPIErrors: non-connection errors stay fatal.
func TestStreamLLMRawForwardsAPIErrors(t *testing.T) {
	srv, _ := sseServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data: {\"error\":{\"message\":\"rate limited\"}}\n\n"))
	})
	r := newTestRoom(t, srv.URL)
	_, _, err := r.streamLLMRaw(context.Background(), "sys",
		[]llm.ChatMessage{{Role: "user", Content: "hi"}}, 1024, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("API error must surface, got %v", err)
	}
}
