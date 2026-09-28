// Package handler wires the stateless Eino agent engine to WebSocket
// clients. Per connection:
//
//   - inbound "user_suggestion" / "agent_prompt" messages trigger an agent
//     step: load the session checkpoint from Redis, run the Eino graph in
//     streaming mode, forward token & tool events to the client, and
//     persist the updated checkpoint back to Redis on completion.
//   - the server keeps no session state in RAM, so reconnects (or a
//     different server instance) resume with full context from Redis.
package handler

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"

	"backend/pkg/agent"
	"backend/pkg/agent/tools"
	"backend/pkg/store"
)

// agentStreamFrame is the JSON envelope pushed to the client for each
// streamed event. `type` values: "token", "tool_call", "tool_result",
// "done", "error".
type agentStreamFrame struct {
	Type       string `json:"type"`
	Content    string `json:"content,omitempty"`
	ToolName   string `json:"toolName,omitempty"`
	ToolCallID string `json:"toolCallId,omitempty"`
	ToolArgs   string `json:"toolArgs,omitempty"`
	ToolResult string `json:"toolResult,omitempty"`
	IsFinal    bool   `json:"isFinal,omitempty"`
	Error      string `json:"error,omitempty"`
}

// inboundMessage is the client -> server prompt envelope.
type inboundMessage struct {
	Type    string `json:"type"` // "user_suggestion" | "agent_prompt"
	Message string `json:"message"`
}

// AgentWSHandler owns the shared, stateless dependencies (engine +
// Redis checkpoint store) and serves one WebSocket connection per call.
type AgentWSHandler struct {
	engine     *agent.Engine
	checkpoint *store.RedisCheckpointStore
}

// NewAgentWSHandler builds the handler.
func NewAgentWSHandler(engine *agent.Engine, checkpoint *store.RedisCheckpointStore) *AgentWSHandler {
	return &AgentWSHandler{engine: engine, checkpoint: checkpoint}
}

// Register mounts the WebSocket route: GET /ws-agent/:id
func (h *AgentWSHandler) Register(app *fiber.App) {
	app.Get("/ws-agent/:id", websocket.New(func(conn *websocket.Conn) {
		h.Handle(conn)
	}))
}

// Handle serves a single agent WebSocket connection until the client
// disconnects. Each prompt message runs one stateless agent step.
func (h *AgentWSHandler) Handle(conn *websocket.Conn) {
	sessionID := conn.Params("id")
	if sessionID == "" {
		conn.WriteMessage(websocket.CloseMessage, []byte("missing session id"))
		return
	}
	log.Printf("[agent-ws:%s] connected", sessionID)
	defer log.Printf("[agent-ws:%s] disconnected", sessionID)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return // client closed or connection dropped
		}

		var msg inboundMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			h.send(conn, agentStreamFrame{Type: "error", Error: "invalid JSON message"})
			continue
		}
		if msg.Message == "" {
			h.send(conn, agentStreamFrame{Type: "error", Error: "empty message"})
			continue
		}

		// Run the step synchronously per prompt; events are forwarded
		// inline as they stream. One turn at a time keeps ordering and
		// checkpoint consistency simple — the frontend sends a prompt and
		// consumes frames until "done".
		h.runStep(conn, sessionID, msg.Message)
	}
}

// runStep performs the full stateless round-trip for one user prompt:
// load checkpoint -> run Eino graph streaming -> forward events ->
// save checkpoint.
func (h *AgentWSHandler) runStep(conn *websocket.Conn, sessionID, prompt string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// (a) Fetch the latest state from Redis — nothing is cached in RAM.
	history, err := h.checkpoint.LoadCheckpoint(ctx, sessionID)
	if err != nil {
		log.Printf("[agent-ws:%s] load checkpoint: %v", sessionID, err)
		h.send(conn, agentStreamFrame{Type: "error", Error: "failed to load session state"})
		return
	}

	// Tag tool-call contexts with the session ID so the VFS tools know
	// which chat's virtual file system to modify.
	toolCtx := tools.WithVFSContext(ctx, sessionID)

	// (b) + (c) Execute the Eino graph in streaming mode, forwarding
	// chunks directly to the client as they arrive.
	updated, err := h.engine.RunStep(toolCtx, prompt, history, func(ev agent.StreamEvent) {
		h.send(conn, agentStreamFrame{
			Type:       ev.Type,
			Content:    ev.Content,
			ToolName:   ev.ToolName,
			ToolCallID: ev.ToolCallID,
			ToolArgs:   ev.ToolArgs,
			ToolResult: ev.ToolResult,
			IsFinal:    ev.IsFinal,
		})
	})
	if err != nil {
		log.Printf("[agent-ws:%s] run step: %v", sessionID, err)
		h.send(conn, agentStreamFrame{Type: "error", Error: err.Error()})
		return
	}

	// (d) Persist the final state back to Redis upon step completion.
	if err := h.checkpoint.SaveCheckpoint(ctx, sessionID, updated); err != nil {
		log.Printf("[agent-ws:%s] save checkpoint: %v", sessionID, err)
		h.send(conn, agentStreamFrame{Type: "error", Error: "failed to persist session state"})
	}
}

// send marshals and pushes one frame, ignoring write errors (the read
// loop will notice a dead connection).
func (h *AgentWSHandler) send(conn *websocket.Conn, frame agentStreamFrame) {
	b, err := json.Marshal(frame)
	if err != nil {
		return
	}
	_ = conn.WriteMessage(websocket.TextMessage, b)
}
