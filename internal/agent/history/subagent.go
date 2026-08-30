package history

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/agent/digest"
	agentformat "github.com/orchestra/orchestra/internal/agent/format"
	"github.com/orchestra/orchestra/llm"
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
	if files := FormatSubagentFiles(history); files != "" {
		b.WriteString(files)
	}
	if tr := strings.TrimSpace(taskResult); tr != "" {
		b.WriteString("## Result\n")
		if digestBudget > 0 && len(tr) > digestBudget*3 {
			digested, ok := digest.DigestToolOutput("task_result", nil, []byte(tr), digestBudget*2)
			if ok {
				tr = digested
			} else {
				tr = agentformat.Truncate(tr, digestBudget*3)
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
	atoms := BuildHistoryAtoms(history[2:])
	var out []string
	seen := map[string]bool{}
	for _, a := range atoms {
		meta := ToolCallMapFromAtom(a)
		for _, m := range a.msgs {
			if m.Role != llm.RoleTool {
				continue
			}
			cm, ok := meta[m.ToolCallID]
			if !ok {
				continue
			}
			name := digest.NormalizeToolName(cm.name)
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
	if budget > 0 && len(content) > budget && !digest.IsDigestedToolContent(content) {
		if d, ok := digest.DigestToolOutput(name, input, []byte(content), budget); ok {
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
		sym := digest.ParseStringField(input, "symbol_name")
		if sym != "" {
			return fmt.Sprintf("explore(%s): %s", sym, body)
		}
	case "grep":
		q := digest.ParseStringField(input, "query")
		if q == "" {
			q = digest.ParseStringField(input, "pattern")
		}
		if q != "" {
			return fmt.Sprintf("grep(%q): %s", q, body)
		}
	case "read":
		p := digest.ParseStringField(input, "path")
		if p != "" {
			return fmt.Sprintf("read(%s): %s", p, body)
		}
	case "semantic_search":
		q := digest.ParseStringField(input, "query")
		if q != "" {
			return fmt.Sprintf("semantic_search(%q): %s", q, body)
		}
	}
	return name + ": " + body
}

// subagentFileTools are the tools whose path argument says what the child
// actually worked on.
var subagentFileTools = map[string]bool{
	"read": true, "edit": true, "write": true, "diff_preview": true, "fs.delete": true, "fs.rename": true,
}

const subagentMaxFiles = 12

// FormatSubagentFiles lists the files a child agent touched, with the tools it
// used on each. The parent otherwise sees only prose findings and cannot tell
// what was already read or edited — so it re-reads, or edits blind.
func FormatSubagentFiles(history []llm.Message) string {
	if len(history) == 0 {
		return ""
	}
	type entry struct {
		path string
		ops  []string
	}
	var order []string
	byPath := map[string]*entry{}

	for _, a := range BuildHistoryAtoms(history) {
		for _, m := range a.msgs {
			for _, tc := range m.ToolCalls {
				name := digest.NormalizeToolName(tc.Function.Name)
				if !subagentFileTools[name] {
					continue
				}
				path := pathFromToolInput(tc.Function.Arguments.Raw())
				if path == "" {
					continue
				}
				e, ok := byPath[path]
				if !ok {
					e = &entry{path: path}
					byPath[path] = e
					order = append(order, path)
				}
				if !containsString(e.ops, name) {
					e.ops = append(e.ops, name)
				}
			}
		}
	}
	if len(order) == 0 {
		return ""
	}
	if len(order) > subagentMaxFiles {
		order = order[len(order)-subagentMaxFiles:]
	}

	var b strings.Builder
	b.WriteString("## Files touched\n")
	for _, p := range order {
		e := byPath[p]
		fmt.Fprintf(&b, "- %s (%s)\n", e.path, strings.Join(e.ops, ", "))
	}
	return b.String()
}

// subagentLastActivityMaxChars caps the tail excerpt in a failure report.
const subagentLastActivityMaxChars = 800

// FormatSubagentProgress describes what a *failed* child got done before it
// stopped: which files it touched and what it was doing last.
//
// On failure the child's history is discarded, so the parent used to see only
// an error string and had to redo the work from nothing — including the reads
// the child had already paid for.
func FormatSubagentProgress(subagentType, goal string, history []llm.Message, digestBudget int) string {
	if len(history) == 0 {
		return ""
	}
	var b strings.Builder
	if files := FormatSubagentFiles(history); files != "" {
		b.WriteString(files)
	}
	if findings := extractSubagentFindings(history, digestBudget); len(findings) > 0 {
		b.WriteString("## Findings before the failure\n")
		for _, f := range findings {
			b.WriteString("- ")
			b.WriteString(f)
			b.WriteByte('\n')
		}
	}
	if last := lastActivity(history); last != "" {
		b.WriteString("## Last activity\n")
		b.WriteString(agentformat.Truncate(last, subagentLastActivityMaxChars))
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		return ""
	}
	return strings.TrimSpace(b.String())
}

// lastActivity returns the child's final assistant text, or its final tool
// result when it ended mid-tool.
func lastActivity(history []llm.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		switch m.Role {
		case llm.RoleAssistant:
			return m.Content
		case llm.RoleTool:
			return "tool result: " + m.Content
		}
	}
	return ""
}

func containsString(in []string, s string) bool {
	for _, v := range in {
		if v == s {
			return true
		}
	}
	return false
}
