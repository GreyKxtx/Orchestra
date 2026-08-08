# Pipeline Issues Audit

Дата: 2026-05-21 · **статус P0/P1 ядра: закрыто** (см. § «Исправления» внизу; регрессии — `internal/agent/audit_regression_test.go`)

Цель: зафиксировать проблемы в текущем agent pipeline Orchestra, особенно вокруг todo-list, task/subagent execution, mode switching и отличий от OpenCode.

## Источники

Проверено локально:

- `internal/agent/agent.go` — основной `Agent.Run`, prompt assembly, todo/task/plan handlers, final/apply path.
- `internal/agent/step_adapter.go` — нормализация LLM response в `StepToolCall` / `StepFinal`.
- `internal/tools/registry.go` — tool surface по режимам и parallel/mutating classification.
- `internal/tools/call.go` — dispatch обычных runner tools.
- `internal/tools/todo.go` — типы todo-list.
- `internal/tasks/tasks.go` — subtask runner.
- `internal/core/core.go` — `agent.run` и `session.message` orchestration.
- `docs/modes.md`, `docs/commands-and-modes.md`, `docs/tools-status.md`, `docs/ROADMAP.md`.

OpenCode (локальный checkout):

- `_opencode/` — полный git checkout (игнорируется Orchestra `.gitignore`, поэтому workspace search его не видит).
- Ключевые файлы для сравнения:
  - `_opencode/packages/opencode/src/agent/agent.ts` — native agents + permission rules.
  - `_opencode/packages/opencode/src/session/prompt.ts` — session loop, plan reminders, agent switch.
  - `_opencode/packages/opencode/src/session/processor.ts` — tool-call stream processing.
  - `_opencode/packages/opencode/src/session/todo.ts` — todo persistence (SQLite).
  - `_opencode/packages/opencode/src/tool/todo.ts` + `todowrite.txt` — TodoWrite tool + prompt.
  - `_opencode/packages/opencode/src/tool/task.ts` + `task.txt` — Task subagent tool.
  - `_opencode/packages/opencode/src/tool/plan.ts` + `plan-exit.txt` — plan_exit orchestration.
  - `_opencode/packages/opencode/src/tool/registry.ts` — tool surface per agent/model.
  - `_opencode/packages/opencode/src/tool/edit.ts` — immediate edit + forgiving match.

## Карта текущего pipeline Orchestra

Упрощенный поток:

1. `core.AgentRun` / `core.SessionMessage` / CLI `apply` создают `tools.Runner`, `agent.Agent`, опционально `tasks.TaskRunner`.
2. `Agent.Run` на каждом шаге собирает:
   - system prompt через `buildSystemPrompt`;
   - user prompt через `BuildUserPrompt`;
   - `<todo_list>` из `a.todos`;
   - CKG context;
   - mode reminder.
3. `nextStep` вызывает LLM и `NormalizeLLMWithDefs`.
4. Для `StepToolCall` есть две ветки:
   - parallel batch, если все tools помечены `ParallelSafe`;
   - serial pipeline, где отдельно обрабатываются `task_*`, `todo*`, `plan_*`, `question`, `skill_invoke`, а затем обычный `Runner.Call`.
5. Для `StepFinal`:
   - dry-run: `final.patches` применяются в staging overlay, затем `StagedOps` прогоняются через `FSApplyOps(DryRun=true)`;
   - apply: `final.patches` резолвятся через `resolver.ResolveExternalPatches`, затем `FSApplyOps(DryRun=false)`.
6. В `session.message` история, todos и pending ops сохраняются в session snapshot. В `agent.run` todos наружу не возвращаются и не сохраняются.

## Сравнение с OpenCode (source-to-source)

### Agents and modes

| Аспект | OpenCode (`agent.ts`) | Orchestra (`registry.go`, `agent.go`) |
|--------|----------------------|---------------------------------------|
| Конфигурация режима | Permission ruleset per agent (`allow/deny/ask` по tool + path patterns) | Статический `ListToolsForMode` + runtime guards |
| `build` | Primary; `question` + permission `plan_enter: allow` | Primary; `plan_enter` как **tool** (stub) |
| `plan` | Primary; edit deny except `.opencode/plans/*.md`; `plan_exit: allow` | Primary; write guard только для `.orchestra/plan.md` |
| `general` | Subagent; **full tools minus `todowrite`** | Режим `general` в registry есть, но `task_spawn` его **не использует** |
| `explore` | Subagent; deny `*`, allow read/grep/glob/bash/web | Режим `explore` есть; `task_spawn` всегда read-only child |
| Utility agents | `compaction`, `title`, `summary` — hidden, no tools | Аналоги есть (`ModeCompaction` и т.д.) |

