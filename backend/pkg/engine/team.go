package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"backend/pkg/agent/tools"
	"backend/pkg/models"
	"backend/pkg/skills"
)

// teamSkillPrompts carries the loaded role prompts the multi-agent team
// needs. The room resolves them from the registry at generation time so
// tests can inject fakes without touching the skills directory.
type teamSkillPrompts struct {
	coordinator string
	coder       string
	reviewer    string
}

// resolveTeamPrompts loads coordinator/coder/reviewer prompts. Planner
// prompt loading stays untouched (dual-model path); the team only takes
// over the execution + review stage after plan approval.
func resolveTeamPrompts(load func(role string) (skills.Prompt, error)) (teamSkillPrompts, error) {
	var out teamSkillPrompts
	for _, tc := range []struct {
		role string
		dst  *string
	}{
		{skills.RoleCoordinator, &out.coordinator},
		{skills.RoleCoder, &out.coder},
		{skills.RoleReviewer, &out.reviewer},
	} {
		p, err := load(tc.role)
		if err != nil {
			return teamSkillPrompts{}, fmt.Errorf("team: %s prompt: %w", tc.role, err)
		}
		if strings.TrimSpace(p.Text) == "" {
			return teamSkillPrompts{}, fmt.Errorf("team: %s prompt is empty", tc.role)
		}
		*tc.dst = p.Text
	}
	return out, nil
}

// canRunTeam reports whether the multi-agent team path is usable: a
// configured eino engine with a tool-calling model plus all three team
// prompts loaded.
func (r *ProjectRoom) canRunTeam(prompts teamSkillPrompts) bool {
	return r.eng != nil && r.eng.SupportsPlanExecute() &&
		prompts.coordinator != "" && prompts.coder != "" && prompts.reviewer != ""
}

// teamTools builds the per-role tool sets with strict least privilege.
func (r *ProjectRoom) teamTools() (coderTools, reviewerTools []tool.BaseTool, err error) {
	store := roomVFSStore{r: r}
	write, err := tools.NewVFSWriteTool(store)
	if err != nil {
		return nil, nil, err
	}
	read, err := tools.NewVFSReadTool(store)
	if err != nil {
		return nil, nil, err
	}
	list, err := tools.NewVFSListTool(store)
	if err != nil {
		return nil, nil, err
	}
	coderTools = []tool.BaseTool{
		&notifyWriteTool{InvokableTool: write, room: r},
		read,
		list,
	}
	rRead, err := tools.NewVFSReadTool(store)
	if err != nil {
		return nil, nil, err
	}
	rList, err := tools.NewVFSListTool(store)
	if err != nil {
		return nil, nil, err
	}
	return coderTools, []tool.BaseTool{rRead, rList}, nil
}

