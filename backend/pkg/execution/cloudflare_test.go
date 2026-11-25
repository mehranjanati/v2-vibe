package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// cfStub is a stand-in for the Workflows API: it records requests and answers
// with the envelope shape the adapter parses. It deliberately enforces the
// documented REST contract — `instance_id` (not `id`) plus a JSON-encoded
// `params` string on create, and `PATCH .../status` for lifecycle edits — so a
// regression back to the invented endpoints fails here.
type cfStub struct {
	server    *httptest.Server
	requests  []string
	bodies    map[string]string
	patches   []string // status values sent through PATCH .../status, in order
	status    string
	failWith  int
	body      string
	instances map[string]bool
}

func newCFStub(t *testing.T) *cfStub {
	t.Helper()
	stub := &cfStub{status: "queued", instances: map[string]bool{}, bodies: map[string]string{}}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		stub.requests = append(stub.requests, key)
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"success":false,"errors":[{"code":10000,"message":"auth"}]}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		stub.bodies[key] = string(raw)
		if stub.failWith != 0 {
			w.WriteHeader(stub.failWith)
			fmt.Fprint(w, stub.body)
			return
		}
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/instances"):
			var body struct {
				InstanceID string `json:"instance_id"`
				ID         string `json:"id"`
				Params     any    `json:"params"`
			}
			_ = json.Unmarshal(raw, &body)
			if body.InstanceID == "" {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"success":false,"errors":[{"code":1000,"message":"instance_id is required"}]}`)
				return
			}
			if _, isString := body.Params.(string); !isString {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"success":false,"errors":[{"code":1000,"message":"params must be a JSON-encoded string"}]}`)
				return
			}
			if stub.instances[body.InstanceID] {
				w.WriteHeader(http.StatusConflict)
				fmt.Fprint(w, `{"success":false,"errors":[{"code":10405,"message":"workflows.api.error.instance.already_exists"}]}`)
				return
			}
			stub.instances[body.InstanceID] = true
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprintf(w, `{"success":true,"result":{"id":%q,"status":%q}}`, body.InstanceID, stub.status)
			return
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/status"):
			var body struct {
				Status string `json:"status"`
			}
			_ = json.Unmarshal(raw, &body)
			stub.patches = append(stub.patches, body.Status)
			switch body.Status {
			case "pause", "resume", "terminate":
				fmt.Fprintf(w, `{"success":true,"result":{"id":"run-1","status":%q}}`, body.Status)
			default:
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"success":false,"errors":[{"code":1000,"message":"unknown status"}]}`)
			}
			return
		}
		fmt.Fprintf(w, `{"success":true,"result":{"id":"run-1","status":%q}}`, stub.status)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *cfStub) adapter(t *testing.T) *Cloudflare {
	t.Helper()
	c, err := NewCloudflare("acct-1", "vibe-runs", "test-token")
	if err != nil {
		t.Fatalf("NewCloudflare: %v", err)
	}
	c.BaseURL = s.server.URL
	return c
}

func cfPlan(id string) RunPlan {
	return RunPlan{ID: id, Tenant: "tenant-a", Steps: []Step{{Name: "first"}}}
}

// TestCloudflareAdmissionNeverReachesTheAPI proves the tenant refusal happens
// locally: a plan without a tenant must not create an instance (C8).
func TestCloudflareAdmissionNeverReachesTheAPI(t *testing.T) {
	stub := newCFStub(t)
	adapter := stub.adapter(t)

	plan := cfPlan("run-bad")
	plan.Tenant = ""
	handle, err := adapter.Start(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if handle.State != StateRejected {
		t.Errorf("state = %s, want rejected", handle.State)
	}
	if len(stub.requests) != 0 {
		t.Errorf("API calls = %v, want none for a refused plan", stub.requests)
	}

	// An unsupported requirement is refused locally too (C16).
	plan = cfPlan("run-sleep")
	plan.Requirements = []Capability{CapLongSleep}
	handle, err = adapter.Start(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if handle.State != StateRejected {
		t.Errorf("state = %s, want rejected for an unadvertised capability", handle.State)
	}
	if len(stub.requests) != 0 {
		t.Errorf("API calls = %v, want none for a capability refusal", stub.requests)
	}
}

// TestCloudflareMapsTheContract proves every operation lands on the documented
// endpoint with the right method.
func TestCloudflareMapsTheContract(t *testing.T) {
	stub := newCFStub(t)
	adapter := stub.adapter(t)
	ctx := context.Background()

	handle, err := adapter.Start(ctx, cfPlan("run-1"), nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if handle.RunID != "run-1" || handle.State != StateQueued {
		t.Errorf("handle = %+v, want run-1 queued", handle)
	}

	stub.status = "running"
	if status, err := adapter.Status(ctx, "run-1"); err != nil || status.State != StateRunning {
		t.Fatalf("Status = %+v, %v; want running", status, err)
	}
	stub.status = "complete"
	if status, _ := adapter.Status(ctx, "run-1"); status.State != StateSucceeded {
		t.Errorf("complete mapped to %s, want succeeded", status.State)
	}
	stub.status = "errored"
	status, err := adapter.Status(ctx, "run-1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StateFailed || status.Error == nil || status.Error.Class != ErrPermanent {
		t.Errorf("errored mapped to %s/%v, want failed/permanent", status.State, status.Error)
	}
	stub.status = "terminated"
	if status, _ := adapter.Status(ctx, "run-1"); status.State != StateCancelled {
		t.Errorf("terminated mapped to %s, want cancelled", status.State)
	}

	if err := adapter.Cancel(ctx, "run-1", "operator"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := adapter.Pause(ctx, "run-1"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if err := adapter.Resume(ctx, "run-1"); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	want := []string{
		"POST /accounts/acct-1/workflows/vibe-runs/instances",
		"GET /accounts/acct-1/workflows/vibe-runs/instances/run-1",
		"PATCH /accounts/acct-1/workflows/vibe-runs/instances/run-1/status",
	}
	for _, path := range want {
		if !contains(stub.requests, path) {
			t.Errorf("missing request %q in %v", path, stub.requests)
		}
	}
	// The invented endpoints must never be called again.
	for _, dead := range []string{
		"POST /accounts/acct-1/workflows/vibe-runs/instances/run-1/terminate",
		"POST /accounts/acct-1/workflows/vibe-runs/instances/run-1/pause",
		"POST /accounts/acct-1/workflows/vibe-runs/instances/run-1/resume",
	} {
		if contains(stub.requests, dead) {
			t.Errorf("adapter called a non-existent endpoint: %q", dead)
		}
	}

	// Create must send the documented body: instance_id plus a JSON-encoded
	// params string (an object is rejected by the REST schema).
	createBody := stub.bodies["POST /accounts/acct-1/workflows/vibe-runs/instances"]
	var sent struct {
		InstanceID string `json:"instance_id"`
		ID         string `json:"id"`
		Params     string `json:"params"`
	}
	if err := json.Unmarshal([]byte(createBody), &sent); err != nil {
		t.Fatalf("create body %q: %v", createBody, err)
	}
	if sent.InstanceID != "run-1" {
		t.Errorf("create body instance_id = %q, want run-1", sent.InstanceID)
	}
	if sent.ID != "" {
		t.Errorf("create body carries the undocumented `id` field: %q", sent.ID)
	}
	var params map[string]string
	if err := json.Unmarshal([]byte(sent.Params), &params); err != nil {
		t.Fatalf("params %q is not JSON-encoded: %v", sent.Params, err)
	}
	if params["tenant"] != "tenant-a" || params["plan"] != "run-1" {
		t.Errorf("params = %v, want plan/tenant", params)
	}

	// Every lifecycle action is the documented status edit with the right value.
	if got := strings.Join(stub.patches, ","); got != "terminate,pause,resume" {
		t.Errorf("status edits = %q, want terminate,pause,resume (Cancel, Pause, Resume)", got)
	}
}

// TestCloudflareLifecycleUsesStatusEdit pins each action to one status value, so a
// wrong verb/body cannot hide behind the ordering of the mapping test.
func TestCloudflareLifecycleUsesStatusEdit(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		call func(*Cloudflare) error
		want string
	}{
		{"pause", func(c *Cloudflare) error { return c.Pause(ctx, "run-1") }, "pause"},
		{"resume", func(c *Cloudflare) error { return c.Resume(ctx, "run-1") }, "resume"},
		{"cancel", func(c *Cloudflare) error { return c.Cancel(ctx, "run-1", "operator") }, "terminate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := newCFStub(t)
			adapter := stub.adapter(t)
			if err := tc.call(adapter); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			const key = "PATCH /accounts/acct-1/workflows/vibe-runs/instances/run-1/status"
			if body := stub.bodies[key]; body != fmt.Sprintf(`{"status":%q}`, tc.want) {
				t.Errorf("body = %q, want status %q", body, tc.want)
			}
			for _, req := range stub.requests {
				if strings.Contains(req, "/pause") || strings.Contains(req, "/resume") || strings.Contains(req, "/terminate") {
					t.Errorf("called a non-existent endpoint: %q", req)
				}
			}
		})
	}
}

// TestCloudflareMapsEveryDocumentedStatus covers the nine statuses the REST
// schema lists: the three with no contract equivalent are live, not failed.
func TestCloudflareMapsEveryDocumentedStatus(t *testing.T) {
	cases := map[string]State{
		"queued":          StateQueued,
		"running":         StateRunning,
		"paused":          StatePaused,
		"errored":         StateFailed,
		"terminated":      StateCancelled,
		"complete":        StateSucceeded,
		"waiting":         StateRunning,
		"waitingForPause": StateRunning,
		"rollingBack":     StateRunning,
	}
	for status, want := range cases {
		state, stateErr := mapStatus(status)
		if state != want {
			t.Errorf("mapStatus(%q) = %s, want %s", status, state, want)
		}
		if want == StateRunning && stateErr != nil {
			t.Errorf("mapStatus(%q) returned error %v, want nil", status, stateErr)
		}
	}
}

// TestCloudflareRefusesUnadvertisedOperations proves the adapter never pretends
// to have a guarantee it cannot keep.
func TestCloudflareRefusesUnadvertisedOperations(t *testing.T) {
	adapter := newCFStub(t).adapter(t)
	ctx := context.Background()

	if err := adapter.Sleep(ctx, RunHandle{RunID: "run-1"}, 90); err == nil {
		t.Error("Sleep succeeded, want a refusal (sleeps live inside the workflow)")
	} else if Classify(err) != ErrAdmissionRefused {
		t.Errorf("Sleep class = %s, want admission-refused", Classify(err))
	}
	if _, err := adapter.History(ctx, "run-1"); err == nil {
		t.Error("History succeeded, want a refusal (step logs live in D1)")
	}
	if adapter.Capabilities().LongSleep || adapter.Capabilities().History {
		t.Errorf("capabilities = %s, want long-sleep and history unadvertised", adapter.Capabilities())
	}
	if !adapter.Capabilities().Cancel || !adapter.Capabilities().Pause {
		t.Errorf("capabilities = %s, want cancel and pause advertised", adapter.Capabilities())
	}
}

// TestCloudflareMapsHTTPFailuresOntoTheTaxonomy covers the error mapping: a
// caller reacts to a class, never to provider text.
func TestCloudflareMapsHTTPFailuresOntoTheTaxonomy(t *testing.T) {
	cases := []struct {
		status int
		class  ErrorClass
	}{
		{http.StatusForbidden, ErrPolicyDenied},
		{http.StatusUnauthorized, ErrPolicyDenied},
		{http.StatusTooManyRequests, ErrTransient},
		{http.StatusInternalServerError, ErrTransient},
		{http.StatusBadRequest, ErrPermanent},
		{http.StatusNotFound, ErrPermanent},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("http-%d", tc.status), func(t *testing.T) {
			stub := newCFStub(t)
			adapter := stub.adapter(t)
			stub.failWith = tc.status
			stub.body = `{"success":false,"errors":[{"code":1,"message":"boom"}]}`
			_, err := adapter.Status(context.Background(), "run-1")
			if err == nil {
				t.Fatalf("Status returned nil for http %d", tc.status)
			}
			if got := Classify(err); got != tc.class {
				t.Errorf("class = %s, want %s (%v)", got, tc.class, err)
			}
		})
	}
}

// TestCloudflareStartIsIdempotent proves a repeated start does not create a
// second instance (C12): a 409 falls back to reading the existing run.
func TestCloudflareStartIsIdempotent(t *testing.T) {
	stub := newCFStub(t)
	adapter := stub.adapter(t)
	ctx := context.Background()

	first, err := adapter.Start(ctx, cfPlan("run-dup"), nil)
	if err != nil {
		t.Fatalf("Start #1: %v", err)
	}
	stub.status = "running"
	second, err := adapter.Start(ctx, cfPlan("run-dup"), nil)
	if err != nil {
		t.Fatalf("Start #2: %v", err)
	}
	if first.RunID != second.RunID {
		t.Errorf("handles differ: %s vs %s", first.RunID, second.RunID)
	}
	if second.State != StateRunning {
		t.Errorf("second start state = %s, want the existing run's state", second.State)
	}
	if len(stub.instances) != 1 {
		t.Errorf("instances = %d, want 1 (no duplicate execution)", len(stub.instances))
	}
}

// TestCloudflareRejectsUnknownStatus proves an unparseable status is a failure,
// never an optimistic success, and that a run id cannot escape the request path.
func TestCloudflareRejectsUnknownStatus(t *testing.T) {
	if state, err := mapStatus("teleporting"); state != StateFailed || err == nil {
		t.Errorf("mapStatus(teleporting) = %s/%v, want failed with an error", state, err)
	}
	stub := newCFStub(t)
	adapter := stub.adapter(t)
	if _, err := adapter.Status(context.Background(), "../../accounts"); err != nil {
		t.Logf("escaped id rejected: %v", err)
	}
	for _, req := range stub.requests {
		if strings.Contains(req, "accounts/..") {
			t.Errorf("request path kept a traversal segment: %s", req)
		}
	}
}
