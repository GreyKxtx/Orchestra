# Под-проект 0: Доводка Go-CKG до точного — Design Spec

**Дата:** 2026-05-01
**Статус:** APPROVED — готов к implementation plan
**Roadmap:** см. `2026-05-01-ckg-runtime-roadmap.md` §5, под-проект 0

---

## 1. Цель и scope

Под-проект 0 — это **только Go**. Сделать существующий CKG алгоритмически точным на самом репо Orchestra. Не трогать другие языки (это под-проект 1), не трогать рантайм (под-проект 2), не трогать векторный поиск (под-проект 4).

**Что в scope:**

1. FQN-имена узлов (полная квалификация по importpath).
2. Edges с FK на `node_id` (а не строковые имена) — обходимый граф.
3. Новый relation `imports` (file → file / package → package).
4. Один долгоживущий Store на жизнь Runner (вместо open/close на каждый tool-call).
5. Миграция схемы БД (`.orchestra/ckg.db` локальный — допустим drop/recreate).

**Что вне scope (явно отложено):**

- Полиглот (под-проект 1).
- Counting `complexity` (туда же — это побочный эффект перевода парсера на полноценный AST-walk per-language).
- Edges типа `implements` / `references` для интерфейсов и переменных. Реализуем только в той части, где это бесплатно (для интерфейсов в Go это сложный type-resolution — отложим до когда будет нормальный type-checker; либо сделаем `implements` через эвристику в под-проекте 1).
- UI (`internal/ckg/ui*` и команда `orchestra ckg-ui`) — продолжает работать на новой схеме, но сам код UI не переписываем.

---

## 2. Текущее состояние (краткая выжимка)

Подробности в `2026-05-01-ckg-runtime-roadmap.md` §3. Релевантное:

- `internal/ckg/store.go:16-30` — `Node{ID, FileID, Name, Type, LineStart, LineEnd, Complexity}` и `Edge{SourceName, TargetName, Relation}` (строковые имена).
- `internal/ckg/parser.go:200-211` — извлекаются только `calls`-edges; receiver-резолюция для Go-методов есть (`Type.Method`), но без import-path префикса.
- `internal/tools/explore_codebase.go:21-49` — `NewStore` на каждый вызов tool-а.
- `internal/cli/ckg_ui.go:25-37` — UI открывает свой Store независимо. **Нельзя сломать.**

---

## 3. Формат FQN для Go

### 3.1. Канонический формат

Использовать стандартное Go-convention `go doc`:

```
<importpath>.<Type>.<Method>      // method on type
<importpath>.<Type>               // type itself
<importpath>.<Func>               // top-level function
<importpath>.<Var>                // package-level var/const (если индексируем)
```

**Где `importpath` = `<module-path>/<relative-package-path>`.**

Примеры на самой Orchestra:

| Символ | FQN |
|---|---|
| top-level func `Run` в `internal/agent` | `github.com/orchestra/orchestra/internal/agent.Run` |
| method `Agent.Run` в `internal/agent` | `github.com/orchestra/orchestra/internal/agent.Agent.Run` |
| type `Agent` в `internal/agent` | `github.com/orchestra/orchestra/internal/agent.Agent` |
| top-level func в `cmd/orchestra/main.go` | `github.com/orchestra/orchestra/cmd/orchestra.main` |

### 3.2. Парсинг module-path

- Читать `go.mod` из `workspace_root` один раз при инициализации Store. Если файла нет — workspace не Go-модуль, fallback на относительный путь как importpath (для не-Go проектов это вообще не нужно — тогда индексер по Go просто отключается).
- Для подмодулей (workspace с несколькими `go.mod`) — MVP-ограничение: индексируем только корневой модуль, остальные пропускаем с warning. Полноценный multi-module — в более поздней итерации.

### 3.3. Open questions / edge cases

