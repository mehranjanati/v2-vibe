package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	agentplan "backend/agent"
	"backend/pkg/design"
	"backend/pkg/llm"
	"backend/pkg/models"
	"backend/pkg/skills"
)

// errPlanRejected marks the B7 gate outcome: the client explicitly
// rejected the proposed plan (plan_rejected). runGeneration must treat a
// rejection as final and NOT fall back to legacy generation.
var errPlanRejected = errors.New("dual-model: plan rejected")

// runDualModelPipeline is the R3 dual-model generation pipeline — the
// structured orchestrator for the planner→coder build flow:
//
//  1. agentplan.GeneratePlan — the planner model (skills/01_planner.md,
//     JSON mode) turns the user request + VFS snapshot into a validated
//     ExecutionPlan.
//  2. plan_structure broadcast — the machine-readable subtasks and steps
//     arrays are sent to the frontend for structured rendering, followed
//     by the human-readable plan_proposed and the B7 approval gate.
//  3. steps loop — every step streams through agentplan.ExecuteStep (the
//     coder model, skills/02_coder.md, SSE) and lands its file in the
//     Redis-backed VFS, broadcasting the standard file_* events.
//
// A nil return means the pipeline finished (including the no-steps case).
// errPlanRejected is returned when the client rejects the plan; any other
// error means the caller may fall back to the legacy generation paths.
func (r *ProjectRoom) runDualModelPipeline(ctx context.Context, prompt string) error {
	// Design direction: assemble the brand/design brief (pattern, style,
	// palette, typography, motion, a11y) ONCE from the user's request and
	// share it with the planner and every coder step. This is what turns
	// "welcome to our website" boilerplate into an on-brand, coherent app —
	// the planner plans the right sections and every file uses the same
	// colors, fonts and motion. Falls back to a neutral default if the
	// design catalog fails to load, so generation never blocks on it.
	designBrief, derr := design.Brief(ctx, prompt)
	if derr != nil {
		log.Printf("[room:%s] design brief unavailable, using neutral default: %v", r.chatID, derr)
	}

	// A3: the planner sees the real existing code, not just path+size — the
	// vector index (idx:vfs) is populated after every finalize and queried
	// here so iterate passes repair code they can actually see. The design
	// brief rides alongside so the plan follows the chosen section order
	// and palette instead of inventing a generic one.
	vfsCtx := r.plannerVFSContext(ctx, prompt)
	if designBrief != nil {
		vfsCtx = designBrief.RenderPlanner() + "\n\n" + vfsCtx
	}
	plan, err := agentplan.GeneratePlan(ctx, prompt, vfsCtx)
	if err != nil {
		return fmt.Errorf("dual-model: plan: %w", err)
	}
	log.Printf("[room:%s] plan generated: %d subtasks, %d steps",
		r.chatID, len(plan.Subtasks), len(plan.Steps))
	r.broadcastPlanStructure(plan)

	// B7 human-in-the-loop gate, identical to the proposePlan flow: show
	// the plan, then pause until the client approves — auto-approve on
	// timeout, abort on reject/cancel.
	display := plan.Render()
	convID := fmt.Sprintf("plan-%d", time.Now().UnixNano())
	r.BroadcastMessage(models.PlanProposed{
		Type:           "plan_proposed",
		ConversationID: convID,
		Plan:           display,
	})
	debugLogEvent(r, "plan_proposed", "conversation", convID, "bytes", len(display))
	if !r.waitForPlanApproval(ctx) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errPlanRejected
	}
	if err := r.appendChatMessage(ctx, "assistant", display); err != nil {
		log.Printf("[room:%s] failed to persist plan chat turn: %v", r.chatID, err)
	}

	if len(plan.Steps) == 0 {
		// A greenfield build (empty VFS) with zero steps is almost always
		// the planner's degenerate JSON-mode output ("steps": [] with full
		// reasoning). Left as-is it completes the run with no files and a
		// blank preview — the exact "empty plan / empty code / blank
		// preview" failure. Treat it as a planner failure so runGeneration
		// falls back to the legacy generation paths instead. For a
		// non-empty VFS an empty plan is a legitimate "no changes needed"
		// verdict, so that path keeps finalizing normally.
		if len(r.GetVFS()) == 0 {
			log.Printf("[room:%s] planner returned no steps for a greenfield build; failing to legacy fallback", r.chatID)
			return errors.New("dual-model: planner produced no steps for an empty VFS")
		}
		// The planner concluded no file changes are needed.
		log.Printf("[room:%s] plan has no steps; nothing to generate", r.chatID)
		r.BroadcastMessage(models.CFAgentStateEvent{
			Type:  "cf_agent_state",
			State: r.BuildAgentState(),
		})
		r.finalizeGeneration("", 0)
		return nil
	}

	// Execute the steps: prefer the multi-agent team (coordinator/coder/
	// reviewer via AgentAsTool) when its prompts + tool-calling model are
	// available; otherwise the single-coder per-step loop below.
	// stepCtx is the plan-level context every coder call carries: the
	// user's ORIGINAL request plus the plan's goal/subtasks/sibling steps.
	// A step's own description is a single technical sentence ("Basic HTML5
	// doctype, lang, charset…") that says nothing about the product, so
	// without this the coder emits generic boilerplate and the preview shows
	// an unrelated page instead of what the user asked for.
	stepCtx := agentplan.StepContextFrom(prompt, plan)
	if designBrief != nil {
		stepCtx.Design = designBrief.CoderBrief()
	}
	if r.canRunTeam(r.teamPrompts) {
		log.Printf("[room:%s] executing plan via multi-agent team", r.chatID)
		if terr := r.runTeam(ctx, prompt, display, r.teamPrompts); terr != nil {
			return fmt.Errorf("dual-model: team: %w", terr)
		}
		if written := int(r.filesWritten.Load()); written == 0 {
			r.BroadcastMessage(models.CFAgentStateEvent{
				Type:  "cf_agent_state",
				State: r.BuildAgentState(),
			})
		}
		r.finalizeGeneration("", int(r.filesWritten.Load()))
		return nil
	}
	r.filesWritten.Store(0)
	var truncatedPaths []string
	for i, step := range plan.Steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		log.Printf("[room:%s] executing step %d/%d: %s %s",
			r.chatID, i+1, len(plan.Steps), step.Action, step.FilePath)
		done, err := r.executeStepOnRoom(ctx, step, stepCtx)
		if err != nil {
			return fmt.Errorf("dual-model: step %d/%d: %w", i+1, len(plan.Steps), err)
		}
		if !done {
			// A1: finish_reason=length — the coder was cut off mid-file.
			truncatedPaths = append(truncatedPaths, step.FilePath)
		}
	}

	// A1: regenerate any file whose coder pass hit the token limit with a
	// doubled budget, then let gap-fill produce files the stream never
	// opened — the same recovery the legacy fence-streaming path runs.
	if len(truncatedPaths) > 0 {
		r.regenerateTruncatedSteps(ctx, plan, truncatedPaths, stepCtx)
	}
	r.fillMissingReferencedFiles(ctx)

	written := int(r.filesWritten.Load())
	if written == 0 {
		// A delete-only plan still needs a state sync so the frontend
		// sees the removals.
		r.BroadcastMessage(models.CFAgentStateEvent{
			Type:  "cf_agent_state",
			State: r.BuildAgentState(),
		})
	}
	r.finalizeGeneration("", written)
	return nil
}

