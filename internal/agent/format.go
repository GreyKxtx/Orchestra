package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/protocol"
)

// Format helpers for tool-call observations the agent inserts back into
// LLM history (TOOL_OK / TOOL_ERR / TOOL_DENIED), validator-error
// messages, and resolver/apply error compactors with structured Data
// context. Extracted from agent.go in C3 (architecture audit) so the
// formatting concerns live separately from the agent's control flow.

func formatToolOK(name string, input json.RawMessage, output json.RawMessage) string {
	return "TOOL_OK " + name + "\ninput=" + compactJSON(input) + "\noutput=" + compactJSON(output)
}

func formatToolError(name string, input json.RawMessage, err error) string {
	code := ""
	if pe, ok := protocol.AsError(err); ok {
		code = string(pe.Code)
	}
	if code != "" {
		return "TOOL_ERR " + name + " code=" + code + "\ninput=" + compactJSON(input) + "\nerror=" + formatErr(err)
	}
	return "TOOL_ERR " + name + "\ninput=" + compactJSON(input) + "\nerror=" + formatErr(err)
}

func formatToolDenied(name string, input json.RawMessage, reason string) string {
	return "TOOL_DENIED " + name + "\ninput=" + compactJSON(input) + "\nreason=" + reason
}

func formatValidatorError(msg string, raw string) string {
	return "VALIDATION_ERROR\nmessage=" + msg + "\nraw=" + truncate(strings.TrimSpace(raw), 400)
}

// formatValidatorErrorCompact returns a compact error message without raw JSON to avoid prompt bloat.
func formatValidatorErrorCompact(msg string) string {
	return "VALIDATION_ERROR\nmessage=" + msg + "\nFix the JSON to match the schema (tool call or PatchSet)."
}

// formatPolicyDeniedCompact returns a compact policy denial message.
func formatPolicyDeniedCompact(toolName string) string {
	return fmt.Sprintf("TOOL_DENIED %s\nreason=requires explicit permission\nUse only tools from the advertised list.", toolName)
}

func formatResolveError(err error) string {
	return "RESOLVE_ERROR\nerror=" + formatErr(err)
}

