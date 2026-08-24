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
	"strings"
	"time"
)

// Config holds the LLM / AI Gateway connection settings, sourced from
// environment variables (AI_GATEWAY_URL, AI_GATEWAY_API_KEY, DEFAULT_MODEL).
type Config struct {
	// GatewayURL is the base URL of the AI Gateway / OpenAI-compatible
	// endpoint, e.g. https://gateway.ai.cloudflare.com/v1/<account>/<gateway>.
	GatewayURL string
	// APIKey is the bearer token for the gateway.
	APIKey string
	// Model is the default model identifier, e.g. "@cf/meta/llama-3.1-8b-instruct".
	Model string
	// Timeout bounds the entire streaming request.
	Timeout time.Duration
	// HTTPClient is optional; defaults to http.DefaultClient.
	HTTPClient *http.Client
}

// NewConfigFromEnv builds a Config from environment variables.
func NewConfigFromEnv() Config {
	return Config{
		GatewayURL: getEnv("AI_GATEWAY_URL", ""),
		APIKey:     getEnv("AI_GATEWAY_API_KEY", ""),
		Model:      getEnv("DEFAULT_MODEL", "@cf/meta/llama-3.1-8b-instruct"),
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
		return nil, errors.New("llm: AI_GATEWAY_URL is not configured")
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
	for scanner.Scan() {
		line := scanner.Text()

		// SSE event framing: lines are "field: value". We only care about
		// "data:" lines; blank lines delimit events.
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				ch <- StreamChunk{Done: true}
				return
			}
			data.WriteString(payload)
			continue
		}

		// Blank line = end of event. Flush accumulated data.
		if line == "" && data.Len() > 0 {
			if !c.emitChunk(ctx, data.String(), ch) {
				return
			}
			data.Reset()
		}
	}

	// Flush any trailing data without a terminating blank line.
	if data.Len() > 0 {
		if !c.emitChunk(ctx, data.String(), ch) {
			return
		}
	}

	if err := scanner.Err(); err != nil {
		select {
		case ch <- StreamChunk{Err: fmt.Errorf("llm: read stream: %w", err)}:
		case <-ctx.Done():
		}
	}
}

// emitChunk parses one SSE data payload and pushes its content delta.
// Returns false if the stream should stop (context cancelled or error).
func (c *Client) emitChunk(ctx context.Context, payload string, ch chan<- StreamChunk) bool {
	var obj struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
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
		content := obj.Choices[0].Delta.Content
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
