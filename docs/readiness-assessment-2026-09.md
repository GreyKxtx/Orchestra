# Orchestra — оценка готовности (сентябрь 2026)

**Дата:** 2026-09-04 · **Срез кода:** `master` @ `34b5324` (2026-08-31). Аудит кода выполнен на `a48c547` (2026-08-17) и затем сверен с 13 новыми коммитами — см. §0 · **Полевые данные:** 9 дней прогонов на проекте `smoke-demo` (2026-08-05 → 08-13)

> **Обновление 2026-09-05.** Поверх `34b5324` легли 18 коммитов, закрывшие P0 #1–7, #9 и P1 #9–16 целиком (см. `parity-plan-2026-09.md`, §0). Строки матриц ниже помечены сноской `†`, где состояние изменилось; исходные оценки оставлены как зафиксированная точка отсчёта. **Открытым из P0 остался только #8** (`orchestra init` → `ORCHESTRA.md`). Наблюдаемость памяти (§3.2 п. 9) закрыта в `cc41475`: событие `memory.note` в `llm_log.jsonl` и статус в чате; счётчики кэша — в `usage.jsonl` (`3d4a68c`). План выхода на ●● по каждой строке — в `parity-plan-2026-09.md`.

---

## Резюме

Orchestra — локальный AI-агент для кодинга на Go (~130K строк, 806 `.go`, 344 тест-файла, 454 коммита с декабря 2025). Ядро — JSON-RPC 2.0 stdio-сервер `orchestra core`; клиенты — TUI (Bubble Tea) и расширение VS Code. Целевая ниша — «Claude Code для локальных моделей» (Qwen 27B через LM Studio / vLLM) с Lead–Worker оркестрацией по тирам.

**Вердикт одной строкой:** инженерное ядро (патчи, LSP, устойчивость к слабым моделям, тесты) — на уровне зрелых конкурентов и в нескольких местах выше; продукт как целое — late beta; **память спроектирована на 4/5, но в реальном прогоне работает на 1.5–2/5**; дистрибуция — 1/5.

| Область | В коде | На практике (демо) | Комментарий |
|---|:-:|:-:|---|
| Агентный цикл, устойчивость к слабым моделям | 4.5 | 4 | Компакция с guard'ом сходимости, overflow-recovery, prefill, per-family промпты |
| Патчи / apply / безопасность правок | 4.5 | 4 | `file_hash` + staging + AST-gate + atomic write; 4 StaleContent на 165 edit |
| Инструменты (≈70) | 4.5 | 4 | Полный набор; описания на русском |
| **Память: файловые слои** | **3** | **1.5** | 1 факт в `agent.md` за 52 сессии; summary-файлы забиты шумом |
| **Память: компакция / дайджесты** | **4.5** | **2.5** | С 30.08: авто-порог под реальное окно модели, хвост истории сохраняется, маркер обрезки. Turn-digest пишутся, но не переживают сессию |
| **Learning stack (lessons → playbooks)** | **3** | **0** | Доступен только в orchestra-режиме; в демо не задействован |
| CKG / навигация по коду | 4 | 3 | Граф построен (55 узлов, 235 рёбер); эмбеддинги 0, `semantic_search` не зарегистрирован |
| Оркестрация Lead–Worker | 3.5 | — | 69 тестов, но нет FS-изоляции воркеров; LLM-verifier выключен по умолчанию; в демо не использовалась |
| LSP (диагностика + auto-provision) | 4.5 | — | Лучше всех CLI-конкурентов |
| MCP | 3 | — | Только stdio; нет HTTP/SSE, OAuth, resources/prompts |
| Провайдеры | 3.5 | 4 | 16 записей каталога = 2 протокола (OpenAI-compat + Anthropic native) |
| TUI / VS Code / Desktop | 4.5 / 3.5 / 0 | — | Desktop — только README |
| Skills / workflows | 4 | — | Skill packs + DAG-workflows; нет slash-команд, нет совместимости с CLAUDE.md/AGENTS.md |
| Тесты, CI, гигиена кода | 4.5 | — | `-race` на Windows в CI, import-rules как тест, 4 TODO на 130K строк |
| **Дистрибуция, лицензия, версии** | **1** | — | Нет LICENSE-файла, нет релизов CLI, `CoreVersion = "vnext"` |

---

## 0. Что изменилось после аудита (`a48c547` → `34b5324`)

13 коммитов от 30–31 августа, 80 файлов (+3095/−560). Все — в агентном цикле, промптах и LLM-клиенте; **ни один файл слоёв памяти не тронут**.

