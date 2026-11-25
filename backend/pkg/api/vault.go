package api

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"backend/pkg/engine"
	"backend/pkg/vault"
)

// P1-AGT-004 (D8.4) — the generic credential store over HTTP.
//
// The store is server-encrypted: a value is sealed with AES-256-GCM (the same
// envelope and key derivation as worker/light/tokenVault.ts) before it reaches
// D1, AAD binds it to `<tenant>:<name>`, and **no route ever returns a value**.
// Resolution happens only at MCP call time, inside handleMCPCall.
//
// The SPA's zero-knowledge vault contract (`/api/vault/config`, `/setup`,
// `/reset` with a client KDF) is deliberately not implemented: D8.4 chose the
// server-side model, and answering those routes with a fake KDF config would be
// a lie. They remain unserved.

// vaultStoreFor returns the hub's store, building it once from the deployment
// secret. `VAULT_SECRET` is preferred; `JWT_SECRET` is accepted because the Edge
// vault derives its key from it, so one secret can cover both planes.
func vaultStoreFor(hub *engine.EngineHub) (*vault.Store, error) {
	if store := hub.VaultStore(); store != nil {
		return store, nil
	}
	secret := strings.TrimSpace(os.Getenv("VAULT_SECRET"))
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("JWT_SECRET"))
	}
	sealer, err := vault.NewSealer(secret)
	if err != nil {
		return nil, err
	}
	store, err := vault.NewStore(hub.D1Client(), sealer)
	if err != nil {
		return nil, err
	}
	hub.SetVaultStore(store)
	return store, nil
}

func vaultNotConfigured(c *fiber.Ctx) error {
	return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
		"success": false,
		"error":   "credential store is not configured (set VAULT_SECRET on the control plane)",
	})
}

func vaultTenant(c *fiber.Ctx) (string, bool) {
	identity, ok := CurrentIdentity(c)
	if !ok || strings.TrimSpace(identity.UserID) == "" {
		return "", false
	}
	return strings.TrimSpace(identity.UserID), true
}

// handleVaultStatus implements GET /api/vault/status.
//
// `exists` reports whether the *server-side* store is configured; `mode` says so
// explicitly, because the SPA's client-KDF vault is a different design (D8.4).
func handleVaultStatus(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenant, ok := vaultTenant(c)
		if !ok {
			return mcpUnauthenticated(c)
		}
		store, err := vaultStoreFor(hub)
		if err != nil {
			if errors.Is(err, vault.ErrNotConfigured) {
				return c.JSON(fiber.Map{"success": true, "data": fiber.Map{
					"exists": false, "configured": false, "mode": "server-encrypted",
				}})
			}
			return vaultNotConfigured(c)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		secrets, err := store.List(ctx, tenant)
		if err != nil {
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"success": false, "error": "credential store is unavailable"})
		}
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{
			"exists":     true,
			"configured": true,
			"mode":       "server-encrypted",
			"algorithm":  "aes-256-gcm",
			"itemCount":  len(secrets),
		}})
	}
}

// handleVaultListSecrets implements GET /api/vault/secrets (metadata only).
func handleVaultListSecrets(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenant, ok := vaultTenant(c)
		if !ok {
			return mcpUnauthenticated(c)
		}
		store, err := vaultStoreFor(hub)
		if err != nil {
			return vaultNotConfigured(c)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		secrets, err := store.List(ctx, tenant)
		if err != nil {
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"success": false, "error": "credential store is unavailable"})
		}
		items := make([]fiber.Map, 0, len(secrets))
		for _, secret := range secrets {
			items = append(items, fiber.Map{
				"name":      secret.Name,
				"ref":       vault.RefPrefix + secret.Name,
				"createdAt": secret.CreatedAt,
				"updatedAt": secret.UpdatedAt,
			})
		}
		return c.JSON(fiber.Map{"success": true, "data": items})
	}
}

// vaultPutRequest is the POST /api/vault/secrets body.
type vaultPutRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// handleVaultPutSecret implements POST /api/vault/secrets. The response is the
// metadata; the value is never echoed.
func handleVaultPutSecret(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenant, ok := vaultTenant(c)
		if !ok {
			return mcpUnauthenticated(c)
		}
		var req vaultPutRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "invalid JSON body"})
		}
		name := strings.TrimSpace(req.Name)
		if name == "" || strings.TrimSpace(req.Value) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "name and value are required"})
		}
		store, err := vaultStoreFor(hub)
		if err != nil {
			return vaultNotConfigured(c)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		secret, err := store.Put(ctx, tenant, name, req.Value)
		if err != nil {
			if errors.Is(err, vault.ErrInvalidName) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": err.Error()})
			}
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"success": false, "error": "credential store is unavailable"})
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": fiber.Map{
			"name": secret.Name, "ref": vault.RefPrefix + secret.Name,
			"createdAt": secret.CreatedAt, "updatedAt": secret.UpdatedAt,
		}})
	}
}

// handleVaultDeleteSecret implements DELETE /api/vault/secrets/:name.
//
// A name containing `/` cannot ride the path parameter reliably, so `?name=`
// wins when present. Unknown names are a success: the end state is what the
// caller asked for, and confirming absence would be an enumeration oracle.
func handleVaultDeleteSecret(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenant, ok := vaultTenant(c)
		if !ok {
			return mcpUnauthenticated(c)
		}
		name := strings.TrimSpace(c.Params("name"))
		if query := strings.TrimSpace(c.Query("name")); query != "" {
			name = query
		}
		if name == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "name is required"})
		}
		store, err := vaultStoreFor(hub)
		if err != nil {
			return vaultNotConfigured(c)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := store.Delete(ctx, tenant, name); err != nil {
			if errors.Is(err, vault.ErrInvalidName) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": err.Error()})
			}
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"success": false, "error": "credential store is unavailable"})
		}
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"deleted": true, "name": name}})
	}
}
