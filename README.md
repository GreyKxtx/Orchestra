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
| Prompt Pipeline | go:embed .txt промпты, маршрутизация по семейству модели (anthropic/gpt/gemini/kimi/local) | ✅ |
| Agent Modes | build, plan, explore, ask, debug, architecture, agent, orchestra, worker, … | ✅ |
| Prompt Caching | Anthropic `cache_control: ephemeral` — экономия ~90% токенов с шага 2 | ✅ |
| Lazy Instructions | Автоматическое обнаружение `ORCHESTRA.md` при чтении файлов | ✅ |
| Line Numbers | `fs.read` возвращает контент с номерами строк для точных edit-ссылок | ✅ |
| Forgiving Edit | Resolver: Pass 2 line-trimmed + Pass 3 indent-flexible (tab↔space) перед StaleContent | ✅ |
| WebFetch | `webfetch` — HTTP GET с SSRF-защитой (private/loopback/link-local заблокированы), HTML→text | ✅ |
| Compaction | Авто-сжатие истории при достижении `compact_threshold_pct` от `MaxPromptBytes`; LLM-summary, non-fatal fallback | ✅ |
| Memory tool | `memory_write` — агент записывает факты в `.orchestra/memory/agent.md`; `LoadProjectMemory` аддитивен (все 3 источника) | ✅ |
| Permission Rules | `permissions.rules` — per-tool allow/deny с glob-паттернами; first-match-wins; `allow` — bypass `--allow-exec/web` для одного вызова | ✅ |
| Parallel Tool Calls | `ParallelSafe`/`Mutating` флаги в `llm.ToolDef`; read-only тулы (`ls`/`read`/`glob`/`grep`/`symbols`/`explore`/`lsp.*`/`webfetch`) выполняются конкурентно в одной пачке через worker-pool (16); mutating (`write`/`edit`/`bash`) — серийно. Pre-tool hooks серийны до fan-out'а чтобы не race'ить по shared-state | ✅ |
| Reasoning Stream | Парсинг `delta.reasoning_content` / `delta.thinking_content` (Qwen3, DeepSeek-R1 через LM Studio); автоматическое заворачивание в `<think>…</think>` для `ReasoningSplitter`; SSE-tap по env-флагу `ORCH_STREAM_DEBUG` | ✅ |
| TUI (Phase 0-5) | Bubbletea + lipgloss; inline tool list, OpenCode-style busy-indicator в статус-баре, mouse wheel scroll, "Thinking:" блок с `┃` бордером, render-cache invalidation на Ctrl+T, mode-aware accent colors | ✅ |
| Planner–Worker | `mode=orchestra` Lead + `subagent_type=worker`, WorkOrder JSON, `target_symbol` scoping, LSP E2E | ✅ |
| Orchestra Lead surface | Strict allowlist **14 tools** (`listToolsOrchestra`); no edit/LSP/bash; Step-1 prompt **≤ 8k tokens** | ✅ |
| CKG v5 | Multi-hop explore (`depth`/`direction`), subgraph cap 1500 tokens, protocol **ToolsVersion 14** | ✅ |
| Learning stack | Dept lessons + playbooks with inject quotas; `lesson_promote` / `playbook_promote` | ✅ |
| LLM fail-fast | Unreachable endpoint (dial / refused / i/o timeout) aborts the turn — no false `prompt too large` compaction loop | ✅ |
| TUI Subagent Bar | Live child tasks (`child_started` / `child_queued` / `child_done`) | ✅ |
| Attachments / Vision | Protocol **v13**: images/SVG/PDF, staging `.orchestra/attachments/`, TUI `/attach`, VS Code drag-drop | ✅ |
| VS Code extension | Webview chat + settings, LSP install modal, per-file diff review, workspace editor for previews | ✅ |

---

## Установка

Готовые архивы для windows-amd64, linux-amd64, darwin-arm64 и darwin-amd64 публикуются в GitHub Releases (тег `v*`), рядом лежат `.sha256`. Распакуйте и положите `orchestra` в `PATH`.

Из исходников нужен C-компилятор — CKG собирается на tree-sitter через cgo (на Windows это MinGW):

```bash
go install github.com/orchestra/orchestra/cmd/orchestra@latest
```

Проверить, что установилось:

```bash
orchestra version
# orchestra v0.3.0 (a1b2c3d)
# protocol 13 · ops 1 · tools 14
```

Три числа — это контракт `initialize`: если TUI или расширение отказываются подключаться, расхождение будет именно в них.

## Быстрый старт

```bash
# Сборка из репозитория
go build -o orchestra ./cmd/orchestra

# Инициализация проекта
orchestra init

# Просмотр плана (без изменений файлов)
orchestra apply --plan-only "добавь логирование в main.go"

# Dry-run apply (по умолчанию — только показывает diff)
orchestra apply "добавь логирование в main.go"

# Реальное применение изменений (создаёт .orchestra.bak)
orchestra apply --apply "добавь логирование в main.go"

# Экспорт unified .patch для ревью (диск не трогается)
orchestra apply --output-patch "добавь логирование в main.go"
orchestra apply --output-patch ./review.patch "…"

# Adaptive profiles: fast или precision
orchestra apply --profile fast "поправь typo в README"
orchestra apply --profile precision "спроектируй пакет X"

# Разрешить выполнение команд через exec.run
orchestra apply --apply --allow-exec "запусти go test и исправь ошибки"

# Разрешить загрузку внешних URL через webfetch
orchestra apply --allow-web "изучи документацию на https://pkg.go.dev/... и добавь пример"

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

### VS Code / Cursor extension

Клиент в `ui/vscode/` — webview chat, settings, attachments/vision (protocol **v13**). Нужен собранный `orchestra` в PATH или рядом с репо.

```bash
go build -o orchestra ./cmd/orchestra
cd ui/vscode && npm ci && npm run compile
# F5 в VS Code, или упаковка: npm run package
```

Подробнее: `ui/vscode/README.md`.

---

## Конфигурация (`.orchestra.yml`)

```yaml
project_root: .
exclude_dirs: [.git, node_modules, dist]

llm:
  provider: openai          # "openai" | "anthropic"
  api_base: http://localhost:1234/v1   # LM Studio (Ollama: :11434/v1, vLLM: :8000/v1)
  api_key: ""
  model: qwen2.5-coder-7b-instruct
  max_tokens: 4096
  timeout_s: 120
  multimodal: true          # images in chat (TUI /attach, VS Code); needs vision-capable model

agent:
  profile: ""               # optional: fast | precision

apply:
  output: disk              # disk | patch
  patch_dir: .orchestra/patches

exec:
  confirm: true             # false = разрешить exec.run без --allow-exec

hooks:
  enabled: false
  pre_tool: ["sh", "-c", "echo pre"]  # ненулевой код = TOOL_DENIED
  post_tool: ["sh", "-c", "echo post"]
  timeout_ms: 5000

mcp:
  servers:
    # Локальный сервер — stdio-подпроцесс
    - name: my-server
      command: ["node", "mcp-server.js"]
      env: {API_KEY: "..."}
      disabled: false

    # Удалённый сервер — Streamable HTTP
    - name: github
      url: https://api.example.com/mcp
      bearer_token_env: GITHUB_MCP_TOKEN   # токен читается из окружения, не из конфига
      headers: {X-Tenant: acme}
      allowed_tools: ["repo_*"]
```

У сервера должно быть задано ровно одно из `command` и `url` — иначе конфиг не загрузится. Plaintext `http://` на не-loopback хост отклоняется (токен ушёл бы по сети открытым); если это внутренняя сеть и вы этого хотите — `allow_insecure_http: true`.

### Секреты: `.orchestra.local.yml`

API-ключи и личные оверрайды кладите в `.orchestra.local.yml` рядом с `.orchestra.yml` (файл в `.gitignore`, `orchestra init` добавляет его туда автоматически). Оверлей deep-merge'ится поверх основного конфига при загрузке; при сохранении настроек (TUI / VS Code) значения из оверлея **не** записываются в общий `.orchestra.yml`:

```yaml
# .orchestra.local.yml — не коммитится
llm:
  api_key: sk-or-...
providers:
  openrouter:
    api_key: sk-or-...   # маскируется только этот лист, остальные поля провайдера — из .orchestra.yml
```

### Глобальный конфиг: `~/.orchestra/config.yml`

