package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// stageLLM plays the three stages, told apart by their tools: the
// investigator (task_result and runtime_query) and the critic (task_result
// alone) answer through task_result, the coder finishes with no patches. The
// critic rejects the first attempt.
type stageLLM struct {
	mu         sync.Mutex
	critiques  int
	coderGoals []string
}

func (s *stageLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func taskResult(content string) *llm.CompleteResponse {
	args, _ := llmJSON(map[string]string{"content": content})
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
		ID: "tr", Type: "function", Function: llm.ToolCallFunc{Name: "task_result", Arguments: llm.ToolArguments(args)},
	}}}}
}

func (s *stageLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	offered := map[string]bool{}
	for _, d := range req.Tools {
		offered[d.Function.Name] = true
	}
	switch {
	case offered["task_result"] && offered["runtime_query"]:
		return taskResult("FOUND: the bug is in a.go line 3"), nil
	case offered["task_result"]:
		s.critiques++
		if s.critiques == 1 {
			return taskResult(`{"status":"reject","reason":"the bug is still there"}`), nil
		}
		return taskResult(`{"status":"accept"}`), nil
	default:
		for _, m := range req.Messages {
			if m.Role == llm.RoleUser {
				s.coderGoals = append(s.coderGoals, m.Content)
			}
		}
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant,
			Content: `{"type":"final","final":{"patches":[]}}`}}, nil
	}
}

// The investigator's findings reach the coder and the critic's verdict
// counts. Built as top-level agents, the two lost task_result — it is offered
// to children only — so the investigation was dropped and the empty verdict
// read as acceptance on the first attempt.
func TestPipeline_StagesAnswerThroughTaskResult(t *testing.T) {
	root := t.TempDir()
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	mock := &stageLLM{}
	res, err := Run(context.Background(), mock, v, tr, "fix the bug", Options{MaxCoderAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(mock.coderGoals) == 0 || !strings.Contains(strings.Join(mock.coderGoals, "\n"), "FOUND: the bug is in a.go") {
		t.Errorf("the coder never saw the investigation: %q", mock.coderGoals)
	}
	if res.Attempts != 2 || !res.Accepted {
		t.Errorf("the critic rejects once and then accepts: attempts=%d accepted=%v", res.Attempts, res.Accepted)
	}
}

func llmJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
