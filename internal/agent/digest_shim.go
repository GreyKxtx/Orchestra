package agent

import (
	"encoding/json"

	"github.com/orchestra/orchestra/internal/agent/digest"
)

// DigestToolOutput shrinks large tool results for LLM history (OpenCode-style prune).
func DigestToolOutput(toolName string, toolInput json.RawMessage, raw []byte, budgetBytes int) (string, bool) {
	return digest.DigestToolOutput(toolName, toolInput, raw, budgetBytes)
}

// AutoMemoryNote builds a one-line session note after explore/grep.
func AutoMemoryNote(toolName string, input json.RawMessage, digestedOrRaw string) string {
	return digest.AutoMemoryNote(toolName, input, digestedOrRaw)
}

func normalizeToolName(name string) string {
	return digest.NormalizeToolName(name)
}
