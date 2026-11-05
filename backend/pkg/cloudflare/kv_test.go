package cloudflare

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestKVPutWritesValue pins the Cloudflare KV write contract: the raw body is
// PUT to /accounts/<acct>/storage/kv/namespaces/<ns>/values/<key> with a
// bearer token. The rollout-verification harness relies on it to seed the
// `session:token:<token>` keys it later verifies.
func TestKVPutWritesValue(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.EscapedPath()
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	kv := NewKVClient("acct", "token")
	kv.SetBaseURL(srv.URL)

	if err := kv.Put(context.Background(), "ns-1", "session:token:abc", "user-123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("expected PUT, got %q", gotMethod)
	}
	if want := "/accounts/acct/storage/kv/namespaces/ns-1/values/session:token:abc"; gotPath != want {
		t.Fatalf("expected path %q, got %q", want, gotPath)
	}
	if gotAuth != "Bearer token" {
		t.Fatalf("expected bearer auth, got %q", gotAuth)
	}
	if !strings.HasPrefix(gotCT, "text/plain") {
		t.Fatalf("expected text/plain content type, got %q", gotCT)
	}
	if gotBody != "user-123" {
		t.Fatalf("expected raw body %q, got %q", "user-123", gotBody)
	}
}

// TestKVPutUpstreamError ensures a failed write is never reported as success
// (the harness must know its seed did not land).
func TestKVPutUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false}`))
	}))
	defer srv.Close()

	kv := NewKVClient("acct", "token")
	kv.SetBaseURL(srv.URL)

	if err := kv.Put(context.Background(), "ns-1", "k", "v"); err == nil {
		t.Fatal("expected an error for a 403 upstream response")
	}
}

// TestKVDeleteRemovesKey pins the DELETE contract and its idempotency: a 404
// (already gone) is success so callers can clean up unconditionally.
func TestKVDeleteRemovesKey(t *testing.T) {
	var gotMethod, gotPath string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(status)
	}))
	defer srv.Close()

	kv := NewKVClient("acct", "token")
	kv.SetBaseURL(srv.URL)

	if err := kv.Delete(context.Background(), "ns-1", "session:token:abc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("expected DELETE, got %q", gotMethod)
	}
	if want := "/accounts/acct/storage/kv/namespaces/ns-1/values/session:token:abc"; gotPath != want {
		t.Fatalf("expected path %q, got %q", want, gotPath)
	}

	// A missing key must still report success (idempotent cleanup).
	status = http.StatusNotFound
	if err := kv.Delete(context.Background(), "ns-1", "session:token:abc"); err != nil {
		t.Fatalf("expected a 404 delete to be treated as success, got %v", err)
	}

	// A genuine upstream failure must surface.
	status = http.StatusForbidden
	if err := kv.Delete(context.Background(), "ns-1", "session:token:abc"); err == nil {
		t.Fatal("expected an error for a 403 delete")
	}
}
