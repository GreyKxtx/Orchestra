package skillrun

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// scriptLLM answers each call with the next step of its script.
type scriptLLM struct {
	mu    sync.Mutex
	steps []func() (*llm.CompleteResponse, error)
}

func (s *scriptLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *scriptLLM) Complete(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.steps) == 0 {
		return finalStep("done")()
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

func finalStep(summary string) func() (*llm.CompleteResponse, error) {
	return func() (*llm.CompleteResponse, error) {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant,
			Content: `{"type":"final","final":{"summary":"` + summary + `","patches":[]}}`}}, nil
	}
}

func failStep() func() (*llm.CompleteResponse, error) {
	return func() (*llm.CompleteResponse, error) { return nil, errors.New("the model refused") }
}

func newSkillRunner(t *testing.T, client llm.Client) (*Runner, *tools.Runner) {
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
	r := New(Config{
		Cfg:       config.DefaultConfig(root),
		Skills:    []*skills.Skill{{Name: "writer", Description: "writes a file", Body: "Write the file named in the task.", Tools: []string{"read", "write"}}},
		Client:    client,
		Validator: v,
		Runner:    tr,
		MaxSteps:  4,
	})
	return r, tr
}

// A skill's child writes into a layer of its own: when it succeeds the edit
// reaches the turn, and the parent reads the file's name and the child's
// closing words.
func TestRun_ASuccessfulSkillsEditReachesTheTurn(t *testing.T) {
	client := &scriptLLM{steps: []func() (*llm.CompleteResponse, error){writeStep("a.txt", "hello\\n"), finalStep("wrote a.txt")}}
	r, tr := newSkillRunner(t, client)
	out, err := r.Run(context.Background(), "writer", "write a.txt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := tr.StagedFileContent(context.Background())["a.txt"]; got != "hello\n" {
		t.Fatalf("the turn does not carry the skill's edit: %q", got)
	}
	if out.Text != "wrote a.txt" {
		t.Errorf("Text = %q", out.Text)
	}
	if !strings.Contains(out.Report, "a.txt") || !strings.Contains(out.Report, "wrote a.txt") {
		t.Errorf("the parent's report names neither the file nor the closing words:\n%s", out.Report)
	}
}

// A skill's child that fails takes its edits with it (ARCH-7): the parent
// used to find the half-done write in its own view, as if it had made it.
func TestRun_AFailedSkillsEditsAreDropped(t *testing.T) {
	client := &scriptLLM{steps: []func() (*llm.CompleteResponse, error){writeStep("a.txt", "half\\n"), failStep()}}
	r, tr := newSkillRunner(t, client)
	if _, err := r.Run(context.Background(), "writer", "write a.txt"); err == nil {
		t.Fatal("a skill whose model refused succeeded")
	}
	if got, ok := tr.StagedFileContent(context.Background())["a.txt"]; ok {
		t.Fatalf("a failed skill's edit reached the turn: %q", got)
	}
}

// A command run with nothing after its name still reaches the model.
func TestRun_AnEmptyTaskRunsTheCommand(t *testing.T) {
	client := &scriptLLM{}
	r, _ := newSkillRunner(t, client)
	out, err := r.Run(context.Background(), "writer", "")
	if err != nil {
		t.Fatalf("Run with no task: %v", err)
	}
	if out.Text != "done" {
		t.Errorf("Text = %q", out.Text)
	}
}
