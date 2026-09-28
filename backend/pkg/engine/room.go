package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/gofiber/contrib/websocket"
	"github.com/redis/go-redis/v9"

	agentplan "backend/agent"
	"backend/pkg/agent"
	"backend/pkg/cloudflare"
	"backend/pkg/llm"
	"backend/pkg/models"
	"backend/pkg/skills"
)

// directMsg is a message destined for exactly ONE client (B2): connection
// handshake state like cf_agent_state / agent_connected must reach only the
// connecting tab, not every connected client.
type directMsg struct {
	client *Client
	data   []byte
}

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
	// d1 is the optional Cloudflare D1 client used to persist validated
	// workflow DAGs (the Redis VFS sync to D1 for the Phase-2 runtime).
	d1 *cloudflare.D1Client
	// eng is the unified eino engine; nil ⇒ legacy llm.Client fallback.
	eng *agent.Engine

	// plannerPrompt is the structured Chain-of-Thought planner system
	// prompt (skills/01_planner.md, decoded by backend/agent). Empty ⇒
	// the room plans with the legacy prose-architect prompt.
	plannerPrompt string

	// teamPrompts carries the multi-agent team role prompts
	// (00_coordinator.md, 02_coder.md, 03_reviewer.md). Empty fields ⇒
	// the room executes with the single-coder dual-model path.
	teamPrompts teamSkillPrompts

	// genStop is reset per generation run; closing it cancels the
	// in-flight generation (client "stop_generation", B9). Guarded by
	// genMu. genStop may be nil before the first run.
	genMu   sync.Mutex
	genStop chan struct{}

	// planApproval carries the client's verdict while the room pauses on
	// a proposed build plan (B7): 1 = approved, 0 = rejected. nil when no
	// approval is pending. Guarded by planMu.
	planMu       sync.Mutex
	planApproval chan int

	// filesWritten counts files written during the current plan-execute
	// run (B5); atomic because tool calls run inside the ADK graph.
	filesWritten atomic.Int64

	// generating mirrors the in-flight generation state so BuildAgentState
	// can expose ShouldBeGenerating (the frontend's reconnect auto-resume
	// flag). Atomic: written by the generation goroutine, read by the
	// handshake/broadcast paths.
	generating atomic.Bool

	// Actor channels.
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	direct     chan directMsg
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

// SetD1Client attaches the Cloudflare D1 client used to persist validated
// workflow DAGs. Safe to call before any generation runs.
func (r *ProjectRoom) SetD1Client(d1 *cloudflare.D1Client) {
	r.d1 = d1
}

// SetPlannerPrompt attaches the structured planner system prompt loaded
// from the skills dir (01_planner.md). Rooms without it keep planning with
// the legacy prose prompt. Call during setup, before any generation runs.
func (r *ProjectRoom) SetPlannerPrompt(prompt string) {
	r.plannerPrompt = prompt
}

// SetTeamPrompts attaches the multi-agent team role prompts (coordinator,
// coder, reviewer). Rooms without them keep the single-coder dual-model
// execution path. Call during setup, before any generation runs.
func (r *ProjectRoom) SetTeamPrompts(prompts teamSkillPrompts) {
	r.teamPrompts = prompts
}

// TeamPrompts returns the room's multi-agent team prompts ("" fields mean
// the team path is disabled for this room).
func (r *ProjectRoom) TeamPrompts() teamSkillPrompts {
	return r.teamPrompts
}

