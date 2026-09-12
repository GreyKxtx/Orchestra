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

// For a mutating command the useful message is the one that names a different
// next action. "git push failed" invites a retry; "the remote has commits this
// branch does not" does not.
func TestGitFailureMessage_MutatingCommandsNameTheNextAction(t *testing.T) {
	cases := []struct {
		name    string
		command string
		stderr  string
		want    string
		absent  string
	}{
		{
			name:    "nothing staged",
			command: "git commit",
			stderr:  "nothing to commit, working tree clean\n",
			want:    "nothing staged",
		},
		{
			name:    "no identity configured",
			command: "git commit",
			stderr:  "fatal: unable to auto-detect email address\n*** Please tell me who you are.\n",
			want:    "user.name/user.email",
		},
		{
			name:    "branch already exists",
			command: "git branch create",
			stderr:  "fatal: a branch named 'feat/x' already exists\n",
			want:    "check it out instead",
		},
		{
			name:    "checkout would clobber work",
			command: "git checkout",
			stderr:  "error: Your local changes to the following files would be overwritten by checkout:\n\tmain.go\n",
			want:    "commit or set the local changes aside",
		},
		{
			name:    "no upstream",
			command: "git push",
			stderr:  "fatal: The current branch feat/x has no upstream branch.\n",
			want:    "set_upstream",
		},
		{
			name:    "remote moved",
			command: "git push",
			stderr:  "! [rejected] main -> main (non-fast-forward)\nhint: Updates were rejected\n",
			want:    "pull and rebase before pushing",
			absent:  "force",
		},
		{
			name:    "credentials refused",
			command: "git push",
			stderr:  "fatal: Authentication failed for 'https://github.com/x/y.git/'\n",
			want:    "needs the user",
		},
		{
			name:    "branch busy in another worktree",
			command: "git checkout",
			stderr:  "fatal: 'master' is already checked out at '/repo'\n",
			want:    "another worktree",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := gitFailureMessage(c.command, c.stderr)
			if !strings.Contains(got, c.want) {
				t.Errorf("message must carry %q, got: %s", c.want, got)
			}
			if got == c.command+" failed" {
				t.Errorf("message says nothing beyond the command name: %s", got)
			}
			// "never force" is advice, not an instruction to force.
			if c.absent != "" && strings.Contains(got, c.absent) && !strings.Contains(got, "never "+c.absent) {
				t.Errorf("message must not suggest %q, got: %s", c.absent, got)
			}
		})
	}
}
