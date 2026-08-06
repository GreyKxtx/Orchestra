# Orchestra TUI

Terminal UI для Orchestra ядра.

## Запуск

```bash
orchestra          # из каталога с .orchestra.yml
# или
orchestra tui
```

## Раскладка (as-is)

```
┌─────────────────────────────────────────────────────────────┐
│  chat scroll (messages · tools · notices · diffs)           │
├─────────────────────────────────────────────────────────────┤
│  ▌ Tasks N/M · Ctrl+T          ← только если есть todos     │
│  ▌ workflow:name · ○ a ⋯ b ✓ c ← только во время workflow   │
├─────────────────────────────────────────────────────────────┤
│  ▌ input                                                     │
│    build · model · provider · shell · ask|allow              │
├─────────────────────────────────────────────────────────────┤
│  project · 12k/60k (20%) · LSP · $…                          │
└─────────────────────────────────────────────────────────────┘
```

Сигналы (один source of truth):

| Сигнал | Где |
|--------|-----|
| mode / model / provider / shell | input mode-line |
| tokens% / LSP / cost / project | status bar |
| todos | Task panel над input |
| workflow stages | WorkflowProgress над input |
| hints | status bar справа |

См. [`docs/architecture/tui-chrome.md`](../../docs/architecture/tui-chrome.md).

## Клавиши

| Клавиша | Действие |
|---|---|
| Enter | отправить |
| Shift+Enter | новая строка |
| Tab | цикл mode (build → plan → explore) |
| Ctrl+T / t | cascade: tools → diff → Tasks (t при todos — Tasks; пустой input) |
| Ctrl+R | свернуть/развернуть Thinking |
| d / Ctrl+D | показать/скрыть inline diff (пустой input) |
| y / a / n / Esc | shell: один раз / на сессию / запретить |
| Esc | отменить turn / закрыть overlay |
| Ctrl+C | выйти |
| Ctrl+K | command palette |
| / | slash-палитра |
| @ | @-mention файлов |
| Ctrl+G | mouse passthrough (выделение терминала) |

Изменения пишутся на диск сразу после write/edit (TUI `apply=true`). Pending ops bar `[a]/[d]/[x]` — только legacy dry-run CLI, не TUI.

## Slash / команды

`/shell` — переключить `shell · ask` ↔ `shell · allow` (алиас `/exec`).  
`/diff`, `/clear`, `/sessions`, `/help`, `/quit`, `/provider`, `/model`, …

## Архитектура

```
ui/tui/
  app.go              — App struct, Init
  app_update.go       — Update dispatcher
  app_keys.go         — routeKey / Enter / chrome hotkeys
  app_mouse.go        — mouse handlers
  app_layout.go       — layout()
  app_diff.go         — commit diff toggle / restore
  app_rpc.go          — agent event handlers (stream/tools/turn/chrome)
  app_view.go         — View + input chrome
  app_session/status/todos/turn/… — session & chrome helpers
  view/               — widgets (chat, tools, task_panel, …)
  state/              — messages, TurnFSM, toolblocks
  rpcclient/          — JSON-RPC stdio → orchestra core
  theme/
```

Ход чата идёт через `session.message` (не one-shot `agent.run`). Streaming events: `message_delta`, `tool_call_*`, `todos_updated`, `step_usage`, `permission/request`.

**Permission modal (shell)**: при `shell · ask` модель спрашивает согласие. `[y]` — один раз, `[a]` — `shell · allow` на сессию (+ prefs), `[t]` — всегда этот tool до конца сессии, `[n]` — запрет.

## Статус

- [x] Chat + streaming + tool blocks + markdown
- [x] Task panel + shell ask/allow + compact tokens%
- [x] Auto-commit pipeline (TUI apply=true)
- [x] Session v2 / TurnFSM
- [ ] Полный Crush-style whitelist — out of scope

См. также: `docs/architecture/tui-pipeline.md`, `docs/architecture/tui-chrome.md`, `docs/PROTOCOL.md`.
