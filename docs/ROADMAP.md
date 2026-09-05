# Orchestra — Roadmap до удобного CLI под локальные модели

**Цель:** довести Orchestra до состояния "Claude Code для локальных моделей" — интерактивный CLI, через который можно реально вести разработку небольшого проекта на локальной модели (vLLM / lm-studio / llama.cpp / ollama). Финальная контрольная точка — построить с его помощью тестовый проект и зафиксировать, что работает, а что нет.

Документ упорядочен по приоритету. **MVP-срез** (фазы 0–4) — это минимум, после которого можно идти на полевые испытания. Фазы 5+ — расширение и шлифовка.

---

## Принципы (держим сквозь все фазы)

1. **Stateless ядро + stateful сессия снаружи.** `Agent` не накапливает состояние между ходами, история живёт в `Session`. Это нужно, чтобы один и тот же код обслуживал и `apply` (один ход), и multi-turn клиенты (TUI, VS Code, IDE).
2. **Провайдер-нейтральность изнутри `llm/`.** OpenAI-семантика (`tool` role, `tool_call_id`, `ToolCalls` на assistant) — внутренний lingua franca. Не утекает в `agent`/`core`/`patch/ops`.
3. **Защитные слои не убираем.** External Patches → Resolver → Internal Ops с `file_hash`/anchors/atomic write — это моат под локалки. Любая оптимизация скорости не должна снимать эти инварианты.
4. **JSON-RPC контракт расширяется аддитивно.** Новые методы — да; ломать существующие `initialize`/`agent.run`/`tool.call` — нет. Версии (`protocol/ops/tools`) бампим осознанно.
5. **Тесты вперёд кода.** Каждая фаза заканчивается зелёными `go test ./...` и `go vet ./...`, и Linux, и Windows (как в CI).

---

## Статус выполнения

| Фаза | Статус | Что даёт пользователю |
|---|---|---|
| 0. Стабилизация | ✅ Готово | зелёная база, без мёртвого кода |
| 1. Усиление под локалки | ✅ Готово | модель реально слушается формата |
| 2. Стриминг | ✅ Готово | пользователь видит прогресс |
| 3. Сессии и multi-turn UI | ✅ Готово | TUI / VS Code / `session.*` |
| 4. Минимальный набор инструментов | ✅ Готово | можно реально редактировать проект |
| ✅ Контрольная точка | ✅ Готово | тест на реальном проекте |
| 5. Субагенты (`task.spawn/wait/cancel`) | ✅ Готово | защита контекста, параллельные задачи |
| 6. Permissions и hooks | ✅ Готово | линтер/форматтер после каждого изменения |
| 7. Memory / ORCHESTRA.md | ✅ Готово | проект знает контекст без объяснений |
| 8. MCP bridge | ✅ Готово | экосистема инструментов (github, sqlite, slack) |
| 9. Eval harness + провайдеры | ✅ Готово | метрики качества, Anthropic API |
| 10. Planner–Worker | ✅ Готово | Lead + Worker, WorkOrder, LSP E2E |

---

## Фаза 10 — Planner–Worker (целевой режим для локальных моделей) ✅

**Цель:** Lead (reasoning) планирует и делегирует; Workers (быстрые coder-модели) имплементируют атомарные правки под AST-Gate + LSP validation loop.

**Design doc:** [docs/architecture/planner-worker.md](./architecture/planner-worker.md)

**Deliverables (2026-08):**

1. ✅ `worker` mode + WorkOrder JSON + validation
2. ✅ Worker validation loop (≤3 LSP iterations) + E2E (`tests/e2e_agent/`, `tests/e2e_real_llm/`)
3. ✅ AST scoping (`target_symbol` + CKG) + AmbiguousMatch eval
4. ✅ `orchestra.tiers` + prefilling `{` для Worker
5. ✅ Lead guard: delegate через `task(worker)`, не monolithic `edit`

