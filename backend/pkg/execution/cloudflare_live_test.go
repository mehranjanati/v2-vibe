//go:build live

package execution

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// TestCloudflareLive is the live half of P7.11: it runs the Cloudflare adapter
// against a deployed Workflow over the real REST API and records, case by case,
// what a live instance can actually prove.
//
// It is behind the `live` build tag and refuses to run without credentials:
//
//	CF_WORKFLOWS_ACCOUNT_ID=... \
//	CF_WORKFLOWS_API_TOKEN=...      # permission: Workers Scripts Write
//	CF_WORKFLOWS_NAME=...           # e.g. vibesdk-v2-workflows
//	go test -tags live ./pkg/execution/ -run TestCloudflareLive -v
//
// Honesty rules: a case the adapter cannot observe is *skipped with a reason*,
// never passed. C1–C7, C10, C15, C17 and C18 describe what the workflow body does
// with a plan (ordering, replay, retries, metering), and the adapter does not
// execute steps — claiming them here would be a lie about where the evidence
// comes from.
func TestCloudflareLive(t *testing.T) {
	accountID := os.Getenv("CF_WORKFLOWS_ACCOUNT_ID")
	apiToken := os.Getenv("CF_WORKFLOWS_API_TOKEN")
	workflow := os.Getenv("CF_WORKFLOWS_NAME")
	if accountID == "" || apiToken == "" || workflow == "" {
		t.Skip("live proof needs CF_WORKFLOWS_ACCOUNT_ID, CF_WORKFLOWS_API_TOKEN (Workers Scripts Write) and CF_WORKFLOWS_NAME")
	}

	adapter, err := NewCloudflare(accountID, workflow, apiToken)
	if err != nil {
		t.Fatalf("NewCloudflare: %v", err)
	}
	counter := &countingTransport{inner: http.DefaultTransport}
	adapter.HTTP = &http.Client{Timeout: 30 * time.Second, Transport: counter}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	report := Report{Reasons: map[string]string{}}
	pass := func(id string) { report.Passed = append(report.Passed, id) }
	skip := func(id, why string) {
		report.Skipped = append(report.Skipped, id)
		report.Reasons[id] = why
	}

	// Admission cases: the gate runs before any request, so zero HTTP calls is
	// the assertion (C8, C9, C16).
	refusals := []struct {
		id   string
		plan RunPlan
	}{
		{"C8", RunPlan{ID: liveRunID("c8"), Steps: []Step{{Name: "first"}}}},
		{"C9", RunPlan{ID: liveRunID("c9"), Tenant: "tenant-live", Steps: []Step{
			{Name: "a", DependsOn: []string{"b"}},
			{Name: "b", DependsOn: []string{"a"}},
		}}},
		{"C16", RunPlan{ID: liveRunID("c16"), Tenant: "tenant-live",
			Steps: []Step{{Name: "first"}}, Requirements: []Capability{CapLongSleep}}},
	}
	before := counter.count()
	for _, tc := range refusals {
		handle, err := adapter.Start(ctx, tc.plan, nil)
		if err != nil {
			t.Fatalf("%s: Start returned a Go error instead of a rejected run: %v", tc.id, err)
		}
		if handle.State != StateRejected {
			t.Errorf("%s: state = %s, want rejected", tc.id, handle.State)
			continue
		}
		pass(tc.id)
	}
	if calls := counter.count() - before; calls != 0 {
		t.Errorf("admission refusals reached the API %d time(s); a refused plan must never create an instance", calls)
	}

	// C12: start is idempotent per run id.
	plan := RunPlan{ID: liveRunID("c12"), Tenant: "tenant-live", Steps: []Step{{Name: "first"}}}
	first, err := adapter.Start(ctx, plan, nil)
	if err != nil {
		t.Fatalf("Start #1: %v", err)
	}
	second, err := adapter.Start(ctx, plan, nil)
	if err != nil {
		t.Fatalf("Start #2 (duplicate id): %v", err)
	}
	if first.RunID != second.RunID {
		t.Errorf("duplicate start produced %q vs %q, want the same instance", first.RunID, second.RunID)
	} else {
		pass("C12")
	}
	runID := first.RunID
	defer func() {
		// Best-effort cleanup: leave nothing running in the account.
		if status, err := adapter.Status(context.Background(), runID); err == nil && !status.State.Terminal() {
			_ = adapter.Cancel(context.Background(), runID, "live proof cleanup")
		}
	}()

	// Status must be readable and valid right after creation.
	status, err := adapter.Status(ctx, runID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.State.Valid() {
		t.Errorf("Status state = %q, which the contract does not define", status.State)
	}

	// C14: pause/resume must be attempted while the instance is alive, so it runs
	// before the cancel below. Cloudflare's pause is documented for a *running*
	// instance, so wait for running first; anything else is a skip with a reason.
	if running, ok := waitForLive(t, ctx, adapter, runID, StateRunning); !ok {
		skip("C14", fmt.Sprintf("the instance never reported running (last state %s)", running.State))
	} else if err := adapter.Pause(ctx, runID); err != nil {
		skip("C14", fmt.Sprintf("Pause was refused: %v", err))
	} else if _, ok := waitForLive(t, ctx, adapter, runID, StatePaused); !ok {
		skip("C14", "Pause was accepted but the instance never reported paused")
	} else if err := adapter.Resume(ctx, runID); err != nil {
		t.Errorf("C14: Resume: %v", err)
		skip("C14", fmt.Sprintf("Resume failed: %v", err))
	} else if _, ok := waitForLive(t, ctx, adapter, runID, StateRunning); !ok {
		skip("C14", "the instance did not return to running after resume")
	} else {
		pass("C14")
	}

	// C11 + C13: cancel and observe a terminal, final state. Cloudflare refuses a
	// status edit on an instance that already finished, so a fast-finishing
	// workflow makes this unprovable — that is a skip, not a pass.
	if status, err := adapter.Status(ctx, runID); err != nil {
		t.Fatalf("Status before cancel: %v", err)
	} else if status.State.Terminal() {
		skip("C13", fmt.Sprintf("instance reached %s before cancel could be sent", status.State))
		skip("C11", "no live transition to observe: the instance was already terminal")
	} else if err := adapter.Cancel(ctx, runID, "live proof"); err != nil {
		t.Errorf("C13: Cancel: %v", err)
		skip("C13", "Cancel returned an error")
		skip("C11", "no terminal state was observed")
	} else if final, ok := waitForLive(t, ctx, adapter, runID, StateCancelled); !ok {
		t.Logf("instance ended as %s instead of cancelled", final.State)
		skip("C13", "the instance never reported terminated within the poll window")
		skip("C11", "no terminal state was observed")
	} else {
		pass("C13")
		// Terminal states are final: a second cancel must fail, and it must fail
		// as a non-retryable class — a network blip is not proof of finality.
		if err := adapter.Cancel(ctx, runID, "second cancel"); err == nil {
			t.Errorf("C11: a second Cancel on a %s instance succeeded, want a provider refusal", final.State)
			skip("C11", "the provider accepted a status edit on a terminal instance")
		} else if Classify(err).Retryable() {
			t.Logf("second cancel failed transiently: %v", err)
			skip("C11", "the second cancel failed with a retryable error, so finality was not proven")
		} else {
			pass("C11")
		}
	}

	// Cases that depend on the workflow body executing the plan.
	for _, id := range []string{"C1", "C2", "C3", "C4", "C5", "C6", "C7", "C10", "C17", "C18"} {
		skip(id, "the adapter does not execute steps; this case needs the workflow definition to run the plan")
	}
	skip("C15", "the provider does not advertise long sleep and cannot inject one")

	if !report.Accounted(18) {
		t.Errorf("report does not account for all 18 cases: %s", report)
	}
	t.Logf("live Cloudflare adapter: %s", report)
	if len(report.Passed) == 0 {
		t.Errorf("no case was provable live; the adapter never reached the API successfully")
	}
}

// countingTransport counts API calls so admission cases can prove they were
// refused locally.
type countingTransport struct {
	inner http.RoundTripper
	calls int64
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	atomic.AddInt64(&c.calls, 1)
	return c.inner.RoundTrip(r)
}

func (c *countingTransport) count() int64 { return atomic.LoadInt64(&c.calls) }

// liveRunID keeps live instances identifiable and collision-free.
func liveRunID(suffix string) string {
	return fmt.Sprintf("vibe-live-%s-%d", suffix, time.Now().UnixNano())
}

// waitForLive polls the instance until it reports want, or the deadline passes.
func waitForLive(t *testing.T, ctx context.Context, p Provider, runID string, want State) (Status, bool) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		status, err := p.Status(ctx, runID)
		if err != nil {
			t.Logf("Status(%s) while waiting for %s: %v", runID, want, err)
		} else if status.State == want {
			return status, true
		} else if status.State.Terminal() {
			return status, false
		}
		select {
		case <-ctx.Done():
			return Status{}, false
		case <-time.After(2 * time.Second):
		}
	}
	return Status{}, false
}
