package engine

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"backend/pkg/agent"
	"backend/pkg/cloudflare"
	"backend/pkg/llm"
)

// SessionVerifier resolves a caller's session token to a user id. The control
// plane stores one on the hub so the route guards in backend/pkg/api can
// authenticate mutating requests without that package owning the identity
// store (see docs/DEV_CHECKLIST.md P0.3).
//
// Contract:
//   - a token that resolves to a user id returns (userID, nil);
//   - a token the store does not know returns ("", nil) — an unknown session,
//     not a failure;
//   - a store that cannot be reached returns a non-nil error. Wrap
//     ErrSessionStoreUnavailable when the reason is configuration/outage so
//     the guard answers 503 instead of 401.
type SessionVerifier interface {
	VerifyUserID(ctx context.Context, token string) (string, error)
}

// ErrSessionStoreUnavailable signals that the session store is not configured
// or unreachable. Guards translate it into 503 (fail closed) rather than 401.
var ErrSessionStoreUnavailable = errors.New("engine: session store unavailable")

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

	// roomOwners maps a room/project id to the user id that owns it (P0.4).
	// First write wins: creation claims the room for its caller, and every
	// later access must present the same identity. Guarded by mu. This is
	// the in-memory half of the ownership model; RoomOwner persists it to
	// Redis so ownership survives room eviction and (when rdb is set)
	// process restarts.
	roomOwners map[string]string

	// sessionVerifier authenticates mutating control-plane requests (P0.3).
	// nil means the boundary is not configured: the route guards degrade to
	// a no-op so the pre-existing behaviour is preserved until the P0.9
	// identity decision lands.
	sessionVerifier SessionVerifier
}

// NewEngineHub creates a hub backed by the given Redis, LLM, and
// Cloudflare clients.
func NewEngineHub(rdb *redis.Client, llmClient *llm.Client, cfClient *cloudflare.Client) *EngineHub {
	return &EngineHub{
		rooms:      make(map[string]*ProjectRoom),
		roomOwners: make(map[string]string),
		rdb:        rdb,
		llm:        llmClient,
		cf:         cfClient,
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

// SetSessionVerifier installs the control-plane session verifier (P0.3).
// Installing one turns the mutating-route guards in backend/pkg/api on;
// leaving it unset keeps the boundary permissive (documented provisional
// state pending the P0.9 identity decision).
func (h *EngineHub) SetSessionVerifier(v SessionVerifier) {
	h.sessionVerifier = v
}

// SessionVerifier returns the installed session verifier (nil when unset).
func (h *EngineHub) SessionVerifier() SessionVerifier {
	return h.sessionVerifier
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

// roomOwnerKey is the Redis key holding a room's owning user id (P0.4).
// Persisted separately from the VFS hash so ownership survives room eviction
// and process restarts.
const roomOwnerKeyPrefix = "room:owner:"

// ClaimRoomOwner records userID as the owner of chatID. First write wins:
// when the room is already owned by someone else the existing owner is
// returned and the claim is refused (ok=false). An empty userID never claims
// (ok=false, owner=""). A Redis read failure is returned as an error and the
// caller must fail closed (503) — never treat it as "unclaimed". The
// in-memory registry and Redis are kept in lockstep so a restart rediscovers
// the Redis claim on first lookup.
func (h *EngineHub) ClaimRoomOwner(chatID, userID string) (owner string, ok bool, err error) {
	if chatID == "" || userID == "" {
		return "", false, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if existing, claimed := h.roomOwners[chatID]; claimed {
		return existing, existing == userID, nil
	}
	// In-memory registry is authoritative for the race: a concurrent claim
	// for the same room inside this process loses here rather than in Redis.
	persisted, lerr := h.loadRoomOwnerLocked(chatID)
	if lerr != nil {
		return "", false, lerr
	}
	if persisted != "" {
		h.roomOwners[chatID] = persisted
		return persisted, persisted == userID, nil
	}

	h.roomOwners[chatID] = userID
	h.storeRoomOwnerLocked(chatID, userID)
	return userID, true, nil
}

// RoomOwner returns the owning user id of chatID. The second return reports
// whether an owner is recorded at all. An error (Redis unreachable) is
// reported via the third return; callers must fail closed on it, never treat
// it as "no owner".
func (h *EngineHub) RoomOwner(chatID string) (owner string, claimed bool, err error) {
	if chatID == "" {
		return "", false, nil
	}

	h.mu.RLock()
	owner, claimed = h.roomOwners[chatID]
	h.mu.RUnlock()
	if claimed {
		return owner, true, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	// Re-check under the write lock: a concurrent Claim may have landed while
	// we released the read lock.
	if owner, claimed = h.roomOwners[chatID]; claimed {
		return owner, true, nil
	}
	owner, err = h.loadRoomOwnerLocked(chatID)
	if err != nil {
		return "", false, err
	}
	if owner == "" {
		return "", false, nil
	}
	h.roomOwners[chatID] = owner
	return owner, true, nil
}

// loadRoomOwnerLocked reads the persisted owner. It must be called with h.mu
// held (read or write). A nil Redis client means "no persistence" — not an
// error — so dev/CI without Redis keeps working on the in-memory registry.
func (h *EngineHub) loadRoomOwnerLocked(chatID string) (string, error) {
	if h.rdb == nil {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	userID, err := h.rdb.Get(ctx, roomOwnerKeyPrefix+chatID).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(userID), nil
}

// storeRoomOwnerLocked persists the owner. Best effort by design: a Redis
// write failure is logged by the caller path (it never blocks creation),
// because the in-memory registry already holds the claim for this process.
func (h *EngineHub) storeRoomOwnerLocked(chatID, userID string) {
	if h.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.rdb.Set(ctx, roomOwnerKeyPrefix+chatID, userID, 0).Err(); err != nil {
		log.Printf("[hub] failed to persist owner of room %s: %v", chatID, err)
	}
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
