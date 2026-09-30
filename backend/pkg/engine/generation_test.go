package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"backend/pkg/cloudflare"
)

// ---------- test doubles ----------

// fakeGenerationKV is an in-memory generationKV standing in for Redis: it
// records the snapshot payloads and their TTLs so the 7-day baseline contract
// is assertable without a live Redis.
type fakeGenerationKV struct {
	mu     sync.Mutex
	values map[string]string
	ttls   map[string]time.Duration
}

func newFakeGenerationKV() *fakeGenerationKV {
	return &fakeGenerationKV{
		values: map[string]string{},
		ttls:   map[string]time.Duration{},
	}
}

func (f *fakeGenerationKV) Set(_ context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	s, ok := value.(string)
	if !ok {
		return redis.NewStatusResult("", fmt.Errorf("fake kv: value %T is not a string", value))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[key] = s
	f.ttls[key] = expiration
	return redis.NewStatusResult("OK", nil)
}

func (f *fakeGenerationKV) Get(_ context.Context, key string) *redis.StringCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.values[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(v, nil)
}

// raw returns the stored payload of key ("" when absent).
func (f *fakeGenerationKV) raw(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.values[key]
}

// ttl returns the write TTL of key (0 when absent).
func (f *fakeGenerationKV) ttl(key string) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ttls[key]
}

// fakeGenRow is one emulated `generations` row.
type fakeGenRow struct {
	id      string
	chatID  string
	parent  string
	status  string
	verdict string
}

// fakeFileRow is one emulated `generation_files` row.
type fakeFileRow struct {
	genID      string
	path       string
	op         string
	beforeHash string
	afterHash  string
	beforeSize *int
	afterSize  *int
	author     string
}

// fakeAuditRow is one emulated `generation_audits` row.
type fakeAuditRow struct {
	genID  string
	actor  string
	action string
	detail string
}

// fakeD1 is a minimal in-memory emulation of the statements generation.go
// issues (running insert, succeeded-parent lookup, close, file rows, audit
// rows). It answers with the real D1 REST envelope shape, so the production
// cloudflare.D1Client — not a mock of it — is exercised end to end.
type fakeD1 struct {
	mu     sync.Mutex
	gens   []*fakeGenRow
	files  []fakeFileRow
	audits []fakeAuditRow
}

// newFakeD1 starts the emulated D1 REST server and returns it plus a
// cloudflare.D1Client pointed at it.
func newFakeD1(t *testing.T) (*fakeD1, *cloudflare.D1Client) {
	t.Helper()
	f := &fakeD1{}
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

// apply executes one statement against the emulated tables. Callers hold mu.
func (f *fakeD1) apply(sql string, params []any) []map[string]any {
	switch {
	case strings.HasPrefix(sql, "INSERT INTO generations"):
		f.gens = append(f.gens, &fakeGenRow{
			id:     paramString(params, 0),
			chatID: paramString(params, 1),
			parent: paramString(params, 2),
			status: "running",
		})

	case strings.HasPrefix(sql, "SELECT id FROM generations WHERE chat_id"):
		chatID := paramString(params, 0)
		// Newest rowid first, mirroring `ORDER BY created_at DESC, rowid DESC`.
		for i := len(f.gens) - 1; i >= 0; i-- {
			g := f.gens[i]
			if g.chatID == chatID && g.status == "succeeded" {
				return []map[string]any{{"id": g.id}}
			}
		}

	case strings.HasPrefix(sql, "UPDATE generations SET"):
		id := paramString(params, 2)
		for _, g := range f.gens {
			if g.id == id {
				g.verdict = paramString(params, 0)
				g.status = paramString(params, 1)
			}
		}

	case strings.HasPrefix(sql, "INSERT OR REPLACE INTO generation_files"):
		row := fakeFileRow{
			genID:      paramString(params, 0),
			path:       paramString(params, 1),
			op:         paramString(params, 2),
			beforeHash: paramString(params, 3),
			afterHash:  paramString(params, 4),
			beforeSize: paramInt(params, 5),
			afterSize:  paramInt(params, 6),
			author:     paramString(params, 7),
		}
		for i, existing := range f.files {
			if existing.genID == row.genID && existing.path == row.path {
				f.files[i] = row
				return nil
			}
		}
		f.files = append(f.files, row)

	case strings.HasPrefix(sql, "INSERT INTO generation_audits"):
		f.audits = append(f.audits, fakeAuditRow{
			genID:  paramString(params, 1),
			actor:  paramString(params, 2),
			action: paramString(params, 3),
			detail: paramString(params, 4),
		})
	}
	return nil
}

func (f *fakeD1) generation(t *testing.T, id string) fakeGenRow {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.gens {
		if g.id == id {
			return *g
		}
	}
	t.Fatalf("no generations row for %s", id)
	return fakeGenRow{}
}

func (f *fakeD1) fileRows() []fakeFileRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeFileRow(nil), f.files...)
}

