package lessons

import (
	"sort"
	"strconv"
	"strings"

	"github.com/orchestra/orchestra/llm"
)

// ToolCountsFromHistory tallies tool names in assistant tool_calls (for lesson lines).
func ToolCountsFromHistory(hist []llm.Message) string {
	counts := map[string]int{}
	for _, m := range hist {
		if m.Role != llm.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			name := strings.ToLower(strings.TrimSpace(tc.Function.Name))
			if name == "" {
				continue
			}
			// Strip mcp wire prefix for readability.
			if i := strings.LastIndex(name, ":"); i >= 0 && strings.Count(name, ":") >= 2 {
				name = name[i+1:]
			}
			counts[name]++
		}
	}
	if len(counts) == 0 {
		return ""
	}
	keys := sortedKeys(counts)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"×"+strconv.Itoa(counts[k]))
	}
	return strings.Join(parts, " ")
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
