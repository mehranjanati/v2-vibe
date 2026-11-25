package api

// P1-AGT-004 (D8.3) — route-level proof for the MCP surface.
//
// The contract these tests pin (docs/architecture/mcp-trust-slice.md):
//   - every route is behind the session guard (401 anonymous, nothing dialed);
//   - the tenant comes from the session: another tenant's grant is invisible
//     (list empty, revoke 404, call 404), never an enumeration oracle;
//   - a grant that does not authorize the call is refused by the Gate (403)
//     and the transport is never dialed;
//   - a grant is issued only against a reviewed, pinned policy — the body
//     cannot carry permissions;
//   - a policy that needs the (still closed) credential store is refused with
//     503 before dialing, instead of running without its credential;
//   - a registration the route surface may not dial (stdio) is a 409 conflict.
//
// The transport is injected through the surface factory, so these tests never
// open a socket; the SSE binding's own wire behaviour is proved in
// backend/pkg/mcp/sse_test.go.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"backend/pkg/engine"
	"backend/pkg/mcp"
)

// fakeMCPTransport counts what actually reached the transport boundary.
type fakeMCPTransport struct {
	tools       []mcp.DiscoveredTool
	discoveries int
	calls       []string
	headers     map[string]string
}

func (f *fakeMCPTransport) DiscoverTools(mcp.ServerRegistration) ([]mcp.DiscoveredTool, error) {
	f.discoveries++
	return f.tools, nil
}

func (f *fakeMCPTransport) InvokeTool(_ mcp.ServerRegistration, tool, args string) (string, error) {
	f.calls = append(f.calls, tool+" "+args)
	return "tool " + tool + " ran with " + args, nil
}

func mcpTestNow() time.Time { return time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC) }

type mcpFixture struct {
	app       *fiber.App
	hub       *engine.EngineHub
	surface   *mcp.Surface
	transport *fakeMCPTransport
}

// newMCPFixture builds the real routes over an in-memory surface. The tokens
// table is the same two-tenant table the P0.7 cases use.
func newMCPFixture(t *testing.T, tokens map[string]string) *mcpFixture {
	t.Helper()
	surface, err := mcp.NewSurface(mcpTestNow)
	if err != nil {
		t.Fatalf("mcp.NewSurface: %v", err)
	}
	for _, registration := range []mcp.ServerRegistration{
		{
			Name: "crm", Endpoint: "https://crm.example.test/sse", Tools: []string{"lookup"},
			RegisteredBy: "owner", RegisteredAt: mcpTestNow(),
		},
		{
			// A local process server: registered, but not routable.
			Name: "local", Endpoint: "stdio://local", Tools: []string{"lookup"},
			RegisteredBy: "owner", RegisteredAt: mcpTestNow(),
		},
	} {
		if err := surface.Registry.Register(registration); err != nil {
			t.Fatalf("Register(%s): %v", registration.Name, err)
		}
	}
	policies := []mcp.ToolPolicy{
		{
			Server: "crm", Tool: "lookup", ToolManifestVersion: "1.0.0",
			Permissions: []string{"leads:read"}, SideEffect: mcp.SideEffectSafe,
			IdempotencyKey: "lead.id",
		},
		{
			// A tool whose review assumes a vault-resolved secret.
			Server: "crm", Tool: "secret", ToolManifestVersion: "1.0.0",
			Permissions: []string{"leads:read"}, SideEffect: mcp.SideEffectSafe,
			Secrets: []string{"vault:crm/token"},
		},
		{
			// The same review, on a registration the route surface may not dial.
			Server: "local", Tool: "lookup", ToolManifestVersion: "1.0.0",
			Permissions: []string{"leads:read"}, SideEffect: mcp.SideEffectSafe,
		},
	}
	for _, policy := range policies {
		if err := surface.Policies.Put(policy); err != nil {
			t.Fatalf("policy Put: %v", err)
		}
	}

	transport := &fakeMCPTransport{tools: []mcp.DiscoveredTool{
		{Name: "lookup", Description: "look up a lead"},
		{Name: "secret", Description: "needs a credential"},
	}}
	// The factory replaces SSEOnlyTransport so the test never dials. The real
	// factory's scheme rule is proved separately (TestMCPUnroutableServerIsConflict
	// uses a surface without this override).
	surface.Transport = func(_ mcp.ServerRegistration, headers map[string]string) (mcp.Transport, error) {
		transport.headers = headers
		return transport, nil
	}

	hub := engine.NewEngineHub(nil, nil, nil)
	if tokens != nil {
		hub.SetSessionVerifier(NewStaticSessionVerifier(tokens))
	}
	hub.SetMCPSurface(surface)

	app := fiber.New()
	RegisterRoutes(app, hub)
	return &mcpFixture{app: app, hub: hub, surface: surface, transport: transport}
}

