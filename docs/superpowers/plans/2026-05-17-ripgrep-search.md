# ripgrep-detection в `search.text` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Если `rg` доступен в PATH — использовать его как backend для инструмента `grep` (`SearchText`). Если нет — молча fallback на существующий Go-native walker.

**Architecture:** Новый файл `internal/search/ripgrep.go` реализует `HasRipgrep()` (одноразовый `sync.Once` probe), `SearchWithRipgrep()` (запуск `rg --json` + parse NDJSON) и `parseRipgrepJSON()` (pure-function парсер). В `runner.go` метод `SearchText()` в самом начале проверяет `search.HasRipgrep()` и ветвится: ripgrep-путь или существующий Go-native-путь. Никаких новых параметров конфигурации — авто-обнаружение.

**Tech Stack:** Go stdlib (`os/exec`, `bufio`, `bytes`, `encoding/json`, `sync`, `errors`). Ripgrep (`rg`) — опциональная внешняя зависимость.

---

## File Structure

- Create: `internal/search/ripgrep.go` — `HasRipgrep`, `SearchWithRipgrep`, `parseRipgrepJSON`
- Create: `internal/search/ripgrep_test.go` — unit-тесты парсера (без rg) + integration (skip без rg)
- Modify: `internal/tools/runner.go` — метод `SearchText`, строки ~551–648

---

### Task 1: `internal/search/ripgrep.go` + тесты парсера

**Files:**
- Create: `internal/search/ripgrep.go`
- Create: `internal/search/ripgrep_test.go`

- [ ] **Step 1: Написать failing тест для `parseRipgrepJSON`**

Создай `internal/search/ripgrep_test.go`:

```go
package search

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// --- parseRipgrepJSON unit tests (no rg binary needed) ---

func TestParseRipgrepJSON_SingleMatch(t *testing.T) {
	// Minimal rg --json output: one match, no context.
	input := `{"type":"begin","data":{"path":{"text":"main.go"}}}
{"type":"match","data":{"path":{"text":"main.go"},"line_number":5,"lines":{"text":"func main() {\n"},"submatches":[]}}
{"type":"end","data":{"path":{"text":"main.go"}}}
{"type":"summary","data":{"elapsed_total":{"secs":0},"stats":{"matches":1,"matched_lines":1,"searches":1,"searches_with_match":1,"bytes_searched":0,"bytes_printed":0}}}
`
	matches := parseRipgrepJSON("/project", []byte(input), 0)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	m := matches[0]
	if m.Line != 5 {
		t.Errorf("expected line 5, got %d", m.Line)
	}
	if m.LineText != "func main() {" {
		t.Errorf("unexpected LineText: %q", m.LineText)
	}
	if filepath.Base(m.FilePath) != "main.go" {
		t.Errorf("unexpected FilePath: %q", m.FilePath)
	}
	if len(m.ContextBefore) != 0 || len(m.ContextAfter) != 0 {
		t.Errorf("expected no context lines, got before=%v after=%v", m.ContextBefore, m.ContextAfter)
	}
}

func TestParseRipgrepJSON_WithContext(t *testing.T) {
	// Match on line 3 with 1 context line before and after.
	input := `{"type":"begin","data":{"path":{"text":"foo.go"}}}
{"type":"context","data":{"path":{"text":"foo.go"},"line_number":2,"lines":{"text":"// comment\n"}}}
{"type":"match","data":{"path":{"text":"foo.go"},"line_number":3,"lines":{"text":"func init() {\n"},"submatches":[]}}
{"type":"context","data":{"path":{"text":"foo.go"},"line_number":4,"lines":{"text":"\tsetup()\n"}}}
{"type":"end","data":{"path":{"text":"foo.go"}}}
`
	matches := parseRipgrepJSON("/project", []byte(input), 1)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	m := matches[0]
	if len(m.ContextBefore) != 1 || m.ContextBefore[0] != "// comment" {
		t.Errorf("unexpected ContextBefore: %v", m.ContextBefore)
	}
	if len(m.ContextAfter) != 1 || m.ContextAfter[0] != "\tsetup()" {
		t.Errorf("unexpected ContextAfter: %v", m.ContextAfter)
	}
}

func TestParseRipgrepJSON_MultiFile(t *testing.T) {
	// Two matches in two different files.
	input := `{"type":"begin","data":{"path":{"text":"a.go"}}}
{"type":"match","data":{"path":{"text":"a.go"},"line_number":1,"lines":{"text":"package main\n"},"submatches":[]}}
{"type":"end","data":{"path":{"text":"a.go"}}}
{"type":"begin","data":{"path":{"text":"b.go"}}}
{"type":"match","data":{"path":{"text":"b.go"},"line_number":2,"lines":{"text":"package main\n"},"submatches":[]}}
{"type":"end","data":{"path":{"text":"b.go"}}}
`
	matches := parseRipgrepJSON("/project", []byte(input), 0)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if filepath.Base(matches[0].FilePath) != "a.go" {
		t.Errorf("expected a.go, got %q", matches[0].FilePath)
	}
	if filepath.Base(matches[1].FilePath) != "b.go" {
		t.Errorf("expected b.go, got %q", matches[1].FilePath)
	}
}

func TestParseRipgrepJSON_Empty(t *testing.T) {
	// Summary-only output (no matches) — rg exits 1, so this is academic,
	// but the parser must return an empty slice without panicking.
	input := `{"type":"summary","data":{"elapsed_total":{"secs":0},"stats":{"matches":0}}}
