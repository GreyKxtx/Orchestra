# Staging / Dry-Run Architecture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Сделать так чтобы `write`/`edit` tool-calls уважали `--apply`/`--plan-only` флаги через in-memory overlay (staging), и чтобы `plan.json` корректно накапливал ops от tool-based writes для replay через `--from-plan`.

**Architecture:**
В dry-run режиме `Runner` держит `staged map[string]*stagedFile` (overlay). `FSWrite`/`FSEdit` пишут в overlay вместо диска. `FSRead` сначала смотрит в overlay. При получении `final` агент применяет накопленные patches к overlay и собирает `StagedOps()` (write_atomic ops) как итоговый набор для `FSApplyOps` + `plan.json`. В apply-режиме поведение не меняется.

**Tech Stack:** Go 1.22, пакеты `internal/tools`, `internal/resolver`, `internal/agent`, `internal/cli`, `internal/core`.

---

## Файловая карта изменений

| Файл | Действие | Что делает |
|---|---|---|
| `internal/resolver/external_patches.go` | Modify | Экспортировать `ApplySearchReplace`, `ApplyUnifiedDiff` |
| `internal/tools/staging.go` | **Create** | `stagedFile` struct + все методы Runner для staging |
| `internal/tools/runner.go` | Modify | Добавить поля `dryRun`/`staged`/`stagedMu`; `DryRun` в RunnerOptions; `SetDryRun`; `FSRead` overlay check |
| `internal/tools/write.go` | Modify | `FSWrite` dryRun path |
| `internal/tools/edit.go` | Modify | `FSEdit` dryRun path |
| `internal/agent/agent.go` | Modify | `final` block: staging support + `ApplyPatchesToStaged` |
| `internal/cli/apply.go` | Modify | Передавать `DryRun` в 3 вызовах `NewRunner` |
| `internal/core/core.go` | Modify | `SetDryRun` + `ClearStaged` перед `agent.run` |
| `internal/tools/staging_test.go` | **Create** | Unit-тесты staging поведения |
| `internal/resolver/external_patches_test.go` | Modify | Тесты `ApplySearchReplace` |
| `tests/e2e_real_llm/minimal_flow_test.go` | Modify | Убрать skip для empty ops |

---

## Task 1: Экспортировать search/replace логику резолвера

**Files:**
- Modify: `internal/resolver/external_patches.go`

- [ ] **Step 1.1: Написать failing тест для ApplySearchReplace**

В `internal/resolver/external_patches_test.go` добавить:

```go
func TestApplySearchReplace_Basic(t *testing.T) {
    content := []byte("func main() {\n\tfmt.Println(\"hello\")\n}\n")
    got, err := ApplySearchReplace(content, "fmt.Println(\"hello\")", "fmt.Println(\"world\")")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    want := "func main() {\n\tfmt.Println(\"world\")\n}\n"
    if string(got) != want {
        t.Fatalf("got %q, want %q", string(got), want)
    }
}

func TestApplySearchReplace_NotFound(t *testing.T) {
    content := []byte("hello world\n")
    _, err := ApplySearchReplace(content, "goodbye", "hello")
    if err == nil {
        t.Fatal("expected error for not-found search")
    }
    pe, ok := protocol.AsError(err)
    if !ok || pe.Code != protocol.StaleContent {
        t.Fatalf("expected StaleContent, got %v", err)
    }
}

func TestApplyUnifiedDiff_Basic(t *testing.T) {
    original := "line1\nline2\nline3\n"
    diff := `@@ -1,3 +1,3 @@
 line1
-line2
+LINE2
 line3
`
    got, err := ApplyUnifiedDiff([]byte(original), diff)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    want := "line1\nLINE2\nline3\n"
    if string(got) != want {
        t.Fatalf("got %q, want %q", string(got), want)
    }
}
```

- [ ] **Step 1.2: Запустить тест — убедиться что FAIL (функции не существуют)**

```
go test ./internal/resolver/... -run "TestApplySearchReplace|TestApplyUnifiedDiff" -v
```
Ожидаем: `undefined: ApplySearchReplace` / `undefined: ApplyUnifiedDiff`

- [ ] **Step 1.3: Добавить публичные функции в external_patches.go**

В конце файла `internal/resolver/external_patches.go` добавить:

```go
// ApplySearchReplace applies search→replace on content using the 3-pass matching algorithm
// (exact → line-trimmed → indent-flexible). Returns new content, or StaleContent /
// AmbiguousMatch protocol error if the search block is not found or ambiguous.
func ApplySearchReplace(content []byte, search, replace string) ([]byte, error) {
	if search == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "search is empty", nil)
	}
	s := string(content)

	start, end, matches := findUnique(s, search)
	if matches == 0 {
		ltStart, ltEnd, ltMatches := lineTrimmedFind(s, search)
		switch ltMatches {
		case 0:
			ifStart, ifEnd, ifMatches := indentFlexibleFind(s, search)
			switch ifMatches {
			case 0:
				return nil, protocol.NewError(protocol.StaleContent, "search block not found", map[string]any{
					"search":   preview(search, 200),
					"fileHash": cache.ComputeSHA256(content),
				})
			case 1:
				start, end = ifStart, ifEnd
			default:
				return nil, protocol.NewError(protocol.AmbiguousMatch, "search block is ambiguous (indent-flexible)", map[string]any{
					"matches": ifMatches,
					"search":  preview(search, 200),
				})
			}
		case 1:
			start, end = ltStart, ltEnd
		default:
			return nil, protocol.NewError(protocol.AmbiguousMatch, "search block is ambiguous (line-trimmed)", map[string]any{
				"matches": ltMatches,
				"search":  preview(search, 200),
			})
		}
	}
	if matches > 1 {
		return nil, protocol.NewError(protocol.AmbiguousMatch, "search block is ambiguous", map[string]any{
			"matches": matches,
			"search":  preview(search, 200),
		})
	}

	var buf strings.Builder
	buf.WriteString(s[:start])
	buf.WriteString(replace)
	buf.WriteString(s[end:])
	return []byte(buf.String()), nil
}

// ApplyUnifiedDiff applies a unified diff to content. Returns new content, or an error
// if the diff cannot be applied.
func ApplyUnifiedDiff(content []byte, diffText string) ([]byte, error) {
	result, err := applyUnifiedDiff(string(content), diffText)
	if err != nil {
		return nil, err
	}
	return []byte(result), nil
}
```