// NewProjectRoom creates a room for chatID. Callers must start it with
// go room.Run(). eng may be nil (legacy raw-client fallback is used).
func NewProjectRoom(chatID string, rdb *redis.Client, llmClient *llm.Client, eng *agent.Engine) *ProjectRoom {
	return &ProjectRoom{
		chatID:     chatID,
		rdb:        rdb,
		llm:        llmClient,
		eng:        eng,
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 256),
		direct:     make(chan directMsg, 256),
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

		case dm := <-r.direct:
			// B2: address one client only (handshake state must not fan
			// out). The client may have unregistered between the send and
			// this loop iteration — its channel is closed then, so skip.
			if _, ok := r.clients[dm.client]; !ok {
				continue
			}
			select {
			case dm.client.send <- dm.data:
			default:
				log.Printf("[room:%s] dropping direct message for slow client", r.chatID)
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
// Safe against goroutine leaks: after Stop() the Run loop is gone, so a
// full broadcast channel must not block the sender forever.
func (r *ProjectRoom) BroadcastMessage(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("[room:%s] broadcast marshal error: %v", r.chatID, err)
		return
	}
	select {
	case r.broadcast <- data:
	case <-r.stop:
	}
}

// SendClient marshals v to JSON and sends it to ONE client only (B2):
// connection-lifecycle state (cf_agent_state, agent_connected) must reach
// the connecting tab, not every connected client. No-op once the room's
// Run loop has stopped.
func (r *ProjectRoom) SendClient(c *Client, v any) {
	if c == nil {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("[room:%s] direct marshal error: %v", r.chatID, err)
		return
	}
	select {
	case r.direct <- directMsg{client: c, data: data}:
	case <-r.stop:
	}
}

// planApprovalTimeout is how long the room waits for the client's plan
// verdict before auto-approving (so a closed tab does not wedge the
// pipeline forever).
const planApprovalTimeout = 10 * time.Minute

// waitForPlanApproval pauses the generation until the client approves
// (ApprovePlan) or rejects (RejectPlan) the proposed plan. Auto-approves
// after planApprovalTimeout. Returns false when the plan was rejected or
// the run was cancelled — the caller must then stop without generating.
func (r *ProjectRoom) waitForPlanApproval(ctx context.Context) bool {
	ch := make(chan int, 1)
	r.planMu.Lock()
	r.planApproval = ch
	r.planMu.Unlock()
	defer func() {
		r.planMu.Lock()
		r.planApproval = nil
		r.planMu.Unlock()
	}()

	timer := time.NewTimer(planApprovalTimeout)
	defer timer.Stop()

	select {
	case verdict := <-ch:
		debugLogEvent(r, "plan_verdict", "approved", verdict == 1)
		return verdict == 1
	case <-ctx.Done():
		return false
	case <-r.stop:
		return false
	case <-timer.C:
		log.Printf("[room:%s] plan approval timeout; auto-approving", r.chatID)
		return true
	}
}

// ApprovePlan resolves a pending plan gate with "proceed" (client
// plan_approved). No-op when no approval is pending.
func (r *ProjectRoom) ApprovePlan() { r.signalPlanVerdict(1) }

// RejectPlan resolves a pending plan gate with "abort" (client
// plan_rejected). No-op when no approval is pending.
func (r *ProjectRoom) RejectPlan() { r.signalPlanVerdict(0) }

// signalPlanVerdict delivers the verdict to a waiting generation, if any.
func (r *ProjectRoom) signalPlanVerdict(verdict int) {
	r.planMu.Lock()
	ch := r.planApproval
	r.planMu.Unlock()
	if ch == nil {
		log.Printf("[room:%s] plan verdict %d ignored (no approval pending)", r.chatID, verdict)
		return
	}
	select {
	case ch <- verdict:
	default:
	}
}

// canPlanExecute reports whether the plan-execute-replan path is usable
// for this room: a configured eino engine whose chat model supports the
// tool-calling interface the planexecute prebuilt requires.
func (r *ProjectRoom) canPlanExecute() bool {
	return r.eng != nil && r.eng.SupportsPlanExecute()
}

// generationContext returns a context that is cancelled when either the
// timeout elapses, the room is stopped, or the client sends
// stop_generation (B9: clean cancel). The returned cancel func must
// always be called; it also releases the stop watcher.
func (r *ProjectRoom) generationContext(timeout time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	// Fresh per-run stop channel so a previous CancelGeneration cannot
	// cancel the next generation.
	r.genMu.Lock()
	r.genStop = make(chan struct{})
	stop := r.genStop
	r.genMu.Unlock()

	release := make(chan struct{})
	go func() {
		select {
		case <-r.stop:
			cancel()
		case <-stop:
			cancel()
		case <-ctx.Done():
		case <-release:
		}
	}()
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			close(release)
			cancel()
		})
	}
}

// CancelGeneration cancels any in-flight generation run (client
// "stop_generation" message). Safe to call at any time, including when
// no generation is running.
func (r *ProjectRoom) CancelGeneration() {
	r.genMu.Lock()
	defer r.genMu.Unlock()
	if r.genStop == nil {
		return
	}
	select {
	case <-r.genStop: // already closed
	default:
		close(r.genStop)
		log.Printf("[room:%s] generation cancel requested", r.chatID)
	}
}

// streamLLM runs ONE assistant turn through the unified eino stack
// (agent.Engine / adk.Runner — P3 unification). When no engine is
// configured it falls back to the legacy raw llm.StreamChat client.
//
// system is the per-turn instruction; msgs are the non-system turns in
// order (the last one is the new user turn, earlier ones are history).
// onDelta receives each streamed text delta. Returns the full assistant
// text and the terminal finish_reason ("" when unavailable, e.g. on the
// engine path).
func (r *ProjectRoom) streamLLM(
	ctx context.Context,
	system string,
	msgs []llm.ChatMessage,
	maxTokens int,
	onDelta func(string),
) (string, string, error) {
	if r.eng != nil {
		hist := make([]*schema.Message, 0, len(msgs))
		for _, m := range msgs[:len(msgs)-1] {
			hist = append(hist, chatToSchema(m))
		}
		var full strings.Builder
		_, err := r.eng.RunStepWith(ctx, system, nil, msgs[len(msgs)-1].Content, hist,
			func(ev agent.StreamEvent) {
				if ev.Type == "token" && !ev.IsFinal && ev.Content != "" {
					full.WriteString(ev.Content)
					onDelta(ev.Content)
				}
			})
		return full.String(), "", err
	}
	return r.streamLLMRaw(ctx, system, msgs, maxTokens, onDelta)
}