Настройки, одинаковые во всех проектах — провайдеры, ключи, тиры, предпочтения — живут в `~/.orchestra/config.yml`. Проектный `.orchestra.yml` указывает только то, что отличается.

Порядок наложения (позже — сильнее):

```
~/.orchestra/config.yml          пользовательские значения по умолчанию
<проект>/.orchestra.yml          общий, коммитится
<проект>/.orchestra.local.yml    машинные оверрайды и секреты
```

`project_root` из глобального файла игнорируется — иначе все проекты смотрели бы в одну директорию. Ключи, заданные глобально, при сохранении настроек из TUI / VS Code **не** записываются в проектный файл: `.orchestra.yml` коммитится, и утащить туда ключ из домашней директории нельзя.

### Память проекта

Создайте `ORCHESTRA.md` в корне проекта — он будет автоматически инжектироваться в системный промпт агента (макс. 2 КБ). Альтернативно: `.orchestra/memory/*.md` или `~/.orchestra/memory.md`.

---

## Архитектура (ключевые абстракции)

**Два уровня патчей — строго разделены:**

- **External Patches** (`internal/patches`) — гибкий LLM-формат: `file.search_replace`, `file.unified_diff`, `file.write_atomic`. Содержат `file_hash` версии, которую читал LLM.
- **Internal Ops** (`internal/ops`) — детерминированный формат записи на диск: `file.replace_range`, `file.write_atomic`, `file.mkdir_all`. Координаты 0-based, end-exclusive. Каждая операция содержит `conditions.file_hash`.
- `internal/resolver` — мост: `ResolveExternalPatches` конвертирует External → Internal, перечитывая файлы и вычисляя точные диапазоны.
- `internal/applier` — запись ops; при `apply.output=patch` / `--output-patch` — unified diff без записи workspace.

**Agent loop** (`internal/agent/agent.go`): системный промпт + история → `llm.Complete` → `tool_call` (выполнить, добавить в историю, продолжить) или `final` (резолвить патчи → применить). Recoverable ошибки (`StaleContent`, `AmbiguousMatch`) возвращаются в историю компактными хинтами. Профили `fast`/`precision` — см. `docs/architecture/`.

**Три режима `apply`:**
1. `direct` — агент in-process.
2. `--via-core` — спавнит `orchestra core` как subprocess, управляет через JSON-RPC.
3. `--from-plan` — воспроизводит сохранённый `plan.json` без LLM.

Аудит TUI-пайплайна: [docs/architecture/tui-pipeline.md](docs/architecture/tui-pipeline.md). Planner–Worker: [docs/architecture/planner-worker.md](docs/architecture/planner-worker.md).

---

## Тесты

```bash
go vet ./...
go test ./...
go test -race ./...

# Один пакет / один тест
go test ./internal/agent -run TestAgent_Run -v
go test ./protocol/jsonrpc -race -count=10

# E2E с реальным LLM (не входит в CI)
$env:ORCH_E2E_LLM = "1"
go test ./tests/e2e_real_llm -v -count=1

# Planner–Worker E2E (mock, в CI)
go test ./tests/e2e_agent/... -run 'Orchestra|Worker|Ambiguous|Staging' -count=1
```

## TUI (консольный агент)

```bash
orchestra                  # интерактивный TUI (по умолчанию)
orchestra --apply          # LIVE: сразу пишет на диск
orchestra --apply --allow-exec
orchestra tui              # alias
```
 
---

## Документация

- [Changelog](docs/CHANGELOG.md)
- [Protocol contract](docs/PROTOCOL.md)
- [Roadmap](docs/ROADMAP.md)
- [Agent modes](docs/modes.md) — authoritative для режимов
- [Planner–Worker architecture](docs/architecture/planner-worker.md)
- [TUI pipeline](docs/architecture/tui-pipeline.md)
- [LSP auto-provision](docs/architecture/lsp-auto-provision.md)
- [Commands & CLI reference](docs/commands-and-modes.md)
- [Tools & commands status](docs/tools-status.md)
- [Package paths (authoritative map)](docs/architecture/paths.md)
- [Module layout & import rules](docs/architecture/modules.md)

---

## Требования

- Go 1.22+
- LLM API: OpenAI-совместимый провайдер (LM Studio, vLLM, OpenAI, Anthropic…)

## Лицензия

MIT