**Критическое расхождение:** в OpenCode **`plan_enter` — это permission flag**, а не tool. В registry есть только `plan_exit` (и то за флагом `OPENCODE_EXPERIMENTAL_PLAN_MODE`). Orchestra рекламирует `plan_enter` как tool и возвращает `not_supported` — это не порт OpenCode, а собственная недореализованная абстракция.

### Session loop

| Аспект | OpenCode | Orchestra |
|--------|----------|-----------|
| Хранение | SQLite session parts (text/tool/reasoning/patch) | `[]llm.Message` history + optional session snapshot |
| Loop owner | `SessionPrompt.loop` → `runLoop` → `SessionProcessor.process` | `Agent.Run` for loop |
| Tool execution | AI SDK stream events, каждый tool part tracked | Serial pipeline + optional `runParallelToolBatch` |
| Repeated calls | `doom_loop` permission ask после 3 identical tool calls | `CircuitBreaker` dedup + denied/error caps |
| Disk changes | `edit`/`write` сразу; snapshot patch parts после step | dry-run staging + `final.patches` → resolver |
| Compaction | `SessionCompaction` + overflow detection | `compactHistory` + `truncateMessages` |

OpenCode session loop **всегда session-scoped**. Orchestra one-shot `agent.run` / CLI `apply` не имеют session DB и не могут воспроизвести plan→build switch так же, как OpenCode, без доработки core/TUI.

### Todo

| Аспект | OpenCode (`tool/todo.ts`, `session/todo.ts`, `todowrite.txt`) | Orchestra (`tools/todo.go`, `agent.go`) |
|--------|---------------------------------------------------------------|----------------------------------------|
| Tools | Только **`todowrite`** (нет `todoread`) | `todowrite` + `todoread` |
| Schema | `{ content, status, priority }` — **без `id`** | `{ id, content, status }` — **без `priority`** |
| Status enum | `pending`, `in_progress`, **`completed`**, `cancelled` | `pending`, `in_progress`, **`done`**, `cancelled` |
| Persistence | SQLite `todo` table per session | In-memory `a.todos`; snapshot только в `session.message` |
| Prompt injection | Нет `<todo_list>` block — модель видит todos через tool output + ACP UI events | `<todo_list>` prepended to user prompt каждый step |
| Tool prompt | Большой `todowrite.txt` (~170 строк): when/when-not, examples, one-in-progress rule | Короткое description в registry (~1 строка) |
| Permission | `ctx.ask({ permission: "todowrite" })` | Нет permission gate на todo tools |
| UI sync | ACP agent парсит `todowrite` output → PlanEntry для клиента | TUI не получает todos из RPC result |

**Вывод:** todo-list в Orchestra structurally похож, но contract расходится по status naming, полям schema и отсутствию богатого tool prompt. Это объясняет, почему модель может писать `completed`, а Orchestra ожидает `done`.

### Task / subagents

| Аспект | OpenCode (`tool/task.ts`) | Orchestra (`tasks/tasks.go`, `registry.go`) |
|--------|---------------------------|---------------------------------------------|
| Tool API | Один tool **`task`**: `description`, `prompt`, **`subagent_type`**, optional `task_id` | Три tools: **`task_spawn`**, **`task_wait`**, **`task_cancel`** |
| Child selection | `agent.get(subagent_type)` → `general` / `explore` / custom | Нет выбора — всегда `ListToolsForChild()` (read-only) |
| Child session | Отдельная session в DB, parent blocks until done | Goroutine + in-memory map |
| Resume | `task_id` продолжает ту же subagent session | Не поддерживается |
| Permissions | Derived from parent session + subagent rules; disables nested `task`/`todowrite` | Child без `SubtaskRunner` (no recursion) |
| Result format | `<task_result>...</task_result>` + `task_id: ...` | `task_result` tool для child; parent получает JSON от spawn/wait |
| `general` subagent | Full write/edit/bash (minus todowrite) | `listToolsGeneral` существует, но **не wired** в TaskRunner |

