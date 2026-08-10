# Инструменты агента — обзор и ситуации применения

## Навигация по файлам

| Инструмент | Что делает | Лучше всего когда |
|---|---|---|
| `ls` | Список файлов/папок с учётом exclude | Нужно понять структуру директории, найти что вообще есть |
| `glob` | Поиск файлов по паттерну (`**/*.go`) | Знаешь расширение или часть имени файла |
| `read` | Читает файл целиком, возвращает content + file_hash | Нужен file_hash для патча; нужны строки вне символов (конфиг, yaml, текст) |

---

## Поиск по коду

| Инструмент | Что делает | Лучше всего когда |
|---|---|---|
| `grep` | Текстовый поиск по паттерну (regex) во всём проекте | Ищешь конкретную строку, имя переменной, паттерн вызова |
| `symbols` | Outline одного файла: список функций/типов/методов | Нужно быстро увидеть что есть в конкретном одном файле |
| `semantic_search` | Эмбеддит query и возвращает top-K CKG-узлов по cosine similarity | Ищешь концепт без точного имени ("rate limiting", "retry logic", "websocket handler"). Требует `orchestra ckg embed` для индексации |

---

## Понимание кода (CKG) — ГОТОВО ✅

Три уровня глубины — выбираются **автоматически** по форме запроса:

| Запрос | Уровень | Что возвращает |
|---|---|---|
| `explore("internal/agent")` | **Пакет** | Все типы + список методов каждого + экспортируемые функции + внутренние по файлам. Без кода тел. ~30-60 строк |
| `explore("Agent")` | **Тип** | Определение struct/interface + полный список всех методов с номерами строк |
| `explore("Agent.Run")` | **Символ** | Полный код метода/функции + callers (кто вызывает) + callees (что вызывает) |
| `explore("RecordSuccessfulCall")` | **Символ** | Суффиксный поиск: находит `CircuitBreaker.RecordSuccessfulCall` автоматически |

> **Что сделали:**
> - Убрана пагинация (раньше 80 строк + offset → 5-7 вызовов для одной функции, теперь 1 вызов)
> - Добавлен пакетный уровень: `explore("internal/agent")` — обзор без кода тел
> - Добавлен суффиксный поиск: можно писать просто `RecordSuccessfulCall` без префикса struct
> - Дедупликация в агентском цикле: повторный вызов того же инструмента блокируется и возвращает СТОП в `content` tool-message (раньше — игнорируемый user-hint)

---

## Рантайм / Observability — ГОТОВО ✅

| Инструмент | Что делает | Лучше всего когда |
|---|---|---|
| `runtime_query` | По trace_id из OTel возвращает spans с привязкой к файлу/строке/FQN из CKG | Есть конкретный баг и trace_id — видно какой код выполнялся и в каком порядке |

---

## LSP — точная навигация ✅ ВКЛЮЧЕНО (polyglot)

`orchestra init` пишет `lsp.servers` по workspace detect (или go+ts+py fallback на пустом репо). Runtime merge отсекает серверы языков, которых нет в проекте (TS-only не держит gopls из polyglot yaml).

| Инструмент | Что делает | Лучше всего когда |
|---|---|---|
| `lsp.definition` | Go to definition по позиции (файл + строка + колонка) | Нужно найти где объявлена переменная/тип на конкретной строке |
| `lsp.references` | Все места использования символа | Нужно найти все вызовы перед переименованием или удалением |
| `lsp.hover` | Тип и документация символа в позиции | Нужно знать тип переменной не читая весь файл |
| `lsp.diagnostics` | Ошибки и warnings LSP для файла | После редактирования — проверить что не сломал типы |
| `lsp.rename` | Переименовать символ во всём проекте через LSP | Безопасный refactor — LSP знает все места использования |

> **Semantic dry-run (без `--apply`):** `edit`/`write` пишут в **staging overlay** (диск не трогается).
> Перед stage — **AST-Gate** (tree-sitter). После stage — **LSP sync** (`didOpen`/`didChange` с effective content).
> Ответ tool включает `diagnostics`; при errors agent inject'ит hint `LSP_ERRORS`.
> LSP servers **lazy** (`lsp.lazy_start: true`): Go+TS monorepo регистрирует оба, subprocess только на первый touch `.go`/`.ts`. `WarmupLSP` ensure'ит бинарники без eager spawn. TTL: `idle_ttl_seconds: 300`.
> Resolve: PATH → `~/.orchestra/lsp/<id>/<ver>/`. Auto-install / upgrade: TUI modal, `orchestra lsp ensure`, `orchestra lsp upgrade` — [`docs/architecture/lsp-auto-provision.md`](architecture/lsp-auto-provision.md).

