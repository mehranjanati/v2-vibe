package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"backend/pkg/design"
)

// coffeePlan mirrors the real planner output for "build simple coffe
// landing": mechanics-only step descriptions, coffee only in the goal.
const coffeePlanJSON = `{
  "thought_process": "greenfield coffee landing",
  "subtasks": ["Structure", "Styling", "Interaction"],
  "goal": "A simple coffee landing page with a hero section and CTA.",
  "steps": [
    {"action": "create", "file_path": "public/index.html", "description": "Basic HTML5 doctype and viewport meta tag.", "associated_subtask_index": 0},
    {"action": "create", "file_path": "public/styles.css", "description": "Define the :root design tokens.", "associated_subtask_index": 1},
    {"action": "create", "file_path": "public/js/main.js", "description": "Initialize the hero section.", "associated_subtask_index": 2}
  ]
}`

// TestStepContextFrom: the coder context carries the user's original
// request verbatim plus the plan's goal/subtasks, and every step reduced
// to the fields the coder needs (no associated_subtask_index).
func TestStepContextFrom(t *testing.T) {
	plan := mustParse(t, coffeePlanJSON)
	ctx := StepContextFrom("  build simple coffe landing  ", plan)

	if ctx.UserRequest != "build simple coffe landing" {
		t.Errorf("user_request = %q, want the trimmed original prompt", ctx.UserRequest)
	}
	if !strings.Contains(ctx.Goal, "coffee") {
		t.Errorf("goal = %q, want the plan goal", ctx.Goal)
	}
	if len(ctx.Subtasks) != 3 {
		t.Errorf("subtasks = %v, want the plan's 3", ctx.Subtasks)
	}
	if len(ctx.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(ctx.Steps))
	}
	if ctx.Steps[0].FilePath != "public/index.html" || ctx.Steps[0].Action != ActionCreate {
		t.Errorf("step ref 0 = %+v", ctx.Steps[0])
	}
	if ctx.Steps[0].Description == "" {
		t.Error("step ref lost its description")
	}
	if ctx.IsEmpty() {
		t.Error("a populated context must not report IsEmpty")
	}
	// PlanStepRef must not leak the planner-internal grouping index.
	raw, _ := json.Marshal(ctx.Steps[0])
	if strings.Contains(string(raw), "subtask_index") {
		t.Errorf("PlanStepRef leaks associated_subtask_index: %s", raw)
	}
}

// TestStepContextIsEmpty: an entirely blank context reports empty so the
// task JSON omits plan_context instead of sending "{}".
func TestStepContextIsEmpty(t *testing.T) {
	if !(StepContext{}).IsEmpty() {
		t.Error("zero StepContext must be empty")
	}
	if (StepContext{UserRequest: "x"}).IsEmpty() {
		t.Error("a user request alone makes it non-empty")
	}
	if (StepContext{RelatedFiles: map[string]string{"a": "b"}}).IsEmpty() {
		t.Error("related files alone make it non-empty")
	}
}

// TestWithRelatedFiles pins the cross-file wiring fix: a later step
// receives the REAL content of files earlier steps already wrote, so
// styles.css can match the class names index.html actually emitted.
//
// Without it the coder guesses selectors — markup says
// class="about-section" while the stylesheet defines .about (unstyled
// section), markup says id="order-now" while the script binds .order-now
// (dead CTA).
func TestWithRelatedFiles(t *testing.T) {
	plan := mustParse(t, coffeePlanJSON)
	base := StepContextFrom("build simple coffe landing", plan)

	const markup = `<section class="about-section"><button id="order-now">Order</button></section>`
	written := map[string]string{
		"public/index.html":  markup,
		"public/styles.css":  ":root { --color-primary: #6f4e37; }\n",
		"public/js/other.js": "window.App = {};\n",
	}

	// From styles.css's perspective: it sees the finished markup and any
	// other sibling, but never itself.
	got := base.WithRelatedFiles("public/styles.css", written)
	if got.RelatedFiles["public/index.html"] != markup {
		t.Errorf("styles.css did not receive the real markup: %q",
			got.RelatedFiles["public/index.html"])
	}
	if _, self := got.RelatedFiles["public/styles.css"]; self {
		t.Error("the file being written must not be duplicated into related_files")
	}
	if got.RelatedFiles["public/js/other.js"] != "window.App = {};\n" {
		t.Error("extra already-written siblings must be included")
	}
	// The base context is not mutated (callers reuse it for every step).
	if len(base.RelatedFiles) != 0 {
		t.Errorf("WithRelatedFiles mutated the receiver: %v", base.RelatedFiles)
	}
	if base.UserRequest != got.UserRequest || base.Goal != got.Goal {
		t.Error("WithRelatedFiles must preserve the rest of the context")
	}
}

