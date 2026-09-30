package engine

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"backend/pkg/agent"
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
// The coder tools (incl. the vfs_write path through roomVFSStore) write with
// the "coder" author, so agent file writes are attributed to the coder role.
func (r *ProjectRoom) teamTools() (coderTools, reviewerTools []tool.BaseTool, err error) {
	store := roomVFSStore{r: r, author: writeAuthorCoder}
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

// runTeam executes the approved plan through the DeepAgent team: a
// coordinator DeepAgent delegates to isolated coder/reviewer
// sub-agents (each with its own model, instruction + tool set + context).
//
// P1.3.1 lineage hook: the returned verdict is the run's REAL verdict — the
// reviewer's `approve`/`request_changes` when the closing summary carries one,
// `done` otherwise — and is what the caller records in the generation lineage
// (`runDualModelPipeline` → FinishGenerationRecord). It is always non-empty
// when err is nil.
//
// runTeam deliberately does NOT open its own lineage record: it is nested
// inside the run whose record runDualModelPipeline already opened (its only
// call site is there), so a second Start/Finish pair here would double-count
// one generation — two `generations` rows, two baselines — and break the
// parent chain. The team's lineage contribution is the verdict it returns.
func (r *ProjectRoom) runTeam(ctx context.Context, prompt, plan string, prompts teamSkillPrompts) (string, error) {
	coderTools, reviewerTools, err := r.teamTools()
	if err != nil {
		return "", fmt.Errorf("team: build tools: %w", err)
	}
	vfsCtx := tools.WithVFSContext(ctx, r.chatID)
	r.filesWritten.Store(0)

	// Per-role models: the coder runs on the coder model/temperature/budget
	// from the skills registry and the reviewer on its own, instead of all
	// three roles sharing one engine model. roleModel degrades to that engine
	// model on any failure, so a typo'd CODER_MODEL can never kill the run.
	reg := skills.NewRegistry()
	coderModel := r.roleModel(vfsCtx, reg, skills.RoleCoder)
	reviewerModel := r.roleModel(vfsCtx, reg, skills.RoleReviewer)
	coordinatorModel := r.roleModel(vfsCtx, reg, skills.RoleCoordinator)

	coder, err := adk.NewChatModelAgent(vfsCtx, &adk.ChatModelAgentConfig{
		Name:          "coder",
		Description:   "Writes ONE complete file per call via vfs_write given a path plus precise requirements.",
		Instruction:   prompts.coder,
		Model:         coderModel,
		GenModelInput: noFormatInstruction,
		MaxIterations: 8,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig:    compose.ToolsNodeConfig{Tools: coderTools},
			EmitInternalEvents: true,
		},
	})
	if err != nil {
		return "", fmt.Errorf("team: coder agent: %w", err)
	}
	reviewer, err := adk.NewChatModelAgent(vfsCtx, &adk.ChatModelAgentConfig{
		Name:          "reviewer",
		Description:   "Read-only quality gate: reads files and returns APPROVE or REQUEST_CHANGES with concrete fixes. Never writes.",
		Instruction:   prompts.reviewer,
		Model:         reviewerModel,
		GenModelInput: noFormatInstruction,
		MaxIterations: 6,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig:    compose.ToolsNodeConfig{Tools: reviewerTools},
			EmitInternalEvents: true,
		},
	})
	if err != nil {
		return "", fmt.Errorf("team: reviewer agent: %w", err)
	}

	// The coordinator is Eino's DeepAgent prebuilt rather than a hand-built
	// ChatModelAgent with two adk.NewAgentTool wrappers. That is the shape
	// CloudWeGo recommends for multi-agent systems: the module source marks
	// adk/prebuilt/supervisor, the workflow agents and deterministic agent
	// transfer NOT RECOMMENDED ("agent transfer with full context sharing
	// between agents has not proven to be more effective empirically.
	// Consider using ChatModelAgent with AgentTool or DeepAgent instead").
	// DeepAgent is the agent-as-tool shape we already had, plus:
	//
	//   - coder/reviewer become SubAgents reached through the built-in `task`
	//     tool (task{subagent_type, description}); each keeps its own
	//     instruction, tool set and context, exactly as before;
	//   - the built-in write_todos handler gives the coordinator a plan
	//     checklist without us shipping a tool for it;
	//   - deep supplies its own GenModelInput, which — like noFormatInstruction
	//     below — never runs the ADK default's FString pass over the prompt.
	//
	// WithoutGeneralSubAgent keeps the delegation surface to exactly the two
	// roles we defined. MaxIteration sits above the old 24 because a deep run
	// now spends turns on write_todos in addition to delegating.
	coordinator, err := deep.New(vfsCtx, &deep.Config{
		Name:                   "coordinator",
		Description:            "Supervises the code-generation team: delegates file work to coder, quality-gates via reviewer.",
		ChatModel:              coordinatorModel,
		Instruction:            prompts.coordinator + teamToolContract,
		SubAgents:              []adk.Agent{coder, reviewer},
		MaxIteration:           30,
		WithoutGeneralSubAgent: true,
		ToolsConfig: adk.ToolsConfig{
			EmitInternalEvents: true,
		},
	})
	if err != nil {
		return "", fmt.Errorf("team: coordinator agent: %w", err)
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
			return "", fmt.Errorf("team: run: %w", ev.Err)
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
		return "", fmt.Errorf("team: run finished without writing any files")
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
	return verdict, nil
}

