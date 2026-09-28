package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// validPlanJSON is a schema-faithful planner payload shared by the tests.
const validPlanJSON = `{
  "thought_process": "VFS empty; greenfield build planned.",
  "subtasks": ["Data & state", "UI & wiring"],
  "goal": "a working todo app",
  "steps": [
    {"action": "create", "file_path": "public/js/data.js", "description": "seed todo items", "associated_subtask_index": 0},
    {"action": "create", "file_path": "public/js/store.js", "description": "state + localStorage", "associated_subtask_index": 0},
    {"action": "modify", "file_path": "public/index.html", "description": "wire the entry point", "associated_subtask_index": 1}
  ]
}`

func mustParse(t *testing.T, raw string) ExecutionPlan {
	t.Helper()
	plan, err := ParsePlan(raw)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	return plan
}

func TestParsePlanClean(t *testing.T) {
	plan := mustParse(t, validPlanJSON)
	if plan.Goal != "a working todo app" {
		t.Errorf("goal = %q", plan.Goal)
	}
	if len(plan.Subtasks) != 2 || len(plan.Steps) != 3 {
		t.Fatalf("subtasks=%d steps=%d; want 2, 3", len(plan.Subtasks), len(plan.Steps))
	}
	if plan.Steps[0].FilePath != "public/js/data.js" || plan.Steps[0].Action != ActionCreate {
		t.Errorf("step 0 = %+v", plan.Steps[0])
	}
	if plan.Steps[2].Action != ActionModify || plan.Steps[2].AssociatedSubtaskIndex != 1 {
		t.Errorf("step 2 = %+v", plan.Steps[2])
	}
	if err := plan.Validate(); err != nil {
		t.Errorf("Validate on parsed plan: %v", err)
	}
}

func TestParsePlanTolerant(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{"fenced json", "```json\n" + validPlanJSON + "\n```"},
		{"fenced bare", "```\n" + validPlanJSON + "\n```"},
		{"prose around", "Here is the plan:\n" + validPlanJSON + "\nHope this helps!"},
		{"padded", "\n\n   " + validPlanJSON + "   \n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := mustParse(t, tc.payload)
			if len(plan.Steps) != 3 || plan.Goal != "a working todo app" {
				t.Fatalf("unexpected plan: goal=%q steps=%d", plan.Goal, len(plan.Steps))
			}
		})
	}
}

func TestParsePlanInvalid(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"empty", "   ", "empty"},
		{"no json", "I will not use JSON today.", "no JSON object"},
		{"unclosed", `{"thought_process": "x"`, "no JSON object"},
		{"broken json", `{"thought_process": "x" "goal": "y"}`, "malformed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePlan(tc.payload)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), "agent:") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	plan := mustParse(t, `{
	  "thought_process": "  padded  ",
	  "goal": "  g  ",
	  "subtasks": ["", "  st  "],
	  "steps": [
	    {"action": "  CREATE ", "file_path": "  public/index.html ", "description": " d ", "associated_subtask_index": 0}
	  ]
	}`)
	if plan.ThoughtProcess != "padded" || plan.Goal != "g" {
		t.Errorf("trim failed: %+v", plan)
	}
	if len(plan.Subtasks) != 1 || plan.Subtasks[0] != "st" {
		t.Errorf("subtasks = %#v; want [st]", plan.Subtasks)
	}
	if plan.Steps[0].Action != ActionCreate || plan.Steps[0].FilePath != "public/index.html" || plan.Steps[0].Description != "d" {
		t.Errorf("step = %+v", plan.Steps[0])
	}

	// Nil slices must marshal as [] and never as null.
	empty := ExecutionPlan{}
	empty.Normalize()
	raw, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal empty plan: %v", err)
	}
	js := string(raw)
	if strings.Contains(js, "null") {
		t.Errorf("empty plan marshals with null: %s", js)
	}
	if !strings.Contains(js, `"subtasks":[]`) || !strings.Contains(js, `"steps":[]`) {
		t.Errorf("empty plan marshals without [] slices: %s", js)
	}
}

func TestValidateAccepts(t *testing.T) {
	plan := mustParse(t, validPlanJSON)
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// An empty step list is allowed (planner explains why in thought_process).
	empty := ExecutionPlan{ThoughtProcess: "nothing to change", Subtasks: []string{}, Steps: []PlanStep{}}
	if err := empty.Validate(); err != nil {
		t.Fatalf("Validate empty steps: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	plan := ExecutionPlan{
		Subtasks: []string{"st", " "},
		Steps: []PlanStep{
			{Action: "rename", FilePath: "a.js", AssociatedSubtaskIndex: 0},
			{Action: ActionCreate, FilePath: "", AssociatedSubtaskIndex: 5},
			{Action: ActionModify, FilePath: "b.js", AssociatedSubtaskIndex: 0},
			{Action: ActionDelete, FilePath: "b.js", AssociatedSubtaskIndex: 0},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("expected validation errors")
	}
	for _, want := range []string{
		"subtask 1 is empty",
		`action "rename"`,
		"step 1: file_path is empty",
		"associated_subtask_index 5 is out of range",
		`file "b.js" is planned twice`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("joined error misses %q:\n%s", want, err)
		}
	}
}

func TestRender(t *testing.T) {
	plan := mustParse(t, validPlanJSON)
	out := plan.Render()
	for _, want := range []string{
		"**Goal:** a working todo app",
		"**Subtasks:**",
		"1. Data & state",
		"**Steps:**",
		"1. `create` `public/js/data.js` — seed todo items _(subtask 1: Data & state)_",
		"3. `modify` `public/index.html`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render misses %q:\n%s", want, out)
		}
	}

	if got := (ExecutionPlan{}).Render(); got != "No file changes needed." {
		t.Errorf("empty render = %q", got)
	}
}

// TestNormalizeDeduplicatesFiles: providers frequently re-plan a file
// (e.g. "create public/index.html" at step 0 and again at step 2). The
// plan must not be rejected — Normalize keeps the LAST occurrence so the
// later modify/finalize intent wins, and the plan validates cleanly.
func TestNormalizeDeduplicatesFiles(t *testing.T) {
	plan := ExecutionPlan{
		Subtasks: []string{"setup", "polish"},
		Steps: []PlanStep{
			{Action: ActionCreate, FilePath: "public/index.html", AssociatedSubtaskIndex: 0},
			{Action: ActionCreate, FilePath: "public/styles.css", AssociatedSubtaskIndex: 0},
			{Action: ActionModify, FilePath: "public/index.html", AssociatedSubtaskIndex: 1},
			{Action: ActionModify, FilePath: "public/styles.css", AssociatedSubtaskIndex: 1},
		},
	}
	plan.Normalize()
	if err := plan.Validate(); err != nil {
		t.Fatalf("deduped plan must validate: %v", err)
	}
	if got := len(plan.Steps); got != 2 {
		t.Fatalf("expected 2 steps after dedup, got %d", got)
	}
	// Both remaining steps must be the later (modify) ones.
	if plan.Steps[0].Action != ActionModify || plan.Steps[0].FilePath != "public/index.html" {
		t.Fatalf("step 0 should be the later modify of index.html, got %s %s", plan.Steps[0].Action, plan.Steps[0].FilePath)
	}
	if plan.Steps[1].Action != ActionModify || plan.Steps[1].FilePath != "public/styles.css" {
		t.Fatalf("step 1 should be the later modify of styles.css, got %s %s", plan.Steps[1].Action, plan.Steps[1].FilePath)
	}
}