**Осталось вручную:** полный TUI smoke LSP install modal (чеклист в `ui/tui/README.md`); unit-тест modal — `ui/tui/view/modal_test.go`. Field eval / Real LLM E2E — см. [field-eval-e2e.md](./architecture/field-eval-e2e.md).

---

## Фаза 11 — Orchestra vNext (автономия Lead–Worker)

**Цель:** автоматизировать верификацию Worker до Lead, CKG-enriched WorkOrder, parallel workers, Lead scratchpad.

**Design doc:** [docs/architecture/orchestra-vnext.md](./architecture/orchestra-vnext.md)

| # | Улучшение | Статус |
|---|-----------|--------|
| 1 | Verifier / Critic subagent | ✅ deterministic hook; LLM verifier 🔲 |
| 2 | CKG fields в WorkOrder | ✅ schema + runtime guard |
| 3 | Parallel workers (prompt + eval) | 🟡 infra ✅ |
| 4 | Spec constraints enforcement | 🟡 prompt ✅ |
| 5 | Lead scratchpad (`.orchestra/state.md`) | ✅ Done |
| 6 | Forced LSP on `task_result` | ✅ Done |

---

## MVP: что входит в первую тестируемую версию

| Фаза | Что даёт пользователю | Без чего нельзя выйти |
|---|---|---|
| 0. Стабилизация | зелёная база, без мёртвого кода | да |
| 1. Усиление под локалки | модель реально слушается формата | да |
| 2. Стриминг | пользователь видит прогресс | да |
| 3. Сессии и multi-turn UI | TUI / VS Code / `session.*` | да |
| 4. Минимальный набор инструментов | можно реально редактировать проект | да |

После Фазы 4 → **Контрольная точка: тест на реальном проекте**. Только потом фазы 5+.

---

## Фаза 0 — Стабилизация текущей базы

**Цель:** свести репозиторий к компилируемому, тестируемому, согласованному vNext-состоянию. Без этого все следующие фазы будут на зыбкой почве.

### Задачи

1. **Разобрать `git status`.** Сейчас в рабочем дереве 30+ удалённых v0.2 файлов и десятки новых vNext (см. статус: `D pkg/cli/...`, `D internal/context/...`, `D internal/gitutil/...`, `?? internal/agent/...`). Разбить на 3-5 коммитов: "remove v0.2", "vNext core", "vNext agent+tools", "vNext tests", "docs".
2. **~~Решить судьбу `code.symbols`.~~** Зафиксировано: LSP → tree-sitter (CGO) → regex для Go; инструмент остаётся в registry. См. `internal/tools/nav/symbols*.go`.
3. **Удалить мёртвые артефакты тестов из корня:** `e2e_real_llm_results.txt`, … — проверить `.gitignore` (в репо сейчас чисто).
4. **~~Решить судьбу `orchestra core --http`.~~** Оставлен как **debug-only** (loopback + token); stdio — supported transport. CLI/doc предупреждения добавлены.
5. **Привести `docs/` в порядок.** Устаревшие task/checklist файлы удалены; периодически сверять `README.md` с `docs/commands-and-modes.md`.

### Definition of Done

- `go vet ./...` и `go test ./...` зелёные на Linux и Windows.
- `git status` чистый или содержит только новые файлы для следующих фаз.
- В корне нет `*.exe`, `*_results.txt`, `*_report.md`.
- `README.md` отражает реальный набор команд и ссылается только на живые `docs/*`.

### Риски

- Соблазн начать "ну заодно подправлю". Не надо. Только чистка, без новых фич.

---

## Фаза 1 — Усиление под локальные модели ✅

**Цель:** локальная модель 7B-20B стабильно следует контракту инструментов, не уходит в ретраи на 30 секунд, выдаёт корректные патчи.

### Задачи

