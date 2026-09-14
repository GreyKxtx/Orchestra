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

// What does a child that says nothing report?
//
// Found while writing the verifier tests: my scripted child called task_result
// with {"status":"done","result":"..."} — plausible, and wrong. The tool takes
// one argument, "content". The agent unmarshals into a struct with that single
// field, discards the mismatch (`_ = json.Unmarshal`), and finishes the child
// with an EMPTY result.
//
// That empty result then travels: workerTaskResultSuccess("") returns true —
// an absent status is read as success — so verification runs, the checks pass
// (the file IS valid), and the Lead is handed
// {"status":"verified_success","worker_result":""}: told the job is done, given
// no description of what was done.
//
// The guard that exists, checkWorkerResultSchema, only runs when the Lead
// delegated a WorkOrder (opts.WorkerStrictResult is set at tasks.go:653 under
// `mode == ModeWorker && workOrder != nil`). A plain-text goal — which is what
// the Lead sends most of the time — has no schema check at all.
//
// The verifier has the same hole from the other side: a verdict that arrives
// empty is read as a failed verification, and the Lead is shown
// "llm_verifier_result": "" with nothing to explain it.

// silentChildLLM edits properly, then reports with an argument shape the agent
// cannot read — the exact mistake a model makes when it guesses the schema.
type silentChildLLM struct{ calls int }

func (s *silentChildLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_, _ = ctx, prompt
	return "{}", nil
}

func (s *silentChildLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_, _ = ctx, req
	s.calls++
	if s.calls == 1 {
		return mkToolCall("s0", "read", `{"path":"width.go"}`), nil
	}
	if s.calls == 2 {
		return mkToolCall("s1", "edit", `{"path":"width.go","search":"return 640","replace":"return 1920"}`), nil
	}
	// "result" instead of "content": everything the child wanted to say is
	// dropped here.
	return mkToolCall("s2", "task_result", `{"status":"done","result":"width.go now returns 1920"}`), nil
}

func TestWorker_AResultTheAgentCouldNotReadIsNotReportedAsSuccess(t *testing.T) {
	root := t.TempDir()
	const before = "package main\n\nfunc Width() int {\n\treturn 640\n}\n"
	if err := os.WriteFile(filepath.Join(root, "width.go"), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module evalws\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tr.SetDryRun(true)
	client := &silentChildLLM{}
	r := New(client, v, tr, ChildAgentConfig{})
	t.Cleanup(func() { r.Close(); _ = tr.Close() })

	id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
		// A plain-text goal, not a WorkOrder: this is the shape the Lead sends
		// most often, and the shape with no schema enforcement.
		Goal:         "In width.go, change Width to return 1920.\ntarget_files: width.go",
		SubagentType: "worker", MaxSteps: 6, TimeoutMS: 30_000,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	res, err := r.Wait(context.Background(), id, 30_000)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res == nil {
		t.Fatal("no result")
	}
	t.Logf("Lead was handed: %s", res.Result)

	// The Lead routes on status. Reporting success for an answer that does not
	// exist is worse than reporting failure: the WorkOrder is counted done and
	// nobody looks again.
	if strings.Contains(res.Result, `"status":"verified_success"`) &&
		strings.Contains(res.Result, `"worker_result":""`) {
		t.Errorf("the child's answer was dropped and the Lead was told the work is "+
			"verified. An empty answer is not a successful one:\n%s", res.Result)
	}
}
