package mcp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeTransport is a controllable MCP server: it records invocations and can
// be told to fail.
type fakeTransport struct {
	calls    []string
	failWith error
	tools    []DiscoveredTool
}

func (f *fakeTransport) DiscoverTools(ServerRegistration) ([]DiscoveredTool, error) {
	return f.tools, nil
}

func (f *fakeTransport) InvokeTool(_ ServerRegistration, tool, args string) (string, error) {
	if f.failWith != nil {
		return "", f.failWith
	}
	f.calls = append(f.calls, tool+" "+args)
	return `{"ok":true,"tool":"` + tool + `"}`, nil
}

func fixedNow() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) }

// harness wires a registry, policy store, grant store, audit sink and gate.
type harness struct {
	reg      *Registry
	policies *PolicyStore
	grants   *Store
	audit    *MemorySink
	gate     *Gate
	tr       *fakeTransport
	id       Identity
	ref      ServerRef
	policy   ToolPolicy
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		reg:      NewRegistry(),
		policies: NewPolicyStore(),
		grants:   NewStore(),
		audit:    NewMemorySink(),
		tr:       &fakeTransport{},
		id:       Identity{Tenant: "tenant-a", User: "user-1", Agent: "lead-qualifier"},
		ref:      ServerRef{Server: "crm", Tool: "lookup", Version: "1.0.0"},
		policy: ToolPolicy{
			Server: "crm", Tool: "lookup", ToolManifestVersion: "1.0.0",
			Permissions: []string{"leads:read"}, SideEffect: SideEffectSafe,
			IdempotencyKey: "lead.id",
		},
	}
	gate, err := NewGate(GateConfig{Registry: h.reg, Policies: h.policies, Grants: h.grants, Audit: h.audit, Now: fixedNow})
	if err != nil {
		t.Fatalf("NewGate: %v", err)
	}
	h.gate = gate
	if err := h.reg.Register(ServerRegistration{
		Name: "crm", Endpoint: "stdio://crm", Tools: []string{"lookup"},
		RegisteredBy: "user-1", RegisteredAt: fixedNow(),
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := h.policies.Put(h.policy); err != nil {
		t.Fatalf("policy Put: %v", err)
	}
	return h
}

func (h *harness) mustGrant(t *testing.T, human bool) Grant {
	t.Helper()
	g, err := h.grants.Issue(h.id, h.ref, h.policy, "user-1", human, time.Time{}, fixedNow())
	if err != nil {
		t.Fatalf("Issue grant: %v", err)
	}
	return g
}

func mustDenied(t *testing.T, err error, wantSubstring string) {
	t.Helper()
	var denied *DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("want DeniedError, got %v", err)
	}
	if !strings.Contains(denied.Verdict, wantSubstring) {
		t.Fatalf("verdict %q does not mention %q", denied.Verdict, wantSubstring)
	}
}

func argsWithKey(key string) string {
	b, _ := json.Marshal(map[string]any{"lead": map[string]any{"id": key}})
	return string(b)
}

// --- acceptance: discovery grants nothing -----------------------------------

func TestDiscoverReturnsMetadataOnlyAndGrantsNothing(t *testing.T) {
	h := newHarness(t)
	h.tr.tools = []DiscoveredTool{{Name: "lookup", Description: "look up a lead"}}

	res, err := h.gate.Discover(h.id, "crm", h.tr)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res.Tools) != 1 {
		t.Fatalf("want 1 discovered tool, got %d", len(res.Tools))
	}
	if !strings.Contains(res.Note, "grant") {
		t.Fatalf("discovery note must state that a grant is required, got %q", res.Note)
	}
	if len(h.grants.List(h.id)) != 0 {
		t.Fatal("discovery must not issue any grant")
	}

	// Negative: a call without a grant is denied even though discovery ran.
	_, err = h.gate.Call(CallRequest{Identity: h.id, Ref: h.ref, ArgsJSON: argsWithKey("l1")}, h.tr)
	mustDenied(t, err, "no grant")
	if len(h.tr.calls) != 0 {
		t.Fatal("denied call must never reach the transport")
	}
	events := h.audit.Events()
	if len(events) != 1 || events[0].Decision != DecisionDenied {
		t.Fatalf("want one denied audit event, got %+v", events)
	}
}

func TestDiscoverRequiresTenantAndRegistration(t *testing.T) {
	h := newHarness(t)
	if _, err := h.gate.Discover(Identity{}, "crm", h.tr); err == nil {
		t.Fatal("discovery without a tenant must fail")
	}
	if _, err := h.gate.Discover(h.id, "unknown", h.tr); err == nil {
		t.Fatal("discovery of an unregistered server must fail")
	}
}

// --- acceptance: a granted call carries identity, approval ref, idem key ----

