package trader

import (
	"context"
	"log"

	"backend/pkg/cloudflare"
)

// ChainStatus is the result of verifying a chain segment.
type ChainStatus struct {
	// OK is false when a row no longer matches its recorded hash (someone
	// edited history) or when a link does not point at its predecessor.
	OK bool
	// Rows is how many rows were verified.
	Rows int
	// LastHash is the head of the verified segment (the anchoring value).
	LastHash string
	// BrokenAt is the 1-based position (oldest first) of the first bad row.
	BrokenAt int
	// Reason is empty when OK.
	Reason string
}

// chainRow is one row as the verifier needs it (raw strings, no typing).
type chainRow struct {
	id, runID, event, detail, prev, hash string
}

// VerifyChain re-computes the hash of the last `limit` rows for one
// (tenant, agent) and checks every link. It fails closed on any mismatch and on
// any row whose stored hash is empty (a row written before the chain existed,
// or inserted by a writer that bypassed appendChained).
//
// The segment is verified on its internal links plus every row digest. The link
// from the oldest row of the window back to its predecessor is what the anchor
// seals.
func VerifyChain(ctx context.Context, d1 *cloudflare.D1Client, tenantID, agentName string, limit int) (ChainStatus, error) {
	if d1 == nil {
		return ChainStatus{OK: true, Reason: "no-d1"}, nil
	}
	if tenantID == "" || agentName == "" {
		return ChainStatus{OK: false, Reason: "identity-required"}, nil
	}
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := d1.Query(ctx, tradeJournalChainSQL, tenantID, agentName, limit)
	if err != nil {
		return ChainStatus{}, err
	}
	if len(rows) == 0 {
		return ChainStatus{OK: true, Reason: "empty-chain"}, nil
	}

	seq := make([]chainRow, 0, len(rows))
	for _, r := range rows {
		seq = append(seq, chainRow{
			id:     stringField(r, "id"),
			runID:  stringField(r, "run_id"),
			event:  stringField(r, "event"),
			detail: stringField(r, "detail_json"),
			prev:   stringField(r, "prev_hash"),
			hash:   stringField(r, "row_hash"),
		})
	}
	// Rows arrive newest-first; verify oldest-first so "previous" is a row we
	// already checked.
	for i, j := 0, len(seq)-1; i < j; i, j = i+1, j-1 {
		seq[i], seq[j] = seq[j], seq[i]
	}

	for i, row := range seq {
		if row.hash == "" {
			return ChainStatus{Rows: len(seq), BrokenAt: i + 1, Reason: "row-hash-missing"}, nil
		}
		want := computeRowHash(row.prev, row.id, tenantID, agentName, row.runID, row.event, row.detail)
		if want != row.hash {
			return ChainStatus{Rows: len(seq), BrokenAt: i + 1, Reason: "row-hash-mismatch"}, nil
		}
		if i > 0 && row.prev != seq[i-1].hash {
			return ChainStatus{Rows: len(seq), BrokenAt: i + 1, Reason: "link-broken"}, nil
		}
	}
	return ChainStatus{OK: true, Rows: len(seq), LastHash: seq[len(seq)-1].hash}, nil
}

// Anchor pins a verified chain head into durable, append-only storage outside
// D1's mutable table (the internal Git repo in the control plane). It is an
// interface so this package never imports backend/pkg/engine.
type Anchor interface {
	// AnchorTradeJournal records the chain head. An error is reported to the
	// caller but never loses the journal row itself.
	AnchorTradeJournal(ctx context.Context, tenantID, agentName, headHash string, rows int) (ref string, err error)
}

// AnchorHead verifies the chain and anchors its head when an Anchor is wired.
// A broken chain is NOT anchored: sealing corrupt history would bless it.
func AnchorHead(ctx context.Context, d1 *cloudflare.D1Client, anchor Anchor, tenantID, agentName string) {
	if anchor == nil || d1 == nil {
		return
	}
	st, err := VerifyChain(ctx, d1, tenantID, agentName, 500)
	if err != nil {
		log.Printf("[trade] anchor: verify failed: %v", err)
		return
	}
	if !st.OK {
		log.Printf("[trade] anchor refused: chain %s at row %d", st.Reason, st.BrokenAt)
		return
	}
	if st.LastHash == "" {
		return
	}
	// Count the whole chain, not just the verified window: the anchor records
	// how long the history it seals actually is.
	rows := st.Rows
	if n, cerr := journalRowCount(ctx, d1, tenantID, agentName); cerr == nil {
		rows = n
	}
	if _, err := anchor.AnchorTradeJournal(ctx, tenantID, agentName, st.LastHash, rows); err != nil {
		log.Printf("[trade] anchor failed: %v", err)
	}
}
