# План на следующие сессии

## Последовательность фаз

```
Фаза 1 (2026-05-08):  Live-тесты + тестовое покрытие
Фаза 2 (после):       TUI — первый продуктовый слой, UX-проверка
Фаза 3 (после TUI):   VS Code extension
Фаза 4 (долгосрок):   Свой IDE
```

Логика: сначала убедиться что ядро работает надёжно на реальной модели,
потом строить пользовательский слой поверх проверенного фундамента.
TUI даст живой feedback на UX до того как он замёрзнет в VS Code extension.

---

# Фаза 1 — Live-тесты + покрытие (2026-05-08)

## Контекст

Сессия 2026-05-07: двойной аудит безопасности и корректности. Все найденные
проблемы исправлены и закоммичены. Roadmap A–G полностью закрыт.

## Что было исправлено (не повторять)

**Аудит-раунд 1:** `lsp_tools.go` path traversal, `circuit_breaker.go` off-by-one,
`mcp/client.go` Close hang, `config/config.go` 0644→0600, `rpc_handler.go` DisallowUnknownFields.

**Аудит-раунд 2:** `core/core.go` SessionApplyPending race, `ckg/ui.go` path traversal,
`resolver/external_patches.go` whitespace search + atoi overflow, `git/client.go` dead code,
`CLAUDE.md` описание resolver.

---

## Приоритет 1 — Live-тестирование (нет изменений кода, только проверка)

Эти фичи реализованы но **ни разу не проверялись с реальной моделью** qwen3.6-27b.

### 1.1 Субагенты (`task.spawn / wait / cancel / result`)
```bash
orchestra apply --apply "реши задачу X через несколько параллельных субагентов"
```
Проверить: задачи создаются, результаты возвращаются, отмена работает.

### 1.2 `orchestra apply --via-core`
```bash
orchestra apply --via-core --apply "..."
```
Проверить: инициализация, полный цикл, финальные патчи применяются.

### 1.3 Hooks (pre/post-tool)
Добавить простой хук в `.orchestra.yml` и убедиться что он вызывается.

### 1.4 `orchestra eval`
```bash
orchestra eval --model qwen3.6-27b
```
Проверить: `rename_func` и `add_func` кейсы проходят стабильно.

### 1.5 MCP bridge с реальным сервером
Подключить любой реальный MCP-сервер, проверить что инструменты видны модели.

---

## Приоритет 2 — Тестовое покрытие критических пакетов

Писать тесты, не добавлять функциональность.

### 2.1 `internal/ops` (0 тестов, КРИТИЧНО)
- UnmarshalJSON: нормализация `"type"` → `"op"`, неизвестный тип → error
- Position/Range: граничные значения (0-based, end-exclusive)
- Условие `FileHash`: пустое vs `"sha256:..."` — разный semantics

### 2.2 `internal/applier` (частичное покрытие)
- `atomicWriteFile()` — нет ни одного теста
- Backup: файл уже существует, директория не существует
- Несколько `replace_range` на одном файле — правильный порядок применения

### 2.3 `internal/ckg/ui.go` (0 тестов)
- `/api/source` — path traversal заблокирован (проверить fix из этой сессии)
- Выход за границы `start`/`end` строк не паникует

---

# Фаза 2 — TUI (после Фазы 1)

**Цель:** первый продуктовый слой. Пользоваться ядром как пользователь,
находить UX-проблемы до VS Code extension.

**Технология:** Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea)
(тот же язык, тот же репо, subprocess `orchestra core` по stdio JSON-RPC)

**Минимальный MVP:**
- Чат-панель со streaming токенами от агента
- Список шагов (tool calls) в реальном времени
- Diff-вью pending ops перед apply (y/n подтверждение)
- Статус-бар: модель, шаги, токены

**Что проверяем через TUI (UX-вопросы):**
- Понятно ли какие файлы изменятся до apply?
- Нужен ли history-браузер сессий?
- Удобен ли вывод tool call / результатов?
- Нужна ли CKG-вьюха прямо в терминале?

Ответы на эти вопросы определят дизайн VS Code extension.

---

# Фаза 3 — VS Code extension (после TUI)

- TypeScript + VS Code API
- Транспорт: HTTP-режим (`orchestra core --http`, уже готов)
- Inline diff, sidebar чат, CodeLens через LSP-тул (уже в ядре)

---

# Фаза 4 — Свой IDE (долгосрок)

`orchestra core` уже ведёт себя как Language Server.
CKG + LSP-тул = инфраструктура IDE встроена в ядро.
Нужна только оболочка.

---

## Состояние репо перед Фазой 1

- Ветка: `master`
- `go test ./...` ✅
- `go vet ./...` ✅
