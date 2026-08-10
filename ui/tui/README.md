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
│  ▌ Tasks (sticky checklist)    ← открытые todos над input     │
│  ▌ workflow:name · ○ a ⋯ b ✓ c ← только во время workflow   │
├─────────────────────────────────────────────────────────────┤
│  ▌ input                                                     │
│    build · model · provider · ask|allow                      │
├─────────────────────────────────────────────────────────────┤
│  project · 12k/60k (20%) · LSP · $…                          │
└─────────────────────────────────────────────────────────────┘
```

Сигналы (один source of truth):

- Assistant turn = chronological **segments** (reasoning → tools → text → …), см. `[docs/architecture/tui-chat-segments.md](../../docs/architecture/tui-chat-segments.md)`


| Сигнал                         | Где                                                    |
| ------------------------------ | ------------------------------------------------------ |
| mode / model / provider / ask  | allow                                                  |
| tokens% / LSP / cost / project | status bar                                             |
| todos                          | sticky checklist над input (скрывается когда все done) |
| workflow stages                | WorkflowProgress над input                             |
| hints                          | status bar справа                                      |
| tools expand                   | Ctrl+T / t на assistant turn                           |
| diff expand                    | d / Ctrl+D                                             |


См. `[docs/architecture/tui-chrome.md](../../docs/architecture/tui-chrome.md)`.

## Клавиши


| Клавиша         | Действие                                                                            |
| --------------- | ----------------------------------------------------------------------------------- |
| Enter           | отправить (в очередь, если agent busy)                                              |
| Shift+Enter     | новая строка                                                                        |
| Tab             | цикл mode (build → plan → explore → ask → debug → architecture → agent → orchestra) |
| Ctrl+T / t      | tools → diff                                                                        |
| Ctrl+R          | свернуть/развернуть Thinking                                                        |
| d / Ctrl+D      | показать/скрыть inline diff (пустой input)                                          |
| ↑↓ a x Enter    | per-file diff review (когда diff развёрнут)                                         |
| a / d / x       | pending ops bar: apply · diff · discard (dry-run review)                            |
| y / a / n / Esc | shell: один раз / на сессию / запретить                                             |
| Esc             | отменить turn / закрыть overlay                                                     |
| Ctrl+C          | выйти                                                                               |
| Ctrl+K          | command palette                                                                     |
| Ctrl+S          | sessions list (если есть в проекте)                                                 |
| /               | slash-палитра                                                                       |
| @               | @-mention файлов                                                                    |
| Ctrl+G          | mouse passthrough (выделение терминала)                                             |


TUI по умолчанию коммитит write/edit сразу (`apply=true`). При dry-run core присылает `pending_ops` — в ленте показывается inline bar `⏵ N pending · [a]pply · [d]iff · [x]discard` + per-file review (↑↓ a x Enter).

## Slash / команды

`/shell` — переключить `shell · ask` ↔ `shell · allow` (алиас `/exec`).  
`/attach <path>` — прикрепить файл (image/PDF/SVG); копия в `.orchestra/attachments/`, chips в user bubble, multimodal в LLM при `llm.multimodal: true`.  
`/diff`, `/clear`, `/sessions`, `/help`, `/quit`, `/provider`, `/model`, …

## Архитектура

Chat DTO (`Message`, `Segment`, `ToolBlock`, …) — **`internal/uimodel`**; TUI re-exports via `ui/tui/state/aliases.go`. Слои и import rules: `docs/architecture/modules.md`.

```
ui/tui/
  app.go              — App struct, Init
  app_update.go       — Update dispatcher
  app_keys.go         — routeKey / Enter / selection / scroll
  app_chrome_keys.go  — Ctrl+T/R, bare d/t, tools expand SoT
  app_mouse.go        — mouse handlers
  app_layout.go       — layout()
  app_diff_review.go  — per-file accept/reject
  app_action_bar.go   — inline [a]/[d]/[x] pending ops bar
  app_attach.go       — /attach staging + attachment chips
  app_rpc.go          — agent event handlers (stream/tools/turn/chrome)
  app_view.go         — View + input chrome
  app_status.go       — chromeMetrics + status bar sync
  app_session/todos/turn/… — session & chrome helpers
  view/               — widgets (chat, input_*.go, tools, task_panel, …)
  state/              — messages, TurnFSM, toolblocks
  rpcclient/          — JSON-RPC stdio → orchestra core
  theme/
```

Ход чата идёт через `session.message` (не one-shot `agent.run`). Streaming events: `message_delta`, `tool_call_*`, `todos_updated`, `step_usage`, `permission/request`.

**Permission modal (shell)**: при `shell · ask` модель спрашивает согласие. `[y]` — один раз, `[a]` — `shell · allow` на сессию (+ prefs), `[t]` — всегда этот tool до конца сессии, `[n]` — запрет.

**LSP (status bar `LSP ●/◐ N%`)**: lazy spawn по расширению; без бинарника — modal `lsp.install`. Долгий download — async ensure (status `LSP ◐ gopls 42%`), edit/write могут вернуть `diagnostics_pending`. Shell + lsp modals — FIFO. Worker diagnostics inline + expand. См. `docs/architecture/lsp-auto-provision.md`.

**Manual smoke (LSP + Worker diagnostics):** unit-тест modal — `go test ./ui/tui/view -run TestModal_LSPInstall`. Полный UX:

1. Rebuild TUI, открыть Go-проект без gopls в cache → modal `lsp.install`.
2. Запустить orchestra mode task с worker → после `edit` на `.go` видны `· N LSP error(s)` на tool line.
3. Expand tool block (Ctrl+T) → блок `LSP diagnostics:` с line:col.



## Статус

- [x] Chat + streaming + tool blocks + markdown
- [x] Task panel + shell ask/allow + compact tokens%
- [x] Auto-commit pipeline (TUI apply=true)
- [x] Session v2 / TurnFSM
- [ ] Полный Crush-style whitelist — out of scope

См. также: `docs/architecture/tui-pipeline.md`, `docs/architecture/tui-chrome.md`, `docs/architecture/lsp-auto-provision.md`, `docs/PROTOCOL.md`.