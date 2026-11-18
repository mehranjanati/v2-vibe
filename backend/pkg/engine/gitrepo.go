package engine

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"

	"backend/pkg/cloudflare"
)

// P1.3.4 — internal Git history over D1-CAS.
//
// The decision record (P1.3.0, docs/DEV_SPEC_P1A.md) puts the per-app repo in
// the SAME D1 as lineage: `git_objects` is a content-addressed loose-object
// store (`app_id`, `sha`, `kind`, zlib-compressed payload, size) and `git_refs`
// holds the mutable pointers. go-git stays the object model, so every SHA here
// is a REAL Git SHA — the History API (P1.4) and the GitHub mirror compare them
// against upstream HEAD.
//
// Constraints enforced here (docs/CF_LIMITS.md §8):
//   - content is only ever a bind parameter, never inlined into SQL text;
//   - one row per object (never one giant repo row);
//   - atomicity is "objects first, ref last" in code — D1 has no multi-statement
//     transaction we rely on, so a failed object write leaves the previous
//     history untouched and the caller must not close the generations row.
//
// Not persisted: packfiles (everything is loose) and ref REMOVAL
// (`RemoveReference`/`PackRefs` stay memory-only). Neither is used by
// Init/Open/Commit/Log/Diff/Revert, which is the surface P1.3.4 promises.

// D1 statements of the internal repo. Constants so the emulated D1 in
// gitrepo_test.go mirrors them exactly.
const (
	gitObjectInsertSQL = "INSERT OR IGNORE INTO git_objects (app_id, sha, kind, content, size) VALUES (?, ?, ?, ?, ?)"
	gitObjectSelectSQL = "SELECT content, size, kind FROM git_objects WHERE app_id = ? AND sha = ?"
	gitObjectExistsSQL = "SELECT sha FROM git_objects WHERE app_id = ? AND sha = ?"
	gitRefUpsertSQL    = "INSERT INTO git_refs (app_id, name, sha, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP) " +
		"ON CONFLICT(app_id, name) DO UPDATE SET sha = excluded.sha, updated_at = CURRENT_TIMESTAMP"
	gitRefSelectSQL = "SELECT sha FROM git_refs WHERE app_id = ? AND name = ?"
	gitRefListSQL   = "SELECT name, sha FROM git_refs WHERE app_id = ?"
)

const (
	// gitMainRef is HEAD of the per-app history; every generation commit is
	// also tagged with its own ref so a rollback target stays addressable.
	gitMainRef      = "refs/heads/main"
	gitGenRefPrefix = "refs/gen/"
	// gitD1Timeout bounds every single D1 statement issued by the repo.
	gitD1Timeout = 15 * time.Second
	// gitCommitEmail is the synthetic committer address (Git requires one).
	gitCommitEmail = "vibe@vibe.local"
)

// ErrGitRepoUnavailable signals that no D1 client is attached (dev/CI without
// control-plane credentials). Callers treat history as advisory, like lineage.
var ErrGitRepoUnavailable = errors.New("engine: git repo store unavailable")

// d1Storer is a go-git storage.Storer: objects and refs live in D1, while the
// embedded memory.Storage provides the in-process cache and the interfaces that
// do not need persistence (index, shallow, module, config).
type d1Storer struct {
	*memory.Storage
	appID string
	d1    *cloudflare.D1Client
}

// newD1Storer builds an empty storer for appID. Call loadRefs before reading.
func newD1Storer(appID string, d1 *cloudflare.D1Client) *d1Storer {
	return &d1Storer{Storage: memory.NewStorage(), appID: appID, d1: d1}
}

