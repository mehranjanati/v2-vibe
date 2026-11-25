package api

// P1-AGT-004 (D8.3) — the acceptance criterion taken literally: an
// authenticated route discovers and (with a grant) executes against a **real
// server**, not a fake transport. The server below is a genuine HTTP+SSE MCP
// implementation (endpoint event + POSTed JSON-RPC + message events), so this
// test exercises the route → Gate → SSETransport → socket path end to end.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"

	"backend/pkg/mcp"
)

// mcpSSEServer is a minimal real MCP server over HTTP+SSE.
type mcpSSEServer struct {
	url string
	mu  sync.Mutex
	ch  chan string
}

func (s *mcpSSEServer) push(event string) {
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()
	if ch != nil {
		ch <- event
	}
}

func newMCPSSEServer(t *testing.T) *mcpSSEServer {
	t.Helper()
	server := &mcpSSEServer{}
	mux := http.NewServeMux()

	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		server.mu.Lock()
		server.ch = make(chan string, 8)
		ch := server.ch
		server.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: endpoint\ndata: %s/messages\n\n", server.url)
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
	})

	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		if req.Method == "notifications/initialized" {
			return
		}
		var result string
		switch req.Method {
		case "initialize":
			result = `{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"live","version":"1"}}`
		case "tools/list":
			result = `{"tools":[{"name":"lookup","description":"live lookup"}]}`
		case "tools/call":
			args, _ := req.Params["arguments"].(map[string]any)
			lead, _ := args["lead"].(map[string]any)
			id, _ := lead["id"].(string)
			result = fmt.Sprintf(`{"content":[{"type":"text","text":"live lead %s"}],"isError":false}`, id)
		default:
			result = `{}`
		}
		server.push(fmt.Sprintf("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":%s}\n\n", req.ID, result))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	server.url = srv.URL
	return server
}

// TestMCPRouteExecutesAgainstARealServer is the literal acceptance criterion:
// discover → issue against the reviewed policy → granted call, each through the
// authenticated route and the real SSE binding.
func TestMCPRouteExecutesAgainstARealServer(t *testing.T) {
	live := newMCPSSEServer(t)

	f := newMCPFixture(t, p07Tokens())
	if err := f.surface.Registry.Register(mcp.ServerRegistration{
		Name: "live", Endpoint: live.url + "/sse", Tools: []string{"lookup"},
		RegisteredBy: "owner", RegisteredAt: mcpTestNow(),
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := f.surface.Policies.Put(mcp.ToolPolicy{
		Server: "live", Tool: "lookup", ToolManifestVersion: "1.0.0",
		Permissions: []string{"leads:read"}, SideEffect: mcp.SideEffectSafe,
		IdempotencyKey: "lead.id",
	}); err != nil {
		t.Fatalf("policy Put: %v", err)
	}
	// The production wire rule, not a fake: http(s) only, real SSE client.
	f.surface.Transport = mcp.SSEOnlyTransport

	status, body := workflowRequest(t, f.app, "GET", "/api/mcp/servers/live/tools", "token-a", nil)
	if status != fiber.StatusOK {
		t.Fatalf("discover against the real server: expected 200, got %d (%v)", status, body)
	}
	data, _ := body["data"].(map[string]any)
	tools, _ := data["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected the server's one tool, got %v", data["tools"])
	}
	if first, _ := tools[0].(map[string]any); first["name"] != "lookup" {
		t.Fatalf("unexpected tool metadata: %v", tools[0])
	}

	status, body = workflowRequest(t, f.app, "POST", "/api/mcp/grants", "token-a", map[string]any{
		"server": "live", "tool": "lookup", "version": "1.0.0",
	})
	if status != fiber.StatusOK {
		t.Fatalf("issue grant: expected 200, got %d (%v)", status, body)
	}
	grant, _ := body["data"].(map[string]any)
	grantID, _ := grant["id"].(string)

	status, body = workflowRequest(t, f.app, "POST", "/api/mcp/call", "token-a", map[string]any{
		"server": "live", "tool": "lookup", "version": "1.0.0", "grantId": grantID,
		"args": map[string]any{"lead": map[string]any{"id": "lead-42"}},
	})
	if status != fiber.StatusOK {
		t.Fatalf("granted call against the real server: expected 200, got %d (%v)", status, body)
	}
	result, _ := body["data"].(map[string]any)
	if output, _ := result["output"].(string); output != "live lead lead-42" {
		t.Fatalf("the server's real output must come back unchanged, got %q", output)
	}
	if echoed, _ := result["grantId"].(string); echoed != grantID {
		t.Fatalf("result must echo the authorizing grant, got %q", echoed)
	}
}
