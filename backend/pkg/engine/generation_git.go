package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"

	"backend/pkg/cloudflare"
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
	// generationCommitMaxFileBytes is the per-file ceiling a revision may carry
	// (P1.3.6). 1 MiB sits safely under the 2,000,000-byte hard cap on one D1
	// row / string (docs/CF_LIMITS.md §8 `[S7]`) even after the storer's zlib +
	// base64 encoding. A larger file is SKIPPED, never truncated: a half file
	// in history would be worse than a missing one.
	generationCommitMaxFileBytes = 1 << 20
)

// generationSecretPattern is the P1.3.6 credential rule: a credential-ish key
// assigned a value (`token = ...`, `apiKey: ...`, `PASSWORD=...`).
//
// The spec's rule is the keyword + separator; committing on that alone would
// block a large share of legitimate app code, because a login form writes
// `password: e.target.value` and `const password = form.password.value`. The
// separator match is therefore only the first stage — the value must also look
// like literal key material (`generationValueIsLiteral`), so property access,
// environment lookups, empty values and documented placeholders pass through.
// Everything that does look like a hardcoded credential blocks the commit.
var generationSecretPattern = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_])(token|secret|api[_-]?key|password|passwd)\s*[:=]\s*(.+)$`)

// generationCredentialPlaceholders are values that carry no secret material.
var generationCredentialPlaceholders = map[string]struct{}{
	"": {}, "null": {}, "undefined": {}, "true": {}, "false": {},
	"password": {}, "passwd": {}, "secret": {}, "token": {},
	"changeme": {}, "xxx": {}, "***": {},
}

// generationSecretMarkers mark an obvious non-secret even inside a quoted
// value ("your-api-key-here", "example-token", "{{API_KEY}}").
var generationSecretMarkers = []string{"your-", "your_", "example", "placeholder", "dummy", "xxxx", "{{", "<"}

// generationCommitSkip records one file left out of a revision (P1.3.6 size
// cap) so the skip is auditable instead of silent.
type generationCommitSkip struct {
	Path   string
	Size   int
	Reason string
}

// generationSecretFinding records the file (and line) whose content blocked
// the commit. The matched VALUE is deliberately not carried: an audit row or a
// log line must never become the leak the gate exists to prevent.
type generationSecretFinding struct {
	Path string
	Line int
}

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

	// P1.3.6 pre-commit gate. It sits on the way INTO the repo, so a blocked
	// generation simply produces no revision — the run itself already
	// succeeded and the user already has the files.
	if strings.TrimSpace(author) == "" {
		return "", errors.New("git: pre-commit: commit requires an explicit author (P1.3.6)")
	}
	kept, skipped, blocked := scanGenerationCommitFiles(files)
	for _, s := range skipped {
		log.Printf("[room:%s] git: %s (%d bytes) is over the %d-byte per-file cap; excluded from revision %s",
			r.chatID, s.Path, s.Size, generationCommitMaxFileBytes, genID)
	}
	writeGenerationCommitAudits(r.d1, genID, skipped, blocked)
	if len(blocked) > 0 {
		return "", fmt.Errorf("git: pre-commit: refusing revision %s: credential-like assignment at %s (P1.3.6)",
			genID, formatGenerationSecretFindings(blocked))
	}
	if len(kept) == 0 {
		return "", nil
	}
	files = kept

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

// ---------- P1.3.6 pre-commit gate ----------

// scanGenerationCommitFiles applies the two content rules of P1.3.6 to one
// revision: the per-file size cap and the credential scan. Paths are visited in
// sorted order so the findings — and therefore the refusal error and the audit
// rows — are deterministic.
func scanGenerationCommitFiles(files map[string]string) (map[string]string, []generationCommitSkip, []generationSecretFinding) {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	kept := make(map[string]string, len(files))
	var skipped []generationCommitSkip
	var blocked []generationSecretFinding
	for _, path := range paths {
		content := files[path]
		if len(content) > generationCommitMaxFileBytes {
			skipped = append(skipped, generationCommitSkip{
				Path:   path,
				Size:   len(content),
				Reason: "over the 1 MiB per-file cap",
			})
			continue
		}
		if line, ok := generationFirstCredentialLine(content); ok {
			blocked = append(blocked, generationSecretFinding{Path: path, Line: line})
			continue
		}
		kept[path] = content
	}
	return kept, skipped, blocked
}

// generationFirstCredentialLine returns the 1-based line of the first
// credential-like assignment in content.
func generationFirstCredentialLine(content string) (int, bool) {
	for i, line := range strings.Split(content, "\n") {
		if generationLineHasCredential(line) {
			return i + 1, true
		}
	}
	return 0, false
}

// generationLineHasCredential reports whether one line hardcodes a credential.
// The keyword + separator is only the first stage; see generationValueIsLiteral.
func generationLineHasCredential(line string) bool {
	m := generationSecretPattern.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	return generationValueIsLiteral(m[2])
}

// generationValueIsLiteral reports whether the right-hand side of a
// credential-ish assignment is literal key material rather than code.
//
// Not a leak: empty values, null/undefined, documented placeholders
// ("your-api-key-here", "{{API_KEY}}"), property access (`form.password`,
// `req.body.password`), env lookups (`process.env.API_KEY`), template
// interpolation and short unquoted identifiers. A leak: a quoted literal of 6+
// characters or a long opaque unquoted token — the shapes real keys have.
func generationValueIsLiteral(raw string) bool {
	v := strings.TrimSpace(raw)
	v = strings.TrimRight(v, " \t,;)}]")
	if v == "" {
		return false
	}

	quoted := false
	if q := v[0]; q == '"' || q == '\'' || q == '`' {
		end := strings.IndexByte(v[1:], q)
		if end < 0 {
			// Unterminated literal: cannot judge it, and blocking on a guess
			// would break ordinary code.
			return false
		}
		v = v[1 : 1+end]
		quoted = true
		if q == '`' && strings.Contains(v, "${") {
			return false
		}
	}

	lower := strings.ToLower(v)
	if _, ok := generationCredentialPlaceholders[lower]; ok {
		return false
	}
	for _, marker := range generationSecretMarkers {
		if strings.Contains(lower, marker) {
			return false
		}
	}

	if quoted {
		return len(v) >= 6
	}
	// Unquoted values: whitespace, dots, dollars, parentheses and braces mean
	// code (property access, env lookup, comparison, call), never key material.
	// What is left is an env-file style value, which counts when it is long or
	// mixes letters and digits — the shape of a real key.
	if len(v) < 8 || strings.ContainsAny(v, " \t.$()[]{}'\"") {
		return false
	}
	hasDigit := strings.ContainsAny(v, "0123456789")
	hasLetter := strings.ContainsAny(v, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	return len(v) >= 16 || (hasDigit && hasLetter)
}