| Коммит | Что сделано | Влияние на выводы документа |
|---|---|---|
| `5371e4c`, `34b5324` | Волатильный блок (todos, `<working_state>`, digests, reminder) перенесён **после** истории → system + первое сообщение байт-идентичны весь ход; для Anthropic native — 3 cache-breakpoint'а; `cached_prompt_tokens`/`cache_write_tokens` в usage-событиях (и на streaming-пути) | Частично закрывает P1 #14. Для Anthropic-моделей **через OpenRouter** `cache_control` по-прежнему не передаётся (только native-клиент) — эпизод «983K токенов за ход» лечится лишь наполовину |
| `32dd885` | Каталог окон контекста для облачных моделей (`llm/model_context.go`); `compact_threshold_pct: 0` = auto (60/75/85% по размеру окна), `-1` = off; компакция суммирует только старую часть истории, хвост (30% бюджета) сохраняется; `history_prune_keep_recent` 2→6, `child_max_steps` 12→24 | Оценка компакции «в коде» поднята 4 → 4.5. В демо-конфиге порог пришпилен (`85`) — рекомендуется сбросить в `0` |
| `4a94da7` | Маркер `[history trimmed: N steps, files …]` на месте вырезанного куска истории | Качество; убирает «невидимую дыру» после truncate |
| `2b74610` | Дешёвая модель компакции (`providers.fast`) пробрасывается в детей, pipeline и workflow-стадии | Экономия токенов в orchestra-режиме |
| `145a9c3` | bytes/token = 3 на шаге 1, если ≥30% не-ASCII в промпте | Точнее бюджет для кириллицы на локалках |
| `c8314d6` | `StaleContent` несёт `nearest` — ближайший по Левенштейну фрагмент файла; `protocol.Error.Data` доходит до модели | Быстрее восстановление после 4× StaleContent, как в демо |
| `5f20221` | Упавший сабагент возвращает родителю прогресс (файлы, находки, последнее действие) | Меньше повторных чтений в Lead–Worker |
| `3917f26` | Exec-consent разделён: `git.commit/branch/checkout/push`, worktree, `gh.pr.create` — только build/debug/general; worker 31→22 инструмента, verifier 30→22 | Усиливает модель безопасности (§4); закрывает риск «`git.checkout` у одного ребёнка переключает ветку всем» |
| `145753e` | Все 20 промптов — на английском, тест на кириллицу | Сужает вывод про язык: **описания ~60 инструментов и CLI-help по-прежнему на русском** (`internal/tools/*/registry.go`) |
| `c212fbe`, `f7390c5` | Локальный addendum для всех режимов (не только build); `<available_tools>` инжектируется только семействам local/unknown — system prompt для Claude 6.7 → 1.9 KB | Экономия Step-1 бюджета на облачных моделях |
| `c704dac` | Единый реестр режимов, `{{PLAN_PATH}}` во всех режимах, `system.<mode>.txt` | Исправления корректности |

**Не изменилось (проверено на `34b5324`):** `internal/memory/inject.go` (hybrid == eager), `internal/core/auto_summary.go` (гейт ≥8, ошибки в stderr), `internal/agent/digest/tool_digest.go` (авто-заметки только grep/explore), `internal/agent/working/state.go` (digests в рамках сессии), `languages.enabled` (мёртвый), `protocol/version.go`, `ui/vscode/README.md` (`tools_version=12`), **LICENSE отсутствует**. Все выводы §3.2 актуальны для текущего HEAD.

Отдельно: в origin есть ветка `cursor/fix-agent-pipeline-e103` — параллельная линия с 2026-04-20 (382 своих коммита, отстаёт от master на 458, последний коммит 2026-07-28). Для оценки не релевантна, но как гигиена — либо закрыть, либо пометить архивной.

---

## 1. Метод и источники

1. **Код** — три независимых аудита исходников (память; агентный цикл/инструменты/оркестрация; интеграции/поверхности/качество) с привязкой к `файл:строка`.
2. **Документация** — `README.md`, `docs/ROADMAP.md`, `docs/CHANGELOG.md`, `docs/memory.md`, `docs/architecture/{memory-context,context-overflow,planner-worker,turn-digest-working-state}.md`, `docs/pipeline-issues-audit.md` (собственное source-to-source сравнение с OpenCode), `docs/tools-status.md`, `docs/modes.md`.
3. **Полевые артефакты** `smoke-demo/.orchestra/`: `usage.jsonl` (91 ход), `llm_log.jsonl` (1474 события), `memory/` (52 сессии), `ckg.db`, `sessions/*.json`. Плюс `.crush/` — попытка параллельного прогона Crush.
4. **Конкуренты** — публичные данные на сентябрь 2026 (Claude Code, OpenCode, Crush, Cursor, Aider, Cline, Gemini CLI); ссылки в приложении Б. Точность — на уровне «фича есть / нет / частично».

Важная поправка к самооценке проекта: в `README.md` и `docs/tools-status.md` практически всё отмечено ✅. Это честно относительно «реализовано ли», но ничего не говорит о том, **срабатывает ли оно в реальной работе**. Именно этот разрыв и является главным содержанием документа.

---

## 2. Полевой прогон: что реально произошло за 9 дней

| Показатель | Значение | Что это значит |
|---|---|---|
| Ходов (`session.turn`) | 91 (+1 `apply`) | Реальная многодневная работа над React-приложением |
| Prompt / completion токенов | 9.92M / 195K | Соотношение 51:1 — контекст переотправляется целиком на каждом шаге |
| Медиана prompt на вызов | ~15K токенов | Нормально для локалки с 60–120K окном |
| Ходов со средним prompt > 40K/вызов | 5 из 91 | Все — длинные многошаговые сессии |
| Самый дорогой ход | 15 вызовов, **983K prompt-токенов, $2.18, 5 мин** (Claude Sonnet 5 через OpenRouter) | На момент прогона префикс промпта менялся каждый шаг (волатильный блок стоял до истории) — кэш не мог сработать; с `5371e4c` (30.08) префикс стабилен. Но `cache_control` для Anthropic-моделей отправляет только native-клиент; через OpenRouter breakpoint'ов нет. Компакция не сработала — при `num_ctx: 262144` и пришпиленном `85%` порог не достигался |
| Вызовы инструментов | read 328 · edit 165 · grep 148 · write 78 · ls 56 · glob 21 · repo_map 10 · **memory_read 8 · memory_search 4 · task 3** · explore 2 · symbols 2 · bash 2 | `memory_write` — **0**: модель ни разу сама не записала факт. `semantic_search` — 0: не зарегистрирован (нет `embed.model`) |
| Ошибки инструментов | 6× `fs.write` без `file_hash`; 4× `StaleContent`; 1× `SyntaxError` (AST-gate); 3× невалидный JSON аргументов | Защитные слои работают: ни одной битой записи на диск |
| LLM-ошибки | 211 событий, из них **183 за 07.08** | Недоступный/401 vLLM-endpoint через ngrok. Клиент долбил мёртвый endpoint; fail-fast (`ad7813d`) появился 17.08 — уже после прогонов |
| Crush (конкурент, тот же проект) | `No agent configuration found` → `context deadline exceeded`, 0 сессий в БД | Сравнение «в бою» не состоялось |