// encodedPayload returns the uncompressed object payload (what Git hashes).
func encodedPayload(obj plumbing.EncodedObject) ([]byte, error) {
	r, err := obj.Reader()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// SetEncodedObject stores the object in the memory cache AND writes the loose
// object to D1 before returning, so a returned hash is durable.
func (s *d1Storer) SetEncodedObject(obj plumbing.EncodedObject) (plumbing.Hash, error) {
	h, err := s.Storage.SetEncodedObject(obj)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if s.d1 == nil {
		return plumbing.ZeroHash, ErrGitRepoUnavailable
	}
	raw, err := encodedPayload(obj)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if err := s.putObject(h, obj.Type(), raw); err != nil {
		return plumbing.ZeroHash, err
	}
	return h, nil
}

// putObject compresses + base64-encodes the payload and stores one row. The
// content is a bind parameter (never SQL text) per docs/CF_LIMITS.md §8.
func (s *d1Storer) putObject(h plumbing.Hash, t plumbing.ObjectType, raw []byte) error {
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(raw); err != nil {
		_ = zw.Close()
		return fmt.Errorf("gitrepo: compress %s: %w", h, err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("gitrepo: compress %s: %w", h, err)
	}
	encoded := base64.StdEncoding.EncodeToString(compressed.Bytes())

	ctx, cancel := context.WithTimeout(context.Background(), gitD1Timeout)
	defer cancel()
	if _, err := s.d1.Exec(ctx, gitObjectInsertSQL, s.appID, h.String(), t.String(), encoded, len(raw)); err != nil {
		return fmt.Errorf("gitrepo: store object %s: %w", h, err)
	}
	return nil
}

// EncodedObject resolves an object from memory first, then D1. A decoded
// object is cached in memory so a commit walk reads each object once.
func (s *d1Storer) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	if obj, err := s.Storage.EncodedObject(t, h); err == nil {
		return obj, nil
	}
	if s.d1 == nil {
		return nil, plumbing.ErrObjectNotFound
	}
	raw, kind, err := s.loadObject(h)
	if err != nil {
		return nil, err
	}
	objType := plumbing.ObjectType(0)
	for _, candidate := range []plumbing.ObjectType{
		plumbing.CommitObject, plumbing.TreeObject, plumbing.BlobObject, plumbing.TagObject,
	} {
		if candidate.String() == kind {
			objType = candidate
			break
		}
	}
	if objType == 0 {
		return nil, fmt.Errorf("gitrepo: object %s has unknown kind %q", h, kind)
	}
	if t != plumbing.AnyObject && objType != t {
		return nil, plumbing.ErrObjectNotFound
	}
	obj := s.Storage.NewEncodedObject()
	obj.SetType(objType)
	w, err := obj.Writer()
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(raw); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	// Cache only: never re-write an object that came from D1.
	if _, err := s.Storage.SetEncodedObject(obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// HasEncodedObject reports whether the object exists locally or in D1.
func (s *d1Storer) HasEncodedObject(h plumbing.Hash) error {
	if err := s.Storage.HasEncodedObject(h); err == nil {
		return nil
	}
	if s.d1 == nil {
		return plumbing.ErrObjectNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitD1Timeout)
	defer cancel()
	rows, err := s.d1.Query(ctx, gitObjectExistsSQL, s.appID, h.String())
	if err != nil {
		return fmt.Errorf("gitrepo: has object %s: %w", h, err)
	}
	if len(rows) == 0 {
		return plumbing.ErrObjectNotFound
	}
	return nil
}

// EncodedObjectSize returns the uncompressed payload size.
func (s *d1Storer) EncodedObjectSize(h plumbing.Hash) (int64, error) {
	if size, err := s.Storage.EncodedObjectSize(h); err == nil {
		return size, nil
	}
	if s.d1 == nil {
		return 0, plumbing.ErrObjectNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitD1Timeout)
	defer cancel()
	rows, err := s.d1.Query(ctx, gitObjectSelectSQL, s.appID, h.String())
	if err != nil {
		return 0, fmt.Errorf("gitrepo: object size %s: %w", h, err)
	}
	if len(rows) == 0 {
		return 0, plumbing.ErrObjectNotFound
	}
	return int64(wfInt(rows[0], "size")), nil
}

// loadObject reads and decompresses one object row.
func (s *d1Storer) loadObject(h plumbing.Hash) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitD1Timeout)
	defer cancel()
	rows, err := s.d1.Query(ctx, gitObjectSelectSQL, s.appID, h.String())
	if err != nil {
		return nil, "", fmt.Errorf("gitrepo: load object %s: %w", h, err)
	}
	if len(rows) == 0 {
		return nil, "", plumbing.ErrObjectNotFound
	}
	compressed, err := base64.StdEncoding.DecodeString(wfStr(rows[0], "content"))
	if err != nil {
		return nil, "", fmt.Errorf("gitrepo: decode object %s: %w", h, err)
	}
	zr, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, "", fmt.Errorf("gitrepo: inflate object %s: %w", h, err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, "", fmt.Errorf("gitrepo: inflate object %s: %w", h, err)
	}
	return raw, wfStr(rows[0], "kind"), nil
}

// SetReference caches the ref and persists it. The ref write is the commit
// point of a history mutation, so callers move refs only after every object
// write succeeded.
func (s *d1Storer) SetReference(ref *plumbing.Reference) error {
	if err := s.Storage.SetReference(ref); err != nil {
		return err
	}
	if s.d1 == nil {
		return ErrGitRepoUnavailable
	}
	if ref.Hash().IsZero() {
		// Symbolic refs (HEAD -> refs/heads/main) stay memory-only; the
		// persisted graph is hash refs, which is all Init/Commit/Revert use.
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitD1Timeout)
	defer cancel()
	if _, err := s.d1.Exec(ctx, gitRefUpsertSQL, s.appID, string(ref.Name()), ref.Hash().String()); err != nil {
		return fmt.Errorf("gitrepo: store ref %s: %w", ref.Name(), err)
	}
	return nil
}

// Reference resolves a ref from memory first, then D1 (priming the cache).
func (s *d1Storer) Reference(name plumbing.ReferenceName) (*plumbing.Reference, error) {
	if ref, err := s.Storage.Reference(name); err == nil {
		return ref, nil
	}
	if s.d1 == nil {
		return nil, plumbing.ErrReferenceNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitD1Timeout)
	defer cancel()
	rows, err := s.d1.Query(ctx, gitRefSelectSQL, s.appID, string(name))
	if err != nil {
		return nil, fmt.Errorf("gitrepo: read ref %s: %w", name, err)
	}
	if len(rows) == 0 {
		return nil, plumbing.ErrReferenceNotFound
	}
	sha := wfStr(rows[0], "sha")
	if sha == "" {
		return nil, plumbing.ErrReferenceNotFound
	}
	ref := plumbing.NewHashReference(name, plumbing.NewHash(sha))
	_ = s.Storage.SetReference(ref)
	return ref, nil
}

// loadRefs primes the memory ref cache from D1 so IterReferences and
// CountLooseRefs see the persisted refs.
func (s *d1Storer) loadRefs(ctx context.Context) error {
	if s.d1 == nil {
		return ErrGitRepoUnavailable
	}
	cctx, cancel := lineageCtx(ctx)
	defer cancel()
	rows, err := s.d1.Query(cctx, gitRefListSQL, s.appID)
	if err != nil {
		return fmt.Errorf("gitrepo: list refs: %w", err)
	}
	for _, row := range rows {
		name, sha := wfStr(row, "name"), wfStr(row, "sha")
		if name == "" || sha == "" {
			continue
		}
		_ = s.Storage.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(name), plumbing.NewHash(sha)))
	}
	return nil
}

