# ТЗ: Semantic Dry-Run (AST-Gate + Staging⇄LSP + Real-Time Diagnostics)

**Проект:** Orchestra  
**Версия:** 1.0 (2026-05)  
**Статус:** к реализации  
**Связанные документы:**
- `docs/tools-overview.md` §7 — автодиагностика после edit (✅ только disk apply)
- `docs/CHANGELOG.md` — Tools v5, `SyncAndDiagnose`, auto-diagnostics
- `docs/commands-and-modes.md` §3.3 — LSP feedback loop vs OpenCode
- `docs/architecture-uml.md` §8.1 — архитектурный контраст (LSP loop = gap)
- `docs/superpowers/plans/2026-05-19-post-audit-refactor.md` — H2/H5/H7 (diagnostics)
- Skills: `docs/examples/skills/error_handler.md`, `type_fixer.md`, `lint_fixer.md`

---

## 0. Контекст: «killer feature» уже частично есть

### Что работает сегодня (real-time **после записи на диск**)

```
edit/write (--apply) → applier → disk
                              → lsp.SyncAndDiagnose(content)
                              → FSEditResponse.Diagnostics[]
                              → agent: extractLSPErrors → inject LSP_ERRORS в history
                              → DiagTracker: блокирует cosmetic re-write с тем же fingerprint
```

**Код:**
| Слой | Файл | Что делает |
|------|------|------------|
| Tools | `internal/tools/edit.go`, `write.go` | `SyncAndDiagnose` после успешного apply |
| LSP | `internal/lsp/manager.go` | `DidOpen`/`DidChange`, `WaitForUpdate`, cache |
| Agent | `internal/agent/tool_dispatch.go` | inject `LSP_ERRORS` hint в history |
| Guard | `internal/agent/guard/diag_tracker.go` | streak одинаковых ошибок → эскалация |
| Format | `internal/agent/format/format.go` | `ExtractLSPErrors` (cap 20 errors) |

### Что **не** работает (дыра killer feature)

| Сценарий | Сейчас |
|----------|--------|
| `edit`/`write` в **dry-run** (staging) | Patches staged, **нет** LSP, **нет** diagnostics |
| `lsp.diagnostics` на staged файл | LSP читает **диск** (`ensureOpen` → `ReadFile`) |
| Real-time fix **до** `--apply` | Модель не видит compile/type errors на будущем коде |

**Вывод:** killer feature = **закрыть dry-run path**. Фазы 1–3 этого ТЗ доводят поведение до OpenCode/golden TZ без ломки hash/ops/staging.

---

## 1. Цели и non-goals

### Цели
1. **AST-Gate** — синтаксически битый код не попадает в staging.
2. **Staging ⇄ LSP** — language server видит overlay до `--apply`.
3. **Soft validation loop** — `edit`/`write` в dry-run возвращают LSP errors в tool response + agent hint.
4. **Lazy LSP + TTL** — полиглот без eager RAM на старте.

### Non-goals (эта волна)
- Полный VFS как SoT для grep/CKG/bash
- Enforced agent loop (жёсткая state machine)
- RAM watchdog (1.5GB) — опционально после Ф4
- `textDocument/typeDefinition` tool
- Bump `ToolsVersion` / `ProtocolVersion` без необходимости

---

## 2. Фаза 1 — AST-Gate

### 2.1 Требование
После применения search/replace или write content к тексту файла, **до** `stageFile`, выполнить tree-sitter parse и отклонить патч при синтаксической ошибке.

### 2.2 Точки интеграции
| Место | Файл |
|-------|------|
| Dry-run edit | `internal/tools/edit.go` — после `ApplySearchReplace`, перед `stageFile` |
| Dry-run write | `internal/tools/write.go` — перед `stageFile` |
| Staging patch replay | `internal/tools/staging.go` — `ApplyPatchesToStaged` после merge content |
| Agent final patches | `internal/tools/runner.go` — `ApplyPatchesToStaged` (optional, same gate) |

### 2.3 API (новый пакет или `internal/ckg`)

```go
// ValidateSyntax checks parsed tree for ERROR/missing nodes.
// Returns nil if extension has no grammar (skip gate, log at debug).
func ValidateSyntax(path string, content []byte) error
```

**Реализация:** переиспользовать `internal/ckg.ParseFile` / grammar map из `parser.go`. Обход AST на `ERROR`, `MISSING` nodes; сообщение вида:

```text
SYNTAX_ERROR path=internal/foo.go line=42
missing '}' — fix your patch before staging.
```

