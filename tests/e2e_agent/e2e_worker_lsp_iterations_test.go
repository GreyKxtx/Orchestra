package e2e_agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/agent/eval"
	"github.com/orchestra/orchestra/internal/cache"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/tasks"
	"github.com/orchestra/orchestra/internal/tools"
)

// workerLSPFixLLM scripts Worker mode: bad edit → LSP hint → fix → final (dry-run).
type workerLSPFixLLM struct {
	step        int
	initialHash string
	badHash     string
}

func newWorkerLSPFixLLM(initialHash string) *workerLSPFixLLM {
	badContent := "package main\n\nfunc Foo() {}\n\nvar _ = badSymbol\n"
	return &workerLSPFixLLM{
		initialHash: initialHash,
		badHash:     cache.ComputeSHA256([]byte(badContent)),
	}
}

func (l *workerLSPFixLLM) Plan(_ context.Context, _ string) (string, error) { return "{}", nil }

func (l *workerLSPFixLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	switch l.step {
	case 0:
		l.step++
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:   "call_edit_bad",
				Type: "function",
				Function: llm.ToolCallFunc{
					Name: "edit",
					Arguments: llm.ToolArguments([]byte(
						`{"path":"main.go","search":"func Foo() {}","replace":"func Foo() {}\n\nvar _ = badSymbol","file_hash":"` + l.initialHash + `"}`,
					)),
				},
			}},
		}}, nil
	case 1:
		l.step++
		for _, m := range req.Messages {
			if m.Role == llm.RoleUser && strings.Contains(m.Content, "LSP_ERRORS") {
				break
			}
		}
		hash := l.badHash
		if h := toolResultFileHash(req.Messages); h != "" {
			hash = h
		}
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:   "call_edit_fix",
				Type: "function",
				Function: llm.ToolCallFunc{
					Name: "edit",
					Arguments: llm.ToolArguments([]byte(
						`{"path":"main.go","search":"\n\nvar _ = badSymbol\n","replace":"","file_hash":"` + hash + `"}`,
					)),
				},
			}},
		}}, nil
	default:
		l.step++
		return &llm.CompleteResponse{Message: llm.Message{
			Role:    llm.RoleAssistant,
			Content: `{"type":"final","final":{"patches":[]}}`,
		}}, nil
	}
}

// TestWorker_E2E_LSPIterationsLeq3 verifies Worker mode completes a typical
// LSP feedback loop (bad edit → LSP_ERRORS → fix) within ≤3 edit iterations.
func TestWorker_E2E_LSPIterationsLeq3(t *testing.T) {
	root := t.TempDir()
	original := "package main\n\nfunc Foo() {}\n"
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(original), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	initialHash := cache.ComputeSHA256([]byte(original))

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{
		DryRun: true,
		ForceDiagnosticsHook: func(content string) []lsp.ToolDiagnostic {
			if strings.Contains(content, "badSymbol") {
				return []lsp.ToolDiagnostic{
					{StartLine: 4, StartCol: 1, Severity: "error", Message: "undefined: badSymbol"},
				}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	metrics := eval.NewLoopMetrics()
	mockLLM := newWorkerLSPFixLLM(initialHash)

	ag, err := agent.New(mockLLM, v, tr, agent.Options{
		Mode:     agent.ModeWorker,
		MaxSteps: 12,
		Apply:    false,
		OnEvent:  metrics.OnAgentEvent,
	})
	if err != nil {
		t.Fatalf("New agent: %v", err)
	}

	workOrder, err := json.Marshal(tasks.WorkOrder{
		Intent:             "Fix compile error in main.go",
		TargetFile:         "main.go",
		AcceptanceCriteria: []string{"no badSymbol", "func Foo() preserved"},
	})
	if err != nil {
		t.Fatalf("marshal work order: %v", err)
	}
	goal := tasks.FormatChildGoal("worker", "micro", string(workOrder))

	_, _, err = ag.Run(context.Background(), nil, goal)
	if err != nil {
		t.Fatalf("Run worker dry-run: %v", err)
	}

	if got := metrics.LSPIterationCount(); got > eval.MaxLSPIterations {
		t.Fatalf("LSP iterations (edit/write count) = %d, want ≤ %d", got, eval.MaxLSPIterations)
	}
	if got := metrics.LSPHintCount(); got < 1 {
		t.Fatalf("expected at least one LSP_ERRORS hint, got %d", got)
	}
	if len(tr.StagedOps()) == 0 {
		t.Fatal("expected staged ops after worker fix")
	}

	onDisk, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != original {
		t.Fatalf("dry-run must not modify disk; got %q", string(onDisk))
	}
}
