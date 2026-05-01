# Orchestra vNext+1 — CKG, Runtime Tracing, Multi-Agent Roadmap

**Дата:** 2026-05-01
**Статус:** Vision / Roadmap (живой документ)
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

| # | Проблема | Где смотреть | Эффект | Чинится в |
|---|---|---|---|---|
| 1 | Полиглот заявлен, реально работает только Go. `getSitterLanguage()` возвращает language только для `.go`, для остальных — `nil` → `ParseFile` молча возвращает пустоту. Sqlite-store при этом честно записывает `language = "python"/"typescript"/...`, но узлов и рёбер для них нет. | `internal/ckg/parser.go:178-185`, `internal/ckg/parser.go:38-57` | `ExploreSymbol` бесполезен на любом не-Go проекте. | Под-проект 1 |
| 2 | Имена не квалифицированы. `Node.Name` хранит short-name (или `Type.Method` для Go-method). Если в проекте две функции `Run` в разных пакетах — `ExploreSymbol("Run")` вернёт обе. | `internal/ckg/store.go:16-23`, `internal/ckg/provider.go:24-49` | Контекст для LLM становится неоднозначным; модель получает вдвое больше шума. | Под-проект 0 |
| 3 | Edges хранят имена строкой, а не ссылку на `node_id`. Колонки `source_name` / `target_name`, индекса по ним нет. | `internal/ckg/store.go:74-81` | Граф нельзя обходить алгоритмически (BFS/DFS «кто транзитивно зависит от X»), только string-match. | Под-проект 0 |
| 4 | Только `relation = "calls"`. В `Edge`-структуре поле `Relation` есть, но парсер извлекает лишь вызовы; `imports`, `implements`, `instantiates`, `references` — пусто. | `internal/ckg/parser.go:200-211` | Невозможно ответить «кто реализует интерфейс X», «какие пакеты импортируют этот модуль». | Под-проект 0 (imports), Под-проект 1 (implements / references) |
| 5 | Tool на каждый вызов открывает новый SQLite-handle. | `internal/tools/explore_codebase.go:21-49` | Не катастрофа (incremental scan быстрый), но избыточный overhead и риск race при конкурентных вызовах из разных горутин (`cache=shared` помогает, но не решает). | Под-проект 0 |
| 6 | `complexity` всегда 0. Колонка в схеме есть, парсер её не считает. | `internal/ckg/parser.go:106-111`, `internal/ckg/store.go:64-72` | Невозможно ранжировать «горячие» / сложные функции. | Под-проект 1 (как побочный эффект перевода парсера на нормальную AST-walk per-language) |
| 7 | Нет import / dependency-графа между файлами / пакетами / модулями вообще. | `internal/ckg/parser.go` целиком | Невозможен «слой пакетной зависимости» — а это ровно то, что просят при вопросах «что зависит от auth_service». | Под-проект 0 |
| 8 | Vector DB / семантический поиск — нет. | — | LLM получает только символический поиск. На вопросах «где у нас бизнес-логика валидации email» — мимо. | Под-проект 4 |
| 9 | Runtime Observability Bridge — нет. Нет интеграции с OTel / Sentry / Jaeger, нет ingestion, нет mapping `span → CKG node`. | — | Главная фича-дифференциатор отсутствует. | Под-проект 2 |

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

