package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// budgetRecordingModel is a ChatModel that records the max_tokens budget the ADK
// applies to every call and answers with one token, so a full step runs without
// a provider. Both entry points record, because which one the agent uses is an
// ADK implementation detail — the budget must arrive either way.
type budgetRecordingModel struct {
	mu      sync.Mutex
	budgets []int
	calls   int
}

func (m *budgetRecordingModel) record(opts []model.Option) {
	o := model.GetCommonOptions(&model.Options{}, opts...)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if o.MaxTokens == nil {
		m.budgets = append(m.budgets, 0)
		return
	}
	m.budgets = append(m.budgets, *o.MaxTokens)
}

func (m *budgetRecordingModel) callBudgets() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]int(nil), m.budgets...)
}

func (m *budgetRecordingModel) Generate(_ context.Context, _ []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.record(opts)
	return &schema.Message{Role: schema.Assistant, Content: "ok"}, nil
}

func (m *budgetRecordingModel) Stream(_ context.Context, _ []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.record(opts)
	sr, sw := schema.Pipe[*schema.Message](1)
	sw.Send(&schema.Message{Role: schema.Assistant, Content: "ok"}, nil)
	sw.Close()
	return sr, nil
}

func (m *budgetRecordingModel) BindTools([]*schema.ToolInfo) error { return nil }

// TestRunStepOptionsCarryTheBudget pins the option shape: no budget means no run
// options at all (pre-P1.11 behaviour), a budget means exactly one
// WithChatModelOptions carrying model.WithMaxTokens.
func TestRunStepOptionsCarryTheBudget(t *testing.T) {
	if got := runStepOptions(0); got != nil {
		t.Fatalf("runStepOptions(0) = %v, want nil (provider default)", got)
	}
	if got := runStepOptions(-1); got != nil {
		t.Fatalf("runStepOptions(-1) = %v, want nil", got)
	}
	if got := runStepOptions(12288); len(got) != 1 {
		t.Fatalf("runStepOptions(12288) returned %d options, want 1", len(got))
	}
}

// TestRunStepWithBudgetReachesTheModel is the P1.11 acceptance case: the budget
// the room passes per call (and doubles on a truncation retry) actually reaches
// the model as max_tokens, and a plain RunStepWith still sends no budget.
func TestRunStepWithBudgetReachesTheModel(t *testing.T) {
	fake := &budgetRecordingModel{}
	eng := &Engine{chatModel: fake, cfg: EngineConfig{Instruction: "sys", MaxIterations: 1}}
	ctx := context.Background()

	if _, err := eng.RunStepWithBudget(ctx, "sys", nil, "build me a page", nil, 12288, nil); err != nil {
		t.Fatalf("step with budget 12288: %v", err)
	}
	// The truncation-retry path doubles the budget (room.go) — the second call
	// must carry the doubled value, not the exhausted one.
	if _, err := eng.RunStepWithBudget(ctx, "sys", nil, "re-emit the cut files", nil, 24576, nil); err != nil {
		t.Fatalf("retry step with budget 24576: %v", err)
	}
	if _, err := eng.RunStepWith(ctx, "sys", nil, "no budget", nil, nil); err != nil {
		t.Fatalf("step without budget: %v", err)
	}

	got := fake.callBudgets()
	want := []int{12288, 24576, 0}
	if len(got) != len(want) {
		t.Fatalf("model saw %d calls (%v), want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("call %d budget = %d, want %d (all: %v)", i+1, got[i], want[i], got)
		}
	}
}
