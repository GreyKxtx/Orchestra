# GitHub/PR Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Добавить 5 GitHub-инструментов для агента (`gh.pr.list`, `gh.pr.create`, `gh.pr.view`, `gh.issue.list`, `gh.issue.view`), использующих `gh` CLI.

**Architecture:** Новый файл `internal/tools/github.go` реализует `ghAvailable()` (sync.Once probe), `runGH()` helper и 5 методов на `Runner`. Все инструменты гейтированы через `allowExec` (как `git.commit`/`git.push`). `gh` CLI должен быть установлен и авторизован — без него методы возвращают понятную ошибку.

**Tech Stack:** Go stdlib (`os/exec`, `bytes`, `encoding/json`, `sync`), `gh` CLI (опциональная внешняя зависимость, GitHub CLI).

---

## File Structure

- Create: `internal/tools/github.go` — `ghAvailable()`, `runGH()`, 5 методов Runner
- Create: `internal/tools/github_test.go` — тесты (skip если gh не в PATH)
- Modify: `internal/tools/registry.go` — 5 tool defs в блоке `allowExec` + parallel flags
- Modify: `internal/tools/call.go` — 5 case handlers
- Modify: `internal/protocol/version.go` — ToolsVersion 9 → 10
- Modify: `internal/config/config.go` — добавить 5 имён в `validAgentToolNames`

---

### Task 1: `internal/tools/github.go` + тесты

**Files:**
- Create: `internal/tools/github.go`
- Create: `internal/tools/github_test.go`

- [ ] **Step 1: Написать failing тест для input validation**

Создай `internal/tools/github_test.go`:

```go
package tools

import (
	"context"
	"os/exec"
	"testing"
)

func TestGHPRCreate_EmptyTitle(t *testing.T) {
	r := &Runner{workspaceRoot: t.TempDir()}
	_, err := r.GHPRCreate(context.Background(), GHPRCreateRequest{Title: ""})
	if err == nil {
		t.Error("expected error for empty title, got nil")
	}
}

func TestGHPRCreate_WhitespaceTitle(t *testing.T) {
	r := &Runner{workspaceRoot: t.TempDir()}
	_, err := r.GHPRCreate(context.Background(), GHPRCreateRequest{Title: "   "})
	if err == nil {
		t.Error("expected error for whitespace title, got nil")
	}
}

func TestGHAvailable_DoesNotPanic(t *testing.T) {
	// Just verify the function runs and returns a consistent result.
	first := ghAvailable()
	second := ghAvailable()
	if first != second {
		t.Error("ghAvailable() must return consistent result (sync.Once)")
	}
}

// Integration tests — skipped when gh is not installed.

func TestGHPRList_SkipNoGH(t *testing.T) {
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh not in PATH")
	}
	r := &Runner{workspaceRoot: t.TempDir()}
	// Running in a temp dir (not a git repo) — gh should return an error,
	// but the function must not panic.
	_, _ = r.GHPRList(context.Background(), GHPRListRequest{})
}

func TestGHIssueList_SkipNoGH(t *testing.T) {
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh not in PATH")
	}
	r := &Runner{workspaceRoot: t.TempDir()}
	_, _ = r.GHIssueList(context.Background(), GHIssueListRequest{})
}
```

- [ ] **Step 2: Убедиться что тест не компилируется**

```powershell
go test ./internal/tools/... -run TestGHPRCreate -v 2>&1 | Select-String "undefined|cannot|FAIL"
```

Ожидается: ошибка компиляции — `undefined: GHPRCreate`.

- [ ] **Step 3: Создать `internal/tools/github.go`**

