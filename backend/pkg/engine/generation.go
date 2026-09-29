package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"backend/pkg/cloudflare"
)

// ---------- Generation lineage recorder (P1.2) ----------
//
// One row per generation run in D1 (`generations`), one row per touched path in
// `generation_files` (op + hashes + sizes only — the contents stay in the
// baseline snapshot) and an append-only trail in `generation_audits`. The
// baseline a diff is computed against is the VFS as it stood when the run
// started: written to Redis under `vfs:snap:{genID}` with a 7-day TTL, and
// mirrored in memory for the Redis-less path.
//
// Everything here is best-effort. A lineage failure (no D1 client, Redis down,
// snapshot evicted, tables not migrated yet) is logged and returned for the
// caller to log — it must never fail the generation itself. The Go control
// plane is the single writer of these tables (see `worker/database/schema.ts`);
// the runtime Worker never touches them.
//
// P1.3.1/P1.3.2 call the two record functions from runTeam and
// runDualModelPipeline; the per-file attribution intake is filled by
// RecordWriteAuthor (P1.3.3).
//
// Deliberately NOT written here: `commit_sha`/`branch` (P1.3.5 sets them after
// the internal Git commit) and `fork` (P1.3.3/P1.10.7 set it when a manual
// write lands while the agent is still running).
const (
	// generationSnapshotPrefix is the Redis key prefix of a run's VFS
	// baseline: `vfs:snap:{genID}`.
	generationSnapshotPrefix = "vfs:snap:"
	// generationSnapshotTTL is how long a baseline stays readable after the
	// run finished (finish-time diff of a run resumed in another process,
	// later inspection/rollback).
	generationSnapshotTTL = 7 * 24 * time.Hour
	// generationD1Timeout bounds every single D1 statement issued here.
	generationD1Timeout = 15 * time.Second
)

// D1 statements of the lineage recorder. Kept as constants so the emulated D1
// in generation_test.go can mirror them exactly.
const (
	// generationParentSQL resolves the lineage parent: the newest generation
	// that actually landed in this chat. `rowid` breaks the created_at tie —
	// D1's CURRENT_TIMESTAMP has one-second resolution and two generations
	// inside the same second must still chain in insertion order.
	generationParentSQL = "SELECT id FROM generations WHERE chat_id = ? AND status = 'succeeded' ORDER BY created_at DESC, rowid DESC LIMIT 1"
	// generationInsertSQL opens the run (status 'running', verdict NULL).
	generationInsertSQL = "INSERT INTO generations (id, chat_id, parent, status, created_at, updated_at) VALUES (?, ?, ?, 'running', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)"
	// generationCloseSQL closes the run with its verdict + terminal status.
	generationCloseSQL = "UPDATE generations SET verdict = ?, status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?"
	// generationFileInsertSQL writes one diff row. INSERT OR REPLACE keeps a
	// retried Finish for the same generation idempotent (natural key
	// (generation_id, path)).
	generationFileInsertSQL = "INSERT OR REPLACE INTO generation_files (generation_id, path, op, before_hash, after_hash, before_size, after_size, author_agent) VALUES (?, ?, ?, ?, ?, ?, ?, ?)"
	// generationAuditInsertSQL appends one audit row.
	generationAuditInsertSQL = "INSERT INTO generation_audits (id, generation_id, actor, action, detail_json, created_at) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)"
)

// generationKV is the narrow Redis surface the lineage recorder needs
// (snapshot put/get). It is satisfied by *redis.Client; tests inject an
// in-memory fake so the snapshot path is exercised without a live Redis.
type generationKV interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Get(ctx context.Context, key string) *redis.StringCmd
}

// snapshotFile is one file inside a VFS baseline: sha256 + size (what the diff
// compares) and the contents (what makes the snapshot a real baseline for
// later inspection/rollback).
type snapshotFile struct {
	Hash    string `json:"hash"`
	Size    int    `json:"size"`
	Content string `json:"content,omitempty"`
}

