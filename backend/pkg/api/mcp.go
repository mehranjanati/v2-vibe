package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"backend/pkg/engine"
	"backend/pkg/mcp"
	"backend/pkg/vault"
)

// P1-AGT-004 (D8.3) — the authenticated MCP surface.
//
// What these routes may do, exactly:
//
//   - discovery of a registered server's tools (metadata only, no grant, no
//     execution);
//   - issuing a grant for a *reviewed* policy — the body names a server/tool/
//     version, never permissions, so a caller cannot widen its own authority;
//   - listing and revoking the caller's grants;
//   - executing a granted tool through the SSE binding.
//
// What they may not do: register a server (platform configuration, P7.6 owns
// persistence), resolve a secret (the generic credential store is still closed
// by D8 — a policy that lists `vault:` refs is refused instead of resolved), or
// spawn a process (`stdio://` registrations are not routable, D8.3).
//
// Tenant scope comes from the session, never from the body: `mcpIdentity` maps
// the authenticated caller to `mcp.Identity{Tenant: userID}` and every store
// lookup is scoped by it.

// mcpSurfaceOr503 returns the hub's surface or writes the 503.
func mcpSurfaceOr503(hub *engine.EngineHub, c *fiber.Ctx) (*mcp.Surface, error) {
	surface := hub.MCPSurface()
	if surface == nil || surface.Gate == nil {
		return nil, c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"success": false,
			"error":   "MCP surface is not configured",
		})
	}
	return surface, nil
}

// mcpIdentity maps the request's session identity to the gate's identity.
// No session (or the boundary off) means no tenant, and the gate rejects
// tenantless calls — the routes never invent one.
func mcpIdentity(c *fiber.Ctx) (mcp.Identity, bool) {
	id, ok := CurrentIdentity(c)
	if !ok || strings.TrimSpace(id.UserID) == "" {
		return mcp.Identity{}, false
	}
	return mcp.Identity{Tenant: id.UserID, User: id.UserID}, true
}

func mcpUnauthenticated(c *fiber.Ctx) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
		"success": false,
		"error":   "authentication required",
	})
}

// mcpTransportView exposes the binding a registration would use, not its full
// endpoint: a caller needs to know whether a server is routable, not where the
// platform dials it from.
func mcpTransportView(endpoint string) string {
	trimmed := strings.TrimSpace(endpoint)
	if i := strings.Index(trimmed, "://"); i > 0 {
		return trimmed[:i]
	}
	return "unknown"
}

// handleMCPListServers implements GET /api/mcp/servers.
func handleMCPListServers(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		surface, err := mcpSurfaceOr503(hub, c)
		if err != nil {
			return err
		}
		if _, ok := mcpIdentity(c); !ok {
			return mcpUnauthenticated(c)
		}

		servers := make([]fiber.Map, 0)
		for _, registration := range surface.Registry.Registrations() {
			servers = append(servers, fiber.Map{
				"name":         registration.Name,
				"transport":    mcpTransportView(registration.Endpoint),
				"tools":        registration.Tools,
				"registeredBy": registration.RegisteredBy,
				"registeredAt": registration.RegisteredAt,
			})
		}
		return c.JSON(fiber.Map{"success": true, "data": servers})
	}
}

// handleMCPDiscover implements GET /api/mcp/servers/:server/tools.
func handleMCPDiscover(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		surface, err := mcpSurfaceOr503(hub, c)
		if err != nil {
			return err
		}
		identity, ok := mcpIdentity(c)
		if !ok {
			return mcpUnauthenticated(c)
		}

		name := strings.TrimSpace(c.Params("server"))
		registration, found := surface.Registry.Get(name)
		if !found {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "error": "mcp server not found"})
		}
		transport, err := surface.Transport(registration, nil)
		if err != nil {
			// A registration the route surface may not dial (stdio) is a
			// conflict, not a transient failure: retrying changes nothing.
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"success": false, "error": err.Error()})
		}

		discovery, err := surface.Gate.Discover(identity, name, transport)
		if err != nil {
			log.Printf("[api] mcp discovery failed for %s: %v", name, err)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"success": false, "error": "mcp discovery failed"})
		}
		// Explicit wire shape: the Go field names are not the API contract, and a
		// caller must not have to know them.
		tools := make([]fiber.Map, 0, len(discovery.Tools))
		for _, tool := range discovery.Tools {
			tools = append(tools, fiber.Map{
				"name":        tool.Name,
				"description": tool.Description,
			})
		}
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{
			"server": discovery.Server,
			"tools":  tools,
			"note":   discovery.Note,
		}})
	}
}

