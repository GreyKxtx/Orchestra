# Orchestra core protocol (vNext)

Этот документ фиксирует **контракт** между client ↔ core на уровне JSON-RPC 2.0.

## Версии

- **`protocol.ProtocolVersion`**: `1`
- **`protocol.OpsVersion`**: `1`
- **`protocol.ToolsVersion`**: `3`

Совместимость проверяется в `initialize`:

- `protocol_version` **обязан** совпасть.
- `ops_version` / `tools_version` — опциональны, но если переданы, то **должны** совпасть.

### История ToolsVersion

- **v3** (2026-05-05): имена tools переключены на короткие алиасы
  (`read`/`ls`/`glob`/`write`/`edit`/`grep`/`symbols`/`bash`/`explore`/
  `runtime`/`todowrite`/`todoread`/`task_spawn`/`task_wait`/`task_cancel`/
  `task_result`). Канонические имена (`fs.read`, `search.text`, `exec.run`
  и т.д.) по-прежнему принимаются в `tool.call` для обратной совместимости.
- **v2**: добавлены `runtime.query`, `task.*`, `plan_enter`/`plan_exit`,
  `question`.
- **v1**: первоначальный набор `fs.*` / `search.*` / `code.*` / `exec.*`.

## Транспорт

### `stdio` (основной режим)

Фрейминг как в LSP:

```
Content-Length: <bytes>\r\n
\r\n
<json payload bytes>
```

Ограничения (защита от DoS/аллоцирования бесконечности):

- `Content-Length` обязателен, дубликаты запрещены (регистр не важен).
- Лимиты заголовков: `maxHeaderLines=64`, `maxHeaderLineBytes=8KiB`, `maxHeaderBytes=32KiB`.
- Лимит payload: `DefaultMaxContentBytes=4MiB`.
- `\r\n` и `\n` принимаются, пробелы вокруг `:` допускаются.

### HTTP (debug-only)

Включается флагом `orchestra core --http`.

- Bind: **только** `127.0.0.1`.
- Token обязателен (если не задан, генерируется).
- Auth:
  - `Authorization: Bearer <token>`
  - или `X-Orchestra-Token: <token>`

Endpoints:

- `GET /health` — возвращает `core.health` (удобно для проверки/мониторинга)
- `POST /rpc` — JSON-RPC 2.0
  - notification — `204 No Content`
  - request — `200 OK` + JSON-RPC response

Файл `.orchestra/core.http.json` — **временный discovery для отладки**, создаётся только пока процесс жив (удаляется при завершении).

Содержит:
- `protocol_version` — версия JSON-RPC протокола (то же значение, что `protocol.ProtocolVersion`)
- `url` — базовый URL сервера (например `http://127.0.0.1:12345`)
- `token` — токен для авторизации (в plain text, т.к. debug режим; файл защищён правами 0600 и в `.gitignore`)
- `instance_id` — UUID-like идентификатор экземпляра (защита от PID reuse)
- `pid` — PID процесса core
- `started_at_unix` — Unix timestamp (seconds) когда процесс core запустился
- `written_at_unix` — Unix timestamp (seconds) когда discovery файл был записан (для диагностики)

При старте core автоматически очищает stale discovery файлы (процесс мёртв или PID невалидный + файл старше 1 часа).

## JSON-RPC правила

### Batch

Top-level JSON массив (**batch**) **не поддерживается**.

- Ответ: `-32600 Invalid Request` с `id: null`.
- **Причина**: упрощение реализации и предсказуемость поведения. Batch requests добавляют сложность в обработку ошибок и порядок выполнения, что не требуется для текущего use case (одиночные запросы от клиента).

### `id` / notifications

- **Notification** — это request **без поля `id`**.
- `"id": null` — **это request**, на него **отвечаем** (ответ с `id: null`).
- **Важно**: `id: null` не является notification. Это валидный request ID (согласно JSON-RPC 2.0), и ответ обязателен.

Валидные типы `id`:

- string
- number
- null

Любой другой тип (например `{}` / `[]`) — `-32600 Invalid Request` с `id: null`.

### Базовая валидация request

Для того, чтобы request считался валидным:

- `jsonrpc` должен быть строкой `"2.0"`
- `method` должен быть **непустой строкой**

Иначе: `-32600 Invalid Request` с `id: null`.

## Handshake: `initialize`

- До `initialize` разрешены только:
  - `core.health`
  - `initialize`
- Любой другой метод до handshake возвращает ошибку Orchestra `NotInitialized` (см. ниже).

`initialize` идемпотентен:

- тот же набор параметров — OK
- несовпадение параметров — hard fail (состояние уже инициализированного core не ломается)

## Методы

### `core.health`

Request:

```json
{"jsonrpc":"2.0","id":1,"method":"core.health","params":{}}
```

Response `result`:

```json
{
  "status": "ok",
  "core_version": "vnext",
  "protocol_version": 1,
  "ops_version": 1,
  "tools_version": 1,
  "workspace_root": "...",
  "project_id": "sha256:..."
}
```

### `initialize`

`params`:

- `project_root` (string)
- `project_id` (string)
- `protocol_version` (int)
- `ops_version` (int, optional)
- `tools_version` (int, optional)

Response `result`:

```json
{"status":"ok","health":{...}}
```

### `agent.run`

`params` (основные):

- `query` (string)
- `apply` (bool, optional; default=false)
- `backup` (bool, optional)
- `allow_exec` (bool, optional; default=false)
- `debug` (bool, optional)

Лимиты (опционально):

- `max_steps`
- `max_invalid_retries`
- `max_prompt_bytes`

Response `result`:

- `steps` (int)
- `applied` (bool)
- `patches` (optional)
- `ops` (optional)
- `apply_response` (optional)

> Важно: по умолчанию `apply=false` — это dry-run (core ничего не пишет на диск, возвращает diff/план).

### `tool.call`

`params`:

- `name` (string) — имя инструмента (например `fs.*`, `exec.run`)
- `input` (json) — вход конкретного инструмента

Response `result` — JSON-объект/массив (ответ инструмента).

## Методы сессий

Сессия инкапсулирует multi-turn диалог: история сообщений и pending-операции хранятся в памяти core-процесса между вызовами `session.message`.

### `session.start`

Создаёт новую сессию.

`params`: `{}`

Response `result`:

```json
{"session_id": "abc123..."}
```

### `session.message`

Выполняет один агентный ход в рамках сессии.

`params`:

- `session_id` (string, обязательный)
- `content` (string, обязательный) — запрос пользователя
- `apply` (bool, optional; default=false) — применить изменения на диск
- `backup` (bool, optional) — делать резервные копии изменённых файлов
- `allow_exec` (bool, optional) — разрешить инструмент `exec.run`
- `max_steps` (int, optional)
- `max_invalid_retries` (int, optional)
- `max_prompt_bytes` (int, optional)

Response `result`:

- `steps` (int) — число шагов агента
- `applied` (bool)
- `patches` (optional) — diff/patches (при `apply=false`)
- `ops` (optional) — сырые операции
- `apply_response` (optional) — при `apply=true`

Пока ход выполняется, core отправляет уведомления `agent/event` по тому же JSON-RPC соединению.

Структура уведомления:

```json
{
  "jsonrpc": "2.0",
  "method": "agent/event",
  "params": {
    "step": 1,
    "type": "message_delta | tool_call_start | tool_call_end | done",
    "content": "...",
    "tool_call_name": "..."
  }
}
```

Если `apply=false` и агент вернул непустые ops, они сохраняются как pending и могут быть применены через `session.apply_pending`.

### `session.cancel`

Прерывает текущий ход (no-op, если сессия простаивает).

`params`:

- `session_id` (string)

Response `result`: `null`

### `session.apply_pending`

Применяет ops, сохранённые после последнего dry-run хода. Pending сбрасывается после применения или если следующий ход вернул новые ops.

`params`:

- `session_id` (string)
- `backup` (bool, optional)

Response `result`:

- `applied` (bool) — false если pending пуст
- `apply_response` (optional) — при `applied=true`

### `session.history`

Возвращает накопленную историю сообщений сессии.

`params`:

- `session_id` (string)

Response `result`:

- `session_id` (string)
- `messages` (array of `{role, content}`)

### `session.close`

Отменяет текущий ход (если есть) и удаляет сессию. Идемпотентен — если сессия не найдена, возвращает OK.

`params`:

- `session_id` (string)

Response `result`: `null`

## Ошибки

### Стандартные JSON-RPC

- `-32700` Parse error
- `-32600` Invalid Request
- `-32601` Method not found
- `-32603` Internal error

Для `Parse error` и `Invalid Request` `id` в ответе всегда `null` (если id определить нельзя или request некорректный).

### Ошибки Orchestra (server errors)

Orchestra использует диапазон `-32000..-32099`.

| protocol.ErrorCode | JSON-RPC error.code |
|---|---:|
| `InvalidLLMOutput` | `-32001` |
| `StaleContent` | `-32002` |
| `AmbiguousMatch` | `-32003` |
| `PathTraversal` | `-32004` |
| `ExecTimeout` | `-32005` |
| `ExecFailed` | `-32006` |
| `NotInitialized` | `-32007` |
| `ExecDenied` | `-32008` |
| (прочее) | `-32099` |

Payload (в `error.data`) для ошибок из `internal/protocol`:

```json
{
  "code": "NotInitialized",
  "data": {"method":"agent.run"}
}
```

---

Если хочется расширять контракт — меняем `protocol.ProtocolVersion` и обновляем этот документ вместе с тестами.
