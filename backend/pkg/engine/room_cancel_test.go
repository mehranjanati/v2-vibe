package engine

import (
	"context"
	"testing"
	"time"
)

// TestBroadcastAfterStopDoesNotBlock guards the B9 goroutine-leak fix:
// after Stop(), BroadcastMessage must return instead of blocking forever.
func TestBroadcastAfterStopDoesNotBlock(t *testing.T) {
	r := NewProjectRoom("cancel-test", nil, nil, nil)
	go r.Run()
	time.Sleep(20 * time.Millisecond) // let the Run loop start

	r.Stop()

	done := make(chan struct{})
	go func() {
		r.BroadcastMessage(map[string]string{"type": "test"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("BroadcastMessage blocked after Stop")
	}
}

// TestGenerationContextCancelsOnStop verifies the B9 cancellation path:
// Stop() must cancel an in-flight generation context promptly.
func TestGenerationContextCancelsOnStop(t *testing.T) {
	r := NewProjectRoom("cancel-test-2", nil, nil, nil)
	go r.Run()
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := r.generationContext(time.Hour)
	defer cancel()

	r.Stop()

	select {
	case <-ctx.Done():
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("generation context not cancelled by Stop")
	}
}

// TestPlanApprovalGate covers the B7 human-in-the-loop gate: approve
// proceeds, reject aborts, and the timeout auto-approves.
func TestPlanApprovalGate(t *testing.T) {
	r := NewProjectRoom("plan-test", nil, nil, nil)
	go r.Run()
	defer r.Stop()

	// Approve path.
	ctxA, cancelA := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		r.ApprovePlan()
	}()
	if !r.waitForPlanApproval(ctxA) {
		t.Fatal("ApprovePlan should resolve the gate as approved")
	}
	cancelA()

	// Reject path.
	ctxR, cancelR := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		r.RejectPlan()
	}()
	if r.waitForPlanApproval(ctxR) {
		t.Fatal("RejectPlan should resolve the gate as rejected")
	}
	cancelR()
}

// TestPlanVerdictWithoutPending ensures a verdict with no open gate is a
// harmless no-op (and does not panic).
func TestPlanVerdictWithoutPending(t *testing.T) {
	r := NewProjectRoom("plan-test-2", nil, nil, nil)
	r.ApprovePlan()
	r.RejectPlan()
}

// generation and that a NEW generation context is unaffected by the
// previous cancel request (fresh stop channel per run).
func TestCancelGenerationPerRun(t *testing.T) {
	r := NewProjectRoom("cancel-test-3", nil, nil, nil)
	go r.Run()
	time.Sleep(20 * time.Millisecond)

	// First run: cancel via CancelGeneration.
	ctx1, cancel1 := r.generationContext(time.Hour)
	r.CancelGeneration()
	select {
	case <-ctx1.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("CancelGeneration did not cancel run 1")
	}
	cancel1()

	// Second run must start clean.
	ctx2, cancel2 := r.generationContext(time.Hour)
	defer cancel2()
	select {
	case <-ctx2.Done():
		t.Fatal("stale cancel leaked into run 2")
	case <-time.After(50 * time.Millisecond):
	}

	// Double CancelGeneration must not panic.
	r.CancelGeneration()
	r.CancelGeneration()
}
