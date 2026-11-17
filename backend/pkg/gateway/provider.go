package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// Provider is one adapter in front of a model API. Adapters are replaceable:
// nothing outside this package may depend on a concrete provider.
type Provider interface {
	// ID is the registry key (DNS-safe slug).
	ID() string
	// Chat runs one completion. Failing providers return *ProviderError so
	// routing can classify the failure.
	Chat(ctx context.Context, req ProviderRequest) (ProviderResponse, error)
}

// ProviderRequest is the adapter-level request (model already resolved).
type ProviderRequest struct {
	ModelID     string
	Messages    []Message
	MaxTokens   int
	Temperature float64
	// APIKey is resolved from the vault at call time. It is never logged.
	APIKey  string
	Timeout time.Duration
}

// ProviderResponse is the adapter-level result.
type ProviderResponse struct {
	Content string
	Usage   Usage
}

// SecretResolver resolves `vault:` references to secret values at call time.
// The gateway never caches or logs the value.
type SecretResolver interface {
	Resolve(vaultRef string) (string, error)
}

// HTTPProvider talks to any OpenAI-compatible `/chat/completions` endpoint,
// which covers AI Gateway, Workers AI, and self-hosted open models behind an
// OpenAI shim. One adapter, many backends — the point of the gateway.
type HTTPProvider struct {
	// Name is the registry key.
	Name string
	// BaseURL is the API root; `/chat/completions` is appended.
	BaseURL string
	// SecretRef is a `vault:` reference, never a literal key.
	SecretRef string
	// Client is optional; defaults to a client bounded by the request timeout.
	Client *http.Client
}

// ID implements Provider.
func (p *HTTPProvider) ID() string { return p.Name }

// Chat implements Provider.
func (p *HTTPProvider) Chat(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	if strings.TrimSpace(p.BaseURL) == "" {
		return ProviderResponse{}, &ProviderError{Provider: p.Name, Class: ErrFatal, Message: "base URL is empty"}
	}
	if strings.TrimSpace(req.APIKey) == "" {
		return ProviderResponse{}, &ProviderError{Provider: p.Name, Class: ErrFatal, Message: "no API key resolved from the vault reference"}
	}

	payload := map[string]any{
		"model":       req.ModelID,
		"messages":    req.Messages,
		"temperature": req.Temperature,
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ProviderResponse{}, &ProviderError{Provider: p.Name, Class: ErrFatal, Message: "encode request", Err: err}
	}

	url := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ProviderResponse{}, &ProviderError{Provider: p.Name, Class: ErrFatal, Message: "build request", Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)

	client := p.Client
	if client == nil {
		timeout := req.Timeout
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return ProviderResponse{}, &ProviderError{Provider: p.Name, Class: ErrRetryable, Message: "transport", Err: err}
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if readErr != nil {
		return ProviderResponse{}, &ProviderError{Provider: p.Name, Class: ErrRetryable, Status: resp.StatusCode, Message: "read body", Err: readErr}
	}
	if resp.StatusCode >= 400 {
		class := ErrFatal
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			class = ErrRetryable
		}
		return ProviderResponse{}, &ProviderError{
			Provider: p.Name, Class: class, Status: resp.StatusCode,
			Message: snippet(string(raw)),
		}
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ProviderResponse{}, &ProviderError{Provider: p.Name, Class: ErrRetryable, Message: "decode response", Err: err}
	}
	if len(parsed.Choices) == 0 {
		return ProviderResponse{}, &ProviderError{Provider: p.Name, Class: ErrRetryable, Message: "response carried no choices"}
	}
	usage := Usage{
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		TotalTokens:      parsed.Usage.TotalTokens,
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return ProviderResponse{Content: parsed.Choices[0].Message.Content, Usage: usage}, nil
}

func snippet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