// TestWithRelatedFilesOrderAndCaps: siblings are attached in plan order
// (so the entry markup wins the budget, not an arbitrary map iteration),
// extras are sorted for determinism, and both the per-file and total caps
// keep the coder prompt bounded.
func TestWithRelatedFilesOrderAndCaps(t *testing.T) {
	plan := mustParse(t, coffeePlanJSON)
	base := StepContextFrom("build", plan)

	// Every planned file except the one being written, plus unplanned extras.
	written := map[string]string{
		"public/index.html": "<html>A</html>",
		"public/styles.css": "body{}",
		"zzz-extra.js":      "extra",
		"aaa-extra.js":      "extra",
	}
	got := base.WithRelatedFiles("public/js/main.js", written)
	if len(got.RelatedFiles) != 4 {
		t.Errorf("related files = %d, want 4: %v", len(got.RelatedFiles), got.RelatedFiles)
	}
	for _, p := range []string{"public/index.html", "public/styles.css", "aaa-extra.js", "zzz-extra.js"} {
		if _, ok := got.RelatedFiles[p]; !ok {
			t.Errorf("missing sibling %q in %v", p, got.RelatedFiles)
		}
	}

	// More siblings than the cap: planned paths must be preferred over
	// arbitrary unplanned extras.
	many := map[string]string{
		"public/index.html": "<html>A</html>",
		"public/styles.css": "body{}",
		"a.js":              "a", "b.js": "b", "c.js": "c", "d.js": "d", "e.js": "e",
	}
	capped := base.WithRelatedFiles("public/js/main.js", many)
	if len(capped.RelatedFiles) != maxRelatedFiles {
		t.Errorf("related files = %d, want the cap %d", len(capped.RelatedFiles), maxRelatedFiles)
	}
	if _, ok := capped.RelatedFiles["public/index.html"]; !ok {
		t.Errorf("the entry markup must survive the cap: %v", capped.RelatedFiles)
	}
}

// TestWithRelatedFilesTruncatesLongSibling: one huge sibling must not blow
// the coder's context window; it is cut with a visible marker.
func TestWithRelatedFilesTruncatesLongSibling(t *testing.T) {
	plan := mustParse(t, coffeePlanJSON)
	base := StepContextFrom("build", plan)

	huge := strings.Repeat("x", maxRelatedFileChars+500)
	got := base.WithRelatedFiles("public/js/main.js", map[string]string{
		"public/index.html": huge,
	})
	body := got.RelatedFiles["public/index.html"]
	if len(body) >= len(huge) {
		t.Errorf("oversized sibling was not truncated: %d bytes", len(body))
	}
	if !strings.Contains(body, "truncated") {
		t.Error("the truncation must be marked so the coder knows the file is partial")
	}
	if len(body) > maxRelatedFileChars+64 {
		t.Errorf("truncated body = %d bytes, want <= ~%d", len(body), maxRelatedFileChars+64)
	}
}

// TestWithRelatedFilesNoop: nothing written yet (the first step) leaves the
// context untouched — no empty map in the JSON.
func TestWithRelatedFilesNoop(t *testing.T) {
	plan := mustParse(t, coffeePlanJSON)
	base := StepContextFrom("build", plan)

	if got := base.WithRelatedFiles("public/index.html", nil); got.RelatedFiles != nil {
		t.Errorf("nil written must leave RelatedFiles nil, got %v", got.RelatedFiles)
	}
	if got := base.WithRelatedFiles("public/index.html", map[string]string{}); got.RelatedFiles != nil {
		t.Errorf("empty written must leave RelatedFiles nil, got %v", got.RelatedFiles)
	}
	// Blank/whitespace-only siblings are dropped, not sent as "".
	got := base.WithRelatedFiles("public/styles.css", map[string]string{
		"public/index.html": "   \n ",
	})
	if got.RelatedFiles != nil {
		t.Errorf("blank sibling must be dropped, got %v", got.RelatedFiles)
	}
}

