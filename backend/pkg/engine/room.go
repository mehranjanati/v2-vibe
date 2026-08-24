package engine

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/redis/go-redis/v9"

	"backend/pkg/cloudflare"
	"backend/pkg/llm"
	"backend/pkg/models"
)

// Client wraps a single WebSocket connection bound to a ProjectRoom.
type Client struct {
	conn *websocket.Conn
	send chan []byte
}

// NewClient creates a Client bound to the given WebSocket connection.
func NewClient(conn *websocket.Conn) *Client {
	return &Client{
		conn: conn,
		send: make(chan []byte, 256),
	}
}

// ProjectRoom is a Goroutine-based actor representing one chat/project.
// It owns:
//   - the set of connected clients (register/unregister/broadcast channels)
//   - an in-memory VFS map (guarded by sync.RWMutex)
//   - the Redis client used to load/persist the VFS under 'vfs:{chatId}'
//
// The Run loop is the single owner of the clients map; all mutations go
// through the register/unregister channels. The VFS map is guarded by its
// own RWMutex so it can be read/written from any Goroutine safely.
type ProjectRoom struct {
	chatID string
	rdb    *redis.Client
	llm    *llm.Client
	cf     *cloudflare.Client

	// Actor channels.
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	stop       chan struct{}

	// clients is owned by the Run loop only.
	clients map[*Client]bool

	// vfs is the in-memory Virtual File System for this room.
	vfsMu sync.RWMutex
	vfs   map[string]*models.FileEntry

	// done signals that the Run loop has exited.
	done chan struct{}
}

// ChatID returns the room's chat identifier.
func (r *ProjectRoom) ChatID() string {
	return r.chatID
}

// SetCloudflareClient attaches the Cloudflare API client used for Pages
// deployments. Safe to call before any deployment is triggered.
func (r *ProjectRoom) SetCloudflareClient(cf *cloudflare.Client) {
	r.cf = cf
}

// NewProjectRoom creates a room for chatID. Callers must start it with
// go room.Run().
func NewProjectRoom(chatID string, rdb *redis.Client, llmClient *llm.Client) *ProjectRoom {
	return &ProjectRoom{
		chatID:     chatID,
		rdb:        rdb,
		llm:        llmClient,
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 256),
		stop:       make(chan struct{}),
		clients:    make(map[*Client]bool),
		vfs:        make(map[string]*models.FileEntry),
		done:       make(chan struct{}),
	}
}

// Run is the actor's main loop. It owns the clients map and dispatches
// broadcast messages to every connected client.
func (r *ProjectRoom) Run() {
	defer close(r.done)

	// Load initial VFS state from Redis on startup.
	if err := r.loadVFS(); err != nil {
		log.Printf("[room:%s] failed to load VFS from Redis: %v", r.chatID, err)
	}

	for {
		select {
		case client := <-r.register:
			r.clients[client] = true
			log.Printf("[room:%s] client registered (%d connected)", r.chatID, len(r.clients))

		case client := <-r.unregister:
			if _, ok := r.clients[client]; ok {
				delete(r.clients, client)
				close(client.send)
				log.Printf("[room:%s] client unregistered (%d connected)", r.chatID, len(r.clients))
			}

		case msg := <-r.broadcast:
			for client := range r.clients {
				select {
				case client.send <- msg:
				default:
					// Slow client: drop the message rather than block the loop.
					log.Printf("[room:%s] dropping message for slow client", r.chatID)
				}
			}

		case <-r.stop:
			log.Printf("[room:%s] stopping", r.chatID)
			for client := range r.clients {
				close(client.send)
			}
			r.clients = make(map[*Client]bool)
			return
		}
	}
}

// Stop gracefully shuts down the room's Run loop.
func (r *ProjectRoom) Stop() {
	select {
	case <-r.done:
		return
	default:
	}
	close(r.stop)
	<-r.done
}

// RegisterClient adds a client to the room and starts its write pump.
func (r *ProjectRoom) RegisterClient(c *Client) {
	r.register <- c
	go r.writePump(c)
}

// UnregisterClient removes a client from the room.
func (r *ProjectRoom) UnregisterClient(c *Client) {
	r.unregister <- c
}

// BroadcastMessage marshals v to JSON and fans it out to all clients.
func (r *ProjectRoom) BroadcastMessage(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("[room:%s] broadcast marshal error: %v", r.chatID, err)
		return
	}
	r.broadcast <- data
}

// writePump drains the client's send channel into the WebSocket.
func (r *ProjectRoom) writePump(c *Client) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				// Channel closed by the Run loop on unregister/stop.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				log.Printf("[room:%s] write error: %v", r.chatID, err)
				return
			}
		case <-ticker.C:
			// Ping to keep the connection alive.
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ---------- VFS (Redis-backed) ----------

// vfsKey returns the Redis Hash key for this room's VFS.
func (r *ProjectRoom) vfsKey() string {
	return "vfs:" + r.chatID
}

// loadVFS hydrates the in-memory VFS from the Redis Hash 'vfs:{chatId}'.
// Each hash field is a file path; the value is the file contents.
func (r *ProjectRoom) loadVFS() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entries, err := r.rdb.HGetAll(ctx, r.vfsKey()).Result()
	if err != nil {
		return err
	}

	r.vfsMu.Lock()
	defer r.vfsMu.Unlock()
	for path, contents := range entries {
		r.vfs[path] = &models.FileEntry{
			FilePath:     path,
			FileContents: contents,
		}
	}
	log.Printf("[room:%s] loaded %d files from Redis VFS", r.chatID, len(entries))
	return nil
}

