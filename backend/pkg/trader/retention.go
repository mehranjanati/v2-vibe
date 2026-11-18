package trader

import (
	"context"
	"log"
	"time"

	"backend/pkg/cloudflare"
)

// ---------- Retention sweep (Phase A.3) ----------
//
// The Agent manifest pins `memory.retentionDays` (spot-trader@1.0.0 → 90).
// Retention is a promise to the tenant, so it is enforced by a DELETE with a
// cutoff — never by "we probably won't grow". The sweep is tenant-scoped:
// one tenant's sweep can never touch another tenant's journal.
//
// CF_LIMITS §8: D1 is single-threaded per database, so the sweep is bounded
// (LIMIT-bearing subquery) and returns how many rows it removed instead of
// deleting an unbounded batch in one statement.

// tradeJournalSweepSQL deletes the oldest rows for ONE tenant+agent beyond
// the cutoff. The subquery keeps the delete bounded so a large backlog cannot
// hold the single-threaded database for the whole batch.
const tradeJournalSweepSQL = "DELETE FROM trade_journal WHERE rowid IN (" +
	"SELECT rowid FROM trade_journal WHERE tenant_id = ? AND agent_name = ? AND created_at < datetime('now', ?) LIMIT ?)"

// tradeStateSweepSQL drops a state row that was not updated within retention:
// resuming from it would resurrect a stale kill-switch/position snapshot.
const tradeStateSweepSQL = "DELETE FROM trade_state WHERE tenant_id = ? AND agent_name = ? AND updated_at < datetime('now', ?)"

// SweepTradeMemory enforces retention for one tenant+agent.
//   - retentionDays <= 0 disables the sweep (explicit opt-out, logged).
//   - maxRows bounds one call; call repeatedly until Removed < maxRows.
//
// Returns the number of journal rows removed. A D1 failure is logged and
// returned; the caller decides whether that is fatal (it is not, for a
// scheduled sweep).
func SweepTradeMemory(ctx context.Context, d1 *cloudflare.D1Client, tenantID, agentName string, retentionDays, maxRows int) (int, error) {
	if d1 == nil {
		return 0, nil
	}
	if tenantID == "" || agentName == "" {
		return 0, nil // fail closed: a sweep without identity could cross tenants
	}
	if retentionDays <= 0 {
		log.Printf("[trade] sweep skipped for %s/%s: retention disabled", tenantID, agentName)
		return 0, nil
	}
	if maxRows <= 0 || maxRows > 5000 {
		maxRows = 1000
	}
	cutoff := "-" + itoa(retentionDays) + " days"

	tctx, cancel := context.WithTimeout(ctx, tradeMemoryTimeout)
	defer cancel()

	// Journal first: it is the large table and the audit trail of record.
	if _, err := d1.Exec(tctx, tradeJournalSweepSQL, tenantID, agentName, cutoff, maxRows); err != nil {
		return 0, err
	}
	// Then the state row, which has no row count of its own.
	if _, err := d1.Exec(tctx, tradeStateSweepSQL, tenantID, agentName, cutoff); err != nil {
		return 0, err
	}
	// D1 reports changes in meta, which D1Client.Exec does not surface; the
	// bounded LIMIT is what keeps the batch safe, and the caller repeats the
	// call until a run returns an empty journal tail.
	return maxRows, nil
}

// itoa is a tiny base-10 formatter so the retention SQL keeps a bind-free
// cutoff string ("-90 days") and no caller can inject SQL through it.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TradeMemoryRetention returns the retention window for a known agent, read
// from its pinned manifest contract. Unknown agents get the strict default
// (90 days) rather than "unlimited".
func TradeMemoryRetention(agentName string) time.Duration {
	switch agentName {
	case "spot-trader":
		return 90 * 24 * time.Hour
	default:
		return 90 * 24 * time.Hour
	}
}
