package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrKVKeyNotFound is returned by KVClient.Get when the key does not exist
// (Cloudflare answers 404 for a missing KV value).
var ErrKVKeyNotFound = errors.New("kv: key not found")

// KVClient reads values from a Cloudflare Workers KV namespace through the
// REST API v4. It reuses the same account id / API token as the Pages and D1
// clients.
//
// The control plane uses it to verify Edge (light Worker) session tokens: the
// Worker writes `session:token:<token>` -> userId into the VibecoderStore
// namespace (worker/light/lightApp.ts), and the Go control plane reads that
// key back to resolve the caller's identity (backend/pkg/api/auth.go P0.3).
type KVClient struct {
	accountID string
	apiToken  string
	hc        *http.Client
	// baseURL overrides the API root (defaults to
	// https://api.cloudflare.com/client/v4). Set by tests.
	baseURL string
}

// NewKVClient creates a KV client for the given Cloudflare account.
func NewKVClient(accountID, apiToken string) *KVClient {
	return &KVClient{
		accountID: accountID,
		apiToken:  apiToken,
		hc:        &http.Client{Timeout: 10 * time.Second},
	}
}

// SetBaseURL overrides the Cloudflare API root (defaults to
// https://api.cloudflare.com/client/v4). Used by tests to point the client
// at an httptest server emulating the KV REST API.
func (k *KVClient) SetBaseURL(rawURL string) {
	k.baseURL = strings.TrimRight(rawURL, "/")
}

// Get reads one key from a KV namespace. A missing key surfaces as
// ErrKVKeyNotFound; any other failure (credentials, transport, upstream
// status) is returned as a plain error so callers can fail closed.
func (k *KVClient) Get(ctx context.Context, namespaceID, key string) (string, error) {
	if k.accountID == "" || k.apiToken == "" {
		return "", errors.New("kv: accountID and apiToken are required")
	}
	if namespaceID == "" {
		return "", errors.New("kv: namespaceID is required")
	}

	base := k.baseURL
	if base == "" {
		base = "https://api.cloudflare.com/client/v4"
	}
	endpoint := fmt.Sprintf("%s/accounts/%s/storage/kv/namespaces/%s/values/%s",
		base, k.accountID, url.PathEscape(namespaceID), url.PathEscape(key))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("kv: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+k.apiToken)

	resp, err := k.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("kv: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", ErrKVKeyNotFound
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("kv: read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("kv: status %d: %s", resp.StatusCode, string(raw))
	}
	return string(raw), nil
}
