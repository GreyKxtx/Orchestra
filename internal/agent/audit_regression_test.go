package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

// toolCallSequenceLLM returns predefined OpenAI-style tool_call responses.
type toolCallSequenceLLM struct {
	responses []*llm.CompleteResponse
	i         int
}

func (m *toolCallSequenceLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_ = ctx
	_ = prompt
	return "{}", nil
}

func (m *toolCallSequenceLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_ = ctx
	_ = req
	if m.i >= len(m.responses) {
		return &llm.CompleteResponse{
			Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`},
		}, nil
	}
	r := m.responses[m.i]
	m.i++
	return r, nil
}

func newTestAgent(t *testing.T, llmClient llm.Client, opts Options) (*Agent, *tools.Runner) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello\n"), 0o644); err != nil {
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
	t.Cleanup(func() { tr.Close() })
	opts.MaxSteps = 10
	ag, err := New(llmClient, v, tr, opts)
	if err != nil {
		t.Fatal(err)
	}
	return ag, tr
}

func toolMessages(history []llm.Message) []llm.Message {
	var out []llm.Message
	for _, m := range history {
		if m.Role == llm.RoleTool {
			out = append(out, m)
		}
	}
	return out
}

// P0: AgentLogger=nil must not panic on serial runner tool calls.
func TestAgent_Run_NilAgentLogger_SerialTool_NoPanic(t *testing.T) {
	llmClient := &toolCallSequenceLLM{responses: []*llm.CompleteResponse{
		{
			Message: llm.Message{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{{
					ID:   "c1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "read",
						Arguments: llm.ToolArguments([]byte(`{"path":"a.txt"}`)),
					},
				}},
			},
		},
	}}
	ag, _ := newTestAgent(t, llmClient, Options{AgentLogger: nil})
	hist, res, err := ag.Run(context.Background(), nil, "read file")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res == nil {
		t.Fatal("expected result")
	}
	tools := toolMessages(hist)
	if len(tools) != 1 {
		t.Fatalf("tool messages = %d, want 1", len(tools))
	}
	if !strings.Contains(tools[0].Content, "hello") {
		t.Fatalf("unexpected tool content: %s", tools[0].Content)
	}
}

// P0: todoread + read in one LLM response must serial-run both (not parallel batch).
func TestAgent_Run_TodoreadPlusRead_BothInHistory(t *testing.T) {
	llmClient := &toolCallSequenceLLM{responses: []*llm.CompleteResponse{
		{
			Message: llm.Message{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{ID: "t1", Type: "function", Function: llm.ToolCallFunc{Name: "todoread", Arguments: llm.ToolArguments([]byte(`{}`))}},
					{ID: "t2", Type: "function", Function: llm.ToolCallFunc{Name: "read", Arguments: llm.ToolArguments([]byte(`{"path":"a.txt"}`))}},
				},
			},
		},
	}}
	ag, _ := newTestAgent(t, llmClient, Options{
		Mode:         ModeAsk,
		InitialTodos: []tools.TodoItem{{ID: "1", Content: "ship it", Status: tools.TodoDone}},
	})
	hist, _, err := ag.Run(context.Background(), nil, "read todos and file")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	msgs := toolMessages(hist)
	if len(msgs) != 2 {
		t.Fatalf("tool messages = %d, want 2", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "ship it") {
		t.Fatalf("todoread result missing todo: %s", msgs[0].Content)
	}
	if !strings.Contains(msgs[1].Content, "hello") {
		t.Fatalf("read result missing content: %s", msgs[1].Content)
	}
}

// P1: mixed mutating + read batch must execute all calls serially.
func TestAgent_Run_MixedBatch_TodowritePlusRead_BothExecuted(t *testing.T) {
	llmClient := &toolCallSequenceLLM{responses: []*llm.CompleteResponse{
		{
			Message: llm.Message{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{ID: "w1", Type: "function", Function: llm.ToolCallFunc{
						Name: "todowrite",
						Arguments: llm.ToolArguments([]byte(`{"todos":[{"id":"1","content":"done task","status":"done"}]}`)),
					}},
					{ID: "r1", Type: "function", Function: llm.ToolCallFunc{
						Name:      "read",
						Arguments: llm.ToolArguments([]byte(`{"path":"a.txt"}`)),
					}},
				},
			},
		},
	}}
	ag, _ := newTestAgent(t, llmClient, Options{Mode: ModeAsk})
	hist, res, err := ag.Run(context.Background(), nil, "read todos and file")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	msgs := toolMessages(hist)
	if len(msgs) != 2 {
		t.Fatalf("tool messages = %d, want 2", len(msgs))
	}
	if strings.Contains(msgs[0].Content, `"status":"error"`) {
		t.Fatalf("todowrite failed: %s", msgs[0].Content)
	}
	if !strings.Contains(msgs[1].Content, "hello") {
		t.Fatalf("read failed: %s", msgs[1].Content)
	}
	if len(res.Todos) != 1 || res.Todos[0].Content != "done task" {
		t.Fatalf("todos=%+v", res.Todos)
	}
}

func TestAgent_Run_TodoWriteTwoInProgress_ToolError(t *testing.T) {
	llmClient := &toolCallSequenceLLM{responses: []*llm.CompleteResponse{
		{
			Message: llm.Message{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{{
					ID:   "w1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name: "todowrite",
						Arguments: llm.ToolArguments([]byte(`{"todos":[
							{"id":"1","content":"a","status":"in_progress"},
							{"id":"2","content":"b","status":"in_progress"}
						]}`)),
					},
				}},
			},
		},
		{
			Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`},
		},
	}}
	ag, _ := newTestAgent(t, llmClient, Options{})
	hist, _, err := ag.Run(context.Background(), nil, "bad todos")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	msgs := toolMessages(hist)
	if len(msgs) == 0 {
		t.Fatal("expected tool error message")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(msgs[0].Content), &payload); err != nil {
		t.Fatalf("parse tool result: %v", err)
	}
	if payload["status"] != "error" {
		t.Fatalf("expected tool error, got %s", msgs[0].Content)
	}
}