// generationSnapshot is the JSON payload stored at `vfs:snap:{genID}`.
type generationSnapshot struct {
	ChatID    string                  `json:"chat_id"`
	Parent    string                  `json:"parent,omitempty"`
	CreatedAt int64                   `json:"created_at"`
	Files     map[string]snapshotFile `json:"files"`
}

// generationFileDiff is one `generation_files` row. `Before*` is nil for
// `create`, `After*` is nil for `delete`.
type generationFileDiff struct {
	Path       string
	Op         string // create | modify | delete
	BeforeHash string
	AfterHash  string
	BeforeSize *int
	AfterSize  *int
	// Author is the agent that wrote the path ("" = unknown, stored as NULL).
	Author string
}

// generationAudit is one pending `generation_audits` row.
type generationAudit struct {
	actor  string
	action string
	detail map[string]interface{}
}

// hashVFSFile hashes one file's contents with sha256 (the hash column of a diff
// row).
func hashVFSFile(contents string) snapshotFile {
	sum := sha256.Sum256([]byte(contents))
	return snapshotFile{Hash: hex.EncodeToString(sum[:]), Size: len(contents)}
}

// generationKVOrNil resolves the Redis surface of the recorder: an injected
// store (tests) wins, then the room's client. A nil *redis.Client must never
// leak into the interface (a typed nil is not a nil interface).
func (r *ProjectRoom) generationKVOrNil() generationKV {
	if r.genKV != nil {
		return r.genKV
	}
	if r.rdb == nil {
		return nil
	}
	return r.rdb
}

// lineageCtx derives a bounded context that survives the caller's
// cancellation: the lineage rows are best-effort side writes, and a cancelled
// run (B9) must not silently drop the close row.
func lineageCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), generationD1Timeout)
}

// nullableString maps "" to SQL NULL (the parent/verdict columns are nullable).
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// nullableSize maps a missing size to SQL NULL (create has no before_size,
// delete has no after_size).
func nullableSize(size *int) interface{} {
	if size == nil {
		return nil
	}
	return *size
}

// intRef returns a pointer to a copy of v (diff rows carry optional sizes).
func intRef(v int) *int {
	return &v
}

// snapshotVFS captures the room's live VFS as a generation baseline (hash +
// size + contents per path).
func (r *ProjectRoom) snapshotVFS() map[string]snapshotFile {
	entries := r.GetVFS()
	out := make(map[string]snapshotFile, len(entries))
	for path, entry := range entries {
		f := hashVFSFile(entry.FileContents)
		f.Content = entry.FileContents
		out[path] = f
	}
	return out
}

// currentVFSHashes captures the room's live VFS as hashes only — the "after"
// side of a diff never needs the content.
func (r *ProjectRoom) currentVFSHashes() map[string]snapshotFile {
	entries := r.GetVFS()
	out := make(map[string]snapshotFile, len(entries))
	for path, entry := range entries {
		out[path] = hashVFSFile(entry.FileContents)
	}
	return out
}

// storeGenerationSnapshot mirrors the baseline of genID in memory and resets
// the write-attribution intake for the new generation.
func (r *ProjectRoom) storeGenerationSnapshot(genID string, snap *generationSnapshot) {
	r.lineageMu.Lock()
	defer r.lineageMu.Unlock()
	r.lineageSnapshotID = genID
	r.lineageSnapshot = snap
	r.genAuthors = nil
}

