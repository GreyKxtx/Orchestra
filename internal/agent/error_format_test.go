package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/protocol"
)

// TestFormatApplyErrorCompact_StaleContentIncludesPath is the H1 regression:
// the hint must include the path so a multi-file patch failure tells the
// LLM exactly which file to re-read, not "some file changed somewhere."
func TestFormatApplyErrorCompact_StaleContentIncludesPath(t *testing.T) {
	err := protocol.NewError(protocol.StaleContent, "stale", map[string]any{
		"path":     "internal/foo/bar.go",
		"fileHash": "deadbeef",
	})
	hint := formatApplyErrorCompact(err, protocol.StaleContent)
	if !strings.Contains(hint, "internal/foo/bar.go") {
		t.Errorf("path missing from hint: %q", hint)
	}
	if !strings.Contains(hint, "StaleContent") {
		t.Errorf("code missing: %q", hint)
	}
}

// TestFormatApplyErrorCompact_AmbiguousMatchIncludesMatchCount: H1 fix
// also surfaces matches count so the model knows how much disambiguating
// context to add.
func TestFormatApplyErrorCompact_AmbiguousMatchIncludesMatchCount(t *testing.T) {
	err := protocol.NewError(protocol.AmbiguousMatch, "ambig", map[string]any{
		"path":    "main.go",
		"matches": 5,
		"search":  "func init",
	})
	hint := formatApplyErrorCompact(err, protocol.AmbiguousMatch)
	if !strings.Contains(hint, "main.go") {
		t.Errorf("path missing: %q", hint)
	}
	if !strings.Contains(hint, "matches=5") {
		t.Errorf("matches count missing: %q", hint)
	}
}

// TestFormatApplyErrorCompact_PlainErrorFallback: non-protocol errors
// still get a reasonable message without crashing.
func TestFormatApplyErrorCompact_PlainErrorFallback(t *testing.T) {
	hint := formatApplyErrorCompact(errors.New("disk full"), protocol.ErrorCode("Other"))
	if !strings.Contains(hint, "disk full") {
		t.Errorf("underlying err missing: %q", hint)
	}
}

// TestExtractLSPErrors_CapsToTwentyWithSummary is the H2 regression:
// a cascade of 100 errors must be truncated and the LLM must see how
// many were dropped — otherwise the user can't tell whether the report
// was partial.
func TestExtractLSPErrors_CapsToTwentyWithSummary(t *testing.T) {
	diags := []map[string]any{}
	for i := 0; i < 100; i++ {
		diags = append(diags, map[string]any{
			"severity":   "error",
			"message":    "syntax error",
			"start_line": i + 1,
			"start_col":  1,
		})
	}
	payload, _ := json.Marshal(map[string]any{"diagnostics": diags})
	hint := extractLSPErrors(payload)
	if hint == "" {
		t.Fatal("expected non-empty hint")
	}
	// Should mention 20 lines + summary "...and 80 more errors".
	if strings.Count(hint, "line ") != maxLSPErrorsInjected {
		t.Errorf("expected %d line entries, got %d in %q", maxLSPErrorsInjected, strings.Count(hint, "line "), hint)
	}
	if !strings.Contains(hint, "80 more") {
		t.Errorf("summary missing: %q", hint)
	}
}

// TestExtractLSPErrors_BelowCapNoSummary: when errors fit, no summary tail.
func TestExtractLSPErrors_BelowCapNoSummary(t *testing.T) {
	diags := []map[string]any{
		{"severity": "error", "message": "a", "start_line": 1, "start_col": 1},
		{"severity": "error", "message": "b", "start_line": 2, "start_col": 1},
	}
	payload, _ := json.Marshal(map[string]any{"diagnostics": diags})
	hint := extractLSPErrors(payload)
	// The "N more errors" tail is the summary we want to assert is absent.
	if strings.Contains(hint, "more errors") {
		t.Errorf("unexpected summary line for small error list: %q", hint)
	}
}

// TestExtractLSPErrors_WarningsIgnored: warnings/info must not produce a hint.
func TestExtractLSPErrors_WarningsIgnored(t *testing.T) {
	diags := []map[string]any{
		{"severity": "warning", "message": "w", "start_line": 1, "start_col": 1},
		{"severity": "info", "message": "i", "start_line": 2, "start_col": 1},
	}
	payload, _ := json.Marshal(map[string]any{"diagnostics": diags})
	if hint := extractLSPErrors(payload); hint != "" {
		t.Errorf("expected empty, got %q", hint)
	}
}
