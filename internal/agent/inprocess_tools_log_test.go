package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// skill_invoke, todowrite and todoread bypass tools.Runner — the agent answers
// them itself — and only the task* family mirrored the runner's llm_log
// entries. So a run that delegated to a skill or kept a checklist left no
// trace of it in .orchestra/llm_log.jsonl.
//
// Two things broke on that. Anyone reading a log to see why a run went the way
// it did could not tell a delegated subtask from work the parent did inline.
// And the eval's tool_used check reads exactly these entries, so
// `tool_used: skill_invoke` and `tool_used: todowrite` could never pass, no
// matter what the model did — which is how four models came to be recorded as
// "never called it".

type skillStub struct{ called int }

func (s *skillStub) InvokeSkill(context.Context, string, string) (string, error) {
	s.called++
	return "the skill raised retryLimit to 13", nil
}

// inProcessScriptLLM calls the named tools once each, then finishes.
type inProcessScriptLLM struct {
	calls []struct{ name, args string }
	n     int
}

func (s *inProcessScriptLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *inProcessScriptLLM) Complete(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	if s.n < len(s.calls) {
		c := s.calls[s.n]
		s.n++
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID: "c", Type: "function",
				Function: llm.ToolCallFunc{Name: c.name, Arguments: llm.ToolArguments(c.args)},
			}},
		}}, nil
	}
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[]}}`}}, nil
}

func loggedToolNames(t *testing.T, root string) map[string]int {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, ".orchestra", "llm_log.jsonl"))
	if err != nil {
		t.Fatalf("no llm_log.jsonl: %v", err)
	}
	seen := map[string]int{}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e struct {
			Event    string `json:"event"`
			ToolName string `json:"tool_name"`
		}
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if e.Event == "tool_call" && e.ToolName != "" {
			seen[e.ToolName]++
		}
	}
	return seen
}

func TestInProcessTools_LeaveTheSameTraceAsRunnerTools(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".orchestra"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// The CKG index keeps a sqlite handle open; on Windows TempDir cleanup
	// fails while it is held.
	t.Cleanup(func() { _ = runner.Close() })
	validator, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	client := &inProcessScriptLLM{calls: []struct{ name, args string }{
		{"todowrite", `{"todos":[{"id":"1","content":"raise the constant","status":"in_progress"}]}`},
		{"todoread", `{}`},
		{"skill_invoke", `{"skill":"bumper","task":"raise retryLimit"}`},
	}}
	stub := &skillStub{}

	ag, err := New(client, validator, runner, Options{
		MaxSteps:    8,
		AgentLogger: llm.NewLogger(root),
		SkillRunner: stub,
		Skills:      []SkillSpec{{Name: "bumper", Description: "raises a constant"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "raise retryLimit to its next tier"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if stub.called != 1 {
		t.Fatalf("the skill ran %d times, want 1", stub.called)
	}

	logged := loggedToolNames(t, root)
	for _, name := range []string{"todowrite", "todoread", "skill_invoke"} {
		if logged[name] == 0 {
			t.Errorf("%s ran and left no tool_call in llm_log.jsonl (logged: %v)", name, logged)
		}
	}
}