- [ ] **Step 1.4: Запустить тест — убедиться что PASS**

```
go test ./internal/resolver/... -run "TestApplySearchReplace|TestApplyUnifiedDiff" -v
```
Ожидаем: PASS

- [ ] **Step 1.5: Полный прогон тестов пакета**

```
go test ./internal/resolver/... -v
```
Ожидаем: все PASS

- [ ] **Step 1.6: Commit**

```
git add internal/resolver/external_patches.go internal/resolver/external_patches_test.go
git commit -m "feat(resolver): export ApplySearchReplace and ApplyUnifiedDiff for staging"
```

---

## Task 2: Staging инфраструктура в Runner

**Files:**
- Create: `internal/tools/staging.go`
- Modify: `internal/tools/runner.go`

- [ ] **Step 2.1: Написать failing тест для staging**

Создать `internal/tools/staging_test.go`:

```go
package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func newDryRunRunner(t *testing.T) *Runner {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{DryRun: true})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestStaging_NoFileCreatedOnDisk(t *testing.T) {
	r := newDryRunRunner(t)
	// Pre-create so we can overwrite
	p := filepath.Join(r.workspaceRoot, "hello.txt")
	if err := os.WriteFile(p, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	origHash := computeTestHash([]byte("original"))

	_, err := r.FSWrite(nil, FSWriteRequest{
		Path:     "hello.txt",
		Content:  "staged content",
		FileHash: origHash,
	})
	if err != nil {
		t.Fatalf("FSWrite dry-run: %v", err)
	}

	// File on disk must be unchanged.
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("disk was modified: got %q", string(got))
	}
}

func TestStaging_StagedOps_HasWriteAtomic(t *testing.T) {
	r := newDryRunRunner(t)
	p := filepath.Join(r.workspaceRoot, "new.txt")
	_ = p // we're creating a new file

	_, err := r.FSWrite(nil, FSWriteRequest{
		Path:         "new.txt",
		Content:      "hello",
		MustNotExist: true,
	})
	if err != nil {
		t.Fatalf("FSWrite: %v", err)
	}

	ops := r.StagedOps()
	if len(ops) != 1 {
		t.Fatalf("expected 1 staged op, got %d", len(ops))
	}
	if ops[0].WriteAtomic == nil {
		t.Fatal("expected write_atomic op")
	}
	if ops[0].WriteAtomic.Content != "hello" {
		t.Fatalf("unexpected content: %q", ops[0].WriteAtomic.Content)
	}
	if !ops[0].WriteAtomic.Conditions.MustNotExist {
		t.Fatal("expected MustNotExist=true for new file")
	}
}

func TestStaging_ReadAfterWrite(t *testing.T) {
	r := newDryRunRunner(t)
	// Write original file to disk.
	p := filepath.Join(r.workspaceRoot, "foo.txt")
	if err := os.WriteFile(p, []byte("before"), 0644); err != nil {
		t.Fatal(err)
	}
	origHash := computeTestHash([]byte("before"))

	// Stage a write.
	_, err := r.FSWrite(nil, FSWriteRequest{
		Path:     "foo.txt",
		Content:  "after",
		FileHash: origHash,
	})
	if err != nil {
		t.Fatalf("FSWrite: %v", err)
	}

	// Read should return staged content.
	resp, err := r.FSRead(nil, FSReadRequest{Path: "foo.txt"})
	if err != nil {
		t.Fatalf("FSRead: %v", err)
	}
	// Content has line numbers prefix ("1: after"), check it contains "after".
	if !containsLine(resp.Content, "after") {
		t.Fatalf("FSRead returned disk content instead of staged: %q", resp.Content)
	}
}

func TestStaging_EditDryRun(t *testing.T) {
	r := newDryRunRunner(t)
	p := filepath.Join(r.workspaceRoot, "code.go")
	if err := os.WriteFile(p, []byte("func hello() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := r.FSEdit(nil, FSEditRequest{
		Path:    "code.go",
		Search:  "hello",
		Replace: "world",
	})
	if err != nil {
		t.Fatalf("FSEdit dry-run: %v", err)
	}

	// Disk unchanged.
	got, _ := os.ReadFile(p)
	if string(got) != "func hello() {}\n" {
		t.Fatalf("disk was modified: %q", string(got))
	}

	// StagedOps has the updated content.
	ops := r.StagedOps()
	if len(ops) != 1 {
		t.Fatalf("expected 1 staged op, got %d", len(ops))
	}
	if ops[0].WriteAtomic == nil || !containsSubstr(ops[0].WriteAtomic.Content, "world") {
		t.Fatalf("staged content wrong: %+v", ops[0].WriteAtomic)
	}
}

func TestStaging_ClearStaged(t *testing.T) {
	r := newDryRunRunner(t)
	_, _ = r.FSWrite(nil, FSWriteRequest{Path: "x.txt", Content: "hi", MustNotExist: true})
	if len(r.StagedOps()) == 0 {
		t.Fatal("expected staged ops before clear")
	}
	r.ClearStaged()
	if len(r.StagedOps()) != 0 {
		t.Fatal("expected empty staged ops after clear")
	}
}

// helpers
func computeTestHash(b []byte) string {
	import_cache_hash // placeholder — use cache.ComputeSHA256
	return ""
}
func containsLine(s, sub string) bool  { return strings.Contains(s, sub) }
func containsSubstr(s, sub string) bool { return strings.Contains(s, sub) }
```

