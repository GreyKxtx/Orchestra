package view

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestDiagnosticsInlineSuffix_Errors(t *testing.T) {
	got := diagnosticsInlineSuffix([]state.ToolDiagnostic{
		{Severity: "error", Message: "undefined: x"},
		{Severity: "error", Message: "other"},
	})
	if got != " · 2 LSP errors" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatDiagnosticsBlock(t *testing.T) {
	got := formatDiagnosticsBlock([]state.ToolDiagnostic{
		{StartLine: 3, StartCol: 5, Severity: "error", Message: "undefined: Foo", Source: "compiler"},
	})
	if !strings.Contains(got, "LSP diagnostics:") {
		t.Fatalf("missing header: %q", got)
	}
	if !strings.Contains(got, "3:5") || !strings.Contains(got, "undefined: Foo") {
		t.Fatalf("missing diag line: %q", got)
	}
}
