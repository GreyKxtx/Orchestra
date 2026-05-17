# Tools Backlog #8 + #9 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Добавить раздел Imports/Imported-by в package-level explore (#8) и новый инструмент `diff.preview` (#9) для предварительного просмотра изменений без записи на диск.

**Architecture:** #8 — добавить приватный хелпер `appendImportsSection()` в `internal/ckg/provider.go`, вызывать в конце `explorePackage()`. #9 — новый файл `internal/tools/diff_preview.go` + регистрация в `registry.go` + диспетч в `call.go`. Для генерации unified diff используется уже зависимость `github.com/aymanbagabas/go-udiff` (`udiff.Unified`).

**Tech Stack:** Go, SQLite (для CKG queries), go-udiff (уже в go.mod как indirect dep)

---

## Файловая карта

| Файл | Действие | Причина |
|---|---|---|
| `internal/ckg/provider.go` | Modify | Добавить `appendImportsSection()` и вызов в `explorePackage()` |
| `internal/ckg/provider_test.go` | Modify | Тест для imports section |
| `internal/tools/diff_preview.go` | Create | `FSPreviewRequest`, `FSPreviewResponse`, `Runner.FSPreview()` |
| `internal/tools/diff_preview_test.go` | Create | Unit-тесты для diff.preview |
| `internal/tools/registry.go` | Modify | `toolDiffPreview()` + добавить в все tool lists + parallel flags |
| `internal/tools/call.go` | Modify | case "diff.preview" |
| `docs/tools-overview.md` | Modify | Отметить #7 ✅, добавить #8 ✅, добавить #9 в таблицу |

---

### Task 0: Обновить документацию (#7 → done)

**Files:**
- Modify: `docs/tools-overview.md`

- [ ] **Step 1: Обновить статусы в backlog-таблице**

В файле `docs/tools-overview.md` найди секцию "Бэклог улучшений для локальных моделей" и измени строки:

```
| 7 | **Автодиагностика после `edit`** | После каждого `edit`/`write` на .go файл автоматически запускать `go vet` или `lsp.diagnostics` и добавлять результат в tool response | 🔲 Отложено |
| 8 | **Граф зависимостей пакетов** | `explore("internal/agent/imports")` → что импортирует пакет и кто импортирует его. Помогает понять куда поместить новый код | 🔲 Отложено |
| 9 | **`diff_preview`** | Показать что изменится перед применением патча, без записи на диск | 🔲 Отложено |
```

Замени на:
```
| 7 | **Автодиагностика после `edit`** | После каждого `edit`/`write` на .go файл автоматически запускать `go vet` или `lsp.diagnostics` и добавлять результат в tool response | ✅ Готово — LSP diagnostics возвращаются в `FSEditResponse.Diagnostics` и `FSWriteResponse.Diagnostics` когда gopls настроен |
| 8 | **Граф зависимостей пакетов** | Секция "Зависимости" в package-level `explore("internal/agent")` → что импортирует + кто импортирует | ✅ Готово |
| 9 | **`diff.preview`** | Инструмент `diff.preview(path, search, replace)` — unified diff без записи на диск | ✅ Готово |
```

- [ ] **Step 2: Убедиться что файл компилируется (нет кода, просто doc)**

```
go vet ./...
```
Ожидание: OK (без ошибок)

- [ ] **Step 3: Commit**

```bash
git add docs/tools-overview.md
git commit -m "docs: mark tools backlog #7 done, start #8+#9"
```

---

### Task 1: Раздел Imports в package-level explore (#8)

**Files:**
- Modify: `internal/ckg/provider.go` (добавить `appendImportsSection` и вызов)
- Modify: `internal/ckg/provider_test.go` (новый тест)

- [ ] **Step 1: Написать failing тест**

Добавь в конец `internal/ckg/provider_test.go`:

```go
func TestExplorePackage_ImportsSection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	savePackage := func(file, pkgFQN string, edges []Edge) {
		t.Helper()
		nodes := []Node{{FQN: pkgFQN, ShortName: lastSegment(pkgFQN), Kind: "package", LineStart: 1, LineEnd: 1}}
		if err := s.SaveFileNodes(ctx, file, "h", "go", "ex", "pkg", nodes, edges); err != nil {
			t.Fatal(err)
		}
	}
	// auth не импортирует ничего; api и svc импортируют auth.
	savePackage("ex/auth/auth.go", "ex/auth", nil)
	savePackage("ex/api/api.go", "ex/api", []Edge{
		{SourceFQN: "ex/api", TargetFQN: "ex/auth", Relation: "imports"},
		{SourceFQN: "ex/api", TargetFQN: "ex/utils", Relation: "imports"},
	})
	savePackage("ex/svc/svc.go", "ex/svc", []Edge{
		{SourceFQN: "ex/svc", TargetFQN: "ex/auth", Relation: "imports"},
	})

	p := NewProvider(s, "/tmp")

	// Explore auth: должны быть importers (api, svc), без outgoing.
	authResult, err := p.ExploreSymbol(ctx, "ex/auth")
	if err != nil {
		t.Fatalf("ExploreSymbol(ex/auth): %v", err)
	}
	if !strings.Contains(authResult, "Используется в") {
		t.Errorf("expected 'Используется в' in auth result:\n%s", authResult)
	}
	if !strings.Contains(authResult, "ex/api") {
		t.Errorf("expected 'ex/api' importer in auth result:\n%s", authResult)
	}
	if !strings.Contains(authResult, "ex/svc") {
		t.Errorf("expected 'ex/svc' importer in auth result:\n%s", authResult)
	}

	// Explore api: должны быть outgoing imports (auth, utils), без importers.
	apiResult, err := p.ExploreSymbol(ctx, "ex/api")
	if err != nil {
		t.Fatalf("ExploreSymbol(ex/api): %v", err)
	}
	if !strings.Contains(apiResult, "Импортирует") {
		t.Errorf("expected 'Импортирует' in api result:\n%s", apiResult)
	}
	if !strings.Contains(apiResult, "ex/auth") {
		t.Errorf("expected 'ex/auth' in api imports:\n%s", apiResult)
	}
	if !strings.Contains(apiResult, "ex/utils") {
		t.Errorf("expected 'ex/utils' in api imports:\n%s", apiResult)
	}
}
```

- [ ] **Step 2: Запустить тест — убедиться что FAIL**

```
go test ./internal/ckg -run TestExplorePackage_ImportsSection -v
```
Ожидание: FAIL (функция не реализована — секция не появляется в выводе)

- [ ] **Step 3: Реализовать `appendImportsSection` в `provider.go`**

Найди в `internal/ckg/provider.go` строки перед `func isPackagePath` (около строки 292). Вставь ПЕРЕД этой функцией:

```go
// appendImportsSection appends a "### Зависимости" section to sb for pkgPath.
// Silently skips if no import data is available.
func (p *Provider) appendImportsSection(ctx context.Context, sb *strings.Builder, pkgPath string) {
	// Find the full package FQN from a package-kind node in the package files.
	row := p.store.db.QueryRowContext(ctx, `
		SELECT DISTINCT n.fqn FROM nodes n JOIN files f ON n.file_id = f.id
		WHERE n.kind = 'package'
		  AND f.path LIKE ?
		  AND f.path NOT LIKE ?
		LIMIT 1`,
		pkgPath+"/%",
		pkgPath+"/%/%",
	)
	var pkgFQN string
	if err := row.Scan(&pkgFQN); err != nil {
		return
	}

	// Outgoing imports: what this package imports.
	outRows, err := p.store.db.QueryContext(ctx, `
		SELECT DISTINCT e.target_fqn FROM edges e
		JOIN nodes n ON e.source_id = n.id
		WHERE n.fqn = ? AND e.relation = 'imports'
		ORDER BY e.target_fqn`,
		pkgFQN,
	)
	if err != nil {
		return
	}
	defer outRows.Close()
	var outImports []string
	for outRows.Next() {
		var s string
		if err := outRows.Scan(&s); err != nil {
			return
		}
		outImports = append(outImports, s)
	}
	_ = outRows.Err()

	// Incoming imports: who imports this package.
	importers, err := p.Importers(ctx, pkgFQN)
	if err != nil {
		importers = nil
	}

	if len(outImports) == 0 && len(importers) == 0 {
		return
	}

	sb.WriteString("### Зависимости\n")
	if len(outImports) > 0 {
		sb.WriteString("**Импортирует:**\n")
		for _, imp := range outImports {
			sb.WriteString(fmt.Sprintf("- `%s`\n", imp))
		}
	}
	if len(importers) > 0 {
		sb.WriteString("**Используется в:**\n")
		for _, imp := range importers {
			sb.WriteString(fmt.Sprintf("- `%s`\n", imp))
		}
	}
	sb.WriteString("\n")
}
```