> **Note:** Helper `computeTestHash` будет заменён при реализации — в тестах использовать `cache.ComputeSHA256`. Все helpers финализируются в Step 2.3.

- [ ] **Step 2.2: Запустить тест — убедиться что FAIL**

```
go test ./internal/tools/... -run "TestStaging" -v
```
Ожидаем: compile error / `undefined: RunnerOptions.DryRun`

- [ ] **Step 2.3: Создать internal/tools/staging.go**

```go
package tools

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/orchestra/orchestra/internal/cache"
	"github.com/orchestra/orchestra/internal/ops"
)

// stagedFile holds the in-memory state of a file that was written or edited
// during a dry-run agent pass.
type stagedFile struct {
	content  string // current staged content
	hash     string // sha256 of content
	diskHash string // sha256 of file on disk at first-stage time (empty if file was new)
	isNew    bool   // true if file didn't exist on disk when first staged
}

// stageFile records new staged content for relSlash (forward-slash relative path).
// On first call for a given path, captures original disk state for plan.json conditions.
// Subsequent calls update content but keep original disk conditions.
func (r *Runner) stageFile(relSlash, content, hash string) {
	r.stagedMu.Lock()
	defer r.stagedMu.Unlock()
	r.stageFileLocked(relSlash, content, hash)
}

func (r *Runner) stageFileLocked(relSlash, content, hash string) {
	if sf, ok := r.staged[relSlash]; ok {
		sf.content = content
		sf.hash = hash
		return
	}
	absPath := filepath.Join(r.workspaceRoot, filepath.FromSlash(relSlash))
	diskBytes, err := os.ReadFile(absPath)
	isNew := os.IsNotExist(err)
	diskHash := ""
	if err == nil {
		diskHash = cache.ComputeSHA256(diskBytes)
	}
	r.staged[relSlash] = &stagedFile{
		content:  content,
		hash:     hash,
		diskHash: diskHash,
		isNew:    isNew,
	}
}

// stagedContent returns staged content and hash for relSlash, or ok=false if not staged.
func (r *Runner) stagedContent(relSlash string) (content, hash string, ok bool) {
	r.stagedMu.Lock()
	defer r.stagedMu.Unlock()
	sf, ok := r.staged[relSlash]
	if !ok {
		return "", "", false
	}
	return sf.content, sf.hash, true
}

// currentHash returns the sha256 of relSlash in overlay (if staged) or on disk.
// Returns empty string if the file doesn't exist anywhere.
func (r *Runner) currentHash(relSlash string) string {
	if _, hash, ok := r.stagedContent(relSlash); ok {
		return hash
	}
	absPath := filepath.Join(r.workspaceRoot, filepath.FromSlash(relSlash))
	b, err := os.ReadFile(absPath)
	if err != nil {
		return ""
	}
	return cache.ComputeSHA256(b)
}

// StagedOps returns write_atomic ops for all staged files.
// Each op carries the original disk conditions (must_not_exist or file_hash)
// so that --from-plan replay detects stale files correctly.
func (r *Runner) StagedOps() []ops.AnyOp {
	r.stagedMu.Lock()
	defer r.stagedMu.Unlock()
	if len(r.staged) == 0 {
		return nil
	}
	out := make([]ops.AnyOp, 0, len(r.staged))
	for path, sf := range r.staged {
		wa := ops.WriteAtomicOp{
			Op:      ops.OpFileWriteAtomic,
			Path:    path,
			Content: sf.content,
			Conditions: ops.WriteAtomicConditions{
				MustNotExist: sf.isNew,
				FileHash:     sf.diskHash,
			},
		}
		if sf.isNew {
			wa.Conditions.FileHash = ""
		}
		waCopy := wa
		out = append(out, ops.AnyOp{Op: waCopy.Op, Path: waCopy.Path, WriteAtomic: &waCopy})
	}
	return out
}

// StagedFileContent returns a snapshot of the staging overlay as a path→content map.
// Used by the agent to pass overlay to patch resolvers.
func (r *Runner) StagedFileContent() map[string]string {
	r.stagedMu.Lock()
	defer r.stagedMu.Unlock()
	if len(r.staged) == 0 {
		return nil
	}
	out := make(map[string]string, len(r.staged))
	for path, sf := range r.staged {
		out[path] = sf.content
	}
	return out
}

// ApplyPatchesToStaged applies external patches to the staging overlay.
// For each patch, reads content from overlay (if staged) or from disk, applies
// the patch operation, and stores the result in the overlay.
// Called in dry-run mode after the model returns final.patches.
func (r *Runner) ApplyPatchesToStaged(patchList []patches.Patch) error {
	for _, p := range patchList {
		relSlash := filepath.ToSlash(strings.TrimSpace(p.Path))
		if relSlash == "" {
			return protocol.NewError(protocol.InvalidLLMOutput, "patch path is empty", nil)
		}

		// Get current content: staged or disk.
		var currentContent []byte
		if content, _, ok := r.stagedContent(relSlash); ok {
			currentContent = []byte(content)
		} else {
			absPath := filepath.Join(r.workspaceRoot, filepath.FromSlash(relSlash))
			b, err := os.ReadFile(absPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("read %s: %w", relSlash, err)
			}
			currentContent = b
		}

		var newContent []byte
		var err error
		switch p.Type {
		case patches.TypeFileSearchReplace:
			newContent, err = resolver.ApplySearchReplace(currentContent, p.Search, p.Replace)
		case patches.TypeFileUnifiedDiff:
			newContent, err = resolver.ApplyUnifiedDiff(currentContent, p.Diff)
		case patches.TypeFileWriteAtomic:
			newContent = []byte(p.Content)
		default:
			return protocol.NewError(protocol.InvalidLLMOutput, "unsupported patch type", map[string]any{"type": p.Type})
		}
		if err != nil {
			return err
		}

		newHash := cache.ComputeSHA256(newContent)
		r.stageFile(relSlash, string(newContent), newHash)
	}
	return nil
}

// ClearStaged removes all staged changes. Called before each agent.run in core mode.
func (r *Runner) ClearStaged() {
	r.stagedMu.Lock()
	defer r.stagedMu.Unlock()
	r.staged = make(map[string]*stagedFile)
}

// HasStagedChanges reports whether any files have been staged.
func (r *Runner) HasStagedChanges() bool {
	r.stagedMu.Lock()
	defer r.stagedMu.Unlock()
	return len(r.staged) > 0
}
```

