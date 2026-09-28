package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"backend/pkg/agent/tools"
	"backend/pkg/llm"
	"backend/pkg/models"
)

// ---------- VFS adapter: room as a tools.VFSStore ----------

// roomVFSStore adapts the ProjectRoom's in-memory + Redis VFS to the
// tools.VFSStore interface so the planexecute executor can write files
// with the vfs_write tool. chatID arguments are ignored: the room IS the
// scope.
type roomVFSStore struct{ r *ProjectRoom }

func (s roomVFSStore) VFSWrite(_ context.Context, _, path, content string) error {
	s.r.UpsertFile(path, content)
	return nil
}

func (s roomVFSStore) VFSRead(_ context.Context, _, path string) (string, bool, error) {
	s.r.vfsMu.RLock()
	defer s.r.vfsMu.RUnlock()
	entry, ok := s.r.vfs[path]
	if !ok {
		return "", false, nil
	}
	return entry.FileContents, true, nil
}

func (s roomVFSStore) VFSDelete(_ context.Context, _, path string) error {
	s.r.DeleteFile(path)
	return nil
}

func (s roomVFSStore) VFSList(_ context.Context, _ string) ([]string, error) {
	s.r.vfsMu.RLock()
	defer s.r.vfsMu.RUnlock()
	paths := make([]string, 0, len(s.r.vfs))
	for p := range s.r.vfs {
		paths = append(paths, p)
	}
	return paths, nil
}

// ---------- notify decorator: tool writes become file events ----------

// notifyWriteTool wraps an invokable tool and, on success, replays the
// write as the standard file_generating / file_generated WS events so the
// frontend sees tool-driven writes exactly like streamed ones.
type notifyWriteTool struct {
	tool.InvokableTool
	room *ProjectRoom
}

func (t *notifyWriteTool) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	_ = json.Unmarshal([]byte(args), &in)
	if in.Path != "" {
		t.room.BroadcastMessage(models.FileGenerating{
			Type:     "file_generating",
			FilePath: in.Path,
		})
	}
	out, err := t.InvokableTool.InvokableRun(ctx, args, opts...)
	if err != nil {
		return out, err
	}
	if in.Path != "" {
		full := in.Content
		if entry, ok := t.room.GetVFS()[in.Path]; ok {
			full = entry.FileContents
		}
		if llm.LooksLikeJS(in.Path) {
			// Repair syntax slips in tool-written JS too.
			full = llm.SanitizeJS(full)
			_ = t.room.persistSanitized(in.Path, full)
		}
		t.room.BroadcastMessage(models.FileGenerated{
			Type: "file_generated",
			File: &models.FileEntry{FilePath: in.Path, FileContents: full},
		})
		t.room.filesWritten.Add(1)
	}
	return out, nil
}

// persistSanitized re-persists a file after sanitization (the tool already
// wrote the raw version).
func (r *ProjectRoom) persistSanitized(path, content string) error {
	r.UpsertFile(path, content)
	return nil
}

// planExecuteTools builds the executor tool set: vfs_write (wrapped with
// file-event notifications), vfs_read, vfs_list.
func (r *ProjectRoom) planExecuteTools() ([]tool.BaseTool, error) {
	store := roomVFSStore{r: r}
	write, err := tools.NewVFSWriteTool(store)
	if err != nil {
		return nil, err
	}
	read, err := tools.NewVFSReadTool(store)
	if err != nil {
		return nil, err
	}
	list, err := tools.NewVFSListTool(store)
	if err != nil {
		return nil, err
	}
	return []tool.BaseTool{
		&notifyWriteTool{InvokableTool: write, room: r},
		read,
		list,
	}, nil
}

// ---------- plan-execute-replan run ----------