// GetVFS returns a snapshot of the in-memory VFS.
func (r *ProjectRoom) GetVFS() map[string]*models.FileEntry {
	r.vfsMu.RLock()
	defer r.vfsMu.RUnlock()

	snapshot := make(map[string]*models.FileEntry, len(r.vfs))
	for path, entry := range r.vfs {
		cp := *entry
		snapshot[path] = &cp
	}
	return snapshot
}

// UpsertFile writes a file to the in-memory VFS and persists it to Redis.
func (r *ProjectRoom) UpsertFile(path, contents string) {
	r.vfsMu.Lock()
	r.vfs[path] = &models.FileEntry{FilePath: path, FileContents: contents}
	r.vfsMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.rdb.HSet(ctx, r.vfsKey(), path, contents).Err(); err != nil {
		log.Printf("[room:%s] failed to persist file %s: %v", r.chatID, path, err)
	}
}

// DeleteFile removes a file from the in-memory VFS and Redis.
func (r *ProjectRoom) DeleteFile(path string) {
	r.vfsMu.Lock()
	delete(r.vfs, path)
	r.vfsMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.rdb.HDel(ctx, r.vfsKey(), path).Err(); err != nil {
		log.Printf("[room:%s] failed to delete file %s: %v", r.chatID, path, err)
	}
}

// BuildAgentState assembles the AgentState payload the React frontend
// expects on connect (cf_agent_state / agent_connected).
func (r *ProjectRoom) BuildAgentState() *models.AgentState {
	return &models.AgentState{
		BehaviorType:      "phasic",
		ProjectType:       "app",
		GeneratedFilesMap: r.GetVFS(),
	}
}

// ---------- LLM generation ----------

// StartGeneration kicks off an asynchronous code-generation run for the
// given prompt. It spawns a background Goroutine that streams LLM tokens
// and broadcasts them as file_chunk_generated events, then persists the
// resulting files to the VFS. It never blocks the WS read/write loops.
func (r *ProjectRoom) StartGeneration(prompt string) {
	if r.llm == nil {
		log.Printf("[room:%s] no LLM client configured; skipping generation", r.chatID)
		r.BroadcastMessage(models.ErrorEvent{
			Type:  "error",
			Error: "LLM client is not configured (set AI_GATEWAY_URL)",
		})
		return
	}

	go r.runGeneration(prompt)
}

// runGeneration is the background task that drives the LLM stream.
func (r *ProjectRoom) runGeneration(prompt string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Announce generation start.
	r.BroadcastMessage(models.GenerationStarted{
		Type:       "generation_started",
		Message:    "Generation started",
		TotalFiles: len(r.GetVFS()),
	})

	// Build the chat request. The system prompt instructs the model to
	// emit fenced code blocks with file paths.
	req := llm.ChatRequest{
		Messages: []llm.ChatMessage{
			{
				Role: "system",
				Content: "You are a code generation engine. Generate a complete " +
					"project for the user's request. Output each file as a fenced " +
					"code block whose info string is the file path, e.g. " +
					"```src/index.ts\\n...\\n```. Do not include any other text.",
			},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.7,
		MaxTokens:   4096,
	}

	ch, err := r.llm.StreamChat(ctx, req)
	if err != nil {
		log.Printf("[room:%s] LLM stream error: %v", r.chatID, err)
		r.BroadcastMessage(models.ErrorEvent{
			Type:  "error",
			Error: "Failed to start LLM stream: " + err.Error(),
		})
		return
	}

	// Accumulate the full output for post-processing.
	var output strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			log.Printf("[room:%s] LLM stream error: %v", r.chatID, chunk.Err)
			r.BroadcastMessage(models.ErrorEvent{
				Type:  "error",
				Error: "LLM stream error: " + chunk.Err.Error(),
			})
			return
		}
		if chunk.Done {
			break
		}
		if chunk.Content == "" {
			continue
		}

		output.WriteString(chunk.Content)

		// Rule 2: broadcast each token as a file_chunk_generated event.
		r.BroadcastMessage(models.FileChunkGenerated{
			Type:     "file_chunk_generated",
			FilePath: "generated-output.txt",
			Chunk:    chunk.Content,
		})
	}

	// Step 3: parse code blocks, persist to VFS, broadcast completion.
	r.finalizeGeneration(output.String())
}

// finalizeGeneration parses fenced code blocks from the LLM output,
// persists each file to the in-memory VFS and Redis, and broadcasts
// file_generated + cf_agent_state events.
func (r *ProjectRoom) finalizeGeneration(output string) {
	blocks := llm.ParseFileBlocks(output)
	if len(blocks) == 0 {
		log.Printf("[room:%s] no file blocks parsed from LLM output", r.chatID)
		r.BroadcastMessage(models.GenerationComplete{
			Type: "generation_complete",
		})
		return
	}

	for _, block := range blocks {
		// Thread-safe VFS mutation + Redis persistence.
		r.UpsertFile(block.Path, block.Content)

		// Broadcast the completed file.
		r.BroadcastMessage(models.FileGenerated{
			Type: "file_generated",
			File: &models.FileEntry{
				FilePath:     block.Path,
				FileContents: block.Content,
			},
		})
	}

	// Broadcast the updated agent state so the frontend can re-sync.
	r.BroadcastMessage(models.CFAgentStateEvent{
		Type:  "cf_agent_state",
		State: r.BuildAgentState(),
	})

	r.BroadcastMessage(models.GenerationComplete{
		Type: "generation_complete",
	})
}
