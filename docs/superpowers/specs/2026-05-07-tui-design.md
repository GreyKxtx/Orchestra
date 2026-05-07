# TUI для Orchestra — дизайн

**Дата:** 2026-05-07
**Статус:** дизайн утверждён, готов к плану реализации
**Контекст:** Этап 1 продуктового roadmap (TUI → VS Code ext → desktop) поверх готового `orchestra core` (JSON-RPC stdio).

## Постановка

Нужен интерактивный терминальный клиент `orchestra tui`, который:
- по UX **близок к Claude Code** (одна колонка, лента сверху, промпт снизу, без боковых панелей);
- заимствует **input и визуализацию процесса из OpenCode** (slash-команды, @-mention, разворачиваемые tool-блоки, footer hints, индикатор модели);
- общается с ядром через **JSON-RPC stdio** (subprocess), а не через in-process линковку — чистый контракт, та же транспортная модель что у будущего VS Code extension.

OpenCode-овский TUI взять и переделать **нельзя**: текущий форк (`_opencode/packages/opencode/src/cli/cmd/tui/`) написан на TypeScript + Bun + OpenTUI/Solid, тесно сцеплен с их сервером и шиной событий. Используем его как UX-референс, реализуем самостоятельно на Go + Bubble Tea.

## Стек

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — Elm-architecture event loop
- [Lipgloss](https://github.com/charmbracelet/lipgloss) — стили
- [Bubbles](https://github.com/charmbracelet/bubbles) — `textarea`, `viewport`, `list`, `spinner`
- [sahilm/fuzzy](https://github.com/sahilm/fuzzy) — fuzzy-поиск для @-mention
- [teatest](https://github.com/charmbracelet/x/exp/teatest) — snapshot-тесты

## Расположение в репо

```
ui/                            ← зонтичная папка для всех клиентов ядра
  README.md                    ← обзор: какие клиенты есть, как добавить новый
  tui/                         ← Go + Bubble Tea (Этап 1)
    app.go                     ← bubbletea Program, корневая модель
    rpcclient/
      client.go                ← Initialize, AgentRun, OnNotification
    view/
      chat.go                  ← viewport + список сообщений + tool blocks
      input.go                 ← textarea + slash/@ palette
      footer.go                ← подсказки + индикатор модели
      toolblock.go             ← collapsible блок tool call
    state/
      session.go               ← messages, pending ops, текущий step
    testdata/                  ← teatest snapshots
  vscode/                      ← заглушка-README, реализация на Этапе 2
  desktop/                     ← заглушка-README, Этап 3

cmd/orchestra/main.go          ← добавить команду `orchestra tui`
internal/cli/tui.go            ← cobra wrapper, по аналогии с apply.go
```

**Почему `ui/`, а не `internal/tui/`:** VS Code ext и desktop — не Go, под `internal/` им неудобно. `ui/` явно показывает «все клиенты ядра» и даёт им одинаковый уровень видимости.

## Раскладка экрана

```
┌─────────────────────────────────────────────────────────────┐
│  Orchestra · qwen3.6-27b · code-mode · D:\...\Orchestra     │  header (1 строка)
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  > перепиши resolver чтобы поддерживал regex                │
│                                                             │
│  Сначала прочитаю текущую реализацию.                       │  assistant text
│  ▸ read internal/resolver/external_patches.go (412 lines)   │  collapsed tool
│  ▸ grep "ResolveExternal" → 8 matches                       │
│  ▾ edit external_patches.go                                 │  expanded tool
│      @@ -45,6 +45,12 @@                                     │  с inline-diff
│      -  if strings.Contains(...)                            │
│      +  if matched, _ := regexp.Match(...)                  │
│                                                             │
│  Готово, добавил regex-fallback третьим проходом.           │
│                                                             │
│  ⏵ 3 pending ops · [a]pply · [d]iff · [x]discard            │  inline action bar
│                                                             │
├─────────────────────────────────────────────────────────────┤
│ > _                                                         │  multiline textarea
│                                                             │  (растёт до 6 строк)
├─────────────────────────────────────────────────────────────┤
│ ↑↓ history · / commands · @ files · Tab expand · Ctrl+C     │  footer hints
└─────────────────────────────────────────────────────────────┘
```

**Принципы:**
- Одна колонка, без боковых панелей
- Header / footer — фиксированные 1 строка, остальное — viewport ленты
- Tool blocks **inline в потоке**, сворачиваемые/разворачиваемые по Tab
- Pending ops — компактная action-bar в ленте, не модалка
- Модалка только для permission-prompt у `exec.run`
- Slash-палитра и @-mention — floating popup над инпутом

**В MVP не входит:** боковая панель файлов, CKG-граф, tabs, mouse, темы.

## Streaming-события в JSON-RPC (Фаза 0)

Без этого UI получит весь ответ агента одним куском в конце. Нужны server-initiated notifications от core к TUI.

**Новая нотификация** `agent/event` (server → client):

```json
{"jsonrpc":"2.0","method":"agent/event","params":{
  "session_id":"sess-01HF...",
  "type":"token_delta",
  "seq":42,
  "data":{...}
}}
```

**Типы:**

| `type` | `data` | Когда |
|---|---|---|
| `token_delta` | `{"text":"..."}` | каждый токен LLM |
| `tool_call_started` | `{"call_id":"...","name":"read","args":{...}}` | перед `Runner.Call` |
| `tool_call_completed` | `{"call_id":"...","ok":true,"result_preview":"...","duration_ms":42}` | после |
| `step_done` | `{"step":3,"reason":"tool_call"}` | конец итерации |
| `pending_ops` | `{"ops":[...],"diff":"..."}` | агент вернул `final` с патчами |
| `error` | `{"code":"...","message":"...","recoverable":true}` | `StaleContent`, `AmbiguousMatch` и т.п. |

**Дополнительно** — `permission_request` / `permission_response` (request-response, не notification — TUI должен ответить yes/no) для interactive consent на `exec.run`.

**Изменения в коде:**
1. `internal/protocol/version.go` — `ProtocolVersion` 1 → 2 + `docs/PROTOCOL.md`
2. `internal/agent/agent.go` — `Agent.EventSink interface { Emit(Event) }`, опциональный (nil → текущее поведение)
3. `internal/core/core.go` — `SessionRun` принимает callback событий, прокидывает в `Agent.EventSink`
4. `internal/jsonrpc/server.go` — `Notify(method, params)` для server→client
5. `internal/core/rpc_handler.go::AgentRun` — на время вызова создаёт `eventSink`, шлёт `agent/event` через `Notify`
6. `internal/jsonrpc/client.go` — `OnNotification(method, handler)`

**Совместимость:** старые клиенты (`apply --via-core`) не подписываются — события игнорируются. Бамп `ProtocolVersion`, без breaking change в существующих методах.

## Внутренняя структура TUI

Bubble Tea: одна корневая `Model`, `Update(msg) → (Model, Cmd)`, `View() → string`.

```go
type App struct {
    rpc      *rpcclient.Client
    session  state.Session       // история, pending ops, текущий step
    chat     view.Chat           // viewport + сообщения + tool blocks
    input    view.Input          // textarea + slash/@ palette
    footer   view.Footer
    modal    view.Modal          // nil | permissionPrompt | diffView
    width    int
    height   int
}
```

**Поток сообщений:**

| Источник | Msg | Куда |
|---|---|---|
| Терминал | `tea.KeyMsg`, `tea.WindowSizeMsg` | App.Update → роутинг по фокусу |
| RPC client (горутина) | `rpcEventMsg{Event}` | App.Update → session.Apply → chat.Update |
| User submit | `submitMsg{text}` | Cmd: rpc.AgentRun(text) |
| Permission | `permissionRequestMsg` | App.Update → открывает modal |

**Streaming без блокировок UI:** RPC-клиент гонит события в Go-канал, в `App.Init()` создаётся `tea.Cmd` который читает канал и эмитит `rpcEventMsg`. Без mutex'ов в моделях.

**Tool block — конечный автомат:**
```
pending → running (spinner) → completed (✓ summary) → expanded (Tab)
                            ↓
                          failed (✗ + error preview)
```

**Hybrid collapsing rule:** если вывод тула < 10 строк — показывается развёрнуто; длиннее — свёрнут с возможностью раскрыть Tab.

**Slash-палитра:** активируется когда первый символ инпута `/`. Команды MVP: `/help`, `/init`, `/compact`, `/model`, `/mode plan|code`, `/clear`, `/diff`, `/apply`, `/discard`, `/quit`.

**@-mention:** активируется когда текущее слово начинается с `@`. `fs.list` по project_root + fuzzy на клиенте.

**Переиспользуем:**
- `internal/protocol` — типы запросов/ответов
- `internal/jsonrpc` — клиент (расширить `Notify` server-side и `OnNotification` client-side)
- `internal/config` — `.orchestra.yml` для индикатора модели/агента
- `internal/git` — для `/init` без subprocess

**Тестирование:** `teatest` snapshot-тесты на ключевые флоу (palette по `/`, token streaming, pending ops → apply).

## Фазы реализации

**Фаза 0 — Streaming events в JSON-RPC** (предусловие, без UI)
- Бамп `ProtocolVersion` 1 → 2 + `docs/PROTOCOL.md`
- `Agent.EventSink` interface + проброс через `Core.SessionRun`
- `jsonrpc.Server.Notify` + `Client.OnNotification`
- Типы событий + `permission_request/response`
- Unit-тесты: mock client ловит события в правильном порядке
- ~3-5 коммитов

**Фаза 1 — Скелет TUI без агента**
- `ui/tui/` структура, `cmd/orchestra tui`
- Раскладка: header, viewport, input, footer
- Эхо в ленту, resize, Ctrl+C, Esc
- ~2-3 коммита

**Фаза 2 — Подключение к core**
- `ui/tui/rpcclient` поверх `internal/jsonrpc`
- Spawn `orchestra core`, `initialize`, lifecycle
- Submit → `agent.run`, рендер `token_delta` по мере прихода
- Базовые tool blocks (collapsed-only)
- ~3-4 коммита

**Фаза 3 — Полная визуализация процесса**
- Гибридные tool blocks (Tab expand, hybrid-rule)
- Inline-diff для `edit`/`write`
- Pending ops action-bar в ленте
- Permission modal для `exec.run`
- ~3-4 коммита

**Фаза 4 — Современный input**
- Slash-палитра
- @-mention с fuzzy
- Footer hints (динамические)
- Индикатор модели/агента в header
- История ввода ↑↓
- ~3-4 коммита

**Фаза 5 — Polish**
- teatest snapshots
- `ui/README.md`, `ui/tui/README.md`
- Обновление `docs/architecture-uml.md` (TUI → реализован)
- Обновление памяти (product_roadmap: TUI → done)
- ~2 коммита

## Не в MVP (follow-up)

- CKG-граф визуализация
- Multiple sessions / tabs
- Mouse support
- Темы
- Persistent история между запусками

## Открытые вопросы (решаем в плане)

- **Compaction-индикатор:** как показывать что сейчас идёт автосжатие истории
- **Pipeline mode** (Investigator→Coder→Critic): отдельный режим в TUI или как обычный `agent.run`

Оба не блокируют дизайн — детализация в `writing-plans`.