1. **Configurable retry strategy.** ✅ Provider-aware `FillRetryLimits`; `0` в конфиге → auto.
2. **Единая классификация ошибок.** ✅ `guard/classify.go` + circuit-breaker; `step.classified` в llm_log.
3. **Grammar-constrained sampling.** ✅ `ResolveResponseFormat` + auto `json_schema` для local; `supports_json_schema: false` opt-out; OpenAI client auto-detect on reject.
4. **Шаблоны промптов per model family.** ✅ `internal/prompt/files/{mode}-{family}.txt` + `ResolvePromptFamily` (aliases: qwen/chatml/llama → local).
5. **Деприоритизировать `unified_diff`.** ✅ build-local/gpt/gemini/kimi.
6. **Логирование наблюдений.** ✅ `tool_call`, `tool_result`, `step.classified` в llm_log; `tests/eval.ParseLLMLog` + `orchestra eval` колонка RETRIES.

### Definition of Done

- Локальная модель на 5 типовых задачах (`tests/eval/tasks/`: rename, add_func, fix_bug, add_test, refactor) — **ручной прогон** `orchestra eval` (avg invalid retries <3).
- `--from-plan` детерминированно повторяет plan.json — ✅ e2e (`TestApply_FromPlan_*`).
- Grammar-constraint сокращает invalid_retries — проверяется через `orchestra eval` + сравнение лога с/без `supports_json_schema: false`.
- Все юнит-тесты зелёные — ✅ CI.

### Риски

- Не все OpenAI-совместимые сервера поддерживают `json_schema` — mitigated: auto-detect disable + `supports_json_schema: false`.

---

## Фаза 2 — Стриминг ✅

**Цель:** пользователь видит токены модели и вывод инструментов в реальном времени.

### Задачи

1. **`llm.Client.CompleteStream`.** ✅ `llm/stream.go`, OpenAI + Anthropic clients.
2. **OpenAI SSE + tool_calls assembly.** ✅ `ParseSSEStream`, tests in `stream_accumulator_test.go`.
3. **Стриминг в агенте.** ✅ `streamStep` whenever client implements `Streamer`; `OnEvent` optional (UI only).
4. **JSON-RPC notifications.** ✅ `agent/event`, `exec/output_chunk` — `docs/PROTOCOL.md`.
5. **CLI рендеринг.** ✅ `buildCLIRenderer`: tokens, `→ tool`, `← preview`; `--stream` for pipes; TTY checks stdout+stderr.

### Unified Complete path

- `OpenAIClient.Complete` и `AnthropicClient.Complete` drain `CompleteStream` (non-streaming POST removed for OpenAI).
- Fixes LM Studio tool_calls on blocking path.

### Definition of Done

- `orchestra apply` в TTY показывает токены — ✅
- JSON-RPC клиент получает notifications — ✅ (TUI, VS Code)
- Тесты сборки чанков tool_calls — ✅
- Anthropic streaming — ✅

### Документация

- `docs/architecture/streaming.md`

---

## Фаза 3 — Сессии и multi-turn UI ✅

**Цель:** один `orchestra core` держит многораундовую беседу. Это водораздел между «одноразовый `apply`» и «Claude Code-shaped» ассистент.

**Интерактив — через клиенты, не через отдельную CLI-команду.** Отдельный `orchestra chat` (простой readline-REPL) **не планируется и не нужен**: TUI, VS Code extension и будущая IDE покрывают UX. Ядро остаётся headless (`core` + JSON-RPC).

### Реализовано

1. **`internal/core/session`** — `history`, todos, pending ops, compaction, persistence.
2. **JSON-RPC:** `session.start`, `session.message`, `session.cancel`, `session.history`, `session.close`, `session.ui_sync`, `session.apply_pending`. One-shot `agent.run` сохранён для `apply` / CI.
3. **Клиенты:**
   - **TUI** — `orchestra` / `orchestra tui` (`ui/tui/`), multi-turn через `session.message`.
   - **VS Code** — webview chat (`ui/vscode/src/chat/`).
4. **Persistence** — `.orchestra/sessions/<id>.json` (schema v3/v4).
5. **Cancel** — `session.cancel` → `context.Cancel()` до HTTP SSE и tool execution.

### Definition of Done (фактический)

