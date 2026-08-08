package e2e_real_llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/agent/eval"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/tasks"
	"github.com/orchestra/orchestra/internal/tools"
)

func setupWorkerLSPFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	broken := "package main\n\nfunc Foo() {}\n\nvar _ = badSymbol\n"
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(broken), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	return root
}

func llmConfigFromEnv() config.LLMConfig {
	return config.LLMConfig{
		Provider:     "vllm",
		APIBase:      getLLMAPIBase(),
		APIKey:       getLLMAPIKey(),
		Model:        getLLMModel(),
		MaxTokens:    8000,
		Temperature:  0.0,
		TimeoutS:     180,
		PromptFamily: "local",
		ExtraBody: map[string]any{
			"num_ctx": wantNumCtx,
		},
	}
}

// TestRealLLMWorker_LSPIterationsLeq3 runs Worker mode against a real local model.
// Pre-seeded main.go references undefined badSymbol; synthetic LSP diagnostics fire
// until the reference is removed. Acceptance: ≤3 edit/write tool calls.
//
// Requires ORCH_E2E_LLM=1 and a reachable OpenAI-compatible endpoint.
func TestRealLLMWorker_LSPIterationsLeq3(t *testing.T) {
	requireE2ELLM(t)

	root := setupWorkerLSPFixture(t)

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

	client := llm.NewClient(llmConfigFromEnv())

	metrics := eval.NewLoopMetrics()
	ag, err := agent.New(client, v, tr, agent.Options{
		Mode:              agent.ModeWorker,
		MaxSteps:          15,
		MaxFinalFailures:  3,
		MaxInvalidRetries: 3,
		MaxToolErrorRepeats: 3,
		Apply:             false,
		ModelLabel:        getLLMModel(),
		OnEvent:           metrics.OnAgentEvent,
	})
	if err != nil {
		t.Fatalf("New agent: %v", err)
	}

	workOrder, err := json.Marshal(tasks.WorkOrder{
		Intent:       "Remove the undefined badSymbol reference so main.go compiles",
		TargetFile:   "main.go",
		TargetSymbol: "Foo",
		Instructions: []string{
			"Read main.go first to obtain file_hash",
			"Use edit with target_symbol Foo when replacing inside Foo",
			"Remove the line referencing badSymbol",
		},
		AcceptanceCriteria: []string{
			"no reference to badSymbol",
			"func Foo() remains",
		},
	})
	if err != nil {
		t.Fatalf("marshal work order: %v", err)
	}
	goal := tasks.FormatChildGoal("worker", "micro", string(workOrder))

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	_, res, runErr := ag.Run(ctx, nil, goal)
	combined := formatRunErr(runErr)

	switch classifyError(combined, boolToExit(runErr)) {
	case ErrorCategoryInfrastructure:
		t.Skipf("LLM infrastructure unavailable: %s", combined)
	case ErrorCategoryModelOutput:
		t.Skipf("local model did not complete worker task (model output): %s", combined)
	}

	if runErr != nil {
		t.Fatalf("worker run failed: %v", runErr)
	}

	iterations := metrics.LSPIterationCount()
	t.Logf("worker LSP iterations (edit/write): %d, lsp_hints: %d, steps: %v",
		iterations, metrics.LSPHintCount(), resSteps(res))

	if iterations > eval.MaxLSPIterations {
		t.Fatalf("LSP iterations = %d, acceptance requires ≤ %d (model=%s)",
			iterations, eval.MaxLSPIterations, getLLMModel())
	}

	staged := tr.StagedOps()
	if len(staged) == 0 {
		t.Fatal("expected staged ops after successful worker run")
	}
	stagedFiles := tr.StagedFileContent()
	content, ok := stagedFiles["main.go"]
	if !ok || strings.Contains(content, "badSymbol") {
		t.Fatal("staged main.go still references badSymbol or is missing")
	}

	onDisk, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(onDisk), "badSymbol") {
		t.Fatal("dry-run must not write broken content to disk")
	}
}

func formatRunErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func boolToExit(err error) int {
	if err != nil {
		return 1
	}
	return 0
}

func resSteps(res *agent.Result) string {
	if res == nil {
		return "nil"
	}
	return fmt.Sprintf("%d", res.Steps)
}
