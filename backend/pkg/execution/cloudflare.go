package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Cloudflare is the adapter for Cloudflare Workflows over its REST API.
//
// It is deliberately thin: it translates the nine contract operations onto the
// documented instance endpoints and maps every failure onto the taxonomy. It does
// not execute steps — the workflow definition inside the Worker does that — so
// `Start` accepts a StepExecutor and ignores it, and `Sleep` is refused because a
// sleep is declared inside the workflow, not injected from outside.
//
// Status: implemented, **not verified against a live account**. The conformance
// suite needs a deployed workflow plus an account, so this adapter is exercised
// against a stub server for its mapping only. See docs/DEV_CHECKLIST.md P7.11.
type Cloudflare struct {
	// AccountID and Workflow name the deployed workflow.
	AccountID string
	Workflow  string
	// APIToken is a Cloudflare API token with Workflows edit permission. It is
	// never logged or put in a URL.
	APIToken string
	// BaseURL defaults to the public API.
	BaseURL string
	// HTTP defaults to a client with a 30s timeout.
	HTTP *http.Client
}

// CloudflareCapabilities is the advertisement this adapter can honestly keep:
// terminate/pause/resume exist; long sleeps are declared inside the workflow and
// step events live in D1 step logs, not in this API.
var CloudflareCapabilities = Capabilities{Cancel: true, Pause: true, LongSleep: false, History: false}

const defaultCFBaseURL = "https://api.cloudflare.com/client/v4"

