# Инструменты и команды Orchestra — статус реализации

> Источник истины: `internal/tools/registry.go` (`ListTools`/`ListToolsWithSubtasks`/`ListToolsForMode`). Этот документ — человекочитаемая сводка.

## CLI-команды

| Команда | Статус | Примечание |
|---------|--------|------------|
| `orchestra init` | ✅ | Создаёт `.orchestra.yml` в cwd |
| `orchestra core` | ✅ | JSON-RPC 2.0 stdio сервер; `--http` отладочный режим |
| `orchestra apply` | ✅ | Dry-run / `--apply` / `--via-core` / `--from-plan` / `--pipeline` / `--mode` / `--provider` / `--skill` |
| `orchestra chat` | ✅ | Интерактивный REPL поверх `orchestra core` |
| `orchestra search` | ✅ | Regex-поиск с учётом `exclude_dirs` |
| `orchestra llm-ping` | ✅ | Smoke-test LLM, пишет результат в `.orchestra/` |
| `orchestra eval` | ✅ | YAML-задачи с изолированными воркспейсами |
| `orchestra runtime ingest` | ✅ | OTel → SQLite CKG |
| `orchestra ckg-ui` | ✅ | HTTP-визуализатор CKG (`:6061`) |
| `orchestra demo tiny-go` | ✅ | Smoke-test пайплайна патчей без LLM |
| `orchestra daemon` | ✅ (legacy) | HTTP v0.3 демон, только loopback |
| `orchestra mcp list-tools` | ✅ | Перечень тулов всех enabled MCP-серверов |
| `orchestra skills list \| show` | ✅ | Скиллы из `.orchestra/skills/` (user + project) |
| `orchestra instrument` | ✅ | Авто-инструментация OTel для проекта |

---

## Инструменты агента (tool_call)

Имя — то, которое видит LLM (короткий alias). Внутреннее — canonical имя в `Runner.Call`.

### Файловая система

| Имя | Внутреннее | Статус | Что делает |
|-----|-----------|--------|------------|
| `ls` | `fs.list` | ✅ | Листинг файлов с exclude-правилами |
| `read` | `fs.read` | ✅ | Чтение файла; возвращает content + `file_hash` + номера строк |
| `glob` | `fs.glob` | ✅ | Поиск файлов по glob-паттерну (`**` поддерживается) |
| `write` | `fs.write` | ✅ | Атомарная запись (temp → fsync → rename); в dry-run пишет в staging overlay |
| `edit` | `fs.edit` | ✅ | Search-and-replace; строгий: StaleContent / AmbiguousMatch при несовпадении; staging в dry-run |
| `fs.delete` | `fs.delete` | ✅ | Удаление файла; respect project_root |
| `fs.rename` | `fs.rename` | ✅ | Перемещение/переименование файла |
| `diff.preview` | `diff.preview` | ✅ | Diff между текущим контентом и предложенным изменением (до apply) |

### Поиск и навигация

| Имя | Внутреннее | Статус | Что делает |
|-----|-----------|--------|------------|
| `grep` | `search.text` | ✅ | Regex-поиск; авто-fallback на ripgrep если есть в PATH |
| `symbols` | `code.symbols` | ✅ | Символы / outline файла |
| `explore` | `explore_codebase` | ✅ | CKG: пакет / тип / символ — авто-выбор уровня по форме запроса |

### LSP (feedback после правок)

| Имя | Статус | Что делает |
|-----|--------|------------|
| `lsp.definition` | ✅ | Перейти к определению |
| `lsp.references` | ✅ | Где используется символ |
| `lsp.hover` | ✅ | Hover-инфо для символа |
| `lsp.diagnostics` | ✅ | Получить диагностику файла; авто-injection в history после write/edit |
| `lsp.rename` | ✅ | Rename refactor через LSP |

### Выполнение

