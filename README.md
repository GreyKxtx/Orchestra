# Orchestra

**Local AI coding assistant** — LLM читает проект, планирует правки и безопасно применяет их.

Основной транспорт: **JSON-RPC 2.0 over stdio** (LSP-стиль); поверх — CLI.

---

## Возможности

| Фаза | Фича | Статус |
|------|------|--------|
| Core | JSON-RPC 2.0 stdio, agent loop, external/internal patches | ✅ |
| Streaming | SSE-стриминг, накопитель tool-call чанков | ✅ |
| Grammar | Structured output, retry/circuit-breaker, prompt families | ✅ |
| Session | История диалога, todo-лист, `agent.run` по JSON-RPC | ✅ |
| Subagents | `task.spawn/wait/cancel`, дочерние агенты с read-only инструментами | ✅ |
| Hooks | Pre/post-tool shell-хуки, `TOOL_DENIED` при ненулевом коде | ✅ |
| Memory | `ORCHESTRA.md` → `.orchestra/memory/*.md` → `~/.orchestra/memory.md` | ✅ |
| MCP | JSON-RPC 2.0 stdio MCP-клиент, мульти-сервер менеджер | ✅ |
| Providers | Anthropic API + OpenAI-совместимые провайдеры (LM Studio, vLLM…) | ✅ |
| Eval | YAML-задачи, изолированные воркспейсы, `orchestra eval` | ✅ |

---

## Быстрый старт

```bash
# Сборка
go build -o orchestra ./cmd/orchestra

# Инициализация проекта
orchestra init

# Просмотр плана (без изменений файлов)
orchestra apply --plan-only "добавь логирование в main.go"

# Dry-run apply (по умолчанию — только показывает diff)
orchestra apply "добавь логирование в main.go"

# Реальное применение изменений (создаёт .orchestra.bak)
orchestra apply --apply "добавь логирование в main.go"

# Разрешить выполнение команд через exec.run
orchestra apply --apply --allow-exec "запусти go test и исправь ошибки"

# Через subprocess core (JSON-RPC stdio, изолированный)
orchestra apply --via-core "добавь функцию Sum"

# Smoke-test подключения к LLM
orchestra llm-ping

# Поиск по коду
orchestra search "function main"

# Запуск eval-задач (нужен работающий LLM)
orchestra eval                          # tests/eval/tasks/ по умолчанию
orchestra eval path/to/tasks/           # своя директория
```

---

## Конфигурация (`.orchestra.yml`)

```yaml
project_root: .
exclude_dirs: [.git, node_modules, dist]

llm:
  provider: openai          # "openai" | "anthropic"
  api_base: http://localhost:1234   # LM Studio, vLLM, OpenAI…
  api_key: ""
  model: qwen2.5-coder-7b-instruct
  max_tokens: 4096
  timeout_s: 120

exec:
  confirm: true             # false = разрешить exec.run без --allow-exec

hooks:
  enabled: false
  pre_tool: ["sh", "-c", "echo pre"]  # ненулевой код = TOOL_DENIED
  post_tool: ["sh", "-c", "echo post"]
  timeout_ms: 5000

mcp:
  servers:
    - name: my-server
      command: ["node", "mcp-server.js"]
      env: {API_KEY: "..."}
      disabled: false
```

### Память проекта

Создайте `ORCHESTRA.md` в корне проекта — он будет автоматически инжектироваться в системный промпт агента (макс. 2 КБ). Альтернативно: `.orchestra/memory/*.md` или `~/.orchestra/memory.md`.

---

## Архитектура (ключевые абстракции)

**Два уровня патчей — строго разделены:**

- **External Patches** (`internal/externalpatch`) — гибкий LLM-формат: `file.search_replace`, `file.unified_diff`, `file.write_atomic`. Содержат `file_hash` версии, которую читал LLM.
- **Internal Ops** (`internal/ops`) — детерминированный формат записи на диск: `file.replace_range`, `file.write_atomic`, `file.mkdir_all`. Координаты 0-based, end-exclusive. Каждая операция содержит `conditions.file_hash`.
- `internal/resolver` — мост: `ResolveExternalPatches` конвертирует External → Internal, перечитывая файлы и вычисляя точные диапазоны.

**Agent loop** (`internal/agent/agent.go`): системный промпт + история → `llm.Complete` → `tool_call` (выполнить, добавить в историю, продолжить) или `final` (резолвить патчи → применить). Recoverable ошибки (`StaleContent`, `AmbiguousMatch`) возвращаются в историю компактными хинтами.

**Три режима `apply`:**
1. `direct` — агент in-process.
2. `--via-core` — спавнит `orchestra core` как subprocess, управляет через JSON-RPC.
3. `--from-plan` — воспроизводит сохранённый `plan.json` без LLM.

---

## Тесты

```bash
go vet ./...
go test ./...
go test -race ./...

# Один пакет / один тест
go test ./internal/agent -run TestAgent_Run -v
go test ./internal/jsonrpc -race -count=10

# E2E с реальным LLM (не входит в CI)
$env:ORCH_E2E_LLM = "1"
go test ./tests/e2e_real_llm -v -count=1
```

---

## Документация

- [Changelog](docs/CHANGELOG.md)
- [Protocol contract](docs/PROTOCOL.md)
- [Roadmap](docs/ROADMAP.md)

---

## Требования

- Go 1.22+
- LLM API: OpenAI-совместимый провайдер (LM Studio, vLLM, OpenAI, Anthropic…)

## Лицензия

MIT
