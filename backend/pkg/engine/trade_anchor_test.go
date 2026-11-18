package engine

import (
	"context"
	"strings"
	"testing"
)

// TestAnchorAppIDSanitization: the repo id is derived from identity and must be
// safe as a D1 key and a ref namespace.
func TestAnchorAppIDSanitization(t *testing.T) {
	got := AnchorAppID("Tenant_A/../x", "Spot Trader")
	if got != "trade-tenant-a-x-spot-trader" {
		t.Fatalf("unexpected app id: %q", got)
	}
	if strings.ContainsAny(got, " _/.") {
		t.Fatalf("app id must be sanitized: %q", got)
	}
	if AnchorAppID("t1", "spot-trader") == AnchorAppID("t2", "spot-trader") {
		t.Fatal("distinct tenants must map to distinct repos")
	}
}

// TestGitTradeAnchorCommitsHead: the adapter creates a REAL Git commit over the
// D1-CAS store and returns its sha, so the chain head is sealed outside the
// mutable journal table.
func TestGitTradeAnchorCommitsHead(t *testing.T) {
	fake, d1 := newFakeGitD1(t)
	anchor := tradeAnchorOrNil(d1)
	if anchor == nil {
		t.Fatal("expected an anchor when D1 is configured")
	}
	ctx := context.Background()
	head := strings.Repeat("ab", 32)

	sha, err := anchor.AnchorTradeJournal(ctx, "tenant-a", "spot-trader", head, 7)
	if err != nil {
		t.Fatalf("anchor: %v", err)
	}
	if len(sha) != 40 {
		t.Fatalf("expected a real 40-char git sha, got %q", sha)
	}
	appID := AnchorAppID("tenant-a", "spot-trader")
	if fake.objectCount(appID) == 0 {
		t.Fatal("anchor must persist git objects")
	}
	if fake.kindCount(appID, "commit") != 1 {
		t.Fatalf("expected one commit object, have %d", fake.kindCount(appID, "commit"))
	}

	// A second anchor appends a commit on top (history is never rewritten).
	sha2, err := anchor.AnchorTradeJournal(ctx, "tenant-a", "spot-trader", strings.Repeat("cd", 32), 9)
	if err != nil {
		t.Fatalf("second anchor: %v", err)
	}
	if sha2 == sha {
		t.Fatal("second anchor must produce a new commit")
	}
	if fake.kindCount(appID, "commit") != 2 {
		t.Fatalf("expected two commits, have %d", fake.kindCount(appID, "commit"))
	}
}

// TestGitTradeAnchorRejectsEmptyHead: anchoring nothing is an error, not a
// no-op that would produce a meaningless commit.
func TestGitTradeAnchorRejectsEmptyHead(t *testing.T) {
	_, d1 := newFakeGitD1(t)
	anchor := tradeAnchorOrNil(d1)
	if _, err := anchor.AnchorTradeJournal(context.Background(), "t1", "spot-trader", "", 0); err == nil {
		t.Fatal("empty head must be rejected")
	}
}

// TestTradeAnchorNilWithoutD1: the dev path never pretends to anchor.
func TestTradeAnchorNilWithoutD1(t *testing.T) {
	if tradeAnchorOrNil(nil) != nil {
		t.Fatal("no D1 means no anchor")
	}
}