// streamLLMRaw runs an assistant turn through the raw OpenAI-compatible
// client directly, bypassing the eino engine. The raw client IGNORES
// malformed chunk content and keeps streaming (unlike eino whose graph
// node fails/ends on such frames). This is the reliable fallback for
// code generation against flaky Workers AI / gateway models.
//
// Connection drops (unexpected EOF from the provider) are handled
// defensively: an attempt that produced NO output is retried (up to 3
// total attempts); a mid-content drop keeps the partial output so the
// generation retry loop can re-emit the unfinished files.
func (r *ProjectRoom) streamLLMRaw(
	ctx context.Context,
	system string,
	msgs []llm.ChatMessage,
	maxTokens int,
	onDelta func(string),
) (string, string, error) {
	const maxAttempts = 3
	var full strings.Builder
	finish := ""

attemptLoop:
	for attempt := 1; ; attempt++ {
		req := llm.ChatRequest{
			Messages:    append([]llm.ChatMessage{{Role: "system", Content: system}}, msgs...),
			Temperature: 0.6,
			MaxTokens:   maxTokens,
		}
		ch, err := r.llm.StreamChat(ctx, req)
		if err != nil {
			return "", "", err
		}
		gotAny := false
		for chunk := range ch {
			if chunk.Err != nil {
				if !llm.IsConnectionLost(chunk.Err) {
					return "", "", chunk.Err
				}
				if !gotAny && attempt < maxAttempts {
					log.Printf("[room:%s] LLM connection lost before any output (attempt %d/%d); retrying",
						r.chatID, attempt, maxAttempts)
					continue attemptLoop
				}
				// Mid-content drop (or retries exhausted): keep the
				// partial output — the generation retry loop re-emits
				// unfinished files with a larger budget.
				log.Printf("[room:%s] LLM connection lost mid-stream after %d bytes; keeping partial output",
					r.chatID, full.Len())
				return full.String(), "connection_lost", nil
			}
			if chunk.Done {
				finish = chunk.FinishReason
				break
			}
			if chunk.Content == "" {
				continue
			}
			gotAny = true
			full.WriteString(chunk.Content)
			onDelta(chunk.Content)
		}
		return full.String(), finish, nil
	}
}

// chatToSchema converts a legacy chat message into an eino schema message.
func chatToSchema(m llm.ChatMessage) *schema.Message {
	if m.Role == "assistant" {
		return schema.AssistantMessage(m.Content, nil)
	}
	return schema.UserMessage(m.Content)
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
	if r.rdb == nil {
		log.Printf("[room:%s] no Redis client; starting with an empty VFS", r.chatID)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entries, err := r.rdb.HGetAll(ctx, r.vfsKey()).Result()
	if err != nil {
		return err
	}

	r.vfsMu.Lock()
	deleted := []string{}
	for path, contents := range entries {
		// B10 cleanup: legacy builds persisted the raw LLM transcript as a
		// pseudo-file; drop it from the VFS and Redis so it never reaches
		// the file tree or preview again.
		if IsLegacyOutputFile(path) {
			deleted = append(deleted, path)
			continue
		}
		r.vfs[path] = &models.FileEntry{
			FilePath:     path,
			FileContents: contents,
		}
	}
	r.vfsMu.Unlock()
	if len(deleted) > 0 {
		if err := r.rdb.HDel(ctx, r.vfsKey(), deleted...).Err(); err != nil {
			log.Printf("[room:%s] failed to delete legacy transcript files: %v", r.chatID, err)
		}
		log.Printf("[room:%s] removed %d legacy generated-output files from VFS", r.chatID, len(deleted))
	}
	r.vfsMu.Lock()
	defer r.vfsMu.Unlock()
	log.Printf("[room:%s] loaded %d files from Redis VFS", r.chatID, len(r.vfs))
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

// UpsertFile writes a file to the in-memory VFS and persists it to Redis
// (skipped when no Redis client is configured).
func (r *ProjectRoom) UpsertFile(path, contents string) {
	r.vfsMu.Lock()
	r.vfs[path] = &models.FileEntry{FilePath: path, FileContents: contents}
	r.vfsMu.Unlock()

	if r.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.rdb.HSet(ctx, r.vfsKey(), path, contents).Err(); err != nil {
		log.Printf("[room:%s] failed to persist file %s: %v", r.chatID, path, err)
	}
}

// DeleteFile removes a file from the in-memory VFS and Redis (Redis delete
// is skipped when no client is configured).
func (r *ProjectRoom) DeleteFile(path string) {
	r.vfsMu.Lock()
	delete(r.vfs, path)
	r.vfsMu.Unlock()

	if r.rdb == nil {
		return
	}
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
		BehaviorType:       "phasic",
		ProjectType:        "app",
		GeneratedFilesMap:  r.GetVFS(),
		Query:              r.LastPrompt(),
		ShouldBeGenerating: r.generating.Load(),
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
			Error: "LLM client is not configured (set AI_GATEWAY_URL or CLOUDFLARE_ACCOUNT_ID)",
		})
		return
	}

	// Remember the prompt so a later empty `generate_all` (reconnect resume)
	// can re-run the SAME request instead of a generic fallback.
	if prompt != "" && r.rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		r.rdb.Set(ctx, "lastprompt:"+r.chatID, prompt, 24*time.Hour)
		cancel()
	}

	go func() {
		r.generating.Store(true)
		defer r.generating.Store(false)
		// A panic here used to kill the whole backend process (taking every
		// WebSocket down with it). Recover, report, and keep the server up.
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[room:%s] generation panic recovered: %v", r.chatID, rec)
				r.BroadcastMessage(models.ErrorEvent{
					Type:  "error",
					Error: "generation failed internally; please retry",
				})
				r.BroadcastMessage(models.GenerationInterrupted{
					Type:   "generation_interrupted",
					Reason: "internal generation error; files generated so far are kept",
				})
			}
		}()
		r.runGeneration(prompt)
	}()
}

// LastPrompt returns the most recent generation prompt for this room
// (empty string if none). Used to resume generation with the original
// request after a reconnect instead of a generic fallback.
func (r *ProjectRoom) LastPrompt() string {
	if r.rdb == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	val, err := r.rdb.Get(ctx, "lastprompt:"+r.chatID).Result()
	if err != nil {
		return ""
	}
	return val
}

