# Orchestra UI clients

Все клиенты ядра `orchestra core` живут здесь. Каждый — отдельный subdirectory с собственным README.

| Каталог | Стек | Статус |
|---|---|---|
| `tui/` | Go + Bubble Tea | в разработке (Фаза 1) |
| `vscode/` | TypeScript / Node | планируется (Этап 2 product roadmap) |
| `desktop/` | TBD (Tauri или Electron) | планируется (Этап 3 product roadmap) |

## Принципы

- Каждый клиент общается с ядром через JSON-RPC stdio (subprocess `orchestra core`).
- Не дублируем бизнес-логику ядра в клиентах. Клиент = только UI + транспорт.
- Все клиенты опираются на единый `internal/protocol` для типов.

## Добавление нового клиента

1. Создать `ui/<name>/` с собственным README
2. Если клиент на Go — может импортировать `internal/protocol`, `internal/jsonrpc`
3. Если на другом языке — генерировать DTO из `docs/PROTOCOL.md` и переиспользовать схему версионирования
