package eval

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// The git tools — git.status, git.log, git.diff — could not be graded by any
// task, because an eval workspace is a bare temp directory. Every call
// answered "this workspace is not a git repository", which a trace shows
// plainly and no check could ever turn into a verdict. A task that needs them
// says `git: true` and gets a repository with one commit holding exactly the
// files the task defined, so `git status` afterwards describes the agent's
// work and nothing else.

const gitSetupTimeout = 30 * time.Second

func initGitWorkspace(ctx context.Context, root string) error {
	ctx, cancel := context.WithTimeout(ctx, gitSetupTimeout)
	defer cancel()

	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("this task needs a git repository and git is not on PATH: %w", err)
	}

	// Identity and signing are set locally: the machine running the suite may
	// have neither, or may have a signing key that would make commit prompt.
	steps := [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "eval@orchestra.invalid"},
		{"config", "user.name", "Orchestra Eval"},
		{"config", "commit.gpgsign", "false"},
		{"add", "-A"},
		{"commit", "-m", "the workspace as the task defined it"},
	}
	for _, args := range steps {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s in the eval workspace: %v: %s",
				strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