### 2.1 Что осталось в памяти после 52 сессий

| Артефакт | Факт | Оценка |
|---|---|---|
| `.orchestra/memory/agent.md` (долговременная проектная память) | **1 запись** (06.08, 405 байт), корректный LLM-summary | Кросс-сессионная память фактически пуста |
| `sessions/<id>.md` (summary сессии) | Есть у **6 из 52** сессий. Из них **5** — строки вида `grep "className=" — 31 match lines in digest` (десятки подряд), **1** — `ls`-ошибка с битой кодировкой (не-UTF-8 байты) | Слой «session memory» заполнен шумом tool-дайджестов, а не фактами |
| `sessions/<id>.turns.md` (rule-based turn-digest) | 51 файл, 158 блоков `[turn_digest]`; 14 файлов пустые (только шапка) | Механизм работает, но дайджесты не переживают сессию (см. §3.3) |
| `memory/lessons/`, `playbooks/`, `depts/`, `state.md`, `decisions.md` | **Отсутствуют** | Learning stack и orchestra-режим не запускались ни разу |
| `ORCHESTRA.md` | Отсутствует | `orchestra init` не предлагает создать его (аналог `/init` в Claude Code) |
| `ckg.db` | 11 файлов, 55 узлов, 235 рёбер, **0 эмбеддингов, 0 трейсов** | Граф работает; семантический слой — нет |

---

## 3. Пайплайны памяти — детально

### 3.1 Слои и их состояние в коде

| # | Слой | Что хранит / где | Кто пишет | Когда читается | Код | Зрелость |
|---|---|---|---|---|---|:-:|
| 1 | **Файловая память** (аналог CLAUDE.md + auto-memory) | `ORCHESTRA.md` · `.orchestra/memory/sessions/<id>.md` · `.orchestra/memory/agent.md` · `lessons/<dept>.md` · `~/.orchestra/memory.md` | Авто: (а) эвристическая заметка после `explore`/`grep` (`internal/agent/digest/tool_digest.go:284-310`); (б) LLM-summary конца хода (`internal/core/auto_summary.go:17-69`) — при `len(hist) ≥ 8`, ошибки в stderr. Ручное: `memory_write` | `Store.FormatInject` на каждом шаге (`internal/memory/inject.go:11-27`, `agent_prompt.go:158-168`); бюджет 35/25/30/остаток; `[pin]` переживает компакцию | `internal/memory/` (1064 LOC, 7 тестов) | **3** |
| 2 | **memory_search** | substring по слоям + семантическое ранжирование при `embed.model` (последние 48 чанков, порог 0.15) | — | По вызову модели | `internal/memory/semantic.go`, `tools/session/memory.go` | 2.5 |
| 3 | **Episodic lessons (L1)** | `.orchestra/memory/lessons/<dept>.md`, rule-based, без LLM, dedup, trim 48 | **Только** после завершения worker-child (`internal/tasks/tasks.go:662-715` → `lessons_record.go`) | Worker: `<dept_lessons>` при spawn; Lead: `<dept_lessons_all>` ≤5/dept | `internal/lessons/` (842 LOC, 12 тестов) | 3 |
| 4 | **Playbooks + decisions (L2/L3)** | `lesson_promote` → local overlay с `decision_ref: PENDING` → Question Barrier пишет `decisions.md` → auto-seal → `playbook_promote` merge | Lead в `orchestra`/`architecture` режимах | Worker `<dept_playbook>`, Lead индекс | `internal/playbooks/`, `internal/decisions/` (9+2 тестов) | 3 |
| 5 | **Эмбеддинги CKG** | `node_embeddings` в `ckg.db` | **Только** `orchestra ckg embed` или RPC `index.embed` — автозапуска нет | `semantic_search` (регистрируется только при `embed.model`) | `internal/embed/`, `ckg/embed_store.go` | 2 |
| 6 | **CKG** (аналог repo-map) | SQLite граф: tree-sitter, 15–17 языков, инкрементально по hash | Лениво при `explore`/`symbols`/warmup | `explore` (multi-hop BFS/DFS, depth≤2, ≤50 узлов), `repo_map`, `<ckg_context>` на шаге 1 (≤1500 токенов) | `internal/ckg/` (7710 LOC) | **4** |
| 7 | **Компакция + turn digest** | LLM-checkpoint `[Session checkpoint]` + guard «<20% сжатия → truncate»; rule-based `<working_state>` и `[turn_digest]` | Агент на верху каждого шага | `<turn_digests>` (последние 3) в user-prompt **текущей** сессии | `internal/agent/compact.go`, `working/state.go`, `overflow.go` | **4** |
| 8 | MCP `@modelcontextprotocol/server-memory` | Внешний сервер из пресета TUI | Модель, если решит | Не связан с `internal/memory` вообще; Lead его не видит (allowlist 14 инструментов) | `ui/tui/view/dialog_mcp.go:192` | 1 |

### 3.2 Почему на практике получилось «1 факт за 52 сессии» — причины на уровне кода

Все упомянутые ниже файлы не менялись между `a48c547` и `34b5324` — выводы актуальны для текущего HEAD.

