package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// D1Client talks to a Cloudflare D1 database via the REST API v4.
// It reuses the same API token / account credentials as the Pages client.
type D1Client struct {
	accountID  string
	apiToken   string
	databaseID string
	hc         *http.Client
	// baseURL overrides the API root (defaults to
	// https://api.cloudflare.com/client/v4). Set by tests.
	baseURL string
}

// NewD1Client creates a D1 client for the given database.
func NewD1Client(accountID, apiToken, databaseID string) *D1Client {
	return &D1Client{
		accountID:  accountID,
		apiToken:   apiToken,
		databaseID: databaseID,
		hc:         &http.Client{Timeout: 60 * time.Second},
	}
}

// SetBaseURL overrides the Cloudflare API root (defaults to
// https://api.cloudflare.com/client/v4). Used by tests to point the client
// at an httptest server emulating the D1 REST API.
func (d *D1Client) SetBaseURL(url string) {
	d.baseURL = strings.TrimRight(url, "/")
}

type d1Request struct {
	SQL    string        `json:"sql"`
	Params []interface{} `json:"params,omitempty"`
}

type d1Envelope struct {
	Success bool `json:"success"`
	Result  []struct {
		Results []map[string]interface{} `json:"results"`
		Meta    map[string]interface{}   `json:"meta"`
	} `json:"result"`
}

func (d *D1Client) requireCreds() error {
	if d.accountID == "" || d.apiToken == "" || d.databaseID == "" {
		return fmt.Errorf("d1: accountID, apiToken and databaseID are required")
	}
	return nil
}

// query runs a single statement and returns the first result set.
func (d *D1Client) query(ctx context.Context, sql string, args ...interface{}) ([]map[string]interface{}, error) {
	if err := d.requireCreds(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(d1Request{SQL: sql, Params: args})
	if err != nil {
		return nil, fmt.Errorf("d1: marshal request: %w", err)
	}
	base := d.baseURL
	if base == "" {
		base = "https://api.cloudflare.com/client/v4"
	}
	url := fmt.Sprintf("%s/accounts/%s/d1/database/%s/query", base, d.accountID, d.databaseID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("d1: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+d.apiToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("d1: http: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("d1: read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("d1: status %d: %s", resp.StatusCode, string(raw))
	}
	var env d1Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("d1: decode envelope: %w", err)
	}
	if !env.Success {
		return nil, fmt.Errorf("d1: upstream error: %s", string(raw))
	}
	if len(env.Result) == 0 {
		return nil, nil
	}
	return env.Result[0].Results, nil
}

// Query runs a SELECT and returns the resulting rows.
func (d *D1Client) Query(ctx context.Context, sql string, args ...interface{}) ([]map[string]interface{}, error) {
	return d.query(ctx, sql, args...)
}

// Exec runs an INSERT/UPDATE/DELETE; returns true on success.
func (d *D1Client) Exec(ctx context.Context, sql string, args ...interface{}) (bool, error) {
	_, err := d.query(ctx, sql, args...)
	if err != nil {
		return false, err
	}
	return true, nil
}

// UpsertWorkflowDag upserts a validated workflow DAG (schema v2) into the
// workflow_dags table (D1). The table is created by the worker migration
// (worker/database/schema.ts → migrations/); a missing table is reported as
// an error the caller logs and continues past (never fatal to generation).
func (d *D1Client) UpsertWorkflowDag(ctx context.Context, workflowID string, schemaVersion int, dagJSON string) error {
	const sql = "INSERT INTO workflow_dags (workflow_id, schema_version, dag_json, status, updated_at) " +
		"VALUES (?, ?, ?, 'generated', CURRENT_TIMESTAMP) " +
		"ON CONFLICT(workflow_id) DO UPDATE SET schema_version = excluded.schema_version, " +
		"dag_json = excluded.dag_json, status = 'generated', updated_at = CURRENT_TIMESTAMP"
	_, err := d.Exec(ctx, sql, workflowID, schemaVersion, dagJSON)
	if err != nil {
		return fmt.Errorf("d1: upsert workflow_dags: %w", err)
	}
	return nil
}
