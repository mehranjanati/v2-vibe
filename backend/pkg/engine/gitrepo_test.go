package engine

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"

	"backend/pkg/cloudflare"
)

// fakeGitObject is one emulated `git_objects` row.
type fakeGitObject struct {
	kind    string
	content string
	size    int
}

// fakeGitD1 emulates the D1 statements gitrepo.go issues (object insert/select,
// ref upsert/select/list) and records every write so the tests can assert the
// CF_LIMITS §8 contract (content as a bind parameter, one row per object).
type fakeGitD1 struct {
	mu       sync.Mutex
	objects  map[string]fakeGitObject // "appID\x00sha" -> row
	refs     map[string]string        // "appID\x00name" -> commit sha
	inserts  []fakeGitInsert
	requests []fakeGitInsert // every statement + params, for audit/lineage assertions
	serverUS []string        // every SQL string, to prove content is never inlined
}

type fakeGitInsert struct {
	sql    string
	params []any
}

func newFakeGitD1(t *testing.T) (*fakeGitD1, *cloudflare.D1Client) {
	t.Helper()
	f := &fakeGitD1{objects: map[string]fakeGitObject{}, refs: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SQL    string `json:"sql"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.serverUS = append(f.serverUS, req.SQL)
		f.requests = append(f.requests, fakeGitInsert{sql: req.SQL, params: req.Params})
		results := f.apply(req.SQL, req.Params)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result":  []any{map[string]any{"results": results, "meta": map[string]any{}}},
		})
	}))
	t.Cleanup(srv.Close)

	client := cloudflare.NewD1Client("acct", "token", "db")
	client.SetBaseURL(srv.URL)
	return f, client
}

func gitKey(appID, name string) string { return appID + "\x00" + name }

// apply executes one statement against the emulated git tables. Callers hold mu.
func (f *fakeGitD1) apply(sql string, params []any) []map[string]any {
	switch {
	case strings.HasPrefix(sql, "INSERT OR IGNORE INTO git_objects"):
		appID := paramString(params, 0)
		sha := paramString(params, 1)
		row := fakeGitObject{
			kind:    paramString(params, 2),
			content: paramString(params, 3),
			size:    intParamValue(params, 4),
		}
		f.inserts = append(f.inserts, fakeGitInsert{sql: sql, params: params})
		if _, exists := f.objects[gitKey(appID, sha)]; !exists {
			f.objects[gitKey(appID, sha)] = row
		}

	case strings.HasPrefix(sql, "SELECT content, size, kind FROM git_objects"):
		if row, ok := f.objects[gitKey(paramString(params, 0), paramString(params, 1))]; ok {
			return []map[string]any{{"content": row.content, "size": row.size, "kind": row.kind}}
		}

	case strings.HasPrefix(sql, "SELECT sha FROM git_objects"):
		if _, ok := f.objects[gitKey(paramString(params, 0), paramString(params, 1))]; ok {
			return []map[string]any{{"sha": paramString(params, 1)}}
		}

	case strings.HasPrefix(sql, "INSERT INTO git_refs"):
		appID, name, sha := paramString(params, 0), paramString(params, 1), paramString(params, 2)
		f.inserts = append(f.inserts, fakeGitInsert{sql: sql, params: params})
		f.refs[gitKey(appID, name)] = sha

	case strings.HasPrefix(sql, "SELECT sha FROM git_refs"):
		if sha, ok := f.refs[gitKey(paramString(params, 0), paramString(params, 1))]; ok {
			return []map[string]any{{"sha": sha}}
		}

	case strings.HasPrefix(sql, "SELECT name, sha FROM git_refs"):
		appID := paramString(params, 0)
		rows := make([]map[string]any, 0, len(f.refs))
		for key, sha := range f.refs {
			parts := strings.SplitN(key, "\x00", 2)
			if len(parts) == 2 && parts[0] == appID {
				rows = append(rows, map[string]any{"name": parts[1], "sha": sha})
			}
		}
		return rows
	}
	return nil
}

// intParamValue reads an integer bind value (JSON numbers decode as float64).
func intParamValue(params []any, i int) int {
	if i >= len(params) || params[i] == nil {
		return 0
	}
	if fl, ok := params[i].(float64); ok {
		return int(fl)
	}
	return 0
}

// objectCount counts stored objects for one app.
func (f *fakeGitD1) objectCount(appID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for key := range f.objects {
		if strings.HasPrefix(key, appID+"\x00") {
			n++
		}
	}
	return n
}

// kindCount counts stored objects of one kind for one app.
func (f *fakeGitD1) kindCount(appID, kind string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for key, row := range f.objects {
		if row.kind == kind && strings.HasPrefix(key, appID+"\x00") {
			n++
		}
	}
	return n
}

// decodeObject returns the inflated payload + kind of a stored object.
func (f *fakeGitD1) decodeObject(t *testing.T, appID, sha string) ([]byte, string) {
	t.Helper()
	f.mu.Lock()
	row, ok := f.objects[gitKey(appID, sha)]
	f.mu.Unlock()
	if !ok {
		t.Fatalf("object %s not stored for %s", sha, appID)
	}
	compressed, err := base64.StdEncoding.DecodeString(row.content)
	if err != nil {
		t.Fatalf("object %s content is not base64: %v", sha, err)
	}
	zr, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("object %s content is not zlib: %v", sha, err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("inflate %s: %v", sha, err)
	}
	return raw, row.kind
}

// refOf returns the stored ref target.
func (f *fakeGitD1) refOf(appID, name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refs[gitKey(appID, name)]
}

// TestGitRepoCommitLogDiffRevert is the P1.3.4 acceptance test: two generations
// produce two different SHAs chained by parent, the diff is path-level, and a
// revert appends a new commit while the earlier history stays intact.
func TestGitRepoCommitLogDiffRevert(t *testing.T) {
	f, d1 := newFakeGitD1(t)
	ctx := context.Background()
	repo := NewGitRepo(d1, "app-1")

	head, err := repo.Init(ctx)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if head != "" {
		t.Fatalf("fresh repo head = %q, want empty", head)
	}

	h1, err := repo.Commit(ctx, GitCommitRequest{
		Message:      "[gen:g1] build a landing page | verdict:done | author:coder",
		Author:       "coder",
		GenerationID: "g1",
		Files: map[string]string{
			"index.html":       "<h1>v1</h1>",
			"public/js/app.js": "console.log('v1');",
		},
	})
	if err != nil {
		t.Fatalf("commit g1: %v", err)
	}
	if got := f.refOf("app-1", gitMainRef); got != h1 {
		t.Fatalf("main ref = %q, want %q", got, h1)
	}
	if got := f.refOf("app-1", gitGenRefPrefix+"g1"); got != h1 {
		t.Fatalf("generation ref not tagged: %q", got)
	}

	h2, err := repo.Commit(ctx, GitCommitRequest{
		Message:      "[gen:g2] add a script | verdict:approve | author:coder",
		Author:       "coder",
		GenerationID: "g2",
		Files: map[string]string{
			"index.html":         "<h1>v2</h1>",
			"public/js/app.js":   "console.log('v1');", // unchanged: same blob
			"public/js/extra.js": "export const x = 1;",
		},
	})
	if err != nil {
		t.Fatalf("commit g2: %v", err)
	}
	if h2 == h1 {
		t.Fatal("two generations must produce different commit SHAs")
	}

	log, err := repo.Log(ctx, "", 0)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(log) != 2 {
		t.Fatalf("log entries = %d, want 2", len(log))
	}
	if log[0].Hash != h2 || len(log[0].Parents) != 1 || log[0].Parents[0] != h1 {
		t.Fatalf("newest entry = %+v, want hash %s with parent %s", log[0], h2, h1)
	}
	if log[1].Hash != h1 || len(log[1].Parents) != 0 {
		t.Fatalf("root entry = %+v, want hash %s with no parent", log[1], h1)
	}
	if log[0].Author != "coder" {
		t.Errorf("author = %q, want coder", log[0].Author)
	}

	changes, err := repo.Diff(ctx, h1, h2)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	byPath := map[string]GitChange{}
	for _, change := range changes {
		byPath[change.Path] = change
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %+v, want 2 (modify index.html, create extra.js)", changes)
	}
	if byPath["index.html"].Action != "modify" {
		t.Errorf("index.html action = %q, want modify", byPath["index.html"].Action)
	}
	if byPath["public/js/extra.js"].Action != "create" || byPath["public/js/extra.js"].From != "" {
		t.Errorf("extra.js = %+v, want create with no from-hash", byPath["public/js/extra.js"])
	}

	// Revert to the g1 tag: a NEW commit on top of h2 whose tree is h1's.
	h3, err := repo.Revert(ctx, gitGenRefPrefix+"g1", "user")
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	if h3 == h1 || h3 == h2 {
		t.Fatalf("revert must create a new commit, got %s", h3)
	}
	log, err = repo.Log(ctx, "", 0)
	if err != nil {
		t.Fatalf("log after revert: %v", err)
	}
	if len(log) != 3 {
		t.Fatalf("log entries after revert = %d, want 3", len(log))
	}
	if log[0].Hash != h3 || len(log[0].Parents) != 1 || log[0].Parents[0] != h2 {
		t.Fatalf("revert entry = %+v, want %s with parent %s", log[0], h3, h2)
	}
	if log[2].Hash != h1 {
		t.Fatalf("history rewritten: oldest = %s, want %s", log[2].Hash, h1)
	}
	reverted, err := repo.Diff(ctx, h2, h3)
	if err != nil {
		t.Fatalf("diff after revert: %v", err)
	}
	actions := map[string]string{}
	for _, change := range reverted {
		actions[change.Path] = change.Action
	}
	if actions["public/js/extra.js"] != "delete" {
		t.Errorf("revert diff = %v, want extra.js deleted", actions)
	}
	if actions["index.html"] != "modify" {
		t.Errorf("revert diff = %v, want index.html restored", actions)
	}
}

// TestGitRepoStoresObjectsAsBoundCompressedRows pins the CF_LIMITS §8 storage
// contract: one row per object, content only as a bind parameter, zlib+base64
// payloads that inflate to real bytes, and content-addressed dedupe (an
// unchanged file is not stored twice across generations).
func TestGitRepoStoresObjectsAsBoundCompressedRows(t *testing.T) {
	f, d1 := newFakeGitD1(t)
	ctx := context.Background()
	repo := NewGitRepo(d1, "app-2")

	shared := "console.log('shared');"
	if _, err := repo.Commit(ctx, GitCommitRequest{
		Message: "one", Author: "coder",
		Files: map[string]string{
			"index.html":       "<h1>1</h1>",
			"public/js/app.js": shared,
		},
	}); err != nil {
		t.Fatalf("commit one: %v", err)
	}
	firstCount := f.objectCount("app-2")

	if _, err := repo.Commit(ctx, GitCommitRequest{
		Message: "two", Author: "coder",
		Files: map[string]string{
			"index.html":       "<h1>2</h1>",
			"public/js/app.js": shared,
		},
	}); err != nil {
		t.Fatalf("commit two: %v", err)
	}
	secondCount := f.objectCount("app-2")

	// CAS expectations for this exact pair of commits: the shared blob is NOT
	// stored again (blob delta = 1: only index.html changed), the `public/`
	// subtree is content-identical so IT dedupes too, and what remains is the
	// new index blob + the new root tree + the new commit = 3 rows.
	if delta := secondCount - firstCount; delta != 3 {
		t.Fatalf("second commit stored %d objects, want 3 (new blob + root tree + commit; shared blob and public/ subtree dedupe)", delta)
	}
	if blobDelta := f.kindCount("app-2", "blob") - 2; blobDelta != 1 {
		t.Fatalf("second commit stored %d blobs, want 1 (the shared blob must dedupe)", blobDelta)
	}

	f.mu.Lock()
	inserts := append([]fakeGitInsert(nil), f.inserts...)
	sqls := append([]string(nil), f.serverUS...)
	f.mu.Unlock()

	if len(inserts) == 0 {
		t.Fatal("no object rows were written")
	}
	objectInserts := 0
	kinds := map[string]bool{}
	for _, insert := range inserts {
		if !strings.HasPrefix(insert.sql, "INSERT OR IGNORE INTO git_objects") {
			continue // ref upserts have their own shape
		}
		objectInserts++
		if len(insert.params) != 5 {
			t.Fatalf("object insert params = %d, want 5", len(insert.params))
		}
		if content := paramString(insert.params, 3); content == "" {
			t.Fatalf("object content parameter is empty: %v", insert.params)
		}
		if insert.params[2] != nil {
			kinds[paramString(insert.params, 2)] = true
		}
		if strings.Contains(insert.sql, "console.log") || strings.Contains(insert.sql, "<h1>") {
			t.Fatalf("file content leaked into SQL text: %s", insert.sql)
		}
	}
	if objectInserts == 0 {
		t.Fatal("no git_objects rows were written")
	}
	for _, kind := range []string{"blob", "tree", "commit"} {
		if !kinds[kind] {
			t.Errorf("no %s object was stored (kinds=%v)", kind, kinds)
		}
	}
	for _, sql := range sqls {
		if strings.Contains(sql, "console.log") {
			t.Fatalf("file content leaked into a SQL statement: %s", sql)
		}
	}

	// The stored commit object inflates to a real Git commit payload (real
	// object model, not a made-up hash).
	head := f.refOf("app-2", gitMainRef)
	raw, kind := f.decodeObject(t, "app-2", head)
	if kind != "commit" {
		t.Fatalf("HEAD object kind = %q, want commit", kind)
	}
	if !strings.Contains(string(raw), "tree ") || !strings.Contains(string(raw), "author coder <") {
		t.Fatalf("commit payload does not look like a Git commit: %q", string(raw))
	}
	if !strings.HasSuffix(string(raw), "\ntwo") {
		t.Fatalf("HEAD commit message missing: %q", string(raw))
	}
}

// TestGitRepoRefsAndObjectsReloadFromD1 proves the history is not memory-only:
// a fresh handle over the same D1 sees the refs and can walk the log.
func TestGitRepoRefsAndObjectsReloadFromD1(t *testing.T) {
	_, d1 := newFakeGitD1(t)
	ctx := context.Background()

	first := NewGitRepo(d1, "app-3")
	h1, err := first.Commit(ctx, GitCommitRequest{
		Message: "first", Author: "coder",
		Files: map[string]string{"index.html": "<h1>x</h1>"},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	second, err := OpenGitRepo(ctx, d1, "app-3")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	head, err := second.Head(ctx)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head != h1 {
		t.Fatalf("fresh handle head = %q, want %q", head, h1)
	}
	log, err := second.Log(ctx, "", 0)
	if err != nil {
		t.Fatalf("log from fresh handle: %v", err)
	}
	if len(log) != 1 || log[0].Hash != h1 {
		t.Fatalf("log from fresh handle = %+v, want one entry %s", log, h1)
	}
}

// TestGitRepoErrors pins the failure modes: no D1, no author, unsafe path, empty
// file set and revert on an empty repo — each fails loudly with nothing written.
func TestGitRepoErrors(t *testing.T) {
	ctx := context.Background()

	if _, err := NewGitRepo(nil, "app-x").Init(ctx); !errors.Is(err, ErrGitRepoUnavailable) {
		t.Fatalf("nil D1 init error = %v, want ErrGitRepoUnavailable", err)
	}
	if _, err := NewGitRepo(nil, "").Init(ctx); err == nil {
		t.Fatal("empty app id must be rejected")
	}

	f, d1 := newFakeGitD1(t)
	repo := NewGitRepo(d1, "app-4")

	if _, err := repo.Commit(ctx, GitCommitRequest{Message: "x", Files: map[string]string{"a.txt": "a"}}); err == nil {
		t.Fatal("commit without an author must be rejected")
	}
	if _, err := repo.Commit(ctx, GitCommitRequest{Message: "x", Author: "coder"}); err == nil {
		t.Fatal("empty commit must be rejected")
	}
	if _, err := repo.Commit(ctx, GitCommitRequest{
		Message: "x", Author: "coder",
		Files: map[string]string{"../escape.txt": "nope"},
	}); err == nil {
		t.Fatal("path traversal must be rejected")
	}
	if got := f.objectCount("app-4"); got != 0 {
		t.Fatalf("rejected commits wrote %d objects, want 0", got)
	}
	if _, err := repo.Revert(ctx, gitMainRef, "user"); err == nil {
		t.Fatal("revert on an empty repo must be rejected")
	}

	if !isFullHash(plumbing.NewHash("0000000000000000000000000000000000000000").String()) {
		t.Fatal("isFullHash rejected a zero hash")
	}
	if isFullHash("HEAD") || isFullHash("refs/heads/main") {
		t.Fatal("isFullHash accepted a non-hash revision")
	}
}
