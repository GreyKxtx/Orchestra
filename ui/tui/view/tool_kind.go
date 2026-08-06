package view

import (
	"encoding/json"
	"fmt"
	"strings"
)

// toolKind groups equivalent tool names (canonical name + LLM aliases) so
// every renderer can switch on a stable kind. Returns "" for unknown tools.
// Aliases include Claude Code's tool names (Read, Glob, LS, Edit, …) so the
// model's training-time tool calls map to consistent icons even when they
// don't exist in our registry.
func toolKind(name string) string {
	switch name {
	case "fs.read", "read", "Read":
		return "read"
	case "fs.list", "ls", "list", "LS":
		return "list"
	case "fs.write", "file.write_atomic", "write", "Write", "Edit", "edit", "MultiEdit":
		return "write"
	case "search.text", "grep", "Grep", "search":
		return "search"
	case "glob", "Glob":
		return "glob"
	case "code.symbols", "symbols", "Symbols":
		return "symbols"
	case "exec.run", "bash", "Bash", "exec":
		return "exec"
	case "task", "Task", "todowrite", "TodoWrite":
		return "task"
	default:
		return ""
	}
}

// toolIcon picks the unicode glyph that fronts a tool line.
// All icons are single-width single-rune characters drawn from the same
// stylistic family (arrows, geometric symbols, math operators) so the column
// of icons stays visually aligned regardless of tool mix.
func toolIcon(name string) string {
	switch toolKind(name) {
	case "read":
		return "→"
	case "list":
		return "≡"
	case "write":
		return "←"
	case "search":
		return "✱"
	case "glob":
		return "✦"
	case "symbols":
		return "◈"
	case "exec":
		return "$"
	case "task":
		return "▣"
	default:
		return "•"
	}
}

// ToolDisplayName is the exported alias for toolDisplayName.
func ToolDisplayName(name string) string { return toolDisplayName(name) }

// ToolArgsPath extracts a short path/preview from the tool's accumulated args,
// suitable for the status bar's running-tool indicator.
func ToolArgsPath(name, argsRaw string) string {
	args := parseToolArgs(argsRaw)
	switch toolKind(name) {
	case "read", "symbols", "write":
		if p, _ := args["filePath"].(string); p != "" {
			return p
		}
		if p, _ := args["path"].(string); p != "" {
			return p
		}
	case "list":
		if p, _ := args["path"].(string); p != "" {
			return p
		}
	case "search", "glob":
		if p, _ := args["pattern"].(string); p != "" {
			return p
		}
		if p, _ := args["query"].(string); p != "" {
			return p
		}
	case "exec":
		if c, _ := args["command"].(string); c != "" {
			if r := []rune(c); len(r) > 50 {
				return string(r[:50]) + "…"
			}
			return c
		}
	}
	return ""
}

// toolDisplayName humanizes a tool name. fs.read → Read, search.text → Grep.
func toolDisplayName(name string) string {
	switch toolKind(name) {
	case "read":
		return "Read"
	case "list":
		return "Ls"
	case "write":
		return "Write"
	case "search":
		return "Grep"
	case "glob":
		return "Glob"
	case "symbols":
		return "Symbols"
	case "exec":
		return "Shell"
	case "task":
		return "Task"
	default:
		return name
	}
}

// toolPreview produces a one-line readable preview of the tool's call.
// e.g. `Read /path/to/file.go`, `Grep "TODO" in src (3 matches)`.
func toolPreview(name, argsRaw, result string) string {
	args := parseToolArgs(argsRaw)
	disp := toolDisplayName(name)
	switch toolKind(name) {
	case "read":
		path, _ := args["filePath"].(string)
		if path == "" {
			path, _ = args["path"].(string)
		}
		return strings.TrimSpace(disp + " " + path)

	case "list":
		path, _ := args["path"].(string)
		s := disp + " " + path
		if c := countLines(result); c > 0 {
			s += fmt.Sprintf(" (%d entries)", c)
		}
		return strings.TrimSpace(s)

	case "search":
		pat, _ := args["pattern"].(string)
		if pat == "" {
			pat, _ = args["query"].(string)
		}
		path, _ := args["path"].(string)
		s := disp
		if pat != "" {
			s += fmt.Sprintf(" %q", pat)
		}
		if path != "" {
			s += " in " + path
		}
		if c := countLines(result); c > 0 {
			label := "matches"
			if c == 1 {
				label = "match"
			}
			s += fmt.Sprintf(" (%d %s)", c, label)
		}
		return strings.TrimSpace(s)

	case "glob":
		pat, _ := args["pattern"].(string)
		path, _ := args["path"].(string)
		s := disp
		if pat != "" {
			s += " " + pat
		}
		if path != "" {
			s += " in " + path
		}
		if c := countLines(result); c > 0 {
			label := "files"
			if c == 1 {
				label = "file"
			}
			s += fmt.Sprintf(" (%d %s)", c, label)
		}
		return strings.TrimSpace(s)

	case "symbols":
		path, _ := args["filePath"].(string)
		if path == "" {
			path, _ = args["path"].(string)
		}
		return strings.TrimSpace(disp + " " + path)

	case "write":
		if name == "edit" {
			disp = "Edit"
		}
		path, _ := args["filePath"].(string)
		if path == "" {
			path, _ = args["path"].(string)
		}
		return strings.TrimSpace(disp + " " + path)

	case "exec":
		desc, _ := args["description"].(string)
		if desc != "" {
			return desc
		}
		cmd, _ := args["command"].(string)
		if cmd != "" {
			if r := []rune(cmd); len(r) > 60 {
				return string(r[:60]) + "…"
			}
			return cmd
		}
		return disp

	default:
		return name
	}
}

// parseToolArgs tolerantly decodes the streamed args JSON. Args arrive as
// many `tool_call_delta` chunks — by the time we render a preview between
// deltas the buffer is usually incomplete. To still surface a useful preview
// (e.g. partial filePath) we:
//  1. Try the strict path first — a complete JSON object decodes fine.
//  2. On failure, attempt to close the object with the right number of
//     closing braces/brackets and quotes, then re-try. This produces a
//     best-effort map populated with whatever fields had already streamed.
func parseToolArgs(raw string) map[string]any {
	out := map[string]any{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out
	}
	if json.Unmarshal([]byte(raw), &out) == nil {
		return out
	}
	if patched := tryCloseJSON(raw); patched != "" {
		out = map[string]any{}
		if json.Unmarshal([]byte(patched), &out) == nil {
			return out
		}
	}
	return out
}

// tryCloseJSON appends the minimum number of `"`, `}`, `]` characters needed
// to make raw parse as a JSON object. Returns "" when the input is too
// malformed to recover (e.g. unbalanced opening).
func tryCloseJSON(raw string) string {
	var depthObj, depthArr int
	inStr, esc := false, false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if esc {
			esc = false
			continue
		}
		switch {
		case c == '\\':
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
			// inside string — ignore structural chars
		case c == '{':
			depthObj++
		case c == '}':
			depthObj--
		case c == '[':
			depthArr++
		case c == ']':
			depthArr--
		}
	}
	if depthObj < 0 || depthArr < 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(raw)
	if inStr {
		b.WriteByte('"')
	}
	for i := 0; i < depthArr; i++ {
		b.WriteByte(']')
	}
	for i := 0; i < depthObj; i++ {
		b.WriteByte('}')
	}
	return b.String()
}

// countLines returns the number of non-empty lines in s.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// truncateLines caps text at maxLines, appending an "… N more lines" marker.
func truncateLines(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n") +
		fmt.Sprintf("\n… %d more lines · Ctrl+T", len(lines)-maxLines)
}