// genRows returns a copy of every emulated `generations` row in insertion
// order (the lineage hook tests count rows and read their terminal state).
func (f *fakeD1) genRows() []fakeGenRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeGenRow, 0, len(f.gens))
	for _, g := range f.gens {
		out = append(out, *g)
	}
	return out
}

func (f *fakeD1) auditRows() []fakeAuditRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeAuditRow(nil), f.audits...)
}

// waitForAudits polls for n audit rows: the trail is written on a detached
// goroutine, so it is eventually consistent by design.
func (f *fakeD1) waitForAudits(t *testing.T, n int) []fakeAuditRow {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if rows := f.auditRows(); len(rows) >= n {
			return rows
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d audit rows (have %d)", n, len(f.auditRows()))
	return nil
}

// paramString reads a string bind value (SQL NULL and other types ⇒ "").
func paramString(params []any, i int) string {
	if i >= len(params) || params[i] == nil {
		return ""
	}
	s, _ := params[i].(string)
	return s
}

// paramInt reads an integer bind value; SQL NULL ⇒ nil (D1 decodes JSON
// numbers as float64).
func paramInt(params []any, i int) *int {
	if i >= len(params) || params[i] == nil {
		return nil
	}
	if fl, ok := params[i].(float64); ok {
		v := int(fl)
		return &v
	}
	return nil
}

// sha256Of is the independently computed expected hash (never the production
// helper) so the tests really pin the hashing contract.
func sha256Of(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum[:])
}

// TestRecordWriteAuthorIntakeDocumentsSemantics: pins the G0 intake contract
// RecordWriteAuthor implements — the exact behavior P1.3.3 wires every write
// path through. The assertions read through takeGenerationAuthors (the same
// consume path FinishGenerationRecord uses) rather than the private map, so
// they also pin that the intake is readable and single-use per generation.
func TestRecordWriteAuthorIntakeDocumentsSemantics(t *testing.T) {
	room := NewProjectRoom("chat-intake", nil, nil, nil)

	// An empty path is a no-op: no entry, and the intake stays nil. A caller
	// that loses the path cannot invent a phantom attribution row.
	room.RecordWriteAuthor("", "coder")
	if got := room.takeGenerationAuthors(); got != nil {
		t.Fatalf("empty path produced intake %v, want nil", got)
	}

	// A non-empty author is recorded against its path; the map is created
	// lazily because a fresh room has genAuthors == nil.
	room.RecordWriteAuthor("index.html", "coder")
	room.RecordWriteAuthor("public/app.js", "gapfill")
	got := room.takeGenerationAuthors()
	if len(got) != 2 {
		t.Fatalf("intake = %v, want 2 entries", got)
	}
	if got["index.html"] != "coder" || got["public/app.js"] != "gapfill" {
		t.Errorf("intake = %v, want index.html=coder and public/app.js=gapfill", got)
	}

	// Consuming clears the intake: attribution belongs to exactly one
	// generation, so the next run must not inherit the previous run's authors.
	if again := room.takeGenerationAuthors(); again != nil {
		t.Errorf("intake after consume = %v, want nil", again)
	}

	// A blank or whitespace-only author CLEARS the entry instead of storing an
	// empty string — that is what lets an unknown author become NULL
	// author_agent rather than a fake one.
	room.RecordWriteAuthor("a.txt", "coder")
	room.RecordWriteAuthor("b.txt", "reviewer")
	room.RecordWriteAuthor("a.txt", "   ")
	room.RecordWriteAuthor("b.txt", "")
	if cleared := room.takeGenerationAuthors(); len(cleared) != 0 {
		t.Errorf("intake after clearing = %v, want no entries", cleared)
	}

	// Re-recording a path overwrites it: the intake is last-write-wins, which
	// is what makes a mid-generation manual edit show up as the final author.
	room.RecordWriteAuthor("data.js", "coder")
	room.RecordWriteAuthor("data.js", "user")
	if last := room.takeGenerationAuthors(); last["data.js"] != "user" {
		t.Errorf("data.js author = %q, want the last write to win", last["data.js"])
	}
}

