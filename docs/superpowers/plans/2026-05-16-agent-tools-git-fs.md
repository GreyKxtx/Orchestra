# Agent Tools — Git & FS Extended Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `fs.delete`, `fs.rename`, and 7 git tools (`git.status`, `git.diff`, `git.log`, `git.commit`, `git.branch`, `git.checkout`, `git.push`) to the Orchestra agent tool registry.

**Architecture:** Each tool follows the existing Request → Handler (`*Runner` method) → Response → `Call()` dispatch pattern in `internal/tools/`. Git read tools (`status`, `diff`, `log`) are always registered; git write tools (`commit`, `branch`, `checkout`, `push`) require `allowExec=true`, mirroring how `bash` is gated. All git tools shell out to the system `git` binary via a shared `runGit()` helper — no new dependencies. `ToolsVersion` is bumped once at the end from 6 → 7.

**Tech Stack:** Go standard library (`os`, `os/exec`, `path/filepath`, `bytes`, `strings`), existing `internal/protocol`, `internal/tools` patterns.

---

## File Map

| File | Change |
|------|--------|
| `internal/tools/fs_extra.go` | **Create**: `FSDelete`, `FSRename` implementations + Request/Response types |
| `internal/tools/fs_extra_test.go` | **Create**: tests for `fs.delete` and `fs.rename` |
| `internal/tools/git.go` | **Create**: `runGit` helper + all 7 git tool implementations |
| `internal/tools/git_test.go` | **Create**: tests for all git tools |
| `internal/tools/registry.go` | **Modify**: add 9 new `toolXxx()` defs, update `ListTools()`, `applyParallelFlags()` |
| `internal/tools/call.go` | **Modify**: add 9 dispatch `case` blocks |
| `internal/protocol/version.go` | **Modify**: `ToolsVersion` 6 → 7 |

---

### Task 1: `fs.delete` tool

**Files:**
- Create: `internal/tools/fs_extra.go`
- Create: `internal/tools/fs_extra_test.go`
- Modify: `internal/tools/registry.go`
- Modify: `internal/tools/call.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tools/fs_extra_test.go`:

```go
package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func newFSExtraRunner(t *testing.T) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

func TestFSDelete_File(t *testing.T) {
	r, root := newFSExtraRunner(t)

	path := filepath.Join(root, "todelete.txt")
	if err := os.WriteFile(path, []byte("bye"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := r.FSDelete(context.Background(), FSDeleteRequest{Path: "todelete.txt"})
	if err != nil {
		t.Fatalf("FSDelete: %v", err)
	}
	if resp.Path != "todelete.txt" {
		t.Errorf("path=%q, want %q", resp.Path, "todelete.txt")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("file still exists after delete")
	}
}

func TestFSDelete_Dir_Recursive(t *testing.T) {
	r, root := newFSExtraRunner(t)

	dir := filepath.Join(root, "subdir")
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := r.FSDelete(context.Background(), FSDeleteRequest{Path: "subdir", Recursive: true})
	if err != nil {
		t.Fatalf("FSDelete recursive: %v", err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Error("dir still exists after recursive delete")
	}
}

func TestFSDelete_NonRecursive_NonEmpty_Fails(t *testing.T) {
	r, root := newFSExtraRunner(t)

	dir := filepath.Join(root, "nonempty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := r.FSDelete(context.Background(), FSDeleteRequest{Path: "nonempty", Recursive: false})
	if err == nil {
		t.Fatal("expected error deleting non-empty dir without recursive=true")
	}
}

func TestFSDelete_PathTraversal(t *testing.T) {
	r, _ := newFSExtraRunner(t)
	_, err := r.FSDelete(context.Background(), FSDeleteRequest{Path: "../outside.txt"})
	if err == nil {
		t.Fatal("expected path traversal error")
	}
}

func TestFSDelete_NotExist(t *testing.T) {
	r, _ := newFSExtraRunner(t)
	_, err := r.FSDelete(context.Background(), FSDeleteRequest{Path: "nope.txt"})
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
}

func TestFSDelete_EmptyPath(t *testing.T) {
	r, _ := newFSExtraRunner(t)
	_, err := r.FSDelete(context.Background(), FSDeleteRequest{Path: ""})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/tools/ -run TestFSDelete -v
```

Expected: FAIL — `FSDelete` and `FSDeleteRequest` undefined.

- [ ] **Step 3: Implement `fs.delete`**

Create `internal/tools/fs_extra.go`:

```go
package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/orchestra/orchestra/internal/protocol"
)

// FSDeleteRequest is the input for the fs.delete tool.
type FSDeleteRequest struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive,omitempty"`
}

// FSDeleteResponse is the output of the fs.delete tool.
type FSDeleteResponse struct {
	Path string `json:"path"`
}

