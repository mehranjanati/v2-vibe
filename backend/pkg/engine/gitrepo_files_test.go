package engine

import (
	"context"
	"reflect"
	"testing"
)

// TestFilesAtRoundTrip pins the P1.4 contract that rollback depends on: a
// revision can be materialised back into the exact path → contents map that was
// committed, nested directories included, and an older generation ref still
// resolves to its own tree after HEAD moved on.
func TestFilesAtRoundTrip(t *testing.T) {
	ctx := context.Background()
	_, d1 := newFakeGitD1(t)
	repo, err := OpenGitRepo(ctx, d1, "app-files")
	if err != nil {
		t.Fatalf("OpenGitRepo: %v", err)
	}

	first := map[string]string{
		"index.html":       "<h1>one</h1>",
		"assets/app.js":    "console.log(1)",
		"assets/css/x.css": "body{color:red}",
	}
	sha1, err := repo.Commit(ctx, GitCommitRequest{
		Message:      "generation gen-1",
		Author:       "coder",
		GenerationID: "gen-1",
		Files:        first,
	})
	if err != nil {
		t.Fatalf("commit gen-1: %v", err)
	}

	got, err := repo.FilesAt(ctx, sha1)
	if err != nil {
		t.Fatalf("FilesAt(gen-1): %v", err)
	}
	if !reflect.DeepEqual(got, first) {
		t.Fatalf("FilesAt(gen-1) = %#v, want %#v", got, first)
	}

	second := map[string]string{"index.html": "<h1>two</h1>"}
	sha2, err := repo.Commit(ctx, GitCommitRequest{
		Message:      "generation gen-2",
		Author:       "coder",
		GenerationID: "gen-2",
		Files:        second,
	})
	if err != nil {
		t.Fatalf("commit gen-2: %v", err)
	}
	got2, err := repo.FilesAt(ctx, sha2)
	if err != nil {
		t.Fatalf("FilesAt(gen-2): %v", err)
	}
	if !reflect.DeepEqual(got2, second) {
		t.Fatalf("FilesAt(gen-2) = %#v, want %#v (deleted paths must disappear)", got2, second)
	}

	// The per-generation ref still addresses the old tree after HEAD moved.
	again, err := repo.FilesAt(ctx, gitGenRefPrefix+"gen-1")
	if err != nil {
		t.Fatalf("FilesAt(refs/gen/gen-1): %v", err)
	}
	if !reflect.DeepEqual(again, first) {
		t.Fatalf("FilesAt(refs/gen/gen-1) = %#v, want %#v", again, first)
	}

	// And the rollback primitive itself round-trips: revert to gen-1 and the new
	// HEAD materialises the old working set.
	reverted, err := repo.Revert(ctx, sha1, "rollback")
	if err != nil {
		t.Fatalf("Revert(gen-1): %v", err)
	}
	restored, err := repo.FilesAt(ctx, reverted)
	if err != nil {
		t.Fatalf("FilesAt(revert): %v", err)
	}
	if !reflect.DeepEqual(restored, first) {
		t.Fatalf("FilesAt(revert) = %#v, want the gen-1 tree %#v", restored, first)
	}
}