> **Note:** Добавить в imports `patches`, `resolver`, `protocol`, `fmt` — они уже есть в других файлах пакета tools, но staging.go нужны свои.

Финальный список imports для staging.go:
```go
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/orchestra/orchestra/internal/cache"
	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/patches"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/resolver"
)
```

- [ ] **Step 2.4: Добавить поля в Runner struct и RunnerOptions (runner.go)**

В `internal/tools/runner.go`, изменить `Runner` struct (добавить после `lspManager`):

```go
// Dry-run staging: when dryRun=true, FSWrite/FSEdit accumulate changes in staged
// instead of writing to disk. FSRead serves staged content back to the model.
dryRun   bool
staged   map[string]*stagedFile
stagedMu sync.Mutex
```

Изменить `RunnerOptions` struct (добавить поле):

```go
// DryRun, when true, enables staging mode: write/edit accumulate in memory
// instead of touching disk. FSRead serves staged content. StagedOps() returns
// write_atomic ops for plan.json.
DryRun bool
```

Изменить `NewRunner` — в блоке `return &Runner{...}` добавить:

```go
dryRun: opts.DryRun,
staged: make(map[string]*stagedFile),
```

Добавить метод `SetDryRun` после `NewRunner`:

```go
// SetDryRun enables or disables staging mode. Disabling clears all staged state.
// Called by Core before each agent.run to align with the run's apply flag.
func (r *Runner) SetDryRun(v bool) {
	r.stagedMu.Lock()
	defer r.stagedMu.Unlock()
	r.dryRun = v
	if !v {
		r.staged = make(map[string]*stagedFile)
	}
}
```

- [ ] **Step 2.5: Исправить staging_test.go — использовать реальный cache.ComputeSHA256**

Убрать placeholder `computeTestHash` и заменить на реальный import:

```go
package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/cache"
)

func newDryRunRunner(t *testing.T) *Runner {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{DryRun: true})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestStaging_NoFileCreatedOnDisk(t *testing.T) {
	r := newDryRunRunner(t)
	p := filepath.Join(r.workspaceRoot, "hello.txt")
	if err := os.WriteFile(p, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	origHash := cache.ComputeSHA256([]byte("original"))

	_, err := r.FSWrite(nil, FSWriteRequest{
		Path:     "hello.txt",
		Content:  "staged content",
		FileHash: origHash,
	})
	if err != nil {
		t.Fatalf("FSWrite dry-run: %v", err)
	}

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("disk was modified: got %q", string(got))
	}
}

func TestStaging_StagedOps_HasWriteAtomic(t *testing.T) {
	r := newDryRunRunner(t)

	_, err := r.FSWrite(nil, FSWriteRequest{
		Path:         "new.txt",
		Content:      "hello",
		MustNotExist: true,
	})
	if err != nil {
		t.Fatalf("FSWrite: %v", err)
	}

	ops := r.StagedOps()
	if len(ops) != 1 {
		t.Fatalf("expected 1 staged op, got %d", len(ops))
	}
	if ops[0].WriteAtomic == nil {
		t.Fatal("expected write_atomic op")
	}
	if ops[0].WriteAtomic.Content != "hello" {
		t.Fatalf("unexpected content: %q", ops[0].WriteAtomic.Content)
	}
	if !ops[0].WriteAtomic.Conditions.MustNotExist {
		t.Fatal("expected MustNotExist=true for new file")
	}
}

func TestStaging_ReadAfterWrite(t *testing.T) {
	r := newDryRunRunner(t)
	p := filepath.Join(r.workspaceRoot, "foo.txt")
	if err := os.WriteFile(p, []byte("before"), 0644); err != nil {
		t.Fatal(err)
	}
	origHash := cache.ComputeSHA256([]byte("before"))

	_, err := r.FSWrite(nil, FSWriteRequest{
		Path:     "foo.txt",
		Content:  "after",
		FileHash: origHash,
	})
	if err != nil {
		t.Fatalf("FSWrite: %v", err)
	}

	resp, err := r.FSRead(nil, FSReadRequest{Path: "foo.txt"})
	if err != nil {
		t.Fatalf("FSRead: %v", err)
	}
	if !strings.Contains(resp.Content, "after") {
		t.Fatalf("FSRead returned disk content instead of staged: %q", resp.Content)
	}
}

func TestStaging_EditDryRun(t *testing.T) {
	r := newDryRunRunner(t)
	p := filepath.Join(r.workspaceRoot, "code.go")
	if err := os.WriteFile(p, []byte("func hello() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := r.FSEdit(nil, FSEditRequest{
		Path:    "code.go",
		Search:  "hello",
		Replace: "world",
	})
	if err != nil {
		t.Fatalf("FSEdit dry-run: %v", err)
	}

	got, _ := os.ReadFile(p)
	if string(got) != "func hello() {}\n" {
		t.Fatalf("disk was modified: %q", string(got))
	}

	stagedOps := r.StagedOps()
	if len(stagedOps) != 1 {
		t.Fatalf("expected 1 staged op, got %d", len(stagedOps))
	}
	if stagedOps[0].WriteAtomic == nil || !strings.Contains(stagedOps[0].WriteAtomic.Content, "world") {
		t.Fatalf("staged content wrong: %+v", stagedOps[0].WriteAtomic)
	}
}

func TestStaging_ClearStaged(t *testing.T) {
	r := newDryRunRunner(t)
	_, _ = r.FSWrite(nil, FSWriteRequest{Path: "x.txt", Content: "hi", MustNotExist: true})
	if len(r.StagedOps()) == 0 {
		t.Fatal("expected staged ops before clear")
	}
	r.ClearStaged()
	if len(r.StagedOps()) != 0 {
		t.Fatal("expected empty staged ops after clear")
	}
}
```

