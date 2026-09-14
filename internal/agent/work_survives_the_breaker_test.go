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

// A turn that finished its work and then got stuck reported nothing but the
// error.
//
// Running out of steps already returns a Result — finalizeOnMaxSteps, with
// StopReason "max_steps" — because the edits are on disk and pretending
// otherwise helps nobody. The circuit breaker had no such path: every trip
// returned `history, nil, err`, and the completed work went unreported.
//
// From the first run of the todo task, with all three edits committed:
//
//	edit host.go    -> disk_commit 99 bytes
//	edit port.go    -> disk_commit 81 bytes
//	edit scheme.go  -> disk_commit 87 bytes
//	write host.go   -> commit changed no file
//	write port.go   -> commit changed no file
//	write scheme.go -> denied, turn over
//
// The workspace was exactly right. The run was reported as
// "InvalidLLMOutput: the model kept calling «write» after it was refused".

// stuckAfterWorkLLM edits one file, then repeats a no-op write until the
// duplicate blocker ends the turn.
type stuckAfterWorkLLM struct{ calls int }

func (s *stuckAfterWorkLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_, _ = ctx, prompt
	return "{}", nil
}

func (s *stuckAfterWorkLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_, _ = ctx, req
	s.calls++
	if s.calls == 1 {
		return toolCallResponse("e1", "edit",
			`{"path":"host.go","search":"\"localhost\"","replace":"\"api.example.com\""}`), nil
	}
	// The same write, over and over: what a model does when it cannot tell
	// that its work already landed.
	return toolCallResponse("w1", "write",
		`{"path":"host.go","content":"package main\n\nfunc Host() string {\n\treturn \"api.example.com\"\n}\n"}`), nil
}

func TestBreaker_WorkAlreadyOnDiskIsReportedNotDiscarded(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "host.go"),
		[]byte("package main\n\nfunc Host() string {\n\treturn \"localhost\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
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

	ag, err := New(&stuckAfterWorkLLM{}, v, tr, Options{MaxSteps: 12, Apply: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, res, runErr := ag.Run(context.Background(), nil, "change Host to api.example.com")

	body, readErr := os.ReadFile(filepath.Join(root, "host.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(body), "api.example.com") {
		t.Fatalf("the edit never landed, so this test is measuring the wrong thing:\n%s", body)
	}

	if runErr != nil {
		t.Errorf("the turn did its work and then got stuck; reporting only the error "+
			"throws away a finished job: %v", runErr)
	}
	if res == nil {
		t.Fatal("no result at all for a turn whose edit is on disk")
	}
	if res.StopReason == "" {
		t.Error("the result must say why the turn stopped, or a caller cannot tell it " +
			"from a clean finish")
	}
}

// The other half: a turn that got stuck having changed nothing has no work to
// report, and must still fail. Otherwise the breaker stops being a signal.
func TestBreaker_ATurnThatChangedNothingStillFails(t *testing.T) {
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

	// Never edits anything: every call is the same read of a missing file.
	client := &loopingReadLLM{}
	ag, err := New(client, v, tr, Options{MaxSteps: 12, Apply: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, _, runErr := ag.Run(context.Background(), nil, "read nothing forever")
	if runErr == nil {
		t.Error("a turn that changed nothing and went in circles must still be an error")
	}
}

type loopingReadLLM struct{}

func (l *loopingReadLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_, _ = ctx, prompt
	return "{}", nil
}

func (l *loopingReadLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_, _ = ctx, req
	return toolCallResponse("r1", "fs.delete", `{"path":"missing.go"}`), nil
}
