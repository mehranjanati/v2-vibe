package api

import (
	"encoding/json"
	"log"
	"os"
	"strings"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/google/uuid"

	"backend/pkg/engine"
	"backend/pkg/github"
	"backend/pkg/models"
)

// RegisterRoutes wires up CORS, logging, the REST session endpoints, and
// the WebSocket route onto the Fiber app.
func RegisterRoutes(app *fiber.App, hub *engine.EngineHub) {
	// Logging middleware.
	app.Use(logger.New())

	// CORS: allow the SPA origins for local dev and the Cloudflare Pages
	// hosts, and expose the headers the React frontend relies on.
	// AllowCredentials must be true because the frontend sends requests
	// with credentials:'include'; Fiber requires explicit (non-wildcard)
	// origins when credentials are enabled.
	app.Use(cors.New(cors.Config{
		AllowOrigins: strings.Join([]string{
			"http://localhost:5173",
			"http://localhost:4173",
			"https://vibeos-dda.pages.dev",
			"https://production.vibeos-dda.pages.dev",
			"https://*.vibeos-dda.pages.dev",
			"https://vibesdk-v2.mehranjannati.workers.dev",
			"https://vibesdk-v2.apjkala25.workers.dev",
		}, ","),
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Session-Token, X-CSRF-Token",
		ExposeHeaders:    "Content-Length, Content-Type",
		AllowCredentials: true,
		MaxAge:           86400,
	}))

	// Health check.
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "rooms": hub.RoomCount()})
	})

	// Dev-only debug endpoints (transcript replay). See debug_routes.go.
	registerDebugRoutes(app, hub)

	// ---- Placeholder endpoints so the React SPA can load ----
	// The Go control plane implements the core agent/session/deploy/ws
	// flows. These stubs return empty/default payloads so the frontend UI
	// (auth check, capabilities, apps feed, status) renders without errors
	// while the full Worker backend is not deployed.
	app.Get("/api/auth/csrf-token", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"success": true,
			"data":    fiber.Map{"token": "dev-csrf-token", "expiresIn": 7200},
		})
	})
	app.Get("/api/auth/profile", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true, "data": nil})
	})
	app.Get("/api/auth/providers", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true, "data": []string{}})
	})
	app.Post("/api/auth/register", handleRegisterPG(hub))
	app.Post("/api/auth/login", handleLoginPG(hub))
	app.Post("/api/auth/verify-email", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"user": fiber.Map{
					"id":            "dev-user-id",
					"email":         "dev@example.com",
					"displayName":   "Dev User",
					"emailVerified": true,
				},
				"sessionId": "dev-session-id",
				"expiresAt": nil,
			},
		})
	})
	app.Get("/api/limits/usage", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"cloudflareConnectEnabled": false,
				"config": fiber.Map{
					"unlimited": true,
				},
				"usage": fiber.Map{
					"promptsUsed": 0,
					"appsCreated": 0,
					"totalToken":  0,
					"inputToken":  0,
					"outputToken": 0,
				},
				"limitCheck": fiber.Map{
					"withinLimits": true,
				},
				"hasUserToken":                false,
				"hasCloudflareConfigured":     false,
				"aiGatewayConnected":          false,
				"aiGatewayEnabled":            false,
				"aiGatewayPreferenceExplicit": false,
			},
		})
	})
	app.Post("/api/agent", func(c *fiber.Ctx) error {
		agentID := uuid.NewString()
		line, err := json.Marshal(fiber.Map{
			"agentId":      agentID,
			"websocketUrl": buildWebSocketURL(c, agentID),
			"behaviorType": "phasic",
			"projectType":  "app",
			"template": fiber.Map{
				"files": []fiber.Map{},
			},
		})
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "marshal failed")
		}
		// NDJSON - one JSON object per line (the frontend parses via ndjsonStream)
		payload := append(append([]byte{}, line...), '\n')
		c.Set("Content-Type", "application/x-ndjson")
		return c.Status(200).Send(payload)
	})
	app.Get("/api/status", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"status": "ok"}})
	})
	app.Get("/api/capabilities", func(c *fiber.Ctx) error {
		features := []fiber.Map{
			{
				"id":          "app",
				"name":        "Application",
				"description": "Full-stack web applications",
				"enabled":     true,
				"capabilities": fiber.Map{
					"hasPreview":             true,
					"hasLiveReload":          true,
					"requiresSandbox":        false,
					"requiresWebSocket":      true,
					"supportedViews":         []string{"editor", "preview", "docs", "blueprint"},
					"defaultView":            "editor",
					"supportedExports":       []string{"github"},
					"hasCustomHeaderActions": true,
					"hasCustomSidebar":       false,
					"hasCustomFileFilter":    false,
					"behaviorType":           "think",
				},
			},
			{
				"id":          "presentation",
				"name":        "Presentation",
				"description": "Interactive slide presentations",
				"enabled":     true,
				"capabilities": fiber.Map{
					"hasPreview":             true,
					"hasLiveReload":          true,
					"requiresSandbox":        true,
					"requiresWebSocket":      true,
					"supportedViews":         []string{"editor", "preview", "docs"},
					"defaultView":            "preview",
					"supportedExports":       []string{"github", "pdf", "pptx", "googleslides"},
					"hasCustomHeaderActions": true,
					"hasCustomSidebar":       true,
					"hasCustomFileFilter":    true,
					"behaviorType":           "agentic",
				},
			},
			{
				"id":          "general",
				"name":        "General",
				"description": "General-purpose code generation",
				"enabled":     true,
				"capabilities": fiber.Map{
					"hasPreview":             false,
					"hasLiveReload":          false,
					"requiresSandbox":        false,
					"requiresWebSocket":      true,
					"supportedViews":         []string{"editor", "docs"},
					"defaultView":            "editor",
					"supportedExports":       []string{"github"},
					"hasCustomHeaderActions": false,
					"hasCustomSidebar":       false,
					"hasCustomFileFilter":    false,
					"behaviorType":           "agentic",
				},
			},
		}
		return c.JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"features":          features,
				"version":           "dev",
				"userAccountDeploy": false,
			},
		})
	})
	// Paginated apps: frontend reads lastPage.pagination.hasMore in
	// getNextPageParam, so the stub must include the pagination object.
	app.Get("/api/apps/public", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"apps": []string{},
				"pagination": fiber.Map{
					"limit":   20,
					"offset":  0,
					"total":   0,
					"hasMore": false,
				},
			},
		})
	})
	app.Get("/api/user/apps", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"apps": []string{},
				"pagination": fiber.Map{
					"limit":   20,
					"offset":  0,
					"total":   0,
					"hasMore": false,
				},
			},
		})
	})
	app.Get("/api/apps", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"apps": []string{}}})
	})
	app.Get("/api/apps/recent", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"apps": []string{}}})
	})
	app.Get("/api/apps/favorites", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"apps": []string{}}})
	})

	// App details view - PUBLIC for /app/:id frontend route. The Go
	// control plane has no app persistence, so we return a "not found"
	// shaped empty detail object (matching AppDetailsData) so the SPA can
	// render without a 404. Ownership fields (userId, deploymentId) are
	// omitted; owner-only preview/star/favorite sub-routes are handled
	// separately where the frontend expects a real worker-backed app.
	app.Get("/api/apps/:id", func(c *fiber.Ctx) error {
		appID := c.Params("id")
		return c.JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"id":                         appID,
				"title":                      "",
				"description":                "",
				"iconUrl":                    "",
				"framework":                  "",
				"visibility":                 "private",
				"status":                     "draft",
				"isFeatured":                 false,
				"screenshotUrl":              "",
				"originalPrompt":             "",
				"createdAt":                  "",
				"updatedAt":                  "",
				"lastDeployedAt":             nil,
				"githubRepositoryUrl":        "",
				"githubRepositoryVisibility": "",
				"userName":                   "",
				"userAvatar":                 "",
				"starCount":                  0,
				"viewCount":                  0,
				"forkCount":                  0,
				"likeCount":                  0,
				"userStarred":                false,
				"userFavorited":              false,
				"cloudflareUrl":              nil,
				"previewUrl":                 nil,
				"behaviorType":               "phasic",
				"user": fiber.Map{
					"id":          "",
					"displayName": "",
					"avatarUrl":   nil,
				},
				"agentSummary": nil,
			},
		})
	})

	// REST: create a new agent session. Returns the WebSocket URL the
	// React frontend connects to.
	app.Post("/api/agent/session", handleCreateSession(hub))

	// REST: connect to an existing agent session.
	app.Get("/api/agent/:id/connect", handleConnectAgent(hub))

	// REST: deploy a project's VFS to Cloudflare Pages.
	app.Post("/api/projects/:id/deploy", handleDeployProject(hub))

	// REST: return a project's VFS files as a flat map of path -> contents.
	// Used by the light Worker (Edge) to push generated files to GitHub.
	app.Get("/api/projects/:id/files", handleGetProjectFiles(hub))

	// REST: export a project's VFS to a GitHub repository (push files).
	app.Post("/api/projects/:id/github-export", handleGitHubExport(hub))

	// REST: trigger a workflow run (validates the DAG, records the run,
	// starts the Cloudflare Workflow execution) and inspect a workflow's
	// runs + per-step logs. See workflows.go.
	app.Post("/api/workflows/trigger", handleWorkflowTrigger(hub))
	app.Get("/api/workflows/:workflowId", handleWorkflowGet(hub))

	// WebSocket: real-time agent channel.
	app.Get("/ws/:id", websocket.New(handleWebSocket(hub)))
}