// maxStepTruncateRetries bounds how many extra coder passes a truncated
// step gets before its partial file is dropped and handed to gap-fill.
const maxStepTruncateRetries = 2

// regenerateTruncatedSteps re-runs the coder for every step whose first
// pass hit finish_reason=length (A1). Each retry uses a doubled token
// budget. A step still truncated after all retries has its partial file
// deleted so the gap-fill pass regenerates it from the standard contract
// instead of leaving a broken file in the VFS (a cut-off index.html would
// otherwise blank the preview).
func (r *ProjectRoom) regenerateTruncatedSteps(ctx context.Context, plan agentplan.ExecutionPlan, truncated []string, stepCtx agentplan.StepContext) {
	baseTokens := coderMaxTokens()
	for _, path := range truncated {
		step, ok := findPlanStep(plan, path)
		if !ok {
			continue
		}
		// NOTE: `done` and `err` are declared HERE (outside the retry loop)
		// — Go's `x, y := f()` inside the loop would shadow them and the
		// post-loop check would always see the initial value.
		maxTokens := baseTokens * 2
		done := false
		for attempt := 1; attempt <= maxStepTruncateRetries; attempt++ {
			log.Printf("[room:%s] regenerating truncated %s (attempt %d/%d, maxTokens=%d)",
				r.chatID, path, attempt, maxStepTruncateRetries, maxTokens)
			if attempt > 1 {
				log.Printf("[room:%s] truncated %s still incomplete; retrying with doubled budget", r.chatID, path)
				maxTokens *= 2
			}
			var err error
			done, err = r.executeStepOnRoomBudget(ctx, step, stepCtx, maxTokens)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("[room:%s] truncation-retry for %s failed: %v", r.chatID, path, err)
				done = false
				break
			}
			if done {
				break
			}
		}
		if !done {
			// Drop the partial file: a truncated asset is worse than a
			// missing one — the gap-fill/stub passes below regenerate it
			// from the standard contract and keep the preview alive.
			log.Printf("[room:%s] %s still truncated after retries; dropping partial for gap-fill", r.chatID, path)
			r.DeleteFile(path)
			r.BroadcastMessage(models.FileDeleted{
				Type:     "file_deleted",
				FilePath: path,
			})
		}
	}
}

