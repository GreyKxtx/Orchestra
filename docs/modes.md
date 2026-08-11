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

**Инструменты (top-level Tab):** `ls`, `read`, `glob`, `grep`, `symbols`, `explore`, `repo_map`, LSP (без rename), `runtime_query`, `question`.
**Child:** тот же набор + `task_result` (только у дочернего агента).

**Ограничения:** нет записи, нет exec; nested spawn — только если режим child это допускает.

**Промпт:** `explore.txt`.

---

### `general` — универсальный subagent

**Назначение:** полноценный исполнитель, запускаемый родителем через `task` с `subagent_type: "general"`. Читает и пишет файлы, возвращает результат через `task_result`.

**Инструменты:** `ls`, `read`, `glob`, `write`, `edit`, `grep`, `symbols`, `explore`, `runtime_query`, `todoread`, `task_result`, `bash` (если `--allow-exec`), `task_spawn/wait/cancel` (если включён).

**Отличие от `build`:** нет `todowrite` (отслеживание прогресса — внутреннее), нет `plan_enter` / `question`; завершается через `task_result`, а не через `final.patches`.

**Промпт:** `general.txt`.

> **Эволюция:** `general` остаётся универсальным child; для Orchestra Lead используйте `subagent_type: worker` + `tier` (и специализированные `ask` / `debug` / `architecture`).

---

### `ask` — вопросы по коду (read-only)

**Назначение:** объяснить код / ответить на вопрос без правок. Доступен в Tab и как `subagent_type` для Lead.

**Инструменты (top-level):** `ls`, `read`, `glob`, `grep`, `symbols`, `explore`, LSP (без rename), `question`.
**Child:** + `task_result`.

**Ограничения:** write/edit/bash/`skill_invoke` запрещены (tools + runtime guard). Final-guard не требует правок кода.

**Промпт:** `ask.txt`.

---

### `debug` — поиск причины + точечный фикс

**Назначение:** root cause с evidence; узкий фикс сам или через `worker`. Tab + `subagent_type`.

**Инструменты (top-level):** почти как `build` (read/write/edit, LSP, todos, task spawn).
**Child:** + `task_result`.

**Промпт:** `debug.txt`.

---

### `architecture` — дизайн без production-правок

**Назначение:** границы модулей, потоки, риски; план в `.orchestra/plans/*.md`. Tab + `subagent_type`.

**Инструменты:** как `plan` + research spawn (`task`) + `plan_exit`; write только в plan-пути.

**Промпт:** `architecture.txt` (`{{PLAN_PATH}}`).

---

### `agent` — auto-route (top-level)

**Назначение:** один режим «сам реши»: классификатор выбирает `build` | `plan` | `explore` | `ask` на старте хода.

**Конфиг:**
```yaml
auto_router:
  enabled: true          # default true
  provider: ""           # optional named providers: entry; empty → llm.router.fast_provider или main
  model: ""
```

**Поведение:** `internal/autorouter` — LLM JSON one-shot + keyword heuristic fallback (explain/what-is → `ask`). Auto-router **не** выбирает `orchestra`. Explicit `--mode build|plan|explore|ask` побеждает.

**TUI:** Tab включает `agent`; badge `agent→build` (effective mode) после классификации.

---

### `orchestra` — Lead planner (top-level, opt-in)

**Назначение:** Lead декомпозирует задачу и спавнит Workers по tiers; production `edit`/`write` запрещены (как plan — только plan md).

**Конфиг:**
```yaml
orchestra:
  planner:
    provider: lmstudio-reason
    model: qwen3.6-27b
  tiers:
    - name: complex
      provider: lmstudio-coder
      model: deepseek-coder-33b
    - name: focused
      provider: lmstudio-mid
      model: qwen-coder-14b
    - name: micro
      provider: lmstudio-fast
      model: qwen-7b
  default_tier: focused
  max_worker_retries: 3
```

**Инструменты:** research + plan write + `task`/`task_spawn` (`worker|ask|debug|architecture|explore`, `tier=…` для worker) + `question`.

**Промпт:** `orchestra.txt`. TUI: `/orchestra` — настройки planner/tiers; badge `orchestra · lead`. Tab: `… → agent → orchestra`.

**Маршрутизация по tiers (L1–L5, Fab 5):** [architecture/orchestra-routing.md](./architecture/orchestra-routing.md).

---

### `worker` — atomic WorkOrder executor (child only)

**Назначение:** один WorkOrder → `edit`/`write` + LSP validation loop → `task_result`. Без nested spawn.

**Контекст (скорость):** worker **не** получает историю диалога родителя. Только focused goal: JSON WorkOrder (`intent` / `instructions` / `acceptance_criteria` / `constraints`). Plain-text prompt оборачивается в WorkOrder с дефолтными критериями (`FormatChildGoal`). Session memory и большой prompt budget отключены/урезаны.

**Инструменты:** fs read/write/edit, grep/symbols/explore, LSP, `task_result` (+ bash если caps).

**Промпт:** `worker.txt`. LLM из `orchestra.tiers[tier]` (или явные `provider`/`model` в spawn).

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
| **Planner–Worker (`orchestra` + `worker`)** | ✅ **MVP** | Lead (`mode=orchestra`) + Workers (`subagent_type=worker`, tiers). См. [architecture/planner-worker.md](./architecture/planner-worker.md) |
| `ask` / `debug` / `architecture` | ✅ done | Специализации (Tab + subagent для Lead); без MCP |
| `agent` auto-router | ✅ done | Классификатор → build\|plan\|explore |
| `custom` через `agents:` | ✅ done | Именованные агенты в `.orchestra.yml` |
| TUI-режим | ✅ done | Tab cycle + `/orchestra` settings |
| `critic` | ⏳ planned | Выделить роль Critic из pipeline в отдельный именованный режим |
| `investigator` | ⏳ planned | Выделить роль Investigator из pipeline в отдельный именованный режим |
| Fine-grained permissions | partial | `permissions.rules` allow/deny; ask-mode в TUI для shell |

> Основной сценарий локального стека: Lead в TUI (`mode=orchestra`) + Workers через `task`.
