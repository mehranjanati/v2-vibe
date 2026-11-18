package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
)

// testClaimStore returns a store on a controllable clock, so the TTL case is
// deterministic instead of a sleep.
func testClaimStore() (*claimStore, func(time.Duration)) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	store := newClaimStore()
	store.now = func() time.Time { return now }
	return store, func(d time.Duration) { now = now.Add(d) }
}

func TestClaimStoreGrantsDeniesAndRefreshes(t *testing.T) {
	store, advance := testClaimStore()

	if granted, owner := store.claim("src/app.ts", "coder"); !granted || owner != "coder" {
		t.Fatalf("first claim = (%v, %q), want granted for coder", granted, owner)
	}
	// Another holder is refused and told who owns the path.
	if granted, owner := store.claim("src/app.ts", "reviewer"); granted || owner != "coder" {
		t.Fatalf("second claim = (%v, %q), want denied with owner coder", granted, owner)
	}
	// The same holder re-claiming refreshes the record instead of colliding.
	advance(5 * time.Minute)
	if granted, _ := store.claim("src/app.ts", "coder"); !granted {
		t.Fatal("re-claim by the same holder must be granted")
	}
	views := store.view()
	if len(views) != 1 || views[0].AgeSeconds != 0 {
		t.Fatalf("refresh did not reset the age: %+v", views)
	}
	if views[0].ExpiresInSeconds != int(claimTTLDefault.Seconds()) {
		t.Errorf("ExpiresInSeconds = %d, want the full TTL after a refresh", views[0].ExpiresInSeconds)
	}
}

func TestClaimStoreReleaseIsOwnerOnly(t *testing.T) {
	store, _ := testClaimStore()
	store.claim("index.html", "coder")

	if store.release("index.html", "reviewer") {
		t.Fatal("a non-holder must not release another holder's claim")
	}
	if !store.release("index.html", "coder") {
		t.Fatal("the holder must be able to release")
	}
	if granted, _ := store.claim("index.html", "reviewer"); !granted {
		t.Fatal("after release another holder may claim the path")
	}
}

func TestClaimStoreExpires(t *testing.T) {
	store, advance := testClaimStore()
	store.claim("styles.css", "coder")

	advance(claimTTLDefault - time.Second)
	if granted, _ := store.claim("styles.css", "reviewer"); granted {
		t.Fatal("the claim must still be live one second before the TTL")
	}
	advance(2 * time.Second)
	if granted, _ := store.claim("styles.css", "reviewer"); !granted {
		t.Fatal("an expired claim must be reclaimable by the next holder")
	}
}

func TestClaimWritePathEnforcesOwnership(t *testing.T) {
	store, _ := testClaimStore()
	if err := store.claimWritePath("index.html", "coder"); err != nil {
		t.Fatalf("free path: %v", err)
	}
	err := store.claimWritePath("index.html", "gapfill")
	if err == nil {
		t.Fatal("a write by another holder must be refused")
	}
	if !strings.Contains(err.Error(), `"coder"`) {
		t.Errorf("error %q should name the current owner", err)
	}
	// The same writer may keep writing its own path.
	if err := store.claimWritePath("index.html", "coder"); err != nil {
		t.Fatalf("same holder write: %v", err)
	}

	// Through the VFS store, which is the path the tools actually use.
	room := NewProjectRoom("claims-write", nil, nil, nil)
	coder := roomVFSStore{r: room, author: writeAuthorCoder}
	if err := coder.VFSWrite(context.Background(), "chat", "index.html", "one"); err != nil {
		t.Fatalf("coder write: %v", err)
	}
	gapfill := roomVFSStore{r: room, author: writeAuthorGapfill}
	if err := gapfill.VFSWrite(context.Background(), "chat", "index.html", "two"); err == nil {
		t.Fatal("gapfill must not overwrite a path the coder holds")
	}
	if got := room.GetVFS()["index.html"]; got == nil || got.FileContents != "one" {
		t.Fatalf("refused write still landed: %#v", got)
	}
}

// TestClaimToolsInvokable covers the model-facing contract of P1.9.1.
func TestClaimToolsInvokable(t *testing.T) {
	room := NewProjectRoom("claims-tools", nil, nil, nil)
	toolsList, err := room.claimTools()
	if err != nil {
		t.Fatalf("claimTools: %v", err)
	}
	if len(toolsList) != 3 {
		t.Fatalf("expected claim/release/status, got %d", len(toolsList))
	}
	byName := map[string]tool.InvokableTool{}
	for _, tl := range toolsList {
		info, ierr := tl.Info(context.Background())
		if ierr != nil {
			t.Fatalf("tool info: %v", ierr)
		}
		inv, ok := tl.(tool.InvokableTool)
		if !ok {
			t.Fatalf("%s is not invokable", info.Name)
		}
		byName[info.Name] = inv
	}
	ctx := context.Background()

	out, err := byName["vfs_claim"].InvokableRun(ctx, `{"path":"src/app.ts","holder":"coder"}`)
	if err != nil || !strings.Contains(out, `"granted":true`) {
		t.Fatalf("claim = (%q, %v), want granted", out, err)
	}
	out, err = byName["vfs_claim"].InvokableRun(ctx, `{"path":"src/app.ts","holder":"reviewer"}`)
	if err != nil || !strings.Contains(out, `"granted":false`) {
		t.Fatalf("second claim = (%q, %v), want denied", out, err)
	}
	if !strings.Contains(out, `"owner":"coder"`) {
		t.Errorf("denial %q should name the owner", out)
	}

	out, err = byName["vfs_status"].InvokableRun(ctx, `{}`)
	if err != nil || !strings.Contains(out, `"src/app.ts"`) || !strings.Contains(out, `"count":1`) {
		t.Fatalf("status = (%q, %v), want the one claim", out, err)
	}

	if out, err = byName["vfs_release"].InvokableRun(ctx, `{"path":"src/app.ts","holder":"reviewer"}`); err != nil || !strings.Contains(out, `"released":false`) {
		t.Fatalf("foreign release = (%q, %v), want refused", out, err)
	}
	if out, err = byName["vfs_release"].InvokableRun(ctx, `{"path":"src/app.ts","holder":"coder"}`); err != nil || !strings.Contains(out, `"released":true`) {
		t.Fatalf("owner release = (%q, %v), want released", out, err)
	}
	if views := room.claims.view(); len(views) != 0 {
		t.Errorf("claims left after release: %+v", views)
	}
}

// TestFinalizeReleasesClaims pins the orphan guard on the path that actually
// runs: finalizeGeneration, including its early return when no files parsed.
func TestFinalizeReleasesClaims(t *testing.T) {
	room := NewProjectRoom("claims-finalize", nil, nil, nil)
	if granted, _ := room.claims.claim("index.html", "coder"); !granted {
		t.Fatal("setup claim failed")
	}
	room.finalizeGeneration("", 0) // no files parsed -> early return
	if views := room.claims.view(); len(views) != 0 {
		t.Fatalf("finalize left claims behind: %+v", views)
	}
}