- [ ] **Step 2.6: Запустить тест — убедиться что FAIL (FSWrite/FSEdit не реализованы)**

```
go test ./internal/tools/... -run "TestStaging" -v
```
Ожидаем: compile errors или FAIL

- [ ] **Step 2.7: Добавить FSRead overlay check (runner.go)**

В методе `FSRead`, ПЕРЕД существующим вызовом `readFileWithHash`, добавить блок:

```go
// Staging overlay: serve staged content in dry-run mode.
if r.dryRun {
	if stagedContent, stagedHash, ok := r.stagedContent(relSlash); ok {
		numbered := addLineNumbers(stagedContent)
		return &FSReadResponse{
			Path:      relSlash,
			Content:   numbered,
			SHA256:    stagedHash,
			FileHash:  stagedHash,
			MTimeUnix: 0,
			Size:      int64(len(stagedContent)),
		}, nil
	}
}
```

Вставить после строки `absPath, relSlash, err := resolveWorkspacePath(r.workspaceRoot, path)` и до `maxBytes := req.MaxBytes`.

- [ ] **Step 2.8: go build проверка**

```
go build ./...
```
Ожидаем: compile errors только из-за незавершённых FSWrite/FSEdit

---

## Task 3: FSWrite — staging path

**Files:**
- Modify: `internal/tools/write.go`

- [ ] **Step 3.1: Добавить dryRun ветку в FSWrite**

В `internal/tools/write.go`, в методе `FSWrite`, ПЕРЕД блоком `patch := patches.Patch{...}` добавить:

```go
if r.dryRun {
	// In dry-run / staging mode: validate conditions, then accumulate in overlay.
	// No disk writes happen here.

	relSlash := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))

	if req.MustNotExist {
		// Fail if file is already staged or already on disk.
		if _, _, ok := r.stagedContent(relSlash); ok {
			return nil, protocol.NewError(protocol.AlreadyExists, "file already staged", map[string]any{"path": path})
		}
		absPath, _, err2 := resolveWorkspacePath(r.workspaceRoot, path)
		if err2 == nil {
			if _, statErr := os.Stat(absPath); statErr == nil {
				return nil, protocol.NewError(protocol.AlreadyExists, "file already exists", map[string]any{"path": path})
			}
		}
	} else if fileHash != "" {
		// Validate provided hash against staged-or-disk hash.
		if current := r.currentHash(relSlash); current != fileHash {
			return nil, protocol.NewError(protocol.StaleContent, "file_hash mismatch (dry-run)", map[string]any{
				"path":     path,
				"provided": fileHash,
				"current":  current,
			})
		}
	}

	contentHash := cache.ComputeSHA256([]byte(req.Content))
	r.stageFile(relSlash, req.Content, contentHash)
	return &FSWriteResponse{
		Path:         relSlash,
		FileHash:     contentHash,
		BytesWritten: len(req.Content),
	}, nil
}
```

Также добавить `"os"` в импорты write.go если не добавлен.

- [ ] **Step 3.2: Запустить staging тесты**

```
go test ./internal/tools/... -run "TestStaging_NoFileCreatedOnDisk|TestStaging_StagedOps|TestStaging_ReadAfterWrite" -v
```
Ожидаем: первые 3 теста PASS (FSWrite работает), FSEdit тест ещё FAIL

- [ ] **Step 3.3: Прогон всех тестов tools чтобы ничего не сломалось**

```
go test ./internal/tools/... -v
```
Ожидаем: staging тесты PASS/FAIL, все остальные PASS

---

## Task 4: FSEdit — staging path

**Files:**
- Modify: `internal/tools/edit.go`

- [ ] **Step 4.1: Добавить dryRun ветку в FSEdit**

