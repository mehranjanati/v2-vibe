package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"backend/pkg/cloudflare"
	"backend/pkg/engine"
)

// registerRequest is the POST /api/auth/register body.
type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// loginRequest is the POST /api/auth/login body.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// userRow mirrors a read from the D1 `users` table.
type userRow struct {
	ID           string
	Email        string
	Username     string
	DisplayName  string
	AvatarURL    string
	Bio          string
	Provider     string
	EmailVerif   bool
	PasswordHash string
}

func strField(row map[string]interface{}, key string) string {
	v, ok := row[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// sessionResponse mirrors the frontend's SessionResponse shape.
func sessionResponse(u userRow, sessionID string) fiber.Map {
	return fiber.Map{
		"user": fiber.Map{
			"id":            u.ID,
			"email":         u.Email,
			"displayName":   u.DisplayName,
			"avatarUrl":     u.AvatarURL,
			"bio":           u.Bio,
			"emailVerified": u.EmailVerif,
			"provider":      u.Provider,
		},
		"sessionId": sessionID,
		"expiresAt": time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339),
	}
}

// lookUpUserByEmail fetches a users row by email from D1.
func lookUpUserByEmail(ctx context.Context, d1 *cloudflare.D1Client, email string) (*userRow, error) {
	rows, err := d1.Query(ctx,
		"SELECT id, email, username, display_name, avatar_url, bio, provider, email_verified, password_hash FROM users WHERE email = ? LIMIT 1",
		email,
	)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	row := rows[0]
	return &userRow{
		ID:           strField(row, "id"),
		Email:        strField(row, "email"),
		Username:     strField(row, "username"),
		DisplayName:  strField(row, "display_name"),
		AvatarURL:    strField(row, "avatar_url"),
		Bio:          strField(row, "bio"),
		Provider:     strField(row, "provider"),
		EmailVerif:   row["email_verified"] == float64(1),
		PasswordHash: strField(row, "password_hash"),
	}, nil
}

// handleRegisterD1 implements POST /api/auth/register backed by Cloudflare D1.
func handleRegisterD1(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		d1 := hub.D1Client()
		if d1 == nil {
			return c.Status(503).JSON(fiber.Map{"success": false, "message": "D1 database not configured"})
		}
		var req registerRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "invalid body"})
		}
		if req.Email == "" || req.Password == "" {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "email and password required"})
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		existing, err := lookUpUserByEmail(ctx, d1, req.Email)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "database error"})
		}
		if existing != nil {
			return c.Status(409).JSON(fiber.Map{"success": false, "message": "email already registered"})
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "hash error"})
		}

		now := time.Now()
		id := uuid.NewString()
		display := req.Name
		if display == "" {
			display = req.Email
		}
		// Inline the bcrypt hash in the SQL string. The D1 REST API Base64-encodes
		// bound params that contain special characters (e.g. '$' in a bcrypt hash),
		// which would corrupt the stored value. bcrypt hashes only contain
		// [A-Za-z0-9./$] so inlining is safe (no single quotes).
		_, err = d1.Exec(ctx,
			`INSERT INTO users (id, email, username, display_name, avatar_url, bio, provider, provider_id, email_verified, password_hash, is_active, created_at, updated_at)
			 VALUES ('`+id+`', '`+req.Email+`', NULL, '`+display+`', NULL, NULL, 'email', '`+id+`', 1, '`+string(hash)+`', 1, '`+now.Format(time.RFC3339)+`', '`+now.Format(time.RFC3339)+`')`,
		)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "insert failed: " + err.Error()})
		}

		return c.JSON(fiber.Map{
			"success": true,
			"data":    sessionResponse(userRow{ID: id, Email: req.Email, DisplayName: display, Provider: "email", EmailVerif: false}, uuid.NewString()),
		})
	}
}

// handleLoginD1 implements POST /api/auth/login backed by Cloudflare D1.
func handleLoginD1(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		d1 := hub.D1Client()
		if d1 == nil {
			return c.Status(503).JSON(fiber.Map{"success": false, "message": "D1 database not configured"})
		}
		var req loginRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "invalid body"})
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		u, err := lookUpUserByEmail(ctx, d1, req.Email)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "database error"})
		}
		if u == nil || u.PasswordHash == "" {
			return c.Status(401).JSON(fiber.Map{"success": false, "message": "invalid credentials"})
		}
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
			return c.Status(401).JSON(fiber.Map{"success": false, "message": "invalid credentials"})
		}

		return c.JSON(fiber.Map{
			"success": true,
			"data":    sessionResponse(*u, uuid.NewString()),
		})
	}
}
