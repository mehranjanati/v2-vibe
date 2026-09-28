package engine

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"backend/pkg/agent"
	"backend/pkg/cloudflare"
	"backend/pkg/llm"
)

// EngineHub is the top-level actor registry. It owns the map of
// ProjectRooms (one Goroutine-based actor per chatId) and the shared
// Redis client used for VFS persistence.
//
// All access to the rooms map is guarded by sync.RWMutex to prevent
// data races across Goroutines.
type EngineHub struct {
	mu    sync.RWMutex
	rooms map[string]*ProjectRoom

	rdb *redis.Client
	llm *llm.Client
	cf  *cloudflare.Client
	d1  *cloudflare.D1Client

	// eng is the unified eino agent engine (P3). When set, all room LLM
	// flows (plan, generation, chat) route through it; the raw llm.Client
	// remains only as fallback.
	eng *agent.Engine

	// plannerPrompt is the structured planner system prompt (skills/
	// 01_planner.md) handed to every room created after SetPlannerPrompt.
	plannerPrompt string

	// teamPrompts carries the multi-agent team role prompts (skills/
	// 00_coordinator.md, 02_coder.md, 03_reviewer.md). Empty means the
	// room falls back to the single-coder dual-model path.
	teamPrompts teamSkillPrompts

	// workflowName is the Cloudflare Workflows name (wrangler.v2.jsonc
	// [[workflows]]) that executes generated DAGs via the VibeWorkflow
	// class. Used by POST /api/workflows/trigger to start runs.
	workflowName string
}

// NewEngineHub creates a hub backed by the given Redis, LLM, and
// Cloudflare clients.
func NewEngineHub(rdb *redis.Client, llmClient *llm.Client, cfClient *cloudflare.Client) *EngineHub {
	return &EngineHub{
		rooms: make(map[string]*ProjectRoom),
		rdb:   rdb,
		llm:   llmClient,
		cf:    cfClient,
	}
}

// SetD1Client attaches the Cloudflare D1 client used for persistence
// (auth/users, limits) by the REST API handlers.
func (h *EngineHub) SetD1Client(d1 *cloudflare.D1Client) {
	h.d1 = d1
}

// SetEngine attaches the unified eino agent engine. Rooms created after
// this call route all LLM flows through it (see ProjectRoom.streamLLM).
func (h *EngineHub) SetEngine(eng *agent.Engine) {
	h.eng = eng
}

// SetPlannerPrompt stores the structured planner system prompt (skills/
// 01_planner.md). Rooms created afterwards plan with the ExecutionPlan
// contract; rooms without it fall back to the legacy prose prompt.
func (h *EngineHub) SetPlannerPrompt(prompt string) {
	h.plannerPrompt = prompt
}

// SetTeamPrompts stores the multi-agent team role prompts handed to
// every room created after this call. Rooms without them keep the
// single-coder dual-model execution path.
func (h *EngineHub) SetTeamPrompts(coordinator, coder, reviewer string) {
	h.teamPrompts = teamSkillPrompts{coordinator: coordinator, coder: coder, reviewer: reviewer}
}

// D1Client returns the attached Cloudflare D1 client (nil if unset).
func (h *EngineHub) D1Client() *cloudflare.D1Client {
	return h.d1
}

// SetCloudflareClient attaches the Cloudflare API client (Pages deploys +
// Workflows instance creation). Handled separately from the constructor so
// tests can inject an httptest-backed client.
func (h *EngineHub) SetCloudflareClient(cf *cloudflare.Client) {
	h.cf = cf
}

// CloudflareClient returns the attached Cloudflare API client (nil if unset).
func (h *EngineHub) CloudflareClient() *cloudflare.Client {
	return h.cf
}

// SetWorkflowName stores the Cloudflare Workflows name (wrangler.v2.jsonc
// [[workflows]]) that runs generated DAGs.
func (h *EngineHub) SetWorkflowName(name string) {
	h.workflowName = name
}

// WorkflowName returns the configured Workflows name ("" if unset).
func (h *EngineHub) WorkflowName() string {
	return h.workflowName
}

// Cfg returns an env var value or the provided fallback.
func (h *EngineHub) Cfg(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// GetOrCreateRoom returns the ProjectRoom for chatID, creating and
// starting it (its Run loop) if it does not yet exist.
func (h *EngineHub) GetOrCreateRoom(chatID string) *ProjectRoom {
	h.mu.RLock()
	room, ok := h.rooms[chatID]
	h.mu.RUnlock()
	if ok {
		return room
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Double-check after acquiring the write lock.
	if room, ok = h.rooms[chatID]; ok {
		return room
	}

	room = NewProjectRoom(chatID, h.rdb, h.llm, h.eng)
	room.SetCloudflareClient(h.cf)
	room.SetD1Client(h.d1)
	if h.plannerPrompt != "" {
		room.SetPlannerPrompt(h.plannerPrompt)
	}
	if h.teamPrompts.coordinator != "" {
		room.SetTeamPrompts(h.teamPrompts)
	}
	h.rooms[chatID] = room
	go room.Run()
	return room
}

// GetRoom returns the room for chatID without creating it.
func (h *EngineHub) GetRoom(chatID string) (*ProjectRoom, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	room, ok := h.rooms[chatID]
	return room, ok
}

// GetVFSReadOnly returns the project's VFS as a flat path -> contents map
// WITHOUT creating or starting a room. It prefers the in-memory room (the
// freshest state); when no live room exists it falls back to reading the
// persisted Redis hash ('vfs:{chatId}') directly. Read-only callers (e.g.
// GET /api/projects/:id/files) must use this instead of GetOrCreateRoom so
// that arbitrary GETs never spawn room actors.
func (h *EngineHub) GetVFSReadOnly(chatID string) (map[string]string, error) {
	if room, ok := h.GetRoom(chatID); ok {
		entries := room.GetVFS()
		out := make(map[string]string, len(entries))
		for path, entry := range entries {
			out[path] = entry.FileContents
		}
		return out, nil
	}

	if h.rdb == nil {
		return map[string]string{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entries, err := h.rdb.HGetAll(ctx, "vfs:"+chatID).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(entries))
	for path, contents := range entries {
		// Mirror loadVFS's legacy-key cleanup: never surface the raw LLM
		// transcript pseudo-file to clients.
		if IsLegacyOutputFile(path) {
			continue
		}
		out[path] = contents
	}
	return out, nil
}

// RemoveRoom deletes the room from the registry. Callers should stop the
// room's Run loop first (e.g. via room.Stop()).
func (h *EngineHub) RemoveRoom(chatID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms, chatID)
}

// RoomCount returns the number of active rooms (for observability).
func (h *EngineHub) RoomCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms)
}