OpenCode plan mode prompt (`session/prompt.ts`) явно инструктирует:
- Phase 1: до 3 parallel **`explore`** agents;
- Phase 2: **`general`** agent(s) для design;
- Phase 5: **`plan_exit`**.

Orchestra plan prompts этого workflow не содержат — только generic mode reminder.

### Plan mode switching

| Аспект | OpenCode (`tool/plan.ts`, `session/prompt.ts`) | Orchestra (`agent.go`, `core.go`) |
|--------|-----------------------------------------------|-----------------------------------|
| Вход в plan | UI/session agent field → `agent: "plan"` (нет `plan_enter` tool) | `--mode plan` / RPC `mode: plan` |
| Plan file | `.opencode/plans/<timestamp>-<slug>.md` (или data dir без VCS) | `.orchestra/plan.md` (single fixed path) |
| Plan prompt | ~70 строк workflow (5 phases, explore/general/task guidance) в synthetic user part | Generic `PlanModeReminder` |
| Выход из plan | `plan_exit` tool → `question.ask` → synthetic user msg с `agent: "build"` | `plan_exit` → `Result.SwitchToBuild=true` (**dead end**) |
| Build switch reminder | `BUILD_SWITCH` synthetic part если `wasPlan && agent==build` | `JustSwitchedFromPlan` option exists, но **не wired** |
| Feature flag | `OPENCODE_EXPERIMENTAL_PLAN_MODE` + CLI client gate for plan tool | Нет feature flag; tools always advertised |

OpenCode `plan_exit` делает полную orchestration inline:

```typescript
// _opencode/packages/opencode/src/tool/plan.ts
yield* question.ask({ ... "Switch to build agent?" ... })
yield* session.updateMessage({ role: "user", agent: "build", ... })
yield* session.updatePart({ text: "The plan ... has been approved, you can now edit files.", synthetic: true })
```

Orchestra останавливает run с флагом, который ни `core.AgentRun`, ни `core.SessionMessage`, ни TUI не обрабатывают.

### Apply / edit path

| Аспект | OpenCode | Orchestra |
|--------|----------|-----------|
| File edits | `edit`/`write` tools пишут на диск сразу + LSP loop | `write`/`edit` → staging; apply через `final.patches` |
| Edit recovery | Forgiving match strategies в `edit.ts` (cline/gemini patterns) | Strict resolver: `StaleContent` / `AmbiguousMatch` |
| Patch artifact | Snapshot patch parts в session | `plan.json`, `.orchestra/ops`, dry-run preview |

Это **архитектурное** отличие, не баг. Но оно объясняет, почему OpenCode agent "заканчивает" plain text без `final.patches`, а Orchestra build mode ожидает staging/final path.

### P0. `AgentLogger` может вызвать panic при любом обычном tool result

В `Agent.Run` перед `Runner.Call` логирование tool call guarded:

```go
if a.opts.AgentLogger != nil {
    a.opts.AgentLogger.LogToolCall(name, len(step.Tool.Input))
}
```

Но после `Runner.Call` обе ветки вызывают `a.opts.AgentLogger.LogToolResult(...)` без nil-check.

Почему это баг:

- `core.AgentRun` и `core.SessionMessage` заполняют `AgentLogger` только если `llmClient` является `*llm.OpenAIClient`.
- В тестах, custom clients, local providers или DI logger может быть `nil`.
- Любой обычный tool success/error после `Runner.Call` может упасть panic, вместо того чтобы вернуть tool result.
- Parallel path уже имеет nil-check, serial path нет.

Гипотеза root cause: после добавления nil-guard для `LogToolCall` не был симметрично обновлен `LogToolResult`.

Нужный regression test:

- mock LLM вызывает обычный `read` или `glob`;
- `AgentLogger=nil`;
- ожидание: агент не panics, получает tool result и завершает run.

### P0. Parallel batch для in-process tools ломает `todoread`