// GitCommitRequest is one batch commit: every file in Files becomes a blob, the
// tree hierarchy is rebuilt from the paths, and the commit moves HEAD.
type GitCommitRequest struct {
	Message string
	// Author is the agent role or user that produced the batch. It is
	// mandatory (P1.3.6 turns the same rule into a pre-commit check).
	Author string
	// GenerationID, when set, also tags the commit as refs/gen/<id>, so a
	// rollback target stays addressable after HEAD moves.
	GenerationID string
	Files        map[string]string
}

// GitCommitInfo is one entry returned by Log.
type GitCommitInfo struct {
	Hash    string
	Message string
	Author  string
	When    time.Time
	Parents []string
}

// GitChange is one path-level entry of a Diff.
type GitChange struct {
	Action string // create | modify | delete
	Path   string
	From   string // blob sha before ("" on create)
	To     string // blob sha after ("" on delete)
}

// GitRepo is one app's internal Git history backed by D1-CAS.
type GitRepo struct {
	appID string
	d1    *cloudflare.D1Client
	st    *d1Storer
}

// NewGitRepo binds an app id to a D1 client. Call Init before use.
func NewGitRepo(d1 *cloudflare.D1Client, appID string) *GitRepo {
	return &GitRepo{appID: appID, d1: d1, st: newD1Storer(appID, d1)}
}

// AppID returns the app/chat id this repo belongs to.
func (r *GitRepo) AppID() string { return r.appID }

// Init opens (or initializes) the app's history: it primes the refs from D1 and
// returns the current HEAD ("" when the app has no commit yet). A nil D1 client
// is ErrGitRepoUnavailable, never a silent empty history.
func (r *GitRepo) Init(ctx context.Context) (string, error) {
	if r.appID == "" {
		return "", errors.New("gitrepo: empty app id")
	}
	if r.d1 == nil {
		return "", ErrGitRepoUnavailable
	}
	if err := r.st.loadRefs(ctx); err != nil {
		return "", err
	}
	return r.Head(ctx)
}

