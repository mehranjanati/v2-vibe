package engine

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestEnsureRoomEnforcesPerUserQuota pins the P0.8 cap: a user may hold at
// most MaxRoomsPerUser LIVE rooms, a refused creation leaves no room behind,
// re-entering an existing room is never refused, and the cap is per user.
func TestEnsureRoomEnforcesPerUserQuota(t *testing.T) {
	hub := NewEngineHub(nil, nil, nil)
	hub.SetLifecyclePolicy(RoomLifecyclePolicy{MaxRoomsPerUser: 2})

	for _, id := range []string{"room-a1", "room-a2"} {
		room, err := hub.EnsureRoom(id, "user-a", hub.LifecyclePolicy().MaxRoomsPerUser)
		if err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		if room == nil {
			t.Fatalf("create %s: nil room", id)
		}
		if owner := room.Owner(); owner != "user-a" {
			t.Fatalf("create %s: owner=%q, want user-a", id, owner)
		}
	}

	if _, err := hub.EnsureRoom("room-a3", "user-a", 2); !errors.Is(err, ErrRoomQuotaExceeded) {
		t.Fatalf("third room: err=%v, want ErrRoomQuotaExceeded", err)
	}
	if got := hub.RoomCount(); got != 2 {
		t.Fatalf("after a refused creation: %d live rooms, want 2", got)
	}
	if room, ok := hub.GetRoom("room-a3"); ok || room != nil {
		t.Fatalf("refused creation must not register a room, got %v", room)
	}

	// Re-entering a room the user already has is never refused.
	if _, err := hub.EnsureRoom("room-a1", "user-a", 2); err != nil {
		t.Fatalf("existing room: %v", err)
	}

	// Another user has an independent budget.
	if _, err := hub.EnsureRoom("room-b1", "user-b", 2); err != nil {
		t.Fatalf("user-b first room: %v", err)
	}
	owner, claimed, err := hub.RoomOwner("room-b1")
	if err != nil || !claimed || owner != "user-b" {
		t.Fatalf("user-b ownership: owner=%q claimed=%v err=%v", owner, claimed, err)
	}
}

// TestEnsureRoomQuotaDisabledWhenZero pins that a zero cap restores the
// pre-P0.8 unbounded behaviour.
func TestEnsureRoomQuotaDisabledWhenZero(t *testing.T) {
	hub := NewEngineHub(nil, nil, nil)
	for i := 0; i < 5; i++ {
		if _, err := hub.EnsureRoom(fmt.Sprintf("room-%d", i), "user-a", 0); err != nil {
			t.Fatalf("room-%d: %v", i, err)
		}
	}
	if got := hub.RoomCount(); got != 5 {
		t.Fatalf("live rooms=%d, want 5", got)
	}
}

