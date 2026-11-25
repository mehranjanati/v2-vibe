package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// P1-AGT-004 (D8.2) — the SSE binding against a stub server.
//
// The stub is a real HTTP server (httptest) speaking the MCP HTTP+SSE binding:
// one GET opens the stream and names the POST endpoint, JSON-RPC replies arrive
// as `message` events. These are the same five properties the stdio adapter is
// held to, so the bindings cannot drift apart.

type sseStub struct {
	t    *testing.T
	url  string
	mode string

	mu      sync.Mutex
	streams int
	posts   int
	headers []string
	// ch is the reply channel of the *current* stream. Each exchange opens a new
	// stream, so a stale handler must never consume the next exchange's reply.
	ch chan string
}

// newSSEStub starts the stub. mode: "" (normal), "non-json", "hang" (no reply
// after initialize), "rpc-error", "is-error", "post-500".
func newSSEStub(t *testing.T, mode string) *sseStub {
	t.Helper()
	stub := &sseStub{t: t, mode: mode, ch: make(chan string, 8)}
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", stub.handleStream)
	mux.HandleFunc("/messages", stub.handlePost)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	stub.url = srv.URL
	return stub
}

func (s *sseStub) counts() (streams, posts int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streams, s.posts
}

func (s *sseStub) seenHeaders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.headers...)
}

func (s *sseStub) push(event string) {
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()
	if ch == nil {
		return
	}
	ch <- event
}

