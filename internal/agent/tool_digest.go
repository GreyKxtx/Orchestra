package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const defaultToolDigestBytes = 16 * 1024

// DigestToolOutput shrinks large tool results for LLM history (OpenCode-style prune).
// Returns digested text; second return is true when output was replaced by a digest.
func DigestToolOutput(toolName string, toolInput json.RawMessage, raw []byte, budgetBytes int) (string, bool) {
	if budgetBytes <= 0 || len(raw) <= budgetBytes {
		return string(raw), false
	}
	name := strings.ToLower(strings.TrimSpace(toolName))
	var body string
	switch name {
	case "explore":
		body = digestExplore(toolInput, raw, budgetBytes)
	case "grep":
		body = digestGrep(raw, budgetBytes)
	case "read":
		body = digestRead(raw, budgetBytes)
	case "bash":
		body = digestBash(raw, budgetBytes)
	case "semantic_search":
		body = digestSemanticSearch(raw, budgetBytes)
	case "task", "task_wait", "task_spawn":
		body = digestSubagentTask(raw, budgetBytes)
	default:
		body = digestGeneric(name, raw, budgetBytes)
	}
	header := fmt.Sprintf("[digest tool:%s original_bytes=%d]\n", name, len(raw))
	out := header + body + "\n(full: call " + name + " again with same args for raw output)"
	if len(out) > budgetBytes*2 {
		out = truncate(out, budgetBytes*2)
	}
	return out, true
}

func digestExplore(input json.RawMessage, raw []byte, budget int) string {
	symbol := parseStringField(input, "symbol_name")
	content := unwrapJSONString(raw)
	if content == "" {
		var resp struct {
			Content string `json:"content"`
		}
		if json.Unmarshal(raw, &resp) == nil {
			content = resp.Content
		} else {
			content = string(raw)
		}
	}
	var b strings.Builder
	if symbol != "" {
		b.WriteString("- symbol: " + symbol + "\n")
	}
	if loc := extractExploreLocation(content); loc != "" {
		b.WriteString("- location: " + loc + "\n")
	}
	if callers := extractSectionLines(content, "callers", 5); len(callers) > 0 {
		b.WriteString("- callers: " + strings.Join(callers, "; ") + "\n")
	}
	if callees := extractSectionLines(content, "callees", 5); len(callees) > 0 {
		b.WriteString("- callees: " + strings.Join(callees, "; ") + "\n")
	}
	b.WriteString("- summary: " + firstMeaningfulLines(content, 3, 240) + "\n")
	return truncate(b.String(), budget)
}

func digestGrep(raw []byte, budget int) string {
	text := unwrapJSONString(raw)
	if text == "" {
		text = string(raw)
	}
	lines := strings.Split(text, "\n")
	var matchLines []string
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "Найдено") || strings.HasPrefix(ln, "Совпадений") {
			continue
		}
		if strings.Contains(ln, ":") {
			matchLines = append(matchLines, ln)
		}
	}
	const maxShow = 8
	var b strings.Builder
	b.WriteString(fmt.Sprintf("- matches: %d\n", len(matchLines)))
	for i, ln := range matchLines {
		if i >= maxShow {
			b.WriteString(fmt.Sprintf("- ... +%d more\n", len(matchLines)-maxShow))
			break
		}
		b.WriteString("- " + ln + "\n")
	}
	if b.Len() == 0 {
		b.WriteString("- " + truncate(text, 400) + "\n")
	}
	return truncate(b.String(), budget)
}

