package mcp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// P1-AGT-003 — live transport round-trip.
//
// The stub server is a real separate process (testdata/stub-mcp), so these tests
// exercise the actual protocol channel: spawn, JSON-RPC handshake, tools/list,
// tools/call, and every failure mode the adapter must contain.

var (
	stubOnce sync.Once
	stubPath string
	stubErr  error
)

// stubBinary builds the stub server once per test run and returns its path.
func stubBinary(t *testing.T) string {
	t.Helper()
	stubOnce.Do(func() {
		dir, err := os.MkdirTemp("", "mcp-stub-")
		if err != nil {
			stubErr = err
			return
		}
		stubPath = filepath.Join(dir, "stub-mcp")
		cmd := exec.Command("go", "build", "-o", stubPath, "./testdata/stub-mcp")
		out, err := cmd.CombinedOutput()
		if err != nil {
			stubErr = &buildError{out: string(out), err: err}
		}
	})
	if stubErr != nil {
		t.Fatalf("build stub server: %v", stubErr)
	}
	return stubPath
}

type buildError struct {
	out string
	err error
}

func (e *buildError) Error() string {
	return "go build failed: " + strings.TrimSpace(e.out)
}

// liveHarness is the standard gate harness pointed at the real transport.
func liveHarness(t *testing.T) (*harness, *StdioTransport) {
	t.Helper()
	h := newHarness(t)
	tr := &StdioTransport{Command: []string{stubBinary(t)}, Timeout: 20 * time.Second}
	return h, tr
}

// TestStdioTransportLiveRoundTrip is the first acceptance criterion: a real MCP
// server process is discovered and invoked through the trust gate.
func TestStdioTransportLiveRoundTrip(t *testing.T) {
	h, tr := liveHarness(t)

	// Discovery needs no grant and returns metadata only.
	discovery, err := h.gate.Discover(h.id, "crm", tr)
	if err != nil {
		t.Fatalf("Discover against the live server: %v", err)
	}
	if len(discovery.Tools) != 2 {
		t.Fatalf("expected two advertised tools, got %d: %+v", len(discovery.Tools), discovery.Tools)
	}
	names := map[string]bool{}
	for _, tool := range discovery.Tools {
		names[tool.Name] = true
	}
	if !names["lookup"] || !names["explode"] {
		t.Fatalf("discovery did not report the stub tools: %+v", discovery.Tools)
	}
	if discovery.Note == "" {
		t.Fatal("discovery must state that it grants nothing")
	}

	// A granted call executes for real and returns the server's text content.
	// The policy's idempotency key is the dotted path `lead.id`, so the args must
	// carry it — the gate refuses a call whose dedup key is missing.
	grant := h.mustGrant(t, false)
	result, err := h.gate.Call(CallRequest{
		Identity: h.id,
		Ref:      h.ref,
		GrantID:  grant.ID,
		ArgsJSON: `{"lead":{"id":"lead-42"}}`,
	}, tr)
	if err != nil {
		t.Fatalf("granted Call against the live server: %v", err)
	}
	if !strings.Contains(result.Output, "lead-42") {
		t.Fatalf("unexpected tool output: %q", result.Output)
	}
	if result.GrantID != grant.ID {
		t.Fatalf("result should echo the authorizing grant: %q", result.GrantID)
	}
}

// TestStdioTransportGrantlessCallNeverSpawns pins the second acceptance
// criterion with the real transport: a call without a grant is denied by the
// gate, and the server process is never started. The command is deliberately a
// non-existent binary, so any spawn attempt would surface as a different error.
func TestStdioTransportGrantlessCallNeverSpawns(t *testing.T) {
	h := newHarness(t)
	tr := &StdioTransport{Command: []string{filepath.Join(t.TempDir(), "never-executed")}}

	_, err := h.gate.Call(CallRequest{
		Identity: h.id,
		Ref:      h.ref,
		ArgsJSON: `{"id":"lead-42"}`,
	}, tr)
	mustDenied(t, err, "no grant")
}

// TestStdioTransportRejectsNonJSONStdout pins the channel rule: a server that
// writes anything but JSON-RPC to stdout is refused, not trusted.
func TestStdioTransportRejectsNonJSONStdout(t *testing.T) {
	h := newHarness(t)
	tr := &StdioTransport{Command: []string{"sh", "-c", "echo not-json"}}

	if _, err := h.gate.Discover(h.id, "crm", tr); err == nil {
		t.Fatal("expected a non-JSON stdout to be rejected")
	} else if !strings.Contains(err.Error(), "non-JSON") {
		t.Fatalf("wrong error: %v", err)
	}
}

// TestStdioTransportTimesOut pins the deadline: a hanging server is killed and
// reported, so a call cannot pin a worker forever.
func TestStdioTransportTimesOut(t *testing.T) {
	h := newHarness(t)
	tr := &StdioTransport{Command: []string{"sh", "-c", "sleep 10"}, Timeout: 150 * time.Millisecond}

	start := time.Now()
	_, err := h.gate.Discover(h.id, "crm", tr)
	if err == nil {
		t.Fatal("expected the hanging server to time out")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout did not kill the server promptly (%s)", elapsed)
	}
}

// TestStdioTransportPropagatesToolError pins that a tool-level failure is an
// error, never an empty success.
func TestStdioTransportPropagatesToolError(t *testing.T) {
	h, tr := liveHarness(t)
	ref := ServerRef{Server: "crm", Tool: "explode", Version: "1.0.0"}
	policy := h.policy
	policy.Tool = "explode"
	if err := h.policies.Put(policy); err != nil {
		t.Fatalf("policy Put: %v", err)
	}
	grant, err := h.grants.Issue(h.id, ref, policy, "user-1", false, time.Time{}, fixedNow())
	if err != nil {
		t.Fatalf("grant: %v", err)
	}

	if _, err := h.gate.Call(CallRequest{Identity: h.id, Ref: ref, GrantID: grant.ID}, tr); err == nil {
		t.Fatal("a tool that reports isError must surface as an error")
	}
}
