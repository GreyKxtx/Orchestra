package view

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestBlockToolBody_EditWithDiagnostics(t *testing.T) {
	tb := state.ToolBlock{
		Name:     "edit",
		Status:   state.ToolBlockCompleted,
		ArgsRaw:  `{"path":"main.go","search":"bad","replace":"good"}`,
		Result:   `{"path":"main.go","file_hash":"abc"}`,
		Expanded: true,
		Diagnostics: []state.ToolDiagnostic{
			{StartLine: 4, StartCol: 1, Severity: "error", Message: "undefined: badSymbol", Source: "compiler"},
		},
	}
	body := blockToolBody(tb)
	if !strings.Contains(body, "LSP diagnostics:") {
		t.Fatalf("missing diagnostics header: %q", body)
	}
	if !strings.Contains(body, "undefined: badSymbol") {
		t.Fatalf("missing diag message: %q", body)
	}
}

func TestToolInline_EditDiagnosticsSuffix(t *testing.T) {
	tb := state.ToolBlock{
		Name:   "edit",
		Status: state.ToolBlockCompleted,
		Diagnostics: []state.ToolDiagnostic{
			{Severity: "error", Message: "undefined: x"},
		},
	}
	line := renderInlineTool(tb, 80, 0)
	if !strings.Contains(line, "1 LSP error") {
		t.Fatalf("inline suffix missing: %q", line)
	}
}
