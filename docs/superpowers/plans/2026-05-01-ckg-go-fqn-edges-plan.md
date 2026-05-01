# Под-проект 0: Доводка Go-CKG до точного — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Превратить существующий Go-CKG (`internal/ckg/`) из строкового скелета в алгоритмически точный граф: FQN-имена в Go-конвенции, edges с FK на `node_id` и lazy-резолюцией внешних символов, отдельный relation `imports`, один долгоживущий Store на жизнь Runner.

**Architecture:** Три коммита. (1) Lifecycle: Store как обязательный член `tools.Runner`, инфраструктура миграции через `PRAGMA user_version`. (2) Ядро: новая DDL v2 + парсер на FQN + извлечение imports. (3) Обвязка: новые `SaveFileNodes`, новые методы `Provider` (Callers/Callees/Importers), правка `explore_codebase` и UI.

**Tech Stack:** Go 1.25, modernc.org/sqlite, github.com/smacker/go-tree-sitter, существующий `internal/agent/`/`internal/core/`/`internal/tools/` контракт.

**User preference (из memory `user_prefs.md`):** реализуем функциональность сначала, тесты — после. План отражает этот порядок: код → run build → тесты → run tests → commit.

**Spec:** `docs/superpowers/specs/2026-05-01-ckg-go-fqn-edges-design.md` (APPROVED).

---

## File Structure

| Файл | Действие | Ответственность |
|---|---|---|
| `internal/ckg/store.go` | Modify | DDL v2, миграция, новые типы `Node`/`Edge`, новый `SaveFileNodes` |
| `internal/ckg/gomod.go` | Create | `ParseModulePath(rootDir)` — чтение `go.mod` |
| `internal/ckg/fqn.go` | Create | `GoFQN(modulePath, rootDir, file, recvType, name)` — построение FQN |
| `internal/ckg/parser.go` | Modify | передача `modulePath` + `rootDir`, `ParseFile` возвращает edges с FQN; извлечение imports |
| `internal/ckg/orchestrator.go` | Modify | прокидывание `modulePath` в `ParseFile`, кэш модуля внутри `Orchestrator` |
| `internal/ckg/provider.go` | Modify | `ExploreSymbol` (FQN/short_name), новые методы `Callers/Callees/Importers` |
| `internal/ckg/ui.go` | Modify | правка SELECT'ов под новую схему (если ломается) |
| `internal/tools/tools.go` | Modify | `Runner.ckgStore`/`ckgProvider`, открытие в `NewRunner`, `Runner.Close()` |
| `internal/tools/explore_codebase.go` | Modify | использовать `r.ckgProvider`, без open/close per-call |
| `internal/core/core.go` | Modify | `Core.Close()` (вызывает `Runner.Close()`) |
| `cmd/orchestra/*.go` | Modify | defer `core.Close()` в core/apply commands |
| `internal/ckg/store_test.go` | Create | тесты на миграцию, `SaveFileNodes`, lazy-резолюцию |
| `internal/ckg/fqn_test.go` | Create | тесты на `GoFQN`, `ParseModulePath` |
| `internal/ckg/provider_test.go` | Create | тесты на BFS/DFS, `Callers/Callees/Importers` |
| `internal/ckg/parser_test.go` | Modify | обновить под новую схему `Node`/`Edge` |
| `internal/ckg/orchestrator_test.go` | Modify | тот же фикс |
| `internal/ckg/scanner_test.go` | Modify | тот же фикс (если ломается) |
| `internal/tools/explore_codebase_test.go` | Create | тест что `NewStore` зовётся ровно раз на N вызовов |

---

# Коммит 1: Runner lifecycle + migration framework

**Цель коммита:** Store становится обязательным членом Runner; инфраструктура миграции через `PRAGMA user_version` готова, но схема пока на v1 (бамп до v2 — в коммите 2). Проект собирается и тесты зелёные.

---

### Task 1.1: Добавить `Store.Close()`-safe и migration framework через `PRAGMA user_version`

**Files:**
- Modify: `internal/ckg/store.go:53-88`

- [ ] **Step 1: Заменить `migrate()` на версионированную миграцию**

В `internal/ckg/store.go` заменить тело `migrate()`:

```go
func (s *Store) migrate() error {
    var version int
    if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
        return err
    }

    const targetVersion = 1

    if version < 1 {
        // Initial schema (v1). Existing behaviour preserved.
        ddl := `
        CREATE TABLE IF NOT EXISTS files (
            id INTEGER PRIMARY KEY,
            path TEXT UNIQUE NOT NULL,
            hash TEXT NOT NULL,
            language TEXT NOT NULL,
            updated_at DATETIME NOT NULL
        );
        CREATE TABLE IF NOT EXISTS nodes (
            id INTEGER PRIMARY KEY,
            file_id INTEGER NOT NULL,
            name TEXT NOT NULL,
            type TEXT NOT NULL,
            line_start INTEGER NOT NULL,
            line_end INTEGER NOT NULL,
            complexity INTEGER NOT NULL DEFAULT 0,
            FOREIGN KEY(file_id) REFERENCES files(id) ON DELETE CASCADE
        );
        CREATE TABLE IF NOT EXISTS edges (
            file_id INTEGER NOT NULL,
            source_name TEXT NOT NULL,
            target_name TEXT NOT NULL,
            relation TEXT NOT NULL,
            FOREIGN KEY(file_id) REFERENCES files(id) ON DELETE CASCADE,
            UNIQUE(file_id, source_name, target_name, relation)
        );
        CREATE INDEX IF NOT EXISTS idx_files_path ON files(path);
        CREATE INDEX IF NOT EXISTS idx_nodes_name ON nodes(name);
        `
        if _, err := s.db.Exec(ddl); err != nil {
            return err
        }
        if _, err := s.db.Exec("PRAGMA user_version = 1"); err != nil {
            return err
        }
    }
    _ = targetVersion // используется в коммите 2 при бампе до 2
    return nil
}
```

- [ ] **Step 2: Run build**

Run: `go build ./...`
Expected: успешная сборка без ошибок.

- [ ] **Step 3: Run vet + existing tests**

Run: `go vet ./... && go test ./internal/ckg/...`
Expected: все тесты зелёные. Существующие БД с `user_version = 0` мигрируют до v1 (CREATE TABLE IF NOT EXISTS — no-op для уже созданных таблиц, потом PRAGMA выставляется).

---

### Task 1.2: Добавить `ckgStore` и `ckgProvider` в `tools.Runner`, открывать в `NewRunner`

**Files:**
- Modify: `internal/tools/tools.go:14-78`

- [ ] **Step 1: Добавить импорт ckg и поля в Runner**

В `internal/tools/tools.go` после блока `import (...)` добавить:

```go
import (
    // ... существующие импорты
    "github.com/orchestra/orchestra/internal/ckg"
)
```

В struct `Runner` добавить поля после `mcpCaller`:

```go
type Runner struct {
    workspaceRoot string
    excludeDirs   []string

    execTimeout     time.Duration
    execOutputLimit int

    mcpCaller MCPCaller

    ckgStore    *ckg.Store
    ckgProvider *ckg.Provider
}
```

- [ ] **Step 2: Открывать Store в `NewRunner`**

Заменить тело `NewRunner` (`internal/tools/tools.go:44-73`) на версию, которая открывает Store:

