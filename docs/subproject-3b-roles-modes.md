# Sub-project 3B — Roles & Modes (Plan / Build / Explore)

**Статус:** Design doc  
**Дата:** 2026-05-04  
**Предыстория:** 3A (CKG Pre-injection) готов. Этот документ описывает, что мы берём из OpenCode, что адаптируем, и как это ложится на нашу архитектуру.

---

## 1. Что мы нашли в OpenCode

### 1.1 Роли агентов

OpenCode имеет **4 именованных агента** + 3 скрытых служебных:

| Агент | Режим | Инструменты | Назначение |
|-------|-------|-------------|-----------|
| **build** | primary | всё | Дефолт. Пишет код, запускает команды |
| **plan** | primary | read-only (+ запись в `.opencode/plans/*.md`) | Анализ, составление плана |
| **general** | subagent | всё кроме `todowrite` | Многошаговые исследования |
| **explore** | subagent | `grep, glob, list, bash, read` only | Быстрый поиск по кодовой базе |

Скрытые: `compaction`, `title`, `summary` — служебные, не интересны.

### 1.2 Главного «мозга» нет

Никакого центрального диспетчера. Маршрутизация — три механизма:

1. **User-driven**: Tab циклически переключает primary-агентов (build ↔ plan)
2. **Tool-driven**: `task` инструмент порождает дочернюю сессию с нужным субагентом. Родитель блокируется до завершения
3. **Agent-switch tool**: `plan_exit` записывает синтетическое сообщение с `agent: "build"`, следующая итерация цикла подхватывает нового агента

### 1.3 Системные промты

OpenCode не кладёт режимный контекст в system prompt. Вместо этого — **«reminders»**, которые инжектируются в последнее user-сообщение перед `llm.Complete()`.

**Plan mode reminder** (`plan.txt`):
```
CRITICAL: Plan mode ACTIVE - you are in READ-ONLY phase. STRICTLY FORBIDDEN:
ANY file edits, modifications, or system changes.
Your responsibility: think, read, search, build a well-formed plan.
Ask clarifying questions when weighing tradeoffs.
```

**Build-switch reminder** (`build-switch.txt`):
```
Your operational mode has changed from plan to build.
You are no longer in read-only mode.
You are permitted to make file changes, run shell commands.
```

### 1.4 Инструмент `question`

Позволяет агенту **заблокировать выполнение** и задать пользователю структурированные вопросы с вариантами ответа. Реализован через `Effect.Deferred` (аналог канала в Go):

1. LLM эмитит `question` tool call со списком вопросов
2. Тул публикует событие `Question.Asked`, **блокирует** файбер на Deferred
3. UI рендерит диалог с вариантами
4. Пользователь отвечает → `Question.reply()` разрешает Deferred
5. Тул возвращает ответы в модель: `"question"="answer"`, выполнение продолжается

### 1.5 Переход Plan → Build

`plan_exit` тул:
1. Вызывает `question.ask()`: «План готов. Переключиться на build?» (Yes/No)
2. На «No» — остаётся в plan
3. На «Yes» — пишет синтетическое user-сообщение с `agent: "build"` + текст: «Plan approved. Execute the plan»
4. Следующий цикл читает сообщение, загружает build-агента, инжектирует `build-switch.txt`

---

## 2. Что адаптируем для Orchestra (Go)

### 2.1 Соответствие концепций

| OpenCode (TS) | Orchestra (Go) | Статус |
|---|---|---|
| `build` agent | `Options{Mode: "build"}` + полный `ListTools()` | Нужно добавить |
| `plan` agent | `Options{Mode: "plan"}` + `ListToolsReadOnly()` | Нужно добавить |
| `explore` subagent | `ListToolsForChild()` (уже есть, урезанный сет) | Уже есть! |
| `general` subagent | Текущий дочерний агент через `task.spawn` | Уже есть! |
| Permission system | `toolFilter(mode string)` в `tools.Runner.Call()` | Нужно добавить |
| `question` tool | Новый `tools.Question` + канал блокировки | Нужно добавить |
| `plan_exit` tool | `toolPlanExit()` + синтетическое сообщение | Нужно добавить |
| `plan_enter` tool | `toolPlanEnter()` | Нужно добавить |
| Reminders (injected) | Append к userPrompt в `nextStep()` | Нужно добавить |
| CKG context | `a.ckgContext` (✅ **готово в 3A**) | Готово |

### 2.2 Новые значения `PromptFamily` → `AgentMode`

Переименовываем концепцию. Сейчас `PromptFamily` — это семейство модели (qwen/llama/etc). Режим агента — отдельное поле `Mode string` в `Options`.

