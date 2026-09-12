package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/protocol/schema"
)

// A small model that has decided to read one file will keep deciding it. The
// blocked-call reply used to cost a step and nothing else, so the turn ran to
// MaxSteps re-asking for a file it already had — on a 16k window that is the
// whole turn spent, with no answer at the end.
func TestRun_AModelStuckOnOneFileIsStoppedEarly(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	const sameRead = `{"type":"tool_call","tool":{"name":"read","input":{"path":"a.txt"}}}`
	steps := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		steps = append(steps, sameRead)
	}
	script := &scriptedLLM{steps: steps}

	ag, err := New(script, v, tr, Options{
		MaxSteps: 20,
		// The window this whole exercise is about.
		ModelContextTokens: 16384,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, _, runErr := ag.Run(context.Background(), nil, "what does a.txt say")
	if runErr == nil {
		t.Fatal("a model that will not stop re-reading must end the turn, not run to MaxSteps")
	}
	// And it must say why, so the transcript explains itself.
	if !strings.Contains(strings.ToLower(runErr.Error()), "read") {
		t.Fatalf("the error must name the tool that looped: %v", runErr)
	}
	if script.i >= 20 {
		t.Fatalf("the loop ran the whole budget anyway: %d steps", script.i)
	}
}
