package trader

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"backend/pkg/cloudflare"
)

// ---------- test helpers (local copies: this package is standalone) ----------

// paramString reads a bind parameter as a string (missing/other types → "").
func paramString(params []any, i int) string {
	if i >= len(params) || params[i] == nil {
		return ""
	}
	if s, ok := params[i].(string); ok {
		return s
	}
	return ""
}

// paramInt reads a bind parameter as an *int (nil when absent/not numeric).
func paramInt(params []any, i int) *int {
	if i >= len(params) || params[i] == nil {
		return nil
	}
	switch v := params[i].(type) {
	case float64:
		n := int(v)
		return &n
	case int:
		n := v
		return &n
	case int64:
		n := int(v)
		return &n
	}
	return nil
}

// ---------- fake trade D1 (same envelope shape as production) ----------

type fakeTradeJournalRow struct {
	id     string
	tenant string
	agent  string
	runID  string
	event  string
	detail string
	prev   string
	hash   string
}

type fakeTradeStateRow struct {
	tenant string
	agent  string
	state  string
	runID  string
}

type fakeTradeD1 struct {
	mu      sync.Mutex
	journal []fakeTradeJournalRow
	states  map[string]fakeTradeStateRow // tenant\x00agent -> row
}

func newFakeTradeD1(t *testing.T) (*fakeTradeD1, *cloudflare.D1Client) {
	t.Helper()
	f := &fakeTradeD1{states: map[string]fakeTradeStateRow{}}
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

func (f *fakeTradeD1) apply(sql string, params []any) []map[string]any {
	switch {
	case strings.HasPrefix(sql, "INSERT INTO trade_journal"):
		// Chained insert: (id, tenant, agent, run, event, detail, prev, hash)
		f.journal = append(f.journal, fakeTradeJournalRow{
			id:     paramString(params, 0),
			tenant: paramString(params, 1),
			agent:  paramString(params, 2),
			runID:  paramString(params, 3),
			event:  paramString(params, 4),
			detail: paramString(params, 5),
			prev:   paramString(params, 6),
			hash:   paramString(params, 7),
		})
	case strings.HasPrefix(sql, "SELECT row_hash FROM trade_journal"):
		tenant, agent := paramString(params, 0), paramString(params, 1)
		for i := len(f.journal) - 1; i >= 0; i-- {
			if j := f.journal[i]; j.tenant == tenant && j.agent == agent {
				return []map[string]any{{"row_hash": j.hash}}
			}
		}
	case strings.HasPrefix(sql, "SELECT COUNT(*) AS n FROM trade_journal"):
		tenant, agent := paramString(params, 0), paramString(params, 1)
		n := 0
		for _, j := range f.journal {
			if j.tenant == tenant && j.agent == agent {
				n++
			}
		}
		return []map[string]any{{"n": float64(n)}}
	case strings.HasPrefix(sql, "SELECT id, run_id, event"):
		tenant, agent := paramString(params, 0), paramString(params, 1)
		var out []map[string]any
		for i := len(f.journal) - 1; i >= 0; i-- { // newest first, like rowid DESC
			j := f.journal[i]
			if j.tenant == tenant && j.agent == agent {
				out = append(out, map[string]any{
					"id": j.id, "run_id": j.runID, "event": j.event,
					"detail_json": j.detail, "prev_hash": j.prev, "row_hash": j.hash,
				})
			}
		}
		return out
	case strings.HasPrefix(sql, "INSERT INTO trade_state"):
		tenant, agent := paramString(params, 0), paramString(params, 1)
		f.states[tenant+"\x00"+agent] = fakeTradeStateRow{
			tenant: tenant, agent: agent,
			state: paramString(params, 2), runID: paramString(params, 3),
		}
	case strings.HasPrefix(sql, "SELECT state_json"):
		tenant, agent := paramString(params, 0), paramString(params, 1)
		if row, ok := f.states[tenant+"\x00"+agent]; ok {
			return []map[string]any{{"state_json": row.state, "updated_run_id": row.runID}}
		}
	case strings.HasPrefix(sql, "SELECT run_id, event"):
		tenant, agent := paramString(params, 0), paramString(params, 1)
		var out []map[string]any
		for i := len(f.journal) - 1; i >= 0; i-- {
			j := f.journal[i]
			if j.tenant == tenant && j.agent == agent {
				out = append(out, map[string]any{
					"run_id": j.runID, "event": j.event,
					"detail_json": j.detail, "created_at": "2026-01-01",
				})
			}
		}
		return out
	}
	return nil
}

// TestTradeMemoryRoundTrip: checkpoint then resume returns the same state.
func TestTradeMemoryRoundTrip(t *testing.T) {
	f, d1 := newFakeTradeD1(t)
	ctx := context.Background()

	st := TradeState{
		Equity:            10000,
		Positions:         []TradePosition{{Symbol: "BTCUSDT", Side: "buy", Quantity: 0.1, Entry: 97000, StopLoss: 94000, TakeProfit: 103000}},
		ConsecutiveLosses: 0,
		KillSwitch:        false,
	}
	SaveTradeState(ctx, d1, nil, "tenant-a", "spot-trader", "run-1", st)
	AppendTradeEvent(ctx, d1, "tenant-a", "spot-trader", "run-1", TradeEventFill, map[string]interface{}{"symbol": "BTCUSDT"})

	got, found := LoadTradeState(ctx, d1, nil, "tenant-a", "spot-trader")
	if !found {
		t.Fatal("LoadTradeState: expected state, got none")
	}
	if got.Equity != 10000 || len(got.Positions) != 1 || got.Positions[0].Symbol != "BTCUSDT" {
		t.Fatalf("LoadTradeState: wrong state: %+v", got)
	}
	if got.UpdatedRunID != "run-1" {
		t.Fatalf("LoadTradeState: run not stamped: %q", got.UpdatedRunID)
	}
	journal := LoadTradeJournal(ctx, d1, "tenant-a", "spot-trader", 10)
	if len(journal) != 1 || journal[0]["event"] != TradeEventFill {
		t.Fatalf("LoadTradeJournal: wrong events: %+v", journal)
	}
	if len(f.journal) != 1 {
		t.Fatalf("expected 1 journal row, have %d", len(f.journal))
	}
}

// TestTradeMemoryTenantIsolation: tenant B sees nothing of tenant A (P7.6 gate).
func TestTradeMemoryTenantIsolation(t *testing.T) {
	_, d1 := newFakeTradeD1(t)
	ctx := context.Background()

	SaveTradeState(ctx, d1, nil, "tenant-a", "spot-trader", "run-1", TradeState{Equity: 5000})
	AppendTradeEvent(ctx, d1, "tenant-a", "spot-trader", "run-1", TradeEventSignal, map[string]interface{}{"symbol": "ETHUSDT"})

	if _, found := LoadTradeState(ctx, d1, nil, "tenant-b", "spot-trader"); found {
		t.Fatal("cross-tenant state read must return not-found")
	}
	if rows := LoadTradeJournal(ctx, d1, "tenant-b", "spot-trader", 10); len(rows) != 0 {
		t.Fatalf("cross-tenant journal read must be empty, got %+v", rows)
	}
}

// TestTradeMemoryEmptyIdentityFailsClosed: empty tenant/agent never touches D1.
func TestTradeMemoryEmptyIdentityFailsClosed(t *testing.T) {
	f, d1 := newFakeTradeD1(t)
	ctx := context.Background()

	SaveTradeState(ctx, d1, nil, "", "spot-trader", "run-1", TradeState{Equity: 1})
	AppendTradeEvent(ctx, d1, "tenant-a", "", "run-1", TradeEventSignal, nil)
	if _, found := LoadTradeState(ctx, d1, nil, "", ""); found {
		t.Fatal("empty identity must not resolve state")
	}
	if len(f.journal) != 0 {
		t.Fatalf("empty identity must not append, have %d rows", len(f.journal))
	}
}
