package engine

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"
)

// Room lifecycle + quotas (P0.8).
//
// The hub used to keep every ProjectRoom alive for the life of the process:
// one Run goroutine, one in-memory VFS snapshot and one ownership entry per
// project a user ever opened. A long-running control plane therefore leaked
// memory monotonically and had no per-user ceiling. This file is the documented
// policy and its enforcement:
//
//   - idle eviction — a room with no connected clients, no generation running
//     and no activity for ROOM_IDLE_TTL is stopped and removed from the
//     registry. Eviction is lossless: UpsertFile/DeleteFile write through to
//     the Redis VFS hash, and the ownership claim lives in
//     room:owner:<chatID> (see hub.go), so a later request recreates the room
//     with the same files and the same tenant.
//   - quota — creating a NEW room for a user who already owns
//     ROOM_MAX_PER_USER live rooms is refused with ErrRoomQuotaExceeded
//     (HTTP 429 at the route). Existing rooms are never refused, so a quota
//     trip cannot lock anyone out of a project they already have.
//   - janitor — StartJanitor sweeps every ROOM_SWEEP_INTERVAL.
//
// All three are disabled by setting the value to 0, which restores the
// pre-P0.8 unbounded behaviour for a single-tenant deployment.

// Default room lifecycle policy. The TTL is deliberately generous: a project
// survives a coffee break, a CI run and a tab that is reopened later; the cap
// is a per-user blast-radius limit, not a product limit.
const (
	DefaultRoomIdleTTL       = 30 * time.Minute
	DefaultRoomMaxPerUser    = 10
	DefaultRoomSweepInterval = time.Minute
)

// ErrRoomQuotaExceeded is returned by EnsureRoom when the caller already owns
// the maximum number of live rooms. The HTTP layer maps it to 429.
var ErrRoomQuotaExceeded = errors.New("engine: room quota exceeded")

// RoomLifecyclePolicy is the documented TTL/heartbeat/cap policy for rooms.
type RoomLifecyclePolicy struct {
	// IdleTTL is how long a room may stay quiet (no clients, no generation,
	// no activity) before it is evicted. 0 disables eviction.
	IdleTTL time.Duration
	// MaxRoomsPerUser caps the number of live rooms one user may own.
	// 0 disables the cap.
	MaxRoomsPerUser int
	// SweepInterval is how often the janitor scans the registry.
	SweepInterval time.Duration
}

// DefaultRoomLifecyclePolicy returns the policy used when the env vars are
// unset.
func DefaultRoomLifecyclePolicy() RoomLifecyclePolicy {
	return RoomLifecyclePolicy{
		IdleTTL:         DefaultRoomIdleTTL,
		MaxRoomsPerUser: DefaultRoomMaxPerUser,
		SweepInterval:   DefaultRoomSweepInterval,
	}
}

// RoomLifecyclePolicyFromEnv builds the policy from the control-plane
// environment. getenv is hub.Cfg (value or fallback). Invalid values fall back
// to the default with a warning instead of failing startup: room lifecycle is
// not a reason to refuse to boot.
//
//	ROOM_IDLE_TTL        Go duration ("30m", "2h"); 0 disables eviction
//	ROOM_MAX_PER_USER    integer live-room cap; 0 disables the cap
//	ROOM_SWEEP_INTERVAL  Go duration ("1m", "30s"); must be > 0
func RoomLifecyclePolicyFromEnv(getenv func(key, fallback string) string) RoomLifecyclePolicy {
	policy := DefaultRoomLifecyclePolicy()

	if raw := strings.TrimSpace(getenv("ROOM_IDLE_TTL", "")); raw != "" {
		d, err := time.ParseDuration(raw)
		switch {
		case err != nil:
			log.Printf("[hub] invalid ROOM_IDLE_TTL %q (%v); using %s", raw, err, policy.IdleTTL)
		case d < 0:
			log.Printf("[hub] negative ROOM_IDLE_TTL %q; using %s", raw, policy.IdleTTL)
		default:
			policy.IdleTTL = d
		}
	}

	if raw := strings.TrimSpace(getenv("ROOM_MAX_PER_USER", "")); raw != "" {
		n, err := strconv.Atoi(raw)
		switch {
		case err != nil:
			log.Printf("[hub] invalid ROOM_MAX_PER_USER %q (%v); using %d", raw, err, policy.MaxRoomsPerUser)
		case n < 0:
			log.Printf("[hub] negative ROOM_MAX_PER_USER %q; using %d", raw, policy.MaxRoomsPerUser)
		default:
			policy.MaxRoomsPerUser = n
		}
	}

	if raw := strings.TrimSpace(getenv("ROOM_SWEEP_INTERVAL", "")); raw != "" {
		d, err := time.ParseDuration(raw)
		switch {
		case err != nil:
			log.Printf("[hub] invalid ROOM_SWEEP_INTERVAL %q (%v); using %s", raw, err, policy.SweepInterval)
		case d <= 0:
			log.Printf("[hub] non-positive ROOM_SWEEP_INTERVAL %q; using %s", raw, policy.SweepInterval)
		default:
			policy.SweepInterval = d
		}
	}

	return policy
}