### 2.4 Контракт ошибки
- Новый protocol code: `SyntaxError` (или `InvalidLLMOutput` с `data.path`, `data.line`) — **решить при impl**; предпочтительно отдельный code для агента.
- Tool возвращает error → agent loop продолжается (recoverable), **не** stage.

### 2.5 Конфиг (optional)
```yaml
agent:
  ast_gate: true   # default true when CKG grammar exists
```

### 2.6 Тесты
- Go: `func foo() {` без `}` → reject
- Valid Go/TS snippet → pass
- Extension без grammar (`.md`) → pass (skip)
- `staging_test.go`: broken patch не попадает в `StagedOps`

### 2.7 Критерий приёмки
- [x] 0 синтаксически invalid файлов в staging overlay (для поддерживаемых языков CKG)
- [x] `go test ./internal/tools/... ./internal/ckg/...` green

**Реализовано (2026-05):** `internal/ckg/syntax.go` (`ValidateSyntax`), `protocol.SyntaxError`, gate в `Runner.stageFile` (default on, `DisableASTGate` opt-out).

---

## 3. Фаза 2 — Staging ⇄ LSP bridge

### 3.1 Требование
LSP document state = **effective content** (disk ∪ staging overlay). `lsp.*` tools и post-edit sync используют staged text.

### 3.2 Новые/изменённые API

```go
// internal/lsp/manager.go

// SyncStaged pushes overlay content to LSP (didOpen or didChange full sync).
// version bumped per URI in client docVersions map.
func (m *Manager) SyncStaged(ctx context.Context, relPath, content string) error

// EffectiveContent returns staged content if runner has overlay, else disk.
// Lives on tools.Runner, used by Manager via callback or interface.
type ContentProvider interface {
    EffectiveContent(relPath string) (content string, fromStaging bool, ok bool)
}
```

### 3.3 Протокол (per file)

| Событие | LSP notification |
|---------|------------------|
| Первый touch файла (read/edit/write/lsp.*) | `didOpen` с effective content |
| Успешный stage после AST-Gate | `didChange` full document, version++ |
| `--apply` успешен | optional: keep open OR `didClose` — **default: keep open** |
| Clear staging / rollback | `didChange` с disk content, version++ |
| fs.delete staged file | `didClose` |

**Sync mode:** Full document (проще). Incremental — later.

### 3.4 Изменения по файлам
| Файл | Изменение |
|------|-----------|
| `internal/tools/staging.go` | после `stageFile` → `lspManager.SyncStaged` |
| `internal/tools/edit.go` | dry-run path: sync после stage |
| `internal/tools/write.go` | dry-run path: sync после stage |
| `internal/lsp/manager.go` | `ensureOpen` читает effective content, не только disk |
| `internal/tools/runner.go` | wire `ContentProvider`, pass to LSP manager |

### 3.5 Тесты
- Integration с `internal/lsp/lsptest`: stage edit → `GetDiagnostics` видит intentional type error **без** disk write
- `staging_test.go` + mock LSP manager

### 3.6 Критерий приёмки
- [x] После dry-run edit с type error, `lsp.diagnostics` возвращает error **до** `--apply` (via `SyncStaged` + `ContentProvider`; full diag in Ф3)
- [x] Disk file unchanged until apply

**Статус:** ✅ реализовано (`ContentProvider`, `SyncStaged`, `fileContent`, `EffectiveContent`, `ListStagedPaths`, тесты `staging_lsp_test.go`, `manager_test.go`).

---

## 4. Фаза 3 — Soft validation loop (real-time fix)

### 4.1 Требование
Ответ `edit`/`write` в dry-run включает LSP diagnostics так же, как disk path сегодня.

### 4.2 Изменения
| Файл | Изменение |
|------|-----------|
| `internal/tools/edit.go` | dry-run return: после `SyncStaged` → `GetDiagnostics` / `SyncAndDiagnose` |
| `internal/tools/write.go` | аналогично |
| `internal/agent/tool_dispatch.go` | без изменений логики — уже inject на `write`/`edit` + diagnostics в JSON |

**Формат ответа (existing shape):**
```json
{
  "path": "internal/foo.go",
  "file_hash": "abc...",
  "diagnostics": [
    {"severity": "error", "message": "undefined: bar", "start_line": 10, "start_col": 4}
  ]
}
```

**Текст для модели (если errors):** agent hint `LSP_ERRORS — ...` (уже есть).