```go
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const ghOutputLimit = 512 * 1024 // 512 KB

var (
	ghOnce  sync.Once
	ghFound bool
	ghBin   string
)

// ghAvailable reports whether the gh CLI is available in PATH.
// The result is cached after the first call.
func ghAvailable() bool {
	ghOnce.Do(func() {
		p, err := exec.LookPath("gh")
		if err == nil {
			ghBin = p
			ghFound = true
		}
	})
	return ghFound
}

// runGH runs a gh CLI command in the workspace root and returns stdout bytes.
// stderr is captured and included in errors. Timeout defaults to 30 s.
func runGH(ctx context.Context, workdir string, args ...string) ([]byte, error) {
	if !ghAvailable() {
		return nil, fmt.Errorf("gh CLI not available in PATH")
	}
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(tctx, ghBin, args...)
	cmd.Dir = workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		// Trim to a reasonable size for error messages.
		if len(msg) > 512 {
			msg = msg[:512]
		}
		return nil, fmt.Errorf("gh %s: %s", args[0], msg)
	}

	out := stdout.Bytes()
	if len(out) > ghOutputLimit {
		out = out[:ghOutputLimit]
	}
	return out, nil
}

// ─── gh.pr.list ──────────────────────────────────────────────────────────────

// GHPRListRequest is the input for the gh.pr.list tool.
type GHPRListRequest struct {
	State string `json:"state,omitempty"` // "open" (default), "closed", "merged", "all"
	Limit int    `json:"limit,omitempty"` // default 20
	Base  string `json:"base,omitempty"`  // filter by base branch
}

// GHPRListItem is one entry in the PR list.
type GHPRListItem struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	State     string `json:"state"`
	Author    string `json:"author"`
	URL       string `json:"url"`
	Base      string `json:"base"`
	Head      string `json:"head"`
	UpdatedAt string `json:"updated_at"`
}

// GHPRListResponse is the output of the gh.pr.list tool.
type GHPRListResponse struct {
	PRs []GHPRListItem `json:"prs"`
}

// GHPRList lists pull requests in the current repo.
func (r *Runner) GHPRList(ctx context.Context, req GHPRListRequest) (*GHPRListResponse, error) {
	args := []string{
		"pr", "list",
		"--json", "number,title,state,author,url,baseRefName,headRefName,updatedAt",
	}
	state := req.State
	if state == "" {
		state = "open"
	}
	args = append(args, "--state", state)

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	args = append(args, "--limit", fmt.Sprintf("%d", limit))

	if req.Base != "" {
		args = append(args, "--base", req.Base)
	}

	out, err := runGH(ctx, r.workspaceRoot, args...)
	if err != nil {
		return nil, err
	}

	var raw []struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		State     string `json:"state"`
		Author    struct{ Login string `json:"login"` } `json:"author"`
		URL       string `json:"url"`
		Base      string `json:"baseRefName"`
		Head      string `json:"headRefName"`
		UpdatedAt string `json:"updatedAt"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("gh pr list: parse JSON: %w", err)
	}

	items := make([]GHPRListItem, len(raw))
	for i, p := range raw {
		items[i] = GHPRListItem{
			Number:    p.Number,
			Title:     p.Title,
			State:     p.State,
			Author:    p.Author.Login,
			URL:       p.URL,
			Base:      p.Base,
			Head:      p.Head,
			UpdatedAt: p.UpdatedAt,
		}
	}
	return &GHPRListResponse{PRs: items}, nil
}

// ─── gh.pr.create ────────────────────────────────────────────────────────────

// GHPRCreateRequest is the input for the gh.pr.create tool.
type GHPRCreateRequest struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Base  string `json:"base,omitempty"` // target branch; defaults to repo default
	Draft bool   `json:"draft,omitempty"`
}

// GHPRCreateResponse is the output of the gh.pr.create tool.
type GHPRCreateResponse struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Title  string `json:"title"`
}

