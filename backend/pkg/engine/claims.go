package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// ---------- P1.9: path ownership (vfs_claim) ----------
//
// Ownership is a SIGNAL, not shared history: a claim says "this holder is
// writing this path right now", so two agents in one composed team cannot
// silently clobber each other. It is deliberately not a queueing lock — a
// denied claim tells the model to pick another path or ask, which is behaviour a
// coordinator can reason about.
//
// Claims are in-memory and per-room: they describe a live run, not durable
// state, so they never touch D1 or Redis. Every claim expires (default ten
// minutes) and a finished generation releases all of them, so an aborted run
// cannot leave a path claimed forever.
//
// The clock is injectable (`now`) because a timeout test against real time is a
// flake generator, and `pkg/engine` has no time abstraction anywhere else.

const (
	// claimTTLDefault is the orphan guard: a holder that never releases loses
	// its path after this window.
	claimTTLDefault = 10 * time.Minute
	// claimAuthorFallback labels the holder when a caller omits one. The claim
	// tools are only handed to the coder set, so the role comes from the tool
	// set itself — this string is a label, not an identity claim.
	claimAuthorFallback = writeAuthorCoder
)

// pathClaim is one live ownership record.
type pathClaim struct {
	holder string
	at     time.Time
}

// claimStore holds path → claim for one room.
type claimStore struct {
	mu     sync.Mutex
	now    func() time.Time
	ttl    time.Duration
	claims map[string]pathClaim
}

func newClaimStore() *claimStore {
	return &claimStore{
		now:    time.Now,
		ttl:    claimTTLDefault,
		claims: make(map[string]pathClaim),
	}
}

// claimView is the projection the status tool, the logs and the tests use.
type claimView struct {
	Path             string `json:"path"`
	Holder           string `json:"holder"`
	AgeSeconds       int    `json:"ageSeconds"`
	ExpiresInSeconds int    `json:"expiresInSeconds"`
}