```go
func NewRunner(workspaceRoot string, opts RunnerOptions) (*Runner, error) {
    if strings.TrimSpace(workspaceRoot) == "" {
        return nil, fmt.Errorf("workspaceRoot is empty")
    }
    rootAbs, err := filepath.Abs(workspaceRoot)
    if err != nil {
        return nil, fmt.Errorf("abs workspaceRoot: %w", err)
    }

    exclude := append([]string(nil), opts.ExcludeDirs...)
    if len(exclude) == 0 {
        exclude = []string{".git", "node_modules", "dist", "build", ".orchestra"}
    }

    timeout := opts.ExecTimeout
    if timeout <= 0 {
        timeout = 30 * time.Second
    }
    limit := opts.ExecOutputLimit
    if limit <= 0 {
        limit = 100 * 1024
    }

    orchDir := filepath.Join(rootAbs, ".orchestra")
    if err := os.MkdirAll(orchDir, 0755); err != nil {
        return nil, fmt.Errorf("mkdir .orchestra: %w", err)
    }
    dbPath := filepath.Join(orchDir, "ckg.db")
    store, err := ckg.NewStore("file:" + dbPath + "?cache=shared")
    if err != nil {
        return nil, fmt.Errorf("open ckg store: %w", err)
    }
    provider := ckg.NewProvider(store, rootAbs)

    return &Runner{
        workspaceRoot:   rootAbs,
        excludeDirs:     exclude,
        execTimeout:     timeout,
        execOutputLimit: limit,
        ckgStore:        store,
        ckgProvider:     provider,
    }, nil
}
```

- [ ] **Step 3: Добавить `Runner.Close()`**

После `WorkspaceRoot()` (`internal/tools/tools.go:75`) добавить:

```go
// Close releases resources held by the Runner (CKG store, etc).
// Safe to call multiple times.
func (r *Runner) Close() error {
    if r.ckgStore != nil {
        err := r.ckgStore.Close()
        r.ckgStore = nil
        r.ckgProvider = nil
        return err
    }
    return nil
}
```

- [ ] **Step 4: Run build**

Run: `go build ./...`
Expected: сборка зелёная.

---

### Task 1.3: Перевести `explore_codebase` на `r.ckgProvider`

**Files:**
- Modify: `internal/tools/explore_codebase.go:21-49`

- [ ] **Step 1: Заменить тело `ExploreCodebase`**

Заменить функцию (`internal/tools/explore_codebase.go:21-49`) на:

```go
func (r *Runner) ExploreCodebase(ctx context.Context, req ExploreCodebaseRequest) (*ExploreCodebaseResponse, error) {
    if r.ckgStore == nil || r.ckgProvider == nil {
        return nil, fmt.Errorf("ckg store not initialized")
    }

    // Update graph incrementally on every call (millisecond if no changes).
    orch := ckg.NewOrchestrator(r.ckgStore, r.workspaceRoot)
    if err := orch.UpdateGraph(ctx); err != nil {
        return nil, err
    }

    content, err := r.ckgProvider.ExploreSymbol(ctx, req.SymbolName)
    if err != nil {
        return nil, err
    }
    return &ExploreCodebaseResponse{Content: content}, nil
}
```

И убрать неиспользуемые импорты (`os`, `path/filepath`) из этого файла. Добавить `fmt`.

- [ ] **Step 2: Run build**

Run: `go build ./...`
Expected: сборка зелёная.

---

### Task 1.4: `Core.Close()` + вызов из cmd

**Files:**
- Modify: `internal/core/core.go`
- Modify: `internal/cli/apply.go` (или туда, где создаётся Runner)
- Modify: `cmd/orchestra/main.go` (или сабкоманды `core` / `apply`)

- [ ] **Step 1: Найти, где создаётся Runner**

Run: `grep -rn "tools.NewRunner" internal/ cmd/`
Зафиксировать список файлов. Ожидаются: `internal/core/core.go`, `internal/cli/apply.go` (как минимум).

- [ ] **Step 2: Добавить Close-метод в Core**

В `internal/core/core.go` (там, где определена структура `Core`) добавить метод (если его ещё нет):

```go
// Close releases resources held by Core (Runner / CKG store).
// Safe to call multiple times.
func (c *Core) Close() error {
    if c.runner != nil {
        return c.runner.Close()
    }
    return nil
}
```

(Если поле зовётся не `runner` — поправить.)

- [ ] **Step 3: Defer Close на стороне CLI**

В каждом месте, где создаётся `*tools.Runner` напрямую (вне Core) — добавить `defer runner.Close()`. Где создаётся `*core.Core` — `defer core.Close()`.

Конкретные строки фиксируются по результатам шага 1; для каждой добавить:

```go
defer func() {
    if err := runner.Close(); err != nil {
        // не маскируем основную ошибку, просто логируем
        log.Printf("runner close: %v", err)
    }
}()
```

(или для Core — `defer core.Close()`).

- [ ] **Step 4: Run build + tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: всё зелёное на твоей платформе. На Windows проверить отдельно `go test ./internal/ckg/... ./internal/tools/...`.

---

### Task 1.5: Тесты на lifecycle Runner (Store открывается один раз)

**Files:**
- Create: `internal/tools/explore_codebase_test.go`

- [ ] **Step 1: Написать тест**

Создать `internal/tools/explore_codebase_test.go`:

```go
package tools

import (
    "context"
    "os"
    "path/filepath"
    "testing"
)

func TestRunnerOpensCKGStoreOnce(t *testing.T) {
    tmp := t.TempDir()
    // Создать минимальный go-файл, чтобы было что индексировать.
    src := "package foo\n\nfunc Hello() {}\n"
    if err := os.WriteFile(filepath.Join(tmp, "foo.go"), []byte(src), 0644); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/foo\n\ngo 1.25\n"), 0644); err != nil {
        t.Fatal(err)
    }

    runner, err := NewRunner(tmp, RunnerOptions{})
    if err != nil {
        t.Fatalf("NewRunner: %v", err)
    }
    defer runner.Close()

    if runner.ckgStore == nil {
        t.Fatal("ckgStore is nil after NewRunner")
    }
    storePtr := runner.ckgStore

    ctx := context.Background()
    for i := 0; i < 5; i++ {
        if _, err := runner.ExploreCodebase(ctx, ExploreCodebaseRequest{SymbolName: "Hello"}); err != nil {
            t.Fatalf("ExploreCodebase #%d: %v", i, err)
        }
        if runner.ckgStore != storePtr {
            t.Fatalf("ckgStore pointer changed on call #%d — store reopened!", i)
        }
    }
}

func TestRunnerCloseIdempotent(t *testing.T) {
    tmp := t.TempDir()
    runner, err := NewRunner(tmp, RunnerOptions{})
    if err != nil {
        t.Fatal(err)
    }
    if err := runner.Close(); err != nil {
        t.Fatalf("first Close: %v", err)
    }
    if err := runner.Close(); err != nil {
        t.Fatalf("second Close: %v", err)
    }
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/tools/ -run TestRunnerOpensCKGStoreOnce -run TestRunnerCloseIdempotent -v`
Expected: оба теста PASS.

- [ ] **Step 3: Commit (Коммит 1)**

```bash
git add internal/ckg/store.go internal/tools/tools.go internal/tools/explore_codebase.go internal/tools/explore_codebase_test.go internal/core/core.go internal/cli/apply.go cmd/orchestra/
git commit -m "refactor(ckg): runner-owned store + migration framework

- Runner now owns *ckg.Store opened in NewRunner, closed in Runner.Close()
- explore_codebase uses long-lived store instead of open/close per call
- migrate() switched to PRAGMA user_version pattern (currently v1, schema unchanged)
- Core.Close() cascades to Runner.Close()
- tests verify single-open + idempotent Close"
```

Run: `git status` — ожидаем clean working tree (или другие непричастные изменения).

---

# Коммит 2: Schema v2 + парсер FQN + imports