// GHPRCreate creates a pull request from the current branch.
func (r *Runner) GHPRCreate(ctx context.Context, req GHPRCreateRequest) (*GHPRCreateResponse, error) {
	if strings.TrimSpace(req.Title) == "" {
		return nil, fmt.Errorf("gh pr create: title is required")
	}

	args := []string{
		"pr", "create",
		"--title", req.Title,
		"--body", req.Body,
		"--json", "number,url,title",
	}
	if req.Base != "" {
		args = append(args, "--base", req.Base)
	}
	if req.Draft {
		args = append(args, "--draft")
	}

	out, err := runGH(ctx, r.workspaceRoot, args...)
	if err != nil {
		return nil, err
	}

	var resp GHPRCreateResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("gh pr create: parse JSON: %w", err)
	}
	return &resp, nil
}

// ─── gh.pr.view ──────────────────────────────────────────────────────────────

// GHPRViewRequest is the input for the gh.pr.view tool.
type GHPRViewRequest struct {
	Number int `json:"number,omitempty"` // 0 = current branch's PR
}

// GHPRComment is a single comment on a PR.
type GHPRComment struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	URL    string `json:"url"`
}

// GHPRViewResponse is the output of the gh.pr.view tool.
type GHPRViewResponse struct {
	Number   int           `json:"number"`
	Title    string        `json:"title"`
	Body     string        `json:"body"`
	State    string        `json:"state"`
	URL      string        `json:"url"`
	Author   string        `json:"author"`
	Base     string        `json:"base"`
	Head     string        `json:"head"`
	Comments []GHPRComment `json:"comments"`
}

// GHPRView returns details of a pull request. Number=0 uses the current branch's PR.
func (r *Runner) GHPRView(ctx context.Context, req GHPRViewRequest) (*GHPRViewResponse, error) {
	args := []string{
		"pr", "view",
		"--json", "number,title,body,state,url,author,baseRefName,headRefName,comments",
	}
	if req.Number > 0 {
		args = append(args, fmt.Sprintf("%d", req.Number))
	}

	out, err := runGH(ctx, r.workspaceRoot, args...)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Number   int    `json:"number"`
		Title    string `json:"title"`
		Body     string `json:"body"`
		State    string `json:"state"`
		URL      string `json:"url"`
		Author   struct{ Login string `json:"login"` } `json:"author"`
		Base     string `json:"baseRefName"`
		Head     string `json:"headRefName"`
		Comments []struct {
			Author struct{ Login string `json:"login"` } `json:"author"`
			Body   string                               `json:"body"`
			URL    string                               `json:"url"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("gh pr view: parse JSON: %w", err)
	}

	comments := make([]GHPRComment, len(raw.Comments))
	for i, c := range raw.Comments {
		comments[i] = GHPRComment{Author: c.Author.Login, Body: c.Body, URL: c.URL}
	}
	return &GHPRViewResponse{
		Number:   raw.Number,
		Title:    raw.Title,
		Body:     raw.Body,
		State:    raw.State,
		URL:      raw.URL,
		Author:   raw.Author.Login,
		Base:     raw.Base,
		Head:     raw.Head,
		Comments: comments,
	}, nil
}

// ─── gh.issue.list ───────────────────────────────────────────────────────────

// GHIssueListRequest is the input for the gh.issue.list tool.
type GHIssueListRequest struct {
	State  string   `json:"state,omitempty"`  // "open" (default), "closed", "all"
	Labels []string `json:"labels,omitempty"` // filter by label names
	Limit  int      `json:"limit,omitempty"`  // default 20
}

// GHIssueListItem is one entry in the issue list.
type GHIssueListItem struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	State     string   `json:"state"`
	Author    string   `json:"author"`
	URL       string   `json:"url"`
	Labels    []string `json:"labels"`
	UpdatedAt string   `json:"updated_at"`
}

// GHIssueListResponse is the output of the gh.issue.list tool.
type GHIssueListResponse struct {
	Issues []GHIssueListItem `json:"issues"`
}

// GHIssueList lists issues in the current repo.
func (r *Runner) GHIssueList(ctx context.Context, req GHIssueListRequest) (*GHIssueListResponse, error) {
	args := []string{
		"issue", "list",
		"--json", "number,title,state,author,url,labels,updatedAt",
	}
	state := req.State
	if state == "" {
		state = "open"
	}
	args = append(args, "--state", state)

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	args = append(args, "--limit", fmt.Sprintf("%d", limit))

	for _, label := range req.Labels {
		args = append(args, "--label", label)
	}

	out, err := runGH(ctx, r.workspaceRoot, args...)
	if err != nil {
		return nil, err
	}

	var raw []struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		State     string `json:"state"`
		Author    struct{ Login string `json:"login"` } `json:"author"`
		URL       string `json:"url"`
		Labels    []struct{ Name string `json:"name"` } `json:"labels"`
		UpdatedAt string `json:"updatedAt"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("gh issue list: parse JSON: %w", err)
	}

	items := make([]GHIssueListItem, len(raw))
	for i, issue := range raw {
		labels := make([]string, len(issue.Labels))
		for j, l := range issue.Labels {
			labels[j] = l.Name
		}
		items[i] = GHIssueListItem{
			Number:    issue.Number,
			Title:     issue.Title,
			State:     issue.State,
			Author:    issue.Author.Login,
			URL:       issue.URL,
			Labels:    labels,
			UpdatedAt: issue.UpdatedAt,
		}
	}
	return &GHIssueListResponse{Issues: items}, nil
}

