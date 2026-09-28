package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// jsonFenceRe extracts fenced ```json blocks from the skill prompts.
var jsonFenceRe = regexp.MustCompile("(?s)```json[ \t]*\r?\n(.*?)```")

// TestPlannerPromptEmitsValidPlan locks backend/skills/01_planner.md to the
// ExecutionPlan contract: every ```json example in the prompt must decode
// into an ExecutionPlan and pass Validate. If this test fails after editing
// the prompt, its output contract has drifted from types.go — fix the
// prompt, not the parser.
func TestPlannerPromptEmitsValidPlan(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "skills", "01_planner.md"))
	if err != nil {
		t.Fatalf("read planner prompt: %v", err)
	}
	blocks := jsonFenceRe.FindAllStringSubmatch(string(raw), -1)
	if len(blocks) == 0 {
		t.Fatal("no ```json example found in 01_planner.md")
	}
	for i, m := range blocks {
		payload := strings.TrimSpace(m[1])
		plan, err := ParsePlan(payload)
		if err != nil {
			t.Errorf("json example %d does not decode into ExecutionPlan: %v\n%s", i+1, err, payload)
			continue
		}
		if err := plan.Validate(); err != nil {
			t.Errorf("json example %d violates the plan contract: %v", i+1, err)
		}
	}
}
