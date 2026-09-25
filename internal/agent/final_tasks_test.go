package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

// runningAgency reports one running task until something waits for it.
type runningAgency struct {
	*fakeAgency
	mu     sync.Mutex
	waited []string
}

func (r *runningAgency) Board() []TaskBoardEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return []TaskBoardEntry{{TaskID: "task_7", Agent: "worker", Status: "running", Goal: "edit a.go", Finished: len(r.waited) > 0}}
}

func (r *runningAgency) WaitMany(_ context.Context, ids []string, _ int) (*WaitManyResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.waited = append(r.waited, ids...)
	return &WaitManyResult{Results: []*SubtaskResult{{TaskID: "task_7", Status: "done", Result: "WORKER-RESULT"}}}, nil
}

// The top-level agent does not finish while its tasks run: the first final is
// sent back with the list, the second makes the runtime collect them.
func TestFinalWaitsForRunningTasks(t *testing.T) {
	runner := &runningAgency{fakeAgency: &fakeAgency{info: leadInfo()}}
	llmClient := &recordingFinalLLM{}
	ag, _ := newTestAgent(t, llmClient, Options{Mode: ModeBuild, SubtaskRunner: runner})
	if _, _, err := ag.Run(context.Background(), nil, "do the work"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if llmClient.calls < 3 {
		t.Fatalf("expected refusal, wait, then the final, got %d calls", llmClient.calls)
	}
	if !strings.Contains(llmClient.notes[0], "task_7") || !strings.Contains(llmClient.notes[0], "task_wait") {
		t.Fatalf("the first refusal must name the task and the way out: %q", llmClient.notes[0])
	}
	if len(runner.waited) != 1 || runner.waited[0] != "task_7" {
		t.Fatalf("the second final must make the runtime wait, waited=%v", runner.waited)
	}
	if !strings.Contains(llmClient.notes[1], "WORKER-RESULT") {
		t.Fatalf("the collected results go back to the model: %q", llmClient.notes[1])
	}
}

// recordingFinalLLM always answers with an empty final and records the last
// user message of each request after the first.
type recordingFinalLLM struct {
	calls int
	notes []string
}

func (r *recordingFinalLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (r *recordingFinalLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	r.calls++
	if r.calls > 1 {
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if m := req.Messages[i]; m.Role == llm.RoleUser && (strings.Contains(m.Content, "task_7")) {
				r.notes = append(r.notes, m.Content)
				break
			}
		}
	}
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "All done."}}, nil
}
