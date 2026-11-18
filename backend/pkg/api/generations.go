package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"backend/pkg/cloudflare"
	"backend/pkg/engine"
)

// ---------- History API (P1.4) ----------
//
// Generation and VFS live in the Go control plane, so history lives here (spec
// `docs/DEV_SPEC_P1A.md` P1.4):
//
//	GET  /api/generations/:chatId                 — the generation rows of one app
//	GET  /api/generations/:chatId/:genId/diff     — one run's recorded changes
//	POST /api/generations/:chatId/:genId/rollback — revert-commit + working-set restore
//
// Reads come from the lineage tables (`generations`, `generation_files`) that
// the recorder writes; every row is linked to a real Git commit through
// `commit_sha` (P1.3.5). Rollback is the only writer here: it creates a NEW
// commit — a revert never rewrites history — then restores the room's working
// set from that commit.
//
// Still out of scope (P1.4.5–P1.4.7): the GitHub mirror, PR creation and the
// import endpoint.

const (
	// generationHistoryLimit caps the list response.
	generationHistoryLimit = 50
	// generationListSQL / generationOneSQL read the lineage rows; `rowid`
	// breaks the created_at tie exactly like the recorder's parent lookup
	// (D1's CURRENT_TIMESTAMP has one-second resolution).
	generationListSQL = "SELECT id, chat_id, parent, status, verdict, fork, commit_sha, branch, created_at, updated_at FROM generations WHERE chat_id = ? ORDER BY created_at DESC, rowid DESC LIMIT ?"
	generationOneSQL  = "SELECT id, chat_id, parent, status, verdict, fork, commit_sha, branch, created_at, updated_at FROM generations WHERE id = ? AND chat_id = ? LIMIT 1"
	// generationFilesSQL reads one run's diff rows: op + hashes + sizes, never
	// contents (contents stay in the Git objects).
	generationFilesSQL = "SELECT path, op, before_hash, after_hash, before_size, after_size, author_agent FROM generation_files WHERE generation_id = ? ORDER BY path"
	// rollbackAuditSQL records the rollback next to the lineage trail.
	rollbackAuditSQL = "INSERT INTO generation_audits (id, generation_id, actor, action, detail_json, created_at) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)"
	// generationQueryTimeout bounds one request's D1 work.
	generationQueryTimeout = 10 * time.Second
)

// generationSummary is one row of the history list.
type generationSummary struct {
	ID        string  `json:"id"`
	ChatID    string  `json:"chatId"`
	Parent    *string `json:"parent"`
	Status    string  `json:"status"`
	Verdict   *string `json:"verdict"`
	Fork      bool    `json:"fork"`
	CommitSHA *string `json:"commitSha"`
	Branch    *string `json:"branch"`
	CreatedAt *string `json:"createdAt"`
	UpdatedAt *string `json:"updatedAt"`
}

// generationDiffFile is one recorded file change (no contents).
type generationDiffFile struct {
	Path       string  `json:"path"`
	Op         string  `json:"op"`
	BeforeHash *string `json:"beforeHash"`
	AfterHash  *string `json:"afterHash"`
	BeforeSize *int    `json:"beforeSize"`
	AfterSize  *int    `json:"afterSize"`
	Author     *string `json:"author"`
}

// handleGenerationList implements GET /api/generations/:chatId.
//
// The route runs the session boundary and the ownership guard first, so this
// handler only ever lists the caller's own app.
func handleGenerationList(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		d1 := hub.D1Client()
		if d1 == nil {
			return c.Status(503).JSON(fiber.Map{"success": false, "error": "generation history (D1) is not configured"})
		}
		chatID := c.Params("chatId")
		if chatID == "" {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "chatId is required"})
		}

		ctx, cancel := context.WithTimeout(c.Context(), generationQueryTimeout)
		defer cancel()
		rows, err := d1.Query(ctx, generationListSQL, chatID, generationHistoryLimit)
		if err != nil {
			log.Printf("[api] generation history query failed for %s: %v", chatID, err)
			return c.Status(502).JSON(fiber.Map{"success": false, "error": "generation history is unavailable"})
		}

		out := make([]generationSummary, 0, len(rows))
		for _, row := range rows {
			out = append(out, generationSummaryOf(row))
		}
		return c.JSON(fiber.Map{
			"success": true,
			"data":    fiber.Map{"chatId": chatID, "generations": out},
		})
	}
}