`
	matches := parseRipgrepJSON("/project", []byte(input), 0)
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

func TestParseRipgrepJSON_ContextDoesNotCrossFiles(t *testing.T) {
	// Last context line of file A must not appear as before-context for file B's match.
	input := `{"type":"begin","data":{"path":{"text":"a.go"}}}
{"type":"match","data":{"path":{"text":"a.go"},"line_number":10,"lines":{"text":"match in a\n"},"submatches":[]}}
{"type":"context","data":{"path":{"text":"a.go"},"line_number":11,"lines":{"text":"after a\n"}}}
{"type":"end","data":{"path":{"text":"a.go"}}}
{"type":"begin","data":{"path":{"text":"b.go"}}}
{"type":"match","data":{"path":{"text":"b.go"},"line_number":1,"lines":{"text":"match in b\n"},"submatches":[]}}
{"type":"end","data":{"path":{"text":"b.go"}}}
`
	matches := parseRipgrepJSON("/project", []byte(input), 3)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	// b.go match must have no before-context (different file than a.go entries).
	if len(matches[1].ContextBefore) != 0 {
		t.Errorf("b.go match should have no before-context, got %v", matches[1].ContextBefore)
	}
}

// --- SearchWithRipgrep integration test (skipped if rg not in PATH) ---

func TestSearchWithRipgrep_Basic(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not in PATH")
	}

	dir := t.TempDir()
	writeSearchFile(t, dir, "hello.go", "package main\n\nfunc hello() {}\n")
	writeSearchFile(t, dir, "world.go", "package main\n\nfunc world() {}\n")

	opts := DefaultOptions()
	opts.ContextLines = 0
	matches, err := SearchWithRipgrep(dir, "hello", nil, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("expected at least one match")
	}
	found := false
	for _, m := range matches {
		if filepath.Base(m.FilePath) == "hello.go" {
			found = true
		}
	}
	if !found {
		t.Error("expected match in hello.go")
	}
}

func TestSearchWithRipgrep_CaseInsensitive(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not in PATH")
	}

	dir := t.TempDir()
	writeSearchFile(t, dir, "f.go", "package main\nfunc Hello() {}\nfunc hello() {}\n")

	opts := DefaultOptions()
	opts.CaseInsensitive = true
	opts.ContextLines = 0
	matches, err := SearchWithRipgrep(dir, "HELLO", nil, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Errorf("expected 2 case-insensitive matches, got %d", len(matches))
	}
}

func TestSearchWithRipgrep_NoMatches(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not in PATH")
	}

	dir := t.TempDir()
	writeSearchFile(t, dir, "f.go", "package main\n")

	opts := DefaultOptions()
	matches, err := SearchWithRipgrep(dir, "NONEXISTENT_STRING_XYZ", nil, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

func TestSearchWithRipgrep_ExcludeDirs(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not in PATH")
	}

	dir := t.TempDir()
	writeSearchFile(t, dir, "root.go", "func target() {}")
	writeSearchFile(t, dir, "vendor/dep.go", "func target() {}")

	opts := DefaultOptions()
	opts.ContextLines = 0
	matches, err := SearchWithRipgrep(dir, "target", []string{"vendor"}, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		if filepath.Base(filepath.Dir(m.FilePath)) == "vendor" {
			t.Errorf("vendor dir should be excluded, got match in %s", m.FilePath)
		}
	}
}

// writeSearchFile is a helper to create test fixture files.
func writeSearchFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Убедиться что тест не компилируется (функции ещё нет)**

```powershell
go test ./internal/search/... -run TestParseRipgrepJSON -v 2>&1 | Select-String "FAIL|error|cannot"
```

Ожидается: ошибка компиляции — `undefined: parseRipgrepJSON`.

- [ ] **Step 3: Написать `internal/search/ripgrep.go`**

```go
package search

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	rgOnce  sync.Once
	rgFound bool
	rgBin   string
)

// HasRipgrep reports whether rg is available in PATH.
// The result is cached after the first call.
func HasRipgrep() bool {
	rgOnce.Do(func() {
		p, err := exec.LookPath("rg")
		if err == nil {
			rgBin = p
			rgFound = true
		}
	})
	return rgFound
}

