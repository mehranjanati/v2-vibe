package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// P1-AGT-003 — the first real MCP transport.
//
// Transport was defined (registry.go) so a live adapter could land behind the
// trust boundary "without touching policy, grant, or audit code". This is that
// adapter for MCP's stdio binding: newline-delimited JSON-RPC 2.0 over a child
// process's stdin/stdout, which is how a local MCP server is normally shipped.
//
// Deliberate limits of this proof of concept (all documented, none silent):
//
//   - one process per call. Discovery and invocation each spawn the server, do
//     the handshake, and exit. A persistent session (and its reconnect rules) is
//     a follow-up; per-call spawns keep the trust story simple.
//   - stdout is the protocol channel only. Anything a server writes to stdout
//     that is not JSON-RPC is a protocol error, not a warning — a sloppy server
//     must not be able to smuggle text into a tool result. Server logs belong on
//     stderr, which this adapter captures for error messages only.
//   - the deadline covers the whole exchange; on expiry the process is killed.
//   - no environment is inherited except what the caller passes, so a tool cannot
//     read control-plane secrets from the ambient environment.

// DefaultStdioTimeout bounds one discovery or invocation exchange.
const DefaultStdioTimeout = 20 * time.Second

// StdioTransport runs an MCP server as a child process.
type StdioTransport struct {
	// Command is argv (binary first). Endpoint from the registration is used
	// when this is empty, split on spaces.
	Command []string
	// Env is the child's complete environment ("K=V").
	Env []string
	// Timeout bounds one exchange; zero means DefaultStdioTimeout.
	Timeout time.Duration
	// ClientName/Version identify this platform during initialize.
	ClientName    string
	ClientVersion string
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// DiscoverTools performs initialize + tools/list and returns the advertised
// metadata. Nothing here authorizes execution: the Gate still requires a grant.
func (t *StdioTransport) DiscoverTools(server ServerRegistration) ([]DiscoveredTool, error) {
	raw, err := t.exchange(server, func(enc *json.Encoder) error {
		if err := t.initialize(enc); err != nil {
			return err
		}
		return enc.Encode(rpcRequest{JSONRPC: "2.0", ID: 2, Method: "tools/list"})
	}, 2)
	if err != nil {
		return nil, err
	}

	var listing struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &listing); err != nil {
		return nil, fmt.Errorf("mcp stdio: tools/list result is not a tool list: %w", err)
	}
	out := make([]DiscoveredTool, 0, len(listing.Tools))
	for _, tool := range listing.Tools {
		// Side effect and approvals are *declared* here and are deliberately not
		// trusted: execution uses the reviewed Tool Manifest values.
		out = append(out, DiscoveredTool{Name: tool.Name, Description: tool.Description})
	}
	return out, nil
}

// InvokeTool runs one tool through tools/call and returns the server's text
// content. The Gate calls this only after every trust check passed.
func (t *StdioTransport) InvokeTool(server ServerRegistration, toolName string, argsJSON string) (string, error) {
	var args any
	if strings.TrimSpace(argsJSON) == "" {
		args = map[string]any{}
	} else if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("mcp stdio: arguments are not valid JSON: %w", err)
	}

	raw, err := t.exchange(server, func(enc *json.Encoder) error {
		if err := t.initialize(enc); err != nil {
			return err
		}
		return enc.Encode(rpcRequest{
			JSONRPC: "2.0", ID: 2, Method: "tools/call",
			Params: map[string]any{"name": toolName, "arguments": args},
		})
	}, 2)
	if err != nil {
		return "", err
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("mcp stdio: tools/call result is malformed: %w", err)
	}
	var text strings.Builder
	for _, part := range result.Content {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
	}
	if result.IsError {
		return "", fmt.Errorf("mcp stdio: tool %q reported an error: %s", toolName, text.String())
	}
	return text.String(), nil
}

// initialize sends the MCP handshake (initialize + the required initialized
// notification) so tools/list and tools/call are legal.
func (t *StdioTransport) initialize(enc *json.Encoder) error {
	name := t.ClientName
	if name == "" {
		name = "vibesdk"
	}
	version := t.ClientVersion
	if version == "" {
		version = "1.0.0"
	}
	if err := enc.Encode(rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": name, "version": version},
		},
	}); err != nil {
		return err
	}
	// A notification carries no id and expects no response.
	return enc.Encode(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"})
}

// exchange spawns the server, writes the request sequence, and returns the raw
// result of the response with the given id. Every failure path is explicit: a
// hang, a crash, non-JSON stdout, or a JSON-RPC error all surface as an error so
// the Gate can deny the call.
func (t *StdioTransport) exchange(server ServerRegistration, write func(*json.Encoder) error, wantID int64) (json.RawMessage, error) {
	argv := t.argvFor(server)
	if len(argv) == 0 {
		return nil, fmt.Errorf("mcp stdio: server %q has no command", server.Name)
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = DefaultStdioTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = t.Env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: stdout pipe: %w", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp stdio: start %s: %w", argv[0], err)
	}
	// kill is idempotent enough for the deferred cleanup: a server that already
	// exited returns an error we intentionally ignore.
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	go func() {
		enc := json.NewEncoder(stdin)
		if err := write(enc); err != nil {
			_ = stdin.Close()
			return
		}
		_ = stdin.Close()
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var probe struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			return nil, fmt.Errorf("mcp stdio: server %q wrote non-JSON on stdout (protocol channel only): %s", server.Name, truncate(line, 120))
		}
		// Notifications and other ids are not ours; keep reading.
		if len(probe.ID) == 0 || string(probe.ID) == "null" {
			continue
		}
		var id int64
		if err := json.Unmarshal(probe.ID, &id); err != nil || id != wantID {
			continue
		}

		var resp rpcResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			return nil, fmt.Errorf("mcp stdio: malformed response: %w", err)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp stdio: server %q returned error %d: %s", server.Name, resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
	if err := scanner.Err(); err != nil && ctx.Err() != nil {
		return nil, fmt.Errorf("mcp stdio: server %q timed out after %s", server.Name, timeout)
	}
	return nil, fmt.Errorf("mcp stdio: server %q closed stdout before responding%s", server.Name, stderrSuffix(stderr.String()))
}

// argvFor resolves the command: the transport's own Command wins, otherwise the
// registration endpoint ("stdio://" scheme stripped) is split on spaces.
func (t *StdioTransport) argvFor(server ServerRegistration) []string {
	if len(t.Command) > 0 {
		return t.Command
	}
	endpoint := strings.TrimSpace(server.Endpoint)
	endpoint = strings.TrimPrefix(endpoint, "stdio://")
	if endpoint == "" {
		return nil
	}
	return strings.Fields(endpoint)
}

func stderrSuffix(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// stderr is diagnostics, never protocol: keep the last line, bounded.
	lines := strings.Split(raw, "\n")
	return ": " + truncate(lines[len(lines)-1], 200)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// stdioExchangeSeq keeps request ids unique per process run without state.
var stdioExchangeSeq int64

// NextStdioRequestID returns a monotonic id for callers that pipeline requests.
func NextStdioRequestID() int64 { return atomic.AddInt64(&stdioExchangeSeq, 1) }
