package git

import (
	"strings"

	"github.com/orchestra/orchestra/protocol"
)

// gitExitError turns a non-zero git exit into something the caller can act on.
//
// "git status failed" is what every git tool used to say, with the reason
// tucked into the error's data. In an evaluation run a model called git.status
// thirty times against a workspace that simply was not a repository, learning
// nothing each time — the one fact that would have stopped it was in git's
// stderr and never in the message.
//
// The reasons below are the ones where knowing what happened changes the next
// move: retrying the same command is right after a conflict is resolved and
// pointless when the workspace is not a repository at all.
func gitExitError(command, stderr string, code int) error {
	return protocol.NewError(protocol.ExecFailed, gitFailureMessage(command, stderr), map[string]any{
		"stderr": stderr,
		"exit":   code,
	})
}

// gitFailureMessage is the message half on its own, for the two call sites
// that attach extra data (the path that failed to stage, git commit's stdout).
func gitFailureMessage(command, stderr string) string {
	first := firstStderrLine(stderr)
	msg := command + " failed"

	switch {
	case strings.Contains(stderr, "not a git repository"):
		msg = command + ": this workspace is not a git repository — there is no history to read here"
	case strings.Contains(stderr, "does not have any commits yet"),
		strings.Contains(stderr, "bad default revision"):
		msg = command + ": the repository has no commits yet"
	case strings.Contains(stderr, "unknown revision"),
		strings.Contains(stderr, "bad revision"),
		strings.Contains(stderr, "did not match any file(s) known to git"):
		msg = command + ": " + first + " — check the name against git.log or git.branch"

	// Mutating commands: the cases where the fix is a different action, not
	// a retry of the same one.
	case strings.Contains(stderr, "nothing to commit"),
		strings.Contains(stderr, "no changes added to commit"):
		msg = command + ": there is nothing staged to commit — check git.status first"
	case strings.Contains(stderr, "Please tell me who you are"),
		strings.Contains(stderr, "unable to auto-detect email address"):
		msg = command + ": git has no user.name/user.email configured in this repository — a commit cannot be authored"
	case strings.Contains(stderr, "already exists"):
		msg = command + ": " + first + " — the branch is already there; check it out instead of creating it"
	case strings.Contains(stderr, "Your local changes"),
		strings.Contains(stderr, "would be overwritten"):
		msg = command + ": " + first + " — commit or set the local changes aside first"
	case strings.Contains(stderr, "no upstream branch"),
		strings.Contains(stderr, "has no upstream"):
		msg = command + ": the branch has no upstream — push with set_upstream to create it"
	case strings.Contains(stderr, "non-fast-forward"),
		strings.Contains(stderr, "fetch first"),
		strings.Contains(stderr, "behind its remote"):
		msg = command + ": the remote has commits this branch does not — pull and rebase before pushing, never force"
	case strings.Contains(stderr, "Authentication failed"),
		strings.Contains(stderr, "could not read Username"),
		strings.Contains(stderr, "Permission denied"):
		msg = command + ": the remote refused the credentials — this needs the user, not a retry"
	case strings.Contains(stderr, "checked out at"):
		msg = command + ": " + first + " — that branch is checked out in another worktree"

	case first != "":
		msg = command + ": " + first
	}
	return msg
}

// firstStderrLine returns git's first real complaint, without its "fatal: "
// or "error: " prefix.
func firstStderrLine(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, prefix := range []string{"fatal: ", "error: ", "warning: "} {
			line = strings.TrimPrefix(line, prefix)
		}
		return line
	}
	return ""
}