// OpenGitRepo is Init for callers that want the repo handle.
func OpenGitRepo(ctx context.Context, d1 *cloudflare.D1Client, appID string) (*GitRepo, error) {
	repo := NewGitRepo(d1, appID)
	if _, err := repo.Init(ctx); err != nil {
		return nil, err
	}
	return repo, nil
}

// Head returns the commit refs/heads/main points at ("" when the repo is empty).
func (r *GitRepo) Head(ctx context.Context) (string, error) {
	ref, err := r.st.Reference(gitMainRef)
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return ref.Hash().String(), nil
}

// Commit writes every file as a content-addressed blob, rebuilds the tree
// hierarchy and creates one commit on top of HEAD. Object rows are written
// before the ref moves, so an interrupted commit leaves the previous history
// intact (the commit point is the ref update, docs/CF_LIMITS.md §8).
func (r *GitRepo) Commit(ctx context.Context, req GitCommitRequest) (string, error) {
	if r.appID == "" {
		return "", errors.New("gitrepo: empty app id")
	}
	author := strings.TrimSpace(req.Author)
	if author == "" {
		return "", errors.New("gitrepo: commit requires an explicit author")
	}
	if len(req.Files) == 0 {
		return "", errors.New("gitrepo: commit has no files")
	}
	tree, err := r.writeTree(req.Files)
	if err != nil {
		return "", err
	}
	head, err := r.Head(ctx)
	if err != nil {
		return "", err
	}
	return r.commitTree(tree, head, req.Message, author, req.GenerationID)
}

// treeNode is the in-memory shape of one directory while a tree is built.
type treeNode struct {
	files []object.TreeEntry
	dirs  map[string]*treeNode
}

// writeTree turns the flattened path -> contents map into nested tree objects
// (Git has no flat paths: each directory is its own tree object).
func (r *GitRepo) writeTree(files map[string]string) (plumbing.Hash, error) {
	root := &treeNode{dirs: make(map[string]*treeNode)}
	for path, content := range files {
		clean := strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "./")
		clean = strings.TrimPrefix(clean, "/")
		if clean == "" {
			return plumbing.ZeroHash, errors.New("gitrepo: empty file path")
		}
		segments := strings.Split(clean, "/")
		for _, seg := range segments {
			if seg == "" || seg == "." || seg == ".." {
				return plumbing.ZeroHash, fmt.Errorf("gitrepo: unsafe file path %q", path)
			}
		}
		blob, err := r.writeBlob(content)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		node := root
		for _, dir := range segments[:len(segments)-1] {
			if node.dirs[dir] == nil {
				node.dirs[dir] = &treeNode{dirs: make(map[string]*treeNode)}
			}
			node = node.dirs[dir]
		}
		node.files = append(node.files, object.TreeEntry{
			Name: segments[len(segments)-1],
			Mode: filemode.Regular,
			Hash: blob,
		})
	}
	return r.writeTreeNode(root)
}

// writeTreeNode stores one directory tree bottom-up.
func (r *GitRepo) writeTreeNode(node *treeNode) (plumbing.Hash, error) {
	entries := make([]object.TreeEntry, 0, len(node.files)+len(node.dirs))
	entries = append(entries, node.files...)
	dirs := make([]string, 0, len(node.dirs))
	for name := range node.dirs {
		dirs = append(dirs, name)
	}
	sort.Strings(dirs)
	for _, name := range dirs {
		sub, err := r.writeTreeNode(node.dirs[name])
		if err != nil {
			return plumbing.ZeroHash, err
		}
		entries = append(entries, object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: sub})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return storeGitObject(r.st, &object.Tree{Entries: entries})
}

// writeBlob stores one file's contents as a blob object.
func (r *GitRepo) writeBlob(content string) (plumbing.Hash, error) {
	obj := r.st.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	w, err := obj.Writer()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if _, err := w.Write([]byte(content)); err != nil {
		_ = w.Close()
		return plumbing.ZeroHash, err
	}
	if err := w.Close(); err != nil {
		return plumbing.ZeroHash, err
	}
	return r.st.SetEncodedObject(obj)
}