// SetLifecyclePolicy installs the room lifecycle policy. Call it at startup,
// before StartJanitor, so the janitor and the creation path read one policy.
func (h *EngineHub) SetLifecyclePolicy(policy RoomLifecyclePolicy) {
	h.policyMu.Lock()
	h.policy = policy
	h.policyMu.Unlock()
}

// LifecyclePolicy returns the installed policy (defaults when unset).
func (h *EngineHub) LifecyclePolicy() RoomLifecyclePolicy {
	h.policyMu.RLock()
	policy := h.policy
	h.policyMu.RUnlock()
	if policy == (RoomLifecyclePolicy{}) {
		return DefaultRoomLifecyclePolicy()
	}
	return policy
}


// EnsureRoom returns the room for chatID, creating it when absent, and claims a
// newly created room for ownerID. When the caller already owns maxRoomsPerUser
// live rooms and chatID does not exist yet, it returns ErrRoomQuotaExceeded and
// creates nothing. maxRoomsPerUser <= 0 disables the cap; an empty ownerID
// never claims and never counts (single-tenant/dev path).
func (h *EngineHub) EnsureRoom(chatID, ownerID string, maxRoomsPerUser int) (*ProjectRoom, error) {
	return h.ensureRoom(chatID, ownerID, maxRoomsPerUser)
}

// ensureRoom is the shared creation path for GetOrCreateRoom (unowned, uncapped)
// and EnsureRoom (owned, capped).
func (h *EngineHub) ensureRoom(chatID, ownerID string, maxRoomsPerUser int) (*ProjectRoom, error) {
	h.mu.RLock()
	room, ok := h.rooms[chatID]
	h.mu.RUnlock()
	if ok {
		room.Touch()
		return room, nil
	}

	ownerID = strings.TrimSpace(ownerID)

	// P0.7: resolve the room's tenant before creating it so a regenerated
	// session stamps generated DAGs with the recorded owner even when this
	// process never held the claim in memory. A lookup failure is logged, not
	// fatal: the guards that authorize caller access fail closed on their own
	// lookups.
	owner, claimed, oerr := h.RoomOwner(chatID)
	if oerr != nil {
		log.Printf("[hub] owner lookup for room %s failed: %v", chatID, oerr)
	}

	h.mu.Lock()

	// Double-check after acquiring the write lock.
	if room, ok = h.rooms[chatID]; ok {
		h.mu.Unlock()
		room.Touch()
		return room, nil
	}

	// P0.8: the quota check and the insertion share this write lock, so two
	// concurrent creations cannot both slip under the cap.
	if ownerID != "" && maxRoomsPerUser > 0 {
		if live := h.roomsOwnedByLocked(ownerID); live >= maxRoomsPerUser {
			h.mu.Unlock()
			log.Printf("[hub] room quota reached for user %s (%d live rooms, cap %d); refusing to create %s",
				ownerID, live, maxRoomsPerUser, chatID)
			return nil, ErrRoomQuotaExceeded
		}
	}

	room = NewProjectRoom(chatID, h.rdb, h.llm, h.eng)
	room.SetCloudflareClient(h.cf)
	room.SetD1Client(h.d1)
	switch {
	case claimed && owner != "":
		// A recorded claim wins over the caller's identity (first write wins).
		room.SetOwner(owner)
	case ownerID != "":
		// Claim on creation so the quota counts this room immediately and the
		// generated DAGs are tenant-stamped. storeRoomOwnerLocked persists it to
		// Redis, which is what makes the claim survive eviction.
		room.SetOwner(ownerID)
		h.roomOwners[chatID] = ownerID
		h.storeRoomOwnerLocked(chatID, ownerID)
	}
	if h.plannerPrompt != "" {
		room.SetPlannerPrompt(h.plannerPrompt)
	}
	if h.teamPrompts.coordinator != "" {
		room.SetTeamPrompts(h.teamPrompts)
	}
	h.rooms[chatID] = room
	h.mu.Unlock()

	go room.Run()
	return room, nil
}