`registry.go` помечает `todoread` как `ParallelSafe`. Если модель отдаст несколько parallel-safe calls, `NormalizeLLMWithDefs` вернет `Step.Tools`, а `Agent.Run` уйдет в `runParallelToolBatch`.

Проблема: `runParallelToolBatch` вызывает только `a.tools.Call(...)`. Но `todoread` не реализован в `Runner.Call`; он существует только как in-process handler serial pipeline.

Последствие:

- single `todoread` работает;
- batched `todoread` + `read` / `glob` / `grep` уйдет в `Runner.Call("todoread")`;
- `Runner.Call` вернет `unknown tool: todoread`;
- circuit breaker считает это tool error, хотя tool advertised как safe.

Гипотеза root cause: `todoread` классифицировали как read-only и parallel-safe, но забыли, что он не является runner tool.

Нужный regression test:

- LLM response содержит два tool calls: `todoread` и `read`;
- ожидание: оба tool results добавлены в history, `todoread` возвращает текущий todo-list.

### P1. Mixed tool batches silently drop all calls except first

`NormalizeLLMWithDefs` при нескольких tool calls делает batch только если все calls `ParallelSafe`. Если хотя бы один tool mutating, берется только первый call, остальные игнорируются. `Agent.Run` отправляет TUI event `skipped`, но не добавляет tool messages за skipped calls и не заставляет модель переэмитить их.

Почему это плохо:

- LLM часто может вызвать `read` + `edit`, `todowrite` + `read`, `glob` + `grep` + `edit` одним ответом.
- Если первым был не тот call, часть плана silently не выполняется.
- UI видит "skipped", но модель получает tool result только по первому call и может считать, что остальные уже выполнены.

Отличие от OpenCode: OpenCode session processor обрабатывает stream events tool-by-tool и хранит состояние каждого tool part; в публичном processor нет режима "drop all but first" как основного механизма execution.

Гипотеза root cause: fallback "first-call wins" был добавлен как защита от retry loops, но стал semantic-loss механизмом.

Нужный regression test:

- response с `todowrite` + `read`;
- проверить, что не теряется todo update или что модель получает явный tool error по каждому skipped call.

### P1. Todo status не совпадает с OpenCode и распространенной модельной привычкой

OpenCode `todowrite.txt` использует статусы:

- `pending`
- `in_progress`
- `completed`
- `cancelled`

Orchestra schema использует:

- `pending`
- `in_progress`
- `done`
- `cancelled`

Почему это важно:

- модели, обученные на Claude Code/OpenCode-подобных prompts, часто пишут `completed`;
- user-facing Cursor tool тоже использует `completed`;
- `tools.TodoStatus` в Go не валидирует enum на unmarshal, поэтому `completed` может попасть в runtime state, хотя schema говорит `done`;
- downstream docs/tests используют `done`, а prompt/модель могут использовать `completed`.

Гипотеза root cause: типы были названы под internal `done`, но tool prompt/экосистема ожидают `completed`.

Нужные решения перед кодом:

- выбрать канонический статус;
- если сохраняем `done`, явно обучить prompt и UI;
- если переходим на `completed`, обновить schema, tests, docs и migration/compat handling.

### P1. `todowrite` не валидирует invariants runtime

Schema требует наличие `id`, `content`, `status`, но `handleTodoTool` не проверяет:

- пустой `id`;
- пустой `content`;
- неизвестный статус;
- больше одного `in_progress`;
- отсутствие `in_progress`, когда есть активная работа;
- duplicate ids.

Почему это плохо:

- при providers без строгой tool schema валидации плохой todo-list становится частью prompt;
- модель начинает опираться на поврежденный checklist;
- session snapshot сохраняет мусор между turns.

OpenCode тоже не полностью валидирует workflow invariant в `todo.ts`, но Zod валидирует shape, а detailed `todowrite.txt` сильнее задает поведение. В Orchestra prompt для todo намного слабее.

Нужный regression test:

- `todowrite` с двумя `in_progress`;
- ожидаем structured tool error, а не принятие списка.

### P1. `plan_enter` в Orchestra — ложная абстракция (в OpenCode такого tool нет)

Orchestra `listToolsBuild` рекламирует `plan_enter` и обрабатывает его как stub. В OpenCode:

- `plan_enter` — только permission на build agent (`agent.ts:120`);
- в `tool/registry.ts` **нет** plan_enter tool, только `plan_exit` (gated by flag).

**Root cause:** при порте OpenCode modes была добавлена симметричная пара enter/exit tools, хотя OpenCode переключает plan→build через session agent field + synthetic messages, а не через enter tool.

**Fix direction:** убрать `plan_enter` из tool surface ИЛИ реализовать session-level switch (не stub tool response).

### P1. Plan workflow prompt сильно беднее OpenCode

OpenCode injects в plan mode детальный 5-phase workflow (`session/prompt.ts:285-354`): explore agents → general design → review → write plan file → plan_exit.

Orchestra plan mode получает только generic reminder из `internal/prompt/reminders.go`.

**Symptom:** модель в plan mode не знает, что нужно вызывать `task_spawn(explore)` параллельно, писать plan incrementally, завершать через `plan_exit`.

### P1. Plan file path не совпадает с OpenCode semantics

- OpenCode: per-session plan file `.opencode/plans/<created>-<slug>.md`
- Orchestra: fixed `.orchestra/plan.md`

**Risk:** docs/prompts referencing OpenCode plan workflow не бьются с Orchestra path guard.

### P1. Todo schema mismatch с OpenCode (и экосистемой)

См. таблицу выше. Дополнительно:

- OpenCode todo **без `id`** — Orchestra требует `id` в JSON schema;
- OpenCode имеет **`priority`** — Orchestra нет;
- status **`completed`** vs **`done`**.

### P1. `plan_exit` / `SwitchToBuild` — см. ниже (подтверждено local diff)

В `Agent.Run` при approved `plan_exit` возвращается `Result{SwitchToBuild: true}`. Но:

- `core.AgentRun` не читает `res.SwitchToBuild`;
- `core.SessionMessage` не читает `res.SwitchToBuild`;
- `AgentRunResult` и `SessionMessageResult` не содержат это поле;
- TUI/RPC не получает явного сигнала на restart в build mode.

Документация утверждает, что `plan_exit` переключает в build и запускает `JustSwitchedFromPlan` reminder. В текущем pipeline это фактически dead-end.

### P1. `plan_exit` выставляет `SwitchToBuild`, но callers его не используют

**OpenCode reference:** `_opencode/packages/opencode/src/tool/plan.ts` — switch happens inside tool via `session.updateMessage({ agent: "build" })`.

**Orchestra:** `agent.go:834` returns `SwitchToBuild: true`; no consumer in `core.go` or TUI.

- `session.message` в mode `plan`, mock `QuestionAsker` approve;
- ожидание: либо response содержит `switch_to_build`, либо core делает второй run в build mode с `JustSwitchedFromPlan`.

### P1. `plan_enter` advertised в build mode, но runtime всегда возвращает `not_supported`

`ListToolsForMode("build")` добавляет `plan_enter`. Но `Agent.Run` обрабатывает его как stub:

```json
{"status":"not_supported","message":"plan_enter недоступен в текущем режиме..."}
```

Почему это плохо:

- model видит tool как доступный и может тратить turns;
- docs/tools-status отмечают `plan_enter` как готовый;
- пользовательский mental model "можно перейти в plan" не соответствует коду.

Нужное решение:

- либо убрать `plan_enter` из advertised tools до реализации;
- либо добавить полноценный switch signal на уровень core/TUI, аналогично `plan_exit`.

### P1. `task_spawn` не соответствует OpenCode `task` tool

**OpenCode reference:** `_opencode/packages/opencode/src/tool/task.ts` — `subagent_type` selects `general`/`explore`/custom with full permission inheritance.

**Orchestra:** `TaskRunner.Spawn` always uses `ListToolsForChild()` read-only set.

В `registry.go` есть `listToolsGeneral`, где general agent имеет write/edit/bash capabilities и завершение через `task_result`. Но `TaskRunner.Spawn` всегда использует `ListToolsForChild`, где есть только read-only tools + `task_result`.

Почему это плохо:

- docs говорят, что `general` subagent может делать многошаговую работу с write tools;
- actual `task_spawn` годится только для исследования;
- модель может делегировать "исправь это" и получить child, который не способен исправлять;
- итоговый fallback `completed with N patch(es)` маскирует неправильное завершение child без `task_result`.

