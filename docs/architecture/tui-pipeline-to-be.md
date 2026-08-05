# TUI Pipeline — To-Be Architecture

Целевая архитектура и план миграции. Реализация multi-turn TUI **не входит** в текущий эпик (только документация); Patch Mode и Adaptive Profiles — реализуются сейчас.

## 1. Сравнение паттернов

| Паттерн | Как уже проявляется в Orchestra | Что добавить |
|---------|----------------------------------|--------------|
| Actor Model | Bubble Tea: single-threaded `Update` + Cmd; Core — один процесс на workspace | Явная модель «Session Actor» на стороне core (один writer истории) |
| Event-Driven | JSON-RPC notifications `agent/event`, `exec/output_chunk`, workflow stages | Единый event envelope с `session_id` + `turn_id` |
| State machine | Разрозненные флаги: `agentBusy`, `pendingOps`, `activeCancel`, modals | Turn FSM: `idle → composing → running → applying → done/error` |
| CQRS-lite | Dry-run = query/preview; `--apply` = command | Patch Mode как третья команда: «материализовать unified diff без write» |

Вывод: не нужна полная переписка на actors/broker. Нужно **соединить** существующий multi-turn core path с TUI и **убрать** дублирование session stores.

## 2. Целевая схема

```mermaid
sequenceDiagram
  participant User
  participant TUI as BubbleTea_App
  participant Store as UnifiedSessionStore
  participant RPC as rpcclient
  participant Core as Core_SessionMessage
  participant Agent as Agent_Run

  User->>TUI: first message
  TUI->>RPC: session.start
  RPC->>Core: session.start
  Core-->>TUI: session_id
  TUI->>Store: bind UI id == core session_id

  User->>TUI: Enter (query)
  TUI->>TUI: turn FSM: composing then running
  TUI->>Store: append UI message + persist
  TUI->>RPC: session.message(session_id, content, apply, profile)
  RPC->>Core: SessionMessage
  Core->>Agent: Run(ctx, inHistory, content)
  Agent-->>RPC: agent/event (with session_id)
  RPC-->>TUI: render deltas
  Core->>Core: AppendHistory + Snapshot unified schema
  Core-->>TUI: SessionMessageResult
  TUI->>TUI: turn FSM: done
  TUI->>Store: persist UI projection
```

### Unified session file (v2)

Один файл `.orchestra/sessions/<id>.json`:

```json
{
  "version": 2,
  "id": "...",
  "title": "...",
  "model": "...",
  "created_at": "...",
  "updated_at": "...",
  "ui_messages": [ /* projection for chat viewport */ ],
  "history": [ /* llm.Message — agent memory */ ],
  "todos": [],
  "pending_ops": [],
  "plan_path": "",
  "profile": "fast|precision|",
  "apply_output": "disk|patch"
}
```

Правила:

- Core — writer durable state (`history`, todos, pending_ops).
- TUI — writer `ui_messages` / title; при конфликте merge по `updated_at` + version field.
- Load session восстанавливает **и** UI, **и** agent history (через `session.start` с known id / `LoadOrCreate`).

### Turn state machine (TUI)

```mermaid
stateDiagram-v2
  [*] --> Idle
  Idle --> Composing: keystrokes
  Composing --> Idle: clear_input
  Composing --> Running: Enter
  Running --> Applying: pending_ops_and_user_apply
  Running --> Done: EventAgentRunCompleted
  Running --> Error: EventError
  Applying --> Done: ops.apply_ok
  Applying --> Error: ops.apply_fail
  Done --> Idle
  Error --> Idle
  Running --> Idle: cancel
```

Заменяет ad-hoc `agentBusy` + modal flags как единственный источник истины для input gating.

## 3. Application strategies (реализуется в этом эпике)

```yaml
apply:
  output: disk   # or patch
  patch_dir: .orchestra/patches
```

| Mode | Поведение |
|------|-----------|
| `disk` (default) | Как сейчас: dry-run без `--apply`; с `--apply` — write + `.orchestra.bak` |
| `patch` | Agent всегда считает dry-run для диска; после получения diffs — запись unified `.patch` (`git apply`-совместимый) в `patch_dir` |

CLI: `--output-patch [path]` перекрывает конфиг. Не путать с `--mode plan` и skill PATCH-ONLY.

## 4. Adaptive execution profiles (реализуется в этом эпике)

```yaml
agent:
  profile: ""   # fast | precision | empty=defaults
```

| Profile | MaxSteps | Context | Timeout | Tools | ResponseFormat |
|---------|----------|---------|---------|-------|----------------|
| *(empty)* | config defaults (24) | `limits.context_kb` | `llm.timeout_s` | mode toolset | config |
| `fast` | 10 | 32 KiB | 60s | без LSP/browser | без schema force |
| `precision` | 36 | 128 KiB | 300s | полный + LSP | `json_schema` если доступно |

Приоритет: CLI `--profile` > named `agents:` override > `agent.profile` config > defaults.

Поля `profile` / `apply_output` зеркалятся в `AgentRunParams` / `SessionMessageParams`.

## 5. План миграции (вне текущего эпика)

### Phase M1 — Wire TUI to session.* — DONE upstream (`2bd96e6`)

1. ~~На старте TUI: `session.start`~~ — реализовано (`ensureCoreSession`).
2. ~~Заменить `AgentRun` на `SessionMessage`~~ — реализовано в `app_session.go` / `rpcclient`.
3. Cancel / fallback на `agent.run` при отсутствии session id — на месте.
4. Тесты multi-turn — желательно добить в follow-up.

### Phase M2 — Unify session schema

1. Ввести `version: 2` snapshot в core persist.
2. Migrator: UI-only v1 records → v2 с пустым `history`; core v1 → v2 с пустым `ui_messages`.
3. Deprecate `internal/sessionstore` как отдельную схему (оставить thin helper для title/list).
4. Документировать в PROTOCOL.md; bump `ProtocolVersion`.

### Phase M3 — Turn FSM + polish

1. Ввести явный `TurnState` в `ui/tui/state`.
2. Gating input/cancel/apply через FSM.
3. Optional: backpressure policy для events (coalesce deltas).

### Phase M4 — Hardening

1. `SessionMessage` под тем же `runMu`/staging contract, что `AgentRun`.
2. Cross-process flock documentation / fail-closed.
3. `go test -race` на Linux CI для session/tasks/tui packages.

## 6. Не-цели текущего эпика

- Переключение TUI на `session.message` (только план выше).
- Слияние session schema v2 (только дизайн).
- Удаление dual path `final.patches` vs `edit`/`write` (см. ROADMAP).