| Случай | Решение в MVP | Альтернатива на будущее |
|---|---|---|
| Generic types (`Container[T].Add`) | FQN без параметров типа: `pkg.Container.Add` | Хранить параметры типа отдельной колонкой |
| Pointer vs value receiver (`*Agent` vs `Agent`) | FQN одинаковый: `pkg.Agent.Run` | — (не нужна различимость для целей CKG) |
| Anonymous functions / closures | Не индексировать в MVP | FQN типа `pkg.outer.func1` (как делает компилятор) |
| Build-tags (`//go:build linux`) | Индексировать все файлы независимо | Хранить set of tags per-file, фильтровать при запросе |
| `internal/` пакеты | FQN полный, как обычно | — |
| `vendor/` | Не индексировать (уже в excludes сканера) | — |

### 3.4. Альтернативы, которые мы НЕ выбрали (и почему)

- **Только относительный путь** (`internal/agent.Agent.Run`): короче, но не уникален при индексировании нескольких модулей. Также не совпадает с тем, что выводит `go doc` / IDE — лишний mental overhead.
- **Hash-based ID** (`sha256(file:line:name)`): уникально, но человеконечитаемо. Для tool output это плохо.
- **Slash-separated** (`internal/agent/Agent/Run`): экзотика, не совпадает ни с одним стандартом Go.

---

## 4. Схема БД v2

### 4.1. Принципы

- Edges по `node_id` (FK + индекс).
- Внешние символы (вызовы в stdlib, в чужие модули) допускаются — `target_id NULL`, но `target_fqn` обязателен. Это позволяет:
  - Видеть вызовы `fmt.Printf` без необходимости индексировать stdlib.
  - **Lazy-резолвить**: когда файл с целевым символом появится в индексе, можно за один проход обновить `target_id` для всех висящих edges.
- FQN — основной идентификатор символа. Поиск по FQN — точный (UNIQUE INDEX). Поиск по short-name — отдельный индекс для backwards-compatibility / fuzzy fallback.

### 4.2. DDL (предлагаемая)

```sql
-- Files: без изменений по структуре, добавлен module_path для Go
CREATE TABLE files (
  id          INTEGER PRIMARY KEY,
  path        TEXT UNIQUE NOT NULL,
  hash        TEXT NOT NULL,
  language    TEXT NOT NULL,
  module_path TEXT,                       -- для Go: распаршенный go.mod
  package     TEXT,                       -- для Go: имя пакета
  updated_at  DATETIME NOT NULL
);

-- Nodes: добавлен fqn (UNIQUE), убран отдельный name (вычисляется из fqn)
CREATE TABLE nodes (
  id          INTEGER PRIMARY KEY,
  file_id     INTEGER NOT NULL,
  fqn         TEXT UNIQUE NOT NULL,        -- 'github.com/foo/bar/pkg.Type.Method'
  short_name  TEXT NOT NULL,               -- 'Method' (для fuzzy search)
  kind        TEXT NOT NULL,               -- 'func', 'method', 'struct', 'interface', 'type'
  line_start  INTEGER NOT NULL,
  line_end    INTEGER NOT NULL,
  complexity  INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY(file_id) REFERENCES files(id) ON DELETE CASCADE
);

CREATE INDEX idx_nodes_fqn        ON nodes(fqn);
CREATE INDEX idx_nodes_short_name ON nodes(short_name);
CREATE INDEX idx_nodes_file_id    ON nodes(file_id);

-- Edges: source_id обязательный FK, target — либо id (внутренний), либо только fqn (внешний)
CREATE TABLE edges (
  id         INTEGER PRIMARY KEY,
  source_id  INTEGER NOT NULL,
  target_id  INTEGER,                      -- NULL если target не индексирован
  target_fqn TEXT NOT NULL,                -- всегда заполнен
  relation   TEXT NOT NULL,                -- 'calls', 'imports', 'instantiates'
  FOREIGN KEY(source_id) REFERENCES nodes(id) ON DELETE CASCADE,
  FOREIGN KEY(target_id) REFERENCES nodes(id) ON DELETE SET NULL
);

CREATE INDEX idx_edges_source_id  ON edges(source_id);
CREATE INDEX idx_edges_target_id  ON edges(target_id);
CREATE INDEX idx_edges_target_fqn ON edges(target_fqn);
CREATE UNIQUE INDEX idx_edges_unique ON edges(source_id, target_fqn, relation);

-- Версия схемы
PRAGMA user_version = 2;
```

