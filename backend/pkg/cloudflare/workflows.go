package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ---------- Cloudflare Workflows REST API (trigger + status) ----------
//
// The control plane starts DAG executions by creating Workflow instances on
// the account's Cloudflare Workflows (wrangler.v2.jsonc → the VibeWorkflow
// class). Endpoints used:
//
//	POST /accounts/{account_id}/workflows/{name}/instances
//	     body: { "instance_id": "...", "params": { ...input... } }
//	     → result: { "id", "status", "version_id" }
//	GET  /accounts/{account_id}/workflows/{name}/instances/{instance_id}
//	     → result: { "id", "status", "started_on", "ended_on", "output", ... }
//
// Every response uses the standard v4 envelope { success, errors, result }.

// workflowAPIEnvelope is the standard Cloudflare v4 response envelope.
type workflowAPIEnvelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result map[string]interface{} `json:"result"`
}

// workflowInstancesURL builds the instances collection URL for one workflow.
func workflowInstancesURL(baseURL, accountID, workflowName string) string {
	return fmt.Sprintf("%s/accounts/%s/workflows/%s/instances", baseURL, accountID, workflowName)
}

// requireWorkflowCreds guards the Workflows calls.
func (c *Client) requireWorkflowCreds(workflowName string) error {
	if c.accountID == "" || c.apiToken == "" {
		return fmt.Errorf("cloudflare: accountID and apiToken are required")
	}
	if workflowName == "" {
		return fmt.Errorf("cloudflare: workflowName is required")
	}
	return nil
}

// CreateWorkflowInstance triggers a new execution of the named workflow.
// instanceID is the user-provided instance id (must be unique within the
// workflow — the control plane passes the workflow_instances D1 row id so
// the runtime worker can find its row); params is the event payload handed
// to the DAG's trigger node.
func (c *Client) CreateWorkflowInstance(ctx context.Context, workflowName, instanceID string, params map[string]interface{}) (map[string]interface{}, error) {
	if err := c.requireWorkflowCreds(workflowName); err != nil {
		return nil, err
	}
	if instanceID == "" {
		return nil, fmt.Errorf("cloudflare: instanceID is required")
	}
	p := params
	if p == nil {
		p = map[string]interface{}{}
	}
	body, err := json.Marshal(map[string]interface{}{
		"instance_id": instanceID,
		"params":      p,
	})
	if err != nil {
		return nil, fmt.Errorf("cloudflare: marshal workflow create: %w", err)
	}
	url := workflowInstancesURL(c.apiBaseURL(), c.accountID, workflowName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cloudflare: build workflow create: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: workflow create: %w", err)
	}
	defer resp.Body.Close()
	raw, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return nil, fmt.Errorf("cloudflare: read workflow create response: %w", rerr)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("cloudflare: workflow create failed (status %d): %s", resp.StatusCode, string(raw))
	}
	var env workflowAPIEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("cloudflare: parse workflow create response: %w", err)
	}
	if !env.Success {
		msg := "unknown error"
		if len(env.Errors) > 0 {
			msg = env.Errors[0].Message
		}
		return nil, fmt.Errorf("cloudflare: workflow create rejected: %s", msg)
	}
	return env.Result, nil
}

// GetWorkflowInstance returns the live status of one workflow instance
// (status, output, error, per-step summary from the Cloudflare runtime).
func (c *Client) GetWorkflowInstance(ctx context.Context, workflowName, instanceID string) (map[string]interface{}, error) {
	if err := c.requireWorkflowCreds(workflowName); err != nil {
		return nil, err
	}
	if instanceID == "" {
		return nil, fmt.Errorf("cloudflare: instanceID is required")
	}
	url := workflowInstancesURL(c.apiBaseURL(), c.accountID, workflowName) + "/" + instanceID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: build workflow get: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: workflow get: %w", err)
	}
	defer resp.Body.Close()
	raw, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return nil, fmt.Errorf("cloudflare: read workflow get response: %w", rerr)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cloudflare: workflow get failed (status %d): %s", resp.StatusCode, string(raw))
	}
	var env workflowAPIEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("cloudflare: parse workflow get response: %w", err)
	}
	if !env.Success {
		msg := "unknown error"
		if len(env.Errors) > 0 {
			msg = env.Errors[0].Message
		}
		return nil, fmt.Errorf("cloudflare: workflow get rejected: %s", msg)
	}
	return env.Result, nil
}

// workflowInstanceAction posts one control operation (terminate/pause/resume)
// against an instance. It is the shared body of the three cancellable
// operations so their request shape, auth header and error text cannot drift.
func (c *Client) workflowInstanceAction(ctx context.Context, workflowName, instanceID, action string, body map[string]interface{}) error {
	if err := c.requireWorkflowCreds(workflowName); err != nil {
		return err
	}
	if strings.TrimSpace(instanceID) == "" {
		return fmt.Errorf("cloudflare: instanceID is required")
	}
	endpoint := workflowInstancesURL(c.apiBaseURL(), c.accountID, workflowName) + "/" + url.PathEscape(instanceID) + "/" + action
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("cloudflare: encode workflow %s: %w", action, err)
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return fmt.Errorf("cloudflare: build workflow %s: %w", action, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare: workflow %s: %w", action, err)
	}
	defer resp.Body.Close()
	raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if rerr != nil {
		return fmt.Errorf("cloudflare: read workflow %s response: %w", action, rerr)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("cloudflare: workflow %s failed (status %d): %s", action, resp.StatusCode, string(raw))
	}
	var env workflowAPIEnvelope
	if err := json.Unmarshal(raw, &env); err == nil && !env.Success {
		msg := "unknown error"
		if len(env.Errors) > 0 {
			msg = env.Errors[0].Message
		}
		return fmt.Errorf("cloudflare: workflow %s rejected: %s", action, msg)
	}
	return nil
}

// TerminateWorkflowInstance cancels a run: the runtime stops at its next safe
// boundary and the instance reports `terminated` (P7-RUN-001c).
func (c *Client) TerminateWorkflowInstance(ctx context.Context, workflowName, instanceID, reason string) error {
	body := map[string]interface{}{}
	if strings.TrimSpace(reason) != "" {
		body["reason"] = strings.TrimSpace(reason)
	}
	return c.workflowInstanceAction(ctx, workflowName, instanceID, "terminate", body)
}

// PauseWorkflowInstance holds a run at its next step boundary.
func (c *Client) PauseWorkflowInstance(ctx context.Context, workflowName, instanceID string) error {
	return c.workflowInstanceAction(ctx, workflowName, instanceID, "pause", nil)
}

// ResumeWorkflowInstance continues a paused run without re-executing completed
// steps (the runtime owns that guarantee).
func (c *Client) ResumeWorkflowInstance(ctx context.Context, workflowName, instanceID string) error {
	return c.workflowInstanceAction(ctx, workflowName, instanceID, "resume", nil)
}

// TrimWorkflowName normalizes a Workflows name (defensive against env
// whitespace); empty input stays empty.
func TrimWorkflowName(name string) string {
	return strings.TrimSpace(name)
}