// rgJSONLine is one NDJSON line from `rg --json`.
type rgJSONLine struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// rgJSONData is the `data` field for `match` and `context` types.
type rgJSONData struct {
	Path struct {
		Text string `json:"text"`
	} `json:"path"`
	LineNumber int `json:"line_number"`
	Lines      struct {
		Text string `json:"text"`
	} `json:"lines"`
}

// rawEntry is an intermediate representation while parsing rg JSON output.
type rawEntry struct {
	path    string
	lineNum int
	text    string
	isMatch bool
}

// SearchWithRipgrep runs rg and returns Match results compatible with SearchInProject.
// scopePaths are absolute paths to scope the search to (files or dirs).
// Pass nil to search the entire root.
func SearchWithRipgrep(root, query string, excludeDirs []string, opts Options, scopePaths []string) ([]Match, error) {
	if !HasRipgrep() {
		return nil, fmt.Errorf("ripgrep not available")
	}

	args := []string{"--json"}
	if opts.CaseInsensitive {
		args = append(args, "-i")
	}
	if opts.ContextLines > 0 {
		args = append(args, fmt.Sprintf("-C%d", opts.ContextLines))
	}
	if opts.MaxMatchesPerFile > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", opts.MaxMatchesPerFile))
	}
	for _, dir := range excludeDirs {
		args = append(args, "--glob=!**/"+dir+"/**")
		args = append(args, "--glob=!"+dir)
	}
	args = append(args, "--", query)
	if len(scopePaths) > 0 {
		args = append(args, scopePaths...)
	} else {
		args = append(args, root)
	}

	cmd := exec.Command(rgBin, args...)
	cmd.Dir = root
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil // exit 1 = no matches found, not an error
		}
		return nil, fmt.Errorf("ripgrep: %w", err)
	}

	return parseRipgrepJSON(root, stdout.Bytes(), opts.ContextLines), nil
}

// parseRipgrepJSON parses the NDJSON stream from `rg --json` and converts it
// into Match values. It is a pure function (no I/O) so it is easy to test.
func parseRipgrepJSON(root string, data []byte, contextLines int) []Match {
	var entries []rawEntry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 2<<20), 2<<20)

	for scanner.Scan() {
		var line rgJSONLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Type != "match" && line.Type != "context" {
			continue
		}
		var d rgJSONData
		if err := json.Unmarshal(line.Data, &d); err != nil {
			continue
		}
		p := d.Path.Text
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, filepath.FromSlash(p))
		}
		entries = append(entries, rawEntry{
			path:    p,
			lineNum: d.LineNumber,
			text:    strings.TrimRight(d.Lines.Text, "\r\n"),
			isMatch: line.Type == "match",
		})
	}

	var matches []Match
	for i, e := range entries {
		if !e.isMatch {
			continue
		}

		var before []string
		for j := i - 1; j >= 0 && len(before) < contextLines; j-- {
			if entries[j].path != e.path {
				break
			}
			before = append([]string{entries[j].text}, before...)
		}

		var after []string
		for j := i + 1; j < len(entries) && len(after) < contextLines; j++ {
			if entries[j].path != e.path {
				break
			}
			after = append(after, entries[j].text)
		}

		matches = append(matches, Match{
			FilePath:      e.path,
			Line:          e.lineNum,
			LineText:      e.text,
			ContextBefore: before,
			ContextAfter:  after,
		})
	}

	return matches
}
```

- [ ] **Step 4: Запустить тесты парсера**

```powershell
go test ./internal/search/... -run "TestParseRipgrepJSON|TestSearchWithRipgrep" -v 2>&1
```

Ожидается: все `TestParseRipgrepJSON_*` тесты PASS. `TestSearchWithRipgrep_*` — PASS если `rg` в PATH, SKIP если нет.

- [ ] **Step 5: Запустить весь пакет search + vet**

```powershell
go vet ./internal/search/...
go test ./internal/search/... -v 2>&1
```

Ожидается: все тесты PASS, нет vet-ошибок.

- [ ] **Step 6: Commit**

```powershell
git add internal/search/ripgrep.go internal/search/ripgrep_test.go
git commit -m "feat(search): add ripgrep backend with JSON parser and integration tests"
```

---

### Task 2: Dispatch в `runner.go` `SearchText()`

**Files:**
- Modify: `internal/tools/runner.go` (метод `SearchText`, строки ~551–648)

- [ ] **Step 1: Написать failing тест для проверки dispatch**

В конец `internal/search/ripgrep_test.go` добавить импорт `os` (уже нужен для `writeSearchFile`). Убедиться что тест компилируется — больше ничего писать не нужно, dispatch-тест уже покрывается существующими search-тестами.

Вместо этого верифицируем что build не ломается до правки:

```powershell
go build ./internal/tools/...
```

Ожидается: успешная сборка.

- [ ] **Step 2: Изменить метод `SearchText` в `internal/tools/runner.go`**

Найти блок (строки ~576–613):

```go
	var matches []search.Match
	if len(req.Paths) == 0 {
		m, err := search.SearchInProject(r.workspaceRoot, query, exclude, opts)
		if err != nil {
			return nil, err
		}
		matches = append(matches, m...)
	} else {
		queryLower := strings.ToLower(query)
		for _, p := range req.Paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			abs, _, err := resolveWorkspacePath(r.workspaceRoot, p)
			if err != nil {
				return nil, err
			}
			st, err := os.Stat(abs)
			if err != nil {
				return nil, err
			}
			if st.IsDir() {
				m, err := search.SearchInProject(abs, query, exclude, opts)
				if err != nil {
					return nil, err
				}
				matches = append(matches, m...)
				continue
			}
			// File scope: search only within this file.
			b, err := os.ReadFile(abs)
			if err != nil {
				return nil, err
			}
			matches = append(matches, searchInSingleFile(abs, string(b), query, queryLower, opts)...)
		}
	}
