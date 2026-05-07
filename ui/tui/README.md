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

- [x] **Фаза 1 — скелет** (текущая): раскладка, echo, базовая навигация
- [ ] Фаза 2 — подключение к `orchestra core` через JSON-RPC stdio, streaming событий
- [ ] Фаза 3 — collapsible tool blocks, inline-diff, pending ops action bar
- [ ] Фаза 4 — slash-команды, @-mention, динамические footer hints
- [ ] Фаза 5 — polish, snapshot tests расширенные

## Архитектура

`ui/tui/app.go` — корневая Bubble Tea модель. Делегирует рендеринг в `view/{header,chat,input,footer}.go`. Состояние сессии (history) живёт в `state/session.go`. Phase 2 добавит `rpcclient/` для stdio JSON-RPC.

См. также: `docs/superpowers/specs/2026-05-07-tui-design.md` (общий дизайн TUI), `docs/PROTOCOL.md` (контракт ядра).
