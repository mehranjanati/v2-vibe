package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"backend/pkg/llm"
	"backend/pkg/skills"
)

// StepFinish describes how the coder call ended. Truncated mirrors
// finish_reason == "length" — the signal the dual-model retry loop uses to
// re-generate cut-off files (A1) instead of leaving partial content in the VFS.
type StepFinish struct {
	FinishReason string
	Truncated    bool
}

// ExecuteStep runs ONE file task from the planner's ExecutionPlan through
// the coder model (@cf/qwen/qwen2.5-coder-32b-instruct) in SSE streaming
// mode.
//
// The coder skill prompt (backend/skills/02_coder.md) is loaded verbatim
// from the skills registry as the system message; the step is framed as
// the task JSON the skill's Input contract expects (path, action,
// requirements and — for modify tasks — existing_content). Every code
// delta the model streams is forwarded to outputChan as it arrives, so
// the caller can render the file live while it is being generated.
//
// The caller owns outputChan: it must consume the channel (typically from
// another goroutine) and may close it after ExecuteStep returns. Sending
// stops on ctx cancellation. The returned error is non-nil only when the
// call could not be completed (missing provider config, HTTP failure, or
// a stream error); a clean stream returns nil.
func ExecuteStep(ctx context.Context, step PlanStep, currentFileContent string, outputChan chan<- string) (StepFinish, error) {
	return ExecuteStepWithContext(ctx, step, currentFileContent, StepContext{}, outputChan)
}

// ExecuteStepWithContext is ExecuteStep plus the plan-level context the
// coder needs to write a file that belongs to THIS app: the user's original
// request, the plan goal/subtasks and the sibling step list.
//
// This is the fix for the "generated code has nothing to do with the
// prompt" failure. Without plan_context the coder's only view of the task
// is one step's `description` — a single technical sentence the planner
// writes per file ("Basic HTML5 doctype, lang, charset, and responsive
// viewport meta tag.") — so the user's domain never reaches the model and
// it emits generic boilerplate instead of the requested product.
func ExecuteStepWithContext(ctx context.Context, step PlanStep, currentFileContent string, planCtx StepContext, outputChan chan<- string) (StepFinish, error) {
	reg := skills.NewRegistry()
	prompt, err := reg.LoadRole(skills.RoleCoder)
	if err != nil {
		// The skill prompt (backend/skills/02_coder.md) is the contract:
		// fail loudly, never call the model without it.
		return StepFinish{}, fmt.Errorf("agent: coder: %w", err)
	}
	return executeStepWithPlan(ctx, prompt, llm.NewConfigFromEnv(), step, currentFileContent, planCtx, outputChan, prompt.Config.MaxTokens)
}

// ExecuteStepMaxTokens is ExecuteStep with an explicit token budget, used
// by the truncation-retry loop (A1): a file cut off by finish_reason=length
// is regenerated with a doubled budget.
func ExecuteStepMaxTokens(ctx context.Context, step PlanStep, currentFileContent string, outputChan chan<- string, maxTokens int) (StepFinish, error) {
	return ExecuteStepMaxTokensWithContext(ctx, step, currentFileContent, StepContext{}, outputChan, maxTokens)
}

// ExecuteStepMaxTokensWithContext combines the explicit token budget of
// ExecuteStepMaxTokens with the plan context of ExecuteStepWithContext.
func ExecuteStepMaxTokensWithContext(ctx context.Context, step PlanStep, currentFileContent string, planCtx StepContext, outputChan chan<- string, maxTokens int) (StepFinish, error) {
	reg := skills.NewRegistry()
	prompt, err := reg.LoadRole(skills.RoleCoder)
	if err != nil {
		return StepFinish{}, fmt.Errorf("agent: coder: %w", err)
	}
	return executeStepWithPlan(ctx, prompt, llm.NewConfigFromEnv(), step, currentFileContent, planCtx, outputChan, maxTokens)
}

// executeStepWith is ExecuteStep's injectable core: it takes the already
// loaded coder skill (verbatim system prompt + per-role model config) and
// the provider config, so tests can point the call at a fake gateway.
// The coder model comes from the role config (default
// @cf/qwen/qwen2.5-coder-32b-instruct, CODER_MODEL override win).
func executeStepWith(ctx context.Context, prompt skills.Prompt, cfg llm.Config, step PlanStep, currentFileContent string, outputChan chan<- string) (StepFinish, error) {
	return executeStepWithPlan(ctx, prompt, cfg, step, currentFileContent, StepContext{}, outputChan, prompt.Config.MaxTokens)
}

// executeStepWithPlan is executeStepWith plus the plan context.
func executeStepWithPlan(ctx context.Context, prompt skills.Prompt, cfg llm.Config, step PlanStep, currentFileContent string, planCtx StepContext, outputChan chan<- string, maxTokens int) (StepFinish, error) {
	return executeStepWithPlanMaxTokens(ctx, prompt, cfg, step, currentFileContent, planCtx, outputChan, maxTokens)
}