**Цель коммита:** новая DDL развёрнута (`PRAGMA user_version = 2`), парсер вычисляет полные FQN для Go-символов, извлекает imports как edges с `relation='imports'`. Структуры `Node`/`Edge` обновлены. `SaveFileNodes` пока не адаптирован — он ломается, поэтому изменения коммита 2 и коммита 3 могут потребоваться вместе (см. секцию «Fallback на атомарный коммит 2+3» внизу).

---

### Task 2.1: Создать `internal/ckg/gomod.go` с `ParseModulePath`

**Files:**
- Create: `internal/ckg/gomod.go`

- [ ] **Step 1: Реализовать**

Создать `internal/ckg/gomod.go`:

```go
package ckg

import (
    "bufio"
    "os"
    "path/filepath"
    "strings"
)

// ParseModulePath reads go.mod from rootDir and returns the module path.
// Returns ("", nil) if go.mod is missing (workspace is not a Go module).
// Comments and blank lines are skipped; only the first `module ...` directive is honoured.
func ParseModulePath(rootDir string) (string, error) {
    f, err := os.Open(filepath.Join(rootDir, "go.mod"))
    if err != nil {
        if os.IsNotExist(err) {
            return "", nil
        }
        return "", err
    }
    defer f.Close()

    scanner := bufio.NewScanner(f)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" || strings.HasPrefix(line, "//") {
            continue
        }
        if strings.HasPrefix(line, "module ") {
            mod := strings.TrimSpace(strings.TrimPrefix(line, "module"))
            // Strip optional surrounding quotes
            mod = strings.Trim(mod, `"`)
            return mod, nil
        }
    }
    if err := scanner.Err(); err != nil {
        return "", err
    }
    return "", nil
}
```

- [ ] **Step 2: Run build**

Run: `go build ./internal/ckg/`
Expected: сборка зелёная.

---

### Task 2.2: Создать `internal/ckg/fqn.go` с `GoFQN`

**Files:**
- Create: `internal/ckg/fqn.go`

- [ ] **Step 1: Реализовать**

Создать `internal/ckg/fqn.go`:

```go
package ckg

import (
    "path/filepath"
    "strings"
)

// GoFQN returns the importpath-qualified name for a Go symbol, following
// the convention used by `go doc`:
//
//   <importpath>.<Type>.<Method>   for methods
//   <importpath>.<Func>            for top-level functions
//   <importpath>.<Type>            for top-level types
//
// where importpath = modulePath + "/" + relative-dir-of-file (slash-separated).
//
// modulePath: result of ParseModulePath; may be empty for non-module workspaces.
// rootDir:    workspace root (absolute).
// filePath:   absolute path to the source file.
// recvType:   empty for funcs/types; type name (without pointer) for methods.
// symbol:     the func/method/type name.
func GoFQN(modulePath, rootDir, filePath, recvType, symbol string) string {
    pkgPath := goPackagePath(modulePath, rootDir, filePath)
    if recvType != "" {
        return pkgPath + "." + recvType + "." + symbol
    }
    return pkgPath + "." + symbol
}

// GoPackageFQN returns just the package importpath for `relation='imports'` edges
// (e.g. "github.com/orchestra/orchestra/internal/agent").
func GoPackageFQN(modulePath, rootDir, filePath string) string {
    return goPackagePath(modulePath, rootDir, filePath)
}

func goPackagePath(modulePath, rootDir, filePath string) string {
    relDir, err := filepath.Rel(rootDir, filepath.Dir(filePath))
    if err != nil {
        relDir = ""
    }
    relDir = filepath.ToSlash(relDir)
    if relDir == "." {
        relDir = ""
    }

    switch {
    case modulePath != "" && relDir != "":
        return modulePath + "/" + relDir
    case modulePath != "":
        return modulePath
    case relDir != "":
        return relDir
    default:
        return ""
    }
}

// IsLikelyFQN returns true if `q` looks like a fully-qualified name
// (contains a slash or more than one dot) — used by Provider.ExploreSymbol
// to distinguish FQN lookups from short-name fuzzy lookups.
func IsLikelyFQN(q string) bool {
    return strings.Contains(q, "/") || strings.Count(q, ".") > 1
}
```

- [ ] **Step 2: Run build**

Run: `go build ./internal/ckg/`
Expected: зелёная.

---

### Task 2.3: Тесты на `gomod.go` + `fqn.go`

**Files:**
- Create: `internal/ckg/fqn_test.go`

- [ ] **Step 1: Написать тесты**

Создать `internal/ckg/fqn_test.go`:

```go
package ckg

import (
    "os"
    "path/filepath"
    "testing"
)

func TestParseModulePath(t *testing.T) {
    tmp := t.TempDir()
    if err := os.WriteFile(filepath.Join(tmp, "go.mod"),
        []byte("// header\n\nmodule example.com/foo/bar\n\ngo 1.25\n"), 0644); err != nil {
        t.Fatal(err)
    }
    got, err := ParseModulePath(tmp)
    if err != nil {
        t.Fatal(err)
    }
    if got != "example.com/foo/bar" {
        t.Fatalf("got %q, want example.com/foo/bar", got)
    }
}

func TestParseModulePathMissing(t *testing.T) {
    tmp := t.TempDir()
    got, err := ParseModulePath(tmp)
    if err != nil {
        t.Fatal(err)
    }
    if got != "" {
        t.Fatalf("got %q, want empty", got)
    }
}

func TestGoFQN(t *testing.T) {
    root := filepath.FromSlash("/repo")
    tests := []struct {
        name, modulePath, file, recvType, symbol, want string
    }{
        {"top-level func", "github.com/x/y", "/repo/internal/agent/agent.go", "", "Run",
            "github.com/x/y/internal/agent.Run"},
        {"method", "github.com/x/y", "/repo/internal/agent/agent.go", "Agent", "Run",
            "github.com/x/y/internal/agent.Agent.Run"},
        {"root pkg", "github.com/x/y", "/repo/main.go", "", "main",
            "github.com/x/y.main"},
        {"no module path", "", "/repo/internal/agent/agent.go", "", "Run",
            "internal/agent.Run"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := GoFQN(tt.modulePath, root, filepath.FromSlash(tt.file), tt.recvType, tt.symbol)
            if got != tt.want {
                t.Errorf("got %q, want %q", got, tt.want)
            }
        })
    }
}

