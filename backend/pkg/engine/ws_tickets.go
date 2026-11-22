package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"strings"
	"time"
)

// P0-SEC-008 — short-lived, single-use WebSocket tickets.
//
// The control plane used to put the caller's long-lived Edge session token in
// the WebSocket URL (`?ticket=<session-token>`). Query strings are recorded by
// reverse proxies and CDNs and land in browser history, so that practice leaked
// a 7-day bearer credential into logs.
//
// A ticket is now a separate, narrow credential:
//
//   - random 32-byte value, unrelated to the session token;
//   - short TTL (WSTicketTTL, 15s — the lifetime the SDK already documents);
//   - single use: RedeemWSTicket deletes it atomically;
//   - bound to one (userID, chatID) pair, so a leaked ticket cannot be replayed
//     against another room.
//
// Storage mirrors the P0.4 ownership model: the in-memory map is authoritative
// for the in-process race, Redis is the cross-instance backstop (and the only
// store when the map has been evicted by a restart).

// WSTicketTTL is how long an issued ticket stays redeemable.
const WSTicketTTL = 15 * time.Second

// wsTicketKeyPrefix is the Redis key prefix for a ticket.
const wsTicketKeyPrefix = "ws:ticket:"

// wsTicket is one pending ticket.
type wsTicket struct {
	userID    string
	chatID    string
	expiresAt time.Time
}

// WSTicketRedeemer consumes a ticket exactly once. *EngineHub implements it.
type WSTicketRedeemer interface {
	RedeemWSTicket(ticket string) (userID, chatID string, ok bool, err error)
}

// IssueWSTicket mints a single-use ticket bound to (userID, chatID).
func (h *EngineHub) IssueWSTicket(userID, chatID string) (string, time.Time, error) {
	if userID == "" || chatID == "" {
		return "", time.Time{}, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	ticket := hex.EncodeToString(raw)
	expiresAt := time.Now().Add(WSTicketTTL)

	h.wsTicketsMu.Lock()
	h.sweepWSTicketsLocked(time.Now())
	h.wsTickets[ticket] = wsTicket{userID: userID, chatID: chatID, expiresAt: expiresAt}
	h.wsTicketsMu.Unlock()

	if h.rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.rdb.Set(ctx, wsTicketKeyPrefix+ticket, userID+"|"+chatID, WSTicketTTL).Err(); err != nil {
			// Best effort: the in-memory ticket still works on this instance.
			log.Printf("[hub] failed to persist ws ticket: %v", err)
		}
	}
	return ticket, expiresAt, nil
}

// RedeemWSTicket consumes a ticket. It reports ok=false for an unknown, expired
// or already-redeemed ticket — callers must treat that as unauthenticated.
func (h *EngineHub) RedeemWSTicket(ticket string) (userID, chatID string, ok bool, err error) {
	if ticket == "" {
		return "", "", false, nil
	}

	h.wsTicketsMu.Lock()
	pending, found := h.wsTickets[ticket]
	if found {
		delete(h.wsTickets, ticket) // single use
	}
	h.wsTicketsMu.Unlock()

	if found {
		if time.Now().After(pending.expiresAt) {
			return "", "", false, nil
		}
		return pending.userID, pending.chatID, true, nil
	}

	if h.rdb == nil {
		return "", "", false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// GetDel is atomic: two instances cannot both redeem the same ticket.
	value, gerr := h.rdb.GetDel(ctx, wsTicketKeyPrefix+ticket).Result()
	if gerr != nil {
		if strings.Contains(gerr.Error(), "redis: nil") {
			return "", "", false, nil
		}
		return "", "", false, gerr
	}
	parts := strings.SplitN(value, "|", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", false, nil
	}
	return parts[0], parts[1], true, nil
}

// sweepWSTicketsLocked drops expired entries so the map cannot grow without
// bound. Must be called with wsTicketsMu held.
func (h *EngineHub) sweepWSTicketsLocked(now time.Time) {
	for ticket, pending := range h.wsTickets {
		if now.After(pending.expiresAt) {
			delete(h.wsTickets, ticket)
		}
	}
}