OpenCode task tool принимает `subagent_type` и реально запускает выбранного agent. Orchestra `task_spawn` не принимает тип.

Нужный design decision:

- оставить `task_spawn` только как research tool и переименовать/описать это;
- или добавить `subagent_type` и permission-derived tool surface.

### P2. `task_wait` timeout сообщает `cancelled`, но task продолжает работать

`TaskRunner.Wait` при timeout возвращает `SubtaskResult{Status:"cancelled", Error:"wait timeout"}`, но не вызывает `entry.cancel()`.

Почему это плохо:

- parent agent получает "cancelled" и может считать работу остановленной;
- goroutine продолжает расходовать LLM/tool budget;
- позже `task_wait` может вернуть done/error, что противоречит предыдущему статусу.

Нужный regression test:

- запустить blocking child;
- `task_wait(timeout_ms=1)`;
- проверить, должен ли task быть реально cancelled или status должен быть `timeout` / `running`.

### P2. `TaskRunner.tasks` не чистит завершенные entries

`TaskRunner` хранит tasks в map и никогда не удаляет done/cancelled tasks.

Почему это плохо:

- long-lived core/TUI session может накопить tasks;
- repeated background/subagent usage приводит к memory leak;
- нет TTL или explicit cleanup.

Нужное решение:

- cleanup при `Wait` после terminal result;
- или TTL/retention policy, если нужен resume.

### P2. `agent.run` / `session.message` не возвращают todo state наружу

`Agent.Result` содержит `Todos`, но:

- `AgentRunResult` не содержит todos;
- `SessionMessageResult` не содержит todos;
- только `session.message` сохраняет todos в snapshot.

Почему это плохо:

- TUI/RPC caller не может надежно отрисовать todo-list как first-class state;
- one-shot `agent.run` теряет todo-list полностью;
- debugging todo behavior требует читать session snapshot, а не response.

OpenCode возвращает todos как tool metadata and session state; UI может строиться вокруг persisted session parts.

Нужное решение:

- добавить todos в protocol result/events;
- или явно задокументировать, что todo-list является только model-private prompt state.

### P2. Alias normalization заявлена в комментарии, но не реализована

В serial path есть комментарий:

```go
// Normalize LLM-facing aliases (read, bash, edit, todowrite, task_result, …)
// to canonical names ...
```

Но сразу после него идет только извлечение `toolCallID`. Реальной нормализации нет.

Почему это риск:

- если prompts/docs используют `todo.write`, `task.result`, `fs.read`, `exec.run`, model может вызвать alias и получить `unknown tool`;
- permission checks и in-process handlers ожидают short names (`todowrite`, `task_result`, `read`, `bash`).

Нужное решение:

- либо удалить misleading comment и унифицировать docs на short names;
- либо реализовать `NormalizeToolName` с tests для aliases.

## Документация — актуальность (2026-08)

- **Режимы / Planner–Worker:** [`modes.md`](./modes.md), [`architecture/planner-worker.md`](./architecture/planner-worker.md).
- **TUI pipeline:** [`architecture/tui-pipeline.md`](./architecture/tui-pipeline.md) (M1–M4 done).
- **Удалено как устаревшее:** `subproject-3b-roles-modes.md`, `architecture/tui-pipeline-to-be.md`.

Ниже — исторический аудит расхождений с OpenCode; часть пунктов закрыта в коде, сверяйте с `modes.md` перед правками.

## Приоритет проверок

Перед исправлениями стоит покрыть тестами именно поведение pipeline:

1. Serial tool success/error with `AgentLogger=nil` must not panic.
2. Parallel batch with `todoread` must not go through `Runner.Call`.
3. Mixed tool batch must not silently lose calls without model-visible result.
4. `todowrite` should reject invalid statuses and multiple `in_progress`.
5. Decide `done` vs `completed`, then test schema + prompt + session persistence.
6. `plan_exit` approved path must either return protocol-level `switch_to_build` or perform build restart.
7. `plan_enter` must either be implemented or removed from build tool list/docs.
8. `task_spawn` must either accept `subagent_type` or be documented as read-only research task.
9. `task_wait` timeout semantics must be renamed to `timeout/running` or actually cancel the task.
10. `AgentRunResult` / `SessionMessageResult` should expose todos if UI is expected to show them.

