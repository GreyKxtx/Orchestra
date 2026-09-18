# Orchestra UI clients

Все клиенты ядра `orchestra core` живут здесь. Каждый — отдельный subdirectory с собственным README.

| Каталог | Стек | Транспорт | Статус |
|---|---|---|---|
| `tui/` | Go + Bubble Tea + Lipgloss | stdio | реализован (streaming · tool blocks · `/attach` · @-mention · session v4) |
| `vscode/` | TypeScript / Node | stdio | chat + settings + trajectory · attachments/vision · protocol **v20** |
| `web/` | JS без фреймворка, собирается в `static/` | WebSocket `/ws` | реализован; рендерер общий с `vscode/` |
| `desktop/` | Rust + Tauri поверх `orchestra web` | — (окно на адрес ядра) | оболочка работает; упаковка инсталляторов не доделана |

## Принципы

- Клиент общается с ядром либо через JSON-RPC по stdio (subprocess `orchestra
  core`), либо через тот же JSON-RPC по WebSocket (`orchestra web`, `/ws`).
- Не дублируем бизнес-логику ядра в клиентах. Клиент = только UI + транспорт.
- Все клиенты опираются на sub-module **`protocol/`** (`github.com/orchestra/orchestra/protocol`) для wire-типов.
- `vscode/` и `web/` делят **один рендерер**: фрагменты живут в
  `vscode/media/chat-src/`, сборщик веба читает их там же, а не копирует. Роль
  хоста играет либо расширение, либо веб-адаптер — но рендереру они обязаны
  отвечать одинаковыми сообщениями. Свежесть собранных бандлов проверяют
  `vscode/scripts/check-webview.mjs` и `web/scripts/check-web.mjs` в CI.
- `desktop/` своего фронтенда не имеет: это окно на `orchestra web`.

## Добавление нового клиента

1. Создать `ui/<name>/` с собственным README
2. Если клиент на Go — может импортировать `github.com/orchestra/orchestra/protocol`, `protocol/jsonrpc`, `github.com/orchestra/orchestra/llm`
3. Если на другом языке — генерировать DTO из `docs/PROTOCOL.md` и переиспользовать схему версионирования
