package engine

import (
	"context"
	"fmt"
	"strings"
)

// ---------- Per-generation Git commit (P1.3.5) ----------
//
// P1.3.4 gave the platform a real Git substrate over D1-CAS
// (`backend/pkg/engine/gitrepo.go`: Init/Commit/Log/Diff/Revert, real Git
// objects, one repo per app). P1.3.5 is its producer: exactly ONE commit per
// generation, taken at finalize time from the finished VFS and tagged
// `refs/gen/{genID}` so a rollback target stays addressable after HEAD moves.
//
// Why finalize and not the file writes: one generation is one revision.
// Committing from `UpsertFile`/`DeleteFile` would explode the history (a
// 20-step plan writes hundreds of files) and bury the diff a reviewer needs.
// It also matches the lineage recorder, which already writes one `generations`
// row per run.
//
// Everything here is best-effort, like the rest of the lineage path: no D1
// client, no open generation row, an empty VFS or an unusable repo must never
// fail a generation that already streamed to the user. The caller
// (`finalizeGeneration`) logs the returned error.
//
// P1.3.6 (pre-commit secret scan) belongs at the top of commitGeneration — the
// single point every revision passes through.

const (
	// generationCommitBranch is the branch every generation lands on. One
	// linear branch keeps `Log`/`Diff` simple; the per-generation ref is what
	// makes a specific run addressable.
	generationCommitBranch = "main"
	// generationCommitFallbackAuthor names the engine when the write
	// attribution is mixed or empty (a model that emitted fences the parser
	// attributed to no role).
	generationCommitFallbackAuthor = "engine"
)

// commitGeneration writes the finished VFS as one Git commit and stamps the
// lineage row with the resulting SHA. It returns ("", nil) whenever there is
// nothing to commit — a normal outcome, not an error:
//
//   - no open generation (the plan-execute path opens no lineage row at all),
//   - no D1 client (dev/CI, or credentials missing),
//   - an empty VFS.
//
// The generation id is consumed even when the commit fails, so a retried
// finalize can never produce a second commit for the same run.
func (r *ProjectRoom) commitGeneration() (string, error) {
	genID, author := r.takeGenerationCommitTarget()
	if genID == "" || r.d1 == nil {
		return "", nil
	}
	files := r.vfsContents()
	if len(files) == 0 {
		return "", nil
	}

	ctx, cancel := lineageCtx(context.Background())
	defer cancel()

	repo, err := OpenGitRepo(ctx, r.d1, r.chatID)
	if err != nil {
		return "", fmt.Errorf("git: open repo for %s: %w", r.chatID, err)
	}
	sha, err := repo.Commit(ctx, GitCommitRequest{
		Message:      fmt.Sprintf("generation %s (%d files)", genID, len(files)),
		Author:       author,
		GenerationID: genID,
		Files:        files,
	})
	if err != nil {
		return "", fmt.Errorf("git: commit generation %s: %w", genID, err)
	}

	// Link the lineage row to the revision. A failure here leaves the commit in
	// place (the repo is the source of truth) and only costs the row→SHA
	// pointer, so it is reported but never retried.
	if _, err := r.d1.Exec(ctx, generationCommitSQL, sha, generationCommitBranch, genID); err != nil {
		return sha, fmt.Errorf("git: link generation %s to %s: %w", genID, sha, err)
	}
	return sha, nil
}

// takeGenerationCommitTarget drains the commit target of the run that just
// finished: its id plus the author to record.
func (r *ProjectRoom) takeGenerationCommitTarget() (string, string) {
	r.lineageMu.Lock()
	defer r.lineageMu.Unlock()
	genID := r.lineageCurrentID
	author := strings.TrimSpace(r.lineageBatchAuthor)
	if author == "" {
		// finalize ran before FinishGenerationRecord drained the intake (the
		// legacy fence path, or a test driving finalize directly): the
		// per-path attribution is still live, so resolve it from there. The
		// map is read, never drained — the recorder still needs it.
		author = generationCommitAuthor(r.genAuthors)
	}
	r.lineageCurrentID = ""
	r.lineageBatchAuthor = ""
	if author == "" {
		author = generationCommitFallbackAuthor
	}
	return genID, author
}

// generationCommitAuthor resolves the batch author from the write-attribution
// intake (path → role). One distinct role authored the batch; mixed or empty
// attribution is reported as the engine rather than picking an arbitrary role.
func generationCommitAuthor(authors map[string]string) string {
	distinct := make(map[string]struct{}, len(authors))
	for _, author := range authors {
		if a := strings.TrimSpace(author); a != "" {
			distinct[a] = struct{}{}
		}
	}
	if len(distinct) != 1 {
		return generationCommitFallbackAuthor
	}
	for a := range distinct {
		return a
	}
	return generationCommitFallbackAuthor
}

// vfsContents flattens the VFS into the path → contents map GitRepo.Commit
// takes.
func (r *ProjectRoom) vfsContents() map[string]string {
	entries := r.GetVFS()
	out := make(map[string]string, len(entries))
	for path, entry := range entries {
		if entry == nil {
			continue
		}
		out[path] = entry.FileContents
	}
	return out
}