// FSDelete removes a file or directory at the given workspace-relative path.
// Requires recursive=true to remove non-empty directories.
// In dry-run mode the operation is a no-op (returns success without touching disk).
func (r *Runner) FSDelete(ctx context.Context, req FSDeleteRequest) (*FSDeleteResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	p := strings.TrimSpace(req.Path)
	if p == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}

	abs, relSlash, err := resolveWorkspacePath(r.workspaceRoot, p)
	if err != nil {
		return nil, err
	}

	if _, statErr := os.Stat(abs); os.IsNotExist(statErr) {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path does not exist",
			map[string]any{"path": relSlash})
	}

	if r.dryRun {
		return &FSDeleteResponse{Path: relSlash}, nil
	}

	var removeErr error
	if req.Recursive {
		removeErr = os.RemoveAll(abs)
	} else {
		removeErr = os.Remove(abs)
	}
	if removeErr != nil {
		return nil, protocol.NewError(protocol.ExecFailed,
			fmt.Sprintf("delete failed: %s", removeErr),
			map[string]any{"path": relSlash, "recursive": req.Recursive})
	}

	return &FSDeleteResponse{Path: relSlash}, nil
}
```

- [ ] **Step 4: Add `toolFSDelete()` to `registry.go`**

In `internal/tools/registry.go`, add `toolFSDelete()` to `ListTools()` after `toolFSEdit()`:

```go
toolFSDelete(),
```

Add to the `Mutating` case in `applyParallelFlags()`:

```go
case "write", "edit", "bash", "todowrite", "memory_write",
    "lsp.rename", "plan.enter", "plan.exit",
    "task.spawn", "task.wait", "task.cancel", "question",
    "fs.delete", "fs.rename",
    "git.commit", "git.branch", "git.checkout", "git.push":
    defs[i].Mutating = true
```

Add to the `ParallelSafe` case in `applyParallelFlags()`:

```go
case "ls", "read", "glob", "grep", "symbols", "explore",
    "todoread", "task.result", "runtime.query", "webfetch",
    "lsp.definition", "lsp.references", "lsp.hover", "lsp.diagnostics",
    "diff.preview",
    "git.status", "git.diff", "git.log":
    defs[i].ParallelSafe = true
```

Add the tool definition function at the bottom of `registry.go`:

```go
func toolFSDelete() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "fs.delete",
			Description: "Удалить файл или директорию по workspace-relative пути. Для непустых директорий требуется recursive=true.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path"],
  "properties": {
    "path":      { "type": "string", "minLength": 1, "description": "Workspace-relative путь для удаления." },
    "recursive": { "type": "boolean", "description": "Рекурсивно удалить непустую директорию. По умолчанию false." }
  }
}`),
		},
	}
}
```

- [ ] **Step 5: Add dispatch case to `call.go`**

In `internal/tools/call.go`, add before the `default:` case:

```go
case "fs.delete":
    var req FSDeleteRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.FSDelete(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)
```

- [ ] **Step 6: Run tests**

```
go test ./internal/tools/ -run TestFSDelete -v
```

Expected: PASS — all 6 tests.

- [ ] **Step 7: Build check**

```
go build ./...
```

Expected: no errors.

- [ ] **Step 8: Commit**

```
git add internal/tools/fs_extra.go internal/tools/fs_extra_test.go internal/tools/registry.go internal/tools/call.go
git commit -m "feat(tools): add fs.delete tool"
```

---

### Task 2: `fs.rename` tool

**Files:**
- Modify: `internal/tools/fs_extra.go`
- Modify: `internal/tools/fs_extra_test.go`
- Modify: `internal/tools/registry.go`
- Modify: `internal/tools/call.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/tools/fs_extra_test.go`:

```go
func TestFSRename_File(t *testing.T) {
	r, root := newFSExtraRunner(t)

	if err := os.WriteFile(filepath.Join(root, "before.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := r.FSRename(context.Background(), FSRenameRequest{Path: "before.txt", NewPath: "after.txt"})
	if err != nil {
		t.Fatalf("FSRename: %v", err)
	}
	if resp.Path != "before.txt" || resp.NewPath != "after.txt" {
		t.Errorf("unexpected resp: %+v", resp)
	}
	if _, statErr := os.Stat(filepath.Join(root, "before.txt")); !os.IsNotExist(statErr) {
		t.Error("old path still exists")
	}
	if _, statErr := os.Stat(filepath.Join(root, "after.txt")); statErr != nil {
		t.Errorf("new path doesn't exist: %v", statErr)
	}
}

func TestFSRename_CreatesParentDirs(t *testing.T) {
	r, root := newFSExtraRunner(t)

	if err := os.WriteFile(filepath.Join(root, "src.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := r.FSRename(context.Background(), FSRenameRequest{Path: "src.txt", NewPath: "newdir/dst.txt"})
	if err != nil {
		t.Fatalf("FSRename: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "newdir", "dst.txt")); statErr != nil {
		t.Errorf("new path doesn't exist: %v", statErr)
	}
}

func TestFSRename_PathTraversal_Src(t *testing.T) {
	r, _ := newFSExtraRunner(t)
	_, err := r.FSRename(context.Background(), FSRenameRequest{Path: "../out.txt", NewPath: "dst.txt"})
	if err == nil {
		t.Fatal("expected path traversal error on src")
	}
}

func TestFSRename_PathTraversal_Dst(t *testing.T) {
	r, root := newFSExtraRunner(t)
	if err := os.WriteFile(filepath.Join(root, "src.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := r.FSRename(context.Background(), FSRenameRequest{Path: "src.txt", NewPath: "../escape.txt"})
	if err == nil {
		t.Fatal("expected path traversal error on dst")
	}
}

func TestFSRename_SrcNotExist(t *testing.T) {
	r, _ := newFSExtraRunner(t)
	_, err := r.FSRename(context.Background(), FSRenameRequest{Path: "nope.txt", NewPath: "out.txt"})
	if err == nil {
		t.Fatal("expected error when src does not exist")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/tools/ -run TestFSRename -v
```