// storeGitObject encodes any go-git encodable into a durable object.
func storeGitObject(st *d1Storer, enc interface {
	Encode(plumbing.EncodedObject) error
}) (plumbing.Hash, error) {
	obj := st.NewEncodedObject()
	if err := enc.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return st.SetEncodedObject(obj)
}

// commitTree creates the commit object and THEN moves the refs — the ref update
// is the commit point, so a failure before it leaves HEAD untouched.
func (r *GitRepo) commitTree(tree plumbing.Hash, parent, message, author, genID string) (string, error) {
	sig := object.Signature{Name: author, Email: gitCommitEmail, When: time.Now().UTC()}
	commit := &object.Commit{
		Author:    sig,
		Committer: sig,
		Message:   message,
		TreeHash:  tree,
	}
	if parent != "" {
		commit.ParentHashes = []plumbing.Hash{plumbing.NewHash(parent)}
	}
	h, err := storeGitObject(r.st, commit)
	if err != nil {
		return "", err
	}
	if err := r.st.SetReference(plumbing.NewHashReference(gitMainRef, h)); err != nil {
		return "", err
	}
	if genID != "" {
		genRef := plumbing.ReferenceName(gitGenRefPrefix + genID)
		if err := r.st.SetReference(plumbing.NewHashReference(genRef, h)); err != nil {
			return "", fmt.Errorf("gitrepo: tag generation %s: %w", genID, err)
		}
	}
	return h.String(), nil
}

// Log walks a revision backwards along first parents and returns the commits
// newest-first (limit <= 0 means "all"). An empty repo is an empty log, not an
// error.
func (r *GitRepo) Log(ctx context.Context, rev string, limit int) ([]GitCommitInfo, error) {
	h, err := r.revHash(ctx, rev)
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []GitCommitInfo
	seen := make(map[plumbing.Hash]bool)
	for h != plumbing.ZeroHash && (limit <= 0 || len(out) < limit) {
		if seen[h] {
			break
		}
		seen[h] = true
		commit, err := r.commitObject(h)
		if err != nil {
			return nil, err
		}
		info := GitCommitInfo{
			Hash:    h.String(),
			Message: strings.TrimSpace(commit.Message),
			Author:  commit.Author.Name,
			When:    commit.Author.When.UTC(),
		}
		for _, parent := range commit.ParentHashes {
			info.Parents = append(info.Parents, parent.String())
		}
		out = append(out, info)
		if len(commit.ParentHashes) == 0 {
			break
		}
		h = commit.ParentHashes[0]
	}
	return out, nil
}

