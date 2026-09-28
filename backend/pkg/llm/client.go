package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the LLM / AI connection settings, sourced from environment
// variables. It supports two providers:
//
//   - AI Gateway  : set AI_GATEWAY_URL to
//     https://gateway.ai.cloudflare.com/v1/<account>/<gateway>
//   - Workers AI  : leave AI_GATEWAY_URL empty and set CLOUDFLARE_ACCOUNT_ID.
//     The client then uses Workers AI's OpenAI-compatible endpoint
//     https://api.cloudflare.com/client/v4/accounts/<id>/ai/v1 (no gateway).
type Config struct {
	// GatewayURL is the base URL of the LLM provider. When empty and an
	// account ID is available, NewConfigFromEnv fills it with the Workers AI
	// OpenAI-compatible base URL. The client appends "/chat/completions".
	GatewayURL string
	// APIKey is the bearer token used for the request (gateway token for
	// AI Gateway, a Cloudflare API token for Workers AI).
	APIKey string
	// Model is the default model identifier when DEFAULT_MODEL is unset.
	// @cf/meta/llama-3.3-70b-instruct-fp8-fast is a widely-available general
	// chat model on Workers AI; the older llama-3.1-8b is deprecated (410).
	Model string
	// Timeout bounds the entire streaming request.
	Timeout time.Duration
	// HTTPClient is optional; defaults to http.DefaultClient.
	HTTPClient *http.Client
}

// NewConfigFromEnv builds a Config from environment variables.
//
// If AI_GATEWAY_URL is set it is used verbatim (AI Gateway mode). Otherwise,
// when CLOUDFLARE_ACCOUNT_ID is present, it falls back to Workers AI's
// OpenAI-compatible endpoint so the backend works with no AI Gateway at all.
func NewConfigFromEnv() Config {
	gateway := getEnv("AI_GATEWAY_URL", "")
	accountID := getEnv("CLOUDFLARE_ACCOUNT_ID", "")
	if gateway == "" && accountID != "" {
		gateway = "https://api.cloudflare.com/client/v4/accounts/" + accountID + "/ai/v1"
	}

	apiKey := getEnv("AI_GATEWAY_API_KEY", "")
	if apiKey == "" {
		// Workers AI authenticates with a Cloudflare API token.
		apiKey = getEnv("CLOUDFLARE_API_TOKEN", "")
	}

	return Config{
		GatewayURL: gateway,
		APIKey:     apiKey,
		Model:      getEnv("DEFAULT_MODEL", "@cf/meta/llama-3.3-70b-instruct-fp8-fast"),
		Timeout:    5 * time.Minute,
	}
}

func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(envLookup(key)); v != "" {
		return v
	}
	return fallback
}

// envLookup is a thin indirection so tests can stub it.
var envLookup = os.Getenv

// ChatMessage is a single message in the chat completion request.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the OpenAI-compatible chat completion request body.
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

// StreamChunk is a single SSE delta from the gateway.
type StreamChunk struct {
	// Content is the incremental text delta ("" for non-content chunks).
	Content string
	// Done is true when the stream has finished (data: [DONE]).
	Done bool
	// FinishReason is the terminal finish_reason of the last choice
	// ("stop", "length", "tool_calls", ...). Set on the final Done chunk.
	// "length" means the output was truncated by the token limit.
	FinishReason string
	// Err is set when the stream fails mid-flight.
	Err error
}

// Client streams chat completions from an OpenAI-compatible endpoint.
type Client struct {
	cfg Config
	hc  *http.Client
}

// NewClient creates an LLM streaming client from cfg.
func NewClient(cfg Config) *Client {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: cfg.Timeout}
	}
	return &Client{cfg: cfg, hc: hc}
}

// StreamChat sends a streaming chat completion request. It returns a
// channel of StreamChunk that is closed when the stream ends. The caller
// must consume the channel; cancellation is via ctx.
func (c *Client) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	if c.cfg.GatewayURL == "" {
		return nil, errors.New("llm: no provider configured (set AI_GATEWAY_URL or CLOUDFLARE_ACCOUNT_ID)")
	}
	if req.Model == "" {
		req.Model = c.cfg.Model
	}
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}

	url := strings.TrimRight(c.cfg.GatewayURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: request: %w", err)
	}
	// Retry on 429 (rate limit) with exponential backoff. The LLM provider
	// may throttle bursty traffic; a short wait usually succeeds.
	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		retryAfter := 2 * time.Second
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil {
				retryAfter = time.Duration(secs) * time.Second
			}
		}
		for attempt := 0; attempt < 3; attempt++ {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryAfter):
			}
			// Re-create the request (body was consumed).
			httpReq, err = http.NewRequestWithContext(ctx, "POST", c.cfg.GatewayURL+"/chat/completions", bytes.NewReader(body))
			if err != nil {
				return nil, fmt.Errorf("llm: retry request: %w", err)
			}
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("Accept", "text/event-stream")
			if c.cfg.APIKey != "" {
				httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
			}
			resp, err = c.hc.Do(httpReq)
			if err != nil {
				return nil, fmt.Errorf("llm: retry request: %w", err)
			}
			if resp.StatusCode != http.StatusTooManyRequests {
				break
			}
			resp.Body.Close()
			retryAfter *= 2 // exponential backoff: 2s, 4s, 8s
		}
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("llm: unexpected status %d", resp.StatusCode)
	}

	ch := make(chan StreamChunk, 64)
	go c.readSSE(ctx, resp.Body, ch)
	return ch, nil
}

