package engine

import "testing"

// TestClaimRoomOwnerFirstWriteWins pins the P0.4 ownership contract on the hub
// registry: the first caller owns the room, a re-claim by the same caller is
// idempotent, and a later different caller is refused (the existing owner is
// returned). The in-memory registry is the authority inside a process; Redis
// persistence runs through the same code path when rdb is set.
func TestClaimRoomOwnerFirstWriteWins(t *testing.T) {
	hub := NewEngineHub(nil, nil, nil)

	owner, ok, err := hub.ClaimRoomOwner("room-1", "user-a")
	if err != nil {
		t.Fatalf("claim user-a: %v", err)
	}
	if !ok || owner != "user-a" {
		t.Fatalf("first claim: got owner=%q ok=%v, want user-a/true", owner, ok)
	}

	// Same caller re-claiming is idempotent and still owns.
	owner, ok, err = hub.ClaimRoomOwner("room-1", "user-a")
	if err != nil {
		t.Fatalf("re-claim user-a: %v", err)
	}
	if !ok || owner != "user-a" {
		t.Fatalf("re-claim by owner: got owner=%q ok=%v, want user-a/true", owner, ok)
	}

	// A different caller loses the claim: the existing owner is returned.
	owner, ok, err = hub.ClaimRoomOwner("room-1", "user-b")
	if err != nil {
		t.Fatalf("claim user-b: %v", err)
	}
	if ok || owner != "user-a" {
		t.Fatalf("second claim: got owner=%q ok=%v, want user-a/false", owner, ok)
	}

	owner, claimed, err := hub.RoomOwner("room-1")
	if err != nil {
		t.Fatalf("room owner: %v", err)
	}
	if !claimed || owner != "user-a" {
		t.Fatalf("RoomOwner: got owner=%q claimed=%v, want user-a/true", owner, claimed)
	}
}

// TestRoomOwnerUnclaimed pins the negative lookup: an unknown room reports no
// owner (so the route guard answers 404 rather than leaking its existence).
func TestRoomOwnerUnclaimed(t *testing.T) {
	hub := NewEngineHub(nil, nil, nil)
	owner, claimed, err := hub.RoomOwner("never-claimed")
	if err != nil {
		t.Fatalf("room owner: %v", err)
	}
	if claimed || owner != "" {
		t.Fatalf("expected no owner, got owner=%q claimed=%v", owner, claimed)
	}
}

// TestClaimRoomOwnerEmptyArgs pins that an empty room id or user id never claims
// (so a misconfigured guard cannot mint an anonymous ownership record).
func TestClaimRoomOwnerEmptyArgs(t *testing.T) {
	hub := NewEngineHub(nil, nil, nil)
	for _, tc := range []struct{ chatID, userID string }{
		{"", "user-a"},
		{"room-1", ""},
		{"", ""},
	} {
		owner, ok, err := hub.ClaimRoomOwner(tc.chatID, tc.userID)
		if err != nil {
			t.Fatalf("claim(%q,%q): %v", tc.chatID, tc.userID, err)
		}
		if ok || owner != "" {
			t.Fatalf("claim(%q,%q): got owner=%q ok=%v, want \"\"/false", tc.chatID, tc.userID, owner, ok)
		}
	}
}