// plannerMaxTokens bounds the structured planner calls (initial plan +
// contract-repair pass). It is taken from the planner role config in the
// skills registry — the SAME budget agentplan.GeneratePlan uses in the
// dual-model path — so both plan paths always run with one budget (B1).
// Falls back to the registry default of 8192.
func plannerMaxTokens() int {
	reg := skills.NewRegistry()
	if cfg, ok := reg.Config(skills.RolePlanner); ok && cfg.MaxTokens > 0 {
		return cfg.MaxTokens
	}
	return 8192
}

// legacyPlanPrompt is the pre-structured planning prompt: a prose architect
// plan used when the skills loader did not provide the planner skill
// (skills/01_planner.md). It keeps the plan gate working with zero setup.
const legacyPlanPrompt = "You are a senior frontend architect. The user wants a web app. " +
	"Produce a CONCISE build plan in Markdown for a STATIC single-page " +
	"frontend (no backend). Structure it exactly as:\n" +
	"**Proposed Plan & Assumptions**\n" +
	"1. **Screens & Components:** bullet list of every screen/section and its key components.\n" +
	"2. **Mock Data:** what entities and how many realistic items each.\n" +
	"3. **Interactions:** filters/search/cart/tabs the app will support.\n" +
	"4. **Visual Style:** palette direction, typography, layout feel.\n\n" +
	"Be specific and concrete (name actual sections and items). Max 250 words. " +
	"Reply in the SAME language the user wrote in. Output ONLY the plan."

// proposePlan runs the planning phase of the pipeline: a first LLM call
// that turns the raw user request into a build plan.
//
// Two modes:
//   - Structured (plannerPrompt set from skills/01_planner.md): the model
//     emits ONE raw JSON object, decoded and validated against the
//     ExecutionPlan contract (backend/agent) with a single repair
//     round-trip on violation. The chat thread gets the human-readable
//     Render() markdown; the returned machine plan is the normalized raw
//     JSON that feeds the executor prompts.
//   - Legacy (fallback): the prose-architect prompt streams its plan to
//     the chat thread verbatim.
//
// Returns (machinePlan, display). machinePlan is what runGeneration passes
// to runPlanExecute / genPrompt; display is what the user sees (chat
// message + plan_proposed). ("", "") means planning failed unrecoverably —
// the caller then skips the plan gate and generates without a plan.
func (r *ProjectRoom) proposePlan(ctx context.Context, prompt string) (string, string) {
	convID := fmt.Sprintf("plan-%d", time.Now().UnixNano())

	structured := r.plannerPrompt != ""
	planSystem := legacyPlanPrompt
	maxTokens := 1024
	if structured {
		planSystem = r.plannerPrompt
		maxTokens = plannerMaxTokens()
	}

	// In structured mode the raw JSON stream is not for human eyes — the
	// rendered plan is broadcast once, complete, after validation.
	onDelta := func(delta string) {
		r.BroadcastMessage(models.ConversationResponse{
			Type:           "conversation_response",
			Message:        delta,
			ConversationID: convID,
			IsStreaming:    true,
		})
	}
	if structured {
		onDelta = func(string) {}
	}

	plan, _, err := r.streamLLMRaw(ctx, planSystem,
		[]llm.ChatMessage{{Role: "user", Content: prompt}}, maxTokens, onDelta)
	if err != nil {
		log.Printf("[room:%s] plan stream error: %v", r.chatID, err)
		return "", ""
	}

	display := plan
	if structured {
		valid := r.enforcePlanContract(ctx, planSystem, prompt, plan)
		if valid == nil {
			return "", ""
		}
		encoded, err := json.Marshal(valid)
		if err != nil {
			log.Printf("[room:%s] plan re-marshal error: %v", r.chatID, err)
			return "", ""
		}
		plan = string(encoded)
		display = valid.Render()
	}

	// Close the streaming turn.
	r.BroadcastMessage(models.ConversationResponse{
		Type:           "conversation_response",
		Message:        display,
		ConversationID: convID,
		IsStreaming:    false,
	})
	_ = r.appendChatMessage(ctx, "assistant", display)
	return plan, display
}

// enforcePlanContract validates the planner's raw JSON output against the
// ExecutionPlan contract (backend/agent). On violation it runs ONE repair
// round-trip: the model sees its rejected output plus the exact violations
// and must re-emit the corrected plan. Returns the valid normalized plan,
// or nil when the contract stays unrecoverable.
func (r *ProjectRoom) enforcePlanContract(ctx context.Context, system, prompt, raw string) *agentplan.ExecutionPlan {
	plan, perr := agentplan.ParsePlan(raw)
	if perr == nil {
		perr = plan.Validate()
	}
	if perr == nil {
		return &plan
	}

	log.Printf("[room:%s] plan violates the contract (%v); running one repair pass", r.chatID, perr)
	repaired, _, err := r.streamLLMRaw(ctx, system, planRepairMessages(prompt, raw, perr),
		plannerMaxTokens(), func(string) {})
	if err != nil {
		log.Printf("[room:%s] plan repair stream error: %v", r.chatID, err)
		return nil
	}
	plan, perr = agentplan.ParsePlan(repaired)
	if perr != nil {
		log.Printf("[room:%s] plan repair output does not parse: %v", r.chatID, perr)
		return nil
	}
	if verr := plan.Validate(); verr != nil {
		log.Printf("[room:%s] plan repair still violates the contract: %v", r.chatID, verr)
		return nil
	}
	return &plan
}