// roomsOwnedByLocked counts the live rooms owned by userID. Rooms without a
// claim (boundary off, or created through the unowned path) never match, so an
// unauthenticated deployment is unaffected by the quota.
// Must be called with h.mu held.
func (h *EngineHub) roomsOwnedByLocked(userID string) int {
	count := 0
	for _, room := range h.rooms {
		if room.Owner() == userID {
			count++
		}
	}
	return count
}

// EvictIdleRooms stops and removes every room that has no clients attached, no
// generation in flight and has been idle for at least idleTTL. It returns the
// evicted chat ids (empty when nothing qualified). idleTTL <= 0 is a no-op, so
// a deployment that sets ROOM_IDLE_TTL=0 keeps the pre-P0.8 behaviour.
//
// The rooms are stopped AFTER the registry lock is released: Stop waits for the
// Run loop to exit, and holding the hub lock across that wait would stall every
// other room operation.
func (h *EngineHub) EvictIdleRooms(now time.Time, idleTTL time.Duration) []string {
	if idleTTL <= 0 {
		return nil
	}

	h.mu.Lock()
	var evicted []*ProjectRoom
	var ids []string
	for chatID, room := range h.rooms {
		if room.ClientCount() > 0 || room.Generating() {
			continue
		}
		if room.IdleFor(now) < idleTTL {
			continue
		}
		delete(h.rooms, chatID)
		// With Redis the claim survives eviction (room:owner:<chatID>), so the
		// in-memory mirror is pruned with the room. Without Redis the mirror IS
		// the ownership record — dropping it would let another caller claim the
		// evicted room, so it is kept.
		if h.rdb != nil {
			delete(h.roomOwners, chatID)
		}
		evicted = append(evicted, room)
		ids = append(ids, chatID)
	}
	h.mu.Unlock()

	for _, room := range evicted {
		room.Stop()
	}
	return ids
}

// StartJanitor runs the idle-room sweeper until ctx is cancelled. It is a no-op
// (with a log line) when idle eviction is disabled. Call it once at startup
// after SetLifecyclePolicy; the sweep interval is read once at start.
func (h *EngineHub) StartJanitor(ctx context.Context) {
	policy := h.LifecyclePolicy()
	if policy.IdleTTL <= 0 {
		log.Printf("[hub] room janitor disabled (ROOM_IDLE_TTL=0 keeps rooms until process exit)")
		return
	}
	if policy.SweepInterval <= 0 {
		policy.SweepInterval = DefaultRoomSweepInterval
	}
	log.Printf("[hub] room janitor started (idle ttl %s, sweep %s, max %d live rooms per user)",
		policy.IdleTTL, policy.SweepInterval, policy.MaxRoomsPerUser)

	go func() {
		ticker := time.NewTicker(policy.SweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if ids := h.EvictIdleRooms(now, policy.IdleTTL); len(ids) > 0 {
					log.Printf("[hub] evicted %d idle room(s): %s", len(ids), strings.Join(ids, ","))
				}
			}
		}
	}()
}
