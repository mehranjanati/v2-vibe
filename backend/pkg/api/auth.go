package api

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/gofiber/fiber/v2"

	"backend/pkg/cloudflare"
	"backend/pkg/engine"
)

// P0.3 — control-plane auth boundary.
//
// The Go control plane used to serve every mutating route anonymously: Edge
// auth (the light Worker) does not protect Go, so any caller could address any
// project/workflow. This file adds the boundary the rest of P0 depends on:
//
//   - the caller must present a session token (Authorization: Bearer,
//     X-Session-Token, or the Edge `session` cookie);
//   - the token is resolved to a user id by a SessionVerifier;
//   - failures are explicit: 401 for a missing/unknown token, 503 when the
//     session store is not configured or unreachable (fail closed — never a
//     silent allow);
//   - the resolved Identity is attached to the request for downstream
//     ownership scoping (P0.4/P0.7) via CurrentIdentity.
//
// Provisional mechanism: the Edge (light Worker) stays the identity authority
// and the control plane verifies its tokens against the VibecoderStore KV
// namespace (session:token:<token> -> userId, worker/light/lightApp.ts). This
// contracts the split-brain identity instead of inventing a unification —
// the P0.9 decision record owns the final choice (docs/DEV_CHECKLIST.md P0.9).
//
// Enforcement is opt-in: when no SessionVerifier is attached to the hub the
// guards installed by RegisterRoutes degrade to a no-op, so this change does
// not alter a deployment that has not turned the boundary on.

// Identity is the authenticated caller attached to a request by
// RequireSession. Token is the raw session token — never log it.
type Identity struct {
	UserID string
	Token  string
	Source string
}

// identityLocals is the c.Locals key carrying the resolved Identity.
const identityLocals = "auth.identity"

// sessionCookieName is the light Worker's SESSION_COOKIE
// (worker/light/lightApp.ts). It is `access_token`, not `session`: the control
// plane used to look for `session`, a cookie the Edge never sets, so the
// documented cookie transport was dead weight and its comment was wrong
// (P0-SEC-009).
//
// The cookie is normally NOT usable from the split deployment, because it is
// scoped to the Worker origin (Render serves the control plane from another
// host). It works only when both planes are served from one origin (a proxied or
// self-hosted single-domain deployment), which is why the transport exists.
const sessionCookieName = "access_token"

// sessionTokenFromRequest extracts a session token and the source it came
// from. Priority: Authorization: Bearer, X-Session-Token, then the session
// cookie.
func sessionTokenFromRequest(c *fiber.Ctx) (token, source string) {
	if auth := strings.TrimSpace(c.Get(fiber.HeaderAuthorization)); auth != "" {
		if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
			if bearer := strings.TrimSpace(auth[7:]); bearer != "" {
				return bearer, "authorization"
			}
		}
	}
	if header := strings.TrimSpace(c.Get("X-Session-Token")); header != "" {
		return header, "header"
	}
	if cookie := strings.TrimSpace(c.Cookies(sessionCookieName)); cookie != "" {
		return cookie, "cookie"
	}
	return "", ""
}

// CurrentIdentity returns the Identity RequireSession attached to the request.
func CurrentIdentity(c *fiber.Ctx) (Identity, bool) {
	id, ok := c.Locals(identityLocals).(Identity)
	return id, ok
}

// RequireSession authenticates the request before the wrapped handler runs.
// It is safe to install with a nil verifier: a nil verifier is treated as an
// unavailable session store, so the boundary fails closed (503) rather than
// allowing the request. Callers that want the "boundary off" behaviour must
// not install the guard at all (see sessionGuard in routes.go).
func RequireSession(verifier engine.SessionVerifier) fiber.Handler {
	return func(c *fiber.Ctx) error {
		token, source := sessionTokenFromRequest(c)
		if token == "" {
			return unauthenticated(c)
		}
		if verifier == nil {
			return sessionStoreUnavailable(c)
		}

		userID, err := verifier.VerifyUserID(context.Background(), token)
		if err != nil {
			// Configuration/outage → 503 so a broken store can never be
			// mistaken for a valid anonymous session.
			return sessionStoreUnavailable(c)
		}
		if userID == "" {
			return unauthenticated(c)
		}

		// P0-SEC-009: cookie authentication is the one transport a browser
		// attaches automatically, so it is the one that needs a CSRF check.
		// The header transports cannot be set by a cross-site page, but a
		// cookie can — therefore a cookie-authenticated request must come from
		// an allowed Origin (or send no Origin at all, which a browser cannot
		// do for a cross-site request). Same allow-list the CORS/WebSocket
		// layers use.
		if source == "cookie" && !originAllowed(c.Get(fiber.HeaderOrigin)) {
			log.Printf("[api] refused cookie-authenticated request from origin %q", c.Get(fiber.HeaderOrigin))
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"success": false,
				"error":   "origin not allowed",
			})
		}

		c.Locals(identityLocals, Identity{UserID: userID, Token: token, Source: source})
		return c.Next()
	}
}