// findPlanStep returns the plan step targeting path, if any.
func findPlanStep(plan agentplan.ExecutionPlan, path string) (agentplan.PlanStep, bool) {
	for _, st := range plan.Steps {
		if st.FilePath == path {
			return st, true
		}
	}
	return agentplan.PlanStep{}, false
}

// coderMaxTokens returns the coder role's token budget from the skills
// registry (the budget ExecuteStep uses), so the truncation retry can
// double the same number. Falls back to the registry default of 8192.
func coderMaxTokens() int {
	reg := skills.NewRegistry()
	if cfg, ok := reg.Config(skills.RoleCoder); ok && cfg.MaxTokens > 0 {
		return cfg.MaxTokens
	}
	return 8192
}

// executeStepOnRoom runs ONE plan step end to end: read the current file
// content from the VFS (the modify case), stream the coder model through
// agentplan.ExecuteStep forwarding every delta as file_chunk_generated,
// then persist the final content to the VFS (in-memory map + Redis hash)
// and broadcast file_generated. Delete steps only remove the file — the
// frontend syncs removals via the cf_agent_state broadcast at completion.
// Returns done=false when the coder hit finish_reason=length mid-file
// (A1): the partial content is still salvaged to the VFS, and the caller
// is expected to regenerate the file with a doubled budget.
func (r *ProjectRoom) executeStepOnRoom(ctx context.Context, step agentplan.PlanStep, stepCtx agentplan.StepContext) (bool, error) {
	return r.executeStepOnRoomBudget(ctx, step, stepCtx, 0)
}

// executeStepOnRoomBudget is executeStepOnRoom with an explicit token
// budget override (>0) used by the truncation-retry loop; 0 means "use the
// coder role's configured MaxTokens".
func (r *ProjectRoom) executeStepOnRoomBudget(ctx context.Context, step agentplan.PlanStep, stepCtx agentplan.StepContext, maxTokens int) (bool, error) {
	if step.Action == agentplan.ActionDelete {
		debugLogEvent(r, "file_deleted", "path", step.FilePath)
		r.DeleteFile(step.FilePath)
		r.BroadcastMessage(models.FileDeleted{
			Type:     "file_deleted",
			FilePath: step.FilePath,
		})
		return true, nil
	}

	current := ""
	if entry, ok := r.GetVFS()[step.FilePath]; ok && entry != nil {
		current = entry.FileContents
	}

	// Steps run sequentially, so the VFS already holds every file an
	// earlier step produced. Handing the coder those real contents is what
	// lets styles.css use the class names index.html actually emitted and
	// main.js bind the ids that really exist — otherwise the model guesses
	// selectors and the pieces silently fail to connect.
	stepCtx = stepCtx.WithRelatedFiles(step.FilePath, r.siblingFileContents(stepCtx))

	debugLogEvent(r, "file_generating", "path", step.FilePath)
	r.BroadcastMessage(modelsFileGenerating(step.FilePath))

	// ExecuteStep pushes every generated delta; drain it concurrently so
	// the UI renders the file live while the model is still writing it.
	output := make(chan string)
	var collected strings.Builder
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for chunk := range output {
			collected.WriteString(chunk)
			debugLogEvent(r, "file_chunk_generated", "path", step.FilePath, "bytes", len(chunk))
			r.BroadcastMessage(modelsFileChunk(step.FilePath, chunk))
		}
	}()

	var finish agentplan.StepFinish
	var err error
	if maxTokens > 0 {
		finish, err = agentplan.ExecuteStepMaxTokensWithContext(ctx, step, current, stepCtx, output, maxTokens)
	} else {
		finish, err = agentplan.ExecuteStepWithContext(ctx, step, current, stepCtx, output)
	}
	close(output)
	wg.Wait()
	if err != nil {
		return false, fmt.Errorf("execute step %q: %w", step.FilePath, err)
	}

	content := coderFileContent(step.FilePath, collected.String())
	r.UpsertFile(step.FilePath, content)
	r.BroadcastMessage(modelsFileGenerated(step.FilePath, content))
	r.filesWritten.Add(1)
	debugLogEvent(r, "file_generated", "path", step.FilePath, "bytes", len(content))
	return !finish.Truncated, nil
}