func TestIsLikelyFQN(t *testing.T) {
    cases := map[string]bool{
        "Run":                          false,
        "Agent.Run":                    false, // ровно одна точка — short_name с receiver
        "github.com/x/y.Agent.Run":     true,
        "internal/agent.Run":           true,
        "fmt.Println":                  false,
    }
    for in, want := range cases {
        if got := IsLikelyFQN(in); got != want {
            t.Errorf("IsLikelyFQN(%q): got %v, want %v", in, got, want)
        }
    }
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/ckg/ -run TestParseModulePath -run TestGoFQN -run TestIsLikelyFQN -v`
Expected: все тесты PASS.

---

### Task 2.4: Обновить DDL до v2 в `Store.migrate()`

**Files:**
- Modify: `internal/ckg/store.go` (тело `migrate()`, бамп до v2)

- [ ] **Step 1: Заменить migrate на v2-вариант**

В `internal/ckg/store.go` заменить тело `migrate()` целиком:

```go
func (s *Store) migrate() error {
    var version int
    if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
        return err
    }

    const targetVersion = 2

    if version >= targetVersion {
        return nil
    }

    // Local cache — drop+recreate is acceptable. Incremental scan rebuilds quickly.
    drop := `
        DROP TABLE IF EXISTS edges;
        DROP TABLE IF EXISTS nodes;
        DROP TABLE IF EXISTS files;
    `
    if _, err := s.db.Exec(drop); err != nil {
        return err
    }

    ddl := `
    CREATE TABLE files (
        id          INTEGER PRIMARY KEY,
        path        TEXT UNIQUE NOT NULL,
        hash        TEXT NOT NULL,
        language    TEXT NOT NULL,
        module_path TEXT,
        package     TEXT,
        updated_at  DATETIME NOT NULL
    );
    CREATE INDEX idx_files_path ON files(path);

    CREATE TABLE nodes (
        id          INTEGER PRIMARY KEY,
        file_id     INTEGER NOT NULL,
        fqn         TEXT UNIQUE NOT NULL,
        short_name  TEXT NOT NULL,
        kind        TEXT NOT NULL,
        line_start  INTEGER NOT NULL,
        line_end    INTEGER NOT NULL,
        complexity  INTEGER NOT NULL DEFAULT 0,
        FOREIGN KEY(file_id) REFERENCES files(id) ON DELETE CASCADE
    );
    CREATE INDEX idx_nodes_fqn        ON nodes(fqn);
    CREATE INDEX idx_nodes_short_name ON nodes(short_name);
    CREATE INDEX idx_nodes_file_id    ON nodes(file_id);

    CREATE TABLE edges (
        id         INTEGER PRIMARY KEY,
        source_id  INTEGER NOT NULL,
        target_id  INTEGER,
        target_fqn TEXT NOT NULL,
        relation   TEXT NOT NULL,
        FOREIGN KEY(source_id) REFERENCES nodes(id) ON DELETE CASCADE,
        FOREIGN KEY(target_id) REFERENCES nodes(id) ON DELETE SET NULL
    );
    CREATE INDEX idx_edges_source_id  ON edges(source_id);
    CREATE INDEX idx_edges_target_id  ON edges(target_id);
    CREATE INDEX idx_edges_target_fqn ON edges(target_fqn);
    CREATE UNIQUE INDEX idx_edges_unique ON edges(source_id, target_fqn, relation);
    `
    if _, err := s.db.Exec(ddl); err != nil {
        return err
    }
    if _, err := s.db.Exec("PRAGMA user_version = 2"); err != nil {
        return err
    }
    return nil
}
```

- [ ] **Step 2: Обновить структуры `Node` и `Edge`**

В `internal/ckg/store.go` заменить определения (`internal/ckg/store.go:16-30`):

```go
type Node struct {
    ID         int64
    FileID     int64
    FQN        string // "github.com/x/y/internal/agent.Agent.Run"
    ShortName  string // "Run"
    Kind       string // "func" | "method" | "struct" | "interface" | "type" | "package"
    LineStart  int
    LineEnd    int
    Complexity int
}