// buildWebSocketURL derives the ws:// URL the frontend should connect to,
// preferring the incoming request host so it works behind a proxy.
func buildWebSocketURL(c *fiber.Ctx, chatID string) string {
	host := c.Get("X-Forwarded-Host")
	if host == "" {
		host = c.Hostname()
	}
	if host == "" {
		host = os.Getenv("WS_HOST")
	}
	if host == "" {
		host = "localhost:8080"
	}
	return "ws://" + host + "/ws/" + chatID
}

// handleCreateSession implements POST /api/agent/session.
func handleCreateSession(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		chatID := uuid.NewString()

		// Create (and start) the room actor for this session.
		hub.GetOrCreateRoom(chatID)

		log.Printf("[api] created agent session %s", chatID)
		return c.JSON(fiber.Map{
			"websocketUrl": buildWebSocketURL(c, chatID),
			"agentId":      chatID,
			"behaviorType": "phasic",
			"projectType":  "app",
		})
	}
}

// handleConnectAgent implements GET /api/agent/:id/connect.
func handleConnectAgent(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		chatID := c.Params("id")
		if chatID == "" {
			return fiber.NewError(fiber.StatusBadRequest, "missing agent id")
		}

		// Ensure the room exists so the WebSocket handshake has a target.
		hub.GetOrCreateRoom(chatID)

		return c.JSON(fiber.Map{
			"success":      true,
			"websocketUrl": buildWebSocketURL(c, chatID),
			"agentId":      chatID,
		})
	}
}