// TestCoderTaskContentCarriesPlanContext: the user turn the coder receives
// embeds plan_context (including related_files) alongside the step's own
// requirements — the wire-level guarantee that the user's prompt reaches
// the model at all.
func TestCoderTaskContentCarriesPlanContext(t *testing.T) {
	plan := mustParse(t, coffeePlanJSON)
	ctx := StepContextFrom("build simple coffe landing", plan).
		WithRelatedFiles("public/styles.css", map[string]string{
			"public/index.html": `<section class="about-section"></section>`,
		})

	step := plan.Steps[1] // public/styles.css
	raw := coderTaskContent(step, "", ctx)

	var task struct {
		Path         string `json:"path"`
		Action       string `json:"action"`
		Requirements string `json:"requirements"`
		PlanContext  *struct {
			UserRequest  string            `json:"user_request"`
			Goal         string            `json:"goal"`
			Subtasks     []string          `json:"subtasks"`
			Steps        []PlanStepRef     `json:"steps"`
			RelatedFiles map[string]string `json:"related_files"`
		} `json:"plan_context"`
		ExistingContent string `json:"existing_content"`
	}
	if err := json.Unmarshal([]byte(raw), &task); err != nil {
		t.Fatalf("task JSON: %v\n%s", err, raw)
	}
	if task.Path != "public/styles.css" || task.Action != ActionCreate {
		t.Errorf("task = %+v", task)
	}
	if task.PlanContext == nil {
		t.Fatal("plan_context missing from the coder task")
	}
	if task.PlanContext.UserRequest != "build simple coffe landing" {
		t.Errorf("user_request = %q", task.PlanContext.UserRequest)
	}
	if got := task.PlanContext.RelatedFiles["public/index.html"]; !strings.Contains(got, "about-section") {
		t.Errorf("related_files lost the real markup: %q", got)
	}
	if task.ExistingContent != "" {
		t.Errorf("a create task must not carry existing_content: %q", task.ExistingContent)
	}
}

// TestCoderTaskContentOmitsEmptyPlanContext: with no context at all the
// field is omitted entirely rather than serialized as an empty object.
func TestCoderTaskContentOmitsEmptyPlanContext(t *testing.T) {
	raw := coderTaskContent(PlanStep{Action: ActionCreate, FilePath: "a.js"}, "", StepContext{})
	if strings.Contains(raw, "plan_context") {
		t.Errorf("empty plan_context must be omitted: %s", raw)
	}
	if !strings.Contains(raw, `"path":"a.js"`) {
		t.Errorf("task JSON = %s", raw)
	}
}

// TestCoderTaskContentCarriesDesign pins the design-system flow: when a
// Design brief is attached to the step context it must ride the coder task
// JSON as plan_context.design, including the verbatim token block — this is
// the wire guarantee that every generated file shares one design system.
func TestCoderTaskContentCarriesDesign(t *testing.T) {
	cb := &design.CoderBrief{
		Style:     "Minimalism & Swiss Style",
		Pattern:   "Hero + Features + CTA",
		Sections:  []string{"Hero", "Features", "CTA"},
		TokensCSS: ":root {\n  --primary: #92400E;\n}\n",
	}
	cb.Fonts.Heading = "'Playfair Display', Georgia, serif"
	cb.Fonts.Body = "'Inter', system-ui, sans-serif"
	cb.Product = "Bakery/Cafe"

	raw := coderTaskContent(PlanStep{
		Action:   ActionCreate,
		FilePath: "public/styles.css",
	}, "", StepContext{Design: cb})

	var task struct {
		PlanContext *struct {
			Design *design.CoderBrief `json:"design"`
		} `json:"plan_context"`
	}
	if err := json.Unmarshal([]byte(raw), &task); err != nil {
		t.Fatalf("task JSON: %v\n%s", err, raw)
	}
	if task.PlanContext == nil || task.PlanContext.Design == nil {
		t.Fatalf("plan_context.design missing from coder task: %s", raw)
	}
	d := task.PlanContext.Design
	if d.Style != "Minimalism & Swiss Style" || d.Product != "Bakery/Cafe" {
		t.Errorf("design fields lost: %+v", d)
	}
	if !strings.Contains(d.TokensCSS, "--primary: #92400E") {
		t.Errorf("token block not carried verbatim: %q", d.TokensCSS)
	}
}

// TestCoderBriefCompactness: the distilled payload must stay well within
// the per-step prompt budget (it rides every coder call).
func TestCoderBriefCompactness(t *testing.T) {
	b, err := design.Brief(context.Background(), "build simple coffee landing")
	if err != nil {
		t.Fatalf("brief: %v", err)
	}
	cb := b.CoderBrief()
	if cb == nil {
		t.Fatal("nil coder brief")
	}
	raw, err := json.Marshal(cb)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	t.Logf("coder brief = %d bytes", len(raw))
	if len(raw) > 3500 {
		t.Errorf("coder brief too large: %d bytes", len(raw))
	}
	if !strings.Contains(string(raw), "--primary") {
		t.Error("token block missing from coder brief")
	}
	// The coder MUST see the exact same token block the planner planned
	// against — that identity is what makes every file share one design
	// system.
	if cb.TokensCSS != b.TokensCSS() {
		t.Error("coder brief token block differs from the planner's")
	}
}
