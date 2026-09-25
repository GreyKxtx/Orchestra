package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/protocol/wire"
)

// childScript answers each model call with the next step of its script and
// records the trace every call ran under.
type childScript struct {
	mu     sync.Mutex
	steps  []func(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error)
	i      int
	traces []llm.Trace
}

func (s *childScript) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *childScript) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.traces = append(s.traces, llm.TraceFrom(ctx))
	if s.i >= len(s.steps) {
		return final("done"), nil
	}
	step := s.steps[s.i]
	s.i++
	return step(ctx, req)
}

func writeCall(path, content string) func(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	return func(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
		args := `{"path":"` + path + `","content":"` + content + `"}`
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "call-write", Type: "function",
			Function: llm.ToolCallFunc{Name: "write", Arguments: llm.ToolArguments(args)},
		}}}}, nil
	}
}

func final(summary string) *llm.CompleteResponse {
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant,
		Content: `{"type":"final","final":{"summary":"` + summary + `","patches":[]}}`}}
}

func finalStep(summary string) func(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	return func(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) { return final(summary), nil }
}

func newChildRunner(t *testing.T) (*tools.Runner, *schema.Validator) {
	t.Helper()
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	return tr, v
}

func writerOptions(t *testing.T) agent.Options {
	t.Helper()
	defs, err := tools.ResolveToolNamesWithPolicy([]string{"read", "write"}, tools.Capabilities{})
	if err != nil {
		t.Fatal(err)
	}
	return ChildOptions(nil, func(o *agent.Options) {
		o.MaxSteps = 4
		o.CustomTools = defs
		o.SystemPromptOverride = "you write the file you are asked for"
	})
}

// A child writes into a layer of its own; when it succeeds the layer is
// committed into its caller's view, and the outcome names the files.
func TestRunChild_ASuccessfulChildsEditsReachItsCaller(t *testing.T) {
	tr, v := newChildRunner(t)
	client := &childScript{steps: []func(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error){
		writeCall("a.txt", "hello\\n"), finalStep("wrote a.txt"),
	}}
	ctx := context.Background()
	out, err := RunChild(ctx, ChildRun{Client: client, Validator: v, Tools: tr, Options: writerOptions(t), Goal: "write a.txt", Kind: "skill:writer"})
	if err != nil {
		t.Fatalf("RunChild: %v", err)
	}
	if got := tr.StagedFileContent(ctx)["a.txt"]; got != "hello\n" {
		t.Fatalf("the caller's view does not carry the child's edit: %q", got)
	}
	if len(out.Committed) != 1 || out.Committed[0] != "a.txt" {
		t.Errorf("Committed = %v, want [a.txt]", out.Committed)
	}
	if out.Text() != "wrote a.txt" {
		t.Errorf("Text() = %q, want the child's closing words", out.Text())
	}
	if out.TaskID == "" {
		t.Error("the child ran without an identity")
	}
}

// A child that fails takes its edits with it: nothing of what it wrote
// reaches its caller.
func TestRunChild_AFailedChildsEditsAreDropped(t *testing.T) {
	tr, v := newChildRunner(t)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	client := &childScript{steps: []func(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error){
		writeCall("a.txt", "half done\\n"),
		func(ctx context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
			cancel(errors.New("the caller gave up"))
			return nil, ctx.Err()
		},
	}}
	out, err := RunChild(ctx, ChildRun{Client: client, Validator: v, Tools: tr, Options: writerOptions(t), Goal: "write a.txt", Kind: "skill:writer"})
	if err == nil {
		t.Fatal("a cancelled child succeeded")
	}
	if out == nil || len(out.History) == 0 {
		t.Error("the outcome should carry what the child managed before it failed")
	}
	if got, ok := tr.StagedFileContent(context.Background())["a.txt"]; ok {
		t.Fatalf("a failed child's edit reached its caller: %q", got)
	}
}

// A file the caller changed under the child since the child first wrote it
// is a conflict: the child's run succeeded, but its commit is its error and
// the caller keeps its own version.
func TestRunChild_AConflictingCommitIsTheChildsError(t *testing.T) {
	tr, v := newChildRunner(t)
	ctx := context.Background()
	client := &childScript{steps: []func(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error){
		writeCall("a.txt", "the child's\\n"),
		func(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
			// The caller writes the same file while the child runs.
			if _, err := tr.FSWrite(ctx, tools.FSWriteRequest{Path: "a.txt", Content: "the caller's\n"}); err != nil {
				return nil, err
			}
			return final("wrote a.txt"), nil
		},
	}}
	out, err := RunChild(ctx, ChildRun{Client: client, Validator: v, Tools: tr, Options: writerOptions(t), Goal: "write a.txt", Kind: "skill:writer"})
	if err == nil || !strings.Contains(err.Error(), "merge_conflict") {
		t.Fatalf("err = %v, want a merge conflict", err)
	}
	if len(out.Committed) != 0 {
		t.Errorf("a conflicting layer committed %v", out.Committed)
	}
	if got := tr.StagedFileContent(ctx)["a.txt"]; got != "the caller's\n" {
		t.Errorf("the caller's view = %q, want its own version kept", got)
	}
}