1. **Единственный канал кросс-сессионной памяти — один хрупкий LLM-вызов.** `maybeAutoSummaryMemory` требует ≥8 сообщений в истории, живой compaction-клиент и при любой ошибке молча пишет в stderr (`auto_summary.go:21, 58-61`). Короткие ходы («продолжай», «да давай» — их в демо большинство) не проходят гейт.
2. **`memory.mode: hybrid` == `eager`.** В `inject.go:29-35` `switch` различает только `lazy`; `hybrid` и `eager` — один код-путь, разница — строка `<memory_hint>`. Задокументированные три режима — фактически два.
3. **Авто-заметки покрывают два инструмента.** `AutoMemoryNote` пишет строку только для `explore` и `grep` — и именно эти строки («grep X — N match lines») составляют 5 из 6 session-summary. Правки, запуски тестов, ошибки сборки — то, что действительно стоит помнить — в память не попадают.
4. **Turn-digest не переживают сессию.** `FormatRecentTurnDigests` ключуется текущим `session_id` (`working/state.go:313-320`); в `agent.md` дайджесты не продвигаются. Новая сессия стартует «слепой», хотя структурированный ledger (files/done/open) уже есть.
5. **Эмбеддинги никогда не строятся сами.** Нет триггера кроме ручной команды; в демо-конфиге задан `embed.provider`, но не `embed.model` → `semantic_search` не регистрируется (`core_agent.go:361-363`), `memory_search` тихо деградирует до substring (ошибка embed отбрасывается, `session/memory.go:96`).
6. **`languages.enabled` — мёртвый конфиг.** Объявлен и дефолтится в `["go"]` (`config.go:849-850`), не читается ни одним индексатором — поэтому JS/TS индексируются независимо от настройки. Безвредно, но вводит в заблуждение.
7. **Весь learning stack — только для orchestra-режима.** Lessons пишутся исключительно из worker-детей; `lesson_promote`/`playbook_promote` гейтятся `ModeOrchestra`/`ModeArchitecture` (`learning_tools.go:13-15`). Пользователь single-agent `build` (а это 100% демо) не получает ничего. Плюс нет поставляемых L2-playbook'ов — только шаблоны в `docs/examples/playbooks/`.
8. **Кодировка на Windows.** В одном summary-файле — не-UTF-8 байты (вероятно cp1251 из tool-ответа `ls` с русским сообщением об ошибке). Память с битыми байтами затем инжектируется в промпт.
9. **Нет наблюдаемости памяти.** Ни одно событие записи/пропуска/ошибки памяти не попадает в `llm_log.jsonl` или TUI notice — увидеть, что память «не работает», можно только открыв файлы руками.

### 3.3 Сравнение подсистем памяти с конкурентами

Легенда: ●● сильно · ● есть · ◐ частично · ○ нет. Данные по конкурентам — на 09/2026 (приложение Б).

| Возможность | **Orchestra** | Claude Code | OpenCode | Crush | Cursor | Aider | Cline | Gemini CLI |
|---|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|
| Инструкции проекта (файл в репо, иерархия) | ● `ORCHESTRA.md` (по директориям, lazy discovery) | ●● `CLAUDE.md` project/user/local + imports | ● `AGENTS.md` | ● `CRUSH.md`/`AGENTS.md` | ● `.cursor/rules` | ◐ `CONVENTIONS.md` | ● `.clinerules` | ●● `GEMINI.md` 4 уровня |
| Агент сам пишет долговременную память | ◐ `memory_write` + auto-summary (в демо: 1 запись, 0 ручных) | ●● Auto Memory (`MEMORY.md` + topic files, по умолчанию с v2.1.59) | ○ (плагины) | ○ | ● Memories (авто, opt-in) | ○ | ◐ memory-bank конвенция + `new_rule` | ● tiered memory + экспериментальный Auto Memory |
| Индекс/поиск по памяти | ● `memory_search` (substring + semantic при embed) | ◐ индекс `MEMORY.md`, topic files on demand | ○ | ○ | ● | ○ | ○ | ◐ |
| Сессионная память между ходами | ● session `.md` + turn-digest (но шум) | ● транскрипт + `/resume` | ●● SQLite сессии, resume, share | ● SQLite сессии | ● | ◐ `/clear` вручную | ● checkpoints | ● checkpointing |
| Компакция контекста | ●● digests → LLM checkpoint → truncate; overflow-recovery с калибровкой | ● auto ~95% + `/compact [фокус]` | ● prune + summary + retry | ● auto-summary | ● | ◐ | ◐ | ● |
| Индекс кода (структурный/семантический) | ●● CKG tree-sitter граф + multi-hop + опц. эмбеддинги | ○ grep/glob | ○ | ○ | ●● embeddings индекс | ● repo-map tree-sitter + PageRank | ◐ tree-sitter outline | ○ |
| Episodic learning (уроки из ошибок → правила) | ◐ lessons→playbooks, только orchestra-режим, human-in-the-loop | ◐ через auto memory (`feedback` записи) | ○ | ○ | ◐ Memories | ○ | ◐ `new_rule` | ◐ Auto Memory предлагает skills |

**Вывод по памяти.** Архитектурно Orchestra — одна из самых полных систем в классе: пять файловых слоёв с бюджетами, pins, semantic re-rank, rule-based digest без LLM, learning stack с human-gate. Но продуктово она уступает Claude Code и Gemini CLI, потому что **у тех память пишется по умолчанию и всегда**, а у Orchestra — по узкому гейту, в двух инструментах или только в редком режиме. Разрыв закрывается не архитектурой, а триггерами и наблюдаемостью (см. §8, P0–P1).

