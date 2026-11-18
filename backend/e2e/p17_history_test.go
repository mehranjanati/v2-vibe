//go:build e2e

// P1.7.3 — History exit-gate E2E, at the route level.
//
// Two generations produce two real Git commits with the correct parent; the
// second generation's recorded diff is served; rolling back to the first
// creates a NEW revert commit carrying the first generation's tree. Everything
// runs in-process against the SAME wiring production uses — RegisterRoutes +
// EngineHub + cloudflare.D1Client — over an emulated D1, so the proof needs no
// credentials and no deployed service.
//
// Scope note: the `generations` / `generation_files` rows are seeded the way the
// recorder writes them (the recorder is covered by
// backend/pkg/engine/generation_test.go and the commit hook by
// generation_git_test.go). The Git history, the HTTP surface and the revert are
// real.
//
// Run: go test -tags e2e ./e2e/ -run P17 -v
package e2e

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

	"backend/pkg/api"
	"backend/pkg/cloudflare"
	"backend/pkg/engine"
)

const p17ChatID = "p17-chat"

// p17D1 emulates the statements the History API and the internal Git store
// issue. It answers the real D1 REST envelope, so the production client runs
// unmodified.
type p17D1 struct {
	mu          sync.Mutex
	generations []map[string]any
	files       map[string][]map[string]any
	audits      []map[string]any
	objects     map[string]map[string]any // "app\x00sha" → {kind,content,size}
	refs        map[string]string         // "app\x00name" → sha
}

