package ckg

import (
	"fmt"
	"strings"
)

// FormatNodesForPrompt renders nodes as a compact <ckg_context> block.
// Returns an empty string if nodes is empty.
// The total output (including XML tags) is capped at maxBytes.
func FormatNodesForPrompt(nodes []Node, maxBytes int) string {
	if len(nodes) == 0 {
		return ""
	}
	const header = "<ckg_context>\n"
	const footer = "</ckg_context>"
	if maxBytes <= len(header)+len(footer) {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(header)
	for _, n := range nodes {
		line := fmt.Sprintf("%s (%s, L%d-%d)\n", n.FQN, n.Kind, n.LineStart, n.LineEnd)
		if sb.Len()+len(line)+len(footer) > maxBytes {
			break
		}
		sb.WriteString(line)
	}
	sb.WriteString(footer)
	return sb.String()
}
