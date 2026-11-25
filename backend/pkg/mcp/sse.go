package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// P1-AGT-004 (D8.2 amendment) — the SSE binding of the same Transport interface.
//
// `Transport` was defined so a second binding could land "without touching
// policy, grant, or audit code" (registry.go). `SSETransport` is that binding
// for MCP's HTTP+SSE transport: one GET opens an event stream, the server names
// a POST endpoint, and JSON-RPC responses arrive as `message` events.
//
// What this adapter is allowed to decide, and what it is not:
//
//   - It never authorizes. The Gate runs every trust check before InvokeTool is
//     ever called, and a denied call never dials (proved by a counting stub).
//   - It never resolves credentials. `Headers` is the caller's input: a caller
//     resolves `vault:` references and hands the adapter a ready header set.
//     Nothing here reads a secret from ambient state, and no header value is
//     ever logged or echoed in an error.
//   - It never follows a redirect: following one would send a credential-bearing
//     request to a host the registration never named.
//   - It refuses plain `http` except on a loopback host, so a credential cannot
//     cross the wire in clear text by accident.
//
// Deliberate limits of this binding (documented, not silent): one stream per
// exchange (no persistent session or reconnect rules — matching the stdio
// adapter's one-process-per-call proof), no streamable-HTTP replies (the answer
// must arrive on the event stream), and no request batching or pagination.

// DefaultSSETimeout bounds one discovery or invocation exchange.
const DefaultSSETimeout = 20 * time.Second

const (
	// maxSSEStreamBytes caps one exchange's event stream: a reply is small, and
	// an unbounded stream is a way to pin a worker.
	maxSSEStreamBytes = 4 << 20
	// maxSSEReplyBytes caps the body of a POST acknowledgement.
	maxSSEReplyBytes = 1 << 20
)

// SSETransport speaks MCP over HTTP+SSE.
type SSETransport struct {
	// Endpoint is the SSE URL (`https://host/sse`). When empty, the
	// registration's Endpoint is used.
	Endpoint string
	// Headers are sent on both the stream and the POSTs. A caller resolves
	// `vault:` references before constructing the transport; this adapter never
	// sees a reference and never logs a value.
	Headers map[string]string
	// Timeout bounds one exchange; zero means DefaultSSETimeout.
	Timeout time.Duration
	// ClientName/Version identify this platform during initialize.
	ClientName    string
	ClientVersion string
	// HTTPClient overrides the transport's client (tests/proxies). nil uses a
	// safe default that refuses redirects.
	HTTPClient *http.Client
}

// DiscoverTools performs initialize + tools/list and returns the advertised
// metadata. Nothing here authorizes execution: the Gate still requires a grant.
func (t *SSETransport) DiscoverTools(server ServerRegistration) ([]DiscoveredTool, error) {
	raw, err := t.exchange(server, rpcRequest{JSONRPC: "2.0", ID: 2, Method: "tools/list"})
	if err != nil {
		return nil, err
	}
	return parseToolList("mcp sse", raw)
}

// InvokeTool runs one tool through tools/call and returns the server's text
// content. The Gate calls this only after every trust check passed.
func (t *SSETransport) InvokeTool(server ServerRegistration, toolName string, argsJSON string) (string, error) {
	var args any
	if strings.TrimSpace(argsJSON) == "" {
		args = map[string]any{}
	} else if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("mcp sse: arguments are not valid JSON: %w", err)
	}

	raw, err := t.exchange(server, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "tools/call",
		Params: map[string]any{"name": toolName, "arguments": args},
	})
	if err != nil {
		return "", err
	}
	return parseToolCallResult("mcp sse", toolName, raw)
}

// exchange opens one stream, completes the handshake, posts the target request
// and returns its raw result. One deadline covers the whole exchange.
func (t *SSETransport) exchange(server ServerRegistration, call rpcRequest) (json.RawMessage, error) {
	endpoint, err := t.endpointFor(server)
	if err != nil {
		return nil, err
	}
	timeout := t.timeout()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	sess, err := t.dial(ctx, server, endpoint)
	if err != nil {
		return nil, err
	}
	defer sess.close()

	name := t.ClientName
	if name == "" {
		name = "vibesdk"
	}
	version := t.ClientVersion
	if version == "" {
		version = "1.0.0"
	}
	if err := sess.post(rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": name, "version": version},
		},
	}); err != nil {
		return nil, err
	}
	if _, err := sess.await(1); err != nil {
		return nil, err
	}
	// A notification carries no id and expects no response.
	if err := sess.post(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"}); err != nil {
		return nil, err
	}
	if err := sess.post(call); err != nil {
		return nil, err
	}
	return sess.await(call.ID)
}

func (t *SSETransport) timeout() time.Duration {
	if t.Timeout <= 0 {
		return DefaultSSETimeout
	}
	return t.Timeout
}

