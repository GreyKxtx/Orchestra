package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// editFirstLLM always tries edit before any read/explore, then the circuit trips.
type editFirstLLM struct {
	mu      sync.Mutex
	calls   int
	denials int
}

func (m *editFirstLLM) Plan(_ context.Context, _ string) (string, error) { return "", nil }

func (m *editFirstLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range req.Messages {
		if msg.Role == llm.RoleTool && strings.Contains(msg.Content, "Policy violation") {
			m.denials++
		}
	}
	m.calls++
	input, _ := json.Marshal(map[string]string{
		"path":    "handler.go",
		"search":  "package main",
		"replace": "package patched",
	})
	return &llm.CompleteResponse{
		Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:   "edit_call",
				Type: "function",
				Function: llm.ToolCallFunc{
					Name:      "edit",
					Arguments: llm.ToolArguments(input),
				},
			}},
		},
	}, nil
}

func TestExploreFirstPolicy_BlocksEditWithoutRead(t *testing.T) {
	root := t.TempDir()
	src := "package main\n\nfunc Handler() {}\n"
	if err := os.WriteFile(filepath.Join(root, "handler.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := &editFirstLLM{}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	verifyOff := false
	r := New(mock, v, tr, ChildAgentConfig{WorkerVerifyEnabled: &verifyOff})
	t.Cleanup(func() {
		r.Close()
		_ = tr.Close()
	})

	id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
		Goal:         `{"intent":"patch handler","target_files":["handler.go"]}`,
		SubagentType: "worker",
		MaxSteps:     8,
		TimeoutMS:    15_000,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	res, err := r.Wait(context.Background(), id, 15_000)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, "handler.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != src {
		t.Fatalf("edit must not apply under explore-first gate; disk=%q", got)
	}

	mock.mu.Lock()
	calls, denials := mock.calls, mock.denials
	mock.mu.Unlock()
	if denials < 1 {
		t.Fatalf("expected Policy violation denials in tool results, denials=%d calls=%d result=%+v", denials, calls, res)
	}
	// MaxDeniedToolRepeats default is 2 → third consecutive deny trips the
	// circuit. If the denied counter reset on each attempt, the loop would
	// continue until MaxSteps (8).
	if calls > 4 {
		t.Fatalf("denied counter appears reset: %d LLM calls (want circuit trip around 3)", calls)
	}
	if res != nil && res.Status == "done" && calls > 4 {
		t.Fatalf("run completed without circuit trip: %+v", res)
	}
}
