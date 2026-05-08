# Orchestra TUI

Terminal UI для Orchestra ядра.

## Запуск

```bash
orchestra tui
```

(Требует, чтобы в cwd был `.orchestra.yml` для отображения модели в header'e — иначе будет "(none)".)

## Раскладка

```
┌─────────────────────────────────────────────────────────────┐
│  Orchestra · qwen3.6-27b · code · <project>                 │  header
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  > пользовательский ввод                                    │
│                                                             │
│  ответ агента                                               │  ← (Фаза 1: echo)
│                                                             │
├─────────────────────────────────────────────────────────────┤
│ > _                                                         │  multiline textarea
├─────────────────────────────────────────────────────────────┤
│ ↑↓ history · Enter send · Shift+Enter newline · Ctrl+C quit │  footer
└─────────────────────────────────────────────────────────────┘
```

## Клавиши

| Клавиша | Действие |
|---|---|
| Enter | отправить ввод |
| Shift+Enter | новая строка в инпуте |
| Tab | развернуть/свернуть последний tool block |
| a | применить pending ops (если есть) |
| d | показать/скрыть inline diff |
| x | отменить pending ops |
| y / n / Esc | разрешить/запретить exec.run (modal) |
| Esc | очистить инпут (без modal) |
| Ctrl+C | выйти |
| / | открыть slash-палитру команд |
| @ | @-mention fuzzy-поиск файлов |
| ↑ / ↓ | история ввода (single-line mode) |

## Статус по фазам

- [x] **Фаза 1 — скелет**: раскладка, echo, базовая навигация
- [x] **Фаза 2 — подключение к ядру** (текущая): JSON-RPC stdio, streaming token deltas, tool blocks (collapsed)
- [x] **Фаза 3** — collapsible tool blocks (Tab expand), inline diff view, pending ops action bar ([a]pply / [d]iff / [x]discard), permission modal для exec.run
- [x] **Фаза 4** — slash-палитра (`/help` `/clear` `/model` `/mode` `/diff` `/apply` `/discard` `/quit`), @-mention fuzzy (`@`), история ввода ↑↓, динамические footer hints
- [x] **Фаза 5** — расширенные behavior tests (slash palette, /help, /clear, /quit, Esc), обновление документации

## Архитектура

```
ui/tui/
  app.go           ← корневая Bubble Tea модель (Update / View / Init)
  mention_test.go  ← unit-тесты для @-mention helpers
  view/
    chat.go        ← viewport + сообщения + tool blocks + diff
    input.go       ← textarea wrapper + SetValue
    footer.go      ← hints line (динамические)
    header.go      ← model · mode · cwd
    modal.go       ← permission modal для exec.run
    palette.go     ← SlashPalette + MentionPalette (fuzzy)
    diff.go        ← LCS-based colored diff renderer
  state/
    session.go     ← Messages, ToolBlocks, RoleDiff
    toolblock.go   ← ToolBlock lifecycle state
    history.go     ← InputHistory (↑↓ recall)
  rpcclient/
    client.go      ← JSON-RPC stdio wrapper (Spawn, AgentRun, ApplyOps, RespondPermission)
    events.go      ← Event types + kinds
```

`app.go` координирует все состояния: `paletteActive` (slash), `mentionActive` (@), `agentBusy` (RPC), `permModal` (exec consent), `pendingOps` (apply/discard). Все мутации состояния — только в `Update()` goroutine.

См. также: `docs/superpowers/specs/2026-05-07-tui-design.md` (дизайн), `docs/PROTOCOL.md` (контракт ядра), `docs/architecture-uml.md` (TUI в Containers diagram).

## Подключение к ядру (Фаза 2)

TUI спаунит `orchestra core --workspace-root <cwd>` как subprocess и общается через stdin/stdout JSON-RPC. На submit (Enter) вызывается `agent.run`; streaming события (`message_delta`, `tool_call_start/completed`, `pending_ops`) рендерятся в ленту по мере прихода.

**Tool blocks** показываются свернутыми одной строкой:
- `⋯ name` — выполняется
- `▸ name → preview` — завершён успешно
- `▸ name → error: ...` (красным) — упал

**Pending ops** показываются action bar'ом в ленте: `⏵ N pending ops · [a]pply · [d]iff · [x]discard`. Нажатие `[a]` применяет ops через RPC `ops.apply` (без перезапуска LLM); `[d]` показывает inline diff; `[x]` отменяет.

**Если subprocess падает или initialize не проходит** — Run возвращает ошибку до запуска UI; на лету ошибки показываются как `[error] ...` в ленте.

**Permission modal для exec.run**: при попытке модели вызвать `bash` без `--allow-exec` TUI показывает интерактивный диалог `[y] Allow / [n] Deny`. Ответ передаётся ядру через server-initiated JSON-RPC `permission/request`.