> Все 5 инструментов покрыты интеграционными тестами с реальным gopls (`internal/tools/lsp_tools_test.go`).
> Dry-run + LSP loop: `tests/e2e_agent/e2e_dryrun_lsp_test.go`.

---

## Изменение файлов

| Инструмент | Что делает | Лучше всего когда |
|---|---|---|
| `edit` | Точечная замена search → replace (нужен уникальный search-фрагмент) | Правки в существующем файле — изменить конкретный кусок |
| `write` | Перезапись/создание файла целиком | Новый файл; или файл нужно переписать полностью |
| `diff.preview` | Применяет search→replace в памяти, возвращает unified diff | Хочешь проверить что изменится перед применением `edit` |

> В dry-run (`apply: false` / без `--apply`) `edit`/`write` обновляют staging overlay; `read`/`grep`/`ls` видят staged content.
> `--apply` или `agent.run apply: true` сбрасывает overlay на диск через applier (`conditions.file_hash`).

---

## Исполнение

| Инструмент | Что делает | Лучше всего когда |
|---|---|---|
| `bash` | Запуск команды в workspace (с timeout и лимитом вывода). С `run_in_background: true` — возвращает `bg_id` сразу, процесс работает в фоне | Foreground: быстрые команды (тесты, формат, ls). Background: dev-серверы, длинные билды/тесты, watch-процессы |
| `bash.output` | Возвращает новый stdout/stderr с прошлого опроса + статус (running/done/killed/timed_out) + exit code | Опросить прогресс/результат фонового процесса; `peek: true` — без сдвига курсора |
| `bash.kill` | Терминирует фоновый процесс по `bg_id` | Завершить ненужный dev-сервер / зависший процесс |

---

## Управление задачами и памятью

| Инструмент | Что делает | Лучше всего когда |
|---|---|---|
| `todowrite` | Обновить чеклист задач (виден в каждом шаге) | Длинная задача с несколькими шагами — держать трек прогресса |
| `todoread` | Прочитать текущий чеклист | Напомнить себе что осталось |
| `memory_write` | Сохранить факт в `.orchestra/memory/agent.md` (перс. между сессиями) | Важное решение или предпочтение пользователя которое нужно помнить в следующих сессиях |

---

## Параллельные подзадачи

| Инструмент | Что делает | Лучше всего когда |
|---|---|---|
| `task_spawn` | Создать дочерний агент для независимого исследования | Нужно одновременно исследовать несколько независимых частей кодовой базы |
| `task_wait` | Дождаться результата дочернего агента | После task_spawn |
| `task_cancel` | Отменить дочернюю задачу | Задача уже не нужна |
| `task_result` | Дочерний агент сообщает результат родителю | Только внутри дочернего агента |

---

## Режимы планирования

| Инструмент | Что делает | Лучше всего когда |
|---|---|---|
| `plan_exit` | Завершить план и запросить переход в build-режим | План в `.orchestra/plans/<id>.md` готов (только в `--mode plan`) |
| `question` | Задать вопрос пользователю (блокирует выполнение) | Критичный выбор который нельзя угадать из кода |

**Вход в plan:** `orchestra apply --mode plan` или RPC `mode: "plan"`. `plan_enter` **не** рекламируется как tool — legacy stub отвечает `not_supported` (см. `docs/tools-status.md`).

---

## Веб

| Инструмент | Что делает | Лучше всего когда |
|---|---|---|
| `webfetch` | Загрузить URL, вернуть текст | Нужна документация библиотеки, спецификация API |
| `websearch` | Поиск через Tavily / Brave (configurable provider) | Нужны актуальные ссылки или контекст которого нет в кодовой базе |

---

## Git и GitHub

| Инструмент | Что делает | Лучше всего когда |
|---|---|---|
| `git.status` / `git.diff` / `git.log` | Read-only git | Понять текущее состояние веток / истории |
| `git.commit` / `git.branch` / `git.checkout` / `git.push` | Mutating git (требует `--allow-exec`) | Управляемый workflow с подтверждениями |
| `gh.pr.list` / `gh.pr.view` / `gh.issue.list` / `gh.issue.view` | Read-only `gh` | Контекст из GitHub без выхода в браузер |
| `gh.pr.create` | Создать PR (требует `--allow-exec`) | Завершение задачи в git workflow |

