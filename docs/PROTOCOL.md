# Orchestra core protocol (vNext)

Этот документ фиксирует **контракт** между client ↔ core на уровне JSON-RPC 2.0.

## Версии

- **`protocol.ProtocolVersion`**: `7`
- **`protocol.OpsVersion`**: `1`
- **`protocol.ToolsVersion`**: `12`

Совместимость проверяется в `initialize`:

- `protocol_version` **обязан** совпасть.
- `ops_version` / `tools_version` — опциональны, но если переданы, то **должны** совпасть.

### История ProtocolVersion

- **v6** (2026-08-05): unified session schema v2 (`.orchestra/sessions/<id>.json` with `ui_messages` + `history`); `session.start` accepts optional `session_id`; new methods `session.get`, `session.list`, `session.ui_sync`.
- **v5** (2026-08-05): `agent.run` / `session.message` — поля `apply_output` (`disk`|`patch`), `patch_path`, `profile` (`fast`|`precision`); в result может быть `patch_path` при `apply_output=patch`.
- **v4** (2026-05-18): добавлены методы `workflow.list`, `workflow.run`, `skill.list`, `skill.invoke`; streaming events `workflow/stage_start` и `workflow/stage_done`.
- **v3** (2026-05-07): добавлены agent-level streaming events (`tool_call_completed`, `step_done`, `pending_ops`, `recoverable_error`) и bidirectional `permission/request` method.
- **v2** (2026-05-06): добавлено опциональное поле `mode` в `agent.run` и `session.message` для custom-agents (Sub-project D).
- **v1**: первоначальный набор методов.

### История ToolsVersion

- **v11** (2026-05-21): unified `task` tool (sync subagent); `todowrite` accepts `completed` status alias; plan mode uses `.orchestra/plans/<session-id>.md`.
- **v10**: `gh.pr.*`, `gh.issue.*` (allowExec-gated).
- **v6** (2026-05-15): добавлен инструмент `diff.preview`.
- **v5**: добавлены `lsp.definition`, `lsp.references`, `lsp.hover`, `lsp.diagnostics`, `lsp.rename`; поле `diagnostics` в ответах `fs.write` и `fs.edit`.
- **v4** (2026-05-05): `fs.read` content field now prefixed with 1-based line
  numbers (`1: line`, `2: line`). The `sha256`/`file_hash` fields are still
  computed from the raw file bytes. Do NOT include line-number prefixes in
  `fs.edit` search strings.
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

### `$/cancelRequest`

Client-initiated notification (no `id` field) that asks the server to cancel
an in-flight request. Matches LSP semantics: unknown / already-completed
ids are silently ignored.

`params`:

- `id` (string | number, required) — the `id` of the original request to cancel.

The server cancels the per-request `context.Context` it derived for the
target dispatch. Handlers that honour ctx (`agent.run`, `workflow.run`,
`skill.invoke`, every tool inside the agent loop) unwind promptly and the
original request returns a response — typically an error wrapping
`context.Canceled` or an `ExecFailed` / domain-specific code.

The reference TUI client wires this automatically: cancelling the Go ctx
passed to `Client.Call` fires `$/cancelRequest` on the wire as the call
returns `ctx.Err()`. Custom clients should follow the same pattern.

Dispatch is asynchronous on the server side specifically so a cancel
notification arriving mid-call is read and processed without waiting for
the long-running handler to return. Response order is therefore not
guaranteed to match request order — multiple in-flight requests may
complete in any order.

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
  "protocol_version": 3,
  "ops_version": 1,
  "tools_version": 6,
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
- `mode` (string, optional) — имя built-in режима (`build`, `plan`, `explore`, …) или custom-агента, определённого в `agents:` в `.orchestra.yml`; пустая строка → поведение `build` по умолчанию.
- `apply_output` (string, optional; default=`disk`) — `disk` (запись/dry-run как раньше) или `patch` (экспорт unified `.patch`, диск не трогается; mutually exclusive с `apply=true`).
- `patch_path` (string, optional) — путь для `.patch` при `apply_output=patch` (иначе `apply.patch_dir` / `.orchestra/patches/orchestra-<ts>.patch`).
- `profile` (string, optional) — adaptive preset `fast` | `precision` (см. `docs/architecture/tui-pipeline.md` §9).

> **Skills:** CLI также принимает `--skill <name>`, который загружает file-based agent definition из `<project>/.orchestra/skills/<name>.md`. Скилл резолвится в синтетический `AgentDefinition` и идёт через тот же путь `--mode`, поэтому JSON-RPC surface не меняется — это CLI-side loader поверх существующего `AgentOptions`. См. `docs/skills.md`.

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
- `patch_path` (optional) — абсолютный путь к записанному `.patch` при `apply_output=patch`
- `todos` (optional) — чеклист после хода (`todowrite`)
- `plan_path` (optional) — путь plan-файла при `mode: plan`
- `switch_to_build` (optional, legacy) — true если `plan_exit` одобрен; core обычно уже выполнил build-продолжение in-process
- `usage` (optional) — token summary

