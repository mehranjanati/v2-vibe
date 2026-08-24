package api

import (
	"context"
	"database/sql"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"

	"backend/pkg/engine"
)

// userRowPG mirrors a read from the Postgres `users` table.
type userRowPG struct {
	ID           string
	Email        string
	Username     sql.NullString
	DisplayName  string
	AvatarURL    sql.NullString
	Bio          sql.NullString
	Provider     string
	EmailVerif   bool
	PasswordHash sql.NullString
}

// sessionResponsePG mirrors the frontend's SessionResponse shape.
func sessionResponsePG(u userRowPG, sessionID string) fiber.Map {
	avatar := ""
	bio := ""
	if u.AvatarURL.Valid {
		avatar = u.AvatarURL.String
	}
	if u.Bio.Valid {
		bio = u.Bio.String
	}
	return fiber.Map{
		"user": fiber.Map{
			"id":            u.ID,
			"email":         u.Email,
			"displayName":   u.DisplayName,
			"avatarUrl":     avatar,
			"bio":           bio,
			"emailVerified": u.EmailVerif,
			"provider":      u.Provider,
		},
		"sessionId": sessionID,
		"expiresAt": time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339),
	}
}

// pgDSN returns the DSN for the auth Postgres DB from the hub config.
func pgDSN(hub *engine.EngineHub) string {
	host := hub.Cfg("PG_HOST", "localhost")
	port := hub.Cfg("PG_PORT", "5433")
	user := hub.Cfg("PG_USER", "postgres")
	pass := hub.Cfg("PG_PASSWORD", "password")
	db := hub.Cfg("PG_DATABASE", "chatwoot_dev")
	sslmode := hub.Cfg("PG_SSLMODE", "disable")
	return "host=" + host + " port=" + port + " user=" + user + " password=" + pass + " dbname=" + db + " sslmode=" + sslmode
}

// openPG connects (and pings) to the auth Postgres.
func openPG(hub *engine.EngineHub) (*sql.DB, error) {
	db, err := sql.Open("pgx", pgDSN(hub))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// lookUpUserByEmailPG fetches a user by email from Postgres.
func lookUpUserByEmailPG(ctx context.Context, db *sql.DB, email string) (*userRowPG, error) {
	row := db.QueryRowContext(ctx,
		"SELECT id, email, username, display_name, avatar_url, bio, provider, email_verified, password_hash FROM app_users WHERE email = $1 LIMIT 1",
		email,
	)
	u := &userRowPG{}
	err := row.Scan(&u.ID, &u.Email, &u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio, &u.Provider, &u.EmailVerif, &u.PasswordHash)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// handleRegisterPG implements POST /api/auth/register backed by Postgres.
func handleRegisterPG(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		db, err := openPG(hub)
		if err != nil {
			return c.Status(503).JSON(fiber.Map{"success": false, "message": "auth database unavailable"})
		}
		defer db.Close()

		var req registerRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "invalid body"})
		}
		if req.Email == "" || req.Password == "" {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "email and password required"})
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		existing, err := lookUpUserByEmailPG(ctx, db, req.Email)
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
		_, err = db.ExecContext(ctx,
			`INSERT INTO app_users (id, email, username, display_name, avatar_url, bio, provider, provider_id, email_verified, password_hash, is_active, created_at, updated_at)
			 VALUES ($1, $2, NULL, $3, NULL, NULL, 'email', $4, false, $5, true, $6, $6)`,
			id, req.Email, display, id, string(hash), now,
		)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "insert failed: " + err.Error()})
		}

		return c.JSON(fiber.Map{
			"success": true,
			"data": sessionResponsePG(userRowPG{
				ID:          id,
				Email:       req.Email,
				DisplayName: display,
				Provider:    "email",
			}, uuid.NewString()),
		})
	}
}

// handleLoginPG implements POST /api/auth/login backed by Postgres.
func handleLoginPG(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		db, err := openPG(hub)
		if err != nil {
			return c.Status(503).JSON(fiber.Map{"success": false, "message": "auth database unavailable"})
		}
		defer db.Close()

		var req loginRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "invalid body"})
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		u, err := lookUpUserByEmailPG(ctx, db, req.Email)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "database error"})
		}
		if u == nil || !u.PasswordHash.Valid || u.PasswordHash.String == "" {
			return c.Status(401).JSON(fiber.Map{"success": false, "message": "invalid credentials"})
		}
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash.String), []byte(req.Password)) != nil {
			return c.Status(401).JSON(fiber.Map{"success": false, "message": "invalid credentials"})
		}

		return c.JSON(fiber.Map{
			"success": true,
			"data":    sessionResponsePG(*u, uuid.NewString()),
		})
	}
}