// executeStepWithPlanMaxTokens is the single coder call implementation:
// injectable skill + provider config (so tests can point it at a fake
// gateway), the plan context, and an explicit token budget used by the
// truncation-retry loop (A1) to re-emit a cut-off file with more room.
func executeStepWithPlanMaxTokens(ctx context.Context, prompt skills.Prompt, cfg llm.Config, step PlanStep, currentFileContent string, planCtx StepContext, outputChan chan<- string, maxTokens int) (StepFinish, error) {
	if cfg.GatewayURL == "" {
		return StepFinish{}, errors.New("agent: coder: no LLM provider configured (set AI_GATEWAY_URL or CLOUDFLARE_ACCOUNT_ID)")
	}
	if outputChan == nil {
		return StepFinish{}, errors.New("agent: coder: nil output channel")
	}

	client := llm.NewClient(cfg)
	ch, err := client.StreamChat(ctx, llm.ChatRequest{
		Model: prompt.Config.Model,
		Messages: []llm.ChatMessage{
			{Role: "system", Content: prompt.Text},
			{Role: "user", Content: coderTaskContent(step, currentFileContent, planCtx)},
		},
		Temperature: prompt.Config.Temperature,
		MaxTokens:   maxTokens,
	})
	if err != nil {
		return StepFinish{}, fmt.Errorf("agent: coder: stream: %w", err)
	}

	finish := StepFinish{}
	var total int
	for chunk := range ch {
		if chunk.Err != nil {
			return StepFinish{}, fmt.Errorf("agent: coder: stream error: %w", chunk.Err)
		}
		if chunk.Content != "" {
			select {
			case outputChan <- chunk.Content:
				total += len(chunk.Content)
			case <-ctx.Done():
				return StepFinish{}, ctx.Err()
			}
		}
		if chunk.Done {
			finish.FinishReason = chunk.FinishReason
			finish.Truncated = chunk.FinishReason == "length"
			log.Printf("[coder] file %q generated (%d bytes, finish=%s, model %s)",
				step.FilePath, total, chunk.FinishReason, prompt.Config.Model)
			return finish, nil
		}
	}
	return finish, nil
}

// coderTaskContent frames the human turn as the task JSON the coder skill
// (02_coder.md) mandates: path, action, requirements (the step's
// self-sufficient description), plan_context (the user's original request,
// the plan goal/subtasks and the sibling step list) and existing_content
// when the file already has content (the modify case). The coder's entire
// reply IS the file content, so the framing must be compact and
// unambiguous.
func coderTaskContent(step PlanStep, currentFileContent string, planCtx StepContext) string {
	requirements := step.Description
	if !planCtx.IsEmpty() {
		// Deliver design fragments per file: the markup fragments ride only
		// the index.html step, style rules only the stylesheet step, init
		// code only the script step. Other steps see the compact block
		// descriptors from the shared brief. The shared StepContext is
		// never mutated — each task gets its own shallow copy.
		ctx := planCtx
		if planCtx.Design != nil {
			if frags := planCtx.Design.FileFragments(step.FilePath); frags != nil {
				d := *planCtx.Design
				d.Blocks = frags
				ctx.Design = &d
				// The markup contract is deterministic: the page must
				// contain EVERY listed fragment, in order. Requirements
				// alone sometimes scope the file too narrowly (the model
				// then ships two sections and stops), so the full-skeleton
				// mandate is appended programmatically.
				lp := strings.ToLower(step.FilePath)
				if (strings.HasSuffix(lp, ".html") || strings.HasSuffix(lp, ".htm")) && len(frags) > 0 {
					requirements += fmt.Sprintf(
						" The file MUST contain EVERY %d section listed in plan_context.design.blocks (nav, hero, … footer), in that listed order, wrapped in one complete HTML document — no section may be omitted.",
						len(frags))
				}
			}
		}
		task := coderTask{
			Path:         step.FilePath,
			Action:       step.Action,
			Requirements: requirements,
			PlanContext:  &ctx,
		}
		if currentFileContent != "" {
			task.ExistingContent = currentFileContent
		}
		b, err := json.Marshal(task)
		if err != nil {
			// Task fields are plain strings; marshal cannot realistically
			// fail. Fall back to a plain-text framing rather than dropping
			// the task.
			return fmt.Sprintf("path: %s\naction: %s\nrequirements: %s",
				step.FilePath, step.Action, requirements)
		}
		return string(b)
	}
	task := coderTask{
		Path:         step.FilePath,
		Action:       step.Action,
		Requirements: requirements,
	}
	if currentFileContent != "" {
		task.ExistingContent = currentFileContent
	}
	b, err := json.Marshal(task)
	if err != nil {
		return fmt.Sprintf("path: %s\naction: %s\nrequirements: %s",
			step.FilePath, step.Action, requirements)
	}
	return string(b)
}

// coderTask mirrors the Input section of backend/skills/02_coder.md.
type coderTask struct {
	Path            string       `json:"path"`
	Action          string       `json:"action"`
	Requirements    string       `json:"requirements"`
	PlanContext     *StepContext `json:"plan_context,omitempty"`
	ExistingContent string       `json:"existing_content,omitempty"`
}