| № | Название | Цель | Зависимости | Грубая оценка | Definition of Done |
|---|---|---|---|---|---|
| **0** | Доводка Go-CKG до точного | Сделать существующий CKG алгоритмически полезным на самом репо Orchestra. FQN, edges по `node_id`, imports, Store как член Runner. | — | 2-3 дня | Запрос `ExploreSymbol("Run")` возвращает только целевой узел с FQN формата `<module-path>/<pkg>.<Type>.<Method>`. Граф проходится BFS/DFS по `node_id`, не по строкам. Запрос «кто импортирует пакет `internal/agent`» отрабатывает корректно. Один Store живёт на весь жизненный цикл Runner. `go test ./internal/ckg/... ./internal/tools/...` зелёный на Linux и Windows. |
| **1** | Полиглот | Tree-sitter для Python + TypeScript / JavaScript первой волной. Затем Java, C / C++, Rust. FQN-эмуляция per-language. | 0 | 5-8 дней (первая волна, py + ts/js) | На тестовом py- и ts-проекте `ExploreSymbol` возвращает узлы с корректными FQN (`module.Class.method` для python, `<file>#<export>.<function>` для ts). Граф вызовов содержит cross-file edges. Edge-cases (anonymous functions, dynamic imports, default exports) задокументированы и покрыты тестами или явно отмечены как known-limitations. |
| **2** | Runtime Observability Bridge MVP | Ingestion OpenTelemetry JSON → SQLite (`traces`, `spans`). Резолвер `span.code.filepath:lineno → ckg.node_id`. Tool `query_runtime` для агента. | 0 (резолвер требует точного CKG) | 6-10 дней | Скрипт-донор отправляет OTel-batch с известным span. CKG содержит соответствующий node. `query_runtime{trace_id}` возвращает spans, для каждого — связанный CKG node (или `null` с диагностикой при отсутствии). E2E-тест: агент получает trace-кусок и CKG-кусок одной парой запросов и формулирует диагноз. |
| **3** | Multi-agent оркестрация | Dispatcher + Investigator + Coder + Critic поверх vNext-механизма субагентов. | 0, 2 (Investigator бесполезен без runtime) | 5-7 дней | На синтетическом сценарии «дай trace_id, найди и почини баг» Dispatcher автоматически вызывает Investigator → Coder → Critic. На входе только `trace_id` или текстовое описание; на выходе — patch + успешный `go test` (или явный отчёт «не починилось, причина X»). |
| **4** | Vector DB / семантический поиск | Эмбеддинги функций (или файлов) + ANN-поиск. Tool `semantic_search` для агента. | 0, 1 | 5-8 дней | Запрос «где валидируется email» возвращает топ-5 функций с релевантностью выше порога на тестовом проекте. Индекс инкрементально обновляется по тем же хешам, что и CKG. Embedding-провайдер настраивается в `.orchestra.yml`. |

**Зависимости визуально:**

```
Под-проект 0 ─┬─ Под-проект 1
              ├─ Под-проект 2 ── Под-проект 3
              └─ Под-проект 4 (через 1 для не-Go)
```

**Критический путь к дифференциатору:** 0 → 2 → 3. Под-проекты 1 и 4 — расширение охвата, можно делать параллельно или после.

**Грубая оценка:**
- До полного дифференциатора (0 + 2 + 3): ~14-20 дней работы.
- До покрытия мейнстрим-языков (0 + 1 + 2 + 3): ~19-28 дней.

---

## 6. Что делаем дальше

Следующая сессия:

1. Открыть отдельный спек на **Под-проект 0** в `docs/superpowers/specs/<дата>-ckg-go-fqn-edges-design.md`.
2. Внутри спека — детали реализации: схема FQN per Go (`<module-path>/<pkg>.<Type>.<Method>`), миграция существующих БД (drop / recreate допустим — `.orchestra/` локальный артефакт, гитигнорится), API-изменения в `Provider.ExploreSymbol`, как Store становится членом `tools.Runner`, какие тесты добавить (unit + integration на тестовом мини-репо).
3. После реализации Под-проекта 0 — отдельная сессия, отдельный спек на Под-проект 1.

**Этот roadmap — живой.** При закрытии каждого под-проекта — отметка в таблице секции 5 (статус, дата, ссылка на PR / коммит). Архитектурные изменения — обновление секции 4. Если по ходу работы обнаруживаются новые проблемы CKG — добавляются в секцию 3.

---

## Ссылки

- vNext roadmap: `docs/ROADMAP.md` (фазы 0–9, готово)
- Lifecycle SVG: `orchestra_request_lifecycle.svg` (схема vNext, корень репозитория)
- Текущая реализация CKG: `internal/ckg/`, `internal/tools/explore_codebase.go`
- Архитектурные инварианты vNext: `.cursor/rules/projectrules.mdc`, `CLAUDE.md`
