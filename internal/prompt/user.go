package prompt

import (
	"fmt"
	"strings"
)

// BuildUserPrompt builds the user-facing message content:
// it includes the IDE snapshot and the user's query.
func BuildUserPrompt(userQuery string, snap WorkspaceSnapshot, allowedTools []string) string {
	var b strings.Builder

	b.WriteString("<user_info>\n")
	if snap.OS != "" {
		b.WriteString("os: ")
		b.WriteString(snap.OS)
		b.WriteByte('\n')
	}
	if snap.Shell != "" {
		b.WriteString("shell: ")
		b.WriteString(snap.Shell)
		b.WriteByte('\n')
	}
	if snap.WorkspaceRoot != "" {
		b.WriteString("workspace_root: ")
		b.WriteString(snap.WorkspaceRoot)
		b.WriteByte('\n')
	}
	b.WriteString("is_git_repo: ")
	b.WriteString(fmt.Sprintf("%v", snap.IsGitRepo))
	b.WriteByte('\n')

	if snap.ActiveFile != "" {
		b.WriteString("active_file: ")
		b.WriteString(snap.ActiveFile)
		b.WriteByte('\n')
	}
	if snap.CursorLine != 0 || snap.CursorCol != 0 {
		b.WriteString(fmt.Sprintf("cursor: line=%d col=%d\n", snap.CursorLine, snap.CursorCol))
	}
	if len(snap.OpenFiles) > 0 {
		b.WriteString("open_files:\n")
		for _, f := range snap.OpenFiles {
			b.WriteString("- ")
			b.WriteString(f)
			b.WriteByte('\n')
		}
	}
	if snap.TerminalsPath != "" {
		b.WriteString("terminals_path: ")
		b.WriteString(snap.TerminalsPath)
		b.WriteByte('\n')
	}
	if len(snap.ChangedFiles) > 0 {
		b.WriteString("changed_files:\n")
		for _, f := range snap.ChangedFiles {
			b.WriteString("- ")
			b.WriteString(f)
			b.WriteByte('\n')
		}
	}
	b.WriteString("</user_info>\n\n")

	b.WriteString("<user_query>\n")
	b.WriteString(strings.TrimSpace(userQuery))
	b.WriteString("\n</user_query>\n")

	return b.String()
}

// BuildUserPromptWithHistory appends a bounded history tail to the base user prompt.
// maxBytes bounds the final string length (best-effort, in bytes).
func BuildUserPromptWithHistory(baseUserPrompt string, history []string, maxBytes int) string {
	if maxBytes <= 0 {
		return baseUserPrompt + "\n"
	}
	header := strings.TrimRight(baseUserPrompt, "\n") + "\n\nИстория (самые свежие события в конце):\n"
	footer := "\n\nВерни следующий шаг: либо tool call, либо финальный PatchSet JSON.\n"

	budget := maxBytes - len(header) - len(footer)
	if budget <= 0 {
		return strings.TrimSpace(baseUserPrompt)
	}

	var selected []string
	size := 0
	for i := len(history) - 1; i >= 0; i-- {
		item := history[i]
		need := len(item) + 2
		if size+need > budget {
			break
		}
		selected = append(selected, item)
		size += need
	}
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}

	return header + strings.Join(selected, "\n\n") + footer
}