## Предлагаемый порядок исправлений

1. Стабилизировать crash/loop risks:
   - nil-guard `AgentLogger.LogToolResult`;
   - fix `todoread` parallel path;
   - stop silent mixed-batch drops.
2. Уточнить todo contract:
   - выбрать `completed` или `done`;
   - усилить `todowrite` prompt;
   - добавить runtime validation.
3. Развести mode switching:
   - либо убрать advertised `plan_enter`;
   - либо провести `SwitchToBuild` через core/session/TUI.
4. Перепроектировать subagents:
   - добавить `subagent_type`;
   - отделить `explore` read-only от `general` write-capable;
   - решить resume/background story.
5. Обновить docs после кода:
   - `docs/modes.md`;
   - `docs/tools-status.md`;
   - `docs/commands-and-modes.md`;
   - `docs/PROTOCOL.md`, если меняется wire contract.

## Открытые вопросы

1. ~~Где лежит локальный checkout OpenCode?~~ — **`_opencode/`** (resolved).
2. Todo status: мигрировать Orchestra на OpenCode `completed` + убрать `id`/`priority` mismatch, или адаптировать prompts под `done`?
3. Task API: портировать OpenCode unified `task` tool с `subagent_type`, или оставить spawn/wait/cancel но добавить type selection?
4. Plan mode: портировать OpenCode 5-phase prompt + per-session plan path, или упростить до fixed `.orchestra/plan.md`?
5. `plan_enter`: удалить как tool (как в OpenCode) и переключать plan только через CLI/RPC mode?
6. `plan_exit`: повторить OpenCode pattern (synthetic user message + second run in build) или добавить protocol field `switch_to_build`?
7. Нужен ли `todoread` (Orchestra-only) или достаточно OpenCode-style только `todowrite` + prompt injection?

## Исправления (2026-05-21)

Все пункты из «Предлагаемый порядок исправлений» (1–4) и критические P0/P1 из чеклиста реализованы:

| # | Проблема | Решение |
|---|----------|---------|
| 1 | `AgentLogger` nil panic на `LogToolResult` | Nil-guard в `runSerialToolCall` (`internal/agent/tool_dispatch.go`) |
| 2 | Parallel batch + in-process tools | `todoread`/`task_result` убраны из `parallelSafeTools`; `allParallelSafeCalls` блокирует in-process tools |
| 3 | Mixed batch drops calls | `step_adapter` сохраняет все calls; serial loop по всем через `runSerialToolCall` |
| 4 | `SwitchToBuild` dead end | Core: второй прогон в `build` после `plan_exit` (`internal/core/agent_run.go`); поле `switch_to_build` в RPC results |
| 5 | `question` без asker в core/TUI | RPC `question/ask` + `rpcQuestionAsker`; TUI `QuestionModal` |
| 6 | `task_spawn` всегда read-only | `subagent_type` (`explore`/`general`); tools через `ListToolsForChild` / `ListToolsForMode` |
| 7 | `task_wait` timeout → `cancelled` | Timeout вызывает `entry.cancel()`, статус `"timeout"` |
| 8 | Todo schema mismatch | `ValidateTodos`: `completed`→`done`, id uniqueness, ≤1 `in_progress`; todos в RPC results |
| 9 | `plan_enter` stub в build | Убран из `listToolsBuild`; fallback handler оставлен для legacy calls |

Проверка: `go vet ./...`, `go test ./...` — OK.

**Остаётся открытым (P2 / архитектура):** ~~unified `task` tool~~, ~~per-session plan path~~, ~~усиление todo prompt~~, ~~обновление PROTOCOL/modes~~ — **сделано 2026-05-21 (ToolsVersion 11)**.

### P2 (2026-05-21)

| Область | Решение |
|---------|---------|
| Unified `task` | `task` tool: sync spawn+wait; `task_spawn/wait/cancel` для async |
| Plan path | `.orchestra/plans/<session-id>.md`; legacy `.orchestra/plan.md` supported |
| Todo prompt | `internal/prompt/files/todowrite.txt`; schema accepts `completed` |
| Docs | `PROTOCOL.md`, `modes.md`, `tools-status.md` updated |
