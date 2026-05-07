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
| Esc | очистить инпут |
| Ctrl+C | выйти |

## Статус по фазам

- [x] **Фаза 1 — скелет**: раскладка, echo, базовая навигация
- [x] **Фаза 2 — подключение к ядру** (текущая): JSON-RPC stdio, streaming token deltas, tool blocks (collapsed)
- [ ] Фаза 3 — collapsible tool blocks expand-on-Tab, inline-diff, pending ops action bar
- [ ] Фаза 4 — slash-команды, @-mention, динамические footer hints
- [ ] Фаза 5 — polish, snapshot tests расширенные

## Архитектура

`ui/tui/app.go` — корневая Bubble Tea модель. Делегирует рендеринг в `view/{header,chat,input,footer}.go`. Состояние сессии (history) живёт в `state/session.go`. Phase 2 добавил `rpcclient/` для stdio JSON-RPC.

См. также: `docs/superpowers/specs/2026-05-07-tui-design.md` (общий дизайн TUI), `docs/PROTOCOL.md` (контракт ядра).

## Подключение к ядру (Фаза 2)

TUI спаунит `orchestra core --workspace-root <cwd>` как subprocess и общается через stdin/stdout JSON-RPC. На submit (Enter) вызывается `agent.run`; streaming события (`message_delta`, `tool_call_start/completed`, `pending_ops`) рендерятся в ленту по мере прихода.

**Tool blocks** показываются свернутыми одной строкой:
- `⋯ name` — выполняется
- `▸ name → preview` — завершён успешно
- `▸ name → error: ...` (красным) — упал

**Pending ops** пока показываются placeholder-сообщением `[N pending ops — apply with /apply (Phase 3)]`. Реальный action bar (apply / discard / diff) — Фаза 3.

**Если subprocess падает или initialize не проходит** — Run возвращает ошибку до запуска UI; на лету ошибки показываются как `[error] ...` в ленте.

**Permission/request на bash пока не wired**: bash-вызовы будут отклонены статическим gate'ом (нужен `--allow-exec` или `exec.confirm: false` в `.orchestra.yml`). Modal-диалог появится в Фазе 3.
