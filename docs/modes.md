# Режимы агента Orchestra

Режим задаётся флагом `--mode` команды `apply`, параметром `mode` в `agent.run` / `session.message` через JSON-RPC. Переключение **plan → build** внутри сессии — через `plan_exit` (core выполняет второй прогон автоматически при одобрении).

Источник правды: `internal/agent/agent.go` (константы `Mode*`), `internal/tools/registry.go` (`ListToolsForMode`), `internal/prompt/files/*.txt` (промпты).

---

## Реализованные режимы

### `build` — основной (по умолчанию)

**Назначение:** выполнение задачи: чтение кода, написание изменений, применение патчей.

**Инструменты:** `ls`, `read`, `glob`, `write`, `edit`, `grep`, `symbols`, `explore`, `runtime_query`, `todowrite`, `todoread`, `bash` (если `--allow-exec`), `task` + `task_spawn/wait/cancel` (если включён SubtaskRunner), `question` (если включён QuestionAsker).

**Примечание:** `plan_enter` **не** рекламируется как tool (как в OpenCode) — вход в plan только через `--mode plan` / RPC `mode: "plan"`.

**Промпты:** `build.txt`; вариации по семейству модели: `build-anthropic.txt`, `build-gpt.txt`, `build-gemini.txt`, `build-local.txt`, `build-kimi.txt`.

---

### `plan` — read-only планирование

**Назначение:** анализ задачи и составление плана в `.orchestra/plans/<session-id>.md` (или timestamped path для one-shot `agent.run`) без риска изменить код.

**Инструменты:** `ls`, `read`, `glob`, `write` (только plan-файл!), `grep`, `symbols`, `explore`, `runtime_query`, `todowrite`, `todoread`, `plan_exit`, `task` + `task_spawn/wait/cancel` (если включён), `question` (если включён).

**Ограничения:** любая запись за пределами `.orchestra/plans/*.md` (и legacy `.orchestra/plan.md`) → `PLAN_MODE_WRITE_DENIED`. `edit` полностью заблокирован.

**Переход:** `plan_exit` спрашивает пользователя о переключении в `build`; при согласии core запускает второй прогон с `JustSwitchedFromPlan` и synthetic query. RPC result: `switch_to_build` (legacy flag, обычно уже обработан in-core).

**Промпт:** `plan.txt`; `plan-local.txt` для локальных моделей.

---

### `explore` — subagent для поиска

**Назначение:** read-only исследование кодовой базы как дочерний агент. Запускается через `task` (sync) или `task_spawn` + `task_wait`.

**Инструменты:** `ls`, `read`, `glob`, `grep`, `symbols`, `task_result`.

**Ограничения:** нет записи, нет exec, нельзя порождать дальнейшие subtasks.

**Промпт:** `explore.txt`.

---

### `general` — универсальный subagent

**Назначение:** полноценный исполнитель, запускаемый родителем через `task` с `subagent_type: "general"`. Читает и пишет файлы, возвращает результат через `task_result`.

**Инструменты:** `ls`, `read`, `glob`, `write`, `edit`, `grep`, `symbols`, `explore`, `runtime_query`, `todoread`, `task_result`, `bash` (если `--allow-exec`), `task_spawn/wait/cancel` (если включён).

**Отличие от `build`:** нет `todowrite` (отслеживание прогресса — внутреннее), нет `plan_enter` / `question`; завершается через `task_result`, а не через `final.patches`.

**Промпт:** `general.txt`.

---

### `compaction` — сжатие истории (внутренний)

**Назначение:** автоматическое сжатие накопленной истории диалога в компактный текст, когда контекст приближается к лимиту. Вызывается самим агентом, не пользователем напрямую.

**Инструменты:** нет (чистый LLM-вывод).

**Промпт:** `compaction.txt`.

---

### `title` — генерация заголовка (внутренний)

**Назначение:** генерация короткого заголовка сессии/задачи по запросу пользователя. Используется для именования сессий в TUI.

**Инструменты:** нет.

**Промпт:** `title.txt`.

---

### `summary` — саммари выполненной работы (внутренний)

**Назначение:** создание краткого резюме завершённой задачи для показа пользователю или сохранения в истории.

**Инструменты:** нет.

**Промпт:** `summary.txt`.

---

## Маршрутизация промптов по семейству модели

`BuildSystemPromptForMode(mode, family)` ищет файлы в порядке:

```
{mode}-{family}.txt → {mode}.txt → build.txt
```

Поддерживаемые семейства:

| Family | Модели | Промпт-файлы |
|--------|--------|-------------|
| `anthropic` | claude-* | `build-anthropic.txt` |
| `gpt` | gpt-*, o1*, o3* | `build-gpt.txt` |
| `gemini` | gemini-* | `build-gemini.txt` |
| `kimi` | kimi-*, moonshot-* | `build-kimi.txt` |
| `local` | qwen*, llama*, mistral*, deepseek*, phi* | `build-local.txt`, `plan-local.txt` |
| `default` | всё остальное | `{mode}.txt` / `build.txt` |

`DetectPromptFamily(modelName)` / `ResolvePromptFamily` автоматически выбирает семейство по имени модели, если `llm.prompt_family` пуст.

Для **`local`** (Qwen, Llama, Nemotron, …) промпт `build-local.txt` задаёт **edit/write по умолчанию**; `final.patches` — только для редкого multi-file батча. Пользователю не нужно выбирать «патчи или физика» флагом.

### Каталог tools в system prompt

Каждый ход агент добавляет блок `<available_tools>` из **живых** tool defs (mode + caps + MCP + skills): имя + короткое описание. В user prompt остаётся краткий `<tool_names>`. Статические списки в `*.txt` больше не считаются источником правды.

---

## Инжекция напоминаний

### Max-steps reminder

При достижении 2/3 лимита шагов в историю вставляется синтетическое сообщение `role: assistant` из `max-steps.txt`. Цель: не дать модели потратить оставшиеся шаги на исследование вместо финального патча.

### Plan-mode reminder

При `JustSwitchedFromPlan=true` (переключение `plan` → `build`) в начало истории вставляется одноразовый reminder из `plan-switch.txt`.

---

## Планируемые режимы

| Режим | Статус | Описание |
|-------|--------|---------|
| `custom` через конфиг | ⏳ planned | Описать роль агента в `.orchestra.yml` без правки кода (аналог OpenCode `cfg.agent`) |
| TUI-режим | ⏳ planned | Интерактивный терминальный UI (Bubble Tea / Charmbracelet) |
| `critic` | ⏳ planned | Выделить роль Critic из pipeline в отдельный именованный режим |
| `investigator` | ⏳ planned | Выделить роль Investigator из pipeline в отдельный именованный режим |
| Fine-grained permissions | ⏳ planned | Правила allow/ask/deny per-tool и per-glob (аналог OpenCode permission ruleset) |
