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

// valuesURL builds the Cloudflare KV single-value endpoint for a key. It
// validates that the client is configured (account id + token + namespace)
// so callers never issue an unauthenticated request, and is shared by
// Get/Put/Delete. Tests override the API root via SetBaseURL.
func (k *KVClient) valuesURL(namespaceID, key string) (string, error) {
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
	return fmt.Sprintf("%s/accounts/%s/storage/kv/namespaces/%s/values/%s",
		base, k.accountID, url.PathEscape(namespaceID), url.PathEscape(key)), nil
}

// Get reads one key from a KV namespace. A missing key surfaces as
// ErrKVKeyNotFound; any other failure (credentials, transport, upstream
// status) is returned as a plain error so callers can fail closed.
func (k *KVClient) Get(ctx context.Context, namespaceID, key string) (string, error) {
	endpoint, err := k.valuesURL(namespaceID, key)
	if err != nil {
		return "", err
	}

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

// Put writes one key into a KV namespace. Cloudflare stores the raw request
// body as the value (there is no JSON envelope on the write path) and
// answers 200 with {"success":true}. Any non-2xx status is returned as an
// error so callers fail loudly instead of assuming the write landed.
//
// The control plane does not mint Edge sessions at runtime (Edge stays the
// identity authority — P0.9), but it needs Put to provision and clean up the
// `session:token:<token>` keys used by the P0.3 rollout-verification harness
// (backend/e2e/p03_session_boundary_test.go).
func (k *KVClient) Put(ctx context.Context, namespaceID, key, value string) error {
	endpoint, err := k.valuesURL(namespaceID, key)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, strings.NewReader(value))
	if err != nil {
		return fmt.Errorf("kv: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+k.apiToken)
	req.Header.Set("Content-Type", "text/plain")

	resp, err := k.hc.Do(req)
	if err != nil {
		return fmt.Errorf("kv: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("kv: put status %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}

// Delete removes one key from a KV namespace. Deletion is idempotent: a
// missing key (404) is reported as success so callers can clean up without
// first checking existence. Any other failure is returned as an error.
func (k *KVClient) Delete(ctx context.Context, namespaceID, key string) error {
	endpoint, err := k.valuesURL(namespaceID, key)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("kv: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+k.apiToken)

	resp, err := k.hc.Do(req)
	if err != nil {
		return fmt.Errorf("kv: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("kv: delete status %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}
