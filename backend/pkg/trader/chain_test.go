package trader

import (
	"context"
	"strings"
	"testing"
)

// fakeAnchor records the heads it was asked to seal.
type fakeAnchor struct {
	calls []string // "tenant/agent/head/rows"
	err   error
}

func (f *fakeAnchor) AnchorTradeJournal(_ context.Context, tenantID, agentName, headHash string, rows int) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.calls = append(f.calls, tenantID+"/"+agentName+"/"+headHash+"/"+itoaTest(rows))
	return "sha:anchor", nil
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestChainVerifiesWhatItWrote: appends form a valid chain from genesis.
func TestChainVerifiesWhatItWrote(t *testing.T) {
	f, d1 := newFakeTradeD1(t)
	ctx := context.Background()
	for _, ev := range []string{TradeEventSignal, TradeEventApproval, TradeEventFill} {
		AppendTradeEvent(ctx, d1, "t1", "spot-trader", "run-1", ev, map[string]interface{}{"k": ev})
	}
	// Genesis link: the first row must have an empty prev_hash.
	f.mu.Lock()
	if f.journal[0].prev != genesisHash {
		t.Fatalf("first row must start at genesis, got %q", f.journal[0].prev)
	}
	// Each later row points at its predecessor.
	for i := 1; i < len(f.journal); i++ {
		if f.journal[i].prev != f.journal[i-1].hash {
			t.Fatalf("row %d does not link to row %d", i, i-1)
		}
	}
	f.mu.Unlock()

	st, err := VerifyChain(ctx, d1, "t1", "spot-trader", 100)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !st.OK || st.Rows != 3 {
		t.Fatalf("expected OK over 3 rows, got %+v", st)
	}
	if st.LastHash == "" {
		t.Fatal("last hash must be the anchoring value")
	}
}

// TestChainDetectsEditedRow: editing a stored detail breaks the row digest.
func TestChainDetectsEditedRow(t *testing.T) {
	f, d1 := newFakeTradeD1(t)
	ctx := context.Background()
	AppendTradeEvent(ctx, d1, "t1", "spot-trader", "run-1", TradeEventSignal, map[string]interface{}{"signal": "flat"})
	AppendTradeEvent(ctx, d1, "t1", "spot-trader", "run-1", TradeEventFill, map[string]interface{}{"qty": 1})

	// An attacker rewrites history: flat becomes long.
	f.mu.Lock()
	f.journal[0].detail = `{"signal":"long"}`
	f.mu.Unlock()

	st, err := VerifyChain(ctx, d1, "t1", "spot-trader", 100)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if st.OK {
		t.Fatal("edited row must break the chain")
	}
	if st.Reason != "row-hash-mismatch" || st.BrokenAt != 1 {
		t.Fatalf("expected mismatch at row 1, got %+v", st)
	}
}

// TestChainDetectsDeletedRow: dropping the middle row breaks the link, even
// when every remaining row still hashes correctly on its own.
func TestChainDetectsDeletedRow(t *testing.T) {
	f, d1 := newFakeTradeD1(t)
	ctx := context.Background()
	AppendTradeEvent(ctx, d1, "t1", "spot-trader", "run-1", TradeEventSignal, nil)
	AppendTradeEvent(ctx, d1, "t1", "spot-trader", "run-1", TradeEventApproval, nil)
	AppendTradeEvent(ctx, d1, "t1", "spot-trader", "run-1", TradeEventFill, nil)

	f.mu.Lock()
	f.journal = append(f.journal[:1], f.journal[2:]...) // drop the approval row
	f.mu.Unlock()

	st, _ := VerifyChain(ctx, d1, "t1", "spot-trader", 100)
	if st.OK || st.Reason != "link-broken" {
		t.Fatalf("deleted row must break the link, got %+v", st)
	}
}

// TestChainRejectsUnchainedRow: a row inserted without a hash (bypassing
// appendChained) fails verification instead of being trusted.
func TestChainRejectsUnchainedRow(t *testing.T) {
	f, d1 := newFakeTradeD1(t)
	ctx := context.Background()
	AppendTradeEvent(ctx, d1, "t1", "spot-trader", "run-1", TradeEventSignal, nil)
	f.mu.Lock()
	f.journal = append(f.journal, fakeTradeJournalRow{id: "forged", tenant: "t1", agent: "spot-trader", runID: "run-x", event: "fill"})
	f.mu.Unlock()

	st, _ := VerifyChain(ctx, d1, "t1", "spot-trader", 100)
	if st.OK || st.Reason != "row-hash-missing" {
		t.Fatalf("unchained row must fail, got %+v", st)
	}
}

// TestChainIsTenantScoped: one tenant's tampering never touches another chain.
func TestChainIsTenantScoped(t *testing.T) {
	f, d1 := newFakeTradeD1(t)
	ctx := context.Background()
	AppendTradeEvent(ctx, d1, "t1", "spot-trader", "run-1", TradeEventSignal, map[string]interface{}{"a": 1})
	AppendTradeEvent(ctx, d1, "t2", "spot-trader", "run-2", TradeEventSignal, map[string]interface{}{"b": 2})

	f.mu.Lock()
	for i := range f.journal {
		if f.journal[i].tenant == "t1" {
			f.journal[i].detail = `{"a":999}`
		}
	}
	f.mu.Unlock()

	if st, _ := VerifyChain(ctx, d1, "t2", "spot-trader", 100); !st.OK {
		t.Fatalf("t2 chain must stay valid: %+v", st)
	}
	if st, _ := VerifyChain(ctx, d1, "t1", "spot-trader", 100); st.OK {
		t.Fatal("t1 chain must fail")
	}
}

// TestAnchorSealsVerifiedHeadOnly: a good chain is anchored with its head; a
// broken chain is never anchored (sealing corrupt history would bless it).
func TestAnchorSealsVerifiedHeadOnly(t *testing.T) {
	f, d1 := newFakeTradeD1(t)
	ctx := context.Background()
	AppendTradeEvent(ctx, d1, "t1", "spot-trader", "run-1", TradeEventSignal, nil)

	anchor := &fakeAnchor{}
	AnchorHead(ctx, d1, anchor, "t1", "spot-trader")
	if len(anchor.calls) != 1 {
		t.Fatalf("expected one anchor call, got %d", len(anchor.calls))
	}
	if !strings.Contains(anchor.calls[0], "t1/spot-trader/") || !strings.HasSuffix(anchor.calls[0], "/1") {
		t.Fatalf("anchor call malformed: %s", anchor.calls[0])
	}

	f.mu.Lock()
	f.journal[0].detail = `{"tampered":true}`
	f.mu.Unlock()
	AnchorHead(ctx, d1, anchor, "t1", "spot-trader")
	if len(anchor.calls) != 1 {
		t.Fatalf("broken chain must not be anchored, calls=%d", len(anchor.calls))
	}
}