// siblingFileContents collects the real contents of the plan's sibling
// files that already exist in the VFS — i.e. everything an earlier step
// wrote during this run, plus any pre-existing planned file.
//
// The result feeds StepContext.WithRelatedFiles so a later step can match
// the markup/styles its siblings actually produced. Only planned paths are
// considered: unrelated project files (a workflow.json, generated output
// logs) would just consume the coder's context budget.
func (r *ProjectRoom) siblingFileContents(stepCtx agentplan.StepContext) map[string]string {
	if len(stepCtx.Steps) == 0 {
		return nil
	}
	files := r.GetVFS()
	out := make(map[string]string, len(stepCtx.Steps))
	for _, st := range stepCtx.Steps {
		entry, ok := files[st.FilePath]
		if !ok || entry == nil || entry.FileContents == "" {
			continue
		}
		out[st.FilePath] = entry.FileContents
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// coderFileContent normalizes the coder's raw reply before it reaches the
// VFS: a single wrapped markdown fence (the model drifting from the
// no-fence contract) is stripped, JavaScript gets the shared syntax
// repair, and the file ends with exactly one trailing newline per the
// skill's output contract.
func coderFileContent(path, raw string) string {
	content := raw
	if stripped, ok := stripSingleFence(content); ok {
		content = stripped
	}
	if llm.LooksLikeJS(path) {
		content = llm.SanitizeJS(content)
	}
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return ""
	}
	return content + "\n"
}

// stripSingleFence removes one wrapping markdown fence ("```<info>\n" …
// "\n```"). Unlike llm.ParseFileBlocks it does NOT require the fence info
// string to look like a file path — the coder is contracted to emit raw
// content only, so any info tag (html, js, …) is drift to repair, not a
// separate block. Returns ok=false when s is not a single fenced block.
func stripSingleFence(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s, false
	}
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		return s, false // opener line not closed yet
	}
	body := s[nl+1:]
	stripped := strings.TrimSuffix(body, "```")
	if stripped == body {
		// No closing fence. Two cases: a stream truncated right after the
		// opener (body is still partial content) or a trailing "```" that
		// was consumed by a partial-fence guard. Either way the fence
		// opener is drift — break it and keep the body so the truncated
		// content still lands in the VFS (A5). The truncation marker for
		// the A1 retry loop comes from the gap-fill pass on top of this.
		return strings.TrimRight(body, "\n"), true
	}
	return strings.TrimRight(stripped, "\n"), true
}

// vfsSnapshot renders the room's VFS as the compact per-file listing the
// planner prompt expects as the current state: one "path (N bytes)" line
// per file, or "(empty)" for a fresh project.
func (r *ProjectRoom) vfsSnapshot() string {
	files := r.GetVFS()
	if len(files) == 0 {
		return "(empty)"
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b strings.Builder
	for _, p := range paths {
		size := 0
		if files[p] != nil {
			size = len(files[p].FileContents)
		}
		fmt.Fprintf(&b, "%s (%d bytes)\n", p, size)
	}
	return strings.TrimRight(b.String(), "\n")
}

// plannerVFSContext is the planner's view of the project: the compact VFS
// snapshot plus the RAG retriever's "Relevant existing code context" for
// the user's request (A3). The vector index is populated after every
// finalized generation, so iterative requests get the real code — not just
// path+size — and can produce plans that repair the existing app.
func (r *ProjectRoom) plannerVFSContext(ctx context.Context, prompt string) string {
	snap := r.vfsSnapshot()
	rag := r.BuildRAGContext(ctx, prompt, 6)
	if rag == "" {
		return snap
	}
	return snap + "\n\n" + rag
}

// broadcastPlanStructure sends the plan's machine-readable arrays —
// subtasks plus the ordered per-file steps — as one plan_structure event
// so the frontend can render the structured plan view.
func (r *ProjectRoom) broadcastPlanStructure(plan agentplan.ExecutionPlan) {
	steps := make([]models.PlanStructureStep, 0, len(plan.Steps))
	for _, st := range plan.Steps {
		steps = append(steps, models.PlanStructureStep{
			Action:                 st.Action,
			FilePath:               st.FilePath,
			Description:            st.Description,
			AssociatedSubtaskIndex: st.AssociatedSubtaskIndex,
		})
	}
	debugLogEvent(r, "plan_structure", "subtasks", len(plan.Subtasks), "steps", len(steps))
	r.BroadcastMessage(models.PlanStructure{
		Type:     "plan_structure",
		Goal:     plan.Goal,
		Subtasks: plan.Subtasks,
		Steps:    steps,
	})
}