### 4.3. Imports как edges

`relation = 'imports'`:

- `source_id` = специальный node типа `kind = 'package'` для пакета-импортёра.
- `target_fqn` = importpath импортируемого пакета (`github.com/foo/bar/baz`).
- `target_id` указывает на package-node, если такой пакет проиндексирован.

То есть для каждого пакета у нас будет один node типа `package`, и imports — это edges между такими package-nodes. Это даёт ровно ту фичу, которую ты хотел: «кто импортирует `internal/agent`» — это `SELECT source_id FROM edges WHERE target_fqn = '...internal/agent' AND relation='imports'`.

**Альтернатива:** отдельная таблица `package_imports`. Минус — две системы, нельзя делать BFS «в одну сторону» (например, «кто транзитивно зависит от пакета X через любые edges»). Я склоняюсь к единой таблице.

### 4.4. Resolver edges (lazy)

При `SaveFileNodes` для нового файла:
1. Удалить старые nodes/edges этого файла (CASCADE).
2. Insert новых nodes (получить их id).
3. Insert edges с `target_id = (SELECT id FROM nodes WHERE fqn = ?)`. Если найдено — заполнено; если нет — NULL.
4. Дополнительно: **обновить ранее висевшие edges**, чьи `target_fqn` совпадают с FQN новых nodes (`UPDATE edges SET target_id = ? WHERE target_fqn = ? AND target_id IS NULL`).

Шаг 4 — это и есть lazy-резолюция. Цена — один UPDATE на FQN, при инкрементальном изменении одного файла дешёво.

### 4.5. Альтернативы, которые мы НЕ выбрали

- **Только intra-graph edges** (без внешних): теряем call-graph до stdlib и сторонних библиотек. Полезно знать, что «эта функция вызывает `os.Exit`».
- **Промежуточная таблица symbols (id, fqn, indexed bool)**: чище семантически, но overkill для MVP. Если позже понадобится хранить мета-инфо о внешних символах (например, версию библиотеки) — добавим тогда.

---

## 5. Жизненный цикл Store

### 5.1. Решение

Store — **обязательный** член `tools.Runner`, открывается в `NewRunner`, закрывается в новом методе `Runner.Close()`. Lazy-init не нужен: открытие SQLite-handle стоит миллисекунды, оверхеда нет.

```go
// internal/tools/tools.go
type Runner struct {
    workspaceRoot string
    excludeDirs   []string
    execTimeout     time.Duration
    execOutputLimit int
    mcpCaller MCPCaller

    ckgStore    *ckg.Store     // <-- новое
    ckgProvider *ckg.Provider  // <-- удобный фасад
}

func NewRunner(workspaceRoot string, opts RunnerOptions) (*Runner, error) {
    // ... как было ...
    dbPath := filepath.Join(rootAbs, ".orchestra", "ckg.db")
    if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil { return nil, err }
    store, err := ckg.NewStore("file:" + dbPath + "?cache=shared")
    if err != nil { return nil, err }
    r := &Runner{ /* ... */, ckgStore: store, ckgProvider: ckg.NewProvider(store, rootAbs) }
    return r, nil
}

func (r *Runner) Close() error {
    if r.ckgStore != nil { return r.ckgStore.Close() }
    return nil
}
```

### 5.2. Кто вызывает Close()

Сейчас Runner создаётся в `internal/core/core.go` и живёт всё время процесса `orchestra core`. Нужно:

1. Добавить `Core.Close()`, который зовёт `Runner.Close()`.
2. В `cmd/orchestra` при выходе вызывать `core.Close()` (через defer в command или через signal handler).

Это маленькое расширение API Core, но **аддитивное** — никто его пока не зовёт, ничего не ломает.

