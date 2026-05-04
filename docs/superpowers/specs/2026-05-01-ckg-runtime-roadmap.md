# Orchestra vNext+1 — CKG, Runtime Tracing, Multi-Agent Roadmap

**Дата:** 2026-05-01  
**Обновлён:** 2026-05-04  
**Статус:** В работе — под-проекты 0–3 закрыты, под-проект 4 в очереди  
**Уровень:** надстройка над `docs/ROADMAP.md` (vNext, фазы 0–9 — готовы)

---

## 1. Контекст и цель

Orchestra на 2026-05-01 — это работающее ядро vNext: stateless `Agent.Run`, JSON-RPC over stdio, потоковый вывод, сессии, MCP bridge, hooks, eval. Все 9 фаз исходного roadmap закрыты. Базовый CLI (`apply` / `chat`) работает на локальной модели (qwen3.6-27b на момент написания).

Следующий рывок — превратить продукт из «локального Claude Code» в инструмент с уникальным дифференциатором. План:

1. **Точный контекст по любым проектам** — Code Knowledge Graph (CKG) с алгоритмически обходимым графом, FQN-разрешением и поддержкой полиглота.
2. **Runtime Observability Bridge** — связка стек-трейсов / метрик / логов с узлами CKG. Идея: агент видит не только статику, но и runtime-поведение, причём они физически связаны через `file:line → node_id`.
3. **Multi-agent оркестрация** — Dispatcher + Investigator + Coder + Critic, поверх существующего `Agent.Run` через механизм субагентов (фаза 5 vNext, готова).

**Дифференциатор vs Cursor / Cline / Continue.** Конкуренты дают LLM либо статический контекст (graph / embeddings), либо рантайм-инфо (логи Sentry, трейсы), но не **связанные** примитивы. Связка `trace_id → span → file:line → CKG node → caller chain` — это то, чего ни у кого из них нет в коробке. Именно она оправдывает усилия по доводке CKG до точного состояния и интеграции рантайма.

---

## 2. Анализ существующей схемы `orchestra_request_lifecycle.svg`

SVG в корне репозитория описывает lifecycle одного запроса в текущем (vNext) ядре:

```
User query
  → CLI (apply / chat)
  → JSON-RPC (stdio / HTTP)
  → Core (session, config merge, agent build)
  → Agent loop:
      BuildPrompt → LLM call → NormalizeLLM → StepToolCall?
                                              ├─ Tool runner → result → history (loop back)
                                              └─ StepFinal → Resolver + Applier → Result
                                                                                    ↓
                                           Patches / diffs / applied (plan.json, diff.txt, git commit)
```

Сбоку: hooks, MCP tools, subagents (пунктиром), memory inject, circuit breaker.

**Что схема покрывает:** один запрос, один agent, один LLM, один tool-loop. Это **транзакционный pipeline** — он живой и работает.

**Чего на схеме нет (и не должно быть — это другой уровень абстракции):**
- Knowledge Graph как самостоятельный долгоживущий слой.
- Runtime Observability Bridge.
- Маршрутизация задачи между несколькими агентами с ролями.
- Семантический поиск / Vector DB.

**Вывод.** Схема корректна для vNext, но **не описывает** ту архитектуру, к которой мы идём. После реализации под-проектов 2 и 3 потребуется **вторая схема** — высокоуровневая, с Toolchain (CKG + Vector + Runtime Bridge) и multi-agent topology поверх существующего lifecycle. Текущий SVG в этой будущей схеме станет одним из «кубиков» (то, что происходит внутри одного агента).

---

## 3. Аудит текущего CKG

Реализация: `internal/ckg/` (orchestrator, store, parser, scanner, provider) + `internal/tools/explore_codebase.go`. Ниже — таблица проблем, выявленных при чтении кода.