func (s *sseStub) handleStream(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.streams++
	s.headers = append(s.headers, r.Header.Get("Authorization"))
	s.ch = make(chan string, 8)
	ch := s.ch
	s.mu.Unlock()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flusher", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "event: endpoint\ndata: %s/messages\n\n", s.url)
	flusher.Flush()

	for {
		select {
		case event := <-ch:
			fmt.Fprint(w, event)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *sseStub) handlePost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     int64          `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.posts++
	s.headers = append(s.headers, r.Header.Get("Authorization"))
	s.mu.Unlock()

	if s.mode == "post-500" {
		http.Error(w, "stub refuses", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)

	if req.Method == "notifications/initialized" {
		return
	}
	if s.mode == "hang" && req.Method != "initialize" {
		return
	}
	if s.mode == "non-json" {
		s.push("event: message\ndata: not-json\n\n")
		return
	}
	if s.mode == "rpc-error" {
		s.push(fmt.Sprintf("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":-32000,\"message\":\"stub refused\"}}\n\n", req.ID))
		return
	}

	var result string
	switch req.Method {
	case "initialize":
		result = `{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"stub","version":"1"}}`
	case "tools/list":
		result = `{"tools":[{"name":"lookup","description":"look up a lead"},{"name":"explode","description":"always fails"}]}`
	case "tools/call":
		name, _ := req.Params["name"].(string)
		args, _ := req.Params["arguments"].(map[string]any)
		if s.mode == "is-error" || name == "explode" {
			result = `{"content":[{"type":"text","text":"boom"}],"isError":true}`
			break
		}
		lead, _ := args["lead"].(map[string]any)
		id, _ := lead["id"].(string)
		result = fmt.Sprintf(`{"content":[{"type":"text","text":"lead %s"}],"isError":false}`, id)
	default:
		result = `{}`
	}
	s.push(fmt.Sprintf("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":%s}\n\n", req.ID, result))
}

// sseHarness points the standard gate harness at the stub. The harness registers
// "crm" with a stdio endpoint on purpose: the transport's own Endpoint wins,
// exactly like StdioTransport's Command does.
func sseHarness(t *testing.T, mode string) (*harness, *sseStub, *SSETransport) {
	t.Helper()
	h := newHarness(t)
	stub := newSSEStub(t, mode)
	tr := &SSETransport{Endpoint: stub.url + "/sse", Timeout: 5 * time.Second, Headers: map[string]string{"Authorization": "Bearer vault-resolved"}}
	return h, stub, tr
}

var _ = strings.TrimSpace

// TestSSETransportLiveRoundTrip is the acceptance criterion for this binding: a
// granted call against a real HTTP+SSE server round-trips through the gate, and
// the caller-supplied headers (vault-resolved, never a literal in the
// registration) reach both the stream and the POSTs.
func TestSSETransportLiveRoundTrip(t *testing.T) {
	h, stub, tr := sseHarness(t, "")

	discovery, err := h.gate.Discover(h.id, "crm", tr)
	if err != nil {
		t.Fatalf("Discover over SSE: %v", err)
	}
	if len(discovery.Tools) != 2 {
		t.Fatalf("expected two advertised tools, got %d: %+v", len(discovery.Tools), discovery.Tools)
	}
	names := map[string]bool{}
	for _, tool := range discovery.Tools {
		names[tool.Name] = true
	}
	if !names["lookup"] || !names["explode"] {
		t.Fatalf("discovery did not report the stub tools: %+v", discovery.Tools)
	}
	if discovery.Note == "" {
		t.Fatal("discovery must state that it grants nothing")
	}

	grant := h.mustGrant(t, false)
	result, err := h.gate.Call(CallRequest{
		Identity: h.id,
		Ref:      h.ref,
		GrantID:  grant.ID,
		ArgsJSON: `{"lead":{"id":"lead-42"}}`,
	}, tr)
	if err != nil {
		t.Fatalf("granted Call over SSE: %v", err)
	}
	if !strings.Contains(result.Output, "lead-42") {
		t.Fatalf("unexpected tool output: %q", result.Output)
	}
	if result.GrantID != grant.ID {
		t.Fatalf("result should echo the authorizing grant: %q", result.GrantID)
	}

	headers := stub.seenHeaders()
	if len(headers) == 0 {
		t.Fatal("the stub saw no requests")
	}
	for _, header := range headers {
		if header != "Bearer vault-resolved" {
			t.Fatalf("every request must carry the caller-supplied credential, got %q", header)
		}
	}
	if streams, posts := stub.counts(); streams == 0 || posts == 0 {
		t.Fatalf("expected real traffic, got streams=%d posts=%d", streams, posts)
	}
}

// TestSSETransportGrantlessCallNeverDials pins the acceptance criterion with the
// real transport: denial happens before any connection, so the server sees no
// request at all.
func TestSSETransportGrantlessCallNeverDials(t *testing.T) {
	h, stub, tr := sseHarness(t, "")

	_, err := h.gate.Call(CallRequest{
		Identity: h.id,
		Ref:      h.ref,
		ArgsJSON: `{"lead":{"id":"lead-42"}}`,
	}, tr)
	mustDenied(t, err, "no grant")

	if streams, posts := stub.counts(); streams != 0 || posts != 0 {
		t.Fatalf("a denied call must not dial, got streams=%d posts=%d", streams, posts)
	}
}

// TestSSETransportRejectsNonJSONStream pins the channel rule: the event stream is
// the protocol channel, so free text is a protocol error — not a tool result.
func TestSSETransportRejectsNonJSONStream(t *testing.T) {
	h, _, tr := sseHarness(t, "non-json")

	_, err := h.gate.Discover(h.id, "crm", tr)
	if err == nil || !strings.Contains(err.Error(), "non-JSON") {
		t.Fatalf("want a non-JSON protocol error, got %v", err)
	}
}

// TestSSETransportTimesOut pins the deadline: a server that answers the
// handshake and then goes silent fails the exchange instead of pinning it.
func TestSSETransportTimesOut(t *testing.T) {
	h, _, tr := sseHarness(t, "hang")
	tr.Timeout = 150 * time.Millisecond

	start := time.Now()
	_, err := h.gate.Discover(h.id, "crm", tr)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a timeout error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("the deadline did not bound the exchange (%s)", elapsed)
	}
}

// TestSSETransportPropagatesToolError pins both error shapes: a JSON-RPC error
// and an `isError: true` result. Neither may surface as an empty success.
func TestSSETransportPropagatesToolError(t *testing.T) {
	t.Run("json-rpc error", func(t *testing.T) {
		h, _, tr := sseHarness(t, "rpc-error")
		grant := h.mustGrant(t, false)

		_, err := h.gate.Call(CallRequest{
			Identity: h.id, Ref: h.ref, GrantID: grant.ID,
			ArgsJSON: `{"lead":{"id":"lead-42"}}`,
		}, tr)
		if err == nil || !strings.Contains(err.Error(), "stub refused") {
			t.Fatalf("want the server's JSON-RPC error, got %v", err)
		}
	})

	t.Run("isError result", func(t *testing.T) {
		h, _, tr := sseHarness(t, "is-error")
		grant := h.mustGrant(t, false)

		_, err := h.gate.Call(CallRequest{
			Identity: h.id, Ref: h.ref, GrantID: grant.ID,
			ArgsJSON: `{"lead":{"id":"lead-42"}}`,
		}, tr)
		if err == nil || !strings.Contains(err.Error(), "reported an error") {
			t.Fatalf("want an isError failure, got %v", err)
		}
	})
}

// TestSSETransportPostRefusalFailsClosed: a non-2xx answer to a POST is an
// error, never a silent continue.
func TestSSETransportPostRefusalFailsClosed(t *testing.T) {
	h, _, tr := sseHarness(t, "post-500")

	_, err := h.gate.Discover(h.id, "crm", tr)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("want the POST refusal, got %v", err)
	}
}

// TestSSETransportRefusesUnsafeEndpoints pins the wire rule: https everywhere,
// http only on a loopback host, and no other scheme. These fail before dialing.
func TestSSETransportRefusesUnsafeEndpoints(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name     string
		endpoint string
		want     string
	}{
		{"plain http off loopback", "http://mcp.example.com/sse", "plain http"},
		{"stdio endpoint is not an sse url", "stdio://crm", "not supported"},
		{"empty endpoint falls back to the registration, never to a default", "", "not supported"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &SSETransport{Endpoint: tc.endpoint, Timeout: time.Second}
			_, err := h.gate.Discover(h.id, "crm", tr)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}
