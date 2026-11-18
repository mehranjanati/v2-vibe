package engine

import (
	"context"
	"strings"
	"testing"
	"time"

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

// waitForGenerationAudit blocks until the detached audit writer has issued a
// generation_audits row with the given action (writers run on their own
// goroutine by design, so the test polls instead of assuming an order).
func waitForGenerationAudit(t *testing.T, fake *fakeGitD1, action string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fake.mu.Lock()
		found := false
		for _, req := range fake.requests {
			if !strings.HasPrefix(req.sql, "INSERT INTO generation_audits") {
				continue
			}
			for _, p := range req.params {
				if s, ok := p.(string); ok && s == action {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		fake.mu.Unlock()
		if found {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %q audit row reached D1", action)
}

func TestPreCommitGateBlocksHardcodedCredential(t *testing.T) {
	ctx := context.Background()
	room, fake := newGenerationCommitRoom(t)

	openedGeneration(room, "gen-secret")
	room.UpsertFile("src/api.js", "const base = 1;\nconst API_KEY = \"sk-live-9f3ab7c21d4e\";\n", "coder")

	sha, err := room.commitGeneration()
	if err == nil {
		t.Fatalf("commit of a hardcoded credential = (%q, nil), want a refusal", sha)
	}
	if !strings.Contains(err.Error(), "src/api.js:2") {
		t.Errorf("error %q should name the offending path:line", err)
	}
	if strings.Contains(err.Error(), "sk-live-9f3ab7c21d4e") {
		t.Error("the refusal error leaks the credential value")
	}

	// No revision is created for the run.
	repo, oerr := OpenGitRepo(ctx, room.d1, room.chatID)
	if oerr != nil {
		t.Fatalf("OpenGitRepo: %v", oerr)
	}
	if log, lerr := repo.Log(ctx, gitMainRef, 0); lerr != nil || len(log) != 0 {
		t.Fatalf("blocked generation produced a revision: %v / %v", log, lerr)
	}

	// The block is auditable, and the consumed id prevents a retry into a commit.
	waitForGenerationAudit(t, fake, "commit_blocked")
	if sha, err := room.commitGeneration(); sha != "" || err != nil {
		t.Fatalf("retry after a block = (%q, %v), want (\"\", nil)", sha, err)
	}
}

func TestPreCommitGateAllowsRealCodeValues(t *testing.T) {
	ctx := context.Background()
	room, fake := newGenerationCommitRoom(t)

	openedGeneration(room, "gen-forms")
	room.UpsertFile("src/login.tsx", strings.Join([]string{
		"const [state, setState] = useState({ password: \"\" });",
		"onChange={(e) => setState({ password: e.target.value })}",
		"const token = form.password;",
		"const key2 = process.env.API_KEY;",
		"const placeholder = '{{API_KEY}}';",
		"const apiKey = import.meta.env.VITE_API_KEY;",
		`<input type="password" name="password" />`,
	}, "\n"), "coder")

	if sha, err := room.commitGeneration(); err != nil || sha == "" {
		t.Fatalf("commit of ordinary form code = (%q, %v), want a commit", sha, err)
	}
	repo, err := OpenGitRepo(ctx, room.d1, room.chatID)
	if err != nil {
		t.Fatalf("OpenGitRepo: %v", err)
	}
	if log, lerr := repo.Log(ctx, gitMainRef, 0); lerr != nil || len(log) != 1 {
		t.Fatalf("expected 1 commit, got %d (%v)", len(log), lerr)
	}
	fake.mu.Lock()
	joined := strings.Join(fake.serverUS, "\n")
	fake.mu.Unlock()
	if strings.Contains(joined, "commit_blocked") {
		t.Error("an ordinary login form was flagged as a credential leak")
	}
}

func TestPreCommitGateSkipsOversizeFile(t *testing.T) {
	ctx := context.Background()
	room, fake := newGenerationCommitRoom(t)

	big := strings.Repeat("a", generationCommitMaxFileBytes+1)
	openedGeneration(room, "gen-big")
	room.UpsertFile("assets/huge.txt", big, "coder")
	room.UpsertFile("index.html", "<h1>ok</h1>", "coder")

	sha, err := room.commitGeneration()
	if err != nil || sha == "" {
		t.Fatalf("commit with one oversize file = (%q, %v), want a commit of the rest", sha, err)
	}

	// The oversized content never reaches D1 and the skip is recorded.
	fake.mu.Lock()
	for _, ins := range fake.inserts {
		for _, p := range ins.params {
			if s, ok := p.(string); ok && s == big {
				fake.mu.Unlock()
				t.Fatal("oversize file content was written to the object table")
			}
		}
	}
	fake.mu.Unlock()
	waitForGenerationAudit(t, fake, "commit_file_skipped")

	repo, oerr := OpenGitRepo(ctx, room.d1, room.chatID)
	if oerr != nil {
		t.Fatalf("OpenGitRepo: %v", oerr)
	}
	changes, derr := repo.Diff(ctx, "", gitMainRef)
	if derr != nil {
		t.Fatalf("Diff: %v", derr)
	}
	for _, ch := range changes {
		if ch.Path == "assets/huge.txt" {
			t.Error("the skipped file is part of the revision")
		}
	}
}

func TestGenerationCredentialRule(t *testing.T) {
	leaks := []string{
		`const API_KEY = "sk-live-9f3ab7c21d4e";`,
		`token: 'ghp_A1b2C3d4E5f6G7h8I9j0'`,
		`PASSWORD=hunter2hunter2`,
		`secret: "c2VjcmV0LXZhbHVl"`,
		`api-key = ABCDEFGHIJKLMNOPQRSTUVWXYZ123456`,
	}
	for _, line := range leaks {
		if !generationLineHasCredential(line) {
			t.Errorf("expected a credential match for %q", line)
		}
	}
	allowed := []string{
		`password: e.target.value,`,
		`const password = form.password.value;`,
		`token: "",`,
		`secret: null,`,
		`apiKey: process.env.API_KEY,`,
		`const apiKey = import.meta.env.VITE_API_KEY;`,
		`placeholder: 'your-api-key-here'`,
		"token: `${session.token}`,",
		`password: "{{PASSWORD}}",`,
		`<input type="password" name="password" />`,
		`if (password === confirmPassword) {`,
	}
	for _, line := range allowed {
		if generationLineHasCredential(line) {
			t.Errorf("false positive: %q would have blocked the commit", line)
		}
	}
}