// readSSE parses the SSE stream and pushes deltas onto ch.
func (c *Client) readSSE(ctx context.Context, body io.ReadCloser, ch chan<- StreamChunk) {
	defer body.Close()
	defer close(ch)

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var data strings.Builder
	finishReason := ""
	doneSent := false
	for scanner.Scan() {
		line := scanner.Text()

		// SSE event framing: lines are "field: value". We only care about
		// "data:" lines; blank lines delimit events.
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				doneSent = true
				ch <- StreamChunk{Done: true, FinishReason: finishReason}
				return
			}
			data.WriteString(payload)
			continue
		}

		// Blank line = end of event. Flush accumulated data.
		if line == "" && data.Len() > 0 {
			if !c.emitChunk(ctx, data.String(), ch, &finishReason) {
				return
			}
			data.Reset()
		}
	}

	// Flush any trailing data without a terminating blank line.
	if data.Len() > 0 {
		if !c.emitChunk(ctx, data.String(), ch, &finishReason) {
			return
		}
	}

	// Clean body close WITHOUT a [DONE] marker: the provider ended the
	// stream prematurely — same as a dropped connection.
	if !doneSent && scanner.Err() == nil {
		select {
		case ch <- StreamChunk{Err: &ConnectionLostError{Err: io.ErrUnexpectedEOF}}:
		case <-ctx.Done():
		}
		return
	}

	if err := scanner.Err(); err != nil {
		// A premature body close (Workers AI drops long streams under
		// load) is a recoverable condition, not a hard failure: surface
		// it as ConnectionLostError so callers can retry an empty
		// attempt or keep the partial output. Plain io.EOF means the
		// server ended the body without a [DONE] marker — same thing.
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			select {
			case ch <- StreamChunk{Err: &ConnectionLostError{Err: err}}:
			case <-ctx.Done():
			}
			return
		}
		select {
		case ch <- StreamChunk{Err: fmt.Errorf("llm: read stream: %w", err)}:
		case <-ctx.Done():
		}
	}
}

// ConnectionLostError marks a stream whose HTTP body ended prematurely
// (unexpected EOF) — the model output was cut off mid-flight by the
// provider, not by a client bug.
type ConnectionLostError struct {
	Err error
}

func (e *ConnectionLostError) Error() string {
	return "llm: connection lost mid-stream: " + e.Err.Error()
}

func (e *ConnectionLostError) Unwrap() error { return e.Err }

// IsConnectionLost reports whether err was caused by the provider closing
// the stream body prematurely.
func IsConnectionLost(err error) bool {
	var cl *ConnectionLostError
	return errors.As(err, &cl)
}

// emitChunk parses one SSE data payload and pushes its content delta.
// finishReason is updated when the payload carries choices[0].finish_reason
// (usually the final chunk). Returns false if the stream should stop
// (context cancelled or error).
func (c *Client) emitChunk(ctx context.Context, payload string, ch chan<- StreamChunk, finishReason *string) bool {
	var obj struct {
		Choices []struct {
			Delta struct {
				// Workers AI streams single numeric tokens as bare JSON
				// numbers ("content":8, not "content":"8"). A plain
				// string field here fails to unmarshal the WHOLE payload
				// and silently drops every digit of the generated code —
				// so decode raw and coerce below.
				Content json.RawMessage `json:"content"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(payload), &obj); err != nil {
		// Non-JSON payloads (e.g. gateway keep-alives) are ignored.
		return true
	}
	if obj.Error != nil {
		select {
		case ch <- StreamChunk{Err: errors.New(obj.Error.Message)}:
		case <-ctx.Done():
			return false
		}
		return false
	}
	if len(obj.Choices) > 0 {
		if obj.Choices[0].FinishReason != nil {
			*finishReason = *obj.Choices[0].FinishReason
		}
		content := coerceDeltaContent(obj.Choices[0].Delta.Content)
		if content == "" {
			return true
		}
		select {
		case ch <- StreamChunk{Content: content}:
		case <-ctx.Done():
			return false
		}
	}
	return true
}

// coerceDeltaContent converts a raw JSON "content" value into plain text.
// Workers AI emits numeric tokens as bare JSON numbers ("content":8); such
// a value decodes to its literal text ("8"). Proper JSON strings decode
// normally, and null/empty yields "".
func coerceDeltaContent(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return ""
		}
		return s
	}
	// Bare number (or other scalar): its literal JSON text IS the token.
	return string(raw)
}