| Имя | Внутреннее | Статус | Что делает |
|-----|-----------|--------|------------|
| `bash` | `exec.run` | ✅ | Shell-команда, timeout + output cap; требует `--allow-exec`. С `run_in_background: true` — возвращает `bg_id` сразу |
| `bash.output` | — | ✅ | Новый stdout/stderr с прошлого опроса + статус/exit code для bg-процесса; `peek: true` без сдвига курсора |
| `bash.kill` | — | ✅ | Терминирует bg-процесс |
| `webfetch` | `web.fetch` | ✅ | HTTP GET URL → текст; SSRF-защита; требует `--allow-web` |
| `websearch` | `web.search` | ✅ | Поиск через Tavily / Brave (provider в `.orchestra.yml`); требует `--allow-web` |
| `memory_write` | `memory.write` | ✅ | Записывает факт в `.orchestra/memory/agent.md` с timestamp |

### Git и GitHub

| Имя | Статус | Что делает |
|-----|--------|------------|
| `git.status` | ✅ | `git status --porcelain` |
| `git.log` | ✅ | История коммитов |
| `git.diff` | ✅ | Diff staged/unstaged/против ref |
| `git.commit` | ✅ | Создать коммит (требует `--allow-exec`) |
| `git.branch` | ✅ | Список / создание веток |
| `git.checkout` | ✅ | Переключение / создание ветки |
| `git.push` | ✅ | Push с гардами |
| `gh.pr.list` | ✅ | Список PR (через `gh`) |
| `gh.pr.create` | ✅ | Создать PR (требует `--allow-exec`) |
| `gh.pr.view` | ✅ | Просмотр PR |
| `gh.issue.list` | ✅ | Список issues |
| `gh.issue.view` | ✅ | Просмотр issue |

### Браузер (Playwright MCP)

Регистрируются только при `--allow-browser` (требует Node.js + `npx`).

| Имя | Статус |
|-----|--------|
| `browser.navigate` | ✅ |
| `browser.snapshot` | ✅ |
| `browser.screenshot` | ✅ |
| `browser.click` | ✅ |
| `browser.type` | ✅ |
| `browser.fill` | ✅ |
| `browser.select` | ✅ |
| `browser.eval` | ✅ |
| `browser.wait` | ✅ |
| `browser.close` | ✅ |

### Задачи и сессия

| Имя | Внутреннее | Статус | Что делает |
|-----|-----------|--------|------------|
| `todowrite` | `todo.write` | ✅ | Обновить чеклист задач сессии |
| `todoread` | `todo.read` | ✅ | Прочитать чеклист |
| `task_spawn` | `task.spawn` | ✅ | Создать дочерний агент-задачу |
| `task_wait` | `task.wait` | ✅ | Дождаться результата дочерней задачи |
| `task_cancel` | `task.cancel` | ✅ | Отменить дочернюю задачу |
| `task_result` | `task.result` | ✅ | Вернуть результат родительскому агенту (subagent path) |
| `skill_invoke` | `skill_invoke` | ✅ | Синхронный вызов скилла как child-agent с его prompt/tools/model/provider |

### Режимы и планирование

| Имя | Статус | Что делает |
|-----|--------|------------|
| `plan_enter` | ✅ | Переключиться в режим `plan` (read-only) |
| `plan_exit` | ✅ | Выйти из `plan`, запросить переключение в `build` |
| `question` | ✅ | Задать уточняющий вопрос пользователю (блокирует до ответа) |

### Runtime / CKG

| Имя | Статус | Что делает |
|-----|--------|------------|
| `runtime_query` | ✅ | OTel-spans с привязкой к CKG-узлам по `trace_id` |

### MCP-server tools

Тулы внешних MCP-серверов из `mcp:` в `.orchestra.yml` подключаются под именами `mcp:<server>:<tool>`. Перечень — `orchestra mcp list-tools`.

---

## Конфигурация и роадмапы

- **Core-parity roadmap A–G** — закрыт. Tool aliases, forgiving resolver (LineTrimmed + IndentationFlexible), compaction-агент, кастомные агенты, permission ruleset, webfetch, memory tool, LSP.
- **Competitive gap roadmap 1–6** — закрыт. ripgrep, GitHub tools, LSP diagnostics, MCP-CLI, multi-provider auth, skills (CLI + `skill_invoke` + `$ARGUMENTS` + user-global dir).
- **Двухслойная архитектура патчей + staging overlay в dry-run** — `--apply` корректно отделяет план от записи; `write`/`edit` в dry-run пишут в memory overlay, на диск не попадают.