---

## Браузер (Playwright MCP)

Регистрируются только при `--allow-browser` (нужны Node.js + `npx`). Полный набор: `browser.navigate`, `snapshot`, `screenshot`, `click`, `type`, `fill`, `select`, `eval`, `wait`, `close`. Используй для скриптинга реальных веб-проверок, e2e-flow в браузере, debug визуальных регрессий.

---

## Скиллы — переиспользуемые агент-бандлы ✅

| Инструмент | Что делает | Лучше всего когда |
|---|---|---|
| `skill_invoke` | Синхронно вызвать именованный скилл из `.orchestra/skills/` как child-agent с его prompt/tools/model/provider | Подзадача чётко описывается одним из доступных скиллов (видны в `<available_skills>` блоке системного промпта); хочешь делегировать только её, оставив родительский контекст |

Скилл — это `.md` файл с YAML frontmatter (`name`, `description`, `tools`, `model`, `provider`) и Markdown-body как system prompt. Поддерживается `$ARGUMENTS` подстановка. Подробности — `docs/skills.md`.

---

## Бэклог улучшений для локальных моделей

> **Контекст:** локальные модели (qwen, mistral) хуже интуитивно выбирают инструмент, чаще делают лишние вызовы и труднее останавливаются. Всё ниже направлено на то чтобы нужный ответ приходил за меньше шагов.

| # | Что | Зачем | Статус |
|---|---|---|---|
| 1 | `explore("internal/agent")` — пакетный обзор | Заменяет ls + symbols × N файлов одним вызовом | ✅ Готово |
| 2 | `explore` без пагинации, весь символ целиком | Убирает цепочки из 5-7 вызовов для одной функции | ✅ Готово |
| 3 | Суффиксный поиск по имени метода | `explore("RecordSuccessfulCall")` находит метод без знания receiver | ✅ Готово |
| 4 | Дедупликация в агентском цикле (СТОП в tool content) | Блокирует повторные вызовы до запуска, не после | ✅ Готово |
| 5 | **`read` → redirect для .go файлов** | Когда модель читает .go файл целиком — вернуть список символов + предложить `explore`. Сейчас модель получает 1700 строк сырого кода и засоряет контекст | ✅ Готово |
| 6 | **`grep` с привязкой к FQN (CKG-grep)** | Каждый match `.go` файла теперь содержит `symbol_fqn` — FQN метода/функции внутри которой найдена строка. `grep("IsDuplicateCall")` → `Agent.Run:704` | ✅ Готово |
| 7 | **Автодиагностика после `edit`** | После каждого `edit`/`write` на .go файл автоматически запускать `go vet` или `lsp.diagnostics` и добавлять результат в tool response | ✅ Готово — LSP diagnostics возвращаются в FSEditResponse.Diagnostics и FSWriteResponse.Diagnostics когда gopls настроен |
| 8 | **Граф зависимостей пакетов** | Секция "Зависимости" в `explore("internal/agent")` → что импортирует пакет + кто импортирует его | ✅ Готово |
| 9 | **`diff.preview`** | Показать что изменится перед применением патча, без записи на диск | ✅ Готово |

---

## Покрытие тестами (актуально на 2026-05-15)

Все инструменты покрыты автоматическими тестами. Единственный пробел — `question` (интерактивный stdin).

| Инструмент | Тест-файл | Тип |
|---|---|---|
| `ls`, `read`, `glob`, `write`, `edit`, `grep` | `*_test.go` в `internal/tools/` | Unit |
| `symbols`, `explore` | `symbols_test.go`, `explore_test.go` | Unit |
| `bash` | `exec_test.go` + `e2e_nollm` | Unit + E2E |
| `todowrite/read`, `memory_write`, `runtime_query` | `*_test.go` | Unit |
| `webfetch` | `webfetch_test.go` | Unit (SSRF + HTML) |
| `lsp.*` (5 шт.) | `lsp_tools_test.go` | Integration (gopls) |
| `task_spawn/wait/cancel/result` | `tasks_test.go` | Unit |
| `plan_enter/exit` | `agent_test.go` | Unit |
| `write`, `edit` staging (dry-run) | `staging_test.go` | Unit |
| `write`, `edit` E2E | `tests/e2e_real_llm/` | E2E (real LLM) |
| `diff.preview` | `diff_preview_test.go` | Unit |
