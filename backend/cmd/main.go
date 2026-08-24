package main

import (
	"context"
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"backend/pkg/api"
	"backend/pkg/cloudflare"
	"backend/pkg/engine"
	"backend/pkg/llm"
)

func main() {
	// Load environment variables from .env (optional; missing file is fine).
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// ---- Redis client ----
	redisAddr := os.Getenv("REDIS_URL")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	// ---- LLM / AI Gateway client ----
	llmClient := llm.NewClient(llm.NewConfigFromEnv())

	// ---- Cloudflare API client (Pages deployments) ----
	cfClient := cloudflare.NewClient(
		os.Getenv("CLOUDFLARE_ACCOUNT_ID"),
		os.Getenv("CLOUDFLARE_API_TOKEN"),
	)
	// ---- Cloudflare D1 client (auth/users + limits persistence) ----
	// Requires the D1 database_id from wrangler.jsonc as D1_DATABASE_ID.
	d1Client := cloudflare.NewD1Client(
		os.Getenv("CLOUDFLARE_ACCOUNT_ID"),
		os.Getenv("CLOUDFLARE_API_TOKEN"),
		os.Getenv("D1_DATABASE_ID"),
	)


	// ---- Redis Vector Index (idx:vfs) ----
	ctx := context.Background()
	if err := engine.EnsureVectorIndex(ctx, rdb); err != nil {
		log.Printf("WARNING: vector index not created: %v (search/RAG will fall back to no context)", err)
	} else {
		log.Println("Redis vector index ensured")
	}

	// ---- Engine hub (actor registry) ----
	hub := engine.NewEngineHub(rdb, llmClient, cfClient)
	hub.SetD1Client(d1Client)

	// ---- Fiber app ----
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	app := fiber.New(fiber.Config{
		AppName: "VibeSDK Go Backend",
	})

	api.RegisterRoutes(app, hub)

	log.Printf("VibeSDK Go backend listening on :%s", port)
	if err := app.Listen(":" + port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