// ---------- tests ----------

const lineageChatID = "chat-lineage-1"

// TestGenerationRecordParentChain: two runs back to back ⇒ the second row's
// parent is the first one, the running row opens before the diff and the
// baseline lands in Redis under vfs:snap:{genID} with the 7-day TTL.
func TestGenerationRecordParentChain(t *testing.T) {
	kv := newFakeGenerationKV()
	d1, d1Client := newFakeD1(t)
	room := NewProjectRoom(lineageChatID, nil, nil, nil)
	room.SetD1Client(d1Client)
	room.genKV = kv
	room.UpsertFile("index.html", "<h1>v1</h1>", "")

	ctx := context.Background()

	gen1, err := room.StartGenerationRecord(ctx, lineageChatID)
	if err != nil {
		t.Fatalf("StartGenerationRecord (1): %v", err)
	}
	if gen1 == "" {
		t.Fatal("StartGenerationRecord returned an empty generation id")
	}

	// Baseline: Redis key present with the 7-day TTL and the chat's files.
	snapKey := generationSnapshotPrefix + gen1
	if got := kv.ttl(snapKey); got != generationSnapshotTTL {
		t.Errorf("snapshot TTL = %v, want %v", got, generationSnapshotTTL)
	}
	var stored generationSnapshot
	if err := json.Unmarshal([]byte(kv.raw(snapKey)), &stored); err != nil {
		t.Fatalf("snapshot payload is not decodable JSON: %v", err)
	}
	if stored.ChatID != lineageChatID {
		t.Errorf("snapshot chat_id = %q, want %q", stored.ChatID, lineageChatID)
	}
	if f, ok := stored.Files["index.html"]; !ok || f.Hash != sha256Of("<h1>v1</h1>") {
		t.Errorf("snapshot files = %+v, want index.html with its sha256", stored.Files)
	}

	// The row opens as running, with no parent (root of the chain).
	if row := d1.generation(t, gen1); row.status != "running" || row.parent != "" {
		t.Errorf("open row = %+v, want status running and no parent", row)
	}

	if err := room.FinishGenerationRecord(ctx, gen1, "done"); err != nil {
		t.Fatalf("FinishGenerationRecord (1): %v", err)
	}
	if row := d1.generation(t, gen1); row.status != "succeeded" || row.verdict != "done" {
		t.Errorf("closed row = %+v, want status succeeded / verdict done", row)
	}

	gen2, err := room.StartGenerationRecord(ctx, lineageChatID)
	if err != nil {
		t.Fatalf("StartGenerationRecord (2): %v", err)
	}
	if gen2 == gen1 {
		t.Fatal("two generations reused the same id")
	}
	if row := d1.generation(t, gen2); row.parent != gen1 {
		t.Errorf("second generation parent = %q, want %q", row.parent, gen1)
	}
	if err := room.FinishGenerationRecord(ctx, gen2, "approve"); err != nil {
		t.Fatalf("FinishGenerationRecord (2): %v", err)
	}

	// One generation_finished audit row per run (detached goroutine).
	audits := d1.waitForAudits(t, 2)
	for _, a := range audits {
		if a.action != "generation_finished" || a.actor != "system" {
			t.Errorf("audit row = %+v, want actor system / action generation_finished", a)
		}
	}
}