---

## 4. Агентный цикл, инструменты, оркестрация

**Цикл.** `Agent.Run` → panic-barrier → шаг: prune → compact → `nextStep` (сборка промпта + LLM) → `StepToolCall` (параллельная пачка read-only / серийно) или `StepFinal`. Лимиты: `max_steps`, `max_invalid_retries 3`, `max_final_failures 6`, `max_denied_repeats 2`, `max_tool_errors 6`; circuit-breaker дублей и doom-loop (warn 3 / block 5). Overflow 400 от vLLM → clamp `max_tokens` → retry → compact → повтор шага (до 2), калибровка bytes/token по реальному `usage`. Для локальных моделей: per-family промпты (`*-local.txt`), grammar `json_schema` (только legacy-путь), `{`-prefill для worker, wire-санитизация имён/схем инструментов. **4.5/5** — лучший в классе для слабых моделей.

**Инструменты (≈70).** fs (`ls/read/glob/write/edit/delete/rename/diff.preview/ast_rename`), nav (`grep/symbols/explore/semantic_search/repo_map`), LSP ×5, exec (`bash` + background `bash.output/kill`), web (`webfetch` c SSRF-защитой, `websearch` Tavily/Brave), browser ×10 (Playwright-MCP через `npx`), git ×7 + worktree ×4 + gh ×5, task ×5 + `skill_invoke`, todo ×2, memory ×3 + `lesson_promote`/`playbook_promote`, `question`, `runtime_query`, `update_working_state`, `contract_freeze`. Безопасность: `write` требует `file_hash` или `must_not_exist`; `edit` — уникальный search-блок (9-pass forgiving resolver); dry-run в staging overlay с LSP-синком и AST-gate; atomic temp→fsync→rename + `.orchestra.bak`; 7-слойная цепочка разрешений (explore-first gate → rules allow/deny/ask → exec/web consent → mode-scopes → human gates G2/G3 на commit/push → pre-hook → dedup → post-hook). С `3917f26` git-мутации, worktree и `gh.pr.create` доступны только build/debug/general — worker и verifier их не получают вовсе (дети делят рабочее дерево с родителем). **4.5/5.**

**Оркестрация (`--mode orchestra`).** Lead с жёстким allowlist 14 инструментов и Step-1 бюджетом ≤8K токенов (проверяется тестом `orchestra_budget_test.go`). `task(subagent_type=worker, tier=…)` → `TaskRunner.Spawn`: WorkOrder JSON, phase-guard, brief-gate, сериализация пересекающихся `target_files`. Worker без истории родителя, prompt cap 48KB, `{`-prefill. Верификация трёхслойная: детерминированная (LSP diag, `go build`, affected `go test`, `tsc --noEmit`) → acceptance-checks → опц. LLM-verifier с явным арбитражем (красные проверки бьют зелёного верификатора). Tier-эскалация — выключена по умолчанию; `worker_llm_verify_enabled` — false по умолчанию. Воркеры **делят один workspace** — `git.worktree.*` есть, но в spawn не подключён. В VS Code нет селектора режима orchestra. **3.5/5** — глубже субагентов Claude Code/OpenCode по дизайну, но не доказано вне собственных тестов; в демо ни разу не использовалось.

**Skills / workflows.** `.orchestra/skills/*.md` с frontmatter (`tools/model/provider/completion_markers`), три источника (project > user > packs), `$ARGUMENTS`, `@refs/`, `skill_invoke`, 24 примера; 12 YAML-workflow (DAG стадий с `{stage.output}`). Нет slash-команд и совместимости с `CLAUDE.md`/`AGENTS.md`. **4/5.**

**Про «открытые долги» из `pipeline-issues-audit.md`.** Тот документ перечисляет четыре незакрытых пункта, но это его собственный текст, а не состояние кода: сверка с `34b5324` показывает, что все четыре уже закрыты. `TaskRunner.removeTask` вызывается из `Wait` (`tasks.go:790,818`) — утечки нет; `AgentRunResult.Todos` объявлен и заполняется (`core_agent.go:79,203`); `NormalizeToolName` существует с таблицей алиасов и тестом (`digest/names.go:21`, `tool_dispatch_test.go:13`); `plan_enter` убран из всех tool-поверхностей (`registry.go:116`) и остался только legacy-обработчик для старых вызовов.

Туда же — пятый пункт, который эта оценка приписала VS Code: **режим `orchestra` в расширении есть и полностью связан** с `e98c673` (пункт меню в `media/chat-src/01-dom-state.js`, `mode` уходит в `session.message` в `panel.ts:943`, плюс разбивка по тирам в футере и `listOrchestraRoles`).

**Методологический вывод, который дороже самих пунктов.** Пять ложных «открыто» подряд возникли одинаково: отрицательный результат поиска был принят за отсутствие функциональности. Четыре пришли из текста аудит-документа, пятый — из поиска, который смотрел в `ui/vscode/src`, тогда как список режимов лежит в `ui/vscode/media/chat-src`. Отсутствие находки — это утверждение о зоне поиска, а не о коде; «не найдено» надо проверять `git log -S` по конкретной строке, прежде чем писать «нет».

**Что действительно остаётся:** описания ~60 инструментов (`internal/tools/*/registry.go`) и CLI-help на русском, притом что сами промпты с `145753e` полностью английские — один ход смешивает языки между system prompt и tool schema. Плюс мелочь того же рода: текст legacy-заглушки `plan_enter` (`tool_dispatch.go:523`) тоже русский.

---

## 5. Интеграции, поверхности, провайдеры

