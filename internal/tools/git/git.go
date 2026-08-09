package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/tools/toolpath"
	"github.com/orchestra/orchestra/protocol"
)

type Client struct {
	root string
}

func NewClient(workspaceRoot string) *Client {
	return &Client{root: workspaceRoot}
}
const gitOutputLimit = 256 * 1024 // 256 KB

// runGit runs a git command in the workspace root.
func (c *Client) runGit(ctx context.Context, timeout time.Duration, args ...string) (stdout, stderr string, exitCode int, err error) {
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
	cmd.Dir = c.root
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

// IsGitSafeRef returns true if s contains only characters valid in a git ref.
func IsGitSafeRef(s string) bool {
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
func (c *Client) GitStatus(ctx context.Context, _ GitStatusRequest) (*GitStatusResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	stdout, stderr, code, err := c.runGit(ctx, 15*time.Second, "status", "--short", "--branch")
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
func (c *Client) GitLog(ctx context.Context, req GitLogRequest) (*GitLogResponse, error) {
	if c == nil {
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
		if !IsGitSafeRef(req.Ref) {
			return nil, protocol.NewError(protocol.InvalidLLMOutput,
				"invalid ref — only alphanumeric / - _ . ~ ^ @ { } allowed",
				map[string]any{"ref": req.Ref})
		}
		args = append(args, req.Ref)
	}
	if req.Path != "" {
		_, relSlash, pathErr := toolpath.ResolveWorkspacePath(c.root, req.Path)
		if pathErr != nil {
			return nil, pathErr
		}
		args = append(args, "--", relSlash)
	}

	stdout, stderr, code, err := c.runGit(ctx, 15*time.Second, args...)
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
func (c *Client) GitDiff(ctx context.Context, req GitDiffRequest) (*GitDiffResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	args := []string{"diff"}
	if req.Staged {
		args = append(args, "--cached")
	}
	if req.Ref != "" {
		if !IsGitSafeRef(req.Ref) {
			return nil, protocol.NewError(protocol.InvalidLLMOutput,
				"invalid ref — only alphanumeric / - _ . ~ ^ @ { } allowed",
				map[string]any{"ref": req.Ref})
		}
		args = append(args, req.Ref)
	}
	if req.Path != "" {
		_, relSlash, pathErr := toolpath.ResolveWorkspacePath(c.root, req.Path)
		if pathErr != nil {
			return nil, pathErr
		}
		args = append(args, "--", relSlash)
	}

	stdout, stderr, code, err := c.runGit(ctx, 30*time.Second, args...)
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

// ─── git.commit ───────────────────────────────────────────────────────────────

type GitCommitRequest struct {
	Message    string   `json:"message"`
	Add        []string `json:"add,omitempty"`
	AllowEmpty bool     `json:"allow_empty,omitempty"`
}

type GitCommitResponse struct {
	Output string `json:"output"`
	Hash   string `json:"hash,omitempty"`
}

func (c *Client) GitCommit(ctx context.Context, req GitCommitRequest) (*GitCommitResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "commit message is empty", nil)
	}

	for _, p := range req.Add {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		addArgs := []string{"add"}
		if p == "." {
			addArgs = append(addArgs, ".")
		} else {
			_, relSlash, pathErr := toolpath.ResolveWorkspacePath(c.root, p)
			if pathErr != nil {
				return nil, pathErr
			}
			addArgs = append(addArgs, relSlash)
		}
		if _, stderr, code, runErr := c.runGit(ctx, 30*time.Second, addArgs...); runErr != nil || code != 0 {
			if runErr != nil {
				return nil, runErr
			}
			return nil, protocol.NewError(protocol.ExecFailed, "git add failed",
				map[string]any{"path": p, "stderr": stderr, "exit": code})
		}
	}

	commitArgs := []string{"commit", "-m", msg}
	if req.AllowEmpty {
		commitArgs = append(commitArgs, "--allow-empty")
	}

	stdout, stderr, code, err := c.runGit(ctx, 30*time.Second, commitArgs...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, protocol.NewError(protocol.ExecFailed, "git commit failed",
			map[string]any{"stderr": stderr, "stdout": stdout, "exit": code})
	}

	hash := ""
	for _, line := range strings.Split(stdout+"\n"+stderr, "\n") {
		if idx := strings.Index(line, "["); idx >= 0 {
			inner := line[idx+1:]
			if ci := strings.Index(inner, "]"); ci >= 0 {
				parts := strings.Fields(inner[:ci])
				if len(parts) >= 2 {
					hash = parts[1]
				}
			}
			break
		}
	}

	return &GitCommitResponse{Output: stdout + stderr, Hash: hash}, nil
}

// ─── git.branch ───────────────────────────────────────────────────────────────

type GitBranchRequest struct {
	List   bool   `json:"list,omitempty"`
	Create string `json:"create,omitempty"`
	Delete string `json:"delete,omitempty"`
}

type GitBranchResponse struct {
	Output   string   `json:"output"`
	Branches []string `json:"branches,omitempty"`
	Current  string   `json:"current,omitempty"`
}

func (c *Client) GitBranch(ctx context.Context, req GitBranchRequest) (*GitBranchResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("runner is nil")
	}

	if req.Delete != "" {
		if !IsGitSafeRef(req.Delete) {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "invalid branch name",
				map[string]any{"branch": req.Delete})
		}
		stdout, stderr, code, err := c.runGit(ctx, 15*time.Second, "branch", "-d", req.Delete)
		if err != nil {
			return nil, err
		}
		if code != 0 {
			return nil, protocol.NewError(protocol.ExecFailed, "git branch delete failed",
				map[string]any{"stderr": stderr, "exit": code})
		}
		return &GitBranchResponse{Output: stdout + stderr}, nil
	}

	if req.Create != "" {
		if !IsGitSafeRef(req.Create) {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "invalid branch name",
				map[string]any{"branch": req.Create})
		}
		stdout, stderr, code, err := c.runGit(ctx, 15*time.Second, "branch", req.Create)
		if err != nil {
			return nil, err
		}
		if code != 0 {
			return nil, protocol.NewError(protocol.ExecFailed, "git branch create failed",
				map[string]any{"stderr": stderr, "exit": code})
		}
		return &GitBranchResponse{Output: stdout + stderr}, nil
	}

	stdout, stderr, code, err := c.runGit(ctx, 15*time.Second, "branch", "--list")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, protocol.NewError(protocol.ExecFailed, "git branch list failed",
			map[string]any{"stderr": stderr, "exit": code})
	}

	var branches []string
	current := ""
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "* ") {
			current = strings.TrimPrefix(line, "* ")
			branches = append(branches, current)
		} else {
			branches = append(branches, line)
		}
	}

	return &GitBranchResponse{Output: stdout, Branches: branches, Current: current}, nil
}