// Edge represents a directed relation in the graph.
// SourceFQN must reference a node within the same file as it's saved (resolved
// inside SaveFileNodes). TargetFQN may reference any node (internal or external);
// the resolver fills in TargetID where possible.
type Edge struct {
    SourceFQN string
    TargetFQN string
    Relation  string // "calls" | "imports" | "instantiates"
}
```

**Внимание:** `SaveFileNodes` теперь не компилируется — это ожидаемо, фиксируется в коммите 3 / Task 3.1. До Task 3.1 пакет `ckg` компилироваться не будет; делайте Task 2.4 → Task 2.5 → Task 2.6 → Task 3.1 без промежуточных `go build`.

---

### Task 2.5: Обновить `parser.go` — выдавать FQN-узлы и edges, извлекать imports

**Files:**
- Modify: `internal/ckg/parser.go`

- [ ] **Step 1: Поменять сигнатуру `ParseFile`**

Сигнатура с дополнительными параметрами для FQN. Заменить:

```go
func ParseFile(ctx context.Context, filePath string) ([]Node, []Edge, error) {
```

на:

```go
// ParseFile parses a Go source file and returns nodes (with FQN) and edges (FQN→FQN).
// modulePath comes from go.mod; rootDir is the workspace root.
func ParseFile(ctx context.Context, modulePath, rootDir, filePath string) ([]Node, []Edge, string, error) {
```

Третий результат — имя пакета (для `files.package`).

- [ ] **Step 2: Реализовать FQN внутри ParseFile**

Существующий цикл по queries оставить, но изменить блок «name + defNode»:

```go
if name != "" && defNode != nil {
    nodeKind := inferNodeType(defNode)
    recvType := ""
    if defNode.Type() == "method_declaration" && ext == ".go" {
        recvNode := defNode.ChildByFieldName("receiver")
        if recvNode != nil {
            recvType = extractGoReceiverType(recvNode, src)
        }
    }
    fqn := GoFQN(modulePath, rootDir, filePath, recvType, name)
    shortName := name
    if recvType != "" {
        shortName = recvType + "." + name // совпадает со старым форматом
    }
    nodes = append(nodes, Node{
        FQN:       fqn,
        ShortName: shortName,
        Kind:      nodeKind,
        LineStart: int(defNode.StartPoint().Row) + 1,
        LineEnd:   int(defNode.EndPoint().Row) + 1,
    })
}
```

- [ ] **Step 3: Резолвить package name**

Перед циклом queries добавить запрос на `package_clause` для имени пакета:

```go
var pkgName string
{
    pq, err := sitter.NewQuery([]byte(`(package_clause (package_identifier) @name)`), lang)
    if err == nil {
        qc := sitter.NewQueryCursor()
        qc.Exec(pq, root)
        if m, ok := qc.NextMatch(); ok && len(m.Captures) > 0 {
            pkgName = m.Captures[0].Node.Content(src)
        }
    }
}
```

И возвращать `pkgName` четвёртым значением.

- [ ] **Step 4: Извлечь imports**

После цикла обработки определений и перед `callQueries` добавить блок imports:

```go
// Imports — package-level edges. Source — the package node (kind='package'),
// which we synthesize once per file. Target — imported package FQN (string).
{
    pkgFQN := GoPackageFQN(modulePath, rootDir, filePath)
    // Synthesize a single package-level node per file (deduped at Save level).
    nodes = append(nodes, Node{
        FQN:        pkgFQN,
        ShortName:  pkgName,
        Kind:       "package",
        LineStart:  1,
        LineEnd:    1,
        Complexity: 0,
    })

    iq, err := sitter.NewQuery([]byte(`(import_spec path: (interpreted_string_literal) @path)`), lang)
    if err == nil {
        qc := sitter.NewQueryCursor()
        qc.Exec(iq, root)
        for {
            m, ok := qc.NextMatch()
            if !ok {
                break
            }
            for _, c := range m.Captures {
                raw := c.Node.Content(src) // includes quotes
                imp := strings.Trim(raw, `"`)
                if imp == "" {
                    continue
                }
                edges = append(edges, Edge{
                    SourceFQN: pkgFQN,
                    TargetFQN: imp,
                    Relation:  "imports",
                })
            }
        }
    }
}
```

(Не забыть `import "strings"` если ещё не импортировано.)

- [ ] **Step 5: Обновить call-edges под FQN**

В блоке `callQueries` заменить логику поиска `sourceName` на FQN. Сейчас она ищет parent node по line range — оставить, но возвращать не `sourceName`, а **`source_fqn` = FQN parent-node**:

```go
// Найти parent-node по line range и взять его FQN
var sourceFQN string
for _, n := range nodes {
    if callLine >= n.LineStart && callLine <= n.LineEnd && n.Kind != "package" {
        sourceFQN = n.FQN
        break
    }
}

if sourceFQN != "" && calledName != "" {
    // calledName — short-имя; для внешних вызовов FQN мы не знаем
    // (Resolver внутри Store попытается заматчить по short_name → fqn для intra-module).
    edges = append(edges, Edge{
        SourceFQN: sourceFQN,
        TargetFQN: calledName, // best-effort; будет заматчено позже по short_name
        Relation:  "calls",
    })
}
```

**Замечание о точности:** для звонков вида `pkg.Func()` (selector_expression) `calledName` сейчас — это field («Func»). Точное разрешение `pkg.Func` → FQN требует tracking imports внутри файла и знания alias'ов. В MVP коммита 2 оставляем best-effort short-name; полное разрешение — задача расширения в под-проекте 1 или отдельной итерации этого. Это явно зафиксировано как known-limitation.

- [ ] **Step 6: Обновить return**

```go
return nodes, edges, pkgName, nil
```

(четыре значения).

---

### Task 2.6: Обновить `orchestrator.go` под новую сигнатуру

**Files:**
- Modify: `internal/ckg/orchestrator.go`

- [ ] **Step 1: Кэшировать modulePath в Orchestrator**

Заменить `Orchestrator` (`internal/ckg/orchestrator.go:10-23`):

```go
type Orchestrator struct {
    store      *Store
    scanner    *Scanner
    root       string
    modulePath string
}

func NewOrchestrator(store *Store, root string) *Orchestrator {
    mp, _ := ParseModulePath(root) // безопасно: пустая строка для не-Go репо
    return &Orchestrator{
        store:      store,
        scanner:    NewScanner(store, root),
        root:       root,
        modulePath: mp,
    }
}
```

- [ ] **Step 2: Прокинуть modulePath + pkgName в SaveFileNodes**

В `UpdateGraph` заменить блок парсинга (`internal/ckg/orchestrator.go:41-64`):

```go
for _, relPath := range toParse {
    absPath := filepath.Join(o.root, filepath.FromSlash(relPath))

    nodes, edges, pkgName, err := ParseFile(ctx, o.modulePath, o.root, absPath)
    if err != nil {
        continue
    }

    hash, err := hashFile(absPath)
    if err != nil {
        continue
    }

    lang := LanguageFromExt(filepath.Ext(absPath))

    if err := o.store.SaveFileNodes(ctx, relPath, hash, lang, o.modulePath, pkgName, nodes, edges); err != nil {
        return err
    }
}
```

(Ожидаем, что `SaveFileNodes` обновлён в Task 3.1 под новые параметры.)

---

### Task 2.7: Commit (ВНИМАНИЕ: см. fallback)

- [ ] **Step 1: Проверка состояния**

К этому моменту пакет `ckg` **не компилируется** — `SaveFileNodes` имеет старую сигнатуру и обращается к удалённым полям. Это нормально по плану.

**Не коммитим коммит 2 отдельно.** Идём в коммит 3, после `SaveFileNodes` сделаем единый сшивающий коммит, либо два маленьких если получится.

Выбор: **fallback на атомарный коммит 2+3** (см. секцию ниже).

---

# Коммит 3: SaveFileNodes + Provider + UI + tests

**Цель:** новая `SaveFileNodes` с резолюцией target_id и lazy-резолюцией ранее висевших edges; `Provider` с `ExploreSymbol`/`Callers`/`Callees`/`Importers`; правка UI; тесты на BFS/DFS, на Importers, на lazy-резолюцию.

---

### Task 3.1: Новый `SaveFileNodes` с резолвером target_id

**Files:**
- Modify: `internal/ckg/store.go` (метод `SaveFileNodes`)

- [ ] **Step 1: Заменить тело SaveFileNodes**

Сигнатура:

```go
func (s *Store) SaveFileNodes(ctx context.Context, path, hash, lang, modulePath, pkgName string, nodes []Node, edges []Edge) error
```

Тело:

```go
func (s *Store) SaveFileNodes(ctx context.Context, path, hash, lang, modulePath, pkgName string, nodes []Node, edges []Edge) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // 1. CASCADE удаляет старые nodes/edges этого файла.
    if _, err := tx.ExecContext(ctx, "DELETE FROM files WHERE path = ?", path); err != nil {
        return err
    }

    // 2. Insert files row.
    res, err := tx.ExecContext(ctx,
        "INSERT INTO files (path, hash, language, module_path, package, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
        path, hash, lang, modulePath, pkgName, time.Now())
    if err != nil {
        return err
    }
    fileID, err := res.LastInsertId()
    if err != nil {
        return err
    }

    // 3. Insert nodes.
    fqnToID := make(map[string]int64, len(nodes))
    if len(nodes) > 0 {
        stmt, err := tx.PrepareContext(ctx,
            `INSERT INTO nodes (file_id, fqn, short_name, kind, line_start, line_end, complexity)
             VALUES (?, ?, ?, ?, ?, ?, ?)
             ON CONFLICT(fqn) DO UPDATE SET
                 file_id    = excluded.file_id,
                 short_name = excluded.short_name,
                 kind       = excluded.kind,
                 line_start = excluded.line_start,
                 line_end   = excluded.line_end,
                 complexity = excluded.complexity
             RETURNING id`)
        if err != nil {
            return err
        }
        defer stmt.Close()

        for i := range nodes {
            var newID int64
            err = stmt.QueryRowContext(ctx,
                fileID, nodes[i].FQN, nodes[i].ShortName, nodes[i].Kind,
                nodes[i].LineStart, nodes[i].LineEnd, nodes[i].Complexity,
            ).Scan(&newID)
            if err != nil {
                return err
            }
            nodes[i].ID = newID
            fqnToID[nodes[i].FQN] = newID
        }
    }

    // 4. Insert edges. SourceID берём из fqnToID (source всегда in-file).
    //    TargetID резолвим SELECT id FROM nodes WHERE fqn = ? (любой файл).
    if len(edges) > 0 {
        ins, err := tx.PrepareContext(ctx,
            `INSERT OR IGNORE INTO edges (source_id, target_id, target_fqn, relation)
             VALUES (?, ?, ?, ?)`)
        if err != nil {
            return err
        }
        defer ins.Close()

        sel, err := tx.PrepareContext(ctx, `SELECT id FROM nodes WHERE fqn = ?`)
        if err != nil {
            return err
        }
        defer sel.Close()

        for _, e := range edges {
            sourceID, ok := fqnToID[e.SourceFQN]
            if !ok {
                // source не найден среди nodes файла — пропускаем (вероятно, ошибка парсера)
                continue
            }
            var targetID *int64
            var tid int64
            err := sel.QueryRowContext(ctx, e.TargetFQN).Scan(&tid)
            if err == nil {
                targetID = &tid
            } else if err != sql.ErrNoRows {
                return err
            }
            if _, err := ins.ExecContext(ctx, sourceID, targetID, e.TargetFQN, e.Relation); err != nil {
                return err
            }
        }
    }

    // 5. Lazy-резолюция: обновить ранее висящие edges, чьи target_fqn совпадают
    //    с FQN наших новых nodes.
    if len(fqnToID) > 0 {
        upd, err := tx.PrepareContext(ctx,
            `UPDATE edges SET target_id = ? WHERE target_fqn = ? AND target_id IS NULL`)
        if err != nil {
            return err
        }
        defer upd.Close()
        for fqn, id := range fqnToID {
            if _, err := upd.ExecContext(ctx, id, fqn); err != nil {
                return err
            }
        }
    }

    return tx.Commit()
}
```

Импорт `database/sql` уже есть в файле. Нужно `import "database/sql"` если ещё нет — оно there.

- [ ] **Step 2: Run build**

Run: `go build ./internal/ckg/`
Expected: пакет компилируется. Если ошибки — фиксим в этом же шаге, не идём дальше.

---

### Task 3.2: Обновить `Provider.ExploreSymbol` под FQN/short_name

**Files:**
- Modify: `internal/ckg/provider.go`

- [ ] **Step 1: Заменить тело ExploreSymbol**

В `internal/ckg/provider.go`, заменить `ExploreSymbol` на:

```go
func (p *Provider) ExploreSymbol(ctx context.Context, query string) (string, error) {
    var rows *sql.Rows
    var err error

    if IsLikelyFQN(query) {
        rows, err = p.store.db.QueryContext(ctx, `
            SELECT n.id, n.fqn, n.short_name, n.kind, n.line_start, n.line_end, f.path
            FROM nodes n JOIN files f ON n.file_id = f.id
            WHERE n.fqn = ?`, query)
    } else {
        rows, err = p.store.db.QueryContext(ctx, `
            SELECT n.id, n.fqn, n.short_name, n.kind, n.line_start, n.line_end, f.path
            FROM nodes n JOIN files f ON n.file_id = f.id
            WHERE n.short_name = ?`, query)
    }
    if err != nil {
        return "", err
    }
    defer rows.Close()

    type hit struct {
        id        int64
        fqn       string
        shortName string
        kind      string
        lineStart int
        lineEnd   int
        relPath   string
    }
    var hits []hit
    for rows.Next() {
        var h hit
        if err := rows.Scan(&h.id, &h.fqn, &h.shortName, &h.kind, &h.lineStart, &h.lineEnd, &h.relPath); err != nil {
            continue
        }
        hits = append(hits, h)
    }

    if len(hits) == 0 {
        return p.fuzzyFallback(ctx, query)
    }
    if len(hits) > 1 && !IsLikelyFQN(query) {
        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("Запрос '%s' неоднозначен — найдено %d символов с одинаковым short-name. Уточните FQN:\n\n", query, len(hits)))
        for _, h := range hits {
            sb.WriteString(fmt.Sprintf("- `%s` (%s в %s, строки %d-%d)\n", h.fqn, h.kind, h.relPath, h.lineStart, h.lineEnd))
        }
        return sb.String(), nil
    }

    var sb strings.Builder
    for _, h := range hits {
        absPath := filepath.Join(p.root, filepath.FromSlash(h.relPath))
        content, err := os.ReadFile(absPath)
        if err != nil {
            sb.WriteString(fmt.Sprintf("Error reading file %s: %v\n\n", h.relPath, err))
            continue
        }
        lines := strings.Split(string(content), "\n")
        ls, le := h.lineStart, h.lineEnd
        if ls < 1 { ls = 1 }
        if le > len(lines) { le = len(lines) }
        snippet := strings.Join(lines[ls-1:le], "\n")

        sb.WriteString(fmt.Sprintf("### `%s` (%s) в `%s` (строки %d-%d)\n", h.fqn, h.kind, h.relPath, ls, le))
        ext := filepath.Ext(h.relPath)
        sb.WriteString(fmt.Sprintf("```%s\n%s\n```\n\n", LanguageFromExt(ext), snippet))

        // Callers
        cRows, _ := p.store.db.QueryContext(ctx,
            `SELECT n.fqn, e.relation FROM edges e JOIN nodes n ON e.source_id = n.id
             WHERE e.target_fqn = ?`, h.fqn)
        if cRows != nil {
            first := true
            for cRows.Next() {
                if first {
                    sb.WriteString("**Вызывается из (callers):**\n")
                    first = false
                }
                var srcFQN, rel string
                if cRows.Scan(&srcFQN, &rel) == nil {
                    sb.WriteString(fmt.Sprintf("- `%s` (%s)\n", srcFQN, rel))
                }
            }
            cRows.Close()
            if !first {
                sb.WriteString("\n")
            }
        }

        // Callees
        dRows, _ := p.store.db.QueryContext(ctx,
            `SELECT e.target_fqn, e.relation FROM edges e WHERE e.source_id = ?`, h.id)
        if dRows != nil {
            first := true
            for dRows.Next() {
                if first {
                    sb.WriteString("**Зависит от (callees):**\n")
                    first = false
                }
                var tgtFQN, rel string
                if dRows.Scan(&tgtFQN, &rel) == nil {
                    sb.WriteString(fmt.Sprintf("- `%s` (%s)\n", tgtFQN, rel))
                }
            }
            dRows.Close()
            if !first {
                sb.WriteString("\n")
            }
        }
    }
    return sb.String(), nil
}