// TestMCPRoutesRequireSession: every route answers 401 without a session, and
// nothing reaches the stores or the transport.
func TestMCPRoutesRequireSession(t *testing.T) {
	f := newMCPFixture(t, p07Tokens())

	routes := []struct {
		method string
		path   string
		body   any
	}{
		{"GET", "/api/mcp/servers", nil},
		{"GET", "/api/mcp/servers/crm/tools", nil},
		{"GET", "/api/mcp/grants", nil},
		{"POST", "/api/mcp/grants", map[string]any{"server": "crm", "tool": "lookup", "version": "1.0.0"}},
		{"POST", "/api/mcp/grants/grant-1/revoke", nil},
		{"POST", "/api/mcp/call", map[string]any{"server": "crm", "tool": "lookup", "version": "1.0.0", "grantId": "grant-1"}},
	}
	for _, route := range routes {
		status, _ := workflowRequest(t, f.app, route.method, route.path, "", route.body)
		if status != fiber.StatusUnauthorized {
			t.Fatalf("anonymous %s %s: expected 401, got %d", route.method, route.path, status)
		}
	}
	if len(f.transport.calls) != 0 || f.transport.discoveries != 0 {
		t.Fatalf("an anonymous request must not reach the transport: calls=%v discoveries=%d", f.transport.calls, f.transport.discoveries)
	}
	if grants := f.surface.Grants.List(mcp.Identity{Tenant: "user-a"}); len(grants) != 0 {
		t.Fatalf("an anonymous request must not issue a grant, got %v", grants)
	}
}