// takeGenerationSnapshot consumes the baseline of genID: the in-memory mirror
// when it belongs to this generation (the Redis-less path), otherwise Redis.
// A miss returns nil and the run is closed without file rows.
func (r *ProjectRoom) takeGenerationSnapshot(genID string) *generationSnapshot {
	r.lineageMu.Lock()
	var mirror *generationSnapshot
	if r.lineageSnapshotID == genID {
		mirror = r.lineageSnapshot
		r.lineageSnapshotID = ""
		r.lineageSnapshot = nil
	}
	r.lineageMu.Unlock()
	if mirror != nil {
		return mirror
	}

	// No mirror for this generation: it was started with a working Redis
	// surface, or the room was restarted mid-run. Redis still has the
	// baseline.
	kv := r.generationKVOrNil()
	if kv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), generationD1Timeout)
	defer cancel()
	raw, err := kv.Get(ctx, generationSnapshotPrefix+genID).Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		log.Printf("[room:%s] generation %s snapshot read failed: %v", r.chatID, genID, err)
		return nil
	}
	var restored generationSnapshot
	if err := json.Unmarshal([]byte(raw), &restored); err != nil {
		log.Printf("[room:%s] generation %s snapshot decode failed: %v", r.chatID, genID, err)
		return nil
	}
	return &restored
}

// takeGenerationAuthors consumes the open generation's write-attribution
// intake (path → agent).
func (r *ProjectRoom) takeGenerationAuthors() map[string]string {
	r.lineageMu.Lock()
	defer r.lineageMu.Unlock()
	authors := r.genAuthors
	r.genAuthors = nil
	return authors
}

// RecordWriteAuthor records which agent wrote path during the current
// generation; FinishGenerationRecord stores it as
// `generation_files.author_agent` (and as the actor of a `file_written` audit
// row). An empty author clears the entry. P1.3.3 threads the explicit author of
// every write (UpsertFile/DeleteFile, the vfs_write tool, gapfill, import)
// through here — the attribution cannot be inferred inside the write wrapper,
// because gapfill never passes through it.
func (r *ProjectRoom) RecordWriteAuthor(path, author string) {
	if path == "" {
		return
	}
	r.lineageMu.Lock()
	defer r.lineageMu.Unlock()
	if strings.TrimSpace(author) == "" {
		delete(r.genAuthors, path)
		return
	}
	if r.genAuthors == nil {
		r.genAuthors = make(map[string]string)
	}
	r.genAuthors[path] = author
}

// normalizeGenerationVerdict keeps only the verdicts of the
// `generation_verdict` contract (worker/database/schema.ts); anything else
// (including "") is stored as SQL NULL instead of inventing a value.
func normalizeGenerationVerdict(verdict string) string {
	switch v := strings.ToLower(strings.TrimSpace(verdict)); v {
	case "approve", "request_changes", "done", "error":
		return v
	}
	return ""
}

// generationStatusForVerdict maps a terminal verdict onto the
// `generation_status` vocabulary:
//
//   - ""            → cancelled: the run was aborted before an outcome existed
//     (cancel/stop), so nothing is known about its files.
//   - error/failed  → failed.
//   - everything else (approve | done | request_changes | an off-contract
//     string) → succeeded. `request_changes` in particular DID land its files,
//     so the next generation chains from it; the verdict column already says
//     the reviewer was unhappy, and an unknown verdict is recorded as NULL
//     rather than silently becoming a fake failure.
func generationStatusForVerdict(verdict string) string {
	switch v := strings.ToLower(strings.TrimSpace(verdict)); v {
	case "":
		return "cancelled"
	case "error", "failed":
		return "failed"
	}
	return "succeeded"
}

