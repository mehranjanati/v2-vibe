package trader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"backend/pkg/cloudflare"
)

// ---------- Hash chain (Phase A2: tamper-EVIDENCE) ----------
//
// Provenance answers "who claims this?"; it cannot stop a writer who can reach
// D1 from inventing a row that looks legitimate. The hash chain answers the
// different question the memory-injection literature raises: "did anything
// change after it was written?"
//
//	row_hash = sha256(prev_hash ‖ id ‖ tenant ‖ agent ‖ run ‖ event ‖ detail)
//
// Every field is inside the digest, so editing ANY field of ANY row breaks
// that row and every later row. Verification fails closed: a broken chain
// halts the run instead of resuming from suspect history.
//
// Anchoring (see Anchor) pins the chain head into the internal Git repo, so
// even a full-table rewrite cannot reproduce the commit history.
const (
	tradeJournalChainedInsertSQL = "INSERT INTO trade_journal (id, tenant_id, agent_name, run_id, event, detail_json, prev_hash, row_hash) VALUES (?, ?, ?, ?, ?, ?, ?, ?)"
	tradeJournalLastHashSQL      = "SELECT row_hash FROM trade_journal WHERE tenant_id = ? AND agent_name = ? ORDER BY rowid DESC LIMIT 1"
	tradeJournalCountSQL         = "SELECT COUNT(*) AS n FROM trade_journal WHERE tenant_id = ? AND agent_name = ?"
	tradeJournalChainSQL         = "SELECT id, run_id, event, detail_json, prev_hash, row_hash FROM trade_journal WHERE tenant_id = ? AND agent_name = ? ORDER BY rowid DESC LIMIT ?"
	// genesisHash is the prev_hash of the first row of a chain.
	genesisHash = ""
	// fieldSep cannot appear in any field we hash (JSON escapes it, ids and
	// refs are ASCII), so the digest is unambiguous.
	fieldSep = "\x1f"
)

// chainLocks serializes appends per (tenant, agent) inside this process. D1 is
// single-threaded, but a read-then-write pair is not atomic across statements:
// without the lock two concurrent appends would read the same prev_hash and
// fork the chain. The control plane owns one room per chat, so one writer per
// (tenant, agent) is the operating assumption this lock makes explicit.
var chainLocks sync.Map // key string -> *sync.Mutex

func chainMutex(tenantID, agentName string) *sync.Mutex {
	key := tenantID + "\x00" + agentName
	if v, ok := chainLocks.Load(key); ok {
		return v.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	actual, _ := chainLocks.LoadOrStore(key, mu)
	return actual.(*sync.Mutex)
}

// computeRowHash is the chain primitive: reordering, renaming or dropping a
// field changes the digest, which is what makes an edit detectable.
func computeRowHash(prevHash, id, tenantID, agentName, runID, event, detailJSON string) string {
	parts := []string{prevHash, id, tenantID, agentName, runID, event, detailJSON}
	sum := sha256.Sum256([]byte(strings.Join(parts, fieldSep)))
	return hex.EncodeToString(sum[:])
}

// lastRowHash returns the chain head for (tenant, agent): ("", nil) for an
// empty chain. A D1 error is returned, never swallowed — appending on top of
// an unreadable chain would fork it silently.
func lastRowHash(ctx context.Context, d1 *cloudflare.D1Client, tenantID, agentName string) (string, error) {
	rows, err := d1.Query(ctx, tradeJournalLastHashSQL, tenantID, agentName)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return genesisHash, nil
	}
	head, _ := rows[0]["row_hash"].(string)
	return head, nil
}

// journalRowCount returns how many rows the chain has (used by anchoring).
func journalRowCount(ctx context.Context, d1 *cloudflare.D1Client, tenantID, agentName string) (int, error) {
	rows, err := d1.Query(ctx, tradeJournalCountSQL, tenantID, agentName)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	switch v := rows[0]["n"].(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case string:
		n, _ := strconv.Atoi(v)
		return n, nil
	}
	return 0, nil
}

// appendChained locks the (tenant, agent) chain, reads its head, computes the
// new row hash and inserts the row. It is the single write primitive of the
// journal.
func appendChained(ctx context.Context, d1 *cloudflare.D1Client, id, tenantID, agentName, runID, event, detailJSON string) error {
	if d1 == nil {
		return nil
	}
	mu := chainMutex(tenantID, agentName)
	mu.Lock()
	defer mu.Unlock()

	prev, err := lastRowHash(ctx, d1, tenantID, agentName)
	if err != nil {
		return fmt.Errorf("trader: chain head unreadable: %w", err)
	}
	rowHash := computeRowHash(prev, id, tenantID, agentName, runID, event, detailJSON)
	if _, err := d1.Exec(ctx, tradeJournalChainedInsertSQL, id, tenantID, agentName, runID, event, detailJSON, prev, rowHash); err != nil {
		return fmt.Errorf("trader: chain append: %w", err)
	}
	return nil
}

func stringField(row map[string]interface{}, key string) string {
	if v, ok := row[key].(string); ok {
		return v
	}
	return ""
}