// TestGenerationStatusForVerdict pins the verdict → status contract:
// request_changes still counts as landed code, an empty verdict is a cancelled
// run and unknown verdicts are stored as NULL.
func TestGenerationStatusForVerdict(t *testing.T) {
	cases := []struct {
		verdict    string
		wantStatus string
		wantStored string
	}{
		{"approve", "succeeded", "approve"},
		{"done", "succeeded", "done"},
		{"request_changes", "succeeded", "request_changes"},
		{"error", "failed", "error"},
		{"", "cancelled", ""},
		{"whatever", "succeeded", ""},
	}
	for _, tc := range cases {
		if got := generationStatusForVerdict(tc.verdict); got != tc.wantStatus {
			t.Errorf("generationStatusForVerdict(%q) = %q, want %q", tc.verdict, got, tc.wantStatus)
		}
		if got := normalizeGenerationVerdict(tc.verdict); got != tc.wantStored {
			t.Errorf("normalizeGenerationVerdict(%q) = %q, want %q", tc.verdict, got, tc.wantStored)
		}
	}
}

// TestGenerationRecordDiffOps: create / modify / delete are detected against
// the start snapshot, untouched files produce no row, and the write-attribution
// intake lands in generation_files.author_agent (+ its own audit row).
func TestGenerationRecordDiffOps(t *testing.T) {
	kv := newFakeGenerationKV()
	d1, d1Client := newFakeD1(t)
	room := NewProjectRoom("chat-diff", nil, nil, nil)
	room.SetD1Client(d1Client)
	room.genKV = kv
	room.UpsertFile("a.txt", "one", "")
	room.UpsertFile("b.txt", "two", "")
	room.UpsertFile("keep.txt", "same", "")

	ctx := context.Background()
	gen, err := room.StartGenerationRecord(ctx, "chat-diff")
	if err != nil {
		t.Fatalf("StartGenerationRecord: %v", err)
	}

	// The run writes: b.txt modified, a.txt deleted, c.txt created.
	room.UpsertFile("b.txt", "TWO", "")
	room.DeleteFile("a.txt", "")
	room.UpsertFile("c.txt", "new", "")
	room.RecordWriteAuthor("c.txt", "gapfill")

	if err := room.FinishGenerationRecord(ctx, gen, "request_changes"); err != nil {
		t.Fatalf("FinishGenerationRecord: %v", err)
	}

	rows := d1.fileRows()
	if len(rows) != 3 {
		t.Fatalf("got %d file rows, want 3 (keep.txt is untouched): %+v", len(rows), rows)
	}
	// Sorted by path: a.txt (delete), b.txt (modify), c.txt (create).
	byPath := map[string]fakeFileRow{}
	for _, r := range rows {
		byPath[r.path] = r
	}

	del, ok := byPath["a.txt"]
	if !ok || del.op != "delete" {
		t.Fatalf("a.txt row = %+v, want op delete", del)
	}
	if del.beforeHash != sha256Of("one") || del.afterHash != "" || del.afterSize != nil {
		t.Errorf("delete row = %+v, want before_hash of %q and no after side", del, "one")
	}
	if del.beforeSize == nil || *del.beforeSize != len("one") {
		t.Errorf("delete row before_size = %v, want %d", del.beforeSize, len("one"))
	}

	mod, ok := byPath["b.txt"]
	if !ok || mod.op != "modify" {
		t.Fatalf("b.txt row = %+v, want op modify", mod)
	}
	if mod.beforeHash != sha256Of("two") || mod.afterHash != sha256Of("TWO") {
		t.Errorf("modify row hashes = %q -> %q, want the sha256 of two -> TWO", mod.beforeHash, mod.afterHash)
	}

	created, ok := byPath["c.txt"]
	if !ok || created.op != "create" {
		t.Fatalf("c.txt row = %+v, want op create", created)
	}
	if created.beforeHash != "" || created.beforeSize != nil || created.afterHash != sha256Of("new") {
		t.Errorf("create row = %+v, want no before side and the sha256 of new", created)
	}
	if created.author != "gapfill" {
		t.Errorf("create row author = %q, want gapfill", created.author)
	}

	if row := d1.generation(t, gen); row.status != "succeeded" || row.verdict != "request_changes" {
		t.Errorf("closed row = %+v, want request_changes to count as landed (succeeded)", row)
	}

	// Detached audit trail: one finished row + one attributed file row.
	audits := d1.waitForAudits(t, 2)
	var attributed bool
	for _, a := range audits {
		if a.action == "file_written" && a.actor == "gapfill" {
			attributed = true
			if !strings.Contains(a.detail, `"path":"c.txt"`) {
				t.Errorf("file_written detail = %s, want the path c.txt", a.detail)
			}
		}
	}
	if !attributed {
		t.Errorf("audits = %+v, want a file_written row with actor gapfill", audits)
	}
}