// handleGetProjectFiles implements GET /api/projects/:id/files. It returns
// the project's VFS as a flat map of file path -> file contents. Callers:
// the light Worker (Edge) pushes generated files to GitHub, and the React
// SPA hydrates the client-side preview when reopening a chat. Read-only:
// it never creates or starts a room (see EngineHub.GetVFSReadOnly).
func handleGetProjectFiles(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		projectID := c.Params("id")
		if projectID == "" {
			return fiber.NewError(fiber.StatusBadRequest, "missing project id")
		}

		files, err := hub.GetVFSReadOnly(projectID)
		if err != nil {
			log.Printf("[api] failed to read VFS for project %s: %v", projectID, err)
			return fiber.NewError(fiber.StatusInternalServerError, "failed to read project files")
		}

		return c.JSON(fiber.Map{
			"success": true,
			"data":    fiber.Map{"files": files},
		})
	}
}

// handleDeployProject implements POST /api/projects/:id/deploy. It
// triggers an asynchronous Cloudflare Pages deployment of the room's VFS
// and returns immediately; real-time status is broadcast over WebSocket.
func handleDeployProject(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		projectID := c.Params("id")
		if projectID == "" {
			return fiber.NewError(fiber.StatusBadRequest, "missing project id")
		}

		room := hub.GetOrCreateRoom(projectID)
		room.DeployRoomVFS(c.Context())

		return c.JSON(fiber.Map{
			"success":   true,
			"message":   "deployment started",
			"projectId": projectID,
		})
	}
}

// handleGitHubExport implements POST /api/projects/:id/github-export.
// It pushes the room's VFS files to a GitHub repository via the Contents
// API, and adds a GitHub Actions workflow that builds + deploys to
// Cloudflare Pages. The GitHub token is read from the request body.
func handleGitHubExport(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		projectID := c.Params("id")
		if projectID == "" {
			return fiber.NewError(fiber.StatusBadRequest, "missing project id")
		}

		var body struct {
			RepositoryName string `json:"repositoryName"`
			Description    string `json:"description"`
			IsPrivate      bool   `json:"isPrivate"`
			Token          string `json:"token"`
		}
		if err := json.Unmarshal(c.Body(), &body); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
		}
		if body.Token == "" {
			return fiber.NewError(fiber.StatusBadRequest, "GitHub token is required")
		}
		if body.RepositoryName == "" {
			return fiber.NewError(fiber.StatusBadRequest, "repositoryName is required")
		}

		room := hub.GetOrCreateRoom(projectID)
		vfs := room.GetVFS()
		if len(vfs) == 0 {
			return c.JSON(fiber.Map{
				"success": false,
				"error":   "VFS is empty; nothing to export",
			})
		}

		gh := github.NewClient(body.Token)

		// Create the repository (or reuse if it already exists).
		repoURL, err := gh.CreateRepository(c.Context(), body.RepositoryName, body.Description, body.IsPrivate)
		if err != nil && err.Error() != "repository already exists" {
			return c.JSON(fiber.Map{
				"success": false,
				"error":   "Failed to create repository: " + err.Error(),
			})
		}
		if repoURL == "" {
			// Repository already exists — assume owner/repo from the name.
			repoURL = "https://github.com/" + body.RepositoryName
		}

		repoInfo, err := github.ExtractRepoInfo(repoURL)
		if err != nil {
			return c.JSON(fiber.Map{
				"success": false,
				"error":   "Invalid repository URL: " + err.Error(),
			})
		}

		// Convert VFS to map[string]string and push (adds workflow).
		files := make(map[string]string, len(vfs))
		for path, entry := range vfs {
			files[path] = entry.FileContents
		}
		if err := gh.PushFiles(c.Context(), repoInfo.Owner, repoInfo.Repo, files); err != nil {
			return c.JSON(fiber.Map{
				"success": false,
				"error":   "Failed to push files: " + err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"success":       true,
			"repositoryUrl": repoURL,
			"message":       "Files pushed to GitHub. GitHub Actions will build and deploy to Cloudflare Pages.",
		})
	}
}

