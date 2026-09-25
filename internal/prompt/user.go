package prompt

import (
	"fmt"
	"strings"
)

// UserQueryBlock wraps the user's query the way every prompt carries it. A
// session keeps each turn's query in its history in this exact form, which is
// how the agent recognises that a query is already in the transcript.
func UserQueryBlock(userQuery string) string {
	return "<user_query>\n" + strings.TrimSpace(userQuery) + "\n</user_query>"
}

// BuildUserContext is BuildUserPrompt without the query: the IDE snapshot and
// the tool names.
func BuildUserContext(snap WorkspaceSnapshot, allowedTools []string) string {
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

	if len(allowedTools) > 0 {
		b.WriteString("<tool_names>\n")
		b.WriteString(strings.Join(allowedTools, ", "))
		b.WriteString("\n</tool_names>\n")
		b.WriteString("(full descriptions: see <available_tools> in system prompt)\n\n")
	}

	return b.String()
}
