package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

// newGenerationCommitRoom builds a room wired to the emulated D1 that speaks
// gitrepo.go's statements. No Redis and no engine: P1.3.5 must not depend on
// either (the lineage path is best-effort by contract).
func newGenerationCommitRoom(t *testing.T) (*ProjectRoom, *fakeGitD1) {
	t.Helper()
	fake, d1 := newFakeGitD1(t)
	room := NewProjectRoom("app-1", nil, nil, nil)
	room.d1 = d1
	return room, fake
}

// openedGeneration mirrors what StartGenerationRecord does for the commit
// path: publish the run id and its baseline (an empty baseline = greenfield).
func openedGeneration(room *ProjectRoom, genID string) {
	room.storeGenerationSnapshot(genID, &generationSnapshot{
		ChatID: room.chatID,
		Files:  map[string]snapshotFile{},
	})
}

func TestFinalizeGenerationCommitsOncePerGeneration(t *testing.T) {
	ctx := context.Background()
	room, fake := newGenerationCommitRoom(t)

	openedGeneration(room, "gen-1")
	room.UpsertFile("index.html", "<h1>hi</h1>", "coder")
	room.UpsertFile("src/main.js", "console.log(1)", "coder")

	// finalizeGeneration is where P1.3.5 commits.
	room.finalizeGeneration("", 2)
	// A second finalize for the same run (retry, salvage path) must not add a
	// revision: the generation id is consumed by the first call.
	room.finalizeGeneration("", 2)

	repo, err := OpenGitRepo(ctx, room.d1, room.chatID)
	if err != nil {
		t.Fatalf("OpenGitRepo: %v", err)
	}
	log, err := repo.Log(ctx, gitMainRef, 0)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(log) != 1 {
		t.Fatalf("expected exactly 1 commit for one generation, got %d", len(log))
	}
	if !strings.Contains(log[0].Message, "gen-1") {
		t.Errorf("commit message %q does not name the generation", log[0].Message)
	}
	if log[0].Author != "coder" {
		t.Errorf("commit author = %q, want the single write-attribution role %q", log[0].Author, "coder")
	}

	// The generation ref keeps a rollback target addressable after HEAD moves.
	ref, err := repo.st.Reference(plumbing.ReferenceName(gitGenRefPrefix + "gen-1"))
	if err != nil {
		t.Fatalf("refs/gen/gen-1 missing: %v", err)
	}
	if ref.Hash().String() != log[0].Hash {
		t.Errorf("refs/gen/gen-1 = %s, commit = %s", ref.Hash(), log[0].Hash)
	}

	// The lineage row gets the pointer (P1.3.5 links the two records).
	if !strings.Contains(strings.Join(fake.serverUS, "\n"), "UPDATE generations SET commit_sha") {
		t.Error("generations row was never stamped with commit_sha/branch")
	}
}

func TestCommitGenerationIsPerGenerationAndDedupesContent(t *testing.T) {
	ctx := context.Background()
	room, fake := newGenerationCommitRoom(t)

	openedGeneration(room, "gen-1")
	room.UpsertFile("index.html", "<h1>one</h1>", "coder")
	room.finalizeGeneration("", 1)

	fake.mu.Lock()
	afterFirst := len(fake.objects)
	fake.mu.Unlock()

	// Second generation rewrites the SAME content: content addressing must
	// dedupe everything but the new commit object itself.
	openedGeneration(room, "gen-2")
	room.UpsertFile("index.html", "<h1>one</h1>", "coder")
	room.finalizeGeneration("", 1)

	repo, err := OpenGitRepo(ctx, room.d1, room.chatID)
	if err != nil {
		t.Fatalf("OpenGitRepo: %v", err)
	}
	log, err := repo.Log(ctx, gitMainRef, 0)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(log) != 2 {
		t.Fatalf("expected one commit per generation (2), got %d", len(log))
	}
	for _, genID := range []string{"gen-1", "gen-2"} {
		if _, err := repo.st.Reference(plumbing.ReferenceName(gitGenRefPrefix + genID)); err != nil {
			t.Errorf("refs/gen/%s missing: %v", genID, err)
		}
	}

	fake.mu.Lock()
	grown := len(fake.objects) - afterFirst
	fake.mu.Unlock()
	if grown != 1 {
		t.Errorf("identical content grew the object table by %d rows, want only the new commit (1)", grown)
	}
}

func TestCommitGenerationNoopWithoutTarget(t *testing.T) {
	room, fake := newGenerationCommitRoom(t)

	// No open generation: the plan-execute path opens no lineage row, so there
	// is nothing to version.
	if sha, err := room.commitGeneration(); sha != "" || err != nil {
		t.Fatalf("commit without a generation = (%q, %v), want (\"\", nil)", sha, err)
	}

	// An open generation but an empty VFS: no revision.
	openedGeneration(room, "gen-empty")
	if sha, err := room.commitGeneration(); sha != "" || err != nil {
		t.Fatalf("commit of an empty VFS = (%q, %v), want (\"\", nil)", sha, err)
	}
	// ... and the id is consumed, so a retry cannot commit the same run twice.
	room.UpsertFile("index.html", "<h1>late</h1>", "")
	if sha, err := room.commitGeneration(); sha != "" || err != nil {
		t.Fatalf("commit after the target was consumed = (%q, %v), want (\"\", nil)", sha, err)
	}

	// No D1 client: dev/CI must stay a silent no-op, never an error.
	room.d1 = nil
	openedGeneration(room, "gen-no-d1")
	room.UpsertFile("index.html", "<h1>x</h1>", "coder")
	if sha, err := room.commitGeneration(); sha != "" || err != nil {
		t.Fatalf("commit without D1 = (%q, %v), want (\"\", nil)", sha, err)
	}

	if len(fake.inserts) != 0 {
		t.Errorf("expected no git writes, got %d", len(fake.inserts))
	}
}

func TestGenerationCommitAuthor(t *testing.T) {
	cases := []struct {
		name    string
		authors map[string]string
		want    string
	}{
		{"empty intake", nil, generationCommitFallbackAuthor},
		{"single role", map[string]string{"a.ts": "coder", "b.ts": "coder"}, "coder"},
		{"mixed roles", map[string]string{"a.ts": "coder", "b.ts": "reviewer"}, generationCommitFallbackAuthor},
		{"blank entries", map[string]string{"a.ts": "  ", "b.ts": ""}, generationCommitFallbackAuthor},
	}
	for _, tc := range cases {
		if got := generationCommitAuthor(tc.authors); got != tc.want {
			t.Errorf("%s: generationCommitAuthor = %q, want %q", tc.name, got, tc.want)
		}
	}
}