Expected: FAIL — `FSRename` and `FSRenameRequest` undefined.

- [ ] **Step 3: Implement `fs.rename`**

Append to `internal/tools/fs_extra.go`:

```go
// FSRenameRequest is the input for the fs.rename tool.
type FSRenameRequest struct {
	Path    string `json:"path"`
	NewPath string `json:"new_path"`
}

// FSRenameResponse is the output of the fs.rename tool.
type FSRenameResponse struct {
	Path    string `json:"path"`
	NewPath string `json:"new_path"`
}

// FSRename moves or renames a file or directory within the workspace.
// Parent directories of new_path are created automatically.
// In dry-run mode the operation is a no-op (returns success without touching disk).
func (r *Runner) FSRename(ctx context.Context, req FSRenameRequest) (*FSRenameResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	src := strings.TrimSpace(req.Path)
	dst := strings.TrimSpace(req.NewPath)
	if src == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	if dst == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "new_path is empty", nil)
	}

	absSrc, relSrc, err := resolveWorkspacePath(r.workspaceRoot, src)
	if err != nil {
		return nil, err
	}
	absDst, relDst, err := resolveWorkspacePath(r.workspaceRoot, dst)
	if err != nil {
		return nil, err
	}

	if _, statErr := os.Stat(absSrc); os.IsNotExist(statErr) {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "source path does not exist",
			map[string]any{"path": relSrc})
	}

	if r.dryRun {
		return &FSRenameResponse{Path: relSrc, NewPath: relDst}, nil
	}

	if mkErr := os.MkdirAll(filepath.Dir(absDst), 0o755); mkErr != nil {
		return nil, protocol.NewError(protocol.ExecFailed, "failed to create parent directories",
			map[string]any{"new_path": relDst, "error": mkErr.Error()})
	}

	if renameErr := os.Rename(absSrc, absDst); renameErr != nil {
		return nil, protocol.NewError(protocol.ExecFailed,
			fmt.Sprintf("rename failed: %s", renameErr),
			map[string]any{"path": relSrc, "new_path": relDst})
	}

	return &FSRenameResponse{Path: relSrc, NewPath: relDst}, nil
}
```

Add `"path/filepath"` to the import block in `fs_extra.go` (after `"os"`):

```go
import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/protocol"
)
```

- [ ] **Step 4: Add `toolFSRename()` to `registry.go`**

Add to `ListTools()` after `toolFSDelete()`:

```go
toolFSRename(),
```

Add the tool definition function:

```go
func toolFSRename() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "fs.rename",
			Description: "Переместить или переименовать файл/директорию внутри workspace. Родительские директории new_path создаются автоматически.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "new_path"],
  "properties": {
    "path":     { "type": "string", "minLength": 1, "description": "Workspace-relative путь источника." },
    "new_path": { "type": "string", "minLength": 1, "description": "Workspace-relative путь назначения." }
  }
}`),
		},
	}
}
```

- [ ] **Step 5: Add dispatch case to `call.go`**

```go
case "fs.rename":
    var req FSRenameRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.FSRename(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)
```

- [ ] **Step 6: Run tests**

```
go test ./internal/tools/ -run "TestFSDelete|TestFSRename" -v
```

Expected: PASS — all 11 tests.

- [ ] **Step 7: Build check**

```
go build ./...
```

- [ ] **Step 8: Commit**

```
git add internal/tools/fs_extra.go internal/tools/fs_extra_test.go internal/tools/registry.go internal/tools/call.go
git commit -m "feat(tools): add fs.rename tool"
```

---

### Task 3: Git helper + `git.status` + `git.log`

**Files:**
- Create: `internal/tools/git.go`
- Create: `internal/tools/git_test.go`
- Modify: `internal/tools/registry.go`
- Modify: `internal/tools/call.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tools/git_test.go`:

```go
package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initGitRepo initialises a git repo in root with a single initial commit.
func initGitRepo(t *testing.T, root string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "readme.txt")
	run("commit", "-m", "initial commit")
}

func newGitRunner(t *testing.T) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func TestGitStatus_CleanRepo(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	resp, err := r.GitStatus(context.Background(), GitStatusRequest{})
	if err != nil {
		t.Fatalf("GitStatus: %v", err)
	}
	if !resp.Clean {
		t.Errorf("expected clean repo, got output: %q", resp.Output)
	}
}