> Важно: по умолчанию `apply=false` — это dry-run (core ничего не пишет на диск, возвращает diff/план). `apply_output=patch` всегда dry-run для workspace и дополнительно пишет unified diff.

### `tool.call`

`params`:

- `name` (string) — имя инструмента (например `fs.*`, `exec.run`)
- `input` (json) — вход конкретного инструмента

Response `result` — JSON-объект/массив (ответ инструмента).

## Методы сессий

Сессия инкапсулирует multi-turn диалог. С **ProtocolVersion 6** состояние персистится в `.orchestra/sessions/<id>.json` (schema **v2**): `history` (LLM-память), `ui_messages` (TUI projection), `pending_ops`, `todos`, `plan_path`.

### `session.start`

Создаёт новую сессию или переоткрывает существующую по id.

`params`:

- `session_id` (string, optional) — если задан, core вызывает `LoadOrCreate` и восстанавливает snapshot v2 с диска

Response `result`:

```json
{"session_id": "20260805T150405-7f3a", "restored": true}
```

- `restored` — true когда на диске уже был snapshot с history и/или ui_messages

### `session.get`

Возвращает unified v2 view (для reopen TUI).

`params`: `{ "session_id": "..." }`

Response `result`:

```json
{
  "session_id": "...",
  "title": "...",
  "model": "...",
  "ui_messages": [],
  "history_len": 4,
  "has_pending": false,
  "restored": true
}
```

### `session.list`

Список сессий для picker (meta без полной history).

`params`: `{}`

Response `result`: `{ "sessions": [ { "id", "title", "model", "created_at", "updated_at", "msg_count" }, ... ] }`

### `session.ui_sync`

Сохраняет TUI chat projection в v2 snapshot (core — единственный writer на диск).

`params`:

- `session_id` (string)
- `title` (string, optional)
- `model` (string, optional)
- `ui_messages` (array) — см. schema v3 в `docs/architecture/tui-chat-segments.md` и `docs/architecture/tui-pipeline.md` §3.

Response `result`: `{ "session_id": "...", "saved": true }`

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
- `apply_output` / `patch_path` / `profile` — как у `agent.run`

Response `result`:

- `steps` (int) — число шагов агента
- `applied` (bool)
- `patches` (optional) — diff/patches (при `apply=false`)
- `ops` (optional) — сырые операции
- `apply_response` (optional) — при `apply=true`
- `patch_path` (optional) — при `apply_output=patch`
- `todos` (optional) — чеклист сессии после хода
- `plan_path` (optional) — canonical plan markdown path (`.orchestra/plans/<session-id>.md`)
- `switch_to_build` (optional, legacy) — см. `agent.run`
- `usage` (optional)

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

### `session.rewind`

Откатывает UI-проекцию и LLM history к user-checkpoint (сообщения после индекса удаляются; assistant-ответы после checkpoint тоже).

`params`:

- `session_id` (string)
- `ui_message_index` (int) — inclusive; должен указывать на `role=user` в `ui_messages`

Response `result`:

- `session_id` (string)
- `ui_messages` (int)
- `history_messages` (int)

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
| `AlreadyInitialized` | `-32009` |
| `ProtocolMismatch` | `-32010` |
| `AlreadyExists` | `-32011` |
| `NotFound` | `-32012` |
| `InvalidParams` | `-32602` (JSON-RPC standard) |
| (прочее) | `-32099` |

Payload (в `error.data`) для ошибок из `internal/protocol`:

```json
{
  "code": "NotInitialized",
  "data": {"method":"agent.run"}
}
```

## Workflow methods (v4+)

Workflows orchestrate skills as a DAG of stages defined by YAML in
`.orchestra/workflows/<name>.yaml` (project) or `~/.orchestra/workflows/`
(user). The runner executes stages in cohort-parallel topological order
with marker-based completion routing (advance / loop / fail / redo:<id>).

### `workflow.list`

`params`: `{}` (or omit)

Response `result`:

```json
{
  "workflows": [
    {"name": "tdd_feature", "description": "...", "stages": ["spec","tests","execute"], "source": "/path/to/file.yaml"}
  ]
}
```

### `workflow.run`

`params`:

- `name` (string, required) — workflow name as registered by `workflow.list`.
- `arguments` (string, required) — user task; passed to every stage as the initial `$ARGUMENTS`.
- `apply` (bool, optional, default false) — when true, stages write to disk; otherwise they run dry-run with the staging overlay.
- `allow_exec` / `allow_web` / `allow_browser` (bool, optional) — per-call policy that filters the tool list each stage may invoke.

Errors:

- `NotFound` (-32012) — workflow or one of its stages' skills is unknown.
- `InvalidParams` (-32602) — empty name/arguments, or `loop_until_marker` references a skill without `completion_markers`.

Response `result`:

```json
{
  "name": "tdd_feature",
  "outputs": {"spec": "...", "tests": "...", "execute": "..."},
  "final_stage": "execute",
  "failure_reason": "",
  "stages": [{"stage_id":"spec","attempt":1,"marker":"PLAN READY","action":"advance","output_kb":2}],
  "duration_ms": 41213
}
```