```

Заменить на:

```go
	var matches []search.Match
	if search.HasRipgrep() {
		// Build absolute scope paths for ripgrep (nil = whole project).
		var scopePaths []string
		for _, p := range req.Paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			abs, _, err := resolveWorkspacePath(r.workspaceRoot, p)
			if err != nil {
				return nil, err
			}
			scopePaths = append(scopePaths, abs)
		}
		m, err := search.SearchWithRipgrep(r.workspaceRoot, query, exclude, opts, scopePaths)
		if err != nil {
			return nil, err
		}
		matches = m
	} else if len(req.Paths) == 0 {
		m, err := search.SearchInProject(r.workspaceRoot, query, exclude, opts)
		if err != nil {
			return nil, err
		}
		matches = append(matches, m...)
	} else {
		queryLower := strings.ToLower(query)
		for _, p := range req.Paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			abs, _, err := resolveWorkspacePath(r.workspaceRoot, p)
			if err != nil {
				return nil, err
			}
			st, err := os.Stat(abs)
			if err != nil {
				return nil, err
			}
			if st.IsDir() {
				m, err := search.SearchInProject(abs, query, exclude, opts)
				if err != nil {
					return nil, err
				}
				matches = append(matches, m...)
				continue
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return nil, err
			}
			matches = append(matches, searchInSingleFile(abs, string(b), query, queryLower, opts)...)
		}
	}
```

- [ ] **Step 3: Сборка + vet**

```powershell
go build ./...
go vet ./...
```

Ожидается: чистая сборка, нет vet-ошибок.

- [ ] **Step 4: Запустить весь test suite**

```powershell
go test ./... -count=1 2>&1 | Select-String "FAIL|ok"
```

Ожидается: все пакеты `ok`, нет строк `FAIL`.

- [ ] **Step 5: Commit**

```powershell
git add internal/tools/runner.go
git commit -m "feat(tools): use ripgrep in SearchText when available, fallback to go-native"
```

---

## Self-Review Checklist

**Spec coverage:**
- `HasRipgrep()` — `sync.Once` probe, кешируется ✅ Task 1
- `SearchWithRipgrep()` — `rg --json`, exclude dirs, case-insensitive, context lines, max-count, scope paths, exit-1 → nil ✅ Task 1
- `parseRipgrepJSON()` — NDJSON parse, context before/after, cross-file boundary guard, trailing CRLF trim ✅ Task 1 (tests: 5 unit)
- Integration tests skip gracefully без rg ✅ Task 1
- Dispatch в `SearchText()`: ripgrep first, Go-native fallback ✅ Task 2
- Все существующие тесты остаются зелёными ✅ Task 2 Step 4

**Placeholder scan:** Нет TBD/TODO.

**Type consistency:**
- `parseRipgrepJSON` возвращает `[]Match` (не `([]Match, error)`) — достаточно, ошибки парсинга игнорируются gracefully (rg сам валидирует вывод)
- `SearchWithRipgrep` сигнатура совпадает с вызовом в `runner.go`
- `rawEntry` — приватный тип внутри пакета, не экспортируется

**Edge cases:**
- rg exit 1 (no matches) → `nil, nil` — не ошибка
- rg exit 2 (ошибка rg) → пробрасывается как `error`
- Нет `rg` в PATH → `HasRipgrep()` = false → Go-native
- Windows-пути: rg выдаёт OS-разделитель, `filepath.Join` в `parseRipgrepJSON` нормализует под ОС, `filepath.ToSlash` в `runner.go` конвертирует для модели ✅