// ResourceOwnerLookup resolves the owning user id of the resource addressed by
// the request. found=false means the resource does not exist or carries no
// recorded owner (→ 404). A non-nil error means the ownership store is
// unreachable; the guard fails closed with 503 rather than treating an outage
// as "unowned".
type ResourceOwnerLookup func(c *fiber.Ctx) (owner string, found bool, err error)

// RequireOwner scopes a route to the caller's own resources. ownerOf reports
// the owner id of the resource addressed by the request (false when the
// resource does not exist or carries no owner). It must run after
// RequireSession.
//
//   - no identity attached      → 401 (guard wiring error, fail closed);
//   - owner unknown             → 404 (unknown/unowned resource; never leak
//     another tenant's existence via 403);
//   - owner != caller           → 403.
//
// It is the boolean adapter over RequireResourceOwner; the P0.4 room/VFS/deploy
// routes install the error-aware RequireResourceOwner directly so an
// unreachable ownership store fails closed.
func RequireOwner(ownerOf func(*fiber.Ctx) (string, bool)) fiber.Handler {
	return RequireResourceOwner(func(c *fiber.Ctx) (string, bool, error) {
		owner, found := ownerOf(c)
		return owner, found, nil
	})
}

// RequireResourceOwner scopes a route to the caller's own resources using an
// error-aware lookup. It carries the same 401/404/403 contract as RequireOwner
// and additionally answers 503 when the lookup reports a store outage — so a
// broken ownership store can never be mistaken for an unowned resource. This is
// the ownership model of P0.4 (rooms/VFS/deploys) and the reusable primitive
// P0.7 uses for workflow tenant scope.
func RequireResourceOwner(lookup ResourceOwnerLookup) fiber.Handler {
	return func(c *fiber.Ctx) error {
		identity, ok := CurrentIdentity(c)
		if !ok || identity.UserID == "" {
			return unauthenticated(c)
		}
		if lookup == nil {
			return sessionStoreUnavailable(c)
		}
		ownerID, found, err := lookup(c)
		if err != nil {
			return sessionStoreUnavailable(c)
		}
		if !found || ownerID == "" {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"success": false,
				"error":   "resource not found",
			})
		}
		if ownerID != identity.UserID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"success": false,
				"error":   "forbidden",
			})
		}
		return c.Next()
	}
}

func unauthenticated(c *fiber.Ctx) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
		"success": false,
		"error":   "authentication required",
	})
}

func sessionStoreUnavailable(c *fiber.Ctx) error {
	return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
		"success": false,
		"error":   "session store unavailable",
	})
}

// EdgeKVVerifier resolves session tokens against the Edge (light Worker)
// sessions held in the VibecoderStore KV namespace, so the control plane can
// authenticate callers without owning the identity store.
type EdgeKVVerifier struct {
	kv          *cloudflare.KVClient
	namespaceID string
}

// NewEdgeKVVerifier returns a verifier backed by the given KV namespace.
func NewEdgeKVVerifier(kv *cloudflare.KVClient, namespaceID string) *EdgeKVVerifier {
	return &EdgeKVVerifier{kv: kv, namespaceID: namespaceID}
}