// formatGenerationSecretFindings renders "path:line" pairs for the refusal
// error. It never includes the matched value.
func formatGenerationSecretFindings(findings []generationSecretFinding) string {
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, fmt.Sprintf("%s:%d", f.Path, f.Line))
	}
	return strings.Join(parts, ", ")
}

// writeGenerationCommitAudits records what the gate did, so a skipped file or a
// blocked revision is auditable long after the log line is gone. Best-effort
// and detached like writeGenerationAudits: the gate's outcome must not depend
// on the audit write.
func writeGenerationCommitAudits(d1 *cloudflare.D1Client, genID string, skipped []generationCommitSkip, blocked []generationSecretFinding) {
	if d1 == nil || (len(skipped) == 0 && len(blocked) == 0) {
		return
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[generation:%s] commit audit writer panic recovered: %v", genID, rec)
			}
		}()
		ctx, cancel := lineageCtx(nil)
		defer cancel()
		for _, s := range skipped {
			payload, err := json.Marshal(map[string]interface{}{
				"path":   s.Path,
				"size":   s.Size,
				"limit":  generationCommitMaxFileBytes,
				"reason": s.Reason,
			})
			if err != nil {
				payload = []byte("{}")
			}
			if _, err := d1.Exec(ctx, generationAuditInsertSQL, uuid.NewString(), genID, "system", "commit_file_skipped", string(payload)); err != nil {
				log.Printf("[generation:%s] audit row commit_file_skipped failed: %v", genID, err)
			}
		}
		for _, f := range blocked {
			payload, err := json.Marshal(map[string]interface{}{
				"path":   f.Path,
				"line":   f.Line,
				"reason": "credential-like assignment",
			})
			if err != nil {
				payload = []byte("{}")
			}
			if _, err := d1.Exec(ctx, generationAuditInsertSQL, uuid.NewString(), genID, "system", "commit_blocked", string(payload)); err != nil {
				log.Printf("[generation:%s] audit row commit_blocked failed: %v", genID, err)
			}
		}
	}()
}