// planRepairMessages builds the message list for the single contract repair
// pass: the original request, the rejected plan, and the exact violations.
func planRepairMessages(prompt, rejected string, violations error) []llm.ChatMessage {
	return []llm.ChatMessage{
		{Role: "user", Content: prompt},
		{Role: "assistant", Content: rejected},
		{Role: "user", Content: "Your plan above violates the output contract:\n" +
			violations.Error() +
			"\n\nEmit the corrected ENTIRE plan again as exactly ONE raw JSON object " +
			"with the keys thought_process, subtasks, goal and steps. " +
			"No prose, no markdown fences, no comments."},
	}
}

func (r *ProjectRoom) runGeneration(prompt string) {
	// B9: the generation is cancellable via room Stop; on cancel the ctx
	// dies, streams close, and a generation_cancelled event is emitted.
	ctx, cancel := r.generationContext(5 * time.Minute)
	defer cancel()

	// Announce generation start.
	r.BroadcastMessage(models.GenerationStarted{
		Type:       "generation_started",
		Message:    "Generation started",
		TotalFiles: len(r.GetVFS()),
	})

	// R3 dual-model pipeline: planner JSON (GeneratePlan) -> per-step
	// coder calls (ExecuteStep) writing straight into the Redis VFS. It
	// runs whenever the planner skill (skills/01_planner.md) is loaded.
	// An explicit plan rejection or a cancellation ends the run; any
	// other failure falls back to the legacy flows below.
	if r.plannerPrompt != "" {
		if err := r.runDualModelPipeline(ctx, prompt); err == nil {
			return
		} else if errors.Is(err, errPlanRejected) {
			log.Printf("[room:%s] plan rejected; skipping generation", r.chatID)
			return
		} else if ctx.Err() != nil {
			debugLogEvent(r, "generation_cancelled", "reason", ctx.Err().Error())
			r.BroadcastMessage(models.GenerationCancelled{
				Type:   "generation_cancelled",
				Reason: "generation cancelled",
			})
			return
		} else {
			log.Printf("[room:%s] dual-model pipeline failed (%v); falling back to legacy generation",
				r.chatID, err)
		}
	}

	// Build the chat request. The system prompt instructs the model to
	// emit fenced code blocks with file paths. It deliberately pushes the
	// model toward a self-contained, single-page frontend: the preview runs
	// statically in a browser iframe (no Node/server runtime), so backend
	// code (Express, MongoDB, ts-node) would never execute or show data.
	// Phase 1: propose a plan and show it in the chat thread. Phase 2 then
	// generates the code following that plan as its blueprint.
	plan, planDisplay := r.proposePlan(ctx, prompt)
	genPrompt := prompt
	if plan != "" {
		// B7 human-in-the-loop: show the plan, then pause until the
		// client approves (plan_approved) — auto-approve on timeout,
		// abort on reject/cancel. Mirrors the vibesdk plan gate.
		convID := fmt.Sprintf("plan-%d", time.Now().UnixNano())
		r.BroadcastMessage(models.PlanProposed{
			Type:           "plan_proposed",
			ConversationID: convID,
			Plan:           planDisplay,
		})
		debugLogEvent(r, "plan_proposed", "conversation", convID, "bytes", len(planDisplay))
		if !r.waitForPlanApproval(ctx) {
			log.Printf("[room:%s] plan not approved; skipping generation", r.chatID)
			return
		}
		genPrompt = prompt + "\n\nFollow this approved build plan exactly:\n" + plan

		// B5: prefer the plan-execute-replan prebuilt — the executor
		// writes files via vfs_write tool calls (file events stream from
		// the tool wrapper) and the replanner revises remaining steps.
		// Falls back to the single-shot fence-streaming path below when
		// the engine/tool model is unavailable or the run fails.
		if r.canPlanExecute() {
			log.Printf("[room:%s] running plan-execute-replan", r.chatID)
			perr := r.runPlanExecute(ctx, prompt, plan)
			if perr == nil || ctx.Err() != nil {
				if ctx.Err() != nil {
					r.BroadcastMessage(models.GenerationCancelled{
						Type:   "generation_cancelled",
						Reason: "generation cancelled",
					})
					return
				}
				r.finalizeGeneration("", int(r.filesWritten.Load()))
				return
			}
			log.Printf("[room:%s] planexecute failed (%v); falling back to fence streaming", r.chatID, perr)
		}
	}

	// Non-system turns for streamLLM (the system prompt is passed
	// separately and stays constant across retries).
	messages := []llm.ChatMessage{
		{Role: "user", Content: genPrompt},
	}
	maxTokens := 12288
	const maxRetries = 2

	// Stream-parse the LLM output into per-file events (the vibesdk
	// pattern): file_generating -> file_chunk_generated (real path) ->
	// file_generated, while the model is still generating. The raw
	// transcript is NEVER exposed as a file. Events are applied via
	// r.applyFileEvents; the completed-file counter is r.filesWritten.
	r.filesWritten.Store(0)

	// Generate, retrying on truncation (the eino-examples
	// adk/agentic/retry_max_output_tokens pattern): when the model hit the
	// token limit (finish_reason=length) and left files unfinished, ask it
	// to re-emit ONLY the incomplete files with a doubled token budget.
	var output strings.Builder
	var truncated []string
	finishReason := ""
	for attempt := 0; ; attempt++ {
		out, trunc, err := r.streamAndApplyFiles(ctx, generationSystemPrompt, messages, maxTokens)
		output.WriteString(out)
		if err != nil {
			log.Printf("[room:%s] LLM stream error: %v", r.chatID, err)
			// B9: distinguish user cancellation from a real failure.
			if ctxErr := ctx.Err(); ctxErr != nil {
				debugLogEvent(r, "generation_cancelled", "reason", ctxErr.Error())
				r.BroadcastMessage(models.GenerationCancelled{
					Type:   "generation_cancelled",
					Reason: "generation cancelled",
				})
				return
			}
			r.BroadcastMessage(models.ErrorEvent{
				Type:  "error",
				Error: "LLM stream error: " + err.Error(),
			})
			// B12: tell the frontend the state may be partial.
			if written := r.filesWritten.Load(); written > 0 {
				r.BroadcastMessage(models.GenerationInterrupted{
					Type:   "generation_interrupted",
					Reason: "stream error after " + fmt.Sprint(written) + " files (some may be partial)",
				})
				// Recover instead of abandoning: some files landed, so
				// run the gap-fill below — it generates whatever the
				// stream never opened and stubs the rest, leaving the
				// preview in the best shape possible despite the error.
				break
			}
			return
		}
		truncated = trunc
		finishReason = "done"

		// Retry whenever files were left unfinished (salvage-flagged).
		// On the legacy fallback path finish_reason=length confirms it;
		// on the engine path the salvage flag alone is authoritative.
		if len(truncated) == 0 || attempt >= maxRetries {
			if len(truncated) > 0 {
				// B12: retries exhausted with salvaged (partial) files.
				r.BroadcastMessage(models.GenerationInterrupted{
					Type:   "generation_interrupted",
					Reason: "token limit reached after " + fmt.Sprint(attempt+1) + " attempts; incomplete files: " + strings.Join(truncated, ", "),
				})
			}
			break
		}

		// Truncation retry: list the salvage-flagged files and ask the
		// model to re-emit them complete, with a doubled budget.
		debugLogEvent(r, "generation_truncated",
			"attempt", attempt+1, "finish", finishReason,
			"truncated", strings.Join(truncated, ","), "maxTokens", maxTokens)
		log.Printf("[room:%s] output truncated (finish=length); retrying %d incomplete files with maxTokens=%d",
			r.chatID, len(truncated), maxTokens*2)
		var cont strings.Builder
		cont.WriteString("Your previous response hit the token limit and was cut off. ")
		cont.WriteString("The following files were left INCOMPLETE: ")
		cont.WriteString(strings.Join(truncated, ", "))
		cont.WriteString(". Re-emit ONLY these files now, each as a complete fenced code block ")
		cont.WriteString("with the same file path as the info string, containing the FULL final ")
		cont.WriteString("file content (never a diff, never a partial file). Do not repeat the other files.")
		messages = append(messages,
			llm.ChatMessage{Role: "assistant", Content: "(previous response cut off by token limit)"},
			llm.ChatMessage{Role: "user", Content: cont.String()},
		)
		maxTokens *= 2
	}
	debugLogEvent(r, "generation_finished",
		"finish", finishReason, "files", int(r.filesWritten.Load()),
		"truncated", strings.Join(truncated, ","))

	// B9: generation cancelled mid-flight (Stop) — no completion event.
	if ctx.Err() != nil {
		r.BroadcastMessage(models.GenerationCancelled{
			Type:   "generation_cancelled",
			Reason: "generation cancelled",
		})
		return
	}

	// Gap-fill: the stream can be cut BEFORE the model ever opens a file
	// (finish=length / connection drop between blocks). Such files are not
	// salvage-flagged, so the retry loop never re-emits them — yet the
	// entry HTML references them (a missing main.js blanks the preview).
	// Detect referenced-but-missing assets and stream them in.
	r.fillMissingReferencedFiles(ctx)

	// Step 3: persist files, broadcast completion.
	r.finalizeGeneration(output.String(), int(r.filesWritten.Load()))
}