func TestGitStatus_DirtyRepo(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := r.GitStatus(context.Background(), GitStatusRequest{})
	if err != nil {
		t.Fatalf("GitStatus: %v", err)
	}
	if resp.Clean {
		t.Error("expected dirty repo")
	}
	if !strings.Contains(resp.Output, "new.txt") {
		t.Errorf("expected new.txt in status output, got: %q", resp.Output)
	}
}

func TestGitStatus_ReturnsCurrentBranch(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	resp, err := r.GitStatus(context.Background(), GitStatusRequest{})
	if err != nil {
		t.Fatalf("GitStatus: %v", err)
	}
	if resp.Branch != "main" {
		t.Errorf("expected branch 'main', got %q", resp.Branch)
	}
}

func TestGitLog_Basic(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	resp, err := r.GitLog(context.Background(), GitLogRequest{N: 5})
	if err != nil {
		t.Fatalf("GitLog: %v", err)
	}
	if !strings.Contains(resp.Output, "initial commit") {
		t.Errorf("expected 'initial commit' in log, got: %q", resp.Output)
	}
}

func TestGitLog_Oneline(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	resp, err := r.GitLog(context.Background(), GitLogRequest{N: 5, Oneline: true})
	if err != nil {
		t.Fatalf("GitLog oneline: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(resp.Output), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line in oneline log, got %d: %q", len(lines), resp.Output)
	}
}

func TestGitLog_InvalidRef(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	_, err := r.GitLog(context.Background(), GitLogRequest{Ref: "../../etc/passwd"})
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/tools/ -run "TestGitStatus|TestGitLog" -v
```

Expected: FAIL — `GitStatus`, `GitStatusRequest`, `GitLog`, `GitLogRequest` undefined.

- [ ] **Step 3: Implement git helper + `git.status` + `git.log`**

Create `internal/tools/git.go`:

```go
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
// Args are constructed by the caller — never pass raw user input as a shell string.
// Returns stdout, stderr, exitCode; err is non-nil only for start failures or timeout.
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

// isGitSafeRef returns true if s contains only characters valid in a git ref
// (letters, digits, /, -, _, ., ~, ^, @, {, }). Prevents shell injection.
func isGitSafeRef(s string) bool {
	if s == "" {
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

	// First line is "## branch...remote" or "## branch".
	branch := ""
	lines := strings.SplitN(stdout, "\n", 2)
	if len(lines) > 0 && strings.HasPrefix(lines[0], "## ") {
		bl := strings.TrimPrefix(lines[0], "## ")
		if idx := strings.IndexAny(bl, ". "); idx >= 0 {
			branch = bl[:idx]
		} else {
			branch = bl
		}
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
	N       int    `json:"n,omitempty"`       // max commits, default 20, max 200
	Ref     string `json:"ref,omitempty"`     // branch/tag/commit to start from
	Path    string `json:"path,omitempty"`    // limit to commits touching this path
	Oneline bool   `json:"oneline,omitempty"` // compact single-line format
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
```

- [ ] **Step 4: Add tool definitions to `registry.go`**

Add to `ListTools()` (unconditionally, before the `if allowExec` block):

```go
toolGitStatus(),
toolGitLog(),
```

Add definition functions at the bottom of `registry.go`:

```go
func toolGitStatus() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.status",
			Description: "Показать текущий git-статус workspace — staged/unstaged изменения, untracked файлы, текущую ветку.",
			Parameters:  mustSchema(`{"type":"object","additionalProperties":false,"properties":{}}`),
		},
	}
}

func toolGitLog() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.log",
			Description: "Показать историю коммитов. n ограничивает количество (по умолчанию 20). Опционально фильтруется по ref или пути.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "n":       { "type": "integer", "minimum": 1, "maximum": 200, "description": "Макс. число коммитов. По умолчанию 20." },
    "ref":     { "type": "string", "description": "Ветка, тег или коммит." },
    "path":    { "type": "string", "description": "Ограничить коммитами, затрагивающими этот workspace-relative путь." },
    "oneline": { "type": "boolean", "description": "Компактный однострочный формат." }
  }
}`),
		},
	}
}
```

- [ ] **Step 5: Add dispatch cases to `call.go`**

```go
case "git.status":
    var req GitStatusRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.GitStatus(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)

case "git.log":
    var req GitLogRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.GitLog(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)
```

- [ ] **Step 6: Run tests**

```
go test ./internal/tools/ -run "TestGitStatus|TestGitLog" -v
```

Expected: PASS.

- [ ] **Step 7: Build check**

```
go build ./...
```

- [ ] **Step 8: Commit**

```
git add internal/tools/git.go internal/tools/git_test.go internal/tools/registry.go internal/tools/call.go
git commit -m "feat(tools): add runGit helper + git.status + git.log"
```

---

### Task 4: `git.diff` tool

**Files:**
- Modify: `internal/tools/git.go`
- Modify: `internal/tools/git_test.go`
- Modify: `internal/tools/registry.go`
- Modify: `internal/tools/call.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/tools/git_test.go`:

```go
func TestGitDiff_UnstagedChange(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := r.GitDiff(context.Background(), GitDiffRequest{})
	if err != nil {
		t.Fatalf("GitDiff: %v", err)
	}
	if !strings.Contains(resp.Output, "readme.txt") {
		t.Errorf("expected readme.txt in diff, got: %q", resp.Output)
	}
}

func TestGitDiff_Staged(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stagecmd := exec.Command("git", "add", "readme.txt")
	stagecmd.Dir = root
	if err := stagecmd.Run(); err != nil {
		t.Fatal(err)
	}

	resp, err := r.GitDiff(context.Background(), GitDiffRequest{Staged: true})
	if err != nil {
		t.Fatalf("GitDiff staged: %v", err)
	}
	if !strings.Contains(resp.Output, "readme.txt") {
		t.Errorf("expected readme.txt in staged diff, got: %q", resp.Output)
	}
}

func TestGitDiff_NoChanges(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	resp, err := r.GitDiff(context.Background(), GitDiffRequest{})
	if err != nil {
		t.Fatalf("GitDiff: %v", err)
	}
	if strings.TrimSpace(resp.Output) != "" {
		t.Errorf("expected empty diff for clean repo, got: %q", resp.Output)
	}
}

func TestGitDiff_InvalidRef(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	_, err := r.GitDiff(context.Background(), GitDiffRequest{Ref: "$(evil)"})
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/tools/ -run TestGitDiff -v
```

Expected: FAIL — `GitDiff` and `GitDiffRequest` undefined.

- [ ] **Step 3: Implement `git.diff`**

Append to `internal/tools/git.go`:

```go
// ─── git.diff ────────────────────────────────────────────────────────────────

// GitDiffRequest is the input for the git.diff tool.
type GitDiffRequest struct {
	Staged bool   `json:"staged,omitempty"` // --cached: show staged changes
	Ref    string `json:"ref,omitempty"`    // compare to this commit/branch
	Path   string `json:"path,omitempty"`   // limit diff to this workspace-relative path
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
```

- [ ] **Step 4: Add `toolGitDiff()` to `registry.go`**

Add to `ListTools()`:

```go
toolGitDiff(),
```

Add definition:

```go
func toolGitDiff() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.diff",
			Description: "Показать diff несохранённых изменений. staged=true — staged (--cached). ref — сравнение с конкретным коммитом/веткой.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "staged": { "type": "boolean", "description": "Показать staged (--cached) изменения вместо unstaged." },
    "ref":    { "type": "string", "description": "Сравнить с этим коммитом, веткой или тегом." },
    "path":   { "type": "string", "description": "Ограничить diff этим workspace-relative файлом или директорией." }
  }
}`),
		},
	}
}
```

- [ ] **Step 5: Add dispatch case to `call.go`**

```go
case "git.diff":
    var req GitDiffRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.GitDiff(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)
```

- [ ] **Step 6: Run tests**

```
go test ./internal/tools/ -run TestGitDiff -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```
git add internal/tools/git.go internal/tools/git_test.go internal/tools/registry.go internal/tools/call.go
git commit -m "feat(tools): add git.diff tool"
```

---

### Task 5: Git write tools (`git.commit`, `git.branch`, `git.checkout`, `git.push`)

These tools are gated by `allowExec=true` — added to `ListTools()` only in the `if allowExec` block, mirroring `bash`.

**Files:**
- Modify: `internal/tools/git.go`
- Modify: `internal/tools/git_test.go`
- Modify: `internal/tools/registry.go`
- Modify: `internal/tools/call.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/tools/git_test.go`:

```go
func TestGitCommit_Basic(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := r.GitCommit(context.Background(), GitCommitRequest{
		Message: "add new.txt",
		Add:     []string{"new.txt"},
	})
	if err != nil {
		t.Fatalf("GitCommit: %v", err)
	}
	if resp.Hash == "" {
		t.Error("expected non-empty commit hash")
	}
	if !strings.Contains(resp.Output, "add new.txt") {
		t.Errorf("expected commit message in output, got: %q", resp.Output)
	}
}

func TestGitCommit_EmptyMessageFails(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)
	_, err := r.GitCommit(context.Background(), GitCommitRequest{Message: ""})
	if err == nil {
		t.Fatal("expected error for empty commit message")
	}
}

func TestGitBranch_List(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	resp, err := r.GitBranch(context.Background(), GitBranchRequest{List: true})
	if err != nil {
		t.Fatalf("GitBranch list: %v", err)
	}
	found := false
	for _, b := range resp.Branches {
		if b == "main" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'main' in branches: %v", resp.Branches)
	}
	if resp.Current != "main" {
		t.Errorf("expected current='main', got %q", resp.Current)
	}
}

func TestGitBranch_Create(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	_, err := r.GitBranch(context.Background(), GitBranchRequest{Create: "feature/test"})
	if err != nil {
		t.Fatalf("GitBranch create: %v", err)
	}

	out, _ := exec.Command("git", "-C", root, "branch", "--list", "feature/test").Output()
	if !strings.Contains(string(out), "feature/test") {
		t.Error("branch 'feature/test' was not created")
	}
}

func TestGitBranch_InvalidNameFails(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	_, err := r.GitBranch(context.Background(), GitBranchRequest{Create: "bad name!"})
	if err == nil {
		t.Fatal("expected error for invalid branch name")
	}
}

func TestGitCheckout_SwitchBranch(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	branchCmd := exec.Command("git", "branch", "other")
	branchCmd.Dir = root
	if err := branchCmd.Run(); err != nil {
		t.Fatal(err)
	}

	_, err := r.GitCheckout(context.Background(), GitCheckoutRequest{Ref: "other"})
	if err != nil {
		t.Fatalf("GitCheckout: %v", err)
	}

	out, _ := exec.Command("git", "-C", root, "branch", "--show-current").Output()
	if strings.TrimSpace(string(out)) != "other" {
		t.Errorf("expected branch 'other', got %q", string(out))
	}
}

func TestGitCheckout_NewBranch(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	_, err := r.GitCheckout(context.Background(), GitCheckoutRequest{
		Ref:       "main",
		NewBranch: "feature/new",
	})
	if err != nil {
		t.Fatalf("GitCheckout -b: %v", err)
	}

	out, _ := exec.Command("git", "-C", root, "branch", "--show-current").Output()
	if strings.TrimSpace(string(out)) != "feature/new" {
		t.Errorf("expected branch 'feature/new', got %q", string(out))
	}
}

func TestGitCheckout_InvalidRefFails(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	_, err := r.GitCheckout(context.Background(), GitCheckoutRequest{Ref: "; rm -rf"})
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./internal/tools/ -run "TestGitCommit|TestGitBranch|TestGitCheckout" -v
```

Expected: FAIL — `GitCommit`, `GitBranch`, `GitCheckout` undefined.

- [ ] **Step 3: Implement write git tools**

Append to `internal/tools/git.go`:

```go
// ─── git.commit ───────────────────────────────────────────────────────────────

// GitCommitRequest is the input for the git.commit tool.
type GitCommitRequest struct {
	Message    string   `json:"message"`
	Add        []string `json:"add,omitempty"`         // paths to stage; ["."] for all
	AllowEmpty bool     `json:"allow_empty,omitempty"` // --allow-empty
}

// GitCommitResponse is the output of the git.commit tool.
type GitCommitResponse struct {
	Output string `json:"output"`
	Hash   string `json:"hash,omitempty"`
}

// GitCommit optionally stages paths, then creates a commit.
func (r *Runner) GitCommit(ctx context.Context, req GitCommitRequest) (*GitCommitResponse, error) {
	if r == nil {
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
			_, relSlash, pathErr := resolveWorkspacePath(r.workspaceRoot, p)
			if pathErr != nil {
				return nil, pathErr
			}
			addArgs = append(addArgs, relSlash)
		}
		if _, stderr, code, runErr := r.runGit(ctx, 30*time.Second, addArgs...); runErr != nil || code != 0 {
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

	stdout, stderr, code, err := r.runGit(ctx, 30*time.Second, commitArgs...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, protocol.NewError(protocol.ExecFailed, "git commit failed",
			map[string]any{"stderr": stderr, "stdout": stdout, "exit": code})
	}

	// Extract short hash from "[main abc1234] message" line.
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

// GitBranchRequest is the input for the git.branch tool.
type GitBranchRequest struct {
	List   bool   `json:"list,omitempty"`   // list branches (default when nothing else set)
	Create string `json:"create,omitempty"` // create a branch with this name
	Delete string `json:"delete,omitempty"` // delete a branch with this name
}

// GitBranchResponse is the output of the git.branch tool.
type GitBranchResponse struct {
	Output   string   `json:"output"`
	Branches []string `json:"branches,omitempty"`
	Current  string   `json:"current,omitempty"`
}

// GitBranch lists, creates, or deletes a local git branch.
func (r *Runner) GitBranch(ctx context.Context, req GitBranchRequest) (*GitBranchResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}

	if req.Delete != "" {
		if !isGitSafeRef(req.Delete) {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "invalid branch name",
				map[string]any{"branch": req.Delete})
		}
		stdout, stderr, code, err := r.runGit(ctx, 15*time.Second, "branch", "-d", req.Delete)
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
		if !isGitSafeRef(req.Create) {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "invalid branch name",
				map[string]any{"branch": req.Create})
		}
		stdout, stderr, code, err := r.runGit(ctx, 15*time.Second, "branch", req.Create)
		if err != nil {
			return nil, err
		}
		if code != 0 {
			return nil, protocol.NewError(protocol.ExecFailed, "git branch create failed",
				map[string]any{"stderr": stderr, "exit": code})
		}
		return &GitBranchResponse{Output: stdout + stderr}, nil
	}

	// Default: list.
	stdout, stderr, code, err := r.runGit(ctx, 15*time.Second, "branch", "--list")
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

// GitCheckoutRequest is the input for the git.checkout tool.
type GitCheckoutRequest struct {
	Ref       string   `json:"ref,omitempty"`        // branch, tag, or commit
	Paths     []string `json:"paths,omitempty"`      // restore these workspace-relative paths
	NewBranch string   `json:"new_branch,omitempty"` // -b: create and switch
}

// GitCheckoutResponse is the output of the git.checkout tool.
type GitCheckoutResponse struct {
	Output string `json:"output"`
}

// GitCheckout switches to a branch/commit or restores specific files.
func (r *Runner) GitCheckout(ctx context.Context, req GitCheckoutRequest) (*GitCheckoutResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	if req.Ref == "" && len(req.Paths) == 0 && req.NewBranch == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "ref, paths, or new_branch required", nil)
	}
	if req.Ref != "" && !isGitSafeRef(req.Ref) {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "invalid ref",
			map[string]any{"ref": req.Ref})
	}
	if req.NewBranch != "" && !isGitSafeRef(req.NewBranch) {
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
			_, relSlash, pathErr := resolveWorkspacePath(r.workspaceRoot, p)
			if pathErr != nil {
				return nil, pathErr
			}
			args = append(args, relSlash)
		}
	}

	stdout, stderr, code, err := r.runGit(ctx, 30*time.Second, args...)
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

// GitPushRequest is the input for the git.push tool.
type GitPushRequest struct {
	Remote      string `json:"remote,omitempty"`       // default "origin"
	Branch      string `json:"branch,omitempty"`       // default: current branch
	SetUpstream bool   `json:"set_upstream,omitempty"` // -u
	Force       bool   `json:"force,omitempty"`        // --force-with-lease (safer than --force)
}

// GitPushResponse is the output of the git.push tool.
type GitPushResponse struct {
	Output string `json:"output"`
}

// GitPush pushes to a remote. Uses --force-with-lease when force=true.
func (r *Runner) GitPush(ctx context.Context, req GitPushRequest) (*GitPushResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	remote := strings.TrimSpace(req.Remote)
	if remote == "" {
		remote = "origin"
	}
	if !isGitSafeRef(remote) {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "invalid remote name",
			map[string]any{"remote": remote})
	}
	if req.Branch != "" && !isGitSafeRef(req.Branch) {
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

	stdout, stderr, code, err := r.runGit(ctx, 60*time.Second, args...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, protocol.NewError(protocol.ExecFailed, "git push failed",
			map[string]any{"stderr": stderr, "exit": code})
	}
	return &GitPushResponse{Output: stdout + stderr}, nil
}
```