func digestRead(raw []byte, budget int) string {
	var resp struct {
		Path      string `json:"path"`
		FileHash  string `json:"file_hash"`
		SHA256    string `json:"sha256"`
		Size      int64  `json:"size"`
		Truncated bool   `json:"truncated"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return digestGeneric("read", raw, budget)
	}
	hash := resp.FileHash
	if hash == "" {
		hash = resp.SHA256
	}
	lineCount := strings.Count(resp.Content, "\n") + 1
	preview := firstMeaningfulLines(resp.Content, 8, 400)
	var b strings.Builder
	fmt.Fprintf(&b, "- path: %s\n", resp.Path)
	fmt.Fprintf(&b, "- file_hash: %s\n", hash)
	fmt.Fprintf(&b, "- size_bytes: %d lines: ~%d truncated: %v\n", resp.Size, lineCount, resp.Truncated)
	b.WriteString("- preview:\n" + preview + "\n")
	return truncate(b.String(), budget)
}

func digestBash(raw []byte, budget int) string {
	var resp struct {
		ExitCode  int    `json:"exit_code"`
		Stdout    string `json:"stdout"`
		Stderr    string `json:"stderr"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return digestGeneric("bash", raw, budget)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- exit_code: %d truncated: %v\n", resp.ExitCode, resp.Truncated)
	if resp.Stdout != "" {
		b.WriteString("- stdout: " + truncate(strings.TrimSpace(resp.Stdout), 300) + "\n")
	}
	if resp.Stderr != "" {
		b.WriteString("- stderr: " + truncate(strings.TrimSpace(resp.Stderr), 200) + "\n")
	}
	return truncate(b.String(), budget)
}

func digestGeneric(name string, raw []byte, budget int) string {
	return "- preview: " + truncate(strings.TrimSpace(string(raw)), budget-12) + "\n"
}

func digestSemanticSearch(raw []byte, budget int) string {
	var resp struct {
		Hits []struct {
			FQN   string  `json:"fqn"`
			Kind  string  `json:"kind"`
			Path  string  `json:"path"`
			Score float32 `json:"score"`
		} `json:"hits"`
		ExploreSummaries []struct {
			FQN     string  `json:"fqn"`
			Summary string  `json:"summary"`
			Score   float32 `json:"score"`
		} `json:"explore_summaries"`
		NextStep string `json:"next_step"`
	}
	if json.Unmarshal(raw, &resp) != nil {
		return digestGeneric("semantic_search", raw, budget)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- hits: %d\n", len(resp.Hits))
	for i, h := range resp.Hits {
		if i >= 6 {
			b.WriteString(fmt.Sprintf("- ... +%d more hits\n", len(resp.Hits)-6))
			break
		}
		fmt.Fprintf(&b, "- %s (%s) score=%.3f path=%s\n", h.FQN, h.Kind, h.Score, h.Path)
	}
	for i, e := range resp.ExploreSummaries {
		if i >= 3 {
			break
		}
		fmt.Fprintf(&b, "- explore(%s): %s\n", e.FQN, truncate(e.Summary, 200))
	}
	if resp.NextStep != "" {
		b.WriteString("- next: " + resp.NextStep + "\n")
	}
	return truncate(b.String(), budget)
}

func digestSubagentTask(raw []byte, budget int) string {
	var resp struct {
		Status      string `json:"status"`
		Result      string `json:"result"`
		Error       string `json:"error"`
		Description string `json:"description"`
	}
	if json.Unmarshal(raw, &resp) != nil {
		return digestGeneric("task", raw, budget)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- status: %s\n", resp.Status)
	if resp.Description != "" {
		b.WriteString("- description: " + resp.Description + "\n")
	}
	if resp.Result != "" {
		b.WriteString("- result: " + truncate(resp.Result, budget/2) + "\n")
	}
	if resp.Error != "" {
		b.WriteString("- error: " + truncate(resp.Error, 200) + "\n")
	}
	return truncate(b.String(), budget)
}

// AutoMemoryNote builds a one-line session note after explore/grep.
func AutoMemoryNote(toolName string, input json.RawMessage, digestedOrRaw string) string {
	switch strings.ToLower(toolName) {
	case "explore":
		sym := parseStringField(input, "symbol_name")
		if sym == "" {
			return ""
		}
		loc := extractExploreLocation(digestedOrRaw)
		if loc != "" {
			return fmt.Sprintf("%s @ %s", sym, loc)
		}
		return sym + " — " + firstMeaningfulLines(digestedOrRaw, 1, 120)
	case "grep":
		q := parseStringField(input, "query")
		if q == "" {
			q = parseStringField(input, "pattern")
		}
		if q == "" {
			return ""
		}
		n := strings.Count(digestedOrRaw, "\n- ")
		if n == 0 {
			n = strings.Count(digestedOrRaw, "\n")
		}
		return fmt.Sprintf("grep %q — %d match lines in digest", q, n)
	default:
		return ""
	}
}

func parseStringField(input json.RawMessage, key string) string {
	if len(input) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(input, &m) != nil {
		return ""
	}
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func unwrapJSONString(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, `"`) {
		var unquoted string
		if json.Unmarshal(raw, &unquoted) == nil {
			return unquoted
		}
	}
	return s
}

var exploreFileLineRe = regexp.MustCompile(`(?m)^(?:#?\s*)?([^\s:]+\.[a-zA-Z0-9]+):(\d+)(?:-(\d+))?`)

func extractExploreLocation(content string) string {
	if m := exploreFileLineRe.FindStringSubmatch(content); len(m) >= 3 {
		if m[3] != "" {
			return fmt.Sprintf("%s:%s-%s", m[1], m[2], m[3])
		}
		return fmt.Sprintf("%s:%s", m[1], m[2])
	}
	// Package overview: first ``` path hint
	for _, ln := range strings.Split(content, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.Contains(ln, "/") && (strings.HasSuffix(ln, ".go") || strings.HasSuffix(ln, ".ts")) {
			return ln
		}
	}
	return ""
}

func extractSectionLines(content, keyword string, max int) []string {
	lower := strings.ToLower(content)
	key := strings.ToLower(keyword)
	idx := strings.Index(lower, key)
	if idx < 0 {
		return nil
	}
	rest := content[idx:]
	var out []string
	for _, ln := range strings.Split(rest, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.Contains(strings.ToLower(ln), key) {
			continue
		}
		if strings.HasPrefix(ln, "**") {
			break
		}
		ln = strings.TrimPrefix(ln, "- ")
		ln = strings.Trim(ln, "`")
		if ln != "" {
			out = append(out, ln)
		}
		if len(out) >= max {
			break
		}
	}
	return out
}

func firstMeaningfulLines(s string, maxLines, maxRunes int) string {
	var lines []string
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "```") {
			continue
		}
		lines = append(lines, ln)
		if len(lines) >= maxLines {
			break
		}
	}
	out := strings.Join(lines, " | ")
	return truncate(out, maxRunes)
}