// mcpGrantView is the caller-facing shape of a grant. It carries what the tool
// may do (the reviewed policy snapshot), never a secret.
func mcpGrantView(g mcp.Grant) fiber.Map {
	view := fiber.Map{
		"id":            g.ID,
		"server":        g.Ref.Server,
		"tool":          g.Ref.Tool,
		"version":       g.Ref.Version,
		"permissions":   g.Policy.Permissions,
		"sideEffect":    g.Policy.SideEffect,
		"approvals":     g.Policy.Approvals,
		"grantedBy":     g.GrantedBy,
		"humanApproved": g.HumanApproved,
		"createdAt":     g.CreatedAt,
		"revoked":       g.Revoked,
	}
	if !g.ExpiresAt.IsZero() {
		view["expiresAt"] = g.ExpiresAt
	}
	if g.Revoked {
		view["revokedAt"] = g.RevokedAt
	}
	return view
}

// mcpGrantRequest is the POST /api/mcp/grants body. It names a reviewed policy
// by (server, tool, version); it deliberately cannot carry permissions,
// side-effect or approvals, so a caller cannot widen its own authority.
type mcpGrantRequest struct {
	Server           string `json:"server"`
	Tool             string `json:"tool"`
	Version          string `json:"version"`
	ExpiresInSeconds int    `json:"expiresInSeconds,omitempty"`
}

// handleMCPIssueGrant implements POST /api/mcp/grants.
func handleMCPIssueGrant(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		surface, err := mcpSurfaceOr503(hub, c)
		if err != nil {
			return err
		}
		identity, ok := mcpIdentity(c)
		if !ok {
			return mcpUnauthenticated(c)
		}

		var req mcpGrantRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "invalid JSON body"})
		}
		ref := mcp.ServerRef{
			Server:  strings.TrimSpace(req.Server),
			Tool:    strings.TrimSpace(req.Tool),
			Version: strings.TrimSpace(req.Version),
		}
		if ref.Server == "" || ref.Tool == "" || ref.Version == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"success": false, "error": "server, tool and version are required",
			})
		}
		if !ref.Valid() {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"success": false, "error": "ref must be mcp/<server>/<tool>@<MAJOR.MINOR.PATCH> (a pinned version, no ranges)",
			})
		}
		if req.ExpiresInSeconds < 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "expiresInSeconds cannot be negative"})
		}

		// The policy is looked up, never supplied: review is what authorizes a
		// grant, and a body that could name its own permissions would bypass it.
		policy, found := surface.Policies.Get(ref.Server, ref.Tool)
		if !found {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"success": false, "error": "no reviewed policy for this tool",
			})
		}
		if policy.ToolManifestVersion != ref.Version {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"success": false, "error": "ref must pin the reviewed version " + policy.ToolManifestVersion,
			})
		}

		var expiresAt time.Time
		if req.ExpiresInSeconds > 0 {
			expiresAt = surface.Now().Add(time.Duration(req.ExpiresInSeconds) * time.Second)
		}
		grant, err := surface.Grants.Issue(identity, ref, policy, identity.User, false, expiresAt, surface.Now())
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		return c.JSON(fiber.Map{"success": true, "data": mcpGrantView(grant)})
	}
}

// handleMCPListGrants implements GET /api/mcp/grants (the caller's tenant only).
func handleMCPListGrants(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		surface, err := mcpSurfaceOr503(hub, c)
		if err != nil {
			return err
		}
		identity, ok := mcpIdentity(c)
		if !ok {
			return mcpUnauthenticated(c)
		}
		grants := surface.Grants.List(identity)
		views := make([]fiber.Map, 0, len(grants))
		for _, grant := range grants {
			views = append(views, mcpGrantView(grant))
		}
		return c.JSON(fiber.Map{"success": true, "data": views})
	}
}

// handleMCPRevokeGrant implements POST /api/mcp/grants/:grantId/revoke.
func handleMCPRevokeGrant(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		surface, err := mcpSurfaceOr503(hub, c)
		if err != nil {
			return err
		}
		identity, ok := mcpIdentity(c)
		if !ok {
			return mcpUnauthenticated(c)
		}
		grantID := strings.TrimSpace(c.Params("grantId"))
		if grantID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "grantId is required"})
		}
		if err := surface.Grants.Revoke(identity, grantID, identity.User, surface.Now()); err != nil {
			// Unknown and another tenant's grant are the same answer, exactly
			// like execution: never an enumeration oracle.
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "error": "grant not found"})
		}
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"revoked": true, "id": grantID}})
	}
}

// mcpCallBody is the POST /api/mcp/call body.
type mcpCallBody struct {
	Server      string          `json:"server"`
	Tool        string          `json:"tool"`
	Version     string          `json:"version"`
	GrantID     string          `json:"grantId"`
	Args        json.RawMessage `json:"args,omitempty"`
	ApprovalRef string          `json:"approvalRef,omitempty"`
}

