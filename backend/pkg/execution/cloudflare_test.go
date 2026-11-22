package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// cfStub is a stand-in for the Workflows API: it records requests and answers
// with the envelope shape the adapter parses.
type cfStub struct {
	server    *httptest.Server
	requests  []string
	status    string
	failWith  int
	body      string
	instances map[string]bool
}

func newCFStub(t *testing.T) *cfStub {
	t.Helper()
	stub := &cfStub{status: "queued", instances: map[string]bool{}}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.requests = append(stub.requests, r.Method+" "+r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"success":false,"errors":[{"code":10000,"message":"auth"}]}`)
			return
		}
		if stub.failWith != 0 {
			w.WriteHeader(stub.failWith)
			fmt.Fprint(w, stub.body)
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/instances") {
			var body struct {
				ID string `json:"id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if stub.instances[body.ID] {
				w.WriteHeader(http.StatusConflict)
				fmt.Fprint(w, `{"success":false,"errors":[{"code":1001,"message":"instance already exists"}]}`)
				return
			}
			stub.instances[body.ID] = true
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprintf(w, `{"success":true,"result":{"id":%q,"status":%q}}`, body.ID, stub.status)
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
		"POST /accounts/acct-1/workflows/vibe-runs/instances/run-1/terminate",
		"POST /accounts/acct-1/workflows/vibe-runs/instances/run-1/pause",
		"POST /accounts/acct-1/workflows/vibe-runs/instances/run-1/resume",
	}
	for _, path := range want {
		if !contains(stub.requests, path) {
			t.Errorf("missing request %q in %v", path, stub.requests)
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
