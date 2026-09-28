// Package agent defines the structured output contracts of the planning
// stage. The planner model emits exactly one raw JSON object that decodes
// into an ExecutionPlan following a strict Chain-of-Thought order:
//
//  1. thought_process — the initial analytical work (VFS analysis,
//     requirement decomposition, chosen approach and risks)
//  2. subtasks        — the high-level decomposition of the problem
//  3. goal            — the summary of the final objective
//  4. steps           — atomic, ordered file instructions (one file each)
package agent

import (
	"sort"
	"strings"

	"backend/pkg/design"
)

// PlanStep is one atomic file instruction inside an ExecutionPlan. Each
// step targets exactly one file and carries the action to apply to it.
type PlanStep struct {
	Action                 string `json:"action"` // "create", "modify", "delete"
	FilePath               string `json:"file_path"`
	Description            string `json:"description"`
	AssociatedSubtaskIndex int    `json:"associated_subtask_index"`
}

// ExecutionPlan is the top-level planning document produced by the
// planner model. The field order mirrors the mandatory Chain-of-Thought
// sequence: analytical reasoning first, then the coarse subtask split,
// then the goal, then the executable atomic steps.
type ExecutionPlan struct {
	ThoughtProcess string     `json:"thought_process"` // کارهای تحلیلی اولیه
	Subtasks       []string   `json:"subtasks"`        // گام‌های کلان مسئله
	Goal           string     `json:"goal"`            // خلاصه هدف نهایی
	Steps          []PlanStep `json:"steps"`           // دستورالعمل‌های اتمی فایل‌ها
}

// StepContext is the plan-level context handed to the coder alongside its
// single file task. It is the difference between a file that is merely
// syntactically valid and a file that actually belongs to the app the user
// asked for.
//
// Without it the coder only ever sees one step's `description` — a single
// technical sentence such as "Basic HTML5 doctype, lang, charset, and
// responsive viewport meta tag." — which carries no trace of the user's
// request. The model then emits a generic boilerplate file (an empty
// <title>Document</title>, a stylesheet holding nothing but :root tokens,
// an index.html that never links the styles.css or main.js the plan also
// created), and the preview renders as an unrelated, unstyled page.
//
// StepContext carries the user's original request, the plan's goal and
// subtasks, and the full step list so every file can be written with the
// right domain, branding and cross-file wiring.
type StepContext struct {
	// UserRequest is the user's original prompt, verbatim. This is the
	// single most important field: it is the only place the product
	// domain ("coffee landing page") survives into the coder call.
	UserRequest string `json:"user_request,omitempty"`
	// Goal is the plan's one-sentence final objective.
	Goal string `json:"goal,omitempty"`
	// Subtasks are the plan's coarse decomposition, in order.
	Subtasks []string `json:"subtasks,omitempty"`
	// Steps is the full ordered step list (path + action + description),
	// so the coder knows which sibling files exist and must be linked,
	// imported or called by the file it is writing.
	Steps []PlanStepRef `json:"steps,omitempty"`
	// RelatedFiles carries the REAL content of sibling files already
	// written by earlier steps (path -> contents).
	//
	// Plan steps execute sequentially, so by the time styles.css or
	// main.js is generated the entry index.html already exists — but
	// planning only described it. Without the actual markup the coder
	// guesses selectors, and the guess is what breaks the app: markup
	// emits `class="about-section"` while the stylesheet defines `.about`
	// (section renders unstyled), markup emits `id="order-now"` while the
	// script binds `.order-now` (the CTA does nothing). Handing over the
	// finished sibling lets the coder match real class names, ids and
	// data attributes instead of inventing parallel ones.
	RelatedFiles map[string]string `json:"related_files,omitempty"`
	// Design is the shared design direction (style, pattern, palette tokens,
	// fonts, motion, a11y) assembled once from the user's request and
	// distilled for the coder. Carried on every step so the whole app shares
	// one design system — without it each file invents its own ad-hoc colors
	// and generic copy.
	Design *design.CoderBrief `json:"design,omitempty"`
}

// PlanStepRef is a compact, non-recursive view of one plan step for the
// coder's plan_context. It deliberately omits associated_subtask_index:
// the coder needs to know what its siblings are, not how the planner
// grouped them.
type PlanStepRef struct {
	Action      string `json:"action"`
	FilePath    string `json:"file_path"`
	Description string `json:"description,omitempty"`
}

// StepContextFrom builds the coder's plan_context from a full plan. The
// returned value is never nil, so callers can pass it straight through;
// an empty plan yields a context with no steps.
func StepContextFrom(userRequest string, plan ExecutionPlan) StepContext {
	refs := make([]PlanStepRef, 0, len(plan.Steps))
	for _, st := range plan.Steps {
		refs = append(refs, PlanStepRef{
			Action:      st.Action,
			FilePath:    st.FilePath,
			Description: st.Description,
		})
	}
	return StepContext{
		UserRequest: strings.TrimSpace(userRequest),
		Goal:        plan.Goal,
		Subtasks:    plan.Subtasks,
		Steps:       refs,
	}
}

// IsEmpty reports whether the context carries nothing worth sending, so
// the coder task can omit plan_context entirely instead of emitting an
// empty object.
func (c StepContext) IsEmpty() bool {
	return c.UserRequest == "" && c.Goal == "" &&
		len(c.Subtasks) == 0 && len(c.Steps) == 0 &&
		len(c.RelatedFiles) == 0 && c.Design == nil
}

// maxRelatedFileChars caps how much of one sibling file is embedded in the
// coder prompt. Stylesheets and entry markup are the files whose selectors
// must line up, and they are small; a very large sibling is cut so a
// multi-file app never blows the coder's context window.
const maxRelatedFileChars = 6000

// maxRelatedFiles caps how many sibling files are embedded, nearest-first
// by plan order.
const maxRelatedFiles = 4

// WithRelatedFiles returns a copy of the context carrying the real content
// of the already-written sibling files, so a later step can match the
// markup an earlier step actually produced instead of guessing at it.
//
// `self` is the file the coder is about to write: its own current content
// is already carried as `existing_content`, so it is excluded here to avoid
// duplicating it. Cap-limited (see maxRelatedFiles / maxRelatedFileChars)
// so the coder prompt stays bounded on large projects.
func (c StepContext) WithRelatedFiles(self string, written map[string]string) StepContext {
	if len(written) == 0 {
		return c
	}
	// Deterministic order: plan step order first, then any extra paths
	// sorted, so the same VFS always produces the same prompt.
	order := make([]string, 0, len(c.Steps))
	for _, st := range c.Steps {
		if st.FilePath != self {
			order = append(order, st.FilePath)
		}
	}
	seen := make(map[string]bool, len(order))
	for _, p := range order {
		seen[p] = true
	}
	extra := make([]string, 0, len(written))
	for p := range written {
		if p != self && !seen[p] {
			extra = append(extra, p)
		}
	}
	sort.Strings(extra)
	order = append(order, extra...)

	out := c
	out.RelatedFiles = make(map[string]string, maxRelatedFiles)
	for _, p := range order {
		if len(out.RelatedFiles) >= maxRelatedFiles {
			break
		}
		body, ok := written[p]
		if !ok || strings.TrimSpace(body) == "" {
			continue
		}
		if len(body) > maxRelatedFileChars {
			body = body[:maxRelatedFileChars] + "\n/* …truncated for the prompt… */"
		}
		out.RelatedFiles[p] = body
	}
	if len(out.RelatedFiles) == 0 {
		out.RelatedFiles = nil
	}
	return out
}