| Компонент | Состояние | Оценка |
|---|---|:-:|
| **TUI** (`ui/tui`, 23K LOC) | Стриминг с сегментами reasoning→tools→text, diff-review, subagent bar, todo-панель, 19 slash-команд, палитра Ctrl+K, @-mention, `/attach`, permission-очередь, сессии/rewind, диалоги провайдеров/MCP/tiers | 4.5 |
| **VS Code / Cursor** (`ui/vscode`, 8.3K TS) | Webview-чат, drag-drop вложений, dry-run/apply, per-file review, LSP-install modal, 6 вкладок настроек; core бандлится под 4 платформы; VSIX 0.1.2. Режим `orchestra` есть в селекторе с разбивкой по тирам в футере (`media/chat-src/01-dom-state.js`, `panel.ts:919`). Нет rich tool-блоков и reasoning-UI | 3.5 |
| **Desktop** (`ui/desktop`) | README на 361 байт, кода нет | 0 |
| **LSP** (`internal/lsp`) | Собственный клиент, реестр ~12 серверов с pinned-версиями, auto-provision `ask|true|false` с consent, кэш `~/.orchestra/lsp/`, async ensure, диагностика прикрепляется к ответам `edit`/`write`, эскалация при повторе одинаковых ошибок | 4.5 |
| **MCP client** | stdio only (протокол 2024-11-05), tools/list+call, `list_changed`, lazy restart ≤3, `allowed_tools`; нет HTTP/SSE/streamable, OAuth, resources, prompts; Orchestra не является MCP-сервером | 3 |
| **Browser** | `npx @playwright/mcp` как stdio-подпроцесс; 10 инструментов; screenshot → в историю при `multimodal` | 3 |
| **Web** | `webfetch` (SSRF-блок private/loopback), `websearch` Tavily/Brave | 3.5 |
| **Провайдеры** (`llm/`) | Каталог 16 (Local/Cloud/Gateway), фактически 2 протокола: OpenAI-compat + Anthropic native (prompt caching `cache_control`). Gemini — через OpenAI-shim. Streaming SSE, reasoning-стрим Qwen3/DeepSeek, retry 429/408/5xx, budget-инвариант vLLM, probe/ping, credits только OpenRouter. Нет OAuth/подписок, Bedrock/Vertex/Azure, встроенной таблицы цен | 3.5 |
| **Runtime evidence** (OTel → CKG) | `orchestra instrument` (14 языков, entry-patch только Go/Py/TS/JS), OTLP-receiver `:4318`, спаны привязываются к узлам CKG, `runtime_query`. Уникально для класса, но не доказано в бою (0 трейсов в демо) | 3.5 |
| **Git / GitHub** | Полный git incl. managed worktrees + `gh` PR/issue; human-gates на commit/push | 3.5 |
| **Конфиг** | `.orchestra.yml` + deep-merge `.orchestra.local.yml` (секреты маскируются при сохранении из UI, файловый лок). ~~Нет глобального `~/.orchestra/config.yml`~~ † есть с `a194576` | 3 → 4 † |

---

## 6. Инженерное качество и дистрибуция

**Сильно.** CI: `go vet` + `go test` + **`go test -race` на Linux и Windows (MinGW)**, importrules-тест, компиляция расширения; nightly stress `-race -count=50` для JSON-RPC/core; 4-платформенная сборка core для VSIX. 344 тест-файла (43% файлов), ~34% строк — тесты; 5 e2e-наборов (`e2e`, `e2e_nollm`, `e2e_agent` на mock-LLM, `e2e_real_llm` за env-флагом, `eval`). **4 TODO/FIXME на 130K строк**, 2 оправданных `panic()` в инициализации. Конвенция audit-тегов в комментариях (`docs/audit-ledger-comments.md`) реально соблюдается.

**Слабо.**
- **Нет `LICENSE` в корне** при заявленном MIT в README — блокер любого внешнего использования (проверено и на `34b5324`).
- Параллельная ветка `cursor/fix-agent-pipeline-e103` (382 коммита с апреля, отстаёт от master на 458) висит в origin без решения.
- **Нет релизов CLI**: ни goreleaser/Makefile/install-скрипта, ни тегов (кроме `v0.2.0` от декабря), ни `--version`; `CoreVersion = "vnext"`. Единственный поставляемый артефакт — VSIX. CGO (tree-sitter) осложняет кросс-компиляцию — но матрица сборки уже есть в `vscode-vsix.yml`.
- Нет линтера кроме `go vet`, нет измерения покрытия (`cover.html` — 2 файла, устарел), нет govulncheck/gosec.
- Дрейф документов: `ui/vscode/README.md` заявляет `tools_version=12` при фактическом 14; `.cursor/rules/projectrules.mdc` ссылается на удалённые пакеты и запрещает `--http`, который поставляется. (Заявленный «v14 handshake» — это `ToolsVersion`; `ProtocolVersion` = 13 корректно.)
- Гигиена: ~246 МБ бинарников, `test_depth.go`, `cover_applier` в корне; `cmd/bench` с захардкоженным `D:\CursorProjects\Orchestra`.
- README, CLI-help и описания инструментов — на русском; международная аудитория отрезана независимо от качества кода.

---

## 7. Сравнительная матрица (продукт целиком)