func TestGrantedCallAuditsIdentityApprovalAndIdempotencyKey(t *testing.T) {
	h := newHarness(t)
	g := h.mustGrant(t, false)

	res, err := h.gate.Call(CallRequest{
		Identity: h.id, Ref: h.ref, GrantID: g.ID, ArgsJSON: argsWithKey("lead-42"),
	}, h.tr)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if res.GrantID != g.ID || res.IdempotencyKey != "lead-42" {
		t.Fatalf("result must echo grant + idempotency key, got %+v", res)
	}
	if len(h.tr.calls) != 1 {
		t.Fatalf("want exactly one transport call, got %d", len(h.tr.calls))
	}

	events := h.audit.Events()
	if len(events) != 1 {
		t.Fatalf("want one audit event, got %d", len(events))
	}
	e := events[0]
	if e.Decision != DecisionAllowed {
		t.Fatalf("want allowed, got %s (%s)", e.Decision, e.Verdict)
	}
	if e.Tenant != "tenant-a" || e.User != "user-1" || e.Agent != "lead-qualifier" {
		t.Fatalf("audit must carry tenant/user/agent identity, got %+v", e)
	}
	if e.GrantID != g.ID || e.IdempotencyKey != "lead-42" {
		t.Fatalf("audit must carry grant + idempotency key, got %+v", e)
	}
	if e.Server != "crm" || e.Tool != "lookup" || e.Version != "1.0.0" {
		t.Fatalf("audit must carry the pinned ref, got %+v", e)
	}
}

func TestApprovalRefIsAuditedWhenPolicyRequiresIt(t *testing.T) {
	h := newHarness(t)
	h.policy.SideEffect = SideEffectIrreversible
	h.policy.Approvals = []ApprovalClass{ApprovalPayment}
	h.policy.RequiresHuman = true
	if err := h.policies.Put(h.policy); err != nil {
		t.Fatalf("policy Put: %v", err)
	}

	// Rejected: standing policy never satisfies a human-required tool.
	if _, err := h.grants.Issue(h.id, h.ref, h.policy, "policy:auto", false, time.Time{}, fixedNow()); err == nil {
		t.Fatal("issuing a human-required grant without human approval must fail")
	}

	g := h.mustGrant(t, true)
	// Rejected: grant exists but this call carries no approval reference.
	_, err := h.gate.Call(CallRequest{Identity: h.id, Ref: h.ref, GrantID: g.ID, ArgsJSON: argsWithKey("l1")}, h.tr)
	mustDenied(t, err, "approval reference")

	_, err = h.gate.Call(CallRequest{
		Identity: h.id, Ref: h.ref, GrantID: g.ID, ArgsJSON: argsWithKey("l1"),
		ApprovalRef: "approval:apr-9",
	}, h.tr)
	if err != nil {
		t.Fatalf("approved call must succeed: %v", err)
	}

	var last AuditEvent
	for _, e := range h.audit.Events() {
		if e.Decision == DecisionAllowed {
			last = e
		}
	}
	if last.ApprovalRef != "approval:apr-9" {
		t.Fatalf("audit must record the approval reference, got %+v", last)
	}
}

// --- security: revocation, drift, tenancy, replay, transport ----------------

