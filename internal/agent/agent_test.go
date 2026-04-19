package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/store"
	"github.com/orchestra/orchestra/internal/tools"
)

type scriptedLLM struct {
	steps []string
	i     int
}

func (s *scriptedLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_ = ctx
	_ = prompt
	return "{}", nil
}

func (s *scriptedLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_ = ctx
	_ = req
	if s.i >= len(s.steps) {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}, nil
	}
	out := s.steps[s.i]
	s.i++
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: out}}, nil
}

type policyRetryLLM struct {
	fileHash string
	stage    int
}

func (p *policyRetryLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_ = ctx
	_ = prompt
	return "{}", nil
}

func (p *policyRetryLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_ = ctx
	// Check for tool messages with denied/error status
	hasDeniedTool := false
	hasToolResult := false
	for _, m := range req.Messages {
		if m.Role == llm.RoleTool {
			hasToolResult = true
			// Check for denied status (may be formatted as "denied" or "status":"denied")
			if strings.Contains(m.Content, `"status":"denied"`) ||
				strings.Contains(m.Content, `"status": "denied"`) ||
				strings.Contains(m.Content, `"denied"`) {
				hasDeniedTool = true
			}
			// Also check for successful tool results
			if strings.Contains(m.Content, `"entries"`) || strings.Contains(m.Content, `"content"`) {
				// Tool succeeded (fs.read returns content, fs.list returns entries)
			}
		}
	}

	switch p.stage {
	case 0:
		if hasDeniedTool {
			return nil, fmt.Errorf("unexpected denied tool in first request")
		}
		p.stage++
		// Return tool call with tool_call_id for proper tool calling loop
		return &llm.CompleteResponse{
			Message: llm.Message{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: llm.ToolCallFunc{
							Name:      "exec.run",
							Arguments: llm.ToolArguments([]byte(`{"command":"echo","args":["hello"]}`)),
						},
					},
				},
			},
		}, nil

	case 1:
		if !hasDeniedTool {
			return nil, fmt.Errorf("expected denied tool message in retry request")
		}
		p.stage++
		// Return tool call for fs.read
		return &llm.CompleteResponse{
			Message: llm.Message{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_2",
						Type: "function",
						Function: llm.ToolCallFunc{
							Name:      "fs.read",
							Arguments: llm.ToolArguments([]byte(`{"path":"a.txt"}`)),
						},
					},
				},
			},
		}, nil

	case 2:
		if !hasToolResult {
			return nil, fmt.Errorf("expected tool result message in prompt")
		}
		p.stage++
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[{"type":"file.search_replace","path":"a.txt","search":"old","replace":"new","file_hash":"` + p.fileHash + `"}]}}`}}, nil

	default:
		// Be safe: keep returning a valid final if called again.
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[{"type":"file.search_replace","path":"a.txt","search":"old","replace":"new","file_hash":"` + p.fileHash + `"}]}}`}}, nil
	}
}

func TestAgent_Run_ToolCallThenFinal_Applies(t *testing.T) {
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

	llm := &scriptedLLM{
		steps: []string{
			`{"type":"tool_call","tool":{"name":"fs.read","input":{"path":"a.txt"}}}`,
			`{"type":"final","final":{"patches":[{"type":"file.search_replace","path":"a.txt","search":"old","replace":"new","file_hash":"` + h + `"}]}}`,
		},
	}

	ag, err := New(llm, v, tr, Options{
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

func TestAgent_Run_ExecDenied_IsRetriedInsideNextStep_AndDoesNotBurnStep(t *testing.T) {
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

	llm := &policyRetryLLM{fileHash: h}

	ag, err := New(llm, v, tr, Options{
		MaxSteps:          10,
		MaxInvalidRetries: 2, // enough for policy retry within nextStep
		AllowExec:         false,
		Apply:             true,
		Backup:            false,
	})
	if err != nil {
		t.Fatalf("New agent failed: %v", err)
	}

	_, res, err := ag.Run(context.Background(), nil, "replace old with new")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	// With proper tool calling loop: exec.run denied (step 1) -> fs.read (step 2) -> final (step 3)
	// The denied tool call is a proper step with assistant message + tool message, not a retry.
	if res.Steps != 3 {
		t.Fatalf("expected 3 steps (exec.run denied + fs.read + final). got=%d", res.Steps)
	}
	after, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(after) != "hello new world\n" {
		t.Fatalf("unexpected content: %q", string(after))
	}
}

func TestAgent_Run_InvalidJSON_Retries(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}

	fileHash := store.ComputeSHA256([]byte("x"))
	llm := &scriptedLLM{
		steps: []string{
			`not json`,
			`{"type":"tool_call","tool":{"name":"fs.list","input":{}}}`,
			fmt.Sprintf(`{"type":"final","final":{"patches":[{"type":"file.search_replace","path":"a.txt","search":"x","replace":"y","file_hash":%q}]}}`, fileHash),
		},
	}

	ag, err := New(llm, v, tr, Options{
		MaxSteps:          10,
		MaxInvalidRetries: 3,
		Apply:             true,
	})
	if err != nil {
		t.Fatalf("New agent failed: %v", err)
	}

	_, _, err = ag.Run(context.Background(), nil, "change x to y")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	after, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(after) != "y" {
		t.Fatalf("unexpected content: %q", string(after))
	}
}

func TestAgent_Run_ExecDenied_RepeatsThenStopsEarly(t *testing.T) {
	root := t.TempDir()

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}

	llm := &scriptedLLM{
		steps: []string{
			`{"type":"tool_call","tool":{"name":"exec.run","input":{"command":"echo","args":["hello"]}}}`,
			`{"type":"tool_call","tool":{"name":"exec.run","input":{"command":"echo","args":["hello"]}}}`,
			`{"type":"tool_call","tool":{"name":"exec.run","input":{"command":"echo","args":["hello"]}}}`,
		},
	}

	ag, err := New(llm, v, tr, Options{
		MaxSteps:             10,
		AllowExec:            false,
		MaxDeniedToolRepeats: 2,
	})
	if err != nil {
		t.Fatalf("New agent failed: %v", err)
	}

	_, _, err = ag.Run(context.Background(), nil, "run echo hello")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.InvalidLLMOutput {
		t.Fatalf("expected code %s, got %s", protocol.InvalidLLMOutput, coreErr.Code)
	}
}

func TestAgent_Run_FinalResolveFailure_RepeatsThenStopsEarly(t *testing.T) {
	root := t.TempDir()

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}

	// Empty "search" passes schema, but resolver rejects it.
	badFinal := `{"type":"final","final":{"patches":[{"type":"file.search_replace","path":"a.txt","search":"","replace":"x","file_hash":"sha256:deadbeef"}]}}`

	llm := &scriptedLLM{
		steps: []string{
			badFinal,
			badFinal,
			badFinal,
		},
	}

	ag, err := New(llm, v, tr, Options{
		MaxSteps:         10,
		MaxFinalFailures: 2,
	})
	if err != nil {
		t.Fatalf("New agent failed: %v", err)
	}

	_, _, err = ag.Run(context.Background(), nil, "do something")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.InvalidLLMOutput {
		t.Fatalf("expected code %s, got %s", protocol.InvalidLLMOutput, coreErr.Code)
	}
}
