package stageinvoke

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/app"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/protocol/wire"
)

type scriptLLM struct {
	mu    sync.Mutex
	steps []func() (*llm.CompleteResponse, error)
}

func (s *scriptLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *scriptLLM) Complete(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.steps) == 0 {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "DONE"}}, nil
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	return step()
}

func writeStep(path, content string) func() (*llm.CompleteResponse, error) {
	return func() (*llm.CompleteResponse, error) {
		args := `{"path":"` + path + `","content":"` + content + `"}`
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "call-write", Type: "function",
			Function: llm.ToolCallFunc{Name: "write", Arguments: llm.ToolArguments(args)},
		}}}}, nil
	}
}

func newInvoker(t *testing.T, client llm.Client, events app.ChildEvents) (*Invoker, *tools.Runner) {
	t.Helper()
	root := t.TempDir()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	inv := New(Config{
		Cfg:       config.DefaultConfig(root),
		Skills:    []*skills.Skill{{Name: "writer", Body: "Write the file named in the task, then say DONE.", Tools: []string{"read", "write"}}},
		Client:    client,
		Validator: v,
		Runner:    tr,
		Events:    events,
	})
	return inv, tr
}

// A stage writes into a layer of its own: a stage that succeeds commits it
// into the workflow's turn, a stage that fails leaves nothing for the next
// attempt to trip over (ARCH-7).
func TestInvoke_AStagesEditsFollowItsOutcome(t *testing.T) {
	client := &scriptLLM{steps: []func() (*llm.CompleteResponse, error){writeStep("a.txt", "hello\\n")}}
	inv, tr := newInvoker(t, client, app.ChildEvents{})
	out, err := inv.Invoke(context.Background(), "writer", "write a.txt")
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out != "DONE" {
		t.Errorf("output = %q", out)
	}
	if got := tr.StagedFileContent(context.Background())["a.txt"]; got != "hello\n" {
		t.Fatalf("the turn does not carry the stage's edit: %q", got)
	}

	client = &scriptLLM{steps: []func() (*llm.CompleteResponse, error){
		writeStep("b.txt", "half\\n"),
		func() (*llm.CompleteResponse, error) { return nil, errors.New("the model refused") },
	}}
	inv, tr = newInvoker(t, client, app.ChildEvents{})
	if _, err := inv.Invoke(context.Background(), "writer", "write b.txt"); err == nil {
		t.Fatal("a stage whose model refused succeeded")
	}
	if got, ok := tr.StagedFileContent(context.Background())["b.txt"]; ok {
		t.Fatalf("a failed stage's edit reached the turn: %q", got)
	}
}

// The client sees a stage as a child: child_started, then child_done.
func TestInvoke_AStageReportsAsAChild(t *testing.T) {
	var mu sync.Mutex
	var kinds []string
	events := app.ChildEvents{Notify: func(ev wire.AgentEvent) {
		mu.Lock()
		kinds = append(kinds, ev.Type+":"+ev.SubagentType+":"+ev.Status)
		mu.Unlock()
	}}
	inv, _ := newInvoker(t, &scriptLLM{}, events)
	if _, err := inv.Invoke(context.Background(), "writer", "say done"); err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 2 || kinds[0] != "child_started:stage:writer:" || kinds[1] != "child_done:stage:writer:done" {
		t.Errorf("events = %v", kinds)
	}
}