// TestEnsureRoomWithoutOwnerNeverClaimsOrCounts pins the single-tenant/dev
// path: no identity means no claim and no quota accounting.
func TestEnsureRoomWithoutOwnerNeverClaimsOrCounts(t *testing.T) {
	hub := NewEngineHub(nil, nil, nil)
	for _, id := range []string{"anon-1", "anon-2"} {
		if _, err := hub.EnsureRoom(id, "", 1); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	if owner, claimed, err := hub.RoomOwner("anon-1"); err != nil || claimed || owner != "" {
		t.Fatalf("anonymous room ownership: owner=%q claimed=%v err=%v", owner, claimed, err)
	}
}


// TestEvictIdleRooms pins the eviction policy: a fresh room survives, an
// attached client or an in-flight generation blocks eviction, a quiet room past
// the TTL is stopped and unregistered, the second sweep is a no-op, and a zero
// TTL disables eviction entirely.
func TestEvictIdleRooms(t *testing.T) {
	hub := NewEngineHub(nil, nil, nil)
	room := hub.GetOrCreateRoom("idle-1")

	if ids := hub.EvictIdleRooms(time.Now(), time.Hour); len(ids) != 0 {
		t.Fatalf("fresh room evicted: %v", ids)
	}

	// A connected client is live traffic, not idleness.
	room.clientCount.Store(1)
	if ids := hub.EvictIdleRooms(time.Now().Add(2*time.Hour), time.Hour); len(ids) != 0 {
		t.Fatalf("room with a client evicted: %v", ids)
	}
	room.clientCount.Store(0)

	// A long-running generation is work, not abandonment.
	room.generating.Store(true)
	if ids := hub.EvictIdleRooms(time.Now().Add(2*time.Hour), time.Hour); len(ids) != 0 {
		t.Fatalf("generating room evicted: %v", ids)
	}
	room.generating.Store(false)

	// Touch() resets the idle clock.
	room.Touch()
	if ids := hub.EvictIdleRooms(time.Now().Add(30*time.Minute), time.Hour); len(ids) != 0 {
		t.Fatalf("recently touched room evicted: %v", ids)
	}

	ids := hub.EvictIdleRooms(time.Now().Add(2*time.Hour), time.Hour)
	if len(ids) != 1 || ids[0] != "idle-1" {
		t.Fatalf("evicted ids=%v, want [idle-1]", ids)
	}
	if got := hub.RoomCount(); got != 0 {
		t.Fatalf("after eviction: %d live rooms, want 0", got)
	}
	select {
	case <-room.done:
	default:
		t.Fatal("evicted room's Run loop is still alive")
	}
	if ids := hub.EvictIdleRooms(time.Now().Add(3*time.Hour), time.Hour); len(ids) != 0 {
		t.Fatalf("second sweep evicted %v, want none", ids)
	}

	// TTL 0 disables eviction.
	hub.GetOrCreateRoom("keep-1")
	if ids := hub.EvictIdleRooms(time.Now().Add(24*time.Hour), 0); len(ids) != 0 {
		t.Fatalf("ttl=0 evicted %v", ids)
	}
	if got := hub.RoomCount(); got != 1 {
		t.Fatalf("ttl=0: %d live rooms, want 1", got)
	}
}

// TestEvictIdleRoomsKeepsOwnerWithoutRedis pins the ownership consequence of
// eviction: without a Redis ownership record the in-memory claim is the only
// record, so eviction must not drop it (otherwise the room could be re-claimed
// by someone else). With Redis the claim is re-read from room:owner:<chatID>.
func TestEvictIdleRoomsKeepsOwnerWithoutRedis(t *testing.T) {
	hub := NewEngineHub(nil, nil, nil)
	if _, err := hub.EnsureRoom("owned-1", "user-a", 10); err != nil {
		t.Fatalf("create: %v", err)
	}
	if ids := hub.EvictIdleRooms(time.Now().Add(2*time.Hour), time.Hour); len(ids) != 1 {
		t.Fatalf("evicted ids=%v, want one", ids)
	}
	owner, claimed, err := hub.RoomOwner("owned-1")
	if err != nil || !claimed || owner != "user-a" {
		t.Fatalf("after eviction: owner=%q claimed=%v err=%v, want user-a/true", owner, claimed, err)
	}
}

// TestRoomLifecyclePolicyFromEnv pins the env contract: defaults when unset,
// parsed values when valid, defaults (not fatal) when invalid, and 0 meaning
// "disabled".
func TestRoomLifecyclePolicyFromEnv(t *testing.T) {
	env := func(values map[string]string) func(string, string) string {
		return func(key, fallback string) string {
			if v, ok := values[key]; ok {
				return v
			}
			return fallback
		}
	}

	if got := RoomLifecyclePolicyFromEnv(env(nil)); got != DefaultRoomLifecyclePolicy() {
		t.Fatalf("unset: %+v, want %+v", got, DefaultRoomLifecyclePolicy())
	}

	got := RoomLifecyclePolicyFromEnv(env(map[string]string{
		"ROOM_IDLE_TTL":       "2h",
		"ROOM_MAX_PER_USER":   "3",
		"ROOM_SWEEP_INTERVAL": "30s",
	}))
	want := RoomLifecyclePolicy{IdleTTL: 2 * time.Hour, MaxRoomsPerUser: 3, SweepInterval: 30 * time.Second}
	if got != want {
		t.Fatalf("valid env: %+v, want %+v", got, want)
	}

	bad := RoomLifecyclePolicyFromEnv(env(map[string]string{
		"ROOM_IDLE_TTL":       "soon",
		"ROOM_MAX_PER_USER":   "many",
		"ROOM_SWEEP_INTERVAL": "-5s",
	}))
	if bad != DefaultRoomLifecyclePolicy() {
		t.Fatalf("invalid env: %+v, want defaults %+v", bad, DefaultRoomLifecyclePolicy())
	}

	off := RoomLifecyclePolicyFromEnv(env(map[string]string{
		"ROOM_IDLE_TTL":     "0",
		"ROOM_MAX_PER_USER": "0",
	}))
	if off.IdleTTL != 0 || off.MaxRoomsPerUser != 0 {
		t.Fatalf("disabled env: %+v, want idle ttl 0 and cap 0", off)
	}
}

// TestRegisterClientAfterStopDoesNotBlock pins the janitor race: eviction can
// stop a room between a WebSocket attach's lookup and its RegisterClient call,
// so the actor channels must degrade to a no-op instead of blocking that
// goroutine forever.
func TestRegisterClientAfterStopDoesNotBlock(t *testing.T) {
	hub := NewEngineHub(nil, nil, nil)
	room := hub.GetOrCreateRoom("stopped-1")
	room.Stop()

	done := make(chan struct{})
	go func() {
		room.RegisterClient(NewClient(nil))
		room.UnregisterClient(NewClient(nil))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RegisterClient/UnregisterClient blocked on a stopped room")
	}
}
