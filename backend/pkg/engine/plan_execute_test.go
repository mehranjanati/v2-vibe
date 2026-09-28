package engine

import (
	"testing"

	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
)

// TestPlanStepsSummary verifies the prompt helper renders plan JSON and
// executed steps without panicking on nil plans.
func TestPlanStepsSummary(t *testing.T) {
	var nilPlan planexecute.Plan
	got := planStepsSummary(nilPlan, nil)
	if got != "" {
		t.Fatalf("nil plan should render empty, got %q", got)
	}

	executed := []planexecute.ExecutedStep{{Step: "step one", Result: "done"}}
	out := planStepsSummary(nilPlan, executed)
	if out == "" || !contains(out, "step one") || !contains(out, "Already executed") {
		t.Fatalf("unexpected summary: %q", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestRoomVFSStore exercises the room-as-VFSStore adapter used by the
// planexecute executor tools.
func TestRoomVFSStore(t *testing.T) {
	r := NewProjectRoom("vfs-store-test", nil, nil, nil)
	s := roomVFSStore{r: r}

	if err := s.VFSWrite(t.Context(), "ignored-chat", "public/index.html", "<p>hi</p>"); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, ok, err := s.VFSRead(t.Context(), "ignored-chat", "public/index.html")
	if err != nil || !ok || got != "<p>hi</p>" {
		t.Fatalf("read: ok=%v got=%q err=%v", ok, got, err)
	}
	paths, err := s.VFSList(t.Context(), "ignored-chat")
	if err != nil || len(paths) != 1 || paths[0] != "public/index.html" {
		t.Fatalf("list: %v %v", paths, err)
	}
	if err := s.VFSDelete(t.Context(), "ignored-chat", "public/index.html"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := s.VFSRead(t.Context(), "ignored-chat", "public/index.html"); ok {
		t.Fatal("file still present after delete")
	}
}