- TUI держит беседу из 5+ ходов на одной модели.
- `session.cancel` / Esc в TUI прерывает текущий ход, сессия сохраняется.
- Тесты на конкурентные сессии и cancel (`internal/core/rpc_handler_test.go`, e2e).
- `docs/PROTOCOL.md` описывает `session.*`.

### Намеренно не делаем

- **`orchestra chat` REPL** — дублировал бы TUI без value; headless сценарии остаются за `orchestra apply`.

---

## Фаза 4 — Минимальный набор инструментов для реальной работы

**Цель:** модели хватает инструментов для типичной задачи "посмотри проект, поправь N файлов, проверь что собирается".

### Задачи

1. **`fs.glob`** — отдельно от `search.text`. Принимает glob pattern, возвращает пути. Локалки часто хотят "перечисли все *.go в internal/" — сейчас это `fs.list` + рекурсия + фильтрация, что они часто ломают.
2. **`fs.write`** — атомарная запись файла как tool (создать/перезаписать). Сейчас это только `final.patches → file.write_atomic`, недоступно посреди разговора. Для типичного "создай новый тест" модель должна уметь сразу.
3. **`fs.edit`** — strict search/replace как tool (а не только в `final.patches`). Это позволяет модели делать инкрементальные правки и сразу видеть, что они применились (например, перед запуском теста). Под капотом — тот же `Resolver` + `Applier` с тем же `file_hash`.
4. **`exec.run` с allowlist.** Заменить бинарный `--allow-exec` на конфиг:
   ```yaml
   exec:
     allow:
       - go
       - npm
       - git
       - pytest
     deny:
       - rm
       - curl
   ```
   В `tool.call` проверка против allowlist. Если команды нет в allow — `ExecDenied`. `--allow-exec` остаётся как override "разрешить всё" (для отладки).
5. **Стриминг вывода `exec.run`.** Через те же notifications из фазы 2. Когда `exec.run` идёт >1 секунды, отправлять `exec/output_chunk`.
6. **`todo.write` / `todo.read`.** Аналог Claude Code TodoWrite — модель ведёт чеклист задач, видит его в каждом промпте. Хранится в сессии (in-memory). Не критично, но сильно помогает локалкам не терять нить на длинных задачах.

### Definition of Done

- Все 6 инструментов есть в `ListTools(allowExec)` и в `docs/PROTOCOL.md`.
- В `internal/tools/` тесты на каждый инструмент с allowlist edge cases для exec.
- На простой задаче "посмотри проект, добавь функцию и тест к ней, запусти `go test`" модель проходит без `exec.run` allow-all.

### Риски

- `fs.edit` как tool создаёт два пути для модели — патчи в `final.patches` и инкрементальные правки. Это может запутать слабую модель. Возможно, для локалок 7B оставить только `fs.edit` и убрать механизм `final.patches` (или свести их к одному и тому же под капотом).

---

## ✅ Контрольная точка: тест на реальном проекте

После Фазы 4 — собираем и пробуем.

### Сценарий теста

1. **Инициализация.** Создать пустой каталог, `git init`, `orchestra init`. Настроить `.orchestra.yml` под локальную модель.
2. **Maiden voyage.** `orchestra` (TUI), описать модели маленький проект на Go (например, "CLI для парсинга .env файлов с командами get/set/list"). Дать ей построить с нуля: придумать структуру, написать main.go, написать тесты, прогнать `go test`, поправить баги.
3. **Расширение.** Через ту же сессию добавить вторую фичу (например, "поддержка комментариев в .env"). Проверить что модель помнит контекст.
4. **Edit-flow.** Закрыть сессию, открыть новую, дать задачу "перепиши команду list с использованием cobra". Проверить что заходит в новый файл, читает существующие, делает корректные правки.
5. **Failure modes.** Намеренно дать сложную задачу (напр., "добавь поддержку YAML с сохранением порядка ключей"). Документировать как модель ломается: уходит в петлю, выдаёт галлюцинации, теряет контекст и т.п.

### Что зафиксировать в отчёте