// ─── git.checkout ─────────────────────────────────────────────────────────────

type GitCheckoutRequest struct {
	Ref       string   `json:"ref,omitempty"`
	Paths     []string `json:"paths,omitempty"`
	NewBranch string   `json:"new_branch,omitempty"`
}

type GitCheckoutResponse struct {
	Output string `json:"output"`
}

func (c *Client) GitCheckout(ctx context.Context, req GitCheckoutRequest) (*GitCheckoutResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	if req.Ref == "" && len(req.Paths) == 0 && req.NewBranch == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "ref, paths, or new_branch required", nil)
	}
	if req.Ref != "" && !IsGitSafeRef(req.Ref) {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "invalid ref",
			map[string]any{"ref": req.Ref})
	}
	if req.NewBranch != "" && !IsGitSafeRef(req.NewBranch) {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "invalid new_branch name",
			map[string]any{"new_branch": req.NewBranch})
	}

	args := []string{"checkout"}
	if req.NewBranch != "" {
		args = append(args, "-b", req.NewBranch)
	}
	if req.Ref != "" {
		args = append(args, req.Ref)
	}
	if len(req.Paths) > 0 {
		args = append(args, "--")
		for _, p := range req.Paths {
			_, relSlash, pathErr := toolpath.ResolveWorkspacePath(c.root, p)
			if pathErr != nil {
				return nil, pathErr
			}
			args = append(args, relSlash)
		}
	}

	stdout, stderr, code, err := c.runGit(ctx, 30*time.Second, args...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, protocol.NewError(protocol.ExecFailed, "git checkout failed",
			map[string]any{"stderr": stderr, "exit": code})
	}
	return &GitCheckoutResponse{Output: stdout + stderr}, nil
}

// ─── git.push ─────────────────────────────────────────────────────────────────

type GitPushRequest struct {
	Remote      string `json:"remote,omitempty"`
	Branch      string `json:"branch,omitempty"`
	SetUpstream bool   `json:"set_upstream,omitempty"`
	Force       bool   `json:"force,omitempty"`
}

type GitPushResponse struct {
	Output string `json:"output"`
}

func (c *Client) GitPush(ctx context.Context, req GitPushRequest) (*GitPushResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	remote := strings.TrimSpace(req.Remote)
	if remote == "" {
		remote = "origin"
	}
	if !IsGitSafeRef(remote) {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "invalid remote name",
			map[string]any{"remote": remote})
	}
	if req.Branch != "" && !IsGitSafeRef(req.Branch) {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "invalid branch name",
			map[string]any{"branch": req.Branch})
	}

	args := []string{"push"}
	if req.SetUpstream {
		args = append(args, "-u")
	}
	if req.Force {
		args = append(args, "--force-with-lease")
	}
	args = append(args, remote)
	if req.Branch != "" {
		args = append(args, req.Branch)
	}

	stdout, stderr, code, err := c.runGit(ctx, 60*time.Second, args...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, protocol.NewError(protocol.ExecFailed, "git push failed",
			map[string]any{"stderr": stderr, "exit": code})
	}
	return &GitPushResponse{Output: stdout + stderr}, nil
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