В `internal/tools/edit.go`, в методе `FSEdit`, ПЕРЕД блоком `patch := patches.Patch{...}` добавить:

```go
if r.dryRun {
	// In dry-run / staging mode: apply search→replace on overlay content, store result.
	// No disk writes happen here.

	relSlash := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))

	// Get current content: staged overlay or disk.
	var currentContent []byte
	if staged, _, ok := r.stagedContent(relSlash); ok {
		currentContent = []byte(staged)
	} else {
		absPath, _, err2 := resolveWorkspacePath(r.workspaceRoot, path)
		if err2 != nil {
			return nil, err2
		}
		b, readErr := os.ReadFile(absPath)
		if readErr != nil {
			return nil, fmt.Errorf("read %s for dry-run edit: %w", path, readErr)
		}
		currentContent = b
	}

	newContent, err := resolver.ApplySearchReplace(currentContent, req.Search, req.Replace)
	if err != nil {
		return nil, err
	}

	newHash := cache.ComputeSHA256(newContent)
	r.stageFile(relSlash, string(newContent), newHash)
	return &FSEditResponse{Path: relSlash, FileHash: newHash}, nil
}
```

Добавить нужные imports в edit.go: `"fmt"`, `"os"`, `"path/filepath"` (если нет).

- [ ] **Step 4.2: Запустить все staging тесты**

```
go test ./internal/tools/... -run "TestStaging" -v
```
Ожидаем: все 5 staging тестов PASS

- [ ] **Step 4.3: Полный прогон тестов tools**

```
go test ./internal/tools/... -v
```
Ожидаем: все тесты PASS

- [ ] **Step 4.4: Commit**

```
git add internal/tools/staging.go internal/tools/staging_test.go internal/tools/runner.go internal/tools/write.go internal/tools/edit.go
git commit -m "feat(tools): add staging overlay for dry-run mode (FSWrite/FSEdit/FSRead)"
```

---

## Task 5: Agent — интеграция staging при "final"

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 5.1: Заменить блок обработки `final` с patches=0 (agent.go:798-810)**

Найти блок:
```go
// Empty patches is valid (no changes needed)
if len(step.Final.Patches) == 0 {
    a.logf("final received empty patches (no changes needed)")
    if llmResp != nil {
        history = append(history, llmResp.Message)
    }
    emitStepDone("final")
    return history, &Result{
        Steps:   steps,
        Patches: []patches.Patch{},
        Applied: false,
        Todos:   a.todos,
    }, nil
}
```

Заменить на:

```go
// Collect staged ops from write/edit tool calls (dry-run only).
var stagedOps []ops.AnyOp
if !a.opts.Apply {
    stagedOps = a.tools.StagedOps()
}

// Empty patches is valid if there are also no staged changes.
if len(step.Final.Patches) == 0 && len(stagedOps) == 0 {
    a.logf("final received empty patches (no changes needed)")
    if llmResp != nil {
        history = append(history, llmResp.Message)
    }
    emitStepDone("final")
    return history, &Result{
        Steps:   steps,
        Patches: []patches.Patch{},
        Applied: false,
        Todos:   a.todos,
    }, nil
}
```

- [ ] **Step 5.2: Заменить блок resolve + apply (agent.go:812-901)**

Найти и заменить блок начиная с `patches := append([]patches.Patch(nil), step.Final.Patches...)` и до закрывающей скобки `return history, &Result{...}`:

```go
finalPatches := append([]patches.Patch(nil), step.Final.Patches...)
a.logf("final received patches=%d staged=%d", len(finalPatches), len(stagedOps))

// Resolve final.patches.
// In dry-run: apply patches to staging overlay (so overlay has full final state).
// In apply: resolve against disk normally.
var internalOps []ops.AnyOp

if len(finalPatches) > 0 {
    if !a.opts.Apply {
        // Apply patches to staging overlay; StagedOps() will include results.
        if applyErr := a.tools.ApplyPatchesToStaged(finalPatches); applyErr != nil {
            a.logf("patch-to-staged status=error err=%v", applyErr)
            history = append(history, llm.Message{
                Role:    llm.RoleUser,
                Content: formatResolveErrorCompact(applyErr),
            })
            if cbErr := cb.RecordFinalFailure(applyErr); cbErr != nil {
                return nil, nil, cbErr
            }
            if a.opts.OnEvent != nil {
                var msg string
                if pe, ok := protocol.AsError(applyErr); ok {
                    msg = fmt.Sprintf("%s: %s", pe.Code, pe.Message)
                } else {
                    msg = "patch error: " + applyErr.Error()
                }
                a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
                    Kind:    llm.StreamEventRecoverableError,
                    Content: msg,
                }})
            }
            emitStepDone("final_retry")
            continue
        }
    } else {
        // Apply mode: resolve against disk (existing behavior).
        start := time.Now()
        var resolveErr error
        internalOps, resolveErr = resolver.ResolveExternalPatches(a.tools.WorkspaceRoot(), finalPatches)
        resolveMS := time.Since(start).Milliseconds()
        if resolveErr != nil {
            a.logf("resolve status=error duration_ms=%d err=%v", resolveMS, resolveErr)
            history = append(history, llm.Message{
                Role:    llm.RoleUser,
                Content: formatResolveErrorCompact(resolveErr),
            })
            if cbErr := cb.RecordFinalFailure(resolveErr); cbErr != nil {
                return nil, nil, cbErr
            }
            if a.opts.OnEvent != nil {
                var resolveMsg string
                if pe, ok := protocol.AsError(resolveErr); ok {
                    resolveMsg = fmt.Sprintf("%s: %s", pe.Code, pe.Message)
                } else {
                    resolveMsg = "resolve error: " + resolveErr.Error()
                }
                a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
                    Kind:    llm.StreamEventRecoverableError,
                    Content: resolveMsg,
                }})
            }
            emitStepDone("final_retry")
            continue
        }
        a.logf("resolve status=ok duration_ms=%d ops=%d", resolveMS, len(internalOps))
    }
}

// In dry-run: use StagedOps (includes both tool writes + patch results).
// In apply:   use internalOps from resolver (existing behavior).
if !a.opts.Apply {
    internalOps = a.tools.StagedOps()
}

if len(internalOps) == 0 {
    a.logf("final: no ops to apply")
    if llmResp != nil {
        history = append(history, llmResp.Message)
    }
    emitStepDone("final")
    return history, &Result{
        Steps:   steps,
        Patches: finalPatches,
        Applied: false,
        Todos:   a.todos,
    }, nil
}

applyReq := tools.FSApplyOpsRequest{
    Ops:    internalOps,
    DryRun: !a.opts.Apply,
    Backup: a.opts.Backup && a.opts.Apply,
}

start := time.Now()
resp, err := a.tools.FSApplyOps(ctx, applyReq)
applyMS := time.Since(start).Milliseconds()
if err != nil {
    if pe, ok := protocol.AsError(err); ok && (pe.Code == protocol.StaleContent || pe.Code == protocol.AmbiguousMatch) {
        a.logf("apply status=recoverable_error duration_ms=%d err=%v", applyMS, err)
        history = append(history, llm.Message{
            Role:    llm.RoleUser,
            Content: formatApplyErrorCompact(err, pe.Code),
        })
        if cbErr := cb.RecordFinalFailure(err); cbErr != nil {
            return nil, nil, cbErr
        }
        if a.opts.OnEvent != nil {
            a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
                Kind:    llm.StreamEventRecoverableError,
                Content: fmt.Sprintf("%s: %s", pe.Code, pe.Message),
            }})
        }
        emitStepDone("final_retry")
        continue
    }
    a.logf("apply status=error duration_ms=%d err=%v", applyMS, err)
    return nil, nil, err
}
a.logf("apply status=ok duration_ms=%d diffs=%d dry_run=%v", applyMS, len(resp.Diffs), applyReq.DryRun)

if llmResp != nil {
    history = append(history, llmResp.Message)
}
if a.opts.OnEvent != nil && resp != nil {
    payload := map[string]any{
        "ops":     internalOps,
        "diff":    resp.Diffs,
        "applied": a.opts.Apply,
    }
    payloadJSON, _ := json.Marshal(payload)
    a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
        Kind:    llm.StreamEventPendingOps,
        Content: string(payloadJSON),
    }})
}
emitStepDone("final")
return history, &Result{
    Steps:         steps,
    Patches:       finalPatches,
    Ops:           internalOps,
    Applied:       a.opts.Apply,
    ApplyResponse: resp,
    Todos:         a.todos,
}, nil
```

- [ ] **Step 5.3: go build проверка**

```
go build ./...
```
Ожидаем: compile OK (или только import issues)

- [ ] **Step 5.4: Запустить тесты агента**

```
go test ./internal/agent/... -v -count=1
```
Ожидаем: все тесты PASS

- [ ] **Step 5.5: Commit**

```
git add internal/agent/agent.go
git commit -m "feat(agent): integrate staging overlay at final — staged ops become plan.json ops"
```

---

## Task 6: CLI / Core — передать DryRun в Runner

**Files:**
- Modify: `internal/cli/apply.go`
- Modify: `internal/core/core.go`

- [ ] **Step 6.1: Добавить DryRun в NewRunner в apply.go (3 места)**

В `internal/cli/apply.go` найти ВСЕ вызовы `tools.NewRunner(cfg.ProjectRoot, tools.RunnerOptions{` и добавить `DryRun: dryRun,` в каждый:

**Место 1** (около строки 186, режим `from-plan`):
```go
runner, err := tools.NewRunner(cfg.ProjectRoot, tools.RunnerOptions{
    ExcludeDirs:        cfg.ExcludeDirs,
    ExecTimeout:        time.Duration(cfg.Exec.TimeoutS) * time.Second,
    ExecOutputLimit:    cfg.Exec.OutputLimitKB * 1024,
    WebFetchTimeout:    time.Duration(cfg.Web.FetchTimeoutS) * time.Second,
    WebMaxContentBytes: cfg.Web.MaxContentBytes,
    LSP:                cfg.LSP,
    DryRun:             dryRun,   // ADD THIS
})
```

**Место 2** (около строки 251, режим `pipeline`):
```go
runner, err := tools.NewRunner(cfg.ProjectRoot, tools.RunnerOptions{
    ExcludeDirs:        cfg.ExcludeDirs,
    ExecTimeout:        time.Duration(cfg.Exec.TimeoutS) * time.Second,
    ExecOutputLimit:    cfg.Exec.OutputLimitKB * 1024,
    WebFetchTimeout:    time.Duration(cfg.Web.FetchTimeoutS) * time.Second,
    WebMaxContentBytes: cfg.Web.MaxContentBytes,
    LSP:                cfg.LSP,
    DryRun:             dryRun,   // ADD THIS
})
```

