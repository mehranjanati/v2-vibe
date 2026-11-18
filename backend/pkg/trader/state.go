package trader

import (
	"context"
	"encoding/json"
	"log"

	"github.com/redis/go-redis/v9"

	"backend/pkg/cloudflare"
)

// SaveTradeState upserts the snapshot to D1 and mirrors it to Redis (fast
// resume). Best-effort on both; D1 is the source of truth.
func SaveTradeState(ctx context.Context, d1 *cloudflare.D1Client, rdb *redis.Client, tenantID, agentName, runID string, state TradeState) {
	if tenantID == "" || agentName == "" {
		log.Printf("[trade] save state: tenant/agent required")
		return
	}
	state.UpdatedRunID = runID
	raw, err := json.Marshal(state)
	if err != nil {
		log.Printf("[trade] save state: marshal: %v", err)
		return
	}
	if d1 != nil {
		tctx, cancel := context.WithTimeout(ctx, tradeMemoryTimeout)
		_, derr := d1.Exec(tctx, tradeStateUpsertSQL, tenantID, agentName, string(raw), runID)
		cancel()
		if derr != nil {
			log.Printf("[trade] state D1 upsert failed: %v", derr)
		}
	}
	if rdb != nil {
		if err := rdb.HSet(ctx, tradeStateKey(tenantID, agentName), "state", string(raw), "run_id", runID).Err(); err != nil {
			log.Printf("[trade] state Redis mirror failed: %v", err)
		}
	}
}

// LoadTradeState resumes the snapshot: Redis mirror first, D1 fallback.
// Returns (zero, false) when nothing was ever saved — the caller starts flat,
// never from another tenant's state.
func LoadTradeState(ctx context.Context, d1 *cloudflare.D1Client, rdb *redis.Client, tenantID, agentName string) (TradeState, bool) {
	var zero TradeState
	if tenantID == "" || agentName == "" {
		return zero, false
	}
	if rdb != nil {
		if raw, err := rdb.HGet(ctx, tradeStateKey(tenantID, agentName), "state").Result(); err == nil && raw != "" {
			var st TradeState
			if json.Unmarshal([]byte(raw), &st) == nil {
				return st, true
			}
		}
	}
	if d1 == nil {
		return zero, false
	}
	tctx, cancel := context.WithTimeout(ctx, tradeMemoryTimeout)
	defer cancel()
	rows, err := d1.Query(tctx, tradeStateSelectSQL, tenantID, agentName)
	if err != nil || len(rows) == 0 {
		return zero, false
	}
	raw, _ := rows[0]["state_json"].(string)
	var st TradeState
	if json.Unmarshal([]byte(raw), &st) != nil {
		return zero, false
	}
	return st, true
}

// LoadTradeJournal reads the last N events for one tenant+agent (newest
// first). Cross-tenant reads are impossible by construction: the WHERE clause
// pins both columns to the caller's identity.
func LoadTradeJournal(ctx context.Context, d1 *cloudflare.D1Client, tenantID, agentName string, limit int) []map[string]interface{} {
	if d1 == nil || tenantID == "" || agentName == "" {
		return nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	tctx, cancel := context.WithTimeout(ctx, tradeMemoryTimeout)
	defer cancel()
	rows, err := d1.Query(tctx, tradeJournalSelectSQL, tenantID, agentName, limit)
	if err != nil {
		log.Printf("[trade] journal load failed: %v", err)
		return nil
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]interface{}{
			"run_id":     r["run_id"],
			"event":      r["event"],
			"detail":     r["detail_json"],
			"created_at": r["created_at"],
		})
	}
	return out
}
