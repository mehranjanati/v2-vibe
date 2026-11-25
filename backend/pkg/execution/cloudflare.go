package execution

import (
	"bytes"
	"context"
	"encoding/json"
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
// Request shapes follow the published Workflows REST schema (create:
// `POST .../instances` with `instance_id` plus a JSON-encoded `params` string;
// lifecycle: `PATCH .../instances/{id}/status` with `{"status":...}`), which is
// also what wrangler 4.90.0 (cloudflare SDK 5.2.0) calls. Status: the mapping is
// verified against that schema and the stub server, but **not yet against a live
// account** — a real run needs a deployed workflow plus a token carrying
// `Workers Scripts Write`. See docs/DEV_CHECKLIST.md P7.11 and
// cloudflare_live_test.go (`-tags live`) for the one-command proof.
type Cloudflare struct {
	// AccountID and Workflow name the deployed workflow.
	AccountID string
	Workflow  string
	// APIToken is a Cloudflare API token with `Workers Scripts Write` (the
	// permission the instance endpoints document). It is never logged or put in
	// a URL.
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

// cfDuplicateInstanceCode is the documented "an instance with this id already
// exists" error (docs: error 10405, `workflows.api.error.instance.already_exists`).
const cfDuplicateInstanceCode = 10405

// isDuplicateInstance reports whether a failed create means "this run id already
// exists" (C12). The numeric code is the stable signal; the message is only a
// fallback because provider prose changes.
func isDuplicateInstance(envelope cfEnvelope) bool {
	for _, e := range envelope.Errors {
		if e.Code == cfDuplicateInstanceCode {
			return true
		}
		msg := strings.ToLower(e.Message)
		if strings.Contains(msg, "already_exists") || strings.Contains(msg, "already exists") {
			return true
		}
	}
	return false
}

// mapStatus translates a Cloudflare instance status onto the contract's states. An
// unknown status is a failure, never an optimistic success.
//
// The REST schema lists nine statuses. Three of them have no contract equivalent
// (`waiting` on an event, `waitingForPause` mid-pause, `rollingBack` mid-terminate);
// they are live, non-terminal instances, so they map to running rather than being
// reported as failures.
func mapStatus(status string) (State, *Error) {
	switch strings.ToLower(status) {
	case "queued":
		return StateQueued, nil
	case "running":
		return StateRunning, nil
	case "waiting", "waitingforpause", "rollingback":
		// Live but not yet in the state the caller asked for; the contract has no
		// waiting/rolling-back state and none of these is terminal.
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
	// The REST contract takes `instance_id` and a JSON-encoded `params` string; an
	// object is not accepted by the documented schema even though the SDK's
	// generated types are looser.
	params, err := json.Marshal(map[string]any{"plan": plan.ID, "tenant": plan.Tenant})
	if err != nil {
		return RunHandle{RunID: plan.ID, Tenant: plan.Tenant, PlanID: plan.ID}, Permanent("cloudflare: encode params: %v", err)
	}
	envelope, err := c.call(ctx, http.MethodPost, c.instanceURL(""), map[string]any{
		"instance_id": plan.ID,
		"params":      string(params),
	})
	if err != nil {
		// A duplicate id arrives as a permanent error; only that case falls back
		// to the existing instance (C12).
		if isDuplicateInstance(envelope) {
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

// setStatus performs the lifecycle edit the REST schema documents:
// `PATCH .../instances/{id}/status` with `{"status": "pause"|"resume"|"terminate"}`
// (also "restart", which this adapter does not expose).
func (c *Cloudflare) setStatus(ctx context.Context, instanceID, status string) error {
	if strings.TrimSpace(instanceID) == "" {
		return Permanent("cloudflare: run id is required")
	}
	_, err := c.call(ctx, http.MethodPatch, c.instanceURL(instanceID)+"/status", map[string]any{"status": status})
	return err
}

// Cancel implements Provider: terminate the instance.
//
// The REST status edit carries no reason field, so the reason stays with the
// caller's run record (and its audit trail); the provider is told only what to do.
func (c *Cloudflare) Cancel(ctx context.Context, runID, _ string) error {
	if !c.Capabilities().Cancel {
		return &Error{Class: ErrAdmissionRefused, Msg: "cloudflare: cancel is not advertised"}
	}
	return c.setStatus(ctx, runID, "terminate")
}

// Pause implements Provider.
func (c *Cloudflare) Pause(ctx context.Context, runID string) error {
	return c.setStatus(ctx, runID, "pause")
}

// Resume implements Provider.
func (c *Cloudflare) Resume(ctx context.Context, runID string) error {
	return c.setStatus(ctx, runID, "resume")
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
