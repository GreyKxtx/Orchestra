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
// Applied to the read-only trio (status / log / diff), which is what an agent
// reaches for unprompted. The mutating commands still say "<cmd> failed"; the
// same treatment suits them and needs exec-enabled tests to verify.
func gitExitError(command, stderr string, code int) error {
	first := firstStderrLine(stderr)
	msg := command + " failed"

	switch {
	case strings.Contains(stderr, "not a git repository"):
		msg = command + ": this workspace is not a git repository — there is no history to read here"
	case strings.Contains(stderr, "does not have any commits yet"),
		strings.Contains(stderr, "bad default revision"):
		msg = command + ": the repository has no commits yet"
	case strings.Contains(stderr, "unknown revision"),
		strings.Contains(stderr, "bad revision"):
		msg = command + ": " + first + " — check the ref name against git.log"
	case first != "":
		msg = command + ": " + first
	}

	return protocol.NewError(protocol.ExecFailed, msg, map[string]any{
		"stderr": stderr,
		"exit":   code,
	})
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