// handleGenerationDiff implements GET /api/generations/:chatId/:genId/diff.
func handleGenerationDiff(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		d1 := hub.D1Client()
		if d1 == nil {
			return c.Status(503).JSON(fiber.Map{"success": false, "error": "generation history (D1) is not configured"})
		}
		chatID, genID := c.Params("chatId"), c.Params("genId")
		if chatID == "" || genID == "" {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "chatId and genId are required"})
		}

		ctx, cancel := context.WithTimeout(c.Context(), generationQueryTimeout)
		defer cancel()
		gen, err := loadGeneration(ctx, d1, chatID, genID)
		if err != nil {
			log.Printf("[api] generation lookup failed for %s/%s: %v", chatID, genID, err)
			return c.Status(502).JSON(fiber.Map{"success": false, "error": "generation history is unavailable"})
		}
		if gen == nil {
			return c.Status(404).JSON(fiber.Map{"success": false, "error": "generation not found"})
		}

		rows, err := d1.Query(ctx, generationFilesSQL, genID)
		if err != nil {
			log.Printf("[api] generation diff query failed for %s: %v", genID, err)
			return c.Status(502).JSON(fiber.Map{"success": false, "error": "generation diff is unavailable"})
		}
		files := make([]generationDiffFile, 0, len(rows))
		for _, row := range rows {
			files = append(files, generationDiffFile{
				Path:       d1Text(row["path"]),
				Op:         d1Text(row["op"]),
				BeforeHash: d1String(row["before_hash"]),
				AfterHash:  d1String(row["after_hash"]),
				BeforeSize: d1Int(row["before_size"]),
				AfterSize:  d1Int(row["after_size"]),
				Author:     d1String(row["author_agent"]),
			})
		}
		return c.JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"chatId":       chatID,
				"generationId": gen.ID,
				"commitSha":    gen.CommitSHA,
				"branch":       gen.Branch,
				"files":        files,
			},
		})
	}
}

// handleGenerationRollback implements POST /api/generations/:chatId/:genId/rollback.
//
// Sequence: (1) the generation must belong to this chat and carry a commit;
// (2) the repo creates a NEW commit restoring that tree (history is never
// rewritten); (3) when the room is live, its working set is replaced with the
// restored tree; (4) the action is recorded in generation_audits.
//
// A stopped room is not an error: the VFS mirror lives in Redis and the room
// reloads it on next start, so the response reports `applied: false` instead of
// pretending the caller's preview already changed.
func handleGenerationRollback(hub *engine.EngineHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		d1 := hub.D1Client()
		if d1 == nil {
			return c.Status(503).JSON(fiber.Map{"success": false, "error": "generation history (D1) is not configured"})
		}
		chatID, genID := c.Params("chatId"), c.Params("genId")
		if chatID == "" || genID == "" {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "chatId and genId are required"})
		}

		ctx, cancel := context.WithTimeout(c.Context(), generationQueryTimeout)
		defer cancel()
		gen, err := loadGeneration(ctx, d1, chatID, genID)
		if err != nil {
			log.Printf("[api] rollback lookup failed for %s/%s: %v", chatID, genID, err)
			return c.Status(502).JSON(fiber.Map{"success": false, "error": "generation history is unavailable"})
		}
		if gen == nil {
			return c.Status(404).JSON(fiber.Map{"success": false, "error": "generation not found"})
		}
		if gen.CommitSHA == nil || *gen.CommitSHA == "" {
			return c.Status(409).JSON(fiber.Map{"success": false, "error": "generation has no internal Git revision"})
		}

		repo, err := engine.OpenGitRepo(ctx, d1, chatID)
		if err != nil {
			log.Printf("[api] rollback: open repo %s: %v", chatID, err)
			return c.Status(502).JSON(fiber.Map{"success": false, "error": "internal Git history is unavailable"})
		}
		newSHA, err := repo.Revert(ctx, *gen.CommitSHA, "rollback")
		if err != nil {
			log.Printf("[api] rollback: revert %s: %v", genID, err)
			return c.Status(502).JSON(fiber.Map{"success": false, "error": "rollback could not be committed"})
		}
		files, err := repo.FilesAt(ctx, newSHA)
		if err != nil {
			log.Printf("[api] rollback: read restored revision %s: %v", newSHA, err)
			return c.Status(502).JSON(fiber.Map{"success": false, "error": "restored revision could not be read"})
		}

		applied := false
		if room, ok := hub.GetRoom(chatID); ok && room != nil {
			applyWorkingSet(room, files)
			applied = true
		}

		payload, merr := json.Marshal(map[string]interface{}{
			"target":         genID,
			"commit":         newSHA,
			"restored_files": len(files),
			"applied":        applied,
		})
		if merr != nil {
			payload = []byte("{}")
		}
		if _, err := d1.Exec(ctx, rollbackAuditSQL, uuid.NewString(), genID, "user", "rollback", string(payload)); err != nil {
			// The rollback already happened; a missing audit row must not turn a
			// successful restore into a failure.
			log.Printf("[api] rollback audit row failed for %s: %v", genID, err)
		}

		return c.JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"chatId":        chatID,
				"generationId":  genID,
				"commitSha":     newSHA,
				"restoredFiles": len(files),
				"applied":       applied,
			},
		})
	}
}

