package e2e_real_llm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/agent/eval"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/tasks"
	toolsrunner "github.com/orchestra/orchestra/internal/tools"
)

// orchestraLeadForRealWorker delegates to a real local Worker model (P9 gated).
type orchestraLeadForRealWorker struct {
	workOrder string
	step      int
}

func (l *orchestraLeadForRealWorker) Plan(_ context.Context, _ string) (string, error) {
	return "{}", nil
}

func (l *orchestraLeadForRealWorker) Complete(_ context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
	switch l.step {
	case 0:
		l.step++
		input, _ := json.Marshal(map[string]any{
			"description":   "fix compile error",
			"subagent_type": "worker",
			"tier":          "micro",
			"prompt":        l.workOrder,
		})
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:   "call_task",
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

// TestRealLLMOrchestra_LeadSpawnsWorker_LSPIterationsLeq3 runs P9 with scripted
// Lead and real local Worker model.
func TestRealLLMOrchestra_LeadSpawnsWorker_LSPIterationsLeq3(t *testing.T) {
	requireE2ELLM(t)

	root := setupWorkerLSPFixture(t)

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	tr, err := toolsrunner.NewRunner(root, toolsrunner.RunnerOptions{
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

	workerClient := llm.NewClient(llmConfigFromEnv())
	workerMetrics := eval.NewLoopMetrics()

	taskRunner := tasks.New(workerClient, v, tr, tasks.ChildAgentConfig{
		OnChildEvent:     workerMetrics.OnAgentEvent,
		MaxWorkerRetries: 3,
		LLMStepTimeout:   time.Duration(llmConfigFromEnv().TimeoutS) * time.Second,
	})
	t.Cleanup(func() { taskRunner.Close() })

	workOrder, _ := json.Marshal(tasks.WorkOrder{
		Intent:       "Remove undefined badSymbol from main.go",
		TargetFile:   "main.go",
		Instructions: []string{"Read main.go first", "Remove badSymbol line"},
	})
	lead := &orchestraLeadForRealWorker{workOrder: string(workOrder)}

	ag, err := agent.New(lead, v, tr, agent.Options{
		Mode:          agent.ModeOrchestra,
		MaxSteps:      12,
		Apply:         false,
		SubtaskRunner: taskRunner,
	})
	if err != nil {
		t.Fatalf("New agent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_, _, runErr := ag.Run(ctx, nil, "fix main.go via worker")
	combined := formatRunErr(runErr)
	switch classifyError(combined, boolToExit(runErr)) {
	case ErrorCategoryInfrastructure:
		t.Skipf("LLM infrastructure unavailable: %s", combined)
	case ErrorCategoryModelOutput:
		t.Skipf("worker model failed: %s", combined)
	}
	if runErr != nil {
		t.Fatalf("orchestra run: %v", runErr)
	}

	if workerMetrics.LSPIterationCount() > eval.MaxLSPIterations {
		t.Fatalf("Worker iterations = %d, want ≤ %d", workerMetrics.LSPIterationCount(), eval.MaxLSPIterations)
	}
	staged := tr.StagedFileContent()
	if content, ok := staged["main.go"]; !ok || strings.Contains(content, "badSymbol") {
		t.Fatal("worker did not fix staged main.go")
	}
	onDisk, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), "badSymbol") {
		t.Fatal("dry-run must not apply worker fix to disk yet")
	}
}
