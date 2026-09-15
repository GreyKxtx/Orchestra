package git

import (
	"context"
	"os"
	"strings"
	"time"

	coregit "github.com/orchestra/orchestra/internal/git"
	"github.com/orchestra/orchestra/protocol"
)

// addAllExceptOrchestraArtifacts is git.commit's add: ["."]. A bare `git add .`
// stages whatever the project's .gitignore does not exclude, and the rule that
// keeps Orchestra's own secrets file, run logs and CKG database out of history
// lives in the block `orchestra init` writes there. In a project init never ran
// in, the model's commit put .orchestra/ckg.db, .orchestra/llm_log.jsonl and
// .orchestra.local.yml into history.
//
// So the same rule set is applied here regardless of .gitignore:
//
//  1. tracked changes — `git add -u` (ignore rules never apply to tracked files;
//     a file the user chose to track stays their choice);
//  2. untracked files that neither the user's own ignore sources
//     (--exclude-standard: .gitignore, info/exclude, core.excludesFile) nor
//     coregit.OrchestraIgnorePatterns exclude.
//
// The patterns go in through --exclude-from rather than -x because -x does not
// honour negation, and the knowledge files init keeps tracked are negations.
func (c *Client) addAllExceptOrchestraArtifacts(ctx context.Context) error {
	if _, stderr, code, err := c.runGit(ctx, 30*time.Second, "add", "-u", "--", "."); err != nil || code != 0 {
		if err != nil {
			return err
		}
		return protocol.NewError(protocol.ExecFailed, gitFailureMessage("git add", stderr),
			map[string]any{"stderr": stderr, "exit": code})
	}

	prefix, stderr, code, err := c.runGit(ctx, 15*time.Second, "rev-parse", "--show-prefix")
	if err != nil {
		return err
	}
	if code != 0 {
		return protocol.NewError(protocol.ExecFailed, gitFailureMessage("git rev-parse", stderr), nil)
	}

	// Only ignore patterns go into this file — no project content — and it is
	// removed before returning.
	excl, err := os.CreateTemp("", "orchestra-commit-exclude-*")
	if err != nil {
		return protocol.NewError(protocol.ExecFailed, "cannot prepare commit exclusions: "+err.Error(), nil)
	}
	exclPath := excl.Name()
	defer os.Remove(exclPath)
	_, werr := excl.WriteString(coregit.AnchorIgnorePatterns(coregit.OrchestraIgnorePatterns, strings.TrimSpace(prefix)))
	cerr := excl.Close()
	if werr != nil || cerr != nil {
		return protocol.NewError(protocol.ExecFailed, "cannot prepare commit exclusions", nil)
	}

	listed, stderr, code, err := c.runGit(ctx, 30*time.Second,
		"ls-files", "-z", "--others", "--exclude-standard", "--exclude-from="+exclPath, "--", ".")
	if err != nil {
		return err
	}
	if code != 0 {
		return protocol.NewError(protocol.ExecFailed, gitFailureMessage("git ls-files", stderr), nil)
	}
	var paths []string
	for _, p := range strings.Split(listed, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}

	// Chunked so a large untracked tree cannot exceed the Windows command-line
	// limit (32 767 characters). ":(literal)" keeps a file named like a glob
	// from matching others.
	const maxChunk = 8000
	for len(paths) > 0 {
		args := []string{"add", "--"}
		size := 0
		n := 0
		for n < len(paths) && (n == 0 || size+len(paths[n])+12 < maxChunk) {
			args = append(args, ":(literal)"+paths[n])
			size += len(paths[n]) + 12
			n++
		}
		paths = paths[n:]
		if _, stderr, code, err := c.runGit(ctx, 30*time.Second, args...); err != nil || code != 0 {
			if err != nil {
				return err
			}
			return protocol.NewError(protocol.ExecFailed, gitFailureMessage("git add", stderr),
				map[string]any{"stderr": stderr, "exit": code})
		}
	}
	return nil
}
