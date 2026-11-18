package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"

	"backend/pkg/cloudflare"
	"backend/pkg/engine"
)

// fakeHistoryD1 emulates the statements the History API and the internal Git
// store issue: the generation selects, the diff rows, the rollback audit insert
// and the `git_objects`/`git_refs` statements `engine.GitRepo` speaks. It
// answers the real D1 REST envelope, so the production client runs unmodified.
type fakeHistoryD1 struct {
	mu          sync.Mutex
	generations []map[string]any            // insertion order; newest last
	files       map[string][]map[string]any // generation_id → diff rows
	audits      []map[string]any            // recorded audit inserts
	objects     map[string]map[string]any   // "app\x00sha" → {content, size, kind}
	refs        map[string]string           // "app\x00name" → sha
}

func newFakeHistoryD1(t *testing.T) (*fakeHistoryD1, *cloudflare.D1Client) {
	t.Helper()
	f := &fakeHistoryD1{
		files:   map[string][]map[string]any{},
		objects: map[string]map[string]any{},
		refs:    map[string]string{},
	}
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

// apply executes one statement against the emulated tables (callers hold mu).
func (f *fakeHistoryD1) apply(sql string, params []any) []map[string]any {
	switch {
	case strings.HasPrefix(sql, "SELECT id, chat_id, parent, status, verdict, fork, commit_sha, branch, created_at, updated_at FROM generations WHERE chat_id"):
		chatID := paramAt(params, 0)
		limit := historyParamInt(params, 1)
		out := make([]map[string]any, 0, len(f.generations))
		for i := len(f.generations) - 1; i >= 0; i-- {
			if paramAt2(f.generations[i]["chat_id"]) != chatID {
				continue
			}
			out = append(out, f.generations[i])
			if limit > 0 && len(out) >= limit {
				break
			}
		}
		return out

	case strings.HasPrefix(sql, "SELECT id, chat_id, parent, status, verdict, fork, commit_sha, branch, created_at, updated_at FROM generations WHERE id"):
		genID, chatID := paramAt(params, 0), paramAt(params, 1)
		for _, row := range f.generations {
			if paramAt2(row["id"]) == genID && paramAt2(row["chat_id"]) == chatID {
				return []map[string]any{row}
			}
		}
		return nil

	case strings.HasPrefix(sql, "SELECT path, op, before_hash, after_hash, before_size, after_size, author_agent FROM generation_files"):
		rows := append([]map[string]any(nil), f.files[paramAt(params, 0)]...)
		sort.Slice(rows, func(i, j int) bool {
			return paramAt2(rows[i]["path"]) < paramAt2(rows[j]["path"])
		})
		return rows

	case strings.HasPrefix(sql, "INSERT INTO generation_audits"):
		f.audits = append(f.audits, map[string]any{
			"generation_id": paramAt(params, 1),
			"actor":         paramAt(params, 2),
			"action":        paramAt(params, 3),
			"detail":        paramAt(params, 4),
		})
		return nil

	case strings.HasPrefix(sql, "INSERT OR IGNORE INTO git_objects"):
		key := paramAt(params, 0) + "\x00" + paramAt(params, 1)
		if _, exists := f.objects[key]; !exists {
			f.objects[key] = map[string]any{
				"kind":    paramAt(params, 2),
				"content": paramAt(params, 3),
				"size":    historyParamInt(params, 4),
			}
		}
		return nil

	case strings.HasPrefix(sql, "SELECT content, size, kind FROM git_objects"):
		if row, ok := f.objects[paramAt(params, 0)+"\x00"+paramAt(params, 1)]; ok {
			return []map[string]any{{"content": row["content"], "size": row["size"], "kind": row["kind"]}}
		}
		return nil

	case strings.HasPrefix(sql, "SELECT sha FROM git_objects"):
		key := paramAt(params, 0) + "\x00" + paramAt(params, 1)
		if _, ok := f.objects[key]; ok {
			return []map[string]any{{"sha": paramAt(params, 1)}}
		}
		return nil

	case strings.HasPrefix(sql, "INSERT INTO git_refs"):
		f.refs[paramAt(params, 0)+"\x00"+paramAt(params, 1)] = paramAt(params, 2)
		return nil

	case strings.HasPrefix(sql, "SELECT sha FROM git_refs"):
		if sha, ok := f.refs[paramAt(params, 0)+"\x00"+paramAt(params, 1)]; ok {
			return []map[string]any{{"sha": sha}}
		}
		return nil

	case strings.HasPrefix(sql, "SELECT name, sha FROM git_refs"):
		appID := paramAt(params, 0)
		var rows []map[string]any
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

// newHistoryEnv builds the fixture: the real routes over the emulated D1. No
// session verifier means the P0.3 boundary stays off, which is what these cases
// exercise (the guarded behaviour is covered by the auth tests).
func newHistoryEnv(t *testing.T) (*fiber.App, *fakeHistoryD1, *cloudflare.D1Client, *engine.EngineHub) {
	t.Helper()
	fake, d1 := newFakeHistoryD1(t)
	app := fiber.New()
	hub := engine.NewEngineHub(nil, nil, nil)
	hub.SetD1Client(d1)
	RegisterRoutes(app, hub)
	return app, fake, d1, hub
}

func TestGenerationHistoryListAndDiff(t *testing.T) {
	app, fake, _, _ := newHistoryEnv(t)

	fake.mu.Lock()
	fake.generations = []map[string]any{
		{"id": "gen-1", "chat_id": "chat-1", "parent": nil, "status": "succeeded", "verdict": "done", "fork": 0, "commit_sha": "sha-1", "branch": "main", "created_at": "2026-11-18 10:00:00", "updated_at": "2026-11-18 10:00:01"},
		{"id": "gen-2", "chat_id": "chat-1", "parent": "gen-1", "status": "succeeded", "verdict": "approve", "fork": 0, "commit_sha": "sha-2", "branch": "main", "created_at": "2026-11-18 11:00:00", "updated_at": "2026-11-18 11:00:01"},
		{"id": "gen-x", "chat_id": "chat-2", "parent": nil, "status": "succeeded", "verdict": nil, "fork": 0, "commit_sha": nil, "branch": nil, "created_at": "2026-11-18 09:00:00", "updated_at": nil},
	}
	fake.files["gen-2"] = []map[string]any{
		{"path": "index.html", "op": "modify", "before_hash": "h1", "after_hash": "h2", "before_size": float64(10), "after_size": float64(12), "author_agent": "coder"},
		{"path": "gone.css", "op": "delete", "before_hash": "h3", "after_hash": nil, "before_size": float64(5), "after_size": nil, "author_agent": nil},
	}
	fake.mu.Unlock()

	status, body := workflowRequest(t, app, http.MethodGet, "/api/generations/chat-1", "", nil)
	if status != 200 {
		t.Fatalf("list status = %d, want 200 (%v)", status, body)
	}
	data, _ := body["data"].(map[string]any)
	gens, _ := data["generations"].([]any)
	if len(gens) != 2 {
		t.Fatalf("listed %d generations, want only chat-1's 2", len(gens))
	}
	newest, _ := gens[0].(map[string]any)
	if newest["id"] != "gen-2" {
		t.Errorf("first row = %v, want the newest generation", newest["id"])
	}
	if newest["commitSha"] != "sha-2" {
		t.Errorf("commitSha = %v, want sha-2 (the P1.3.5 link)", newest["commitSha"])
	}

	status, body = workflowRequest(t, app, http.MethodGet, "/api/generations/chat-1/gen-2/diff", "", nil)
	if status != 200 {
		t.Fatalf("diff status = %d, want 200 (%v)", status, body)
	}
	data, _ = body["data"].(map[string]any)
	files, _ := data["files"].([]any)
	if len(files) != 2 {
		t.Fatalf("diff listed %d files, want 2", len(files))
	}
	first, _ := files[0].(map[string]any)
	if first["path"] != "gone.css" || first["op"] != "delete" {
		t.Errorf("first row = %v/%v, want gone.css delete (sorted by path)", first["path"], first["op"])
	}
	if _, leaked := first["contents"]; leaked {
		t.Error("the diff must never carry file contents")
	}

	// A generation of another chat is not visible through this chat's path.
	status, _ = workflowRequest(t, app, http.MethodGet, "/api/generations/chat-1/gen-x/diff", "", nil)
	if status != 404 {
		t.Errorf("cross-chat diff status = %d, want 404", status)
	}
	status, _ = workflowRequest(t, app, http.MethodGet, "/api/generations/chat-1/nope/diff", "", nil)
	if status != 404 {
		t.Errorf("unknown generation status = %d, want 404", status)
	}
}

func TestGenerationRollbackCreatesRevertCommit(t *testing.T) {
	app, fake, d1, _ := newHistoryEnv(t)
	ctx := context.Background()

	repo, err := engine.OpenGitRepo(ctx, d1, "chat-1")
	if err != nil {
		t.Fatalf("OpenGitRepo: %v", err)
	}
	targetSHA, err := repo.Commit(ctx, engine.GitCommitRequest{
		Message:      "generation gen-1",
		Author:       "coder",
		GenerationID: "gen-1",
		Files:        map[string]string{"index.html": "<h1>one</h1>", "app.js": "one()"},
	})
	if err != nil {
		t.Fatalf("commit gen-1: %v", err)
	}
	headSHA, err := repo.Commit(ctx, engine.GitCommitRequest{
		Message:      "generation gen-2",
		Author:       "coder",
		GenerationID: "gen-2",
		Files:        map[string]string{"index.html": "<h1>two</h1>"},
	})
	if err != nil {
		t.Fatalf("commit gen-2: %v", err)
	}

	fake.mu.Lock()
	fake.generations = []map[string]any{{
		"id": "gen-1", "chat_id": "chat-1", "parent": nil, "status": "succeeded",
		"verdict": "done", "fork": 0, "commit_sha": targetSHA, "branch": "main",
		"created_at": "2026-11-18 10:00:00", "updated_at": "2026-11-18 10:00:01",
	}}
	fake.mu.Unlock()

	status, body := workflowRequest(t, app, http.MethodPost, "/api/generations/chat-1/gen-1/rollback", "", nil)
	if status != 200 {
		t.Fatalf("rollback status = %d, want 200 (%v)", status, body)
	}
	data, _ := body["data"].(map[string]any)
	newSHA, _ := data["commitSha"].(string)
	if newSHA == "" {
		t.Fatal("rollback returned no commit sha")
	}
	if newSHA == targetSHA || newSHA == headSHA {
		t.Errorf("rollback reused a commit (%s); a revert must create a new one", newSHA)
	}
	if data["applied"] != false {
		t.Error("applied must be false when no live room holds the working set")
	}
	if restored, _ := data["restoredFiles"].(float64); restored != 2 {
		t.Errorf("restoredFiles = %v, want the 2 files of gen-1", data["restoredFiles"])
	}

	// The revert commit really carries the old tree.
	tree, err := repo.FilesAt(ctx, newSHA)
	if err != nil {
		t.Fatalf("FilesAt(rollback): %v", err)
	}
	if tree["index.html"] != "<h1>one</h1>" || tree["app.js"] != "one()" {
		t.Errorf("restored tree = %#v, want gen-1's files", tree)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	found := false
	for _, a := range fake.audits {
		if a["action"] == "rollback" {
			found = true
		}
	}
	if !found {
		t.Error("no rollback audit row was written")
	}
}

func TestGenerationRollbackWithoutRevision(t *testing.T) {
	app, fake, _, _ := newHistoryEnv(t)
	fake.mu.Lock()
	fake.generations = []map[string]any{{
		"id": "gen-old", "chat_id": "chat-1", "status": "succeeded",
		"verdict": nil, "fork": 0, "commit_sha": nil, "branch": nil,
	}}
	fake.mu.Unlock()

	status, body := workflowRequest(t, app, http.MethodPost, "/api/generations/chat-1/gen-old/rollback", "", nil)
	if status != 409 {
		t.Fatalf("rollback without a revision = %d, want 409 (%v)", status, body)
	}
}

func TestGenerationHistoryWithoutD1(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app, engine.NewEngineHub(nil, nil, nil))

	status, _ := workflowRequest(t, app, http.MethodGet, "/api/generations/chat-1", "", nil)
	if status != 503 {
		t.Errorf("list without D1 = %d, want 503", status)
	}
}

// TestApplyWorkingSetRestoresAndPrunes pins the working-set half of rollback:
// restored paths are written, paths that only exist now are removed, and the
// writes go through the normal VFS API so Redis and the intake stay consistent.
func TestApplyWorkingSetRestoresAndPrunes(t *testing.T) {
	room := engine.NewProjectRoom("chat-apply", nil, nil, nil)
	room.UpsertFile("index.html", "<h1>two</h1>", "coder")
	room.UpsertFile("stale.css", "body{}", "coder")

	applyWorkingSet(room, map[string]string{"index.html": "<h1>one</h1>", "app.js": "one()"})

	vfs := room.GetVFS()
	if got := vfs["index.html"]; got == nil || got.FileContents != "<h1>one</h1>" {
		t.Errorf("index.html = %#v, want the restored content", got)
	}
	if got := vfs["app.js"]; got == nil || got.FileContents != "one()" {
		t.Errorf("app.js = %#v, want the restored file", got)
	}
	if _, exists := vfs["stale.css"]; exists {
		t.Error("stale.css should have been pruned by the rollback")
	}
}

// historyParamInt reads an int parameter (JSON numbers arrive as float64).
func historyParamInt(params []any, i int) int {
	if i < 0 || i >= len(params) {
		return 0
	}
	switch n := params[i].(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		out, _ := strconv.Atoi(n)
		return out
	}
	return 0
}

// paramAt2 renders a stored column value for comparison (ids are strings).
func paramAt2(v any) string {
	s, _ := v.(string)
	return s
}