- [ ] **Step 4: Add write git tools to `registry.go`**

In `ListTools()`, add inside the `if allowExec` block:

```go
if allowExec {
    out = append(out, toolExecRun())
    out = append(out, toolGitCommit())
    out = append(out, toolGitBranch())
    out = append(out, toolGitCheckout())
    out = append(out, toolGitPush())
}
```

Add definition functions:

```go
func toolGitCommit() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.commit",
			Description: "Добавить файлы в stage и создать git коммит. Используй add=[\".\"} для stage всех изменений.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["message"],
  "properties": {
    "message":     { "type": "string", "minLength": 1, "description": "Commit message." },
    "add":         { "type": "array", "items": {"type":"string"}, "description": "Workspace-relative пути для git add. Используй [\".\"] для всего." },
    "allow_empty": { "type": "boolean", "description": "Разрешить коммит без изменений." }
  }
}`),
		},
	}
}

func toolGitBranch() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.branch",
			Description: "Список, создание или удаление локальной ветки. По умолчанию (без параметров) — список.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "list":   { "type": "boolean", "description": "Список локальных веток (по умолчанию)." },
    "create": { "type": "string",  "description": "Создать ветку с этим именем." },
    "delete": { "type": "string",  "description": "Удалить ветку с этим именем." }
  }
}`),
		},
	}
}

func toolGitCheckout() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.checkout",
			Description: "Переключиться на ветку/коммит или восстановить файлы. new_branch создаёт ветку и переключает (-b).",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "ref":        { "type": "string", "description": "Ветка, тег или коммит для переключения." },
    "paths":      { "type": "array",  "items": {"type":"string"}, "description": "Workspace-relative пути для восстановления из HEAD." },
    "new_branch": { "type": "string", "description": "Создать ветку с этим именем и переключиться (-b)." }
  }
}`),
		},
	}
}

func toolGitPush() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.push",
			Description: "Push текущей ветки на remote. force=true использует --force-with-lease (безопаснее --force). По умолчанию remote='origin'.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "remote":       { "type": "string",  "description": "Имя remote. По умолчанию 'origin'." },
    "branch":       { "type": "string",  "description": "Ветка для push. По умолчанию текущая." },
    "set_upstream": { "type": "boolean", "description": "Установить upstream tracking (-u)." },
    "force":        { "type": "boolean", "description": "Push с --force-with-lease." }
  }
}`),
		},
	}
}
```

- [ ] **Step 5: Add dispatch cases to `call.go`**