func p17NewD1(t *testing.T) (*p17D1, *cloudflare.D1Client) {
	t.Helper()
	f := &p17D1{
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
func (f *p17D1) apply(sql string, params []any) []map[string]any {
	switch {
	case strings.HasPrefix(sql, "SELECT id, chat_id, parent, status, verdict, fork, commit_sha, branch, created_at, updated_at FROM generations WHERE chat_id"):
		chatID := p17String(params, 0)
		limit := p17Int(params, 1)
		out := make([]map[string]any, 0, len(f.generations))
		for i := len(f.generations) - 1; i >= 0; i-- {
			if p17AsString(f.generations[i]["chat_id"]) != chatID {
				continue
			}
			out = append(out, f.generations[i])
			if limit > 0 && len(out) >= limit {
				break
			}
		}
		return out

	case strings.HasPrefix(sql, "SELECT id, chat_id, parent, status, verdict, fork, commit_sha, branch, created_at, updated_at FROM generations WHERE id"):
		genID, chatID := p17String(params, 0), p17String(params, 1)
		for _, row := range f.generations {
			if p17AsString(row["id"]) == genID && p17AsString(row["chat_id"]) == chatID {
				return []map[string]any{row}
			}
		}
		return nil
	}
	return f.applyGit(sql, params)
}

// applyGit handles the diff-row, audit and git-object/ref statements.
func (f *p17D1) applyGit(sql string, params []any) []map[string]any {
	switch {
	case strings.HasPrefix(sql, "SELECT path, op, before_hash, after_hash, before_size, after_size, author_agent FROM generation_files"):
		rows := append([]map[string]any(nil), f.files[p17String(params, 0)]...)
		sort.Slice(rows, func(i, j int) bool {
			return p17AsString(rows[i]["path"]) < p17AsString(rows[j]["path"])
		})
		return rows

	case strings.HasPrefix(sql, "INSERT INTO generation_audits"):
		f.audits = append(f.audits, map[string]any{
			"generation_id": p17String(params, 1),
			"actor":         p17String(params, 2),
			"action":        p17String(params, 3),
			"detail":        p17String(params, 4),
		})
		return nil

	case strings.HasPrefix(sql, "INSERT OR IGNORE INTO git_objects"):
		key := p17String(params, 0) + "\x00" + p17String(params, 1)
		if _, exists := f.objects[key]; !exists {
			f.objects[key] = map[string]any{
				"kind":    p17String(params, 2),
				"content": p17String(params, 3),
				"size":    p17Int(params, 4),
			}
		}
		return nil

	case strings.HasPrefix(sql, "SELECT content, size, kind FROM git_objects"):
		if row, ok := f.objects[p17String(params, 0)+"\x00"+p17String(params, 1)]; ok {
			return []map[string]any{{"content": row["content"], "size": row["size"], "kind": row["kind"]}}
		}
		return nil

	case strings.HasPrefix(sql, "SELECT sha FROM git_objects"):
		if _, ok := f.objects[p17String(params, 0)+"\x00"+p17String(params, 1)]; ok {
			return []map[string]any{{"sha": p17String(params, 1)}}
		}
		return nil

	case strings.HasPrefix(sql, "INSERT INTO git_refs"):
		f.refs[p17String(params, 0)+"\x00"+p17String(params, 1)] = p17String(params, 2)
		return nil

	case strings.HasPrefix(sql, "SELECT sha FROM git_refs"):
		if sha, ok := f.refs[p17String(params, 0)+"\x00"+p17String(params, 1)]; ok {
			return []map[string]any{{"sha": sha}}
		}
		return nil

	case strings.HasPrefix(sql, "SELECT name, sha FROM git_refs"):
		appID := p17String(params, 0)
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

func p17String(params []any, i int) string {
	if i < 0 || i >= len(params) {
		return ""
	}
	s, _ := params[i].(string)
	return s
}

func p17Int(params []any, i int) int {
	if i < 0 || i >= len(params) {
		return 0
	}
	switch n := params[i].(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		out, _ := strconv.Atoi(n)
		return out
	}
	return 0
}

func p17AsString(v any) string {
	s, _ := v.(string)
	return s
}

// TestP17TwoGenerationsDiffRollback is the P1.7.3 exit gate: two generations →
// two commits with the correct parent → the second's recorded diff → rollback
// to the first, which must create a NEW commit carrying gen-1's tree.
func TestP17TwoGenerationsDiffRollback(t *testing.T) {
	emulator, d1 := p17NewD1(t)
	app := fiber.New()
	hub := engine.NewEngineHub(nil, nil, nil)
	hub.SetD1Client(d1)
	api.RegisterRoutes(app, hub)

	ctx := context.Background()
	repo, err := engine.OpenGitRepo(ctx, d1, p17ChatID)
	if err != nil {
		t.Fatalf("OpenGitRepo: %v", err)
	}

	gen1Files := map[string]string{"index.html": "<h1>one</h1>", "app.js": "one()"}
	sha1, err := repo.Commit(ctx, engine.GitCommitRequest{
		Message: "generation gen-1", Author: "coder", GenerationID: "gen-1", Files: gen1Files,
	})
	if err != nil {
		t.Fatalf("commit gen-1: %v", err)
	}
	sha2, err := repo.Commit(ctx, engine.GitCommitRequest{
		Message:      "generation gen-2",
		Author:       "coder",
		GenerationID: "gen-2",
		Files:        map[string]string{"index.html": "<h1>two</h1>"},
	})
	if err != nil {
		t.Fatalf("commit gen-2: %v", err)
	}

	// The Git parent chain is real: gen-2's first parent is gen-1's commit.
	log, err := repo.Log(ctx, "main", 0)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(log) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(log))
	}
	if log[0].Hash != sha2 || log[1].Hash != sha1 {
		t.Fatalf("log = %s,%s; want newest-first %s,%s", log[0].Hash, log[1].Hash, sha2, sha1)
	}
	if len(log[0].Parents) == 0 || log[0].Parents[0] != sha1 {
		t.Fatalf("gen-2 parents = %v, want [%s]", log[0].Parents, sha1)
	}

	// The lineage rows the recorder would have written for those two runs.
	emulator.mu.Lock()
	emulator.generations = []map[string]any{
		{"id": "gen-1", "chat_id": p17ChatID, "parent": nil, "status": "succeeded", "verdict": "done",
			"fork": 0, "commit_sha": sha1, "branch": "main",
			"created_at": "2026-11-18 10:00:00", "updated_at": "2026-11-18 10:00:01"},
		{"id": "gen-2", "chat_id": p17ChatID, "parent": "gen-1", "status": "succeeded", "verdict": "approve",
			"fork": 0, "commit_sha": sha2, "branch": "main",
			"created_at": "2026-11-18 11:00:00", "updated_at": "2026-11-18 11:00:01"},
	}
	emulator.files["gen-2"] = []map[string]any{
		{"path": "app.js", "op": "delete", "before_hash": "h-app", "before_size": float64(5)},
		{"path": "index.html", "op": "modify", "before_hash": "h-one", "after_hash": "h-two",
			"before_size": float64(10), "after_size": float64(10)},
	}
	emulator.mu.Unlock()

	// 1) The list: two rows, newest first, both linked to their commit.
	status, body := p17Request(t, app, http.MethodGet, "/api/generations/"+p17ChatID)
	if status != 200 {
		t.Fatalf("list status = %d (%v)", status, body)
	}
	list, _ := body["data"].(map[string]any)["generations"].([]any)
	if len(list) != 2 {
		t.Fatalf("listed %d generations, want 2", len(list))
	}
	newest, _ := list[0].(map[string]any)
	if newest["id"] != "gen-2" || newest["commitSha"] != sha2 || newest["parent"] != "gen-1" {
		t.Fatalf("newest row = %v, want gen-2 linked to %s with parent gen-1", newest, sha2)
	}
	if newest["branch"] != "main" {
		t.Errorf("branch = %v, want main", newest["branch"])
	}

	// 2) The diff of gen-2 names the deleted and the modified file.
	status, body = p17Request(t, app, http.MethodGet, "/api/generations/"+p17ChatID+"/gen-2/diff")
	if status != 200 {
		t.Fatalf("diff status = %d (%v)", status, body)
	}
	diffData, _ := body["data"].(map[string]any)
	if diffData["commitSha"] != sha2 {
		t.Errorf("diff commitSha = %v, want %s", diffData["commitSha"], sha2)
	}
	files, _ := diffData["files"].([]any)
	if len(files) != 2 {
		t.Fatalf("diff listed %d files, want 2", len(files))
	}
	first, _ := files[0].(map[string]any)
	if first["path"] != "app.js" || first["op"] != "delete" {
		t.Errorf("first diff row = %v/%v, want app.js delete", first["path"], first["op"])
	}

	// 3) Rollback to gen-1: a NEW commit, not a rewrite.
	status, body = p17Request(t, app, http.MethodPost, "/api/generations/"+p17ChatID+"/gen-1/rollback")
	if status != 200 {
		t.Fatalf("rollback status = %d (%v)", status, body)
	}
	rollbackData, _ := body["data"].(map[string]any)
	revertSHA, _ := rollbackData["commitSha"].(string)
	if revertSHA == "" || revertSHA == sha1 || revertSHA == sha2 {
		t.Fatalf("rollback commit = %q, want a new commit distinct from %s/%s", revertSHA, sha1, sha2)
	}
	if restored, _ := rollbackData["restoredFiles"].(float64); restored != 2 {
		t.Errorf("restoredFiles = %v, want gen-1's 2 files", rollbackData["restoredFiles"])
	}

	// 4) The revert really carries gen-1's tree, and HEAD moved onto it. HEAD is
	//    read through a FRESH handle on purpose: refs are primed at Init, so the
	//    handle this test opened earlier still holds the pre-revert cache — the
	//    same reason every HTTP request opens its own handle.
	tree, err := repo.FilesAt(ctx, revertSHA)
	if err != nil {
		t.Fatalf("FilesAt(revert): %v", err)
	}
	if tree["index.html"] != "<h1>one</h1>" || tree["app.js"] != "one()" {
		t.Fatalf("restored tree = %#v, want gen-1's files", tree)
	}
	freshRepo, err := engine.OpenGitRepo(ctx, d1, p17ChatID)
	if err != nil {
		t.Fatalf("OpenGitRepo (fresh): %v", err)
	}
	head, err := freshRepo.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head != revertSHA {
		t.Errorf("HEAD = %s, want the revert commit %s", head, revertSHA)
	}

	// 5) The rollback is audited, and the lineage list is unchanged: a revert is
	//    a Git commit, not a new generation row.
	emulator.mu.Lock()
	audited := false
	for _, audit := range emulator.audits {
		if p17AsString(audit["action"]) == "rollback" {
			audited = true
		}
	}
	emulator.mu.Unlock()
	if !audited {
		t.Error("no rollback audit row was written")
	}
	status, body = p17Request(t, app, http.MethodGet, "/api/generations/"+p17ChatID)
	if status != 200 {
		t.Fatalf("list after rollback = %d", status)
	}
	if got := len(body["data"].(map[string]any)["generations"].([]any)); got != 2 {
		t.Errorf("list after rollback = %d rows, want the same 2 (a revert adds no generation)", got)
	}
}

// p17Request performs one request against the in-process control plane and
// decodes the JSON envelope.
func p17Request(t *testing.T, app *fiber.App, method, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}