// runTeam executes the approved plan through the AgentAsTool team: a
// coordinator ChatModelAgent delegates to isolated coder/reviewer
// sub-agents (each with its own instruction + tool set + context).
func (r *ProjectRoom) runTeam(ctx context.Context, prompt, plan string, prompts teamSkillPrompts) error {
	mcm := r.eng.ChatModel()
	coderTools, reviewerTools, err := r.teamTools()
	if err != nil {
		return fmt.Errorf("team: build tools: %w", err)
	}
	vfsCtx := tools.WithVFSContext(ctx, r.chatID)
	r.filesWritten.Store(0)

	coder, err := adk.NewChatModelAgent(vfsCtx, &adk.ChatModelAgentConfig{
		Name:          "coder",
		Description:   "Writes ONE complete file per call via vfs_write given a path plus precise requirements.",
		Instruction:   prompts.coder,
		Model:         mcm,
		MaxIterations: 8,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig:    compose.ToolsNodeConfig{Tools: coderTools},
			EmitInternalEvents: true,
		},
	})
	if err != nil {
		return fmt.Errorf("team: coder agent: %w", err)
	}
	reviewer, err := adk.NewChatModelAgent(vfsCtx, &adk.ChatModelAgentConfig{
		Name:          "reviewer",
		Description:   "Read-only quality gate: reads files and returns APPROVE or REQUEST_CHANGES with concrete fixes. Never writes.",
		Instruction:   prompts.reviewer,
		Model:         mcm,
		MaxIterations: 6,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig:    compose.ToolsNodeConfig{Tools: reviewerTools},
			EmitInternalEvents: true,
		},
	})
	if err != nil {
		return fmt.Errorf("team: reviewer agent: %w", err)
	}
	coderTool := adk.NewAgentTool(vfsCtx, coder)
	reviewerTool := adk.NewAgentTool(vfsCtx, reviewer)

	coordinator, err := adk.NewChatModelAgent(vfsCtx, &adk.ChatModelAgentConfig{
		Name:          "coordinator",
		Description:   "Supervises the code-generation team: delegates file work to coder, quality-gates via reviewer.",
		Instruction:   prompts.coordinator,
		Model:         mcm,
		MaxIterations: 24,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig:    compose.ToolsNodeConfig{Tools: []tool.BaseTool{coderTool, reviewerTool}},
			EmitInternalEvents: true,
		},
	})
	if err != nil {
		return fmt.Errorf("team: coordinator agent: %w", err)
	}

	r.BroadcastMessage(models.TeamStarted{Type: "team_started"})
	runner := adk.NewRunner(vfsCtx, adk.RunnerConfig{
		Agent:           coordinator,
		EnableStreaming: true,
	})
	task := "Original user request:\n" + prompt +
		"\n\nApproved build plan (follow the steps in order, index.html last):\n" + plan +
		"\n\nDelegate each file step to coder now, then gate once via reviewer."
	iter := runner.Run(vfsCtx, []*schema.Message{schema.UserMessage(task)})
	debugLogEvent(r, "team_start", "plan_bytes", len(plan))

	var finalText strings.Builder
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			r.BroadcastMessage(models.TeamCompleted{
				Type:    "team_completed",
				Verdict: "error",
				Summary: ev.Err.Error(),
			})
			return fmt.Errorf("team: run: %w", ev.Err)
		}
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		mv := ev.Output.MessageOutput
		name := ev.AgentName
		if mv.IsStreaming {
			for {
				chunk, rerr := mv.MessageStream.Recv()
				if rerr != nil {
					break
				}
				if chunk == nil || chunk.Content == "" {
					continue
				}
				r.broadcastTeamToken(name, chunk.Content)
			}
			continue
		}
		if mv.Message == nil {
			continue
		}
		if mv.Message.Content != "" {
			r.broadcastTeamToken(name, mv.Message.Content)
			if name == "" || name == "coordinator" {
				finalText.WriteString(mv.Message.Content)
			}
		}
		for _, tc := range mv.Message.ToolCalls {
			r.BroadcastMessage(models.SubAgentActivity{
				Type:      "subagent_activity",
				AgentName: name,
				ToolName:  tc.Function.Name,
			})
		}
	}

	if written := r.filesWritten.Load(); written == 0 {
		r.BroadcastMessage(models.TeamCompleted{
			Type:    "team_completed",
			Verdict: "error",
			Summary: "run finished without writing any files",
		})
		return fmt.Errorf("team: run finished without writing any files")
	}

	verdict := "done"
	summary := strings.TrimSpace(finalText.String())
	if v, ok := parseReviewVerdict(summary); ok {
		verdict = v
	}
	r.BroadcastMessage(models.TeamCompleted{
		Type:    "team_completed",
		Verdict: verdict,
		Summary: truncateRunes(summary, 2000),
	})
	r.fillMissingReferencedFiles(ctx)
	return nil
}

// broadcastTeamToken forwards one team text delta with the sub-agent
// attribution prefix the frontend renders.
func (r *ProjectRoom) broadcastTeamToken(agentName, text string) {
	if text == "" {
		return
	}
	prefix := ""
	if agentName != "" && agentName != "coordinator" {
		prefix = "[" + agentName + "] "
	}
	r.BroadcastMessage(models.ConversationResponse{
		Type:           "conversation_response",
		ConversationID: "team-" + r.chatID,
		Message:        prefix + text,
		IsStreaming:    false,
	})
}

// parseReviewVerdict extracts the reviewer verdict from the closing
// summary. ok=false means no explicit verdict was found.
func parseReviewVerdict(summary string) (verdict string, ok bool) {
	upper := strings.ToUpper(summary)
	switch {
	case strings.Contains(upper, "APPROVE"):
		return "approve", true
	case strings.Contains(upper, "REQUEST_CHANGES"):
		return "request_changes", true
	default:
		return "", false
	}
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