- Какая модель тестировалась, какой размер контекста использовался.
- Сколько шагов понадобилось на каждую задачу.
- Какие инструменты вызывались чаще всего.
- Какие patches заваливались и почему (`StaleContent`, `AmbiguousMatch`, hash mismatch).
- Какие ошибки были в JSON-формате (если grammar-constraint включён — должно быть около нуля).
- Где UX был неудобный (рендеринг, cancel, /diff).

Этот отчёт — вход в фазы 5+.

---

## Фаза 5 — Субагенты (`task.spawn`)

**Цель:** защитить основной контекст. Тяжёлые подзадачи (поиск по большой кодовой базе, ресёрч) уходят в дочернюю сессию с изолированным окном.

- `task.spawn { description, allowed_tools? } → { task_id }`, `task.wait`, `task.cancel`.
- Дочерняя сессия имеет свой `Session`, своя history. Возвращает родителю один финальный текст или JSON.
- Родитель видит только итог, его контекст не растёт от деталей.
- Под локалки 8k-32k это критично — без этого 4-5 шагов и контекст забит.

---

## Фаза 6 — Permissions и hooks

- Per-tool правила в конфиге: `tools.fs.write.confirm: true` и т.п.
- Hooks: pre-tool-call, post-tool-call, session-start, session-end. Запускаются как subprocess (как у Claude Code).
- Это позволит подключать линтеры/форматтеры/проверки безопасности как внешние процессы.

---

## Фаза 7 — Memory / project context

- Аналог `CLAUDE.md` — `ORCHESTRA.md` или `.orchestra/memory/`.
- Автоматически инжектится в системный промпт при `session.start`.
- Раздельно: глобальная (user-level), проектная (repo-level).

---

## Фаза 8 — MCP bridge

- Клиент MCP (Model Context Protocol).
- MCP-серверы регистрируются в `.orchestra.yml`, их инструменты подмешиваются в `ListTools` с префиксом `mcp:<server>:<tool>`.
- Это открывает экосистему — sqlite, github, slack, etc.

---

## Фаза 9 — Eval harness и поддержка не-OpenAI провайдеров

### Eval harness

- `tests/eval/` — набор задач с ожидаемыми результатами (структура файлов, прохождение тестов).
- `orchestra eval --model <name>` — прогоняет все задачи, собирает метрики (success rate, среднее число шагов, время, retry-count).
- Это позволит осознанно выбирать модели и видеть регрессии при изменениях агента.

### Provider connectors

- `internal/llm/anthropic.go` — Claude API.
- `internal/llm/gemini.go` — Gemini API.
- Поддержка provider-specific capabilities: prompt caching (Claude/OpenAI), thinking blocks (Claude 4), grammar (только локальные).
- Конфиг расширяется: `cfg.LLM.Provider: openai | anthropic | gemini`.

---

## Сквозные правила (нарушаем — ломаем)

1. **Не вводить состояние в `Agent`.** Хочется кешировать что-то — кешируем в `Session`, не в `Agent`.
2. **Не утечь Anthropic/Gemini-специфике в `internal/agent/`** — всё в `internal/llm/<provider>.go`.
3. **Каждый новый JSON-RPC метод документируется в `docs/PROTOCOL.md` в том же PR.** Иначе IDE-плагины протухнут.
4. **Бамп `protocol.ProtocolVersion`** при любом breaking change в существующих методах. Аддитивные — не бампим.
5. **Тесты под Windows.** CI проверяет, локально проверять тоже. Атомарная запись на Windows работает иначе (rename over existing файл нельзя без `MoveFileEx` с `MOVEFILE_REPLACE_EXISTING`).
6. **Не возвращать v0.2-паттерны.** `pkg/cli/`, `internal/context/`, `daemon.json/cache.json` discovery — всё это удалено сознательно.

---

## Оценка объёма (грубо, для одного человека вечерами/выходными)