// claim tries to take path for holder. A live claim by the same holder is
// refreshed; one by another holder is refused together with its owner, so the
// caller can name who owns the path instead of guessing.
func (c *claimStore) claim(path, holder string) (granted bool, owner string) {
	path, holder = strings.TrimSpace(path), strings.TrimSpace(holder)
	if path == "" {
		return false, ""
	}
	if holder == "" {
		holder = claimAuthorFallback
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.expireLocked(now)
	if existing, ok := c.claims[path]; ok && existing.holder != holder {
		return false, existing.holder
	}
	c.claims[path] = pathClaim{holder: holder, at: now}
	return true, holder
}

// release drops path when holder owns it. A non-holder cannot free another
// agent's path — that would defeat the point — so it returns false.
func (c *claimStore) release(path, holder string) bool {
	path, holder = strings.TrimSpace(path), strings.TrimSpace(holder)
	if path == "" {
		return false
	}
	if holder == "" {
		holder = claimAuthorFallback
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked(c.now())
	existing, ok := c.claims[path]
	if !ok || existing.holder != holder {
		return false
	}
	delete(c.claims, path)
	return true
}

// releaseAll drops every claim; a finished generation owns nothing.
func (c *claimStore) releaseAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.claims = make(map[string]pathClaim)
}

// view returns the live claims, oldest first (path as the tie-break).
func (c *claimStore) view() []claimView {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.expireLocked(now)
	out := make([]claimView, 0, len(c.claims))
	for path, claim := range c.claims {
		age := now.Sub(claim.at)
		out = append(out, claimView{
			Path:             path,
			Holder:           claim.holder,
			AgeSeconds:       int(age.Seconds()),
			ExpiresInSeconds: int((c.ttl - age).Seconds()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// expireLocked purges claims older than the TTL (callers hold mu).
func (c *claimStore) expireLocked(now time.Time) {
	for path, claim := range c.claims {
		if now.Sub(claim.at) >= c.ttl {
			delete(c.claims, path)
		}
	}
}

// claimWritePath makes every write honour ownership: a path held by another
// holder is refused (the error names the owner), a free path is claimed for the
// writer, and a path the writer already owns is refreshed. This is what makes
// the signal real even when a model never calls vfs_claim — delegated sub-agents
// in one team share one path space.
func (c *claimStore) claimWritePath(path, holder string) error {
	if granted, owner := c.claim(path, holder); !granted {
		return fmt.Errorf("%s is claimed by %q (release it or use another path)", path, owner)
	}
	return nil
}

// claimsOrNew returns the room's claim store, initialising it for any room that
// was not built by NewProjectRoom (defensive: the constructor always sets it).
func (r *ProjectRoom) claimsOrNew() *claimStore {
	if r.claims == nil {
		r.claims = newClaimStore()
	}
	return r.claims
}

// claimInput is the model-facing schema of vfs_claim / vfs_release.
type claimInput struct {
	Path   string `json:"path" jsonschema:"description=File path to claim or release"`
	Holder string `json:"holder" jsonschema:"description=Who is claiming; defaults to the coder role"`
}

// claimOutput reports the decision and, on a refusal, the current owner.
type claimOutput struct {
	Path             string `json:"path"`
	Holder           string `json:"holder"`
	Granted          bool   `json:"granted"`
	Owner            string `json:"owner,omitempty"`
	ExpiresInSeconds int    `json:"expiresInSeconds,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

// releaseOutput reports whether the caller actually owned the path.
type releaseOutput struct {
	Path     string `json:"path"`
	Released bool   `json:"released"`
	Reason   string `json:"reason,omitempty"`
}

// statusInput optionally filters the claim list to one path.
type statusInput struct {
	Path string `json:"path" jsonschema:"description=Optional path filter"`
}

// statusOutput is the read-only claim list.
type statusOutput struct {
	Claims []claimView `json:"claims"`
	Count  int         `json:"count"`
}

// claimTools are the explicit ownership tools (P1.9.1). They are handed to the
// coder set only, because the coder is the writer that can collide.
func (r *ProjectRoom) claimTools() ([]tool.BaseTool, error) {
	claim, err := utils.InferTool(
		"vfs_claim",
		"Claim a file path before writing it. Returns granted=false with the current owner when another agent holds the path.",
		func(_ context.Context, in claimInput) (*claimOutput, error) {
			path := strings.TrimSpace(in.Path)
			if path == "" {
				return nil, fmt.Errorf("vfs_claim: path is required")
			}
			granted, owner := r.claims.claim(path, in.Holder)
			out := &claimOutput{Path: path, Granted: granted}
			if granted {
				out.Holder = owner
				out.Owner = owner
				out.ExpiresInSeconds = int(r.claims.ttl.Seconds())
				out.Reason = "claimed"
			} else {
				out.Owner = owner
				out.Reason = fmt.Sprintf("held by %q until released or expired", owner)
			}
			return out, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("engine: vfs_claim tool: %w", err)
	}

	release, err := utils.InferTool(
		"vfs_release",
		"Release a path you claimed, so other agents may write it.",
		func(_ context.Context, in claimInput) (*releaseOutput, error) {
			path := strings.TrimSpace(in.Path)
			if path == "" {
				return nil, fmt.Errorf("vfs_release: path is required")
			}
			out := &releaseOutput{Path: path, Released: r.claims.release(path, in.Holder)}
			if !out.Released {
				out.Reason = "no live claim owned by this holder"
			}
			return out, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("engine: vfs_release tool: %w", err)
	}

	status, err := r.claimStatusTool()
	if err != nil {
		return nil, err
	}
	return []tool.BaseTool{claim, release, status}, nil
}

// claimStatusTool is vfs_status on its own: the reviewer gets this without any
// ability to take or drop a claim.
func (r *ProjectRoom) claimStatusTool() (tool.BaseTool, error) {
	status, err := utils.InferTool(
		"vfs_status",
		"List the file paths currently claimed by agents, with holder and remaining time. Read-only.",
		func(_ context.Context, in statusInput) (*statusOutput, error) {
			claims := r.claims.view()
			if filter := strings.TrimSpace(in.Path); filter != "" {
				filtered := make([]claimView, 0, 1)
				for _, c := range claims {
					if c.Path == filter {
						filtered = append(filtered, c)
					}
				}
				claims = filtered
			}
			return &statusOutput{Claims: claims, Count: len(claims)}, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("engine: vfs_status tool: %w", err)
	}
	return status, nil
}
