package agent

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/orchestra/orchestra/internal/llm"
)

const maxToolDescRunes = 140

// formatToolsCatalog builds an <available_tools> block from the live tool defs.
// Local models often ignore the tools[] schema; this puts names + short
// descriptions in the system prompt so the model knows what it can call.
func formatToolsCatalog(defs []llm.ToolDef) string {
	if len(defs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n<available_tools>\n")
	b.WriteString("Use ONLY these tools via real tool_calls (names must match exactly). Schema details are in tools[]; below is the catalog.\n\n")
	for _, d := range defs {
		name := strings.TrimSpace(d.Function.Name)
		if name == "" {
			continue
		}
		desc := compactToolDesc(d.Function.Description, maxToolDescRunes)
		if desc == "" {
			fmt.Fprintf(&b, "- %s\n", name)
			continue
		}
		fmt.Fprintf(&b, "- %s — %s\n", name, desc)
	}
	b.WriteString("\nGroups: read-only (ls/read/glob/grep/symbols/explore/…) may run in parallel; mutating (write/edit/bash/…) one per step.\n")
	b.WriteString("</available_tools>")
	return b.String()
}

func compactToolDesc(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// First sentence / line is enough for the catalog.
	if i := strings.IndexAny(s, ".\n"); i > 0 && i < maxRunes {
		s = strings.TrimSpace(s[:i+1])
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes-1]) + "…"
}
