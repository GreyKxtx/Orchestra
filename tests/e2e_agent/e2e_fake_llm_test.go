package e2e_agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/store"
	"github.com/orchestra/orchestra/internal/tools"
)

type fakeLLM struct {
	responses []*llm.CompleteResponse
	i         int
}

func (f *fakeLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_ = ctx
	_ = prompt
	return "{}", nil
}

func (f *fakeLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_ = ctx
	_ = req
	if f.i >= len(f.responses) {
		// Safe fallback: empty patch set (will be rejected by schema if used).
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"patches":[]}`}}, nil
	}
	out := f.responses[f.i]
	f.i++
	return out, nil
}

func TestAgent_E2E_FakeLLM_RewritesFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello old world\n"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	h := store.ComputeSHA256([]byte("hello old world\n"))

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}

	llmClient := &fakeLLM{responses: []*llm.CompleteResponse{
		{
			Message: llm.Message{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{
						Type: "function",
						Function: llm.ToolCallFunc{
							Name:      "fs.read",
							Arguments: llm.ToolArguments([]byte(`{"path":"a.txt"}`)),
						},
					},
				},
			},
		},
		{
			Message: llm.Message{
				Role: llm.RoleAssistant,
				Content: `{
  "patches": [
    {
      "type": "file.search_replace",
      "path": "a.txt",
      "search": "old",
      "replace": "new",
      "file_hash": "` + h + `"
    }
  ]
}`,
			},
		},
	}}

	ag, err := agent.New(llmClient, v, tr, agent.Options{
		MaxSteps: 10,
		Apply:    true,
		Backup:   false,
	})
	if err != nil {
		t.Fatalf("New agent failed: %v", err)
	}

	_, res, err := ag.Run(context.Background(), nil, "replace old with new")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res == nil || res.ApplyResponse == nil {
		t.Fatalf("expected apply response")
	}

	after, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(after) != "hello new world\n" {
		t.Fatalf("unexpected content: %q", string(after))
	}
}

func TestAgent_E2E_FakeLLM_CreatesNewFile(t *testing.T) {
	root := t.TempDir()

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}

	llmClient := &fakeLLM{responses: []*llm.CompleteResponse{
		{
			Message: llm.Message{
				Role: llm.RoleAssistant,
				Content: `{
  "patches": [
    {
      "type": "file.write_atomic",
      "path": "new.txt",
      "content": "hello\n",
      "mode": 420,
      "conditions": { "must_not_exist": true }
    }
  ]
}`,
			},
		},
	}}

	ag, err := agent.New(llmClient, v, tr, agent.Options{
		MaxSteps: 5,
		Apply:    true,
		Backup:   false,
	})
	if err != nil {
		t.Fatalf("New agent failed: %v", err)
	}

	_, _, err = ag.Run(context.Background(), nil, "create new file")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(root, "new.txt"))
	if err != nil {
		t.Fatalf("read new file failed: %v", err)
	}
	if string(b) != "hello\n" {
		t.Fatalf("unexpected new file content: %q", string(b))
	}
}