func (p *Provider) fuzzyFallback(ctx context.Context, query string) (string, error) {
    rows, err := p.store.db.QueryContext(ctx, `
        SELECT n.fqn, n.kind, f.path FROM nodes n JOIN files f ON n.file_id = f.id
        WHERE n.short_name LIKE ? LIMIT 5`, "%"+query+"%")
    if err != nil {
        return "", err
    }
    defer rows.Close()
    var sugg []string
    for rows.Next() {
        var fqn, kind, p string
        if rows.Scan(&fqn, &kind, &p) == nil {
            sugg = append(sugg, fmt.Sprintf("- `%s` (%s в %s)", fqn, kind, p))
        }
    }
    if len(sugg) == 0 {
        return fmt.Sprintf("Символ '%s' не найден в графе.", query), nil
    }
    return fmt.Sprintf("Символ '%s' не найден точно. Похожие:\n%s", query, strings.Join(sugg, "\n")), nil
}
```

Не забыть импорт `database/sql` в `provider.go` (используется для `*sql.Rows`). Также убедиться, что `os`, `strings`, `fmt`, `path/filepath` импортированы.

- [ ] **Step 2: Добавить `Callers`, `Callees`, `Importers`**

В `provider.go` после `fuzzyFallback` добавить:

```go
// Callers returns all nodes that have a "calls" or "instantiates" edge whose
// target_fqn equals the given fqn.
func (p *Provider) Callers(ctx context.Context, fqn string) ([]Node, error) {
    rows, err := p.store.db.QueryContext(ctx, `
        SELECT n.id, n.file_id, n.fqn, n.short_name, n.kind, n.line_start, n.line_end, n.complexity
        FROM edges e JOIN nodes n ON e.source_id = n.id
        WHERE e.target_fqn = ? AND e.relation IN ('calls','instantiates')`, fqn)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    return scanNodes(rows)
}

// Callees returns all edges originating from the node with the given fqn.
func (p *Provider) Callees(ctx context.Context, fqn string) ([]Edge, error) {
    rows, err := p.store.db.QueryContext(ctx, `
        SELECT e.target_fqn, e.relation FROM edges e
        JOIN nodes n ON e.source_id = n.id
        WHERE n.fqn = ?`, fqn)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []Edge
    for rows.Next() {
        var e Edge
        e.SourceFQN = fqn
        if err := rows.Scan(&e.TargetFQN, &e.Relation); err != nil {
            return nil, err
        }
        out = append(out, e)
    }
    return out, nil
}

// Importers returns FQNs of all packages that import the given package FQN.
func (p *Provider) Importers(ctx context.Context, packageFQN string) ([]string, error) {
    rows, err := p.store.db.QueryContext(ctx, `
        SELECT DISTINCT n.fqn FROM edges e
        JOIN nodes n ON e.source_id = n.id
        WHERE e.target_fqn = ? AND e.relation = 'imports' AND n.kind = 'package'`, packageFQN)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []string
    for rows.Next() {
        var s string
        if err := rows.Scan(&s); err != nil {
            return nil, err
        }
        out = append(out, s)
    }
    return out, nil
}