// ─── gh.issue.view ───────────────────────────────────────────────────────────

// GHIssueViewRequest is the input for the gh.issue.view tool.
type GHIssueViewRequest struct {
	Number int `json:"number"` // required
}

// GHIssueComment is a single comment on an issue.
type GHIssueComment struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	URL    string `json:"url"`
}

// GHIssueViewResponse is the output of the gh.issue.view tool.
type GHIssueViewResponse struct {
	Number   int              `json:"number"`
	Title    string           `json:"title"`
	Body     string           `json:"body"`
	State    string           `json:"state"`
	URL      string           `json:"url"`
	Author   string           `json:"author"`
	Labels   []string         `json:"labels"`
	Comments []GHIssueComment `json:"comments"`
}

// GHIssueView returns details of a specific issue.
func (r *Runner) GHIssueView(ctx context.Context, req GHIssueViewRequest) (*GHIssueViewResponse, error) {
	if req.Number <= 0 {
		return nil, fmt.Errorf("gh issue view: number is required and must be > 0")
	}

	args := []string{
		"issue", "view", fmt.Sprintf("%d", req.Number),
		"--json", "number,title,body,state,url,author,labels,comments",
	}

	out, err := runGH(ctx, r.workspaceRoot, args...)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Number   int    `json:"number"`
		Title    string `json:"title"`
		Body     string `json:"body"`
		State    string `json:"state"`
		URL      string `json:"url"`
		Author   struct{ Login string `json:"login"` } `json:"author"`
		Labels   []struct{ Name string `json:"name"` } `json:"labels"`
		Comments []struct {
			Author struct{ Login string `json:"login"` } `json:"author"`
			Body   string                               `json:"body"`
			URL    string                               `json:"url"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("gh issue view: parse JSON: %w", err)
	}

	labels := make([]string, len(raw.Labels))
	for i, l := range raw.Labels {
		labels[i] = l.Name
	}
	comments := make([]GHIssueComment, len(raw.Comments))
	for i, c := range raw.Comments {
		comments[i] = GHIssueComment{Author: c.Author.Login, Body: c.Body, URL: c.URL}
	}
	return &GHIssueViewResponse{
		Number:   raw.Number,
		Title:    raw.Title,
		Body:     raw.Body,
		State:    raw.State,
		URL:      raw.URL,
		Author:   raw.Author.Login,
		Labels:   labels,
		Comments: comments,
	}, nil
}
```

- [ ] **Step 4: Запустить тесты**

```powershell
go test ./internal/tools/... -run "TestGH" -v 2>&1
```

Ожидается:
- `TestGHPRCreate_EmptyTitle` — PASS
- `TestGHPRCreate_WhitespaceTitle` — PASS
- `TestGHAvailable_DoesNotPanic` — PASS
- `TestGHPRList_SkipNoGH` / `TestGHIssueList_SkipNoGH` — PASS или SKIP

- [ ] **Step 5: Build + vet**

```powershell
go build ./...
go vet ./...
```

Ожидается: чистая сборка.

- [ ] **Step 6: Commit**

```powershell
git add internal/tools/github.go internal/tools/github_test.go
git commit -m "feat(tools): add gh.pr.list/create/view and gh.issue.list/view tools"
```

---

### Task 2: Wire — registry, call.go, version, config

**Files:**
- Modify: `internal/tools/registry.go`
- Modify: `internal/tools/call.go`
- Modify: `internal/protocol/version.go`
- Modify: `internal/config/config.go`

- [ ] **Step 1: Добавить tool definitions в `registry.go`**

В функцию `ListTools` (и `listToolsBuild`, `listToolsGeneral`), в блок `if allowExec { ... }`, после `toolGitPush()` добавить:

```go
out = append(out,
    toolGHPRList(), toolGHPRCreate(), toolGHPRView(),
    toolGHIssueList(), toolGHIssueView(),
)
```

Нужно добавить в **три** места: `ListTools`, `listToolsBuild`, `listToolsGeneral`.

Добавить функции-дескрипторы в конец `registry.go`:

```go
func toolGHPRList() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "gh.pr.list",
			Description: "List pull requests in the current GitHub repository. Requires gh CLI installed and authenticated.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "state":  { "type": "string", "enum": ["open","closed","merged","all"], "description": "Filter by PR state. Default: open." },
    "limit":  { "type": "integer", "minimum": 1, "maximum": 100, "description": "Max PRs to return. Default: 20." },
    "base":   { "type": "string", "description": "Filter by base branch name." }
  }
}`),
		},
	}
}