```go
// Options.Mode values:
const (
    ModeBuild   = "build"   // default, full tool access
    ModePlan    = "plan"    // read-only + plan tools
    ModeExplore = "explore" // grep/glob/list/read only (subagent)
)
```

### 2.3 Инструменты по режимам

**Build** (текущий default):
- Все инструменты: `fs.*`, `search.text`, `code.symbols`, `explore_codebase`, `runtime.query`, `exec.run` (если разрешён), `todo.*`, `task.*`
- Новые: `plan_enter`, `question`

**Plan** (read-only):
- Только: `fs.list`, `fs.read`, `fs.glob`, `search.text`, `code.symbols`, `explore_codebase`, `runtime.query`, `todo.*`, `task.*`
- Запрещены: `fs.write`, `fs.edit`, `exec.run`
- Новые: `plan_exit`, `question`

**Explore** (субагент, уже близко к `ListToolsForChild`):
- Только: `fs.list`, `fs.read`, `fs.glob`, `search.text`, `code.symbols`, `task.result`
- Без `explore_codebase` и `runtime.query` (оставить для скорости)

---

## 3. Инструмент `question` — архитектура для Go

OpenCode использует `Effect.Deferred` (Haskell-style async). В Go — канал + callback:

```go
// tools/question.go

type QuestionItem struct {
    Question string   `json:"question"`
    Options  []string `json:"options,omitempty"` // nil = free text
    Multiple bool     `json:"multiple,omitempty"`
}

type QuestionRequest struct {
    Questions []QuestionItem `json:"questions"`
}

type QuestionResponse struct {
    Answers []string `json:"answers"`
}

// QuestionAsker is implemented by the CLI/RPC layer; blocks until user replies.
type QuestionAsker interface {
    Ask(ctx context.Context, questions []QuestionItem) ([]string, error)
}
```

В `Options` добавляем:
```go
QuestionAsker tools.QuestionAsker // nil = автоответ "no"
```

В `tools.Runner.Call()` при вызове `question`:
1. Вызываем `r.questionAsker.Ask(ctx, questions)` → блокируемся
2. Возвращаем ответы как строку в tool result

**Для CLI** (`orchestra apply`): простейший `StdinQuestionAsker`, читающий из stdin.  
**Для JSON-RPC** (`orchestra core`): `RPCQuestionAsker`, отправляющий метод `agent.question` через обратный канал.

---

## 4. Reminders — как инжектировать

OpenCode добавляет reminder к **последнему user-сообщению** (не в system prompt), чтобы он был «свежим» для модели с attention-bias к концу контекста.

В нашем `nextStep()`:

```go
// После buildUserPrompt, перед buildMessages:
if reminder := modeReminder(a.opts.Mode); reminder != "" {
    userPrompt += "\n\n" + reminder
}
```

```go
func modeReminder(mode string) string {
    switch mode {
    case ModePlan:
        return planModeReminder  // константа из prompt package
    case ModeBuild:
        if wasPlan { // нужен флаг "только что переключились"
            return buildSwitchReminder
        }
    }
    return ""
}
```

Флаг «только что переключились» — `a.opts.JustSwitchedToBuild bool`, выставляется при создании агента через `agent.New(..., opts)`.

---

## 5. Переход Plan → Build в нашей архитектуре

OpenCode использует синтетические сообщения в БД. У нас всё проще — история в памяти:

**Вариант A (рекомендуемый):** `plan_exit` возвращает специальное значение через `SubtaskResult`:

```go
// В agent loop: если tool result содержит sentinel "PLAN_EXIT:approved"
// — прерываем цикл с Result.SwitchToBuild = true
// Вызывающий код (core.AgentRun) перезапускает агента в Mode: "build"
// с той же историей + inject build-switch reminder
```

**Вариант B (упрощённый MVP):** CLI-флаг `orchestra apply --plan-only` делает plan-run.  
Пользователь видит план, нажимает `orchestra apply --from-plan plan.json --apply` для build-run.  
Это уже **частично работает** (`--plan-only` и `--from-plan` уже есть!).

**Рекомендация для MVP**: реализовать Вариант B как базовый (используем существующий `--plan-only`), добавить `question` tool для уточнений в план-режиме. Вариант A (автоматический переход) — следующая итерация.

---

## 6. CKG Pre-injection × Роли (синергия)

С готовым 3A инжекция работает для **всех режимов** автоматически. Но можно тюнить:

| Режим | CKG Pre-injection | Ценность |
|-------|-----------------|----------|
| Plan | ✅ always (уже есть) | Агент сразу видит что менять |
| Build | ✅ always (уже есть) | Точечные правки без лишних read |
| Explore | ❌ нет (субагент) | Субагент сам находит |

---

## 7. Промты — что адаптировать из OpenCode