| # | Проблема | Где смотреть | Эффект | Чинится в | Статус |
|---|---|---|---|---|---|
| 1 | Полиглот заявлен, реально работает только Go. `getSitterLanguage()` возвращает language только для `.go`, для остальных — `nil` → `ParseFile` молча возвращает пустоту. | `internal/ckg/parser.go:178-185` | `ExploreSymbol` бесполезен на любом не-Go проекте. | Под-проект 1 | ✅ Закрыто 2026-05-04, коммит `42c7ce1` |
| 2 | Имена не квалифицированы. `Node.Name` хранит short-name. Если в проекте две функции `Run` в разных пакетах — `ExploreSymbol("Run")` вернёт обе. | `internal/ckg/store.go:16-23` | Контекст для LLM неоднозначный. | Под-проект 0 | ✅ Закрыто 2026-05-04, коммиты `b43c1e3`, `ac8a89c` |
| 3 | Edges хранят имена строкой, а не ссылку на `node_id`. Индекса по ним нет. | `internal/ckg/store.go:74-81` | Граф нельзя обходить алгоритмически. | Под-проект 0 | ✅ Закрыто 2026-05-04, коммиты `b43c1e3`, `ac8a89c` |
| 4 | Только `relation = "calls"`. `imports`, `implements`, `instantiates`, `references` — пусто. | `internal/ckg/parser.go:200-211` | Невозможно ответить «кто импортирует этот модуль». | Под-проект 0 (imports), Под-проект 1 (implements) | ✅ imports — закрыто `ac8a89c`; implements — known-limitation |
| 5 | Tool на каждый вызов открывает новый SQLite-handle. | `internal/tools/explore_codebase.go:21-49` | Избыточный overhead и риск race. | Под-проект 0 | ✅ Закрыто 2026-05-04, коммит `b43c1e3` |
| 6 | `complexity` всегда 0. | `internal/ckg/parser.go:106-111` | Невозможно ранжировать «горячие» функции. | Под-проект 1 | ✅ Закрыто 2026-05-04, коммит `42c7ce1` |
| 7 | Нет import / dependency-графа между файлами / пакетами. | `internal/ckg/parser.go` целиком | Невозможен «слой пакетной зависимости». | Под-проект 0 | ✅ Закрыто 2026-05-04, коммит `ac8a89c` |
| 8 | Vector DB / семантический поиск — нет. | — | LLM получает только символический поиск. | Под-проект 4 | ⏳ В очереди (низкий приоритет) |
| 9 | Runtime Observability Bridge — нет. Нет OTel ingestion, нет mapping `span → CKG node`. | — | Главная фича-дифференциатор отсутствует. | Под-проект 2 | ✅ Закрыто 2026-05-04, коммит `87b078b` |

---

## 4. Целевая архитектура

Высокоуровневая декомпозиция в три слоя.

### Слой 1: Dispatcher

Принимает входящую задачу (баг-репорт, фича-реквест, alert из Sentry / Linear / etc.), маршрутизирует на под-задачи, держит общее состояние.

**Замечание о реализации.** Dispatcher — это сам по себе LLM-агент с tools. Текущий `Agent.Run` (см. `internal/agent/agent.go`) уже близок к нему: умеет делать tool-loop, имеет context, history, retry-policy, hooks. Превращение существующего агента в Dispatcher — это вопрос **набора tools**, доступных ему (`query_ckg`, `query_runtime`, `task.spawn`), а не написания нового движка с нуля. Под-агенты делаются через механизм субагентов (фаза 5 vNext, уже готова).

### Слой 2: Toolchain

API для агентов. Здесь критично разделить **две разные подсистемы**:

- **Static Code Knowledge Graph (CKG).** Символический. SQLite, узлы / рёбра, FQN-имена, обходимость по `node_id`. Текущий `internal/ckg/` — скелет. Под-проекты 0 и 1 доводят его до рабочего состояния.
- **Vector Index.** Семантический. Эмбеддинги функций / файлов / документации. Опционален и далеко в очереди — без него можно жить долго, если CKG точный. Под-проект 4.
- **Runtime Observability Bridge.** Принимает OpenTelemetry JSON, хранит spans / traces, резолвит `span.code.filepath:lineno → ckg.node_id`. Под-проект 2.

**Почему разделять CKG и Vector в одном слое визуально, но не в одной подсистеме.** Разные API, разные гарантии консистентности (CKG детерминирован относительно исходников, Vector — приближённый), разные жизненные циклы (CKG обновляется по хешу файла, Vector — по событиям и порциями). Совмещать в одной таблице SQLite — антипаттерн.

### Слой 3: Worker Agents

- **Investigator.** Получает trace из Runtime Bridge + срез CKG (целевой узел + 1–2 уровня caller chain). Возвращает вердикт: «причина в `pkg.X.method`, строка N, потому что Y».
- **Coder.** Получает вердикт + точный snippet из CKG. Возвращает `final.patches` в существующем формате (см. `internal/externalpatch/`). Работает с тем же External Patches → Resolver → Internal Ops pipeline, что и vNext.
- **Critic.** Запускает **формальные** проверки: `go vet`, `golangci-lint`, тесты, type-check. Это **не** «ещё один LLM-ревью» — такое дублирует Coder и почти ничего не даёт. LLM-проверка нужна только если у неё есть жёсткий критерий (security checklist, policy compliance), а не «оцени код в целом».

