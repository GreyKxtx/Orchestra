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

func TestPrompt_DoesNotLeakDeniedTools(t *testing.T) {
	snap := WorkspaceSnapshot{WorkspaceRoot: "D:/proj"}
	base := BuildUserPrompt("сделай X", snap, []string{"ls", "read", "grep"})
	if strings.Contains(base, "bash") {
		t.Fatalf("did not expect denied tool name in prompt, got:\n%s", base)
	}
}

// BuildUserPrompt builds the user-facing message content:
// it includes the IDE snapshot and the user's query.
func BuildUserPrompt(userQuery string, snap WorkspaceSnapshot, allowedTools []string) string {
	return BuildUserContext(snap, allowedTools) + UserQueryBlock(userQuery) + "\n"
}
