package lsp

import "testing"

// gopls answers "could not import strings (missing metadata for import of
// \"strings\")" for the moment between an edit that adds an import and its
// reload of the package's metadata. A model took it for its own mistake and
// reverted a correct fix (eval debug_root_cause, 2026-10-03). A real missing
// package reads "no required module provides package" and stays.
func TestDiagsToTool_DropsGoplsMissingMetadata(t *testing.T) {
	got := diagsToTool([]Diagnostic{
		{Severity: SeverityError, Source: "compiler", Message: `could not import strings (missing metadata for import of "strings")`},
		{Severity: SeverityError, Source: "compiler", Message: `could not import example.com/x (no required module provides package "example.com/x")`},
	})
	if len(got) != 1 || got[0].Message == "" || got[0].Message[:20] != "could not import exa" {
		t.Fatalf("diagnostics = %+v, want only the real missing package", got)
	}
}