Все три — субагенты с изолированной history, через `task.spawn` (vNext фаза 5).

---

## 5. Декомпозиция на под-проекты

| № | Название | Статус | Коммиты / дата |
|---|---|---|---|
| **0** | Доводка Go-CKG до точного (FQN, edges по node_id, imports, Store как член Runner) | ✅ **DONE** 2026-05-04 | `b43c1e3`, `ac8a89c`, `c5f9ced` |
| **1** | Полиглот (Python, TypeScript, Rust, Java + complexity) | ✅ **DONE** 2026-05-04 | `42c7ce1` |
| **2** | Runtime Observability Bridge MVP (OTel ingestion, span→CKG резолвер, `runtime.query` tool) | ✅ **DONE** 2026-05-04 | `87b078b` |
| **3** | Multi-agent оркестрация | ✅ **DONE** 2026-05-04 | см. ниже |
| **4** | Vector DB / семантический поиск | ⏳ **В очереди** | — |

### Под-проект 3 — детали

| Часть | Что реализовано | Коммит |
|---|---|---|
| **3A** | CKG pre-injection в agent prompt (`ckg_context` блок в user prompt, `FindRelevantNodes`, `FormatNodesForPrompt`) | `a0a9c43` |
| **3B** | Roles & Modes: `ModePlan/ModeBuild`, `plan_exit/plan_enter` tools, `question` tool + `StdinQuestionAsker`, `modeReminder()`, `JustSwitchedFromPlan`, `ListToolsForMode()` | встроен в основную кодовую базу |
| **3C** | Go-level pipeline Investigator→Coder→Critic (`internal/pipeline/pipeline.go`), CLI флаги `--pipeline`, `--pipeline-attempts` | `87b078b`+ текущая сессия |
| **Runtime Bridge** | `TraceContext` в `pipeline.Options`, `fetchRuntimeEvidence()`, `formatRuntimeEvidence()`, `ListToolsForInvestigator()`, CLI флаг `--trace-id` | 2026-05-04, текущая сессия |

**Зависимости визуально:**

```
Под-проект 0 ─┬─ Под-проект 1        ✅✅
              ├─ Под-проект 2 ── Под-проект 3   ✅✅✅
              └─ Под-проект 4 (через 1 для не-Go)   ⏳
```

**Критический путь к дифференциатору пройден:** 0 → 2 → 3 — всё реализовано.  
Следующий шаг по расширению охвата — Под-проект 4 (Vector DB) или полевые испытания.

---

## 6. Текущий статус и что дальше

**Обновлено 2026-05-04.** Критический путь 0→2→3 пройден полностью. Все unit-тесты зелёные (29 пакетов).

### Что готово к полевым испытаниям

```bash
# Базовый pipeline
orchestra apply --pipeline "добавь логирование в handler X" --apply

# Pipeline с runtime-трейсом (нужен ingested trace)
orchestra apply --pipeline "исправь баг из трейса" --trace-id <id> --apply

# Plan → Build flow
orchestra apply --mode plan "рефактори пакет Y"
orchestra apply --from-plan .orchestra/plan.json --apply
```

### Следующие шаги (в порядке приоритета)

1. **Полевые испытания** — запустить pipeline на реальной задаче с qwen3.5-27b, зафиксировать проблемы.
2. **Eval harness** — прогнать `orchestra eval` с реальным LLM на задачах `rename_func` / `add_func`.
3. **Под-проект 4 (Vector DB)** — эмбеддинги + ANN-поиск, `semantic_search` tool. Низкий приоритет пока CKG достаточен.

**Этот roadmap — живой.** При закрытии каждого под-проекта — отметка в таблице секции 5. Новые проблемы CKG добавляются в секцию 3.

---

## Ссылки

- vNext roadmap: `docs/ROADMAP.md` (фазы 0–9, готово)
- Lifecycle SVG: `orchestra_request_lifecycle.svg` (схема vNext, корень репозитория)
- Текущая реализация CKG: `internal/ckg/`, `internal/tools/explore_codebase.go`
- Архитектурные инварианты vNext: `.cursor/rules/projectrules.mdc`, `CLAUDE.md`