// handleWebSocket implements GET /ws/:id. On upgrade it registers the
// client with the room and immediately sends the connection-lifecycle
// payloads the React frontend expects: cf_agent_state and agent_connected.
func handleWebSocket(hub *engine.EngineHub) func(*websocket.Conn) {
	return func(conn *websocket.Conn) {
		chatID := conn.Params("id")
		if chatID == "" {
			conn.WriteMessage(websocket.CloseMessage, []byte("missing agent id"))
			return
		}

		room := hub.GetOrCreateRoom(chatID)

		client := engine.NewClient(conn)
		room.RegisterClient(client)
		defer room.UnregisterClient(client)

		// --- Connection lifecycle handshake ---
		// Both payloads go to the CONNECTING client only (B2): broadcasting
		// them fans duplicate state into every other open tab.

		// 1. cf_agent_state: current agent state snapshot.
		room.SendClient(client, models.CFAgentStateEvent{
			Type:  "cf_agent_state",
			State: room.BuildAgentState(),
		})

		// 2. agent_connected: full state + template details + preview URL.
		room.SendClient(client, models.AgentConnectedEvent{
			Type:            "agent_connected",
			State:           room.BuildAgentState(),
			TemplateDetails: &models.TemplateDetail{},
		})

		// Read loop: handle inbound client messages.
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				log.Printf("[ws:%s] read error: %v", chatID, err)
				break
			}

			var clientMsg models.ClientMessage
			if err := json.Unmarshal(msg, &clientMsg); err != nil {
				log.Printf("[ws:%s] invalid client message: %v", chatID, err)
				continue
			}

			handleClientMessage(room, client, clientMsg)
		}
	}
}

// handleClientMessage routes inbound client messages to room actions. The
// sending client is threaded through so request-scoped replies (B2:
// get_conversation_state) go back only to the requesting tab.
func handleClientMessage(room *engine.ProjectRoom, client *engine.Client, msg models.ClientMessage) {
	switch msg.Type {
	case "get_conversation_state":
		// The frontend requests state on open; we already pushed it during
		// the handshake, but re-send to satisfy the explicit request.
		room.SendClient(client, models.CFAgentStateEvent{
			Type:  "cf_agent_state",
			State: room.BuildAgentState(),
		})

	case "generate_all":
		// Kick off asynchronous LLM code generation. The prompt is taken
		// from the message body if present. When empty (reconnect resume:
		// the frontend resends generate_all without a message), re-use the
		// room's last stored prompt so the SAME app is regenerated — the
		// generic default below is only for genuinely promptless requests.
		prompt := msg.Message
		if prompt == "" {
			prompt = room.LastPrompt()
		}
		if prompt == "" {
			prompt = "Build a simple web application."
		}
		room.StartGeneration(prompt)

	case "user_suggestion":
		// Conversational chat: stream a plain-markdown reply without
		// generating files or touching shouldBeGenerating.
		prompt := msg.Message
		if prompt == "" {
			log.Printf("[room:%s] user_suggestion with empty message; ignoring", room.ChatID())
			break
		}
		room.StartConversation(prompt)

	case "stop_generation":
		// B9: cancel the in-flight generation. The generation goroutine
		// notices the cancelled ctx and emits generation_cancelled.
		room.CancelGeneration()
		log.Printf("[room:%s] stop_generation handled", room.ChatID())

	case "plan_approved":
		// B7: client approved the proposed build plan.
		room.ApprovePlan()

	case "plan_rejected":
		// B7: client rejected the proposed build plan; generation aborts
		// without touching the VFS.
		room.RejectPlan()

	case "resume_generation", "preview",
		"clear_conversation", "rollback_to_commit",
		"capture_screenshot", "deploy":
		// Recognized but not yet implemented in Phase 2; log for visibility.
		log.Printf("[room:%s] received message type %q (not yet handled)", room.ChatID(), msg.Type)

	default:
		log.Printf("[room:%s] unknown message type %q", room.ChatID(), msg.Type)
	}
}