// finalizeGeneration broadcasts the updated agent state and completion
// event after streaming. When the stream parser produced no files at all
// (e.g. a model that ignored the fence contract), it falls back to parsing
// the full output once.
func (r *ProjectRoom) finalizeGeneration(output string, streamedFiles int) {
	if streamedFiles == 0 {
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
	}

	// Phase 1: validate + persist the generated workflow.json (Redis + D1).
	// All generation paths funnel through finalizeGeneration, so a single
	// hook here covers dual-model, plan-execute and fence-streaming runs.
	r.persistWorkflowDag()

	// A3: keep the vector index (idx:vfs) in sync with the VFS so the next
	// planner call can retrieve real existing code ("Relevant existing code
	// context") instead of path+size only. No-op without Redis.
	if err := r.ReindexVFS(); err != nil {
		log.Printf("[room:%s] VFS reindex failed: %v", r.chatID, err)
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

// ---------- Workflow DAG persistence (schema v2) ----------

// workflowDagKey returns the Redis string key holding the latest
// workflow.json envelope for this room.
func (r *ProjectRoom) workflowDagKey() string {
	return "workflow:dag:" + r.chatID
}

// persistWorkflowDag validates the workflow.json produced by the last
// generation and persists it:
//
//   - valid   → the canonical v2 JSON is written to Redis ({workflowDagKey})
//     and upserted to D1 (workflow_dags) for the Phase-2 runtime worker.
//   - invalid → the raw file stays in the VFS so the frontend can still
//     render it; an envelope with status "invalid" + the validation error is
//     written to Redis. Generation is NEVER failed (salvage pattern).
//
// Called from finalizeGeneration, which all generation paths funnel through
// (dual-model, plan-execute, legacy fence-streaming).
func (r *ProjectRoom) persistWorkflowDag() {
	vfs := r.GetVFS()
	entry, ok := vfs["workflow.json"]
	if !ok {
		debugLogEvent(r, "workflow_dag", "status", "absent")
		return
	}

	wf, perr := ParseWorkflowV2(entry.FileContents)
	if perr == nil {
		perr = wf.Validate()
	}
	if perr != nil {
		debugLogEvent(r, "workflow_dag", "status", "invalid", "error", perr.Error())
		_ = r.saveWorkflowDagEnvelope(map[string]any{
			"status": "invalid",
			"raw":    entry.FileContents,
			"error":  perr.Error(),
		})
		return
	}

	b, merr := json.Marshal(wf)
	if merr != nil {
		log.Printf("[room:%s] failed to re-marshal workflow DAG: %v", r.chatID, merr)
		return
	}
	encoded := string(b)
	if err := r.saveWorkflowDagEnvelope(map[string]any{
		"status":        "valid",
		"schemaVersion": WorkflowSchemaV2,
		"dag":           encoded,
	}); err != nil {
		log.Printf("[room:%s] failed to persist workflow DAG to Redis: %v", r.chatID, err)
	}

	// Sync to D1 (best-effort; never fails the generation).
	if r.d1 != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if cerr := r.d1.UpsertWorkflowDag(ctx, r.chatID, WorkflowSchemaV2, encoded); cerr != nil {
			log.Printf("[room:%s] failed to sync workflow DAG to D1: %v", r.chatID, cerr)
		}
	}
}

// saveWorkflowDagEnvelope persists the workflow envelope JSON string in
// Redis ({workflowDagKey}). No-op when Redis is unavailable.
func (r *ProjectRoom) saveWorkflowDagEnvelope(envelope map[string]any) error {
	if r.rdb == nil {
		return nil
	}
	b, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// 7-day TTL: the DAG envelope only needs to outlive the current
	// session; the validated DAG is the durable copy in D1.
	return r.rdb.Set(ctx, r.workflowDagKey(), string(b), 7*24*time.Hour).Err()
}

// ---------- Conversational chat (user_suggestion) ----------

// chatKey returns the Redis List key that persists this room's conversational
// history. Each element is a JSON object {"role": "user"|"assistant",
// "content": "..."} in chronological order. It is separate from the VFS hash
// so conversational replies never collide with the generated file set.
func (r *ProjectRoom) chatKey() string {
	return "chat:" + r.chatID
}

// appendChatMessage appends a single turn to the Redis chat history list.
// Returns an error if Redis is unavailable; callers should degrade gracefully
// (history is best-effort context, not a hard dependency for a reply).
func (r *ProjectRoom) appendChatMessage(ctx context.Context, role, content string) error {
	if r.rdb == nil {
		return errors.New("redis client not configured")
	}
	payload, err := json.Marshal(map[string]string{
		"role":    role,
		"content": content,
	})
	if err != nil {
		return err
	}
	return r.rdb.RPush(ctx, r.chatKey(), payload).Err()
}

// historySoftLimit is the number of turns above which older turns are
// condensed into a summary (B6, the adk/intro/agent_with_summarization
// pattern). Below the limit nothing happens.
const historySoftLimit = 24

// historyKeepRecent is how many most-recent turns are kept verbatim when
// condensing.
const historyKeepRecent = 8

// recentChatHistory returns the most recent turns (oldest first) as LLM
// chat messages. When the stored history exceeds historySoftLimit, older
// turns are condensed into one summary turn via a cheap LLM call instead
// of being cut off blindly (B6). Best-effort: any summarizer failure
// falls back to plain truncation.
func (r *ProjectRoom) recentChatHistory(ctx context.Context, n int64) []llm.ChatMessage {
	if r.rdb == nil {
		return []llm.ChatMessage{}
	}
	values, err := r.rdb.LRange(ctx, r.chatKey(), -n, -1).Result()
	if err != nil {
		log.Printf("[room:%s] failed to read chat history: %v", r.chatID, err)
		return []llm.ChatMessage{}
	}
	turns := decodeChatHistory(values)
	if int64(len(turns)) <= historySoftLimit {
		return turns
	}

	cut := len(turns) - historyKeepRecent
	older, recent := turns[:cut], turns[cut:]
	summary, err := r.summarizeTurns(ctx, older)
	if err != nil {
		log.Printf("[room:%s] history summarization failed, truncating: %v", r.chatID, err)
		return recent
	}
	log.Printf("[room:%s] condensed %d history turns into a summary", r.chatID, cut)
	condensed := make([]llm.ChatMessage, 0, 1+len(recent))
	condensed = append(condensed, llm.ChatMessage{
		Role: "assistant",
		Content: "Earlier conversation summary (replacing " + fmt.Sprint(cut) +
			" older turns):\n" + summary,
	})
	condensed = append(condensed, recent...)
	return condensed
}

// summarizeTurns condenses older conversation turns into a compact
// summary via the LLM (no streaming, no persistence).
func (r *ProjectRoom) summarizeTurns(ctx context.Context, turns []llm.ChatMessage) (string, error) {
	var transcript strings.Builder
	for _, t := range turns {
		role := t.Role
		if role == "" {
			role = "user"
		}
		transcript.WriteString(role)
		transcript.WriteString(": ")
		// Cap each turn so one huge pasted file cannot blow the
		// summarizer budget.
		const maxTurn = 500
		content := t.Content
		if len(content) > maxTurn {
			content = content[:maxTurn] + "…"
		}
		transcript.WriteString(content)
		transcript.WriteString("\n")
	}

	summarizerSystem := "You condense web-app-building conversations. Produce a terse " +
		"bullet summary (max 150 words) of the transcript: what app is being built, " +
		"key decisions, file names mentioned, and outstanding user requests. " +
		"Reply in the SAME language as the transcript. Output ONLY the summary."

	const ctxTimeout = 60 * time.Second
	sctx, cancel := context.WithTimeout(ctx, ctxTimeout)
	defer cancel()
	summary, _, err := r.streamLLM(sctx, summarizerSystem,
		[]llm.ChatMessage{{Role: "user", Content: transcript.String()}}, 512,
		func(string) {})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(summary) == "" {
		return "", errors.New("summarizer returned empty output")
	}
	return summary, nil
}

// decodeChatHistory parses raw Redis list entries (JSON objects shaped
// {"role": "...", "content": "..."}) into LLM chat messages, skipping entries
// that are malformed or blank. It is a pure function so the parsing logic can
// be unit-tested without a live Redis.
func decodeChatHistory(values []string) []llm.ChatMessage {
	history := make([]llm.ChatMessage, 0, len(values))
	for _, v := range values {
		var turn struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(v), &turn); err != nil {
			continue
		}
		if turn.Role == "" || turn.Content == "" {
			continue
		}
		history = append(history, llm.ChatMessage{Role: turn.Role, Content: turn.Content})
	}
	return history
}