func toolGHPRCreate() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "gh.pr.create",
			Description: "Create a pull request from the current branch. Requires gh CLI installed and authenticated.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["title"],
  "properties": {
    "title": { "type": "string", "minLength": 1, "description": "PR title." },
    "body":  { "type": "string", "description": "PR description (markdown)." },
    "base":  { "type": "string", "description": "Base branch. Defaults to repo default branch." },
    "draft": { "type": "boolean", "description": "Create as draft PR." }
  }
}`),
		},
	}
}

func toolGHPRView() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "gh.pr.view",
			Description: "View details of a pull request including description and comments. number=0 uses the current branch's PR.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "number": { "type": "integer", "minimum": 0, "description": "PR number. Omit or 0 = current branch's PR." }
  }
}`),
		},
	}
}

func toolGHIssueList() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "gh.issue.list",
			Description: "List issues in the current GitHub repository. Requires gh CLI installed and authenticated.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "state":  { "type": "string", "enum": ["open","closed","all"], "description": "Filter by state. Default: open." },
    "labels": { "type": "array", "items": { "type": "string" }, "description": "Filter by label names." },
    "limit":  { "type": "integer", "minimum": 1, "maximum": 100, "description": "Max issues to return. Default: 20." }
  }
}`),
		},
	}
}

func toolGHIssueView() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "gh.issue.view",
			Description: "View details of a GitHub issue including description and comments.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["number"],
  "properties": {
    "number": { "type": "integer", "minimum": 1, "description": "Issue number." }
  }
}`),
		},
	}
}
```

Добавить в `applyParallelFlags` в switch:
- В case параллельных инструментов (Pure reads):
  ```go
  "gh.pr.list", "gh.pr.view", "gh.issue.list", "gh.issue.view",
  ```
- В case мутирующих инструментов:
  ```go
  "gh.pr.create",
  ```

Также добавить в `allToolDefsMap()`:
```go
toolGHPRList(), toolGHPRCreate(), toolGHPRView(), toolGHIssueList(), toolGHIssueView(),
```

- [ ] **Step 2: Добавить case handlers в `call.go`**

Добавить перед `default:` в switch:

```go
	case "gh.pr.list":
		var req GHPRListRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GHPRList(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "gh.pr.create":
		var req GHPRCreateRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GHPRCreate(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "gh.pr.view":
		var req GHPRViewRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GHPRView(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "gh.issue.list":
		var req GHIssueListRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GHIssueList(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "gh.issue.view":
		var req GHIssueViewRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GHIssueView(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)
```

- [ ] **Step 3: Bump ToolsVersion в `internal/protocol/version.go`**

Изменить:
```go
// v9: added search.websearch tool.
ToolsVersion = 9
```
На:
```go
// v9: added search.websearch tool.
// v10: added gh.pr.list, gh.pr.create, gh.pr.view, gh.issue.list, gh.issue.view (allowExec-gated).
ToolsVersion = 10
```

- [ ] **Step 4: Добавить имена в `validAgentToolNames` в `internal/config/config.go`**

Найти строку:
```go
"git.commit": true, "git.branch": true, "git.checkout": true, "git.push": true,
```
Добавить после неё:
```go
"gh.pr.list": true, "gh.pr.create": true, "gh.pr.view": true,
"gh.issue.list": true, "gh.issue.view": true,
```

- [ ] **Step 5: Build + vet**

```powershell
go build ./...
go vet ./...
```

Ожидается: чистая сборка, нет ошибок.

- [ ] **Step 6: Запустить весь test suite**

```powershell
go test ./... -count=1 2>&1 | Select-String "FAIL|ok"
```

Ожидается: все пакеты `ok`, нет `FAIL`.

- [ ] **Step 7: Убедиться что registry_test знает про новые инструменты**

```powershell
go test ./internal/tools/... -run TestRegistry -v 2>&1
```

Если тест падает из-за того что новые инструменты не в ожидаемом списке — обновить тест в `registry_test.go` добавив туда `"gh.pr.list"`, `"gh.pr.create"`, `"gh.pr.view"`, `"gh.issue.list"`, `"gh.issue.view"`.

- [ ] **Step 8: Commit**

```powershell
git add internal/tools/registry.go internal/tools/call.go internal/protocol/version.go internal/config/config.go
git commit -m "feat(tools): wire gh.pr/gh.issue tools into registry, call.go, version v10, config"
```

---

## Self-Review Checklist

**Spec coverage:**
- `gh.pr.list` — ✅ Task 1 (impl) + Task 2 (wire)
- `gh.pr.create` — ✅ title required validation + TDD test, Task 2 wire
- `gh.pr.view` — ✅ number=0 uses current branch's PR
- `gh.issue.list` — ✅ state/labels/limit params
- `gh.issue.view` — ✅ number > 0 validation
- `ghAvailable()` — ✅ sync.Once, кешируется
- `runGH()` — ✅ timeout 30s, stderr captured, output truncated 512KB
- `allowExec` gate — ✅ все 5 в блоке allowExec в registry
- ToolsVersion 9→10 — ✅ Task 2 Step 3
- `validAgentToolNames` — ✅ Task 2 Step 4
- `allToolDefsMap()` — ✅ Task 2 Step 1
- Parallel flags: list/view/view/list/view = parallel safe; create = mutating ✅

**Placeholder scan:** Нет TBD/TODO.

**Type consistency:**
- `GHPRListItem`, `GHPRListResponse` — определены в Task 1, используются в Task 2
- `GHPRCreateRequest/Response` — аналогично
- Все case в call.go используют типы из github.go
- `toolGHPRList()` etc. возвращают `llm.ToolDef` как все остальные descriptor-функции