// roleModel resolves one team role's model from the skills registry and
// builds it against the engine's provider endpoint.
//
// It degrades to the engine model — never an error — when the role is
// unregistered or the provider rejects the configuration: losing a whole
// team run to a typo'd CODER_MODEL override would be far worse than running
// that one role on the default model.
//
// The return type is model.BaseChatModel (BaseModel[*schema.Message]) because
// that is all the consumers need — adk.ChatModelAgentConfig.Model and
// deep.Config.ChatModel — while Engine.ChatModel() (ToolCallingChatModel) and
// Engine.NewRoleModel() (ChatModel) are both assignable to it.
func (r *ProjectRoom) roleModel(ctx context.Context, reg skills.Registry, role string) model.BaseChatModel {
	fallback := r.eng.ChatModel()
	cfg, ok := reg.Config(role)
	if !ok {
		return fallback
	}
	rm, err := r.eng.NewRoleModel(ctx, agent.RoleModelConfig{
		Model:       cfg.Model,
		MaxTokens:   cfg.MaxTokens,
		Temperature: cfg.Temperature,
	})
	if err != nil {
		log.Printf("[room:%s] team: %s model unavailable, using the engine model: %v", r.chatID, role, err)
		return fallback
	}
	return rm
}

// noFormatInstruction is the GenModelInput for the team's sub-agents: it
// returns the role's system instruction verbatim.
//
// This is REQUIRED, not just defensive, once the coordinator is a DeepAgent.
// The ADK default (adk.defaultGenModelInput) pushes the instruction through
// prompt.FromMessages(schema.FString, ...) as soon as ANY session value is
// present, and hard-fails the entire run when the text contains literal curly
// braces. The DeepAgent coordinator's built-in write_todos handler calls
// adk.AddSessionValue(ctx, SessionKeyTodos, ...) (deep.go), and the task tool
// runs every sub-agent with withSharedParentSession() (agent_tool.go) — so the
// sub-agents DO see that session value. skills/02_coder.md contains
// `if (el) { ... }` and skills/01_planner.md a full JSON example, either of
// which would therefore break FString parsing and kill the run.
//
// No team role templates session values, so verbatim is both safe and exactly
// what the prompts were written for. (DeepAgent's coordinator gets an
// equivalent from adk/prebuilt/deep/typedGenModelInput.)
func noFormatInstruction(_ context.Context, instruction string, input *adk.AgentInput) ([]adk.Message, error) {
	msgs := make([]adk.Message, 0, len(input.Messages)+1)
	if instruction != "" {
		msgs = append(msgs, schema.SystemMessage(instruction))
	}
	msgs = append(msgs, input.Messages...)
	return msgs, nil
}

// teamToolContract is appended to the coordinator role prompt. The role
// prompt describes WHO delegates to whom; this adds the mechanical HOW for
// the DeepAgent runtime, whose delegation tool is the built-in `task` tool
// rather than a per-role agent tool named after the role. The sub-agent names
// themselves are unchanged (coder, reviewer) — they are now the
// `subagent_type` values.
//
// The last two bullets are deliberate OVERRIDES of deep's built-in task
// prompt (adk/prebuilt/deep/prompt.go), which is written for a generic
// orchestrator and advises parallelizing independent tasks. The build steps
// are dependency-ordered (data -> store -> feature -> UI -> index.html), so
// parallel coder calls would write a consumer before its dependency. That
// advice has to be contradicted explicitly or it wins by being more recent in
// the context.
const teamToolContract = `

## Tool contract (DeepAgent runtime)

- Delegate with the built-in "task" tool:
  task{subagent_type: "coder", description: "<the full per-file brief>"}.
  The only valid subagent_type values are "coder" and "reviewer".
- Track progress with the built-in "write_todos" tool: mark a file step
  in_progress before its coder call and completed once the file lands.
- You have no file tools yourself — every file operation goes through "task".
- OVERRIDE — do NOT parallelize: issue exactly ONE "task" call per turn and
  wait for its result before the next. The plan's steps are dependency-ordered
  (data -> store/state -> feature logic -> UI -> entry point, index.html
  last); running two coder calls at once would let a file be written before
  the file it depends on.
- One file per "task" call. Never bundle two files into one description.
`

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
