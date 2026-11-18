package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"backend/pkg/cloudflare"
	"backend/pkg/trader"
)

// ---------- Trade journal anchoring (Phase A2) ----------
//
// The trader package owns the hash chain; the control plane owns durable,
// append-only storage. This adapter is the seam: it implements
// trader.Anchor over the internal per-app Git repo (D1-CAS), so a chain head is
// sealed outside the mutable `trade_journal` table.
//
// Why Git and not a second table: commit objects are content-addressed and the
// existing repo never rewrites history (Revert appends a commit with the
// target's tree). A table rewrite can be repeated; a commit chain cannot be
// reproduced without the same content.
//
// Repository naming: one repo per (tenant, agent), sanitized so the app id is
// safe as a D1 key and a ref namespace.
var anchorAppIDSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)

// gitTradeAnchor writes one anchor commit per call.
type gitTradeAnchor struct {
	d1 *cloudflare.D1Client
}

// AnchorAppID returns the repo id that holds one (tenant, agent) journal chain.
func AnchorAppID(tenantID, agentName string) string {
	clean := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		s = anchorAppIDSanitizer.ReplaceAllString(s, "-")
		return strings.Trim(s, "-")
	}
	return "trade-" + clean(tenantID) + "-" + clean(agentName)
}

// AnchorTradeJournal commits the chain head. The payload is small by design
// (CF_LIMITS §8: one row per object, content as a bind parameter).
func (a gitTradeAnchor) AnchorTradeJournal(ctx context.Context, tenantID, agentName, headHash string, rows int) (string, error) {
	if a.d1 == nil {
		return "", nil
	}
	if headHash == "" {
		return "", fmt.Errorf("trade anchor: empty head hash")
	}
	appID := AnchorAppID(tenantID, agentName)
	repo, err := OpenGitRepo(ctx, a.d1, appID)
	if err != nil {
		return "", fmt.Errorf("trade anchor: open %s: %w", appID, err)
	}
	payload, err := json.Marshal(map[string]interface{}{
		"tenant":       tenantID,
		"agent":        agentName,
		"head_hash":    headHash,
		"rows":         rows,
		"anchored_at":  time.Now().UTC().Format(time.RFC3339),
		"chain_format": "sha256-v1",
	})
	if err != nil {
		return "", err
	}
	message := fmt.Sprintf("anchor %s rows=%d head=%s", agentName, rows, anchorHeadShort(headHash))
	sha, err := repo.Commit(ctx, GitCommitRequest{
		Message: message,
		Author:  "system",
		Files:   map[string]string{"journal.anchor.json": string(payload)},
	})
	if err != nil {
		return "", fmt.Errorf("trade anchor: commit %s: %w", appID, err)
	}
	return sha, nil
}

// anchorHeadShort keeps commit messages readable without hiding the identity.
func anchorHeadShort(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}

// tradeAnchorOrNil returns the adapter only when D1 is configured, so the dev
// path (no D1) never pretends to anchor.
func tradeAnchorOrNil(d1 *cloudflare.D1Client) trader.Anchor {
	if d1 == nil {
		return nil
	}
	return gitTradeAnchor{d1: d1}
}

var _ = log.Printf