### 5.3. Конкуренция с `ckg-ui` командой

`internal/cli/ckg_ui.go` запускает отдельный процесс `orchestra ckg-ui`, который держит свой handle к той же БД. Это два процесса, не два хендла в одном процессе. SQLite по умолчанию справляется через locking, плюс `cache=shared` уже стоит. **Дополнительных мер не требуется** в MVP.

Если в будущем будет нужно одновременно писать из core и читать из UI без блокировок — переключим UI на `mode=ro&_pragma=query_only(true)`. Сейчас не критично.

### 5.4. Альтернативы, которые мы НЕ выбрали

- **Lazy init с `sync.Once`**: spare microoptimization, добавляет код. Не оправдан.
- **Внешний `*ckg.Provider` в `RunnerOptions`**: чище разделение, но усложняет создание Runner на стороне CLI/Core. Если в будущем Vector/Runtime потребуют похожих провайдеров — пересмотрим (возможно, появится `Toolchain` структура, агрегирующая всё). Для одного CKG — overkill.

---

## 6. Миграция

`.orchestra/ckg.db` — локальный, гитигнор, не критичный.

**Стратегия:** проверка `PRAGMA user_version` в `Store.migrate()`. Если ниже текущей (2) — `DROP TABLE` для всех таблиц + создание новых + `PRAGMA user_version = 2`. На следующем `UpdateGraph` всё восстановится за пару секунд (incremental scan + парсинг ~50-200 файлов на средний Go-проект).

```go
func (s *Store) migrate() error {
    var version int
    s.db.QueryRow("PRAGMA user_version").Scan(&version)
    if version < 2 {
        // Drop all known tables (если они есть), затем создать v2.
        ...
    }
    return nil
}
```

Никакого alembic/golang-migrate. Когда (если) понадобится — добавим.

---

## 7. API изменения

### 7.1. `internal/ckg/store.go`

- `Node` теряет поле `Name` (заменено на `FQN` + `ShortName`). Поле `Type` переименовать в `Kind` — конфликтует со словом «type» в SQL и в Go, делает читаемее.
- `Edge` теряет `SourceName/TargetName` — добавляются `SourceID int64`, `TargetID *int64` (nullable), `TargetFQN string`.
- `SaveFileNodes` сигнатура изменится — она принимает edges, в которых ещё не известен `source_id`. Резолвим внутри транзакции (как сейчас): сначала insert nodes (получаем IDs), потом матчим edges по source-FQN, заполняем `source_id`, делаем insert.

### 7.2. `internal/ckg/provider.go`

- `ExploreSymbol(ctx, name)` — поменять контракт:
  - Если `name` похож на FQN (содержит `/` или > 1 точки) — точный поиск по FQN.
  - Иначе — поиск по `short_name`. Если ровно один — выдать. Если несколько — выдать список FQN с уточняющим вопросом-ответом для модели.
- Добавить новые методы:
  - `Callers(ctx, fqn) ([]Node, error)` — кто вызывает.
  - `Callees(ctx, fqn) ([]Node, error)` — кого вызывает.
  - `Importers(ctx, packageFQN) ([]string, error)` — какие пакеты импортируют этот.

### 7.3. `internal/tools/explore_codebase.go`

- Убрать открытие Store на каждый вызов — использовать `r.ckgProvider`.
- `UpdateGraph` всё ещё вызывается перед запросом для свежести (он incremental, дёшево).

### 7.4. UI (`internal/ckg/ui*`)

- Если UI читает `Node.Name` или `Edge.SourceName/TargetName` — поправить на `FQN/ShortName` и `SourceID/TargetFQN`. Это локальная правка в `ui.go` / `ui/`.

---

## 8. Тесты (Definition of Done)

Из roadmap §5, под-проект 0 — повторяю критерии:

1. Запрос `ExploreSymbol("Run")` на самой Orchestra возвращает только целевой узел с FQN формата `<module-path>/<pkg>.<Type>.<Method>`. Если коллизия по `short_name` — список FQN с предложением уточнить.
2. Граф проходится BFS/DFS по `node_id`, не по строкам. Тест: построить chain из 3 функций A→B→C, запросить «всех кто транзитивно вызывает C», получить {A, B}.
3. Запрос «кто импортирует пакет `internal/agent`» отрабатывает корректно. Тест: после индексации Orchestra-репо, `Importers("github.com/orchestra/orchestra/internal/agent")` возвращает непустой список с `internal/cli`, `internal/core` и т.д.
4. Один Store живёт на весь жизненный цикл Runner. Тест: Mock-репо, 100 последовательных вызовов `ExploreCodebase`, проверить что `Store.NewStore` вызывается ровно 1 раз.
5. `go test ./internal/ckg/... ./internal/tools/...` зелёный на Linux и Windows.
6. Lazy-резолюция: добавить файл с символом, на который висели NULL-target edges, проверить что после `UpdateGraph` они стали non-NULL.

Тесты пишутся параллельно с реализацией (в отдельный шаг плана — **after** functionality, как ты предпочитаешь по memory).

---

## 9. Принятые решения (rationale)

Решения по 5 ключевым вопросам зафиксированы 2026-05-01.

### Q1 → Go-convention `<importpath>.<Type>.<Method>`

**Утверждено.** Не воюем с экосистемой. Когда агент или человек читает контекст, он должен видеть ровно то же, что выдаст `go doc` или подсказка IDE. Это снижает галлюцинации LLM, потому что модели обучались именно на стандартном Go-форматировании.

### Q2 → Edges с nullable `target_id` (подход B)

**Утверждено.** Потерять знания о вызовах `os.Exit`, `log.Fatal`, `sql.Query` и т.п. — выстрелить себе в ногу при дебаге. Гибридная схема (FK на `target_id` + обязательный `target_fqn`) решает проблему «висячих» ссылок без раздувания БД парсингом всей stdlib.

### Q3 → Store — обязательный член Runner

**Утверждено.** Открытие SQLite с `cache=shared` стоит копейки. Явный `NewRunner` → `Close()` делает управление ресурсами предсказуемым. `sync.Once` + lazy-init только усложнит чтение кода.

### Q4 → Миграция drop+recreate

**Утверждено.** `.orchestra/ckg.db` — локальный кэш, гитигнор. Писать `ALTER TABLE` для него — пустая трата времени. При смене схемы дропаем таблицы и перестраиваем за пару секунд incremental scan.

### Q5 → Стратегия коммитов: 2-3 логических шага, fallback — один атомарный

**Утверждено.** Дробить на 4 полностью независимых коммита болезненно: изменение схемы и парсера тесно связаны (без новой схемы парсер не компилируется и наоборот). Целевая разбивка:

1. **Подготовка:** `Runner.Close()` lifecycle + Drop/Recreate в `Store.migrate()` через `PRAGMA user_version`.
2. **Ядро:** новая DDL (FQN, edges с FK + nullable target_id, imports как edges) + парсер на FQN + извлечение imports.
3. **Обвязка:** `Store.SaveFileNodes` под новую схему + `Provider` (BFS/DFS по `node_id`, новые методы `Callers/Callees/Importers`) + `explore_codebase` без open/close per-call + правки UI.

Каждый шаг должен оставлять `go vet ./...` и `go test ./internal/ckg/... ./internal/tools/...` зелёным. Если в процессе окажется, что шаги слишком переплетены и шаг 2 ломает компиляцию без шага 3 — мерджим 2+3 в один атомарный коммит. Это нормально для MVP-рефакторинга.

---

## 10. Что НЕ в этом спеке (важно зафиксировать)

- **Implementation plan** (последовательность задач, файл-за-файлом). Следующий шаг — отдельный документ через `writing-plans` skill.
- **Конкретные сигнатуры Go-кода** для каждого метода — даны эскизные, точные пишем на этапе плана/реализации.
- **Бенчмарки производительности** — добавим в плане, не в спеке.

---

**Следующий шаг:** writing-plans skill → детальная пошаговка реализации.