func TestRevokedGrantFailsClosedImmediately(t *testing.T) {
	h := newHarness(t)
	g := h.mustGrant(t, false)
	if err := h.grants.Revoke(h.id, g.ID, "user-1", fixedNow()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, err := h.gate.Call(CallRequest{Identity: h.id, Ref: h.ref, GrantID: g.ID, ArgsJSON: argsWithKey("l1")}, h.tr)
	mustDenied(t, err, "revoked")
	if len(h.tr.calls) != 0 {
		t.Fatal("a revoked grant must never reach the transport")
	}
}

func TestExpiredGrantFailsClosed(t *testing.T) {
	h := newHarness(t)
	expires := fixedNow().Add(time.Hour)
	g, err := h.grants.Issue(h.id, h.ref, h.policy, "user-1", false, expires, fixedNow())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// Advance the clock past expiry.
	late, _ := NewGate(GateConfig{Registry: h.reg, Policies: h.policies, Grants: h.grants, Audit: h.audit,
		Now: func() time.Time { return expires.Add(time.Second) }})
	_, err = late.Call(CallRequest{Identity: h.id, Ref: h.ref, GrantID: g.ID, ArgsJSON: argsWithKey("l1")}, h.tr)
	mustDenied(t, err, "expired")
}

func TestPolicyDriftForcesRegrant(t *testing.T) {
	h := newHarness(t)
	g := h.mustGrant(t, false)

	// Widen the live policy: the old grant snapshot may not inherit it.
	widened := h.policy
	widened.Permissions = []string{"leads:read", "leads:write"}
	if err := h.policies.Put(widened); err != nil {
		t.Fatalf("policy Put: %v", err)
	}

	_, err := h.gate.Call(CallRequest{Identity: h.id, Ref: h.ref, GrantID: g.ID, ArgsJSON: argsWithKey("l1")}, h.tr)
	mustDenied(t, err, "drifted")
	if len(h.tr.calls) != 0 {
		t.Fatal("a drifted grant must not execute")
	}
}

func TestCrossTenantGrantIsInvisible(t *testing.T) {
	h := newHarness(t)
	g := h.mustGrant(t, false)

	other := Identity{Tenant: "tenant-b", User: "user-2", Agent: "lead-qualifier"}
	_, err := h.gate.Call(CallRequest{Identity: other, Ref: h.ref, GrantID: g.ID, ArgsJSON: argsWithKey("l1")}, h.tr)
	mustDenied(t, err, "grant not found")
	if len(h.grants.List(other)) != 0 {
		t.Fatal("another tenant must not see this grant")
	}
	if got := len(h.audit.ByTenant("tenant-b")); got != 1 {
		t.Fatalf("the denied cross-tenant attempt must be audited under its own tenant, got %d", got)
	}
}

func TestIdempotentReplaySkipsSecondExecution(t *testing.T) {
	h := newHarness(t)
	g := h.mustGrant(t, false)
	req := CallRequest{Identity: h.id, Ref: h.ref, GrantID: g.ID, ArgsJSON: argsWithKey("lead-7")}

	if _, err := h.gate.Call(req, h.tr); err != nil {
		t.Fatalf("first Call: %v", err)
	}
	if _, err := h.gate.Call(req, h.tr); err != nil {
		t.Fatalf("second Call: %v", err)
	}
	if len(h.tr.calls) != 1 {
		t.Fatalf("replay must not re-execute, transport calls = %d", len(h.tr.calls))
	}
	events := h.audit.Events()
	if len(events) != 2 {
		t.Fatalf("both attempts must be audited, got %d", len(events))
	}
	if events[1].Decision != DecisionAllowed || !strings.Contains(events[1].Verdict, "replay") {
		t.Fatalf("replay must be audited as an allowed replay, got %+v", events[1])
	}
}

func TestUnregisteredServerAndMissingIdempotencyKeyAreDenied(t *testing.T) {
	h := newHarness(t)
	g := h.mustGrant(t, false)

	// Args without the declared idempotency field.
	_, err := h.gate.Call(CallRequest{Identity: h.id, Ref: h.ref, GrantID: g.ID, ArgsJSON: `{"lead":{}}`}, h.tr)
	mustDenied(t, err, "idempotency key")

	// Removing the server revokes every grant against it at once.
	newGate := h.gate
	h.reg.Remove("crm")
	_, err = newGate.Call(CallRequest{Identity: h.id, Ref: h.ref, GrantID: g.ID, ArgsJSON: argsWithKey("l1")}, h.tr)
	mustDenied(t, err, "not registered")
}

func TestTransportErrorIsAuditedAndNotAllowed(t *testing.T) {
	h := newHarness(t)
	g := h.mustGrant(t, false)
	h.tr.failWith = errors.New("connection reset")

	_, err := h.gate.Call(CallRequest{Identity: h.id, Ref: h.ref, GrantID: g.ID, ArgsJSON: argsWithKey("l1")}, h.tr)
	if err == nil || errors.As(err, new(*DeniedError)) {
		t.Fatalf("transport failure must surface as a transport error, got %v", err)
	}
	e := h.audit.Events()[0]
	if e.Decision != DecisionDenied || !strings.Contains(e.Verdict, "transport error") {
		t.Fatalf("transport failure must be audited separately, got %+v", e)
	}
}

func TestPolicyAndRefValidationRejectsUnsafeShapes(t *testing.T) {
	h := newHarness(t)

	bad := h.policy
	bad.Secrets = []string{"sk-live-literal"} // literal secret
	if err := h.policies.Put(bad); err == nil {
		t.Fatal("a literal secret must be rejected (vault: refs only)")
	}

	bad2 := h.policy
	bad2.SideEffect = SideEffectIrreversible // no approvals declared
	if err := h.policies.Put(bad2); err == nil {
		t.Fatal("an irreversible tool without an approval class must be rejected")
	}

	bad3 := h.policy
	bad3.ToolManifestVersion = "latest"
	if err := h.policies.Put(bad3); err == nil {
		t.Fatal("an unpinned manifest version must be rejected")
	}

	if _, err := h.grants.Issue(h.id, ServerRef{Server: "crm", Tool: "lookup", Version: "1"}, h.policy, "user-1", false, time.Time{}, fixedNow()); err == nil {
		t.Fatal("a non-semver ref must be rejected")
	}
	if _, err := h.grants.Issue(Identity{}, h.ref, h.policy, "user-1", false, time.Time{}, fixedNow()); err == nil {
		t.Fatal("a grant without a tenant must be rejected")
	}
}