| Фаза | Размер |
|---|---|
| 0. Стабилизация | 1-2 дня |
| 1. Усиление под локалки | 4-7 дней |
| 2. Стриминг | 3-5 дней |
| 3. Сессии и multi-turn UI | 7-10 дней |
| 4. Инструменты | 4-6 дней |
| **MVP до контрольной точки** | **~3-5 недель** |
| 5. Субагенты | 4-6 дней |
| 6. Permissions/hooks | 5-7 дней |
| 7. Memory | 2-3 дня |
| 8. MCP bridge | 5-8 дней |
| 9. Eval + провайдеры | 5-10 дней |

---

## Что делаем дальше

Фазы 0–10 (MVP + Planner–Worker) закрыты. Phase 1 hardening (grammar / classify / logging) — в коде.

**Modularization (P0–P6) — ✅ закрыто.** Источник истины: `docs/architecture/modules.md`.

| Phase | Статус | Артефакт |
|-------|--------|----------|
| P0 | ✅ | `internal/uimodel`, sessionstore без `ui/*` |
| P1 | ✅ | sub-module `protocol/` + `go.work` |
| P2 | ✅ | sub-module `patch/` (ops/resolver/applier/fsutil/cache/relpath) |
| P3 | ✅ | sub-module `llm/` + `lmstudio/` |
| P4 | ✅ | `internal/tools/{exec,git,web,toolslsp,fs,nav,session,task,toolpath,toolschema}/` |
| P5 | ✅ | import-rules CI, core/agent split |
| P6 | ✅ | `call_dispatch`, test colocation, `orchestra daemon` CLI removed |

Legacy `internal/{protocol,jsonrpc,schema,ops,applier,patches,resolver,fsutil,cache,relpath,llm}/` **удалены** — импорты только через sub-modules.

**Закрыто недавно:** attachments + vision (protocol **v13**), TUI `/attach`, VS Code extension, Phase 2 streaming.

---

## Фаза 11 — Стабильность, скорость, OpenCode-parity (локальные модели)

**Цель:** меньше rebounce и шагов LLM на локальном стеке (27B lead + fast worker), стабильнее edit/search, быстрее end-to-end в TUI/eval. Invariants Phase 0–10 **не снимаем**: unique match, `file_hash`, atomic write, External→Internal ops.

**Design reference:** [forgiving-edit.md](./architecture/forgiving-edit.md) — 3-pass vs 9-strategy OpenCode.

### Статус треков

| Трек | Фокус | Статус |
|------|--------|--------|
| **A** Patch / resolver | passes 4–6 + BlockAnchor strict | ✅ A1–A2 |
| **B** Search / nav | CKG, semantic_search, repo-map, grep, explore routing | ✅ B1–B5 (docs/prompts/grep) |
| **C** Agent speed | tiers 27B/4B, parallel batch, compaction, truncate | ✅ C1–C5 |
| **D** Stability | permission `ask` on edit/write, LSP smoke, grammar local | ✅ D1,D3; D2 checklist; D4 existing |
| **E** OpenCode gaps | session import/export, auth CLI lite, worktrees | ✅ E1–E4 |

### A — Patch / edit (`patch/resolver/`)

| ID | Задача | DoD |
|----|--------|-----|
| A1 | Passes **4–6**: WhitespaceNormalized, EscapeNormalized, TrimmedBoundary | ✅ |
| A2 | **BlockAnchor** strict (unique first/last line anchors, **без** Levenshtein) | ✅ |
| A3 | Eval metric: `resolve_failed` count в eval summary / llm_log | ✅ RESOLVE column |
| A4 | Компактные ambiguous hints (match_lines) — регрессия | ✅ exact + strategy field |

**Не в Phase 11a:** Levenshtein fuzzy, MultiOccurrence «replace all» — только если A1–A2 не хватит (Phase 11a-ext).

### B — Search / navigation