// applyWorkingSet replaces the room's working set with files: delete the paths
// that only exist now, then write everything in the restored tree. Both go
// through UpsertFile/DeleteFile so Redis and the write-attribution intake stay
// consistent (author "rollback").
func applyWorkingSet(room *engine.ProjectRoom, files map[string]string) {
	for _, entry := range room.GetVFS() {
		if entry == nil {
			continue
		}
		if _, keep := files[entry.FilePath]; !keep {
			room.DeleteFile(entry.FilePath, "rollback")
		}
	}
	for path, content := range files {
		room.UpsertFile(path, content, "rollback")
	}
}

// loadGeneration reads one lineage row scoped to its chat (nil when absent).
func loadGeneration(ctx context.Context, d1 *cloudflare.D1Client, chatID, genID string) (*generationSummary, error) {
	rows, err := d1.Query(ctx, generationOneSQL, genID, chatID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	summary := generationSummaryOf(rows[0])
	return &summary, nil
}

// generationSummaryOf maps one D1 row onto the API shape.
func generationSummaryOf(row map[string]interface{}) generationSummary {
	return generationSummary{
		ID:        d1Text(row["id"]),
		ChatID:    d1Text(row["chat_id"]),
		Parent:    d1String(row["parent"]),
		Status:    d1Text(row["status"]),
		Verdict:   d1String(row["verdict"]),
		Fork:      d1Bool(row["fork"]),
		CommitSHA: d1String(row["commit_sha"]),
		Branch:    d1String(row["branch"]),
		CreatedAt: d1String(row["created_at"]),
		UpdatedAt: d1String(row["updated_at"]),
	}
}

// D1 returns string | float64 | bool | nil; these normalise a row here instead
// of leaking interface{} into the API types.

func d1String(v interface{}) *string {
	if v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return &s
}

// d1Text is d1String for non-nullable fields.
func d1Text(v interface{}) string {
	if s := d1String(v); s != nil {
		return *s
	}
	return ""
}

func d1Int(v interface{}) *int {
	switch n := v.(type) {
	case float64:
		i := int(n)
		return &i
	case int:
		i := n
		return &i
	case int64:
		i := int(n)
		return &i
	case string:
		var i int
		if _, err := fmt.Sscanf(n, "%d", &i); err != nil {
			return nil
		}
		return &i
	}
	return nil
}

func d1Bool(v interface{}) bool {
	switch b := v.(type) {
	case bool:
		return b
	case float64:
		return b != 0
	case int64:
		return b != 0
	case string:
		return b == "1" || b == "true"
	}
	return false
}
