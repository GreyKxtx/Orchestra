package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// Does a worker's edit go anywhere?
//
// orchestra_delegates_two_edits is the only eval task that exercises the Lead
// branch, and on its first real run it failed with both files untouched: 24
// steps, 27 task calls, 148 LLM calls of which about 124 were children, and
// nothing on disk. The children ran and thought for nine seconds apiece.
//
// Two explanations fit that, and they need different fixes:
//
//   - the worker never edits at all (the WorkOrder, the prompt, the child's
//     own tool surface), or
//   - the worker edits into the shared staging overlay and the work is never
//     flushed. Children are built with agent.Options that never set Apply —
//     `Apply` does not appear anywhere in this package — so
//     commitStagedAfterMutatingTool returns at its first line for every child
//     write, and the edit lives only in the overlay until a parent flushes it.
//
// This drives the real TaskRunner with a scripted child, so the answer is not
// a guess about a model's behaviour. It checks BOTH places: the file, and the
// overlay behind it.

// editingChildLLM is a worker that does exactly what a WorkOrder asks: one
// edit, then a result.
type editingChildLLM struct{ calls int }

func (e *editingChildLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_, _ = ctx, prompt
	return "{}", nil
}

func (e *editingChildLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_, _ = ctx, req
	e.calls++
	call := func(id, name, args string) *llm.CompleteResponse {
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID: id, Type: "function",
				Function: llm.ToolCallFunc{Name: name, Arguments: llm.ToolArguments(args)},
			}},
		}}
	}
	// The read is not decoration: worker mode runs behind the explore-first
	// gate, and an edit that arrives before any read of the WorkOrder scope is
	// denied outright. A script that skips it measures the gate, not the
	// delegation path.
	if e.calls == 1 {
		return call("c0", "read", `{"path":"width.go"}`), nil
	}
	if e.calls == 2 {
		return call("c1", "edit", `{"path":"width.go","search":"return 640","replace":"return 1920"}`), nil
	}
	return &llm.CompleteResponse{Message: llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID: "c2", Type: "function",
			Function: llm.ToolCallFunc{
				Name:      "task_result",
				Arguments: llm.ToolArguments(`{"status":"done","result":"width.go now returns 1920"}`),
			},
		}},
	}}, nil
}

func TestWorker_AnEditByAChildReachesTheWorkspace(t *testing.T) {
	root := t.TempDir()
	widthPath := filepath.Join(root, "width.go")
	const before = "package main\n\n// Width is the frame width in pixels.\nfunc Width() int {\n\treturn 640\n}\n"
	if err := os.WriteFile(widthPath, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module evalws\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	// The shape a real run has: the parent's Runner previews, and each
	// mutating tool is committed as it happens.
	tr.SetDryRun(true)

	client := &editingChildLLM{}
	r := New(client, v, tr, ChildAgentConfig{})
	t.Cleanup(func() {
		r.Close()
		_ = tr.Close()
	})

	id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
		Goal:         "In width.go, change Width to return 1920.\ntarget_files: width.go",
		SubagentType: "worker",
		MaxSteps:     4,
		TimeoutMS:    30_000,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	res, err := r.Wait(context.Background(), id, 30_000)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res == nil {
		t.Fatal("no result from the child")
	}
	t.Logf("child status=%q result=%q err=%q", res.Status, res.Result, res.Error)

	if client.calls == 0 {
		t.Fatal("the child never called the model, so this test is measuring the wrong thing")
	}

	staged := tr.StagedOps()
	onDisk, readErr := os.ReadFile(widthPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	changed := strings.Contains(string(onDisk), "1920")

	// Children are built with agent.Options that never set Apply — the word
	// does not appear anywhere in this package — so
	// commitStagedAfterMutatingTool returns at its first line for every child
	// write and the edit stays in the overlay the child shares with its
	// parent. That is the design: the parent flushes, on its final or on
	// finalizeOnMaxSteps. So the work landing in the overlay IS the child
	// doing its job.
	if !changed && len(staged) == 0 {
		t.Errorf("the child reported status=%q and neither the file nor the staging overlay "+
			"holds the edit — the work does not exist anywhere:\n%s", res.Status, onDisk)
	}
}
