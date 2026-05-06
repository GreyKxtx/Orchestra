# Инструменты и команды Orchestra — статус реализации

## CLI-команды

| Команда | Статус | Примечание |
|---------|--------|------------|
| `orchestra init` | ✅ | Создаёт `.orchestra.yml` в cwd |
| `orchestra core` | ✅ | JSON-RPC 2.0 stdio сервер; `--http` отладочный режим |
| `orchestra apply` | ✅ | Dry-run / `--apply` / `--via-core` / `--from-plan` / `--pipeline` |
| `orchestra chat` | ✅ | Интерактивный REPL поверх `orchestra core` |
| `orchestra search` | ✅ | Regex-поиск с учётом `exclude_dirs` |
| `orchestra llm-ping` | ✅ | Smoke-test LLM, пишет результат в `.orchestra/` |
| `orchestra eval` | ✅ | YAML-задачи с изолированными воркспейсами |
| `orchestra runtime ingest` | ✅ | OTel → SQLite CKG |
| `orchestra ckg-ui` | ✅ | HTTP-визуализатор CKG (`:6061`) |
| `orchestra demo tiny-go` | ✅ | Smoke-test пайплайна патчей без LLM |
| `orchestra daemon` | ✅ (legacy) | HTTP v0.3 демон, только loopback |

---

## Инструменты агента (tool_call)

Имя — то, которое видит LLM.

### Файловая система

| Имя | Внутреннее | Статус | Что делает |
|-----|-----------|--------|------------|
| `ls` | `fs.list` | ✅ | Листинг файлов с exclude-правилами |
| `read` | `fs.read` | ✅ | Чтение файла; возвращает content + `file_hash` + номера строк |
| `glob` | `fs.glob` | ✅ | Поиск файлов по glob-паттерну (`**` поддерживается) |
| `write` | `fs.write` | ✅ | Атомарная запись (temp → fsync → rename), backup по запросу |
| `edit` | `fs.edit` | ✅ | Search-and-replace; строгий: StaleContent / AmbiguousMatch при несовпадении |

### Поиск и навигация

| Имя | Внутреннее | Статус | Что делает |
|-----|-----------|--------|------------|
| `grep` | `search.text` | ✅ | Regex-поиск по содержимому файлов |
| `symbols` | `code.symbols` | ✅ | Символы / outline файла |
| `explore` | `explore_codebase` | ✅ | Поиск символа по имени + мест использования |

### Выполнение

| Имя | Внутреннее | Статус | Что делает |
|-----|-----------|--------|------------|
| `bash` | `exec.run` | ✅ | Shell-команда, timeout + output cap; требует `--allow-exec` |

### Задачи и сессия

| Имя | Внутреннее | Статус | Что делает |
|-----|-----------|--------|------------|
| `todowrite` | `todo.write` | ✅ | Обновить чеклист задач сессии |
| `todoread` | `todo.read` | ✅ | Прочитать чеклист |
| `task_spawn` | `task.spawn` | ✅ | Создать дочерний агент-задачу |
| `task_wait` | `task.wait` | ✅ | Дождаться результата дочерней задачи |
| `task_cancel` | `task.cancel` | ✅ | Отменить дочернюю задачу |
| `task_result` | `task.result` | ✅ | Вернуть результат родительскому агенту (используется subagent'ами) |

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

---

## Не реализовано

| Инструмент | Источник | Приоритет |
|-----------|---------|-----------|
| `webfetch` | OpenCode | средний |
| `websearch` | OpenCode | средний |
| LSP diagnostics | OpenCode | средний (в перспективе — дополнение к CKG) |
| GitHub / PR | OpenCode | низкий |
| Менеджер ключей провайдеров | OpenCode `auth` | низкий |