Streaming: emits `workflow/stage_start` and `workflow/stage_done` notifications.

### `skill.list`

`params`: `{}` (or omit)

Response `result`:

```json
{
  "skills": [
    {
      "name": "spec_writer", "description": "...", "tools": ["read","grep"],
      "provider": "", "model": "", "completion_markers": ["PLAN READY"],
      "origin": "project"
    }
  ]
}
```

### `skill.invoke`

Runs a single skill end-to-end as one child agent turn. Always dry-run
(stages write to the in-memory overlay). Useful for one-shot subtasks
without spinning up a full workflow.

`params`:

- `name` (string, required)
- `arguments` (string, required)
- `allow_exec` / `allow_web` / `allow_browser` (bool, optional)

Errors: `NotFound` (-32012), `InvalidParams` (-32602).

Response `result`:

```json
{
  "skill": "debugger",
  "output": "...assistant final text or task_result payload...",
  "marker": "ROOT CAUSE",
  "steps": 6
}
```

## Streaming events (server → client notifications)

While `agent.run` or `session.message` is in progress, the core may emit
JSON-RPC notifications to the client (no `id` field, one-way).

### `agent/event`

Generic envelope:

| Field | Type | Description |
|---|---|---|
| `step` | int | Current agent loop step number |
| `type` | string | One of the kinds below |
| `content` | string | Type-specific payload (string), used for most kinds |
| `data` | object | Type-specific structured payload, only for `pending_ops` |
| `session_id` | string | Present for `session.message` turns; omitted for one-shot `agent.run` |
| `turn_id` | string | Sortable id for this `agent.run` or `session.message` invocation |
| `tool_call_id`, `tool_call_name`, `tool_call_index`, `args_delta` | optional | Set for tool-call-related kinds |

### Event types

| `type` | Emitted when | Notable fields |
|---|---|---|
| `message_delta` | LLM streamed a token of assistant text | `content` |
| `tool_call_start` | LLM declared intent to call a tool | `tool_call_name`, `tool_call_id` |
| `tool_call_delta` | More argument bytes for in-progress call | `args_delta` |
| `tool_call_completed` | Agent loop finished `tools.Call` | `tool_call_name`, `tool_call_id`, `content` (truncated preview, 256 bytes) |
| `step_done` | End of one agent loop iteration | `content` ∈ {tool_call, final, invalid, final_retry} |
| `pending_ops` | Agent finalized patches (dry-run or pre-apply) | `data` = `{ops: [...], diff: [{path, before, after}], applied: bool}` |
| `recoverable_error` | StaleContent / AmbiguousMatch / schema invalid; loop will retry | `content` (short message) |
| `done` | LLM stream ended | (full assembled response in agent state) |
| `error` | LLM-stream-level error (different from `recoverable_error`) | `content` |

### `exec/output_chunk`

Streamed during `bash` (alias for `exec.run`) tool execution.

| Field | Type | Description |
|---|---|---|
| `step` | int | Current agent loop step |
| `chunk` | string | Raw stdout/stderr chunk |
| `session_id` | string | Present for `session.message` turns; omitted for one-shot `agent.run` |
| `turn_id` | string | Sortable id for this turn |

### `workflow/stage_start` (v4+)

Emitted right before a stage begins execution (including each redo iteration).

| Field | Type | Description |
|---|---|---|
| `name` | string | Workflow name |
| `stage_id` | string | Stage id from the YAML |
| `attempt` | int | 1-based lifetime attempt counter for this stage |

### `workflow/stage_done` (v4+)

Emitted after the stage's child agent has produced output and the runner has decided the next action.

| Field | Type | Description |
|---|---|---|
| `name` | string | Workflow name |
| `stage_id` | string | Stage id |
| `attempt` | int | Attempt that just finished |
| `marker` | string | Completion marker matched in the output, or "" |
| `action` | string | "advance" / "loop" / "fail" / "redo:<id>" |
| `output_kb` | int | Output size in KB (rounded up) |

## Server-initiated requests

Requests where the server initiates and the client must respond. Use JSON-RPC `id` fields to correlate. Server uses `srv-N`-prefixed IDs to avoid collision with client-initiated requests.

### `permission/request`

Asks the user (via TUI/IDE) for interactive consent before running a sensitive tool.

Params:

```json
{"tool": "bash", "description": "go test ./...", "reason": "to verify the fix"}
```

Expected response (`result`):

```json
{"approved": true, "reason": "ok"}
```

If no client request handler is registered (`Client.SetRequestHandler` not called), the client returns method-not-found and the server falls back to the static permission gate (config `exec.confirm` / `--allow-exec`).

### `question/ask`

Interactive Q&A for the `question` tool and `plan_exit` approval (plan → build).

Params:

```json
{
  "questions": [
    {"question": "Which approach?", "options": ["A", "B"], "allow_multiple": false}
  ]
}
```

Expected response (`result`):

```json
{"answers": ["A"]}
```

TUI shows a blocking modal; CLI `apply --mode plan` uses stdin when TTY.

---

Если хочется расширять контракт — меняем `protocol.ProtocolVersion` и обновляем этот документ вместе с тестами.
