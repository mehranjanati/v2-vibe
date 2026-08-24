package engine

import (
	"os"
	"sync"

	"github.com/redis/go-redis/v9"

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

// D1Client returns the attached Cloudflare D1 client (nil if unset).
func (h *EngineHub) D1Client() *cloudflare.D1Client {
	return h.d1
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

	room = NewProjectRoom(chatID, h.rdb, h.llm)
	room.SetCloudflareClient(h.cf)
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