// StartGenerationRecord opens the lineage row for one generation run and
// snapshots the current VFS as the diff baseline.
//
// chatID "" falls back to the room's own chat id. The returned error is
// advisory: the caller logs it and keeps generating (a missing D1 client or an
// unreachable Redis must never fail a run).
func (r *ProjectRoom) StartGenerationRecord(ctx context.Context, chatID string) (string, error) {
	if chatID == "" {
		chatID = r.chatID
	}
	if chatID == "" {
		return "", errors.New("generation: StartGenerationRecord: empty chat id")
	}

	genID := uuid.NewString()
	snap := &generationSnapshot{
		ChatID:    chatID,
		CreatedAt: time.Now().Unix(),
		Files:     r.snapshotVFS(),
	}

	// The in-memory mirror is written FIRST so the finish-time diff still works
	// in a Redis-less deployment (dev/CI) or when the snapshot write below
	// fails.
	r.storeGenerationSnapshot(genID, snap)

	var errs []error

	// Redis copy: the cross-process baseline (7-day TTL).
	if kv := r.generationKVOrNil(); kv != nil {
		payload, err := json.Marshal(snap)
		if err != nil {
			errs = append(errs, fmt.Errorf("generation: marshal snapshot: %w", err))
		} else {
			sctx, cancel := lineageCtx(ctx)
			if err := kv.Set(sctx, generationSnapshotPrefix+genID, string(payload), generationSnapshotTTL).Err(); err != nil {
				errs = append(errs, fmt.Errorf("generation: store snapshot: %w", err))
			}
			cancel()
		}
	}

	if r.d1 == nil {
		return genID, errors.Join(errs...)
	}

	// parent = the newest succeeded generation of this chat ("" = root).
	parent, err := r.lastSucceededGeneration(ctx, chatID)
	if err != nil {
		errs = append(errs, err)
	} else {
		snap.Parent = parent
	}

	dctx, cancel := lineageCtx(ctx)
	defer cancel()
	if _, err := r.d1.Exec(dctx, generationInsertSQL, genID, chatID, nullableString(snap.Parent)); err != nil {
		errs = append(errs, fmt.Errorf("generation: insert running row: %w", err))
	}
	return genID, errors.Join(errs...)
}

