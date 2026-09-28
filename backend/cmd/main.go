package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/gofiber/fiber/v2"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"backend/pkg/agent"
	"backend/pkg/agent/tools"
	"backend/pkg/api"
	"backend/pkg/cloudflare"
	"backend/pkg/engine"
	agenthandler "backend/pkg/handler"
	"backend/pkg/llm"
	"backend/pkg/skills"
	"backend/pkg/store"
)

func main() {
	// Load environment variables from the .env files (all optional; a missing
	// file is fine). godotenv never overrides variables that are already set,
	// so precedence is: real process env > backend/.env > repo-root .env.
	// The repo-root .env is the single source of truth for Cloudflare
	// credentials (kept fresh by `bun run d1:token`); backend/.env only holds
	// backend-specific overrides (Redis URL, PORT, model knobs).
	envFiles := []string{".env", "../.env", "backend/.env"}
	loadedEnv := make([]string, 0, len(envFiles))
	for _, path := range envFiles {
		if err := godotenv.Load(path); err == nil {
			loadedEnv = append(loadedEnv, path)
		}
	}
	if len(loadedEnv) == 0 {
		log.Println("No .env file found, using environment variables")
	} else {
		log.Printf("Loaded env from %s", strings.Join(loadedEnv, ", "))
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
	llmCfg := llm.NewConfigFromEnv()
	llmClient := llm.NewClient(llmCfg)

	// ---- Skills / per-role model registry (dual-model pipeline) ----
	// Loads the role system prompts from the skills dir (01_planner.md,
	// 02_coder.md) and resolves each role's model + generation parameters.
	// A missing or broken skills dir surfaces here as a clear startup
	// error; the pipeline stage that consumes the prompts (R3) will
	// hard-require them.
	skillReg := skills.NewRegistry()
	skillPrompts, skillErr := skillReg.Load()
	if skillErr != nil {
		log.Printf("WARNING: skill prompts unavailable: %v", skillErr)
	} else {
		log.Printf("skill prompts loaded from %q (planner: %d chars, coder: %d chars)",
			skillReg.SkillsDir(),
			len(skillPrompts[skills.RolePlanner]),
			len(skillPrompts[skills.RoleCoder]))
	}

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
	// Workflows: the Cloudflare Workflow name (wrangler.v2.jsonc [[workflows]],
	// the VibeWorkflow runtime) that POST /api/workflows/trigger starts.
	hub.SetWorkflowName(hub.Cfg("WORKFLOWS_NAME", "vibesdk-v2-workflows"))

	// R3: rooms plan with the structured ExecutionPlan contract when the
	// planner skill (01_planner.md) loaded; otherwise the legacy prose
	// planner stays active.
	if skillErr == nil {
		hub.SetPlannerPrompt(skillPrompts[skills.RolePlanner])
		hub.SetTeamPrompts(skillPrompts[skills.RoleCoordinator], skillPrompts[skills.RoleCoder], skillPrompts[skills.RoleReviewer])
	}

	// ---- Stateless Eino agent engine (AI Gateway / Workers AI) ----
	// The engine holds no session state; conversation checkpoints live
	// entirely in Redis via the RedisCheckpointStore below.
	agentCheckpoint := store.NewRedisCheckpointStore(rdb)
	agentEngine, err := agent.NewEngine(ctx, agent.EngineConfig{
		BaseURL:     llmCfg.GatewayURL,
		APIKey:      llmCfg.APIKey,
		Model:       llmCfg.Model,
		Instruction: defaultAgentInstruction,
		Tools:       agentTools(rdb),
	})
	if err != nil {
		log.Printf("WARNING: agent engine disabled: %v", err)
	}

	// P3 unification: rooms route plan/generation/chat through the engine
	// when available; the raw llm.Client stays as fallback only.
	hub.SetEngine(agentEngine)

	// ---- Fiber app ----
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	app := fiber.New(fiber.Config{
		AppName: "VibeSDK Go Backend",
	})

	api.RegisterRoutes(app, hub)

	// ---- Stateless agent WebSocket route: /ws-agent/:id ----
	if agentEngine != nil {
		agenthandler.NewAgentWSHandler(agentEngine, agentCheckpoint).Register(app)
		log.Println("agent WebSocket route registered at /ws-agent/:id")
	}

	log.Printf("VibeSDK Go backend listening on :%s", port)
	if err := app.Listen(":" + port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// defaultAgentInstruction is the system prompt for the stateless Eino
// ReAct agent.
const defaultAgentInstruction = `You are Vibe, an agent that helps users build and manage web applications.
You can:
- read, create, modify and delete files in the project's virtual file system (vfs_* tools)
- call configured REST API endpoints on the user's behalf (rest_api tool)
Think step by step, use tools when needed, and keep answers concise.
When building files, always write complete, working file contents via vfs_write.`

// agentTools assembles the dynamic Eino tools for the agent engine:
// OpenAPI-driven REST execution and the Redis-backed VFS tool set.
func agentTools(rdb *redis.Client) []tool.BaseTool {
	vfsStore := store.NewRedisCheckpointStore(rdb)
	toolSet, err := tools.NewVFSTools(vfsStore)
	if err != nil {
		log.Printf("WARNING: vfs tools unavailable: %v", err)
	}

	var all []tool.BaseTool
	// Wrap every tool with the JSON-repair middleware (B8): slightly
	// malformed model-generated arguments get one repair+retry attempt
	// instead of failing the whole tool call.
	for _, t := range toolSet {
		if invokable, ok := t.(tool.InvokableTool); ok {
			all = append(all, tools.NewJSONRepairTool(invokable))
		} else {
			all = append(all, t)
		}
	}

	// REST tool: configure operations from env (AI_TOOLS_BASE_URL plus a
	// comma-separated spec "METHOD:PATH:SUMMARY" list) when provided.
	if base := os.Getenv("AI_TOOLS_BASE_URL"); base != "" {
		restTool, err := tools.NewRESTTool(tools.RESTToolConfig{
			BaseURL:   base,
			Endpoints: restEndpointsFromEnv(),
			DefaultHeaders: map[string]string{
				"Authorization": "Bearer " + os.Getenv("AI_TOOLS_API_KEY"),
			},
		})
		if err != nil {
			log.Printf("WARNING: rest tool unavailable: %v", err)
		} else {
			all = append(all, tools.NewJSONRepairTool(restTool))
		}
	}
	return all
}

// restEndpointsFromEnv parses REST_TOOL_ENDPOINTS, a comma-separated list
// of "METHOD:PATH:SUMMARY" specs, e.g.
// "GET:/users:List users,POST:/users:Create a user".
func restEndpointsFromEnv() []tools.RESTEndpoint {
	spec := os.Getenv("REST_TOOL_ENDPOINTS")
	if spec == "" {
		return nil
	}
	var eps []tools.RESTEndpoint
	for _, part := range strings.Split(spec, ",") {
		fields := strings.SplitN(strings.TrimSpace(part), ":", 3)
		if len(fields) < 2 {
			continue
		}
		summary := ""
		if len(fields) == 3 {
			summary = fields[2]
		}
		eps = append(eps, tools.RESTEndpoint{
			OperationID: fields[0] + "_" + strings.ReplaceAll(strings.Trim(fields[1], "/"), "/", "_"),
			Method:      fields[0],
			Path:        fields[1],
			Summary:     summary,
		})
	}
	return eps
}