// Diff lists the path-level changes between two revisions (ref names or commit
// SHAs). It returns op + blob hashes only, never file contents (the shape P1.4
// exposes).
func (r *GitRepo) Diff(ctx context.Context, from, to string) ([]GitChange, error) {
	fromCommit, err := r.commitByRev(ctx, from)
	if err != nil {
		return nil, err
	}
	toCommit, err := r.commitByRev(ctx, to)
	if err != nil {
		return nil, err
	}
	fromTree, err := r.treeObject(fromCommit.TreeHash)
	if err != nil {
		return nil, err
	}
	toTree, err := r.treeObject(toCommit.TreeHash)
	if err != nil {
		return nil, err
	}
	changes, err := object.DiffTreeContext(ctx, fromTree, toTree)
	if err != nil {
		return nil, fmt.Errorf("gitrepo: diff trees: %w", err)
	}
	out := make([]GitChange, 0, len(changes))
	for _, change := range changes {
		action := "modify"
		path := change.To.Name
		switch {
		case change.From.Name == "":
			action = "create"
		case change.To.Name == "":
			action = "delete"
			path = change.From.Name
		}
		entry := GitChange{Action: action, Path: path}
		if change.From.Name != "" {
			entry.From = change.From.TreeEntry.Hash.String()
		}
		if change.To.Name != "" {
			entry.To = change.To.TreeEntry.Hash.String()
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// Revert creates a NEW commit whose tree is the target revision's tree, on top
// of the current HEAD. History is append-only: nothing before HEAD is rewritten
// and the target commit stays addressable, which is what makes this the
// product's rollback primitive rather than a destructive reset.
func (r *GitRepo) Revert(ctx context.Context, target, author string) (string, error) {
	author = strings.TrimSpace(author)
	if author == "" {
		return "", errors.New("gitrepo: revert requires an explicit author")
	}
	targetHash, err := r.revHash(ctx, target)
	if err != nil {
		return "", err
	}
	targetCommit, err := r.commitObject(targetHash)
	if err != nil {
		return "", err
	}
	head, err := r.Head(ctx)
	if err != nil {
		return "", err
	}
	if head == "" {
		return "", errors.New("gitrepo: nothing to revert onto")
	}
	message := fmt.Sprintf("revert: %s restores %s", shortHash(plumbing.NewHash(head)), shortHash(targetHash))
	return r.commitTree(targetCommit.TreeHash, head, message, author, "")
}

// revHash resolves "" (HEAD), a full 40-hex SHA, or a ref name ("main",
// "refs/heads/main", "refs/gen/<id>").
func (r *GitRepo) revHash(ctx context.Context, rev string) (plumbing.Hash, error) {
	if rev == "" {
		rev = gitMainRef
	}
	if isFullHash(rev) {
		return plumbing.NewHash(rev), nil
	}
	name := rev
	if !strings.HasPrefix(name, "refs/") {
		name = "refs/heads/" + name
	}
	ref, err := r.st.Reference(plumbing.ReferenceName(name))
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return ref.Hash(), nil
}

// commitObject decodes one stored commit.
func (r *GitRepo) commitObject(h plumbing.Hash) (*object.Commit, error) {
	obj, err := r.st.EncodedObject(plumbing.CommitObject, h)
	if err != nil {
		return nil, fmt.Errorf("gitrepo: commit %s: %w", h, err)
	}
	return object.DecodeCommit(r.st, obj)
}

// commitByRev resolves a revision and decodes its commit.
func (r *GitRepo) commitByRev(ctx context.Context, rev string) (*object.Commit, error) {
	h, err := r.revHash(ctx, rev)
	if err != nil {
		return nil, err
	}
	return r.commitObject(h)
}

// treeObject decodes one stored tree.
func (r *GitRepo) treeObject(h plumbing.Hash) (*object.Tree, error) {
	obj, err := r.st.EncodedObject(plumbing.TreeObject, h)
	if err != nil {
		return nil, fmt.Errorf("gitrepo: tree %s: %w", h, err)
	}
	return object.DecodeTree(r.st, obj)
}

// FilesAt materialises one revision as path → contents (P1.4).
//
// Rollback uses it to restore the working set from the commit the revert just
// created, and the round-trip test uses it to prove VFS → Git → VFS is
// byte-identical. It walks the tree that `writeTree` produced, so the paths are
// the same flat paths the VFS uses.
func (r *GitRepo) FilesAt(ctx context.Context, rev string) (map[string]string, error) {
	commit, err := r.commitByRev(ctx, rev)
	if err != nil {
		return nil, err
	}
	tree, err := r.treeObject(commit.TreeHash)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	if err := r.collectTree(tree, "", out); err != nil {
		return nil, err
	}
	return out, nil
}

// collectTree walks one tree recursively into out.
func (r *GitRepo) collectTree(tree *object.Tree, prefix string, out map[string]string) error {
	for _, entry := range tree.Entries {
		path := entry.Name
		if prefix != "" {
			path = prefix + "/" + entry.Name
		}
		switch entry.Mode {
		case filemode.Dir:
			sub, err := r.treeObject(entry.Hash)
			if err != nil {
				return err
			}
			if err := r.collectTree(sub, path, out); err != nil {
				return err
			}
		case filemode.Regular, filemode.Executable, filemode.Deprecated:
			content, err := r.blobAt(entry.Hash)
			if err != nil {
				return err
			}
			out[path] = content
		default:
			// Symlinks and submodules are never produced by writeTree, so there
			// is no content to invent for them here.
			continue
		}
	}
	return nil
}

// blobAt decodes one stored blob.
func (r *GitRepo) blobAt(h plumbing.Hash) (string, error) {
	obj, err := r.st.EncodedObject(plumbing.BlobObject, h)
	if err != nil {
		return "", fmt.Errorf("gitrepo: blob %s: %w", h, err)
	}
	rc, err := obj.Reader()
	if err != nil {
		return "", fmt.Errorf("gitrepo: blob %s: %w", h, err)
	}
	defer rc.Close() //nolint:errcheck
	content, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("gitrepo: blob %s: %w", h, err)
	}
	return string(content), nil
}

// isFullHash reports whether s is a full lowercase hex SHA-1.
func isFullHash(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, ch := range s {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

// shortHash abbreviates a hash for human-readable commit messages.
func shortHash(h plumbing.Hash) string {
	s := h.String()
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
