package git

import (
	"strings"
	"testing"
)

func TestGitExitError_SaysWhenTheWorkspaceIsNotARepository(t *testing.T) {
	err := gitExitError("git status", "fatal: not a git repository (or any of the parent directories): .git\n", 128)
	msg := err.Error()
	if !strings.Contains(msg, "not a git repository") {
		t.Errorf("the message must carry the reason, got: %s", msg)
	}
	if strings.Contains(msg, "git status failed") {
		t.Errorf("the message must say more than that the command failed, got: %s", msg)
	}
}

func TestGitExitError_SaysWhenThereAreNoCommits(t *testing.T) {
	err := gitExitError("git log", "fatal: your current branch 'main' does not have any commits yet\n", 128)
	if !strings.Contains(err.Error(), "no commits yet") {
		t.Errorf("the message must explain an empty repository, got: %s", err)
	}
}

func TestGitExitError_FallsBackToGitsOwnFirstLine(t *testing.T) {
	err := gitExitError("git diff", "fatal: ambiguous argument 'nope': unknown revision\n", 128)
	msg := err.Error()
	if !strings.Contains(msg, "unknown revision") {
		t.Errorf("the message must quote git, got: %s", msg)
	}
	if strings.Contains(msg, "fatal: ") {
		t.Errorf("git's severity prefix is noise in a tool result, got: %s", msg)
	}
}

func TestGitExitError_SurvivesEmptyStderr(t *testing.T) {
	err := gitExitError("git status", "", 1)
	if !strings.Contains(err.Error(), "git status failed") {
		t.Errorf("with nothing to quote it must still say which command failed, got: %s", err)
	}
}