### 4.3 Timeout
Использовать `lsp.diagnostics_timeout_ms` из config (default 5000ms). При timeout — вернуть `"diagnostics": []` + `"diagnostics_pending": true` (optional field).

### 4.4 Известные баги (fix opportunistically)
- H5: stale diagnostics after version bump — filter by `publishDiagnostics.version`
- См. `docs/superpowers/plans/2026-05-19-post-audit-refactor.md` H5

### 4.5 Критерий приёмки
- [x] `agent_test.go` LSP hint tests pass для **dry-run** path (`TestAgent_Run_LSPErrors_HintInjected_DryRun`, `...Edit_DryRun`)
- [x] E2E: `tests/e2e_agent/e2e_dryrun_lsp_test.go` (bad edit → hint → fix → FSApplyOps)
- [x] TUI показывает diagnostics в tool block (`diagnostics` в agent/event → inline + Ctrl+T expand)

**Статус:** ✅ dry-run `edit`/`write` возвращают `diagnostics` через `SyncAndDiagnose` (как disk path).

**Следующий уровень:** [planner-worker.md](./planner-worker.md) — Lead делегирует Workers; validation loop **внутри** Worker subagent.

---

## 5. Фаза 4 — Lazy LSP + TTL

### 5.1 Lazy start
- **Убрать** eager spawn всех servers в `NewManager`
- `serverForPath(ext)` → `sync.Once` per server config → spawn on first request

### 5.2 TTL (default 5 min)
```go
type serverState struct {
    client       *Client
    lastActivity time.Time
    mu           sync.Mutex
}
```
- Ticker goroutine (1/min): idle > TTL → `shutdown` → `exit` → kill process
- На следующий request → lazy restart

### 5.3 Re-open после wake/restart
При spawn/restart server:
1. Query runner: `ListStagedPaths()` 
2. For each path matching server extensions: `didOpen` effective content

### 5.4 Конфиг
```yaml
lsp:
  lazy_start: true          # default true after phase 4
  idle_ttl_seconds: 300     # 0 = disable TTL
```

### 5.5 Критерий приёмки
- [x] Core start без configured LSP не spawns processes (lazy_start default true)
- [x] After TTL sleep, next `lsp.diagnostics` restarts server + re-opens staged files
- [x] `go test ./internal/lsp/...` green

**Статус:** ✅ реализовано (`lazy_start`, `idle_ttl_seconds`, `ensureClient`, `idleWatcher`, `reopenStaged`, `StagedPathsProvider`).

---

## 6. Порядок коммитов (рекомендуемый)

| # | Commit | Фаза | Зависимости |
|---|--------|------|-------------|
| 1 | `feat(ckg): ValidateSyntax AST gate` | 1 | — |
| 2 | `feat(tools): reject invalid syntax before staging` | 1 | #1 |
| 3 | `feat(lsp): lazy server start` | 4 partial | — |
| 4 | `feat(lsp): SyncStaged + effective content provider` | 2 | — |
| 5 | `feat(tools): LSP sync on dry-run edit/write` | 2 | #4 |
| 6 | `feat(tools): diagnostics in dry-run edit/write response` | 3 | #5 |
| 7 | `feat(lsp): TTL idle shutdown + re-open staged on wake` | 4 | #4,#5 |
| 8 | `test(e2e): dry-run edit → LSP error → fix → apply` | 3 | #6 | ✅ |
| 9 | `docs: update tools-overview + architecture-uml LSP loop` | — | all | ✅ |

---

## 7. Метрики успеха

| Метрика | Baseline | Target после Ф1–3 |
|---------|----------|---------------------|
| Invalid syntax in staging | possible | ~0 (CKG langs) |
| LSP feedback in dry-run | none | same as disk apply |
| Steps to successful `--apply` (local LLM) | baseline TBD | −20–40% |
| RAM at idle (3 LSP servers configured) | all spawned | 0 until first use |

---

## 8. Риски

| Рisk | Mitigation |
|------|------------|
| Full sync didChange slow on large files | cap file size for LSP sync; skip > N MB |
| AST gate false positives (generated code) | allowlist paths / `ast_gate: false` |
| Stale diagnostics (H5) | version filter in diagnostics cache |
| Circular import tools↔lsp | `ContentProvider` interface in `internal/lsp` or small `internal/vfs` |

---

## 9. Out of scope / Phase 5+

- VFS as SoT for grep/CKG
- Enforced validation state machine in agent.Run
- RAM watchdog queue
- `textDocument/didSave` semantics
- Polyglot default configs (pyright, tsserver) in `orchestra init`