// endpointFor resolves and validates the stream URL. A server without one is an
// explicit error, never a silent default.
func (t *SSETransport) endpointFor(server ServerRegistration) (*url.URL, error) {
	raw := strings.TrimSpace(t.Endpoint)
	if raw == "" {
		raw = strings.TrimSpace(server.Endpoint)
	}
	if raw == "" {
		return nil, fmt.Errorf("mcp sse: server %q has no endpoint", server.Name)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("mcp sse: server %q endpoint is not a URL: %w", server.Name, err)
	}
	if err := safeMCPURL(server.Name, parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

// safeMCPURL is the wire rule for both the stream and the POST endpoint: https
// everywhere, http only on a loopback host. A credential-bearing request must
// never cross the network in clear text because a registration said `http://`.
func safeMCPURL(server string, parsed *url.URL) error {
	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(parsed.Hostname()) {
			return nil
		}
		return fmt.Errorf(
			"mcp sse: server %q endpoint %q is plain http on a non-loopback host; use https (credentials would cross the wire in clear text)",
			server, parsed.Host,
		)
	default:
		return fmt.Errorf(
			"mcp sse: server %q endpoint scheme %q is not supported (https, or http on a loopback host)",
			server, parsed.Scheme,
		)
	}
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// sseSession is one open stream plus the endpoint the server named for POSTs.
type sseSession struct {
	server  string
	client  *http.Client
	stream  *http.Response
	reader  *bufio.Reader
	postURL *url.URL
	headers map[string]string
	ctx     context.Context
}

func (t *SSETransport) dial(ctx context.Context, server ServerRegistration, endpoint *url.URL) (*sseSession, error) {
	client := t.HTTPClient
	if client == nil {
		client = &http.Client{
			// A redirect would move a credential-bearing request to a host the
			// registration never named; refuse instead of following.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("mcp sse: build stream request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-store")
	for key, value := range t.Headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("mcp sse: server %q timed out after %s", server.Name, t.timeout())
		}
		return nil, fmt.Errorf("mcp sse: open stream to %q: %w", server.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("mcp sse: server %q answered %d for the event stream", server.Name, resp.StatusCode)
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("mcp sse: server %q answered %q, not text/event-stream", server.Name, contentType)
	}

	sess := &sseSession{
		server:  server.Name,
		client:  client,
		stream:  resp,
		reader:  bufio.NewReaderSize(io.LimitReader(resp.Body, maxSSEStreamBytes), 64*1024),
		headers: t.Headers,
		ctx:     ctx,
	}
	// Closing the body when the deadline fires unblocks a pending read.
	go func() {
		<-ctx.Done()
		_ = resp.Body.Close()
	}()

	post, err := sess.readEndpoint(endpoint)
	if err != nil {
		sess.close()
		return nil, err
	}
	sess.postURL = post
	return sess, nil
}

// readEndpoint waits for the `endpoint` event and resolves it against the stream
// URL (servers usually send a relative path).
func (s *sseSession) readEndpoint(base *url.URL) (*url.URL, error) {
	for {
		event, data, err := s.readEvent()
		if err != nil {
			return nil, err
		}
		if event != "endpoint" {
			continue
		}
		raw := strings.TrimSpace(string(data))
		if raw == "" {
			return nil, fmt.Errorf("mcp sse: server %q sent an empty endpoint event", s.server)
		}
		ref, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("mcp sse: server %q sent an unusable endpoint event: %w", s.server, err)
		}
		resolved := base.ResolveReference(ref)
		if err := safeMCPURL(s.server, resolved); err != nil {
			return nil, err
		}
		return resolved, nil
	}
}

// await reads events until the response with wantID arrives. Anything that is
// not JSON-RPC on the stream is a protocol error, not a warning.
func (s *sseSession) await(wantID int64) (json.RawMessage, error) {
	for {
		event, data, err := s.readEvent()
		if err != nil {
			return nil, err
		}
		if event != "" && event != "message" {
			continue // a server-specific event; not a JSON-RPC reply
		}
		if len(data) == 0 {
			continue
		}
		var probe struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			return nil, fmt.Errorf(
				"mcp sse: server %q wrote non-JSON on the event stream (protocol channel only): %s",
				s.server, truncate(string(data), 120),
			)
		}
		// Notifications and other ids are not ours; keep reading.
		if len(probe.ID) == 0 || string(probe.ID) == "null" {
			continue
		}
		if !rpcRequestIDEquals(probe.ID, wantID) {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, fmt.Errorf("mcp sse: malformed response: %w", err)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp sse: server %q returned error %d: %s", s.server, resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// post sends one JSON-RPC message to the endpoint the server named. The reply is
// expected on the event stream; a non-2xx answer is an explicit failure.
func (s *sseSession) post(req rpcRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("mcp sse: encode %s: %w", req.Method, err)
	}
	httpReq, err := http.NewRequestWithContext(s.ctx, http.MethodPost, s.postURL.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mcp sse: build %s request: %w", req.Method, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for key, value := range s.headers {
		httpReq.Header.Set(key, value)
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		if s.ctx.Err() != nil {
			return fmt.Errorf("mcp sse: server %q timed out while posting %s", s.server, req.Method)
		}
		return fmt.Errorf("mcp sse: post %s to %q: %w", req.Method, s.server, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxSSEReplyBytes))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("mcp sse: server %q answered %d for %s", s.server, resp.StatusCode, req.Method)
	}
	return nil
}

// readEvent reads one SSE event: `event:`/`data:` fields terminated by a blank
// line, with comments and keep-alive blank lines ignored.
func (s *sseSession) readEvent() (string, []byte, error) {
	var event string
	var data bytes.Buffer
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			if s.ctx.Err() != nil {
				return "", nil, fmt.Errorf("mcp sse: server %q timed out before responding", s.server)
			}
			if err == io.EOF {
				return "", nil, fmt.Errorf("mcp sse: server %q closed the event stream before responding", s.server)
			}
			return "", nil, fmt.Errorf("mcp sse: read event stream from %q: %w", s.server, err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if event == "" && data.Len() == 0 {
				continue // keep-alive
			}
			if event == "" {
				event = "message"
			}
			return event, data.Bytes(), nil
		}
		if strings.HasPrefix(line, ":") {
			continue // comment / keep-alive
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			if data.Len()+len(value) > maxSSEStreamBytes {
				return "", nil, fmt.Errorf("mcp sse: server %q sent an event larger than %d bytes", s.server, maxSSEStreamBytes)
			}
			data.WriteString(value)
		}
	}
}

func (s *sseSession) close() {
	_ = s.stream.Body.Close()
}
