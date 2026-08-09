package e2e_agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

// dryRunLSPFixLLM scripts: bad edit → LSP hint → fix edit → final (dry-run).
type dryRunLSPFixLLM struct {
	step        int
	initialHash string
	badHash     string
	sawLSPHint  bool
}

func newDryRunLSPFixLLM(initialHash string) *dryRunLSPFixLLM {
	badContent := "package main\n\nfunc Foo() {}\n\nvar _ = badSymbol\n"
	return &dryRunLSPFixLLM{
		initialHash: initialHash,
		badHash:     cache.ComputeSHA256([]byte(badContent)),
	}
}

func (l *dryRunLSPFixLLM) Plan(_ context.Context, _ string) (string, error) { return "{}", nil }

func (l *dryRunLSPFixLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
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
				l.sawLSPHint = true
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

func toolResultFileHash(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != llm.RoleTool {
			continue
		}
		var resp struct {
			FileHash string `json:"file_hash"`
		}
		if err := json.Unmarshal([]byte(messages[i].Content), &resp); err != nil {
			continue
		}
		if resp.FileHash != "" {
			return resp.FileHash
		}
	}
	return ""
}

// TestAgent_E2E_DryRun_LSPErrorFixApply verifies the semantic dry-run pipeline:
// dry-run edit surfaces LSP diagnostics, agent receives LSP_ERRORS hint, fix stages
// cleanly, disk stays untouched until explicit FSApplyOps.
func TestAgent_E2E_DryRun_LSPErrorFixApply(t *testing.T) {
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
		ForceDiagnosticsForTest: []lsp.ToolDiagnostic{
			{StartLine: 3, StartCol: 1, Severity: "error", Message: "undefined: badSymbol"},
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	mockLLM := newDryRunLSPFixLLM(initialHash)

	ag, err := agent.New(mockLLM, v, tr, agent.Options{
		MaxSteps: 12,
		Apply:    false,
		Backup:   false,
	})
	if err != nil {
		t.Fatalf("New agent: %v", err)
	}

	_, res, err := ag.Run(context.Background(), nil, "fix main.go")
	if err != nil {
		t.Fatalf("Run dry-run: %v", err)
	}
	if !mockLLM.sawLSPHint {
		t.Fatal("expected LSP_ERRORS hint after bad dry-run edit")
	}
	if res == nil || !res.Applied {
		// dry-run: Applied=false is expected
	}
	if res != nil && res.ApplyResponse == nil {
		t.Fatal("expected dry-run ApplyResponse with staged diffs")
	}

	onDisk, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != original {
		t.Fatalf("dry-run must not modify disk; got %q", string(onDisk))
	}

	staged := tr.StagedOps()
	if len(staged) == 0 {
		t.Fatal("expected staged ops after fix edit")
	}

	applyResp, err := tr.FSApplyOps(context.Background(), tools.FSApplyOpsRequest{
		Ops:    staged,
		DryRun: false,
		Backup: false,
	})
	if err != nil {
		t.Fatalf("FSApplyOps apply: %v", err)
	}
	if len(applyResp.Diffs) == 0 {
		t.Fatal("expected diffs from apply")
	}

	after, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimRight(string(after), "\n") != strings.TrimRight(original, "\n") {
		t.Fatalf("after apply expected restored content %q, got %q", original, string(after))
	}
}