// VerifyUserID reads `session:token:<token>` from the Edge KV namespace. A
// missing key is an unknown session (("", nil)); an unconfigured or failing
// store returns ErrSessionStoreUnavailable so the guard answers 503.
func (v *EdgeKVVerifier) VerifyUserID(ctx context.Context, token string) (string, error) {
	if v == nil || v.kv == nil || v.namespaceID == "" {
		return "", engine.ErrSessionStoreUnavailable
	}
	userID, err := v.kv.Get(ctx, v.namespaceID, "session:token:"+token)
	if errors.Is(err, cloudflare.ErrKVKeyNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(userID), nil
}

// UnavailableSessionVerifier always reports an unavailable store. Wiring it
// means "the boundary is on, but no identity store is configured" — guarded
// routes then fail closed with 503 instead of silently allowing callers.
type UnavailableSessionVerifier struct{}

// NewUnavailableSessionVerifier returns a fail-closed SessionVerifier.
func NewUnavailableSessionVerifier() UnavailableSessionVerifier {
	return UnavailableSessionVerifier{}
}

// VerifyUserID implements engine.SessionVerifier.
func (UnavailableSessionVerifier) VerifyUserID(context.Context, string) (string, error) {
	return "", engine.ErrSessionStoreUnavailable
}

// StaticSessionVerifier maps tokens to user ids. Used by tests and by local
// development where no Edge KV namespace is reachable.
type StaticSessionVerifier map[string]string

// NewStaticSessionVerifier returns a verifier over the given token -> userID
// map.
func NewStaticSessionVerifier(tokens map[string]string) StaticSessionVerifier {
	return StaticSessionVerifier(tokens)
}

// VerifyUserID implements engine.SessionVerifier.
func (v StaticSessionVerifier) VerifyUserID(_ context.Context, token string) (string, error) {
	if v == nil {
		return "", nil
	}
	return v[token], nil
}

// P0.5 — WebSocket upgrade authorization.
//
// Browsers cannot set custom headers on a WebSocket handshake, so the session
// token is carried on the URL as the `ticket` (or `token`) query parameter; the
// Authorization: Bearer / X-Session-Token / `session` cookie transports that
// sessionTokenFromRequest already understands are kept as fallbacks for
// programmatic and same-origin clients.

// wsTokenFromRequest extracts what a WebSocket upgrade presents to authenticate.
//
// P0-SEC-008 split the credential by channel, because the two leak differently:
//
//   - the `ticket` query parameter must be a SHORT-LIVED, SINGLE-USE ticket
//     (engine.WSTicketTTL) — never a session token. A query string is recorded by
//     proxies and CDNs, so only a narrow, already-expiring credential may travel
//     there. The legacy `token` query alias was removed for the same reason: it
//     accepted the long-lived session token in the URL.
//   - the header transports (Authorization: Bearer, X-Session-Token, the session
//     cookie) still accept the session token: headers are not logged in URLs.
//
// The returned source is "query" for the ticket and the transport name
// otherwise, so the caller can pick the matching verifier.
func wsTokenFromRequest(c *fiber.Ctx, tickets engine.WSTicketRedeemer) (identity Identity, reject error) {
	if q := strings.TrimSpace(c.Query("ticket")); q != "" {
		if tickets == nil {
			return Identity{}, sessionStoreUnavailable(c)
		}
		userID, chatID, ok, err := tickets.RedeemWSTicket(q)
		if err != nil {
			return Identity{}, sessionStoreUnavailable(c)
		}
		if !ok || userID == "" {
			// Unknown, expired or already-redeemed ticket.
			return Identity{}, unauthenticated(c)
		}
		// A ticket authorizes exactly one room, so a leaked ticket cannot be
		// replayed against a different chat id.
		if chatID != "" && chatID != c.Params("id") {
			return Identity{}, unauthenticated(c)
		}
		return Identity{UserID: userID, Source: "query"}, nil
	}
	token, source := sessionTokenFromRequest(c)
	if token == "" {
		return Identity{}, unauthenticated(c)
	}
	return Identity{Token: token, Source: source}, nil
}

// RequireWebSocketSession authenticates a WebSocket upgrade request before the
// upgrader (websocket.New) runs. A nil verifier and a store outage fail closed
// with 503; a missing/unknown credential is 401 — so a rejected upgrade never
// reaches the room. On success it attaches the resolved Identity
// (CurrentIdentity), binding the socket to the same caller as the HTTP routes.
//
// Query credentials are redeemed as single-use tickets (P0-SEC-008); header
// credentials are verified as session tokens.
func RequireWebSocketSession(verifier engine.SessionVerifier, tickets engine.WSTicketRedeemer) fiber.Handler {
	return func(c *fiber.Ctx) error {
		identity, rejectErr := wsTokenFromRequest(c, tickets)
		if rejectErr != nil {
			return rejectErr
		}
		if identity.Source == "query" {
			// Already resolved by the ticket store.
			c.Locals(identityLocals, identity)
			return c.Next()
		}
		if verifier == nil {
			return sessionStoreUnavailable(c)
		}
		userID, err := verifier.VerifyUserID(context.Background(), identity.Token)
		if err != nil {
			return sessionStoreUnavailable(c)
		}
		if userID == "" {
			return unauthenticated(c)
		}
		// P0-SEC-009: same CSRF rule as RequireSession — a browser attaches the
		// cookie automatically, so a cookie-authenticated upgrade must come from
		// a trusted Origin.
		if identity.Source == "cookie" && !originAllowed(c.Get(fiber.HeaderOrigin)) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"success": false,
				"error":   "origin not allowed",
			})
		}
		identity.UserID = userID
		c.Locals(identityLocals, identity)
		return c.Next()
	}
}