// errorDataString pulls a string field out of a protocol.Error's Data
// payload. Returns "" when the field is missing or not a string. Used by
// the compact error formatters to surface the resolver's structured
// context (path, matches count, hash) to the LLM.
func errorDataString(pe *protocol.Error, key string) string {
	if pe == nil || pe.Data == nil {
		return ""
	}
	m, ok := pe.Data.(map[string]any)
	if !ok {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func errorDataInt(pe *protocol.Error, key string) int {
	if pe == nil || pe.Data == nil {
		return 0
	}
	m, ok := pe.Data.(map[string]any)
	if !ok {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return 0
}

// errorDataInts pulls an []int field (possibly serialised as []float64
// after a JSON round-trip). Used for the AmbiguousMatch match_lines list.
func errorDataInts(pe *protocol.Error, key string) []int {
	if pe == nil || pe.Data == nil {
		return nil
	}
	m, ok := pe.Data.(map[string]any)
	if !ok {
		return nil
	}
	switch v := m[key].(type) {
	case []int:
		return v
	case []any:
		out := make([]int, 0, len(v))
		for _, x := range v {
			switch n := x.(type) {
			case int:
				out = append(out, n)
			case float64:
				out = append(out, int(n))
			}
		}
		return out
	}
	return nil
}

// formatResolveErrorCompact returns a compact resolve error message. H1
// fix: include path from the resolver's Data payload so the model knows
// which file to re-read instead of guessing across a multi-file patch.
// L3 in audit ledger: hints are English — most chat-tuned LLMs respond
// more reliably to English error contexts. The path/code tokens are
// the load-bearing structural fields.
func formatResolveErrorCompact(err error) string {
	if pe, ok := protocol.AsError(err); ok {
		path := errorDataString(pe, "path")
		if path != "" {
			return fmt.Sprintf("RESOLVE_ERROR code=%s path=%s\nRe-read the file (fs.read) and update file_hash in the patch.", pe.Code, path)
		}
		return fmt.Sprintf("RESOLVE_ERROR code=%s\nRe-read the file (fs.read) and update file_hash in the patch.", pe.Code)
	}
	return "RESOLVE_ERROR code=unknown\nerror=" + err.Error() + "\nRe-read the file (fs.read) and update file_hash in the patch."
}

func formatApplyError(err error) string {
	return "APPLY_ERROR\nerror=" + formatErr(err)
}

// formatApplyErrorCompact returns a compact apply error message with
// actionable hint. H1 fix (audit ledger): include path + matches count
// from the resolver's structured Data payload so the LLM gets to pinpoint
// the failing file in a multi-file patch and knows how many ambiguous
// hits it needs to disambiguate. Previously the hint was a single
// "файл изменился" line with zero specifics — the biggest single driver
// of retry-loop bloat in observed runs.
func formatApplyErrorCompact(err error, code protocol.ErrorCode) string {
	pe, _ := protocol.AsError(err)
	path := errorDataString(pe, "path")
	pathSuffix := ""
	if path != "" {
		pathSuffix = " path=" + path
	}
	switch code {
	case protocol.StaleContent:
		return "APPLY_ERROR code=StaleContent" + pathSuffix +
			"\nFile changed on disk. Re-read it (fs.read) and update the patch with the new file_hash."
	case protocol.AmbiguousMatch:
		matches := errorDataInt(pe, "matches")
		linesPart := ""
		if lines := errorDataInts(pe, "match_lines"); len(lines) > 0 {
			// M13 in audit ledger: surface first N match line numbers so
			// the LLM picks disambiguating context from the actual hits.
			parts := make([]string, 0, len(lines))
			for _, ln := range lines {
				parts = append(parts, fmt.Sprintf("%d", ln))
			}
			linesPart = " lines=" + strings.Join(parts, ",")
		}
		if matches > 0 {
			return fmt.Sprintf("APPLY_ERROR code=AmbiguousMatch%s matches=%d%s\nSearch block matched %d locations. Disambiguate: add 2-3 lines of context before or after the existing search.", pathSuffix, matches, linesPart, matches)
		}
		return "APPLY_ERROR code=AmbiguousMatch" + pathSuffix + linesPart +
			"\nSearch block is ambiguous. Add more surrounding context to make it unique."
	}
	return "APPLY_ERROR code=unknown\nerror=" + formatErr(err)
}

// maxLSPErrorsInjected caps how many diagnostics are pasted back into the
// agent's history after a write/edit. A syntax error that cascades into
// hundreds of parser errors (large generated TS file, broken Go go.mod)
// would otherwise blow MaxPromptBytes and force aggressive truncation —
// the model loses useful context and the diagnostics themselves still
// don't all fit. H2 in audit ledger.
const maxLSPErrorsInjected = 20

// extractLSPErrors parses a write/edit tool response JSON and returns a
// user-facing hint if diagnostics with severity "error" are present.
// Capped at maxLSPErrorsInjected entries — additional errors are
// summarised as "...N more" so the model knows the report is partial.
// Returns "" if there are no errors (warnings and info are silently ignored).
func extractLSPErrors(out json.RawMessage) string {
	if len(out) == 0 {
		return ""
	}
	var resp struct {
		Diagnostics []struct {
			Severity  string `json:"severity"`
			Message   string `json:"message"`
			StartLine int    `json:"start_line"`
			StartCol  int    `json:"start_col"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(out, &resp); err != nil || len(resp.Diagnostics) == 0 {
		return ""
	}
	var errs []string
	total := 0
	for _, d := range resp.Diagnostics {
		if d.Severity != "error" {
			continue
		}
		total++
		if len(errs) < maxLSPErrorsInjected {
			errs = append(errs, fmt.Sprintf("  line %d:%d: %s", d.StartLine, d.StartCol, d.Message))
		}
	}
	if total == 0 {
		return ""
	}
	body := strings.Join(errs, "\n")
	if total > maxLSPErrorsInjected {
		body += fmt.Sprintf("\n  ...and %d more errors (showing first %d)", total-maxLSPErrorsInjected, maxLSPErrorsInjected)
	}
	// N6 in audit ledger (Sprint 6): framed as a denial-style hint so the
	// model treats it as a constraint, not a side note. Identical re-call
	// is also blocked at the dedup gate (agent.go:885), but the soft form
	// here nudges the model to think before retrying with cosmetic tweaks.
	return "LSP_ERRORS — the next write/edit on this file with the same errors will be blocked. File was written but has compilation errors:\n" +
		body +
		"\nDiagnose the cause (read + lsp.hover / lsp.references) and fix it before another write/edit. Cosmetic re-writes on the same lines will not help."
}

func formatErr(err error) string {
	if err == nil {
		return ""
	}
	if pe, ok := protocol.AsError(err); ok {
		b, _ := json.Marshal(pe)
		return string(b)
	}
	return err.Error()
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return truncate(string(raw), 400)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return truncate(string(raw), 400)
	}
	return string(b)
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + " ...(truncated)"
}

// truncateID truncates an ID string for logging.
func truncateID(id string, maxLen int) string {
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen] + "..."
}