- [ ] **Step 4: Вызвать `appendImportsSection` в `explorePackage`**

В `internal/ckg/provider.go` найди функцию `explorePackage`. В конце функции, ПЕРЕД строкой:
```go
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("→ Детали типа: ..."))
```

Добавь:
```go
	// Зависимости (imports / imported-by).
	p.appendImportsSection(ctx, &sb, pkgPath)

```

Полный конец функции должен выглядеть так (показан контекст около строки 450):
```go
	// --- Orphan methods ---
	for recv, methods := range methodsByRecv {
		if _, found := typeIndex[recv]; !found {
			sb.WriteString(fmt.Sprintf("### Методы `%s` (тип определён вне пакета или в другом файле)\n", recv))
			sb.WriteString("- " + strings.Join(methods, ", ") + "\n\n")
		}
	}

	// Зависимости (imports / imported-by).
	p.appendImportsSection(ctx, &sb, pkgPath)

	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("→ Детали типа: explore(\"Agent\") · Метод целиком: explore(\"Agent.Run\") · Конкретный поиск: grep(\"паттерн\", paths=[\"%s\"])\n", pkgPath))

	return sb.String(), nil
}
```

- [ ] **Step 5: Запустить тест — убедиться что PASS**

```
go test ./internal/ckg -run TestExplorePackage_ImportsSection -v
```
Ожидание: PASS

- [ ] **Step 6: Запустить все тесты CKG**

```
go test ./internal/ckg/... -v
```
Ожидание: все PASS

- [ ] **Step 7: Commit**

```bash
git add internal/ckg/provider.go internal/ckg/provider_test.go
git commit -m "feat(ckg): add imports/imported-by section to package-level explore"
```

---

### Task 2: Инструмент `diff.preview` (#9)

**Files:**
- Create: `internal/tools/diff_preview.go`
- Create: `internal/tools/diff_preview_test.go`
- Modify: `internal/tools/registry.go`
- Modify: `internal/tools/call.go`

#### Шаг 1 — тест, потом реализация

- [ ] **Step 1: Написать failing тесты**

Создай файл `internal/tools/diff_preview_test.go`:

```go
package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newPreviewRunner(t *testing.T) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

func TestFSPreview_BasicSearchReplace(t *testing.T) {
	r, root := newPreviewRunner(t)

	content := "package main\n\nfunc Hello() string {\n\treturn \"world\"\n}\n"
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	resp, err := r.FSPreview(context.Background(), FSPreviewRequest{
		Path:    "main.go",
		Search:  "return \"world\"",
		Replace: "return \"earth\"",
	})
	if err != nil {
		t.Fatalf("FSPreview: %v", err)
	}
	if resp.Path != "main.go" {
		t.Errorf("path: got %q, want %q", resp.Path, "main.go")
	}
	if !strings.Contains(resp.Diff, "-\treturn \"world\"") {
		t.Errorf("diff missing removed line, got:\n%s", resp.Diff)
	}
	if !strings.Contains(resp.Diff, "+\treturn \"earth\"") {
		t.Errorf("diff missing added line, got:\n%s", resp.Diff)
	}
	// File on disk must NOT be modified.
	got, _ := os.ReadFile(filepath.Join(root, "main.go"))
	if string(got) != content {
		t.Errorf("file was modified on disk — should not happen")
	}
}

func TestFSPreview_SearchNotFound_ReturnsError(t *testing.T) {
	r, root := newPreviewRunner(t)

	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := r.FSPreview(context.Background(), FSPreviewRequest{
		Path:    "f.go",
		Search:  "nonexistent string xyz",
		Replace: "something",
	})
	if err == nil {
		t.Fatal("expected error when search not found, got nil")
	}
}

func TestFSPreview_EmptyPath_ReturnsError(t *testing.T) {
	r, _ := newPreviewRunner(t)
	_, err := r.FSPreview(context.Background(), FSPreviewRequest{Path: "", Search: "x", Replace: "y"})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestFSPreview_EmptySearch_ReturnsError(t *testing.T) {
	r, root := newPreviewRunner(t)
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := r.FSPreview(context.Background(), FSPreviewRequest{Path: "f.go", Search: "", Replace: "y"})
	if err == nil {
		t.Fatal("expected error for empty search")
	}
}

func TestFSPreview_DryRunReadsFromStaging(t *testing.T) {
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{DryRun: true})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	// Stage content without writing to disk.
	r.stageFile("staged.go", "package main\n\nfunc Foo() {}\n", "fakehash")

	resp, err := r.FSPreview(context.Background(), FSPreviewRequest{
		Path:    "staged.go",
		Search:  "func Foo() {}",
		Replace: "func Bar() {}",
	})
	if err != nil {
		t.Fatalf("FSPreview from staging: %v", err)
	}
	if !strings.Contains(resp.Diff, "-func Foo()") {
		t.Errorf("expected staged content in diff, got:\n%s", resp.Diff)
	}
}
```

- [ ] **Step 2: Запустить тесты — убедиться что FAIL**

```
go test ./internal/tools -run TestFSPreview -v
```
Ожидание: compile error (FSPreview не определена)

- [ ] **Step 3: Создать `internal/tools/diff_preview.go`**

```go
package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/resolver"
)

type FSPreviewRequest struct {
	Path    string `json:"path"`
	Search  string `json:"search"`
	Replace string `json:"replace"`
}

type FSPreviewResponse struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
}

// FSPreview applies search→replace in memory and returns a unified diff without writing to disk.
// In dry-run mode it reads from the staging overlay.
func (r *Runner) FSPreview(ctx context.Context, req FSPreviewRequest) (*FSPreviewResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	if req.Search == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "search is empty", nil)
	}

	absPath, relSlash, err := resolveWorkspacePath(r.workspaceRoot, path)
	if err != nil {
		return nil, err
	}

	var current []byte
	if r.dryRun {
		if staged, _, ok := r.stagedContent(relSlash); ok {
			current = []byte(staged)
		} else {
			b, readErr := os.ReadFile(absPath)
			if readErr != nil && !os.IsNotExist(readErr) {
				return nil, readErr
			}
			current = b
		}
	} else {
		b, readErr := os.ReadFile(absPath)
		if readErr != nil {
			return nil, fmt.Errorf("read file: %w", readErr)
		}
		current = b
	}

	newContent, applyErr := resolver.ApplySearchReplace(current, req.Search, req.Replace)
	if applyErr != nil {
		return nil, applyErr
	}

	diff := udiff.Unified("a/"+relSlash, "b/"+relSlash, string(current), string(newContent))
	return &FSPreviewResponse{Path: relSlash, Diff: diff}, nil
}
```

- [ ] **Step 4: Зарегистрировать `diff.preview` в `registry.go`**