// TestGenerationRecordNilRedis: with no Redis AND no D1 client both calls are
// harmless no-ops for the run — the generation stays alive and the VFS is
// untouched.
func TestGenerationRecordNilRedis(t *testing.T) {
	room := NewProjectRoom("chat-nil", nil, nil, nil)
	room.UpsertFile("index.html", "<h1>x</h1>", "")
	ctx := context.Background()

	// chatID "" falls back to the room's own chat id.
	gen1, err := room.StartGenerationRecord(ctx, "")
	if err != nil {
		t.Fatalf("StartGenerationRecord without Redis: %v", err)
	}
	if gen1 == "" {
		t.Fatal("StartGenerationRecord without Redis returned an empty id")
	}

	room.UpsertFile("app.js", "console.log(1)", "")
	if err := room.FinishGenerationRecord(ctx, gen1, ""); err != nil {
		t.Fatalf("FinishGenerationRecord without Redis: %v", err)
	}

	// The generation stayed alive: the VFS is intact and a second run still
	// records (no panics, no stuck state).
	if got := len(room.GetVFS()); got != 2 {
		t.Errorf("VFS has %d files, want 2 (the generation must survive)", got)
	}
	gen2, err := room.StartGenerationRecord(ctx, "chat-nil")
	if err != nil {
		t.Fatalf("second StartGenerationRecord without Redis: %v", err)
	}
	if gen2 == gen1 {
		t.Error("two generations reused the same id without Redis")
	}
	if err := room.FinishGenerationRecord(ctx, gen2, "done"); err != nil {
		t.Fatalf("second FinishGenerationRecord without Redis: %v", err)
	}
}

// TestGenerationRecordDiffsWithoutRedis: with D1 configured but NO Redis
// surface, the in-memory baseline still produces the diff rows — the mirror is
// the Redis-less path, not a cache of the Redis copy.
func TestGenerationRecordDiffsWithoutRedis(t *testing.T) {
	d1, d1Client := newFakeD1(t)
	room := NewProjectRoom("chat-no-redis", nil, nil, nil)
	room.SetD1Client(d1Client)
	room.UpsertFile("main.css", "a{}", "")

	ctx := context.Background()
	gen, err := room.StartGenerationRecord(ctx, "chat-no-redis")
	if err != nil {
		t.Fatalf("StartGenerationRecord: %v", err)
	}
	room.UpsertFile("main.css", "a{color:red}", "")

	if err := room.FinishGenerationRecord(ctx, gen, "done"); err != nil {
		t.Fatalf("FinishGenerationRecord: %v", err)
	}

	rows := d1.fileRows()
	if len(rows) != 1 || rows[0].op != "modify" {
		t.Fatalf("file rows = %+v, want a single modify row from the mirror", rows)
	}
	if rows[0].beforeHash != sha256Of("a{}") || rows[0].afterHash != sha256Of("a{color:red}") {
		t.Errorf("hashes = %q -> %q, want the mirrored baseline of main.css", rows[0].beforeHash, rows[0].afterHash)
	}
	d1.waitForAudits(t, 1)
}

