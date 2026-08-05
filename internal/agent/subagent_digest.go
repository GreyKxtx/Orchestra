package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/llm"
)

// FormatSubagentResult collapses a child agent run into one structured summary
// for the parent (explore subagents: grep/explore/read → single digest).
func FormatSubagentResult(subagentType, goal string, history []llm.Message, taskResult string, digestBudget int) string {
	if strings.TrimSpace(subagentType) == "" {
		subagentType = "explore"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[subagent:%s]\n", subagentType)
	if g := strings.TrimSpace(goal); g != "" {
		b.WriteString("## Goal\n")
		b.WriteString(g)
		b.WriteByte('\n')
	}

	findings := extractSubagentFindings(history, digestBudget)
	if len(findings) > 0 {
		b.WriteString("## Findings\n")
		for _, f := range findings {
			b.WriteString("- ")
			b.WriteString(f)
			b.WriteByte('\n')
		}
	}
	if tr := strings.TrimSpace(taskResult); tr != "" {
		b.WriteString("## Result\n")
		if digestBudget > 0 && len(tr) > digestBudget*3 {
			digested, ok := DigestToolOutput("task_result", nil, []byte(tr), digestBudget*2)
			if ok {
				tr = digested
			} else {
				tr = truncate(tr, digestBudget*3)
			}
		}
		b.WriteString(tr)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func extractSubagentFindings(history []llm.Message, digestBudget int) []string {
	if len(history) <= 2 {
		return nil
	}
	atoms := buildHistoryAtoms(history[2:])
	var out []string
	seen := map[string]bool{}
	for _, a := range atoms {
		meta := toolCallMapFromAtom(a)
		for _, m := range a.msgs {
			if m.Role != llm.RoleTool {
				continue
			}
			cm, ok := meta[m.ToolCallID]
			if !ok {
				continue
			}
			name := normalizeToolName(cm.name)
			switch name {
			case "explore", "grep", "read", "semantic_search", "symbols":
			default:
				continue
			}
			line := summarizeToolFinding(name, json.RawMessage(cm.input), m.Content, digestBudget)
			if line == "" || seen[line] {
				continue
			}
			seen[line] = true
			out = append(out, line)
		}
	}
	const maxFindings = 12
	if len(out) > maxFindings {
		out = out[len(out)-maxFindings:]
	}
	return out
}

func summarizeToolFinding(name string, input json.RawMessage, content string, budget int) string {
	body := content
	if budget > 0 && len(content) > budget && !isDigestedToolContent(content) {
		if d, ok := DigestToolOutput(name, input, []byte(content), budget); ok {
			body = d
		}
	}
	body = strings.TrimSpace(body)
	body = strings.ReplaceAll(body, "\n", " ")
	if len(body) > 320 {
		body = body[:317] + "..."
	}
	switch name {
	case "explore":
		sym := parseStringField(input, "symbol_name")
		if sym != "" {
			return fmt.Sprintf("explore(%s): %s", sym, body)
		}
	case "grep":
		q := parseStringField(input, "query")
		if q == "" {
			q = parseStringField(input, "pattern")
		}
		if q != "" {
			return fmt.Sprintf("grep(%q): %s", q, body)
		}
	case "read":
		p := parseStringField(input, "path")
		if p != "" {
			return fmt.Sprintf("read(%s): %s", p, body)
		}
	case "semantic_search":
		q := parseStringField(input, "query")
		if q != "" {
			return fmt.Sprintf("semantic_search(%q): %s", q, body)
		}
	}
	return name + ": " + body
}
