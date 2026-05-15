package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/protocol"
)

const gitOutputLimit = 256 * 1024 // 256 KB

// runGit runs a git command in the workspace root.
func (r *Runner) runGit(ctx context.Context, timeout time.Duration, args ...string) (stdout, stderr string, exitCode int, err error) {
	gitPath, lookErr := exec.LookPath("git")
	if lookErr != nil {
		return "", "", -1, protocol.NewError(protocol.ExecFailed, "git not found on PATH", nil)
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, gitPath, args...)
	cmd.Dir = r.workspaceRoot
	cmd.Stdin = nil

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()
	if len(stdout) > gitOutputLimit {
		stdout = stdout[:gitOutputLimit] + "\n[output truncated]"
	}
	if len(stderr) > gitOutputLimit {
		stderr = stderr[:gitOutputLimit]
	}

	if runErr != nil {
		if errors.Is(tctx.Err(), context.DeadlineExceeded) {
			return stdout, stderr, -1, protocol.NewError(protocol.ExecTimeout,
				"git command timed out", map[string]any{"args": args})
		}
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			return stdout, stderr, ee.ExitCode(), nil
		}
		return stdout, stderr, -1, protocol.NewError(protocol.ExecFailed,
			"failed to start git", map[string]any{"error": runErr.Error()})
	}
	return stdout, stderr, 0, nil
}

// isGitSafeRef returns true if s contains only characters valid in a git ref.
func isGitSafeRef(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '-' {
		return false
	}
	for _, c := range s {
		ok := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '/' || c == '-' || c == '_' || c == '.' ||
			c == '~' || c == '^' || c == '@' || c == '{' || c == '}'
		if !ok {
			return false
		}
	}
	return true
}

// ─── git.status ──────────────────────────────────────────────────────────────

// GitStatusRequest is the input for the git.status tool.
type GitStatusRequest struct{}

// GitStatusResponse is the output of the git.status tool.
type GitStatusResponse struct {
	Output string `json:"output"`
	Branch string `json:"branch,omitempty"`
	Clean  bool   `json:"clean"`
}

// GitStatus returns the current git working-tree status.
func (r *Runner) GitStatus(ctx context.Context, _ GitStatusRequest) (*GitStatusResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	stdout, stderr, code, err := r.runGit(ctx, 15*time.Second, "status", "--short", "--branch")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, protocol.NewError(protocol.ExecFailed, "git status failed",
			map[string]any{"stderr": stderr, "exit": code})
	}

	branch := ""
	lines := strings.SplitN(stdout, "\n", 2)
	if len(lines) > 0 && strings.HasPrefix(lines[0], "## ") {
		bl := strings.TrimPrefix(lines[0], "## ")
		branch = parseBranchFromStatusLine(bl)
	}

	rest := ""
	if len(lines) > 1 {
		rest = lines[1]
	}
	clean := strings.TrimSpace(rest) == ""

	return &GitStatusResponse{Output: stdout, Branch: branch, Clean: clean}, nil
}

// ─── git.log ─────────────────────────────────────────────────────────────────

// GitLogRequest is the input for the git.log tool.
type GitLogRequest struct {
	N       int    `json:"n,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Path    string `json:"path,omitempty"`
	Oneline bool   `json:"oneline,omitempty"`
}

// GitLogResponse is the output of the git.log tool.
type GitLogResponse struct {
	Output    string `json:"output"`
	Truncated bool   `json:"truncated,omitempty"`
}

// GitLog returns the commit history.
func (r *Runner) GitLog(ctx context.Context, req GitLogRequest) (*GitLogResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	n := req.N
	if n <= 0 {
		n = 20
	}
	if n > 200 {
		n = 200
	}

	args := []string{"log", fmt.Sprintf("-n%d", n)}
	if req.Oneline {
		args = append(args, "--oneline")
	} else {
		args = append(args, "--format=format:%H %as %an%n  %s%n")
	}
	if req.Ref != "" {
		if !isGitSafeRef(req.Ref) {
			return nil, protocol.NewError(protocol.InvalidLLMOutput,
				"invalid ref — only alphanumeric / - _ . ~ ^ @ { } allowed",
				map[string]any{"ref": req.Ref})
		}
		args = append(args, req.Ref)
	}
	if req.Path != "" {
		_, relSlash, pathErr := resolveWorkspacePath(r.workspaceRoot, req.Path)
		if pathErr != nil {
			return nil, pathErr
		}
		args = append(args, "--", relSlash)
	}

	stdout, stderr, code, err := r.runGit(ctx, 15*time.Second, args...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, protocol.NewError(protocol.ExecFailed, "git log failed",
			map[string]any{"stderr": stderr, "exit": code})
	}

	truncated := strings.HasSuffix(stdout, "[output truncated]")
	return &GitLogResponse{Output: stdout, Truncated: truncated}, nil
}

// ─── git.diff ────────────────────────────────────────────────────────────────

// GitDiffRequest is the input for the git.diff tool.
type GitDiffRequest struct {
	Staged bool   `json:"staged,omitempty"`
	Ref    string `json:"ref,omitempty"`
	Path   string `json:"path,omitempty"`
}

// GitDiffResponse is the output of the git.diff tool.
type GitDiffResponse struct {
	Output    string `json:"output"`
	Truncated bool   `json:"truncated,omitempty"`
}

// GitDiff returns the diff of uncommitted changes.
func (r *Runner) GitDiff(ctx context.Context, req GitDiffRequest) (*GitDiffResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	args := []string{"diff"}
	if req.Staged {
		args = append(args, "--cached")
	}
	if req.Ref != "" {
		if !isGitSafeRef(req.Ref) {
			return nil, protocol.NewError(protocol.InvalidLLMOutput,
				"invalid ref — only alphanumeric / - _ . ~ ^ @ { } allowed",
				map[string]any{"ref": req.Ref})
		}
		args = append(args, req.Ref)
	}
	if req.Path != "" {
		_, relSlash, pathErr := resolveWorkspacePath(r.workspaceRoot, req.Path)
		if pathErr != nil {
			return nil, pathErr
		}
		args = append(args, "--", relSlash)
	}

	stdout, stderr, code, err := r.runGit(ctx, 30*time.Second, args...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, protocol.NewError(protocol.ExecFailed, "git diff failed",
			map[string]any{"stderr": stderr, "exit": code})
	}

	truncated := strings.HasSuffix(stdout, "[output truncated]")
	return &GitDiffResponse{Output: stdout, Truncated: truncated}, nil
}

// parseBranchFromStatusLine extracts the branch name from the first line of
// `git status --short --branch` output (after stripping the "## " prefix).
// Handles: "main", "main...origin/main", "HEAD (no branch)",
// "No commits yet on main", "Initial commit on main" (older git).
func parseBranchFromStatusLine(bl string) string {
	for _, pfx := range []string{"No commits yet on ", "Initial commit on "} {
		if strings.HasPrefix(bl, pfx) {
			b := strings.TrimPrefix(bl, pfx)
			if idx := strings.IndexAny(b, ". "); idx >= 0 {
				return b[:idx]
			}
			return strings.TrimSpace(b)
		}
	}
	if idx := strings.IndexAny(bl, ". "); idx >= 0 {
		return bl[:idx]
	}
	return bl
}
