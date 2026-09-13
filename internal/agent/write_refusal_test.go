package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// A refusal is read by a model that is mid-task, and the only question it is
// asking is "how do I get this file changed". Naming the directories it MAY
// write to answers a different question, and leaves it with nowhere to go.
//
// That is not a guess about what models do with an unhelpful refusal. An
// orchestra run against a local model called write, was refused, and called
// write again until the denied-repeat breaker ended the turn:
//
//	Error: InvalidLLMOutput: the model kept calling «write» after it was
//	refused — stopping the turn
//
// Nothing was written, 52 LLM calls were spent, and the workspace was
// untouched. The guard was right to refuse; the refusal was not enough to act
// on. So these tests hold the part that makes it actionable — the route out —
// and they read the message the model actually receives rather than the
// string the code builds, because those are the same thing only if the
// refusal really reaches history.

// recordingLLM answers from a script and keeps every request, so a test can
// read the tool messages the agent fed back.
type recordingLLM struct {
	steps []string
	i     int
	seen  [][]llm.Message
}

func (r *recordingLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_, _ = ctx, prompt
	return "{}", nil
}

func (r *recordingLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_ = ctx
	r.seen = append(r.seen, append([]llm.Message(nil), req.Messages...))
	if r.i >= len(r.steps) {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}, nil
	}
	out := r.steps[r.i]
	r.i++
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: out}}, nil
}

// refusalFor runs one refused write in the given mode and returns the tool
// message the agent put into history for it.
func refusalFor(t *testing.T, mode Mode) string {
	t.Helper()
	root := t.TempDir()

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	// Read first. Orchestra and architecture modes put an explore-first gate
	// ahead of the path guard, so a write with no prior read is refused for a
	// different reason entirely and never reaches the rule under test. The
	// Lead in the real run had read four files before it tried to write.
	if err := os.MkdirAll(filepath.Join(root, "internal", "store"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(root, "internal", "store", "store.go")
	if err := os.WriteFile(store, []byte("package store\n\ntype Store struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	client := &recordingLLM{steps: []string{
		`{"type":"tool_call","tool":{"name":"read","input":{"path":"internal/store/store.go"}}}`,
		`{"type":"tool_call","tool":{"name":"write","input":{"path":"internal/store/store.go","content":"package store"}}}`,
		`{"type":"final","final":{"patches":[]}}`,
	}}

	ag, err := New(client, v, tr, Options{
		MaxSteps: 6,
		Mode:     mode,
		PlanPath: ".orchestra/plans/test-plan.md",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "add a Total method to Store"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, req := range client.seen {
		for _, m := range req {
			if m.Role == llm.RoleTool && strings.Contains(m.Content, "writes are allowed only to") {
				return m.Content
			}
		}
	}
	t.Fatalf("the write was never refused in %s mode, so this test proves nothing", mode)
	return ""
}

func TestWriteRefusal_OrchestraLeadIsToldToDelegate(t *testing.T) {
	got := refusalFor(t, ModeOrchestra)

	if !strings.Contains(got, "orchestra lead") {
		t.Errorf("the refusal must say which rule refused it:\n%s", got)
	}
	// The part that matters: the Lead cannot edit, and delegation is the only
	// route to a changed file. Without this the model has been told what it
	// may not do and nothing else.
	if !strings.Contains(got, "task(") {
		t.Errorf("the refusal must name the way to get the file changed:\n%s", got)
	}
	if !strings.Contains(got, "worker") {
		t.Errorf("the refusal must say who does the editing:\n%s", got)
	}
}

func TestWriteRefusal_PlanModeSaysWhereTheChangeGoesInstead(t *testing.T) {
	got := refusalFor(t, ModePlan)

	if !strings.Contains(got, "plan mode") {
		t.Errorf("the refusal must say which rule refused it:\n%s", got)
	}
	if !strings.Contains(got, "plan_exit") {
		t.Errorf("plan mode must say how the change eventually gets made:\n%s", got)
	}
}

// The refusal still has to say what it always said. A hint that replaced the
// allowed paths rather than adding to them would trade one missing fact for
// another.
func TestWriteRefusal_StillNamesTheAllowedPaths(t *testing.T) {
	for _, mode := range []Mode{ModeOrchestra, ModePlan} {
		got := refusalFor(t, mode)
		if !strings.Contains(got, ".orchestra/plans/test-plan.md") {
			t.Errorf("%s: the refusal must still name the path that IS writable:\n%s", mode, got)
		}
	}
}