| ID | Задача | DoD |
|----|--------|-----|
| B1 | Ops: `orchestra ckg embed --rebuild` перед map-задачами | documented in planner-worker / TUI hints |
| B2 | `semantic_search` + auto-explore включены и smoke-tested | embed.model + CKG store |
| B3 | Prompts: Lead → `repo_map` / `symbols` / `semantic_search` before blind grep | `build-local.txt`, `orchestra.txt` |
| B4 | Grep: mtime sort, лимиты на huge repos | `internal/search/` |
| B5 | Wide queries → `explore` subagent by default in `orchestra` mode | mode routing + test |

### C — Agent speed

| ID | Задача | DoD |
|----|--------|-----|
| C1 | Default tiers: Lead = main LLM, Worker = `providers.fast` | TUI + `.orchestra.yml` example |
| C2 | Parallel batch audit: read+grep in one step where safe | `registry` ParallelSafe flags green |
| C3 | Auto-compaction threshold tuning + long-session smoke | `session.compact` |
| C4 | `truncateMessages` / working_state budget на 20k ctx | no tool-call orphans |
| C5 | Streaming core debounce for `message_delta`/`reasoning_delta` | ✅ default 30ms; `ORCH_STREAM_DEBOUNCE_MS` |

### D — Stability

| ID | Задача | DoD |
|----|--------|-----|
| D1 | `permissions.rules`: `allow \| deny \| ask`; ask on `edit`/`write` | TUI/VS Code modal; PROTOCOL.md |
| D2 | TUI LSP install + worker diagnostics smoke | checklist `ui/tui/README.md` |
| D3 | Local grammar knobs documented + eval with `prompt_family: local` | `.orchestra.yml` template |
| D4 | Circuit breaker / repeat-tool hints audit | agent tests |

### E — Backlog (после A–D)

| ID | Задача |
|----|--------|
| E1 | Session export/import | ✅ `orchestra session export/import/list` |
| E2 | Auth CLI lite | ✅ `orchestra auth set-key`, `auth list` |
| E3 | Git worktrees first-class | ✅ `orchestra worktree`, `git.worktree.*`, `--worktree` |
| E4 | Full 9-pass edit + bounded fuzzy (OpenCode parity) | ✅ fuzzy-block + double-anchor |

### Рекомендуемый порядок

1. **A1 + A4** (resolver 4–6 + metrics) — ~1 нед  
2. **B1–B3 + C1** (CKG/semantic + tiers) — ~1 нед  
3. **A2** BlockAnchor — ~3–5 д  
4. **B4 + C2** — ~1 нед  
5. **D1** или **C3** — по pain из `llm_log.jsonl`  
6. **E*** — по необходимости  

### KPI Phase 11

| Метрика | Источник | Цель |
|---------|----------|------|
| Steps / task | `orchestra eval` | −20%+ vs baseline |
| `resolve_failed` | `llm_log.jsonl` | ↓ после A1 |
| Time-to-first-patch | TUI / eval | ↓ |
| PASS rate | eval tasks | ↑ |

---

### Legacy «Следующий фокус» (field-test checklist)

1. **Field eval** — `orchestra eval` на локальной Qwen/Llama (Phase 1 acceptance).
2. **Real LLM E2E** — `ORCH_E2E_LLM=1 go test ./tests/e2e_real_llm`.
3. **VS Code marketplace** — `npm run package` / vsce publish.
4. **Streaming hardening** — см. «Operational notes» в `docs/architecture/streaming.md` (core debounce, async notify — опционально → Phase 11 **C5**).

### Фаза 1 — knobs в `.orchestra.yml`

```yaml
llm:
  response_format_type: json_schema   # или json_object
  supports_json_schema: true          # false = тихо omit; omit = auto-detect
  prompt_family: local                # или qwen|chatml|llama|llama-instruct (→ local)
```

События в `.orchestra/llm_log.jsonl`: `tool_call`, `tool_result`, `step.classified`
(`kind`: `validation_error` | `tool_denied` | `tool_failed` | `resolve_failed` | `apply_recoverable`),
`memory.note` (`kind`: `written` | `skipped` | `failed`; `source`: `model` | `digest`) — что сделал
авто-писатель памяти в конце хода. То же самое уходит клиенту полем `memory` в результате `session.message`.