// StartConversation kicks off an asynchronous conversational reply for a
// `user_suggestion` message. It streams plain markdown back as
// conversation_response events and persists the user/assistant turns to the
// Redis chat history. Crucially it does NOT alter shouldBeGenerating and does
// NOT create/update any files — the reply is purely textual, so it never
// triggers a preview refresh, Sandpack, or file generation UI.
func (r *ProjectRoom) StartConversation(prompt string) {
	if r.llm == nil {
		log.Printf("[room:%s] no LLM client configured; skipping conversational reply", r.chatID)
		r.BroadcastMessage(models.ErrorEvent{
			Type:  "error",
			Error: "LLM client is not configured (set AI_GATEWAY_URL or CLOUDFLARE_ACCOUNT_ID)",
		})
		return
	}

	go r.runConversation(prompt)
}

// runConversation is the background task behind StartConversation. It appends
// the user turn, streams the LLM reply as conversation_response deltas, appends
// the assistant turn, then broadcasts a non-streaming final event.
//
// Each turn gets a unique conversationId so the frontend renders every reply
// as its own assistant message right after the user's message — instead of
// appending all deltas into a single shared placeholder.
func (r *ProjectRoom) runConversation(prompt string) {
	// B9: conversational replies are cancellable via room Stop too.
	ctx, cancel := r.generationContext(3 * time.Minute)
	defer cancel()

	// Unique id for this conversational turn (stable across all deltas).
	convID := fmt.Sprintf("conv-%d", time.Now().UnixNano())

	if err := r.appendChatMessage(ctx, "user", prompt); err != nil {
		log.Printf("[room:%s] failed to persist user chat turn: %v", r.chatID, err)
	}

	// Build the request from recent history plus the CURRENT user prompt.
	// The prompt must be appended unconditionally: when Redis is unavailable
	// (history read fails) the model would otherwise receive only the system
	// message and answer with a generic greeting. With this, the current turn
	// always reaches the model even without history context.
	chatSystem := "You are a helpful assistant inside a code-generation app. " +
		"Answer the user's question conversationally in plain Markdown. " +
		"Always reply in the SAME language the user wrote in (e.g. Persian question → Persian answer). " +
		"Do NOT emit fenced code blocks with file paths, do NOT attempt to " +
		"generate or modify an app — this is a conversational reply only."
	// Fetch enough history for the summarizer to matter; recentChatHistory
	// condenses older turns above the soft limit (B6).
	msgs := append(r.recentChatHistory(ctx, 64), llm.ChatMessage{
		Role:    "user",
		Content: prompt,
	})

	full, _, err := r.streamLLM(ctx, chatSystem, msgs, 1024,
		func(delta string) {
			r.BroadcastMessage(models.ConversationResponse{
				Type:           "conversation_response",
				ConversationID: convID,
				Message:        delta,
				IsStreaming:    true,
			})
		})
	if err != nil {
		log.Printf("[room:%s] LLM stream error: %v", r.chatID, err)
		r.BroadcastMessage(models.ErrorEvent{
			Type:  "error",
			Error: "Failed to start conversational stream: " + err.Error(),
		})
		return
	}

	if err := r.appendChatMessage(ctx, "assistant", full); err != nil {
		log.Printf("[room:%s] failed to persist assistant chat turn: %v", r.chatID, err)
	}

	// Finalize the turn so the frontend closes the streaming placeholder.
	r.BroadcastMessage(models.ConversationResponse{
		Type:           "conversation_response",
		ConversationID: convID,
		Message:        full,
	})
}
