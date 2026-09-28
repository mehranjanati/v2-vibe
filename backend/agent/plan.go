package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Action constants a PlanStep may carry. The planner prompt mandates
// exactly these values: create only for paths missing from the VFS,
// modify only for existing paths, delete for removals.
const (
	ActionCreate = "create"
	ActionModify = "modify"
	ActionDelete = "delete"
)

// validActions is the closed set of step actions.
var validActions = map[string]struct{}{
	ActionCreate: {},
	ActionModify: {},
	ActionDelete: {},
}

// ParsePlan decodes the planner model's raw output into an ExecutionPlan.
//
// The planner is instructed to emit exactly one raw JSON object, but models
// occasionally wrap it in markdown fences or sprinkle prose around it, so
// decoding is deliberately tolerant: the direct decode is tried first and,
// on failure, the outermost {...} span of the payload is retried. The
// decoded plan is normalized before it is returned (see Normalize).
func ParsePlan(raw string) (ExecutionPlan, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ExecutionPlan{}, errors.New("agent: plan payload is empty")
	}

	var plan ExecutionPlan
	if err := json.Unmarshal([]byte(trimmed), &plan); err != nil {
		start, end, ok := outerJSONObject(trimmed)
		if !ok {
			return ExecutionPlan{}, fmt.Errorf("agent: plan payload contains no JSON object: %w", err)
		}
		if err := json.Unmarshal([]byte(trimmed[start:end]), &plan); err != nil {
			return ExecutionPlan{}, fmt.Errorf("agent: plan JSON is malformed: %w", err)
		}
	}
	plan.Normalize()
	return plan, nil
}

// outerJSONObject returns the byte span [start,end) of the outermost {...}
// object in s.
func outerJSONObject(s string) (int, int, bool) {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return 0, 0, false
	}
	return start, end + 1, true
}

// Normalize trims string fields, lowercases step actions and replaces nil
// slices with empty ones so a normalized plan always marshals with
// "subtasks": [] and "steps": [] instead of null.
//
// It also DEDUPLICATES file paths: providers frequently re-plan a file they
// already listed (e.g. "create public/index.html" at step 0 and again at
// step 2 after adding scripts). The ExecutionPlan contract used to reject
// such plans outright, which sent the whole generation into the weaker
// fence-streaming fallback and produced visibly wrong output. Keeping the
// LAST occurrence of each path (so a later modify/finalize wins) is the
// faithful interpretation of the model's intent.
func (p *ExecutionPlan) Normalize() {
	p.ThoughtProcess = strings.TrimSpace(p.ThoughtProcess)
	p.Goal = strings.TrimSpace(p.Goal)

	subtasks := make([]string, 0, len(p.Subtasks))
	for _, s := range p.Subtasks {
		if s = strings.TrimSpace(s); s != "" {
			subtasks = append(subtasks, s)
		}
	}
	p.Subtasks = subtasks

	// First normalize every step (fields), then keep the last step per path.
	steps := make([]PlanStep, 0, len(p.Steps))
	for _, st := range p.Steps {
		st.Action = strings.ToLower(strings.TrimSpace(st.Action))
		st.FilePath = strings.TrimSpace(st.FilePath)
		st.Description = strings.TrimSpace(st.Description)
		if st.FilePath != "" {
			steps = append(steps, st)
		}
	}

	// Keep the LAST occurrence of each unique path (later steps win).
	lastIdx := make(map[string]int, len(steps))
	for i, st := range steps {
		lastIdx[st.FilePath] = i
	}
	deduped := make([]PlanStep, 0, len(lastIdx))
	for i, st := range steps {
		if prev, ok := lastIdx[st.FilePath]; ok && prev == i {
			deduped = append(deduped, st)
		}
	}
	p.Steps = deduped
}

// Validate reports every contract violation it finds in one joined error so
// the caller can feed the full list back to the model for a repair pass:
//   - subtask entries must be non-empty
//   - actions must be exactly create / modify / delete
//   - every step needs a file path
//   - every step must reference an existing subtask index
//   - a file must be planned at most once (one file per step)
//
// An empty step list is valid: the planner may conclude that no file changes
// are needed as long as thought_process explains why.
func (p ExecutionPlan) Validate() error {
	var errs []error

	for i, s := range p.Subtasks {
		if strings.TrimSpace(s) == "" {
			errs = append(errs, fmt.Errorf("subtask %d is empty", i))
		}
	}

	seen := make(map[string]int, len(p.Steps))
	for i, st := range p.Steps {
		if _, ok := validActions[st.Action]; !ok {
			errs = append(errs, fmt.Errorf("step %d: action %q is not one of %q, %q, %q",
				i, st.Action, ActionCreate, ActionModify, ActionDelete))
		}
		if st.FilePath == "" {
			errs = append(errs, fmt.Errorf("step %d: file_path is empty", i))
		} else if prev, dup := seen[st.FilePath]; dup {
			errs = append(errs, fmt.Errorf("step %d: file %q is planned twice (first at step %d)", i, st.FilePath, prev))
		} else {
			seen[st.FilePath] = i
		}
		if st.AssociatedSubtaskIndex < 0 || st.AssociatedSubtaskIndex >= len(p.Subtasks) {
			errs = append(errs, fmt.Errorf("step %d: associated_subtask_index %d is out of range (want 0..%d)",
				i, st.AssociatedSubtaskIndex, len(p.Subtasks)-1))
		}
	}

	return errors.Join(errs...)
}

// Render formats the plan as compact human-readable Markdown for the chat
// thread (the plan_proposed event carries a plain string). A plan without
// steps renders its reasoning only, or a single "No file changes needed."
// line when it is completely empty.
func (p ExecutionPlan) Render() string {
	var b strings.Builder

	if p.Goal != "" {
		b.WriteString("**Goal:** " + p.Goal + "\n\n")
	}
	if p.ThoughtProcess != "" {
		b.WriteString("**Reasoning:** " + p.ThoughtProcess + "\n\n")
	}
	if len(p.Subtasks) > 0 {
		b.WriteString("**Subtasks:**\n")
		for i, s := range p.Subtasks {
			fmt.Fprintf(&b, "%d. %s\n", i+1, s)
		}
		b.WriteString("\n")
	}
	if len(p.Steps) == 0 {
		if b.Len() == 0 {
			return "No file changes needed."
		}
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString("**Steps:**\n")
	for i, st := range p.Steps {
		line := fmt.Sprintf("%d. `%s` `%s`", i+1, st.Action, st.FilePath)
		if st.Description != "" {
			line += " — " + st.Description
		}
		if st.AssociatedSubtaskIndex >= 0 && st.AssociatedSubtaskIndex < len(p.Subtasks) {
			line += fmt.Sprintf(" _(subtask %d: %s)_", st.AssociatedSubtaskIndex+1, p.Subtasks[st.AssociatedSubtaskIndex])
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