// handleMCPCall implements POST /api/mcp/call.
//
// Order is deliberate: the caller's own grant is resolved first (so another
// tenant's grant is a 404, not an oracle), then a policy that lists `vault:`
// references is refused while the credential store is still closed, and only
// then is a transport built — so every refusal happens before anything is
// dialed. The Gate still re-checks identity, policy drift, approvals and
// idempotency; this handler never authorizes on its own.
func handleMCPCall(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		surface, err := mcpSurfaceOr503(hub, c)
		if err != nil {
			return err
		}
		identity, ok := mcpIdentity(c)
		if !ok {
			return mcpUnauthenticated(c)
		}

		var req mcpCallBody
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "invalid JSON body"})
		}
		ref := mcp.ServerRef{
			Server:  strings.TrimSpace(req.Server),
			Tool:    strings.TrimSpace(req.Tool),
			Version: strings.TrimSpace(req.Version),
		}
		grantID := strings.TrimSpace(req.GrantID)
		if ref.Server == "" || ref.Tool == "" || ref.Version == "" || grantID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"success": false, "error": "server, tool, version and grantId are required",
			})
		}
		if !ref.Valid() {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"success": false, "error": "ref must be mcp/<server>/<tool>@<MAJOR.MINOR.PATCH> (a pinned version, no ranges)",
			})
		}

		registration, found := surface.Registry.Get(ref.Server)
		if !found {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "error": "mcp server not found"})
		}
		grant, found := surface.Grants.Get(identity, grantID)
		if !found {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "error": "grant not found"})
		}

		// D8.4: a policy that names `vault:` refs gets them resolved at call
		// time — never earlier, never into the response. The value becomes the
		// transport's `Authorization: Bearer …` header.
		headers, status, message := mcpSecretHeaders(hub, identity, grant)
		if status != 0 {
			return c.Status(status).JSON(fiber.Map{"success": false, "error": message})
		}

		argsJSON := "{}"
		if len(req.Args) > 0 {
			if !json.Valid(req.Args) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "args is not valid JSON"})
			}
			argsJSON = string(req.Args)
		}

		transport, err := surface.Transport(registration, headers)
		if err != nil {
			// A registration the route surface may not dial (stdio) is a
			// configuration conflict, not a transient failure.
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"success": false, "error": err.Error()})
		}

		result, err := surface.Gate.Call(mcp.CallRequest{
			Identity:    identity,
			Ref:         ref,
			GrantID:     grantID,
			ArgsJSON:    argsJSON,
			ApprovalRef: strings.TrimSpace(req.ApprovalRef),
		}, transport)
		if err != nil {
			var denied *mcp.DeniedError
			if errors.As(err, &denied) {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false, "error": denied.Verdict})
			}
			log.Printf("[api] mcp call failed for %s/%s: %v", ref.Server, ref.Tool, err)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"success": false, "error": "mcp call failed"})
		}
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{
			"output":         result.Output,
			"grantId":        result.GrantID,
			"idempotencyKey": result.IdempotencyKey,
		}})
	}
}

// mcpSecretHeaders resolves a policy's `vault:` refs at call time.
//
// Only one ref is supported today: MCP's HTTP transport carries the credential
// in `Authorization: Bearer <value>`, and a policy naming several refs has no
// header mapping saying where each belongs — refusing is honest, guessing is
// not. The map is built here and handed to the transport; it never reaches a
// response body.
func mcpSecretHeaders(hub *engine.EngineHub, identity mcp.Identity, grant mcp.Grant) (map[string]string, int, string) {
	refs := grant.Policy.Secrets
	if len(refs) == 0 {
		return nil, 0, ""
	}
	if len(refs) > 1 {
		return nil, fiber.StatusServiceUnavailable,
			"this policy names more than one secret and the transport has no per-secret header mapping yet"
	}
	store, err := vaultStoreFor(hub)
	if err != nil {
		return nil, fiber.StatusServiceUnavailable,
			"this tool needs a credential but the credential store is not configured (set VAULT_SECRET)"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	values, err := store.Resolve(ctx, identity.Tenant, refs)
	if err != nil {
		if errors.Is(err, vault.ErrNotFound) {
			return nil, fiber.StatusConflict,
				"the policy references a credential this tenant has not stored: " + refs[0]
		}
		return nil, fiber.StatusServiceUnavailable,
			"the credential store could not resolve the policy reference"
	}
	return map[string]string{"Authorization": "Bearer " + values[refs[0]]}, 0, ""
}