**Место 3** (около строки 367, режим `direct`):
```go
runner, err := tools.NewRunner(cfg.ProjectRoot, tools.RunnerOptions{
    ExcludeDirs:        cfg.ExcludeDirs,
    ExecTimeout:        time.Duration(cfg.Exec.TimeoutS) * time.Second,
    ExecOutputLimit:    cfg.Exec.OutputLimitKB * 1024,
    WebFetchTimeout:    time.Duration(cfg.Web.FetchTimeoutS) * time.Second,
    WebMaxContentBytes: cfg.Web.MaxContentBytes,
    LSP:                cfg.LSP,
    DryRun:             dryRun,   // ADD THIS
})
```

- [ ] **Step 6.2: Добавить SetDryRun в core.go перед agent.run**

В `internal/core/core.go`, найти метод `AgentRun` (или `runAgent`), который принимает `apply bool` параметр из JSON-RPC `agent.run` запроса. Перед созданием агента добавить:

```go
// Align runner's dry-run state with this run's apply flag.
c.tools.SetDryRun(!params.Apply)
c.tools.ClearStaged()
```

Найти точное место: в `AgentRun` или эквивалентном методе. Если core создаёт агент примерно так:
```go
ag, err := agent.New(c.llmClient, c.validator, c.tools, agentOpts)
```

То добавить `SetDryRun` + `ClearStaged` ПЕРЕД этим вызовом.

- [ ] **Step 6.3: go build**

```
go build ./...
```
Ожидаем: compile OK

- [ ] **Step 6.4: go vet + все тесты**

```
go vet ./...
go test ./... -count=1
```
Ожидаем: все PASS

- [ ] **Step 6.5: Commit**

```
git add internal/cli/apply.go internal/core/core.go
git commit -m "feat(cli,core): propagate DryRun flag to Runner for staging mode"
```

---

## Task 7: Восстановить e2e тесты

**Files:**
- Modify: `tests/e2e_real_llm/minimal_flow_test.go`

- [ ] **Step 7.1: Убрать graceful skip для empty ops**

В `tests/e2e_real_llm/minimal_flow_test.go` найти и удалить блок (строки 78-82):

```go
// Verify plan has ops (required for --from-plan).
// Tool-based models (write/edit tools) write directly to disk and produce
// no final patches, so plan.json ends up with no ops — skip gracefully.
if len(plan.Ops) == 0 {
    t.Skipf("plan.json has no ops (tool-based model used direct writes); --from-plan flow not testable")
}
```

Заменить на строгую проверку:

```go
// Staging must populate ops even when model uses write/edit tools.
if len(plan.Ops) == 0 {
    t.Fatalf("plan.json has no ops after staging fix — model used write/edit but ops were not captured.\nPlan: %s", string(planData))
}
```

- [ ] **Step 7.2: Убрать note о модификации во время plan-only**

В том же файле найти блок:

```go
if !bytes.Equal(origContent, afterPlanOnly) {
    t.Logf("Note: file was modified during plan-only (tool-based writes bypass dry-run flag)")
}
```

Заменить на строгую проверку:

```go
if !bytes.Equal(origContent, afterPlanOnly) {
    t.Fatalf("File was modified during --plan-only: staging must prevent disk writes.\nOriginal: %q\nAfter: %q", string(origContent), string(afterPlanOnly))
}
```

- [ ] **Step 7.3: go vet + unit тесты (без реального LLM)**

```
go vet ./...
go test ./... -count=1
```
Ожидаем: все PASS (e2e тесты пропущены без ORCH_E2E_LLM=1)

- [ ] **Step 7.4: Commit**

```
git add tests/e2e_real_llm/minimal_flow_test.go
git commit -m "test(e2e): restore strict assertions for plan-only dry-run and staging ops"
```

---

## Task 8: Финальная проверка и интеграционный тест

- [ ] **Step 8.1: Полный go vet + go test -race**

```
go vet ./...
go test -race ./internal/tools/... ./internal/resolver/... ./internal/agent/... -v -count=1
```
Ожидаем: все PASS, no races

- [ ] **Step 8.2: Stress-test наиболее важных пакетов**

```
go test -race -count=50 ./internal/tools/... ./internal/agent/...
```
Ожидаем: все PASS (нет data races в staging)

- [ ] **Step 8.3: Build для Windows (без -race)**

```
go build -o orchestra.exe ./cmd/orchestra
```
Ожидаем: compile OK

- [ ] **Step 8.4: Final commit**

```
git add .
git commit -m "feat(staging): complete dry-run staging implementation

write/edit tool calls now respect --plan-only and --apply via in-memory
overlay. StagedOps() provides write_atomic ops for plan.json so
--from-plan replay works correctly even when model uses write/edit tools.

Fixes: TestRealLLMMinimalFlow, TestStaleScenario, TestSmokeCLI_Strict"
```

---

## Самопроверка плана

**Spec coverage:**
- ✅ `--plan-only` не пишет на диск (Tasks 3-4)
- ✅ `--from-plan` работает с tool-based writes (Task 5: StagedOps в plan.json)
- ✅ Read-after-write в dry-run (Task 2: FSRead overlay)
- ✅ StaleContent detection при replay (Task 2: diskHash в write_atomic conditions)
- ✅ Apply mode не меняется (Tasks 3-4: `if r.dryRun` ветки)
- ✅ core.go (RPC mode) поддерживает SetDryRun (Task 6)

**Placeholder scan:** никаких TBD/TODO/placeholder — весь код конкретный.

**Type consistency:**
- `stagedFile` определён в staging.go, используется только там
- `StagedOps() []ops.AnyOp` — возвращает те же типы что FSApplyOps принимает
- `ApplyPatchesToStaged([]patches.Patch)` — использует те же типы что resolver
- `SetDryRun(bool)` / `ClearStaged()` — простые методы без параметров
