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
	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/internal/tasks"
	"github.com/orchestra/orchestra/internal/tools"
)

// orchestraLeadLLM delegates to Worker via sync task; never edits production files.
type orchestraLeadLLM struct {
	step      int
	workOrder string
	taskCalls int
	editCalls int
}

func newOrchestraLeadLLM(workOrder string) *orchestraLeadLLM {
	return &orchestraLeadLLM{workOrder: workOrder}
}

func (l *orchestraLeadLLM) Plan(_ context.Context, _ string) (string, error) { return "{}", nil }

func (l *orchestraLeadLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	for _, m := range req.Messages {
		if m.Role != llm.RoleAssistant || len(m.ToolCalls) == 0 {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == "edit" || tc.Function.Name == "write" {
				l.editCalls++
			}
		}
	}

	switch l.step {
	case 0:
		l.step++
		l.taskCalls++
		input, _ := json.Marshal(map[string]any{
			"description":   "fix compile error in main.go",
			"subagent_type": "worker",
			"tier":          "micro",
			"prompt":        l.workOrder,
		})
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:   "call_task_worker",
				Type: "function",
				Function: llm.ToolCallFunc{
					Name:      "task",
					Arguments: llm.ToolArguments(input),
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

func workerLSPFixture(t *testing.T) (root, initialHash string) {
	t.Helper()
	root = t.TempDir()
	original := "package main\n\nfunc Foo() {}\n"
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(original), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return root, cache.ComputeSHA256([]byte(original))
}

func newLSPFixRunner(t *testing.T, root string) *tools.Runner {
	t.Helper()
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
	return tr
}

func workerFixWorkOrderJSON() string {
	wo, _ := json.Marshal(tasks.WorkOrder{
		Intent:             "Fix compile error in main.go after bad edit",
		TargetFile:         "main.go",
		AcceptanceCriteria: []string{"no badSymbol", "func Foo() preserved"},
	})
	return string(wo)
}

// TestOrchestra_E2E_LeadSpawnsWorker_LSPIterations verifies P9: orchestra Lead
// delegates via task(worker) and Worker completes LSP loop within ≤3 edits.
func TestOrchestra_E2E_LeadSpawnsWorker_LSPIterations(t *testing.T) {
	root, initialHash := workerLSPFixture(t)
	tr := newLSPFixRunner(t, root)

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	workerLLM := newWorkerLSPFixLLM(initialHash)
	workerMetrics := eval.NewLoopMetrics()

	taskRunner := tasks.New(workerLLM, v, tr, tasks.ChildAgentConfig{
		OnChildEvent: workerMetrics.OnAgentEvent,
	})
	t.Cleanup(func() { taskRunner.Close() })

	leadLLM := newOrchestraLeadLLM(workerFixWorkOrderJSON())

	ag, err := agent.New(leadLLM, v, tr, agent.Options{
		Mode:          agent.ModeOrchestra,
		MaxSteps:      12,
		Apply:         false,
		SubtaskRunner: taskRunner,
	})
	if err != nil {
		t.Fatalf("New agent: %v", err)
	}

	_, _, err = ag.Run(context.Background(), nil, "fix main.go compile error via worker")
	if err != nil {
		t.Fatalf("orchestra run: %v", err)
	}

	if leadLLM.editCalls != 0 {
		t.Fatalf("Lead must not call edit/write on production files, got %d", leadLLM.editCalls)
	}
	if leadLLM.taskCalls != 1 {
		t.Fatalf("Lead task calls = %d, want 1", leadLLM.taskCalls)
	}
	if got := workerMetrics.LSPIterationCount(); got > eval.MaxLSPIterations {
		t.Fatalf("Worker LSP iterations = %d, want ≤ %d", got, eval.MaxLSPIterations)
	}
	if len(tr.StagedOps()) == 0 {
		t.Fatal("expected staged ops from Worker")
	}
}