// lastSucceededGeneration returns the id of the newest succeeded generation of
// chatID ("" when the chat has none yet).
func (r *ProjectRoom) lastSucceededGeneration(ctx context.Context, chatID string) (string, error) {
	dctx, cancel := lineageCtx(ctx)
	defer cancel()
	rows, err := r.d1.Query(dctx, generationParentSQL, chatID)
	if err != nil {
		return "", fmt.Errorf("generation: parent lookup: %w", err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	return wfStr(rows[0], "id"), nil
}

// FinishGenerationRecord closes one generation run: it diffs the start
// snapshot against the VFS as it stands now, writes the `generation_files`
// rows, closes the `generations` row (verdict + terminal status) and appends
// the audit trail on a detached goroutine.
//
// The returned error is advisory: every lineage step is best-effort and a
// failure here must never fail the generation (callers log, they do not abort).
func (r *ProjectRoom) FinishGenerationRecord(ctx context.Context, genID, verdict string) error {
	if genID == "" {
		return errors.New("generation: FinishGenerationRecord: empty generation id")
	}

	status := generationStatusForVerdict(verdict)
	storedVerdict := normalizeGenerationVerdict(verdict)
	snap := r.takeGenerationSnapshot(genID)
	authors := r.takeGenerationAuthors()

	var rows []generationFileDiff
	hasBaseline := snap != nil
	if hasBaseline {
		rows = diffGenerationFiles(snap.Files, r.currentVFSHashes(), authors)
	} else {
		// Redis unavailable AND no in-memory mirror (e.g. the room was
		// restarted mid-run): the row is still closed, but reporting every
		// existing file as `create` would be a lie — so no file rows.
		log.Printf("[room:%s] generation %s has no VFS snapshot; closing without file rows", r.chatID, genID)
	}

	var errs []error
	if r.d1 != nil {
		cctx, cancel := lineageCtx(ctx)
		if _, err := r.d1.Exec(cctx, generationCloseSQL, nullableString(storedVerdict), status, genID); err != nil {
			errs = append(errs, fmt.Errorf("generation: close row: %w", err))
		}
		cancel()

		rowsCtx, rowsCancel := lineageCtx(ctx)
		for _, row := range rows {
			_, err := r.d1.Exec(rowsCtx, generationFileInsertSQL,
				genID, row.Path, row.Op,
				nullableString(row.BeforeHash), nullableString(row.AfterHash),
				nullableSize(row.BeforeSize), nullableSize(row.AfterSize),
				nullableString(row.Author))
			if err != nil {
				// A failing statement means D1 as a whole is unusable
				// (credentials, missing table): report once, not per row.
				errs = append(errs, fmt.Errorf("generation: file row %q: %w", row.Path, err))
				break
			}
		}
		rowsCancel()

		writeGenerationAudits(r.d1, genID, status, storedVerdict, snap, rows)
	}
	return errors.Join(errs...)
}

// diffGenerationFiles compares the baseline (before) with the VFS as it stands
// when the run finishes (after) and returns one row per touched path, sorted by
// path so the rows and the tests are deterministic. authorByPath carries the
// write-time attribution intake (P1.3.3).
func diffGenerationFiles(before, after map[string]snapshotFile, authorByPath map[string]string) []generationFileDiff {
	paths := make([]string, 0, len(before)+len(after))
	for path := range before {
		paths = append(paths, path)
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	out := make([]generationFileDiff, 0, len(paths))
	for _, path := range paths {
		b, hadBefore := before[path]
		a, hasAfter := after[path]
		row := generationFileDiff{Path: path, Author: authorByPath[path]}
		switch {
		case !hadBefore && hasAfter:
			row.Op = "create"
			row.AfterHash, row.AfterSize = a.Hash, intRef(a.Size)
		case hadBefore && !hasAfter:
			row.Op = "delete"
			row.BeforeHash, row.BeforeSize = b.Hash, intRef(b.Size)
		case hadBefore && hasAfter && b.Hash != a.Hash:
			row.Op = "modify"
			row.BeforeHash, row.BeforeSize = b.Hash, intRef(b.Size)
			row.AfterHash, row.AfterSize = a.Hash, intRef(a.Size)
		default:
			continue // untouched: no row
		}
		out = append(out, row)
	}
	return out
}

// writeGenerationAudits appends the generation's audit trail. It runs on a
// detached goroutine with its own background context: the trail must land even
// when the generation context already died, and an audit failure never surfaces
// as a generation error.
func writeGenerationAudits(d1 *cloudflare.D1Client, genID, status, verdict string, snap *generationSnapshot, rows []generationFileDiff) {
	if d1 == nil {
		return
	}

	finished := generationAudit{
		actor:  "system",
		action: "generation_finished",
		detail: map[string]interface{}{
			"status": status,
			"files":  len(rows),
		},
	}
	if verdict != "" {
		finished.detail["verdict"] = verdict
	}
	if snap != nil {
		finished.detail["chat_id"] = snap.ChatID
		if snap.Parent != "" {
			finished.detail["parent"] = snap.Parent
		}
	} else {
		finished.detail["diff_skipped"] = "no VFS snapshot"
	}
	audits := []generationAudit{finished}

	// Write-time attribution (P1.3.3): one row per attributed file.
	for _, row := range rows {
		if row.Author == "" {
			continue
		}
		audits = append(audits, generationAudit{
			actor:  row.Author,
			action: "file_written",
			detail: map[string]interface{}{"path": row.Path, "op": row.Op},
		})
	}

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[generation:%s] audit writer panic recovered: %v", genID, rec)
			}
		}()
		ctx, cancel := lineageCtx(nil)
		defer cancel()
		for _, a := range audits {
			payload, err := json.Marshal(a.detail)
			if err != nil {
				payload = []byte("{}")
			}
			if _, err := d1.Exec(ctx, generationAuditInsertSQL, uuid.NewString(), genID, a.actor, a.action, string(payload)); err != nil {
				log.Printf("[generation:%s] audit row %q failed: %v", genID, a.action, err)
			}
		}
	}()
}