| Возможность | **Orchestra** | Claude Code | OpenCode | Crush | Cursor | Aider | Cline |
|---|:-:|:-:|:-:|:-:|:-:|:-:|:-:|
| Локальные модели как first-class | ●● | ○ | ● | ● | ○ | ● | ● |
| Multi-provider | ● (16 / 2 протокола) | ◐ Anthropic + Bedrock/Vertex | ●● 75+ | ●● | ● | ●● | ●● |
| Долговременная авто-память | ◐ | ●● | ○ | ○ | ● | ○ | ◐ |
| Компакция контекста | ●● | ● | ● | ● | ● | ◐ | ◐ |
| Индекс кода | ●● CKG | ○ | ○ | ○ | ●● | ● | ◐ |
| LSP-обратная связь после правок | ●● + auto-install | ◐ | ● | ● | ● (IDE) | ◐ | ◐ |
| Безопасность правок (hash/staging/atomic) | ●● | ● permissions/sandbox | ● | ● | ● checkpoints | ● git | ● checkpoints |
| Субагенты / оркестрация | ●● Lead–Worker, tiers, verify | ● subagents, worktrees | ● | ◐ | ● background agents | ◐ architect/editor | ◐ |
| MCP | ◐ stdio → ● stdio + Streamable HTTP † | ●● stdio/HTTP/OAuth | ●● | ●● | ●● | ○ | ●● + marketplace |
| Hooks | ● pre/post tool | ●● lifecycle + matchers | ● plugins | ◐ | ● | ◐ | ● |
| Skills / команды | ●● skills+packs+workflows | ●● skills+slash+plugins | ● | ◐ | ◐ | ○ | ● |
| Vision / вложения | ● | ● | ● | ● | ● | ◐ | ● |
| Поверхности | ● TUI + VS Code | ●● CLI+IDE+desktop+web | ●● TUI+desktop+IDE | ● TUI | ●● IDE | ● CLI+web | ● VS Code |
| Дистрибуция / установка | ○ `go build` → ● release workflow, `go install`, `orchestra version` † | ●● | ●● | ●● | ●● | ●● | ●● |
| Лицензия | ○ файла нет → ● MIT (`LICENSE`) † | закрытая | MIT | FSL | закрытая | Apache-2 | Apache-2 |

### Где Orchestra объективно впереди
1. **LSP auto-provision с consent + диагностика в ответе инструмента** — ни один CLI-агент этого не делает.
2. **CKG + multi-hop explore + мост OTel→граф** — уникальная связка «структура кода + runtime-доказательства».
3. **Двухслойные патчи External→Resolver→Internal Ops** с `file_hash`, AST-gate и atomic write — самая консервативная (и безопасная для слабых моделей) модель правок в классе.
4. **Инженерия под локальные модели**: per-family промпты, prefill, калибруемый token-budget, overflow-recovery, `-race` на Windows.
5. **Learning stack с human-gate** — идея сильнее, чем у всех перечисленных; проблема в охвате, не в дизайне.

### Где отстаёт
1. Память **не пишется по умолчанию** (Claude Code, Gemini CLI, Cursor — пишут). † С `360708b` пишется после каждого изменившего хода; остаётся разрыв в *качестве* — типизация, дедуп, промпт-триггеры (`parity-plan` §1.2).
2. **Дистрибуция и лицензия** — фактически нулевые. † Закрыто до ●: LICENSE, релизный workflow, `go install`; до ●● — install-скрипты, подпись, нотисы, English README.
3. **MCP только stdio**, нет MCP-сервера. † Streamable HTTP есть (`99c8168`); OAuth, resources, prompts, `.mcp.json` и серверный режим — открыты.
4. Нет глобального конфига, OAuth-логинов, native Gemini/Bedrock. † Глобальный конфиг есть (`a194576`); остальное открыто.
5. Русскоязычный интерфейс и описания инструментов.
6. Desktop-поверхность — только обещание в README.

---

## 8. Рекомендации

### P0 — дни (закрывают «продукт не готов к показу»)
| # | Действие | Почему |
|---|---|---|
| 1 | Добавить `LICENSE` (MIT) в корень | Блокер адопции; 1 файл |
| 2 | `orchestra --version`, git-тег, goreleaser/release-workflow на базе матрицы из `vscode-vsix.yml`; `go install` в README | Сейчас поставляется только VSIX |
| 3 | Память: снизить гейт `auto_summary` (≥4 сообщений **или** был `edit`/`write`), включить по умолчанию, ошибки — в `llm_log.jsonl` + TUI notice | Одна запись за 52 сессии — главный продуктовый провал памяти |
| 4 | Убрать tool-дайджесты `grep/explore` из слоя session-summary (или писать их в отдельный `*.tools.md`, не инжектируемый как факты) | 5 из 6 summary — шум |
| 5 | Реализовать `hybrid` (например: ORCHESTRA + pins + последние N записей `agent.md`, остальное через `memory_read`) или убрать режим из документации | Задокументировано три режима, работает два |
| 6 | Удалить `languages.enabled` или подключить к индексатору CKG | Мёртвый конфиг |
| 7 | Нормализовать кодировку tool-ответов перед записью в память (UTF-8 always) | Битые байты в промпте на Windows |
| 8 | `orchestra init` → предложить сгенерировать `ORCHESTRA.md` из `docs/examples/ORCHESTRA.template.md` | Аналог `/init`; в демо файла нет |
| 9 | В конфиге демо и в шаблоне `init`: `compact_threshold_pct: 0` (auto) вместо пришпиленного `85`; убрать `context_limit_kb: 64`, если окно берётся из каталога | Новая семантика с `32dd885`; старые конфиги молча остаются на фиксированном проценте |