// TestGenerationRecordAdvisoryErrors covers the best-effort contract: a missing
// D1 client or an unknown generation id never panics, an empty id is reported
// (programmer error) and a vanished snapshot only skips the diff.
func TestGenerationRecordAdvisoryErrors(t *testing.T) {
	kv := newFakeGenerationKV()
	room := NewProjectRoom("chat-advisory", nil, nil, nil)
	room.genKV = kv
	room.UpsertFile("index.html", "<h1>x</h1>", "")
	ctx := context.Background()

	// Snapshot store present, D1 absent: the baseline is still written and the
	// caller gets no error.
	gen, err := room.StartGenerationRecord(ctx, "chat-advisory")
	if err != nil {
		t.Fatalf("StartGenerationRecord without D1: %v", err)
	}
	if kv.raw(generationSnapshotPrefix+gen) == "" {
		t.Error("baseline missing from the snapshot store")
	}
	if err := room.FinishGenerationRecord(ctx, gen, "done"); err != nil {
		t.Fatalf("FinishGenerationRecord without D1: %v", err)
	}

	// Unknown generation (no mirror, unknown Redis key): the diff is skipped
	// and nothing panics.
	if err := room.FinishGenerationRecord(ctx, "no-such-generation", "done"); err != nil {
		t.Fatalf("FinishGenerationRecord for an unknown id: %v", err)
	}

	// Empty ids are programmer errors and are reported: a room without a chat
	// id cannot open a lineage row, and Finish needs an id.
	noChat := NewProjectRoom("", nil, nil, nil)
	if _, err := noChat.StartGenerationRecord(ctx, ""); err == nil {
		t.Error("StartGenerationRecord with no chat id at all should fail")
	}
	if err := room.FinishGenerationRecord(ctx, "", "done"); err == nil {
		t.Error("FinishGenerationRecord with an empty id should fail")
	}
}

// TestFinishGenerationRecordRestoresSnapshotFromRedis: a run started in one
// room (process) can be finished by another — the baseline comes back from
// vfs:snap:{genID} and the diff is still a modify, not a bogus create.
func TestFinishGenerationRecordRestoresSnapshotFromRedis(t *testing.T) {
	kv := newFakeGenerationKV()
	d1, d1Client := newFakeD1(t)
	chatID := "chat-restore"

	starter := NewProjectRoom(chatID, nil, nil, nil)
	starter.SetD1Client(d1Client)
	starter.genKV = kv
	starter.UpsertFile("a.txt", "one", "")

	ctx := context.Background()
	gen, err := starter.StartGenerationRecord(ctx, chatID)
	if err != nil {
		t.Fatalf("StartGenerationRecord: %v", err)
	}
	starter.UpsertFile("a.txt", "one more", "")

	// The finisher never saw the start: its only baseline is Redis.
	finisher := NewProjectRoom(chatID, nil, nil, nil)
	finisher.SetD1Client(d1Client)
	finisher.genKV = kv
	finisher.UpsertFile("a.txt", "one more", "")

	if err := finisher.FinishGenerationRecord(ctx, gen, "done"); err != nil {
		t.Fatalf("FinishGenerationRecord: %v", err)
	}

	rows := d1.fileRows()
	if len(rows) != 1 {
		t.Fatalf("got %d file rows, want 1: %+v", len(rows), rows)
	}
	if rows[0].op != "modify" || rows[0].path != "a.txt" {
		t.Errorf("row = %+v, want a modify of a.txt", rows[0])
	}
	if rows[0].beforeHash != sha256Of("one") || rows[0].afterHash != sha256Of("one more") {
		t.Errorf("hashes = %q -> %q, want the restored baseline of a.txt", rows[0].beforeHash, rows[0].afterHash)
	}
	if row := d1.generation(t, gen); row.status != "succeeded" {
		t.Errorf("closed row = %+v, want status succeeded", row)
	}
	// Let the detached audit trail land before the emulated D1 goes away.
	d1.waitForAudits(t, 1)
}