```go
case "git.commit":
    var req GitCommitRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.GitCommit(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)

case "git.branch":
    var req GitBranchRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.GitBranch(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)

case "git.checkout":
    var req GitCheckoutRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.GitCheckout(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)

case "git.push":
    var req GitPushRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.GitPush(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)
```

- [ ] **Step 6: Run tests**

```
go test ./internal/tools/ -run "TestGitCommit|TestGitBranch|TestGitCheckout" -v
```

Expected: PASS.

- [ ] **Step 7: Build check**

```
go build ./...
```

- [ ] **Step 8: Commit**

```
git add internal/tools/git.go internal/tools/git_test.go internal/tools/registry.go internal/tools/call.go
git commit -m "feat(tools): add git write tools (commit/branch/checkout/push)"
```

---

### Task 6: Bump `ToolsVersion` + verification test

**Files:**
- Modify: `internal/protocol/version.go`
- Modify: `internal/tools/registry_test.go`

- [ ] **Step 1: Bump ToolsVersion**

In `internal/protocol/version.go`, change:

```go
ToolsVersion = 6
```

to:

```go
ToolsVersion = 7
```

- [ ] **Step 2: Add registry verification test**

Open `internal/tools/registry_test.go` and append (or create the file if absent):

```go
func TestListTools_NewToolsPresent(t *testing.T) {
	// With allowExec: all 9 new tools must appear.
	all := ListTools(true, true)
	names := make(map[string]bool, len(all))
	for _, td := range all {
		names[td.Function.Name] = true
	}
	mustHave := []string{
		"fs.delete", "fs.rename",
		"git.status", "git.diff", "git.log",
		"git.commit", "git.branch", "git.checkout", "git.push",
	}
	for _, name := range mustHave {
		if !names[name] {
			t.Errorf("tool %q missing from ListTools(allowExec=true)", name)
		}
	}

	// Without allowExec: read-only git tools still appear; write git tools do not.
	noExec := ListTools(false, false)
	noExecNames := make(map[string]bool, len(noExec))
	for _, td := range noExec {
		noExecNames[td.Function.Name] = true
	}
	for _, name := range []string{"git.status", "git.diff", "git.log", "fs.delete", "fs.rename"} {
		if !noExecNames[name] {
			t.Errorf("tool %q should appear without allowExec, but it's missing", name)
		}
	}
	for _, name := range []string{"git.commit", "git.branch", "git.checkout", "git.push"} {
		if noExecNames[name] {
			t.Errorf("write git tool %q should NOT appear without allowExec", name)
		}
	}
}
```

- [ ] **Step 3: Run test**

```
go test ./internal/tools/ -run TestListTools_NewToolsPresent -v
```

Expected: PASS.

- [ ] **Step 4: Run full test suite**

```
go test ./...
```

Expected: all tests pass (git tests skipped if git not on PATH, which is fine for CI).

- [ ] **Step 5: Build final binary**

```
go build -o orchestra.exe ./cmd/orchestra
```

Expected: clean build, no errors.

- [ ] **Step 6: Commit**

```
git add internal/protocol/version.go internal/tools/registry_test.go
git commit -m "feat(tools): bump ToolsVersion to 7; add registry verification test"
```

---

## Self-Review

**Spec coverage:**
- [x] `fs.delete` — Task 1
- [x] `fs.rename` — Task 2
- [x] `git.status` — Task 3
- [x] `git.log` — Task 3
- [x] `git.diff` — Task 4
- [x] `git.commit` — Task 5
- [x] `git.branch` — Task 5
- [x] `git.checkout` — Task 5
- [x] `git.push` — Task 5
- [x] `ToolsVersion` bump — Task 6

**Placeholder scan:** No TBDs. All code is concrete Go.

**Type consistency:**
- `GitStatusRequest` (Task 3) → `GitStatus(ctx, GitStatusRequest{})` (tests Task 3) ✓
- `GitDiffRequest` (Task 4) → `GitDiff(ctx, GitDiffRequest{})` (tests Task 4) ✓
- `GitLogRequest` (Task 3) → `GitLog(ctx, GitLogRequest{})` (tests Task 3) ✓
- `GitCommitRequest` (Task 5) → `GitCommit(ctx, GitCommitRequest{})` (tests Task 5) ✓
- `GitBranchRequest` (Task 5) → `GitBranch(ctx, GitBranchRequest{})` (tests Task 5) ✓
- `GitCheckoutRequest` (Task 5) → `GitCheckout(ctx, GitCheckoutRequest{})` (tests Task 5) ✓
- `GitPushRequest` (Task 5) → `GitPush(ctx, GitPushRequest{})` (tests Task 5) ✓
- `FSDeleteRequest` (Task 1) → `FSDelete(ctx, FSDeleteRequest{})` (tests Task 1) ✓
- `FSRenameRequest` (Task 2) → `FSRename(ctx, FSRenameRequest{})` (tests Task 2) ✓

All dispatch names in `call.go` match tool names in `registry.go`. All `applyParallelFlags()` entries match.