### P1 — недели
| # | Действие | Почему |
|---|---|---|
| 9 | Продвигать `[turn_digest]` (files/done/open) в `agent.md` при закрытии сессии — rule-based, без LLM | Дёшево делает память кросс-сессионной даже для локалок |
| 10 | Авто-эмбеддинг при CKG-warmup, если задан `embed.model`; явная ошибка `memory_search`/`semantic_search`, а не тихий fallback | Сейчас семантический слой невидимо выключен |
| 11 | Lessons для single-agent `build`: писать anti-pattern при повторе `StaleContent`/`AmbiguousMatch`/одинаковых LSP-ошибок ≥3 | Learning stack должен работать у 100% пользователей, а не только в orchestra |
| 12 | MCP: streamable-HTTP/SSE транспорт, OAuth, resources | Крупнейший разрыв экосистемы |
| 13 | Глобальный `~/.orchestra/config.yml` (провайдеры, ключи, tiers) | Каждый проект настраивается с нуля |
| 14 | Проброс `cache_control` для `anthropic/*` на OpenAI-compatible пути (OpenRouter) — стабильный префикс и счётчики cached-токенов уже есть (`5371e4c`, `34b5324`), не хватает только breakpoint'ов; плюс предупреждение о стоимости при >N токенов/ход | $2.18 за 5-минутный ход; OpenRouter кэширует Anthropic-модели только с явными маркерами |
| 15 | Английские описания инструментов (`internal/tools/*/registry.go`) и CLI-help; английский README. Промпты уже переведены (`145753e`) | Один ход сейчас смешивает языки между system prompt и tool schema |
| 16 | ~~VS Code: селектор orchestra-режима~~ | Закрыт: режим есть в селекторе и полностью связан (`e98c673`). Весь пункт 16 оказался закрыт до начала работ — см. §4 |

### P2 — квартал
| # | Действие |
|---|---|
| 17 | Ранжирование `<ckg_context>` по эмбеддингам/центральности графа вместо SQL `LIKE` |
| 18 | Worktree-изоляция воркеров (инфраструктура `git.worktree.*` уже есть) |
| 19 | LLM-verifier и tier-эскалация по умолчанию для `complex` tier |
| 20 | Orchestra как MCP-сервер (экспорт CKG/explore/runtime_query внешним агентам) |
| 21 | Coverage в CI + golangci-lint + govulncheck |
| 22 | Убрать Desktop из README до появления кода; native Gemini API, Bedrock/Vertex |

---

## Приложение А. Доказательная база (пути)

- Код: `internal/memory/inject.go:11-60`, `internal/core/auto_summary.go`, `internal/agent/digest/tool_digest.go:284-310`, `internal/agent/working/state.go:313-320`, `internal/config/config.go:849-850`, `internal/core/core_agent.go:361-363`, `internal/tools/session/memory.go:96`, `internal/tasks/tasks.go:662-715`, `internal/agent/learning_tools.go:13-15`, `internal/tools/registry.go:400-486`, `internal/agent/orchestra_budget_test.go`, `internal/tasks/worker_verify.go:592-692`, `protocol/version.go:23,48`, `llm/factory.go:11-15`, `internal/mcp/client.go`, `.github/workflows/{ci,stress,vscode-vsix}.yml`. Новое на `34b5324`: `docs/architecture/prompt-cache.md`, `llm/model_context.go`, `internal/agent/prompt_cache_prefix_test.go`, `internal/agent/compact_split_test.go`, `patch/resolver/nearest.go`.
- Сверка срезов: `git log a48c547..34b5324` (13 коммитов, 80 файлов); `git diff a48c547..34b5324 -- internal/memory internal/core/auto_summary.go internal/agent/digest internal/agent/working` — пусто.
- Полевые данные: `smoke-demo/.orchestra/usage.jsonl` (91 строка), `llm_log.jsonl` (1474 события), `memory/agent.md`, `memory/sessions/` (52 сессии, 6 summary), `ckg.db` (files 11 / nodes 55 / edges 235 / node_embeddings 0), `sessions/20260813T101049-6e4e.json` (44 сообщения, без checkpoint), `smoke-demo/.crush/logs/crush.log`.
- Конфиг демо: `memory.mode: hybrid`, `session_enabled: true`, `agent.auto_session_memory: true`, `compact_threshold_pct: 85`, `context_limit_kb: 64`, `embed.provider: openrouter` (без `model`), `llm.extra_body.num_ctx: 262144`, `languages.enabled: [go]`.

## Приложение Б. Источники по конкурентам (09/2026)

- Claude Code — память и Auto Memory: https://code.claude.com/docs/en/memory · https://blog.memoryplugin.com/claude-code-memory/ · https://claudefa.st/blog/guide/mechanics/auto-memory
- Cursor — Rules/Memories и MCP-банки памяти: https://dev.to/izgorodin/how-to-add-persistent-memory-to-cursor-with-mcp-2026-148n · https://supermemory.ai/blog/cursor-memory-via-mcp/
- OpenCode — архитектура, сессии, LSP, MCP: https://fastino.ai/blog/the-complete-guide-to-opencode-open-source-ai-coding-agents · https://www.datastudios.org/post/opencode-ai-coding-agent-architecture-mcp-integration-and-byok-pricing-explained
- Aider / Cline / обзор CLI-агентов: https://pinggy.io/blog/best_open_source_cli_coding_agents/ · https://hindsight.vectorize.io/blog/2026/06/09/cline-persistent-memory · https://www.kinde.com/learn/ai-for-software-engineering/ai-agents/how-to-write-a-memory-bank-for-your-ai-coding-agent/
- Gemini CLI — tiered memory и Auto Memory: https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/auto-memory.md · https://github.com/google-gemini/gemini-cli/discussions/26216
- Внутреннее сравнение Orchestra с OpenCode (source-to-source): `docs/pipeline-issues-audit.md`, `docs/architecture/context-overflow.md`.