// NewCloudflare validates the wiring. An empty token is refused: an adapter that
// silently calls the API unauthenticated would fail on every run.
func NewCloudflare(accountID, workflow, apiToken string) (*Cloudflare, error) {
	if strings.TrimSpace(accountID) == "" {
		return nil, Permanent("cloudflare: account id is required")
	}
	if strings.TrimSpace(workflow) == "" {
		return nil, Permanent("cloudflare: workflow name is required")
	}
	if strings.TrimSpace(apiToken) == "" {
		return nil, Permanent("cloudflare: api token is required")
	}
	return &Cloudflare{
		AccountID: accountID, Workflow: workflow, APIToken: apiToken,
		BaseURL: defaultCFBaseURL, HTTP: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Capabilities implements Provider.
func (c *Cloudflare) Capabilities() Capabilities { return CloudflareCapabilities }

// cfEnvelope is the API's standard envelope.
type cfEnvelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result json.RawMessage `json:"result"`
}

// cfInstance is the subset of an instance the contract needs.
type cfInstance struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// instanceURL builds one endpoint path. The instance id is path-escaped: a run id
// comes from a caller and must never be able to change the request path.
func (c *Cloudflare) instanceURL(instanceID string) string {
	base := fmt.Sprintf("%s/accounts/%s/workflows/%s/instances",
		strings.TrimRight(c.BaseURL, "/"), url.PathEscape(c.AccountID), url.PathEscape(c.Workflow))
	if instanceID == "" {
		return base
	}
	return base + "/" + url.PathEscape(instanceID)
}

// call performs one API request and decodes the envelope.
func (c *Cloudflare) call(ctx context.Context, method, endpoint string, body any) (cfEnvelope, error) {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return cfEnvelope{}, Permanent("cloudflare: encode request: %v", err)
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return cfEnvelope{}, Permanent("cloudflare: build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// A transport failure is worth retrying: the run may not have started.
		return cfEnvelope{}, Transient("cloudflare: %s %s: %v", method, endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return cfEnvelope{}, Transient("cloudflare: read response: %v", err)
	}
	var envelope cfEnvelope
	_ = json.Unmarshal(raw, &envelope)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return envelope, classifyCloudflareError(resp.StatusCode, envelope)
	}
	return envelope, nil
}

// classifyCloudflareError maps an HTTP status onto the fixed taxonomy.
func classifyCloudflareError(status int, envelope cfEnvelope) error {
	msg := fmt.Sprintf("http %d", status)
	if len(envelope.Errors) > 0 {
		msg = envelope.Errors[0].Message
	}
	switch {
	case status == http.StatusForbidden, status == http.StatusUnauthorized:
		return &Error{Class: ErrPolicyDenied, Msg: "cloudflare: " + msg}
	case status == http.StatusTooManyRequests, status >= 500:
		return &Error{Class: ErrTransient, Msg: "cloudflare: " + msg, RetryAfter: 5 * time.Second}
	default:
		return &Error{Class: ErrPermanent, Msg: "cloudflare: " + msg}
	}
}

// mapStatus translates a Cloudflare instance status onto the contract's states. An
// unknown status is a failure, never an optimistic success.
func mapStatus(status string) (State, *Error) {
	switch strings.ToLower(status) {
	case "queued":
		return StateQueued, nil
	case "running":
		return StateRunning, nil
	case "paused":
		return StatePaused, nil
	case "complete", "completed", "succeeded":
		return StateSucceeded, nil
	case "errored", "failed":
		return StateFailed, &Error{Class: ErrPermanent, Msg: "cloudflare: instance errored"}
	case "terminated", "cancelled", "canceled":
		return StateCancelled, &Error{Class: ErrCancelled, Msg: "cloudflare: instance terminated"}
	default:
		return StateFailed, &Error{Class: ErrPermanent, Msg: fmt.Sprintf("cloudflare: unknown instance status %q", status)}
	}
}

// Start implements Provider. Admission happens locally first: a plan that must not
// run never reaches the API.
func (c *Cloudflare) Start(ctx context.Context, plan RunPlan, _ StepExecutor) (RunHandle, error) {
	if err := Admit(plan, c.Capabilities()); err != nil {
		return RunHandle{RunID: plan.ID, Tenant: plan.Tenant, PlanID: plan.ID, State: StateRejected}, nil
	}
	envelope, err := c.call(ctx, http.MethodPost, c.instanceURL(""), map[string]any{
		"id":     plan.ID,
		"params": map[string]any{"plan": plan.ID, "tenant": plan.Tenant},
	})
	if err != nil {
		// 409 "already exists" arrives as a permanent error; treat it as the
		// idempotent case by reading the existing instance instead.
		var pe *Error
		if errors.As(err, &pe) && strings.Contains(strings.ToLower(pe.Msg), "already exists") {
			return c.existingHandle(ctx, plan)
		}
		return RunHandle{RunID: plan.ID, Tenant: plan.Tenant, PlanID: plan.ID}, err
	}
	instance := cfInstance{ID: plan.ID, Status: "queued"}
	if len(envelope.Result) > 0 {
		_ = json.Unmarshal(envelope.Result, &instance)
	}
	if instance.ID == "" {
		instance.ID = plan.ID
	}
	state, _ := mapStatus(instance.Status)
	return RunHandle{RunID: instance.ID, Tenant: plan.Tenant, PlanID: plan.ID, State: state}, nil
}

// existingHandle returns the handle for an instance that already exists (C12).
func (c *Cloudflare) existingHandle(ctx context.Context, plan RunPlan) (RunHandle, error) {
	status, err := c.Status(ctx, plan.ID)
	if err != nil {
		return RunHandle{RunID: plan.ID, Tenant: plan.Tenant, PlanID: plan.ID}, err
	}
	return RunHandle{RunID: status.RunID, Tenant: status.Tenant, PlanID: status.PlanID, State: status.State}, nil
}

// Status implements Provider.
func (c *Cloudflare) Status(ctx context.Context, runID string) (Status, error) {
	envelope, err := c.call(ctx, http.MethodGet, c.instanceURL(runID), nil)
	if err != nil {
		return Status{}, err
	}
	var instance cfInstance
	if err := json.Unmarshal(envelope.Result, &instance); err != nil {
		return Status{}, Permanent("cloudflare: decode instance %q: %v", runID, err)
	}
	state, stateErr := mapStatus(instance.Status)
	status := Status{RunID: runID, PlanID: runID, State: state, Error: stateErr}
	if instance.Error != nil && instance.Error.Message != "" {
		status.Error = &Error{Class: ErrPermanent, Msg: "cloudflare: " + instance.Error.Message}
	}
	if t, err := time.Parse(time.RFC3339, instance.Start); err == nil {
		status.StartedAt = t
	}
	if t, err := time.Parse(time.RFC3339, instance.End); err == nil {
		status.FinishedAt = t
	}
	// The tenant is not part of the instance payload; the caller's run record
	// carries it. The adapter never invents one.
	return status, nil
}

// Cancel implements Provider: terminate the instance.
func (c *Cloudflare) Cancel(ctx context.Context, runID, reason string) error {
	if !c.Capabilities().Cancel {
		return &Error{Class: ErrAdmissionRefused, Msg: "cloudflare: cancel is not advertised"}
	}
	if strings.TrimSpace(runID) == "" {
		return Permanent("cloudflare: run id is required")
	}
	_, err := c.call(ctx, http.MethodPost, c.instanceURL(runID)+"/terminate", map[string]any{"reason": reason})
	return err
}

// Pause implements Provider.
func (c *Cloudflare) Pause(ctx context.Context, runID string) error {
	_, err := c.call(ctx, http.MethodPost, c.instanceURL(runID)+"/pause", nil)
	return err
}

// Resume implements Provider.
func (c *Cloudflare) Resume(ctx context.Context, runID string) error {
	_, err := c.call(ctx, http.MethodPost, c.instanceURL(runID)+"/resume", nil)
	return err
}

// Sleep implements Provider: refused, because a Cloudflare sleep is declared
// inside the workflow definition and cannot be injected from outside.
func (c *Cloudflare) Sleep(_ context.Context, _ RunHandle, _ time.Duration) error {
	return &Error{Class: ErrAdmissionRefused, Msg: "cloudflare: sleeps are declared inside the workflow definition; the provider cannot inject one"}
}

// History implements Provider: refused here, because step events live in the
// worker's D1 step logs, not in this API.
func (c *Cloudflare) History(_ context.Context, _ string) ([]StepEvent, error) {
	return nil, &Error{Class: ErrAdmissionRefused, Msg: "cloudflare: step history is read from D1 step logs, not the instances API"}
}
