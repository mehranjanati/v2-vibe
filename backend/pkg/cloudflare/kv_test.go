package cloudflare

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestKVGetReadsValueFromNamespace pins the Cloudflare KV values contract:
// GET /accounts/<acct>/storage/kv/namespaces/<ns>/values/<key> with a bearer
// token returns the raw value.
func TestKVGetReadsValueFromNamespace(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("user-123"))
	}))
	defer srv.Close()

	kv := NewKVClient("acct", "token")
	kv.SetBaseURL(srv.URL)

	value, err := kv.Get(context.Background(), "ns-1", "session:token:abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != "user-123" {
		t.Fatalf("expected value %q, got %q", "user-123", value)
	}
	if want := "/accounts/acct/storage/kv/namespaces/ns-1/values/session:token:abc"; gotPath != want {
		t.Fatalf("expected path %q, got %q", want, gotPath)
	}
	if gotAuth != "Bearer token" {
		t.Fatalf("expected bearer auth, got %q", gotAuth)
	}
}

// TestKVGetMissingKey verifies a 404 maps to ErrKVKeyNotFound so callers can
// distinguish "unknown session" from "store unavailable".
func TestKVGetMissingKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	kv := NewKVClient("acct", "token")
	kv.SetBaseURL(srv.URL)

	_, err := kv.Get(context.Background(), "ns-1", "missing")
	if !errors.Is(err, ErrKVKeyNotFound) {
		t.Fatalf("expected ErrKVKeyNotFound, got %v", err)
	}
}

// TestKVGetUpstreamError verifies non-404 failures surface as errors (never as
// an empty value), so guards fail closed.
func TestKVGetUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false}`))
	}))
	defer srv.Close()

	kv := NewKVClient("acct", "token")
	kv.SetBaseURL(srv.URL)

	_, err := kv.Get(context.Background(), "ns-1", "key")
	if err == nil {
		t.Fatal("expected an error for a 403 upstream response")
	}
	if errors.Is(err, ErrKVKeyNotFound) {
		t.Fatal("a 403 must not be reported as a missing key")
	}
}

// TestKVGetRequiresConfig rejects a client without credentials or a namespace
// instead of issuing an unauthenticated request.
func TestKVGetRequiresConfig(t *testing.T) {
	if _, err := NewKVClient("", "").Get(context.Background(), "ns", "k"); err == nil {
		t.Fatal("expected an error when account credentials are missing")
	}
	if _, err := NewKVClient("acct", "token").Get(context.Background(), "", "k"); err == nil {
		t.Fatal("expected an error when the namespace id is missing")
	}
}