func scanNodes(rows *sql.Rows) ([]Node, error) {
    var out []Node
    for rows.Next() {
        var n Node
        if err := rows.Scan(&n.ID, &n.FileID, &n.FQN, &n.ShortName, &n.Kind, &n.LineStart, &n.LineEnd, &n.Complexity); err != nil {
            return nil, err
        }
        out = append(out, n)
    }
    return out, nil
}
```

- [ ] **Step 3: Run build**

Run: `go build ./internal/ckg/`
Expected: зелёная.

---

### Task 3.3: Обновить UI под новую схему

**Files:**
- Modify: `internal/ckg/ui.go` (и `internal/ckg/ui/` если там SQL)

- [ ] **Step 1: Найти SQL-запросы в UI**

Run: `grep -n "SELECT\|FROM nodes\|FROM edges\|source_name\|target_name\|n.name" internal/ckg/ui*.go`
Expected: список мест, где UI обращается к БД.

- [ ] **Step 2: Заменить колонки**

В каждом месте:
- `nodes.name` → `nodes.fqn` (или `nodes.short_name` для отображения).
- `nodes.type` → `nodes.kind`.
- `edges.source_name` → JOIN на `nodes` через `edges.source_id`, выбрать `n.fqn`.
- `edges.target_name` → `edges.target_fqn`.

Для UI чаще всего нужны короткие имена для отображения — выбирать `short_name` для лейблов нод, `fqn` для кликов/деталей.

- [ ] **Step 3: Run build + ручная проверка**

Run: `go build ./...`
Expected: зелёная.

Ручная проверка после коммита: `orchestra ckg-ui -p 6061`, открыть в браузере, убедиться что граф рисуется и не падает.

---

### Task 3.4: Тесты на новую схему — миграция, SaveFileNodes, lazy-резолюция

**Files:**
- Create: `internal/ckg/store_test.go`

- [ ] **Step 1: Написать тесты**

Создать `internal/ckg/store_test.go`:

```go
package ckg

import (
    "context"
    "path/filepath"
    "testing"
)

func newTestStore(t *testing.T) *Store {
    t.Helper()
    tmp := t.TempDir()
    s, err := NewStore(filepath.Join(tmp, "test.db"))
    if err != nil {
        t.Fatalf("NewStore: %v", err)
    }
    t.Cleanup(func() { s.Close() })
    return s
}

func TestMigrateToV2(t *testing.T) {
    s := newTestStore(t)
    var v int
    if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
        t.Fatal(err)
    }
    if v != 2 {
        t.Fatalf("user_version = %d, want 2", v)
    }

    // Ожидаем колонку nodes.fqn (UNIQUE).
    var dummy string
    err := s.db.QueryRow(`SELECT fqn FROM nodes LIMIT 1`).Scan(&dummy)
    // sql.ErrNoRows ок (таблица пустая); другие ошибки — провал.
    if err != nil && err.Error() != "sql: no rows in result set" {
        t.Fatalf("nodes.fqn missing: %v", err)
    }
}

