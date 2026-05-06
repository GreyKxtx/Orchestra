package prompt

import (
	"strings"
	"testing"
)

func TestPrompt_IncludesUserInfo_Minimal(t *testing.T) {
	snap := WorkspaceSnapshot{
		OS:            "windows/amd64",
		Shell:         "powershell",
		WorkspaceRoot: "D:/proj",
		IsGitRepo:     true,
	}
	base := BuildUserPrompt("сделай X", snap, []string{"ls", "read"})
	if !strings.Contains(base, "<user_info>") || !strings.Contains(base, "</user_info>") {
		t.Fatalf("expected <user_info> block, got:\n%s", base)
	}
	if !strings.Contains(base, "workspace_root: D:/proj") {
		t.Fatalf("expected workspace_root in prompt, got:\n%s", base)
	}
	if !strings.Contains(base, "is_git_repo: true") {
		t.Fatalf("expected is_git_repo=true in prompt, got:\n%s", base)
	}
	if !strings.Contains(base, "<user_query>") || !strings.Contains(base, "</user_query>") {
		t.Fatalf("expected <user_query> block, got:\n%s", base)
	}
}

func TestPrompt_RespectsMaxPromptBytes_TruncatesOldestHistory(t *testing.T) {
	snap := WorkspaceSnapshot{WorkspaceRoot: "D:/proj"}
	base := BuildUserPrompt("сделай X", snap, []string{"read"})

	history := []string{
		"OLD_1 " + strings.Repeat("a", 200),
		"OLD_2 " + strings.Repeat("b", 200),
		"NEW_1 " + strings.Repeat("c", 200),
		"NEW_2 " + strings.Repeat("d", 200),
	}

	// Force a tight budget to keep only the tail (but still allow history header+footer).
	p := BuildUserPromptWithHistory(base, history, len(base)+600)

	if strings.Contains(p, "OLD_1") || strings.Contains(p, "OLD_2") {
		t.Fatalf("expected oldest history to be truncated, got:\n%s", p)
	}
	if !strings.Contains(p, "NEW_2") {
		t.Fatalf("expected newest history to be kept, got:\n%s", p)
	}
}

func TestPrompt_DoesNotLeakDeniedTools(t *testing.T) {
	snap := WorkspaceSnapshot{WorkspaceRoot: "D:/proj"}
	base := BuildUserPrompt("сделай X", snap, []string{"ls", "read", "grep"})
	if strings.Contains(base, "bash") {
		t.Fatalf("did not expect denied tool name in prompt, got:\n%s", base)
	}
}