// TestMCPCallWithoutAuthorityIsRefusedBeforeDialing pins the acceptance
// criterion: a call the Gate refuses is a 403 and the transport is never
// touched; an unknown or another tenant's grant is a 404, never an oracle.
func TestMCPCallWithoutAuthorityIsRefusedBeforeDialing(t *testing.T) {
	f := newMCPFixture(t, p07Tokens())
	callBody := func(grantID string) map[string]any {
		return map[string]any{
			"server": "crm", "tool": "lookup", "version": "1.0.0",
			"grantId": grantID, "args": map[string]any{"lead": map[string]any{"id": "lead-42"}},
		}
	}

	// Unknown grant id → 404, nothing dialed.
	status, body := workflowRequest(t, f.app, "POST", "/api/mcp/call", "token-a", callBody("grant-missing"))
	if status != fiber.StatusNotFound {
		t.Fatalf("unknown grant: expected 404, got %d (%v)", status, body)
	}

	// A revoked grant exists but no longer authorizes → the Gate says 403.
	revoked := f.issueGrant(t, "user-a", "crm", "lookup", "1.0.0")
	if err := f.surface.Grants.Revoke(mcp.Identity{Tenant: "user-a", User: "user-a"}, revoked.ID, "user-a", mcpTestNow()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	status, body = workflowRequest(t, f.app, "POST", "/api/mcp/call", "token-a", callBody(revoked.ID))
	if status != fiber.StatusForbidden {
		t.Fatalf("revoked grant: expected 403, got %d (%v)", status, body)
	}

	// Another tenant's grant is invisible: 404 from user-a's session.
	foreign := f.issueGrant(t, "user-b", "crm", "lookup", "1.0.0")
	status, body = workflowRequest(t, f.app, "POST", "/api/mcp/call", "token-a", callBody(foreign.ID))
	if status != fiber.StatusNotFound {
		t.Fatalf("another tenant's grant: expected 404, got %d (%v)", status, body)
	}

	if len(f.transport.calls) != 0 {
		t.Fatalf("a refused call must never dial, got %v", f.transport.calls)
	}
	denied := 0
	for _, event := range f.surface.Audit.Events() {
		if event.Decision == mcp.DecisionDenied {
			denied++
		}
	}
	if denied == 0 {
		t.Fatal("expected at least one denied audit event")
	}
}

// TestMCPDiscoverGrantAndCallRoundTrip is the acceptance criterion end to end:
// an authenticated route discovers, a reviewed policy yields a grant, and the
// granted call executes through the Gate.
func TestMCPDiscoverGrantAndCallRoundTrip(t *testing.T) {
	f := newMCPFixture(t, p07Tokens())

	status, body := workflowRequest(t, f.app, "GET", "/api/mcp/servers/crm/tools", "token-a", nil)
	if status != fiber.StatusOK {
		t.Fatalf("discover: expected 200, got %d (%v)", status, body)
	}
	data, _ := body["data"].(map[string]any)
	tools, _ := data["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("expected two discovered tools, got %v", data["tools"])
	}
	if note, _ := data["note"].(string); note == "" {
		t.Fatal("discovery must state that it grants nothing")
	}

	status, body = workflowRequest(t, f.app, "POST", "/api/mcp/grants", "token-a", map[string]any{
		"server": "crm", "tool": "lookup", "version": "1.0.0",
	})
	if status != fiber.StatusOK {
		t.Fatalf("issue grant: expected 200, got %d (%v)", status, body)
	}
	grant, _ := body["data"].(map[string]any)
	grantID, _ := grant["id"].(string)
	if grantID == "" {
		t.Fatalf("grant response carries no id: %v", body)
	}
	if permissions, _ := grant["permissions"].([]any); len(permissions) != 1 || permissions[0] != "leads:read" {
		t.Fatalf("grant must carry the reviewed permissions, got %v", grant["permissions"])
	}

	status, body = workflowRequest(t, f.app, "POST", "/api/mcp/call", "token-a", map[string]any{
		"server": "crm", "tool": "lookup", "version": "1.0.0", "grantId": grantID,
		"args": map[string]any{"lead": map[string]any{"id": "lead-42"}},
	})
	if status != fiber.StatusOK {
		t.Fatalf("granted call: expected 200, got %d (%v)", status, body)
	}
	result, _ := body["data"].(map[string]any)
	if output, _ := result["output"].(string); output == "" {
		t.Fatalf("call response carries no output: %v", body)
	}
	if echoed, _ := result["grantId"].(string); echoed != grantID {
		t.Fatalf("result must echo the authorizing grant, got %q", echoed)
	}
	if len(f.transport.calls) != 1 {
		t.Fatalf("expected exactly one transport call, got %v", f.transport.calls)
	}
}

func (f *mcpFixture) issueGrant(t *testing.T, tenant, server, tool, version string) mcp.Grant {
	t.Helper()
	policy, found := f.surface.Policies.Get(server, tool)
	if !found {
		t.Fatalf("no policy for %s/%s", server, tool)
	}
	grant, err := f.surface.Grants.Issue(
		mcp.Identity{Tenant: tenant, User: tenant},
		mcp.ServerRef{Server: server, Tool: tool, Version: version},
		policy, tenant, false, time.Time{}, mcpTestNow(),
	)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	return grant
}

// TestMCPGrantIssueIsBoundedByReview: the body names a pinned ref, and the
// policy is looked up — so a caller cannot choose its own permissions, cannot
// grant an unreviewed tool, and cannot float the version.
func TestMCPGrantIssueIsBoundedByReview(t *testing.T) {
	f := newMCPFixture(t, p07Tokens())

	status, body := workflowRequest(t, f.app, "POST", "/api/mcp/grants", "token-a", map[string]any{
		"server": "crm", "tool": "lookup", "version": "1.0.1",
	})
	if status != fiber.StatusConflict {
		t.Fatalf("unpinned version: expected 409, got %d (%v)", status, body)
	}
	if message, _ := body["error"].(string); message == "" {
		t.Fatalf("conflict must name the reviewed version: %v", body)
	}

	status, body = workflowRequest(t, f.app, "POST", "/api/mcp/grants", "token-a", map[string]any{
		"server": "crm", "tool": "unreviewed", "version": "1.0.0",
	})
	if status != fiber.StatusNotFound {
		t.Fatalf("unreviewed tool: expected 404, got %d (%v)", status, body)
	}

	// A body that tries to smuggle authority is ignored: the grant carries the
	// reviewed permissions, not the requested ones.
	status, body = workflowRequest(t, f.app, "POST", "/api/mcp/grants", "token-a", map[string]any{
		"server": "crm", "tool": "lookup", "version": "1.0.0",
		"permissions": []string{"admin:all"}, "sideEffect": "irreversible",
	})
	if status != fiber.StatusOK {
		t.Fatalf("grant with smuggled authority: expected 200, got %d (%v)", status, body)
	}
	grant, _ := body["data"].(map[string]any)
	if permissions, _ := grant["permissions"].([]any); len(permissions) != 1 || permissions[0] != "leads:read" {
		t.Fatalf("the reviewed permissions must win, got %v", grant["permissions"])
	}
	if sideEffect, _ := grant["sideEffect"].(string); sideEffect != string(mcp.SideEffectSafe) {
		t.Fatalf("the reviewed side effect must win, got %v", grant["sideEffect"])
	}
}

// TestMCPCrossTenantGrantsAreInvisible: listing, revoking and calling with
// another tenant's grant all behave as if it did not exist.
func TestMCPCrossTenantGrantsAreInvisible(t *testing.T) {
	f := newMCPFixture(t, p07Tokens())
	foreign := f.issueGrant(t, "user-b", "crm", "lookup", "1.0.0")

	status, body := workflowRequest(t, f.app, "GET", "/api/mcp/grants", "token-a", nil)
	if status != fiber.StatusOK {
		t.Fatalf("list grants: expected 200, got %d (%v)", status, body)
	}
	if list, _ := body["data"].([]any); len(list) != 0 {
		t.Fatalf("another tenant's grant must not be listed: %v", body["data"])
	}

	status, body = workflowRequest(t, f.app, "POST", "/api/mcp/grants/"+foreign.ID+"/revoke", "token-a", nil)
	if status != fiber.StatusNotFound {
		t.Fatalf("revoking another tenant's grant: expected 404, got %d (%v)", status, body)
	}
	if grants := f.surface.Grants.List(mcp.Identity{Tenant: "user-b"}); len(grants) != 1 || grants[0].Revoked {
		t.Fatalf("the foreign grant must stay untouched, got %+v", grants)
	}

	status, body = workflowRequest(t, f.app, "GET", "/api/mcp/grants", "token-b", nil)
	if status != fiber.StatusOK {
		t.Fatalf("list grants as user-b: expected 200, got %d (%v)", status, body)
	}
	if list, _ := body["data"].([]any); len(list) != 1 {
		t.Fatalf("user-b must see exactly its own grant, got %v", body["data"])
	}
}

// TestMCPUnroutableServerIsConflict: a stdio registration is a platform
// configuration the route surface must not dial, so discover/call refuse with
// 409 instead of spawning anything.
func TestMCPUnroutableServerIsConflict(t *testing.T) {
	f := newMCPFixture(t, p07Tokens())
	f.surface.Transport = mcp.SSEOnlyTransport
	grant := f.issueGrant(t, "user-a", "local", "lookup", "1.0.0")

	status, body := workflowRequest(t, f.app, "GET", "/api/mcp/servers/local/tools", "token-a", nil)
	if status != fiber.StatusConflict {
		t.Fatalf("discover on a stdio server: expected 409, got %d (%v)", status, body)
	}
	status, body = workflowRequest(t, f.app, "POST", "/api/mcp/call", "token-a", map[string]any{
		"server": "local", "tool": "lookup", "version": "1.0.0", "grantId": grant.ID,
	})
	if status != fiber.StatusConflict {
		t.Fatalf("call on a stdio server: expected 409, got %d (%v)", status, body)
	}
	if len(f.transport.calls) != 0 {
		t.Fatalf("nothing may be dialed for an unroutable server, got %v", f.transport.calls)
	}
}

// TestMCPSecretPolicyNeedsTheVault: while the generic credential store is
// closed, a tool whose review lists `vault:` refs is refused before dialing —
// never executed without the credential the review assumed.
func TestMCPSecretPolicyNeedsTheVault(t *testing.T) {
	f := newMCPFixture(t, p07Tokens())

	status, body := workflowRequest(t, f.app, "POST", "/api/mcp/grants", "token-a", map[string]any{
		"server": "crm", "tool": "secret", "version": "1.0.0",
	})
	if status != fiber.StatusOK {
		t.Fatalf("issue grant for a secret tool: expected 200, got %d (%v)", status, body)
	}
	grant, _ := body["data"].(map[string]any)
	grantID, _ := grant["id"].(string)

	status, body = workflowRequest(t, f.app, "POST", "/api/mcp/call", "token-a", map[string]any{
		"server": "crm", "tool": "secret", "version": "1.0.0", "grantId": grantID,
	})
	if status != fiber.StatusServiceUnavailable {
		t.Fatalf("secret tool without a vault: expected 503, got %d (%v)", status, body)
	}
	if len(f.transport.calls) != 0 {
		t.Fatalf("a tool needing the vault must not dial, got %v", f.transport.calls)
	}
}

// TestMCPSecretResolvesAtCallTime: a policy that names one `vault:` ref becomes
// the transport's Authorization header, the value never appears in a response,
// and a missing secret is a 409 rather than a call without its credential.
func TestMCPSecretResolvesAtCallTime(t *testing.T) {
	t.Setenv("VAULT_SECRET", "")
	t.Setenv("JWT_SECRET", "")
	f := newMCPFixture(t, p07Tokens())
	store, _ := installTestVault(t, f.hub)
	ctx := context.Background()
	if _, err := store.Put(ctx, "user-a", "crm/token", "s3cret"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	call := func(tool string, grantID string) (int, map[string]any) {
		t.Helper()
		return workflowRequest(t, f.app, "POST", "/api/mcp/call", "token-a", map[string]any{
			"server": "crm", "tool": tool, "version": "1.0.0", "grantId": grantID,
		})
	}

	status, body := workflowRequest(t, f.app, "POST", "/api/mcp/grants", "token-a", map[string]any{
		"server": "crm", "tool": "secret", "version": "1.0.0",
	})
	if status != fiber.StatusOK {
		t.Fatalf("issue grant: expected 200, got %d (%v)", status, body)
	}
	grant, _ := body["data"].(map[string]any)
	grantID, _ := grant["id"].(string)

	status, body = call("secret", grantID)
	if status != fiber.StatusOK {
		t.Fatalf("call with a stored secret: expected 200, got %d (%v)", status, body)
	}
	if got := f.transport.headers["Authorization"]; got != "Bearer s3cret" {
		t.Fatalf("the resolved secret must reach the transport as a bearer header, got %q", got)
	}
	if strings.Contains(fmt.Sprint(body), "s3cret") {
		t.Fatalf("the value must never reach a response: %v", body)
	}

	// A policy whose ref the tenant has not stored is refused, not dialed.
	if err := store.Delete(ctx, "user-a", "crm/token"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	status, body = call("secret", grantID)
	if status != fiber.StatusConflict {
		t.Fatalf("missing secret: expected 409, got %d (%v)", status, body)
	}
	if len(f.transport.calls) != 1 {
		t.Fatalf("a refused call must not dial, got %v", f.transport.calls)
	}

	// Two refs with no header mapping is a refusal, never a guess.
	if err := f.surface.Policies.Put(mcp.ToolPolicy{
		Server: "crm", Tool: "multi", ToolManifestVersion: "1.0.0",
		Permissions: []string{"leads:read"}, SideEffect: mcp.SideEffectSafe,
		Secrets: []string{"vault:a", "vault:b"},
	}); err != nil {
		t.Fatalf("policy Put: %v", err)
	}
	multi := f.issueGrant(t, "user-a", "crm", "multi", "1.0.0")
	if status, body := call("multi", multi.ID); status != fiber.StatusServiceUnavailable {
		t.Fatalf("multi-ref policy: expected 503, got %d (%v)", status, body)
	}
}