**Шаг 4a**: Добавь в `applyParallelFlags` (в секцию pure reads, около строки 56):
```go
case "ls", "read", "glob", "grep", "symbols", "explore",
    "todoread", "task.result", "runtime.query", "webfetch",
    "lsp.definition", "lsp.references", "lsp.hover", "lsp.diagnostics",
    "diff.preview":
    defs[i].ParallelSafe = true
```

**Шаг 4b**: Добавь функцию-определение инструмента в конец файла (перед `mustSchema`):

```go
func toolDiffPreview() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "diff.preview",
			Description: "Предварительный просмотр изменений: применяет search→replace в памяти и возвращает unified diff без записи на диск. Используй перед edit чтобы убедиться что замена правильная.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "search", "replace"],
  "properties": {
    "path":    { "type": "string", "minLength": 1, "description": "Путь к файлу относительно workspace root" },
    "search":  { "type": "string", "minLength": 1, "description": "Текст для поиска (как в edit)" },
    "replace": { "type": "string", "description": "Текст замены" }
  }
}`),
		},
	}
}
```

**Шаг 4c**: Добавь `toolDiffPreview()` в нужные списки:

В `ListTools()` (основной список, около строки 14) — добавь после `toolExploreCodebase()`:
```go
toolDiffPreview(),
```

В `listToolsBuild()` — добавь после `toolExploreCodebase()`:
```go
toolDiffPreview(),
```

В `listToolsPlan()` — добавь после `toolExploreCodebase()`:
```go
toolDiffPreview(),
```

В `listToolsGeneral()` — добавь после `toolExploreCodebase()`:
```go
toolDiffPreview(),
```

В `allToolDefsMap()` — добавь в слайс:
```go
toolDiffPreview(),
```

- [ ] **Step 5: Добавить диспетч в `call.go`**

После case `"explore"` (около строки 123), добавь:

```go
	case "diff.preview":
		var req FSPreviewRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSPreview(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)
```

- [ ] **Step 6: Запустить тесты — убедиться что PASS**

```
go test ./internal/tools -run TestFSPreview -v
```
Ожидание: все 4 теста PASS

- [ ] **Step 7: Запустить go vet и все тесты tools**

```
go vet ./...
go test ./internal/tools/... -count=1
```
Ожидание: vet OK, все тесты green

- [ ] **Step 8: Commit**

```bash
git add internal/tools/diff_preview.go internal/tools/diff_preview_test.go \
         internal/tools/registry.go internal/tools/call.go
git commit -m "feat(tools): add diff.preview tool — unified diff preview without disk write"
```

---

### Task 3: Финальная документация и тесты всего

**Files:**
- Modify: `docs/tools-overview.md`

- [ ] **Step 1: Добавить diff.preview в таблицу "Изменение файлов"**

В секции "Изменение файлов" добавь строку:

```
| `diff.preview` | Применяет search→replace в памяти, возвращает unified diff | Хочешь проверить что изменится перед применением edit |
```

- [ ] **Step 2: Добавить diff.preview в таблицу "Покрытие тестами"**

Добавь строку:
```
| `diff.preview` | `diff_preview_test.go` | Unit |
```

- [ ] **Step 3: Запустить все тесты финально**

```
go test ./... -count=1
```
Ожидание: все green

- [ ] **Step 4: Commit**

```bash
git add docs/tools-overview.md
git commit -m "docs: update tools-overview with diff.preview and imports section"
```

---

## Self-Review

**Spec coverage:**
- #7 (автодиагностика) — закрываем как done (LSP уже в ответах FSEdit/FSWrite) ✓
- #8 (импорты в пакетном explore) — Task 1 ✓
- #9 (diff.preview) — Task 2 ✓

**Placeholder scan:** Весь код полный, нет TBD/TODO.

**Type consistency:**
- `FSPreviewRequest` / `FSPreviewResponse` — определены в Task 2 Step 3, используются в Step 5 ✓
- `appendImportsSection(ctx, &sb, pkgPath)` — определена в Task 1 Step 3, вызывается в Step 4 ✓
- `toolDiffPreview()` — определена в Step 4b, используется в Step 4c ✓