// runPlanExecute runs the eino planexecute prebuilt over the approved
// plan (B5): the planner turns the approved markdown plan into structured
// steps, the executor implements each step with vfs tools (file events
// stream to the client as writes land), and the replanner revises
// remaining steps until the work is done or MaxIterations is exhausted.
func (r *ProjectRoom) runPlanExecute(ctx context.Context, prompt, plan string) error {
	mcm := r.eng.ChatModel()
	execTools, err := r.planExecuteTools()
	if err != nil {
		return fmt.Errorf("planexecute: build tools: %w", err)
	}
	r.filesWritten.Store(0)

	planner, err := planexecute.NewPlanner(ctx, &planexecute.PlannerConfig{
		ToolCallingChatModel: mcm,
		GenInputFn: func(ctx context.Context, _ []adk.Message) ([]adk.Message, error) {
			return []adk.Message{
				schema.UserMessage("Original user request:\n" + prompt +
					"\n\nApproved build plan:\n" + plan +
					"\n\nProduce the ordered implementation plan as JSON steps."),
			}, nil
		},
	})
	if err != nil {
		return fmt.Errorf("planexecute: planner: %w", err)
	}

	executor, err := planexecute.NewExecutor(ctx, &planexecute.ExecutorConfig{
		Model: mcm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: execTools},
		},
		MaxIterations: 8,
		GenInputFn: func(ctx context.Context, in *planexecute.ExecutionContext) ([]adk.Message, error) {
			return []adk.Message{
				schema.UserMessage("Original user request:\n" + prompt +
					"\n\nApproved build plan:\n" + plan +
					"\n\nPlan state (remaining steps, then executed):\n" + planStepsSummary(in.Plan, in.ExecutedSteps) +
					"\n\nImplement the NEXT remaining step now. Write every file with the " +
					"vfs_write tool (FULL file contents, paths like public/index.html). " +
					"Do not print files as text."),
			}, nil
		},
	})
	if err != nil {
		return fmt.Errorf("planexecute: executor: %w", err)
	}

	replanner, err := planexecute.NewReplanner(ctx, &planexecute.ReplannerConfig{
		ChatModel: mcm,
		GenInputFn: func(ctx context.Context, in *planexecute.ExecutionContext) ([]adk.Message, error) {
			return []adk.Message{
				schema.UserMessage("Original user request:\n" + prompt +
					"\n\nApproved build plan:\n" + plan +
					"\n\nExecuted steps so far:\n" + planStepsSummary(in.Plan, in.ExecutedSteps) +
					"\n\nIf the app is complete, respond via the respond tool. " +
					"Otherwise output the REMAINING steps via the plan tool."),
			}, nil
		},
	})
	if err != nil {
		return fmt.Errorf("planexecute: replanner: %w", err)
	}

	agent, err := planexecute.New(ctx, &planexecute.Config{
		Planner:       planner,
		Executor:      executor,
		Replanner:     replanner,
		MaxIterations: 4,
	})
	if err != nil {
		return fmt.Errorf("planexecute: build: %w", err)
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
	})
	iter := runner.Run(ctx, []*schema.Message{schema.UserMessage(prompt + "\n\nApproved plan:\n" + plan)})

	execConvID := "planexec-" + r.chatID
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			return fmt.Errorf("planexecute: run: %w", ev.Err)
		}
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		mv := ev.Output.MessageOutput
		if mv.IsStreaming {
			for {
				chunk, rerr := mv.MessageStream.Recv()
				if rerr != nil {
					break // io.EOF or closed stream
				}
				if chunk == nil || chunk.Content == "" {
					continue
				}
				r.BroadcastMessage(models.ConversationResponse{
					Type:           "conversation_response",
					ConversationID: execConvID,
					Message:        chunk.Content,
					IsStreaming:    true,
				})
			}
		} else if mv.Message != nil && mv.Message.Content != "" {
			r.BroadcastMessage(models.ConversationResponse{
				Type:           "conversation_response",
				ConversationID: execConvID,
				Message:        mv.Message.Content,
				IsStreaming:    false,
			})
		}
	}

	// A "successful" run that wrote no files is a silent failure: the
	// malformed-chunk tolerance can make the ADK graph end "cleanly" with
	// no tool calls actually landing. Treat it as an error so the caller
	// falls back to the raw streaming path (which ignores bad frames and
	// keeps streaming).
	if written := r.filesWritten.Load(); written == 0 {
		return fmt.Errorf("planexecute: run finished without writing any files")
	}

	// A1: the executor's tool writes can be cut off by the model's token
	// limit before every referenced asset lands (a missing main.js blanks
	// the preview). Run the same gap-fill recovery the fence-streaming and
	// dual-model paths use so referenced-but-missing files still get
	// generated (and stubs cover the rest) before finalize.
	r.fillMissingReferencedFiles(ctx)
	return nil
}

// planStepsSummary renders remaining vs executed steps for prompts. The
// default Plan implementation marshals to {"steps":[...]} so we use its
// JSON form; executed steps are plain strings.
func planStepsSummary(p planexecute.Plan, executed []planexecute.ExecutedStep) string {
	var b strings.Builder
	if p != nil {
		if raw, err := json.Marshal(p); err == nil {
			b.WriteString(string(raw))
			b.WriteString("\n")
		}
	}
	if len(executed) > 0 {
		b.WriteString("\nAlready executed:\n")
		for _, es := range executed {
			b.WriteString("- " + es.Step + "\n")
		}
	}
	return b.String()
}