func TestSaveFileNodesAndLazyResolve(t *testing.T) {
    s := newTestStore(t)
    ctx := context.Background()

    nodesA := []Node{
        {FQN: "ex/foo.A", ShortName: "A", Kind: "func", LineStart: 1, LineEnd: 3},
    }
    edgesA := []Edge{
        // Вызов из A в ещё-не-индексированный B (target_id будет NULL).
        {SourceFQN: "ex/foo.A", TargetFQN: "ex/foo.B", Relation: "calls"},
    }
    if err := s.SaveFileNodes(ctx, "foo.go", "h1", "go", "ex", "foo", nodesA, edgesA); err != nil {
        t.Fatalf("save A: %v", err)
    }

    var targetID *int64
    err := s.db.QueryRowContext(ctx,
        `SELECT target_id FROM edges WHERE target_fqn = ?`, "ex/foo.B").Scan(&targetID)
    if err != nil {
        t.Fatal(err)
    }
    if targetID != nil {
        t.Fatalf("target_id should be NULL, got %d", *targetID)
    }

    // Теперь индексируем файл с B — lazy-резолюция должна обновить edge.
    nodesB := []Node{
        {FQN: "ex/foo.B", ShortName: "B", Kind: "func", LineStart: 1, LineEnd: 3},
    }
    if err := s.SaveFileNodes(ctx, "bar.go", "h2", "go", "ex", "foo", nodesB, nil); err != nil {
        t.Fatalf("save B: %v", err)
    }

    err = s.db.QueryRowContext(ctx,
        `SELECT target_id FROM edges WHERE target_fqn = ?`, "ex/foo.B").Scan(&targetID)
    if err != nil {
        t.Fatal(err)
    }
    if targetID == nil {
        t.Fatal("target_id still NULL after lazy resolve")
    }
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/ckg/ -run TestMigrateToV2 -run TestSaveFileNodesAndLazyResolve -v`
Expected: PASS.

---

### Task 3.5: Тесты на Provider — Callers/Callees/Importers + ExploreSymbol по FQN/short_name

**Files:**
- Create: `internal/ckg/provider_test.go`

- [ ] **Step 1: Написать тесты**

Создать `internal/ckg/provider_test.go`:

```go
package ckg

import (
    "context"
    "strings"
    "testing"
)

func TestProviderCallersChain(t *testing.T) {
    s := newTestStore(t)
    ctx := context.Background()

    // A → B → C
    save := func(file, fqn string, edges []Edge) {
        nodes := []Node{{FQN: fqn, ShortName: lastSegment(fqn), Kind: "func", LineStart: 1, LineEnd: 3}}
        if err := s.SaveFileNodes(ctx, file, "h", "go", "ex", "ex", nodes, edges); err != nil {
            t.Fatal(err)
        }
    }
    save("c.go", "ex.C", nil)
    save("b.go", "ex.B", []Edge{{SourceFQN: "ex.B", TargetFQN: "ex.C", Relation: "calls"}})
    save("a.go", "ex.A", []Edge{{SourceFQN: "ex.A", TargetFQN: "ex.B", Relation: "calls"}})

    p := NewProvider(s, "/tmp")
    callers, err := p.Callers(ctx, "ex.C")
    if err != nil { t.Fatal(err) }
    if len(callers) != 1 || callers[0].FQN != "ex.B" {
        t.Fatalf("direct callers of C: got %+v, want [ex.B]", callers)
    }

    callers2, err := p.Callers(ctx, "ex.B")
    if err != nil { t.Fatal(err) }
    if len(callers2) != 1 || callers2[0].FQN != "ex.A" {
        t.Fatalf("direct callers of B: got %+v, want [ex.A]", callers2)
    }
}

func TestProviderImporters(t *testing.T) {
    s := newTestStore(t)
    ctx := context.Background()

    save := func(file, pkgFQN string, edges []Edge) {
        nodes := []Node{{FQN: pkgFQN, ShortName: lastSegment(pkgFQN), Kind: "package", LineStart: 1, LineEnd: 1}}
        if err := s.SaveFileNodes(ctx, file, "h", "go", "ex", "pkg", nodes, edges); err != nil {
            t.Fatal(err)
        }
    }
    save("auth.go", "ex/auth", nil)
    save("api.go", "ex/api", []Edge{{SourceFQN: "ex/api", TargetFQN: "ex/auth", Relation: "imports"}})
    save("svc.go", "ex/svc", []Edge{{SourceFQN: "ex/svc", TargetFQN: "ex/auth", Relation: "imports"}})

    p := NewProvider(s, "/tmp")
    imps, err := p.Importers(ctx, "ex/auth")
    if err != nil { t.Fatal(err) }
    if len(imps) != 2 {
        t.Fatalf("importers of ex/auth: got %v, want 2", imps)
    }
}

func TestExploreSymbolAmbiguousShortName(t *testing.T) {
    s := newTestStore(t)
    ctx := context.Background()

    // Два разных Run в разных пакетах — short_name = "Run".
    save := func(file, fqn string) {
        nodes := []Node{{FQN: fqn, ShortName: "Run", Kind: "func", LineStart: 1, LineEnd: 3}}
        if err := s.SaveFileNodes(ctx, file, "h", "go", "ex", "p", nodes, nil); err != nil {
            t.Fatal(err)
        }
    }
    save("a.go", "ex/a.Run")
    save("b.go", "ex/b.Run")

    p := NewProvider(s, "/tmp")
    out, err := p.ExploreSymbol(ctx, "Run")
    if err != nil { t.Fatal(err) }
    if !strings.Contains(out, "неоднозначен") {
        t.Fatalf("expected ambiguity message, got: %s", out)
    }
    if !strings.Contains(out, "ex/a.Run") || !strings.Contains(out, "ex/b.Run") {
        t.Fatalf("expected both FQNs listed, got: %s", out)
    }
}

func lastSegment(fqn string) string {
    if i := strings.LastIndex(fqn, "."); i >= 0 {
        return fqn[i+1:]
    }
    if i := strings.LastIndex(fqn, "/"); i >= 0 {
        return fqn[i+1:]
    }
    return fqn
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/ckg/ -run TestProviderCallersChain -run TestProviderImporters -run TestExploreSymbolAmbiguousShortName -v`
Expected: PASS.

---

### Task 3.6: Обновить `parser_test.go` и `orchestrator_test.go` под новую сигнатуру

**Files:**
- Modify: `internal/ckg/parser_test.go`
- Modify: `internal/ckg/orchestrator_test.go`

- [ ] **Step 1: Найти что сломалось**

Run: `go test ./internal/ckg/`
Expected: ошибки компиляции/тестов в указанных файлах из-за смены сигнатур `ParseFile` (теперь 4 параметра + 4 возврата) и полей `Node`/`Edge`.

- [ ] **Step 2: Адаптировать тесты**

В каждом сломанном тесте:
- Вызов `ParseFile(ctx, filePath)` → `ParseFile(ctx, "test.module", rootDir, filePath)`.
- Чтение `node.Name` → `node.ShortName` (или `node.FQN` для проверки квалификации).
- Чтение `node.Type` → `node.Kind`.
- `edge.SourceName/TargetName` → `edge.SourceFQN/TargetFQN`.
- Учесть, что теперь возвращается доп. результат `pkgName` (4-й).
- Учесть, что среди nodes теперь есть синтетический `kind="package"` — возможно, потребуется фильтр.

- [ ] **Step 3: Run all ckg tests**

Run: `go test ./internal/ckg/ -v`
Expected: все PASS.

---

### Task 3.7: Финальный sanity-check на самой Orchestra

- [ ] **Step 1: Удалить старую БД, прогнать индекс**

```bash
rm -f .orchestra/ckg.db
go build -o orchestra.exe ./cmd/orchestra
./orchestra.exe core --workspace-root . &
# в другом терминале — отправить tool.call с symbol_name="Run" и symbol_name="Agent.Run"
```

(Или проще — написать маленький скрипт, который инициализирует Runner и делает `ExploreCodebase`. Можно сделать unit-тест в `internal/tools/explore_codebase_test.go` который индексирует Orchestra-репо целиком, но это медленно — делайте локально, не в CI.)

- [ ] **Step 2: Проверить ручной запрос**

`ExploreSymbol("Run")` ожидаем: список с уточнением (несколько Run-функций в разных пакетах) с FQN.
`ExploreSymbol("github.com/orchestra/orchestra/internal/agent.Agent.Run")` ожидаем: точный snippet + callers + callees.
`Importers("github.com/orchestra/orchestra/internal/agent")` ожидаем: непустой список (внутри `internal/cli`, `internal/core` есть импорты).

- [ ] **Step 3: Полный CI**

Run: `go vet ./... && go test ./... && go test -race ./internal/ckg/ ./internal/tools/`
Expected: всё зелёное на Linux. На Windows — `go vet ./... && go test ./...` (без `-race`).

---

### Task 3.8: Commit (Коммит 3 — может быть атомарный 2+3, см. ниже)

- [ ] **Step 1: Commit**

Если идём по плану 2 коммитов (1 + слитый 2+3):

```bash
git add internal/ckg/ internal/tools/explore_codebase_test.go
git commit -m "feat(ckg): FQN, edges by node_id, imports, lazy resolve

- Schema v2: nodes.fqn (UNIQUE), nodes.short_name, edges.source_id (FK),
  edges.target_id (FK, nullable for external symbols), edges.target_fqn.
- Parser: GoFQN follows go-doc convention (importpath.Type.Method).
  Imports extracted as 'imports' edges between package-kind nodes.
- Store.SaveFileNodes: resolves target_id within transaction;
  lazy-resolves previously-NULL edges when matching FQN appears.
- Provider: ExploreSymbol disambiguates by FQN vs short_name;
  new methods Callers/Callees/Importers for graph traversal.
- UI: SELECTs adapted to new column names.
- Tests cover migration, lazy resolve, callers chain, importers, ambiguity."
```

- [ ] **Step 2: Sanity**

```bash
git status         # working tree clean
git log --oneline -3
go test ./...      # last green run
```

---

## Fallback на атомарный коммит 2+3

Если в процессе видно, что коммит 2 ломает компиляцию `ckg` пакета и не может быть зелёным без коммита 3 — это ожидаемо (в коммите 2 сменилась сигнатура `SaveFileNodes`). В этом случае:

- Не делаем промежуточный commit после Task 2.x.
- Все изменения коммитов 2 и 3 уходят одним «feat(ckg): FQN, edges by node_id, imports, lazy resolve».
- Коммит 1 остаётся отдельным.

Итого: **2 коммита вместо 3**. Это явно разрешено в спеке (§9, Q5).

---

## Self-Review

**1. Spec coverage:**
- §3 FQN-формат — Task 2.2 (`fqn.go`), Task 2.3 (тесты), Task 2.5 (использование в parser).
- §4 Schema v2 — Task 2.4 (DDL).
- §4.3 Imports as edges — Task 2.5 (синтетический package-node, import_spec query).
- §4.4 Lazy resolve — Task 3.1 (UPDATE подвисших).
- §5 Store lifecycle — Task 1.2 (открытие в NewRunner), Task 1.3 (использование в tool), Task 1.4 (Close), Task 1.5 (тесты).
- §6 Migration drop+recreate — Task 2.4 (`PRAGMA user_version=2` + DROP).
- §7 API изменения — Task 2.4 (Node/Edge), Task 3.2 (Provider методы).
- §8 DoD пункты 1-6 — Task 3.4 (миграция), Task 3.5 (callers chain, importers, ambiguity), Task 3.6 (старые тесты), Task 1.5 (single-open).

**2. Placeholder scan:** Проверено — конкретный код в каждом шаге, exact пути файлов, exact команды, ожидаемые результаты.

**3. Type consistency:**
- `Node` поля: `ID`, `FileID`, `FQN`, `ShortName`, `Kind`, `LineStart`, `LineEnd`, `Complexity` — используются одинаково в Task 2.4, 2.5, 3.1, 3.2, 3.4, 3.5.
- `Edge` поля: `SourceFQN`, `TargetFQN`, `Relation` — одинаково в Task 2.4, 2.5, 3.1, 3.5.
- `ParseFile` сигнатура: 4 параметра, 4 возврата — Task 2.5, 2.6, 3.6.
- `SaveFileNodes` сигнатура: 8 параметров — Task 2.6, 3.1, 3.4, 3.5.
- `Provider` методы: `ExploreSymbol/Callers/Callees/Importers` — Task 3.2, 3.5.

---

## Что после плана

1. Прогнать sanity на самой Orchestra (Task 3.7).
2. Обновить таблицу в roadmap §5: пометить под-проект 0 как ✅ DONE с датой и ссылкой на коммиты.
3. Открыть следующую сессию для **спека на под-проект 1** (полиглот: py + ts/js).