OpenCode промты заточены под `grep`, `cat`, `sed` (bash). Нам нужно переписать под наши инструменты.

### 7.1 Plan mode system prompt (адаптация)

```
Ты — агент в режиме ПЛАНИРОВАНИЯ (read-only).

СТРОГО ЗАПРЕЩЕНО: fs.write, fs.edit, exec.run — даже если пользователь просит.
Разрешено: fs.read, fs.list, fs.glob, search.text, code.symbols, explore_codebase,
           runtime.query, task.spawn, question, plan_exit.

Твоя задача:
1. Изучи кодовую базу через fs.read / search.text / explore_codebase / runtime.query
2. Используй <ckg_context> — это символы, связанные с задачей пользователя
3. Задавай уточняющие вопросы через question когда нужны трейдоффы
4. Напиши архитектурный план в .orchestra/plan.md через fs.write
   (это единственный fs.write, который разрешён в plan-режиме)
5. Когда план готов — вызови plan_exit для передачи в build-агент

ФОРМАТ ПЛАНА:
## Цель
## Изменяемые файлы (с FQN функций)
## Порядок изменений
## Риски и зависимости
```

### 7.2 Build mode system prompt (адаптация)

Текущий системный промт уже подходит. Добавляем:
```
Используй explore_codebase для поиска символов по имени вместо поиска grep по контенту.
Используй runtime.query для диагностики проблем в production по trace_id.
Если нужно исследовать большой участок — используй task.spawn с explore-агентом.
```

### 7.3 Explore subagent prompt (новый)

```
Ты — исследователь кодовой базы (read-only субагент).

Твои инструменты: fs.read, fs.list, fs.glob, search.text, code.symbols.
Когда закончил — вызови task.result с кратким структурированным ответом.
Не объясняй что делаешь — только результат.
```

---

## 8. План реализации (последовательность)

### Этап 1 — Mode infrastructure (2-3 часа)
- [ ] Добавить `Mode string` в `agent.Options`  
- [ ] `ListToolsForMode(mode, allowExec)` в `tools/registry.go`  
- [ ] Mode reminder в `nextStep()` (plan + build-switch)  
- [ ] Mode-specific system prompt в `BuildSystemPromptForMode(mode, family)`  

### Этап 2 — `question` tool (2-3 часа)
- [ ] `QuestionAsker` интерфейс + `StdinQuestionAsker`  
- [ ] `toolQuestion()` в registry, `Runner.Question()` handler  
- [ ] Добавить `QuestionAsker` в `Runner` и `Options`  
- [ ] Тест с mock QuestionAsker  

### Этап 3 — Plan/Build tools (1-2 часа)
- [ ] `toolPlanExit()` — sentinel в SubtaskResult  
- [ ] `toolPlanEnter()` — переключение в plan-режим  
- [ ] `Agent.Run()` обрабатывает `Result.SwitchMode`  

### Этап 4 — CLI интеграция (1 час)
- [ ] `orchestra apply --mode plan` / `--mode build`  
- [ ] `--mode plan` пишет `.orchestra/plan.md` по завершении  
- [ ] Обновить help  

### Этап 5 — Тесты и промты (1-2 часа)
- [ ] Integration test: plan-режим не может вызвать fs.write (кроме plan.md)  
- [ ] Integration test: question tool с mock asker  
- [ ] Финальная полировка промтов  

---

## 9. Открытые вопросы

1. **`question` в JSON-RPC режиме**: как `orchestra core` отправляет вопрос клиенту? Нужен новый метод протокола `agent.question` (request→response) или через events. Отложить на Этап 4.

2. **Plan file location**: `.orchestra/plan.md` или через `--output` флаг? Пока — фиксированный путь.

3. **`plan_exit` в CLI**: в non-interactive режиме (CI) нет пользователя. `plan_exit` должен auto-approve или вернуть ошибку? → auto-approve в batch-режиме.

4. **Автоматический plan → build**: Вариант A (пересоздать агента с Mode: "build") потребует изменений в `core.AgentRun`. Оставляем на後 этап после MVP.

---

## 10. Что НЕ берём из OpenCode

| Функция OpenCode | Причина отказа |
|---|---|
| TypeScript/Ink TUI | Несовместимо с Go |
| LSP tool (20+ lang servers) | Слишком тяжело для MVP; CKG покрывает базовую навигацию |
| AI SDK (75+ провайдеров) | У нас свой `llm.Client` интерфейс |
| `compaction`/`title`/`summary` agents | Уже есть `truncateMessages` и промт-компрессия |
| Web UI + WebSocket | Out of scope |
| Permission system (динамический) | Заменяем статическим `ListToolsForMode()` — проще и достаточно |