// The task runner forks the layer itself, before the child is scheduled, and
// commits it when it decides to; such a child writes where its caller does.
func TestRunChild_CallerOwnsLayer(t *testing.T) {
	tr, v := newChildRunner(t)
	ctx := context.Background()
	client := &childScript{steps: []func(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error){
		writeCall("a.txt", "hello\\n"), finalStep("wrote a.txt"),
	}}
	out, err := RunChild(ctx, ChildRun{Client: client, Validator: v, Tools: tr, Options: writerOptions(t), Goal: "write a.txt", CallerOwnsLayer: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := tr.StagedFileContent(ctx)["a.txt"]; got != "hello\n" {
		t.Fatalf("the caller's view = %q", got)
	}
	if len(out.Committed) != 0 {
		t.Errorf("no layer of its own, nothing to commit: %v", out.Committed)
	}
}

// A child runs under an identity of its own, below its caller's: its model
// calls are logged as the child's, with the caller as parent, one level
// deeper. A caller that traced the child itself keeps its trace.
func TestRunChild_TracesTheChildUnderItsCaller(t *testing.T) {
	tr, v := newChildRunner(t)
	parent := llm.WithTrace(context.Background(), llm.Trace{RunID: "run-1", TaskID: "task_parent", Depth: 1})
	client := &childScript{}
	out, err := RunChild(parent, ChildRun{Client: client, Validator: v, Tools: tr, Options: writerOptions(t), Goal: "say done", Kind: "stage:review"})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.traces) == 0 {
		t.Fatal("the model was never called")
	}
	got := client.traces[0]
	if got.RunID != "run-1" || got.TaskID != out.TaskID || got.ParentTaskID != "task_parent" || got.Depth != 2 {
		t.Errorf("child trace = %+v, want run-1 / %s under task_parent at depth 2", got, out.TaskID)
	}

	traced := llm.WithTrace(context.Background(), llm.Trace{RunID: "run-1", TaskID: "task_7", ParentTaskID: "task_parent", Depth: 2})
	client = &childScript{}
	if _, err := RunChild(traced, ChildRun{Client: client, Validator: v, Tools: tr, Options: writerOptions(t), Goal: "say done", TaskID: "task_7"}); err != nil {
		t.Fatal(err)
	}
	if got := client.traces[0]; got.TaskID != "task_7" || got.Depth != 2 || got.ParentTaskID != "task_parent" {
		t.Errorf("a caller's own trace was replaced: %+v", got)
	}
}

// child_started opens and child_done closes every child, with its identity,
// its kind and how it ended; the stream sink is built for that identity.
func TestRunChild_EventsFrameTheChild(t *testing.T) {
	tr, v := newChildRunner(t)
	var mu sync.Mutex
	var events []wire.AgentEvent
	var streamFor []string
	ev := ChildEvents{
		Notify: func(e wire.AgentEvent) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		},
		Stream: func(taskID, parentToolCallID, subagentType string) func(agent.AgentEvent) {
			mu.Lock()
			streamFor = append(streamFor, taskID+"/"+parentToolCallID+"/"+subagentType)
			mu.Unlock()
			return func(agent.AgentEvent) {}
		},
	}
	client := &childScript{steps: []func(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error){finalStep("all good")}}
	out, err := RunChild(context.Background(), ChildRun{Client: client, Validator: v, Tools: tr, Options: writerOptions(t),
		Goal: "say done", Kind: "skill:writer", ParentToolCallID: "call-9", Events: ev})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != wire.EventChildStarted || events[1].Type != wire.EventChildDone {
		t.Fatalf("events = %+v, want child_started then child_done", events)
	}
	start, done := events[0], events[1]
	if start.TaskID != out.TaskID || start.SubagentType != "skill:writer" || start.ParentToolCallID != "call-9" || start.Content != "say done" || start.Depth != 1 {
		t.Errorf("child_started = %+v", start)
	}
	if done.TaskID != out.TaskID || done.Status != "done" || done.Content != "all good" || done.Error != "" {
		t.Errorf("child_done = %+v", done)
	}
	if len(streamFor) != 1 || streamFor[0] != out.TaskID+"/call-9/skill:writer" {
		t.Errorf("stream sink built for %v", streamFor)
	}

	// A child that fails closes with the error.
	events = nil
	ctx, cancel := context.WithCancel(context.Background())
	client = &childScript{steps: []func(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error){
		func(ctx context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
			cancel()
			return nil, ctx.Err()
		},
	}}
	if _, err := RunChild(ctx, ChildRun{Client: client, Validator: v, Tools: tr, Options: writerOptions(t), Goal: "say done", Kind: "skill:writer", Events: ev}); err == nil {
		t.Fatal("a cancelled child succeeded")
	}
	if len(events) != 2 || events[1].Status != "cancelled" || events[1].Error == "" {
		t.Errorf("a cancelled child's events = %+v", events)
	}
}

// A panic on the child's path is the child's error, not the process's.
func TestRunChild_ContainsAPanic(t *testing.T) {
	tr, v := newChildRunner(t)
	calls := 0
	ev := ChildEvents{Notify: func(e wire.AgentEvent) {
		calls++
		if calls == 1 {
			panic("the sink is broken")
		}
	}}
	client := &childScript{}
	_, err := RunChild(context.Background(), ChildRun{Client: client, Validator: v, Tools: tr, Options: writerOptions(t), Goal: "say done", Events: ev})
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("err = %v, want the panic as the child's error", err)
	}
	if calls != 2 {
		t.Errorf("child_done still closes a child whose start panicked: %d notifications", calls)
	}
}

// ClosingText unwraps a final envelope to its summary and leaves prose as
// it is.
func TestClosingText(t *testing.T) {
	hist := []llm.Message{
		{Role: llm.RoleUser, Content: "do it"},
		{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"summary":"raised retryLimit to 13","patches":[]}}`},
	}
	if got := ClosingText(hist); got != "raised retryLimit to 13" {
		t.Errorf("ClosingText = %q", got)
	}
	hist[1].Content = "  done, see a.txt  "
	if got := ClosingText(hist); got != "done, see a.txt" {
		t.Errorf("ClosingText = %q", got)
	}
	if got := ClosingText(nil); got != "" {
		t.Errorf("ClosingText(nil) = %q", got)
	}
}
