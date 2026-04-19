# Orchestra vNext — ТЗ ядра и архитектура

## 0. Назначение

**Orchestra** — ядро AI‑оркестратора для работы с кодовой базой.

Цели vNext:

- Единый **JSON‑контракт** для CLI/IDE/Daemon.
- Agent Loop с инструментами (tools) и безопасным применением правок.
- Готовность к индексации (Tree-sitter) и расширению без переписывания архитектуры.

Не цели vNext (можно позже): полноценный RAG/векторная БД, file-watching, сложные рефакторинги уровня типов.

---

## 1. Термины

- **Workspace** — корень проекта.
- **Daemon** — долгоживущий процесс, держит состояние workspace.
- **Client** — CLI или IDE‑расширение.
- **Tool** — действие, которое агент может вызвать (fs/search/exec/code).\*
- **External Patch** — гибкий формат правок, удобный для LLM.
- **Internal Ops** — строгие операции, которые применяет ядро.
- **Resolver** — компонент, превращающий External Patch → Internal Ops.

---

## 2. Архитектура (high-level)

### 2.1. Слои

1. **Transport** (Client ↔ Core): JSON‑RPC 2.0 (stdio). Опционально HTTP для отладки.
2. **Core**: Agent Loop + ToolRunner + Resolver + Applier.
3. **State**: Cache/Index в памяти + persisted snapshot (.orchestra/\*).

### 2.2. Режимы

- **CLI mode**: запускает Core как процесс (stdio) или подключается к daemon.
- **Daemon mode**: обслуживает workspace постоянно, ускоряет контекст/поиск.

---

## 3. Transport: JSON‑RPC 2.0 (основной)

### 3.1. Почему stdio

- Ноль проблем с портами.
- Стандарт де‑факто для IDE (LSP‑подход).
- Безопасность: доступ только у родительского процесса.

### 3.2. Формат сообщений

**Request**

```json
{ "jsonrpc":"2.0", "id":1, "method":"agent.run", "params":{...} }
```

**Response**

```json
{ "jsonrpc":"2.0", "id":1, "result":{...} }
```

**Error**

```json
{ "jsonrpc":"2.0", "id":1, "error":{ "code":-32001, "message":"...", "data":{...} } }
```

### 3.3. HTTP (опционально)

- Только локально (127.0.0.1).
- Если включено — **обязателен token** в `.orchestra/daemon.json`.

---

## 4. Версионирование

- `protocol_version`: версия JSON‑RPC методов/схем.
- `ops_version`: версия формата Internal Ops.
- `tools_version`: версия интерфейсов инструментов.
- Совместимость: Client обязан проверять `/health` (или rpc `core.health`) и делать fallback при несовместимости.

---

## 5. Data Model: документы и версии

### 5.1. File identity

Каждая операция и ответ инструментов должны опираться на версионность документа:

- `doc_version` (монотонный счетчик на файл в daemon) **или**
- `file_hash` (хэш содержимого файла на момент планирования).

Минимум vNext: **file\_hash**.

---

## 6. Patch Model

### 6.1. External Patch (то, что получает Core от LLM)

LLM не обязан считать точные координаты. Разрешены внешние формы:

- `file.search_replace` (старый блок → новый блок)
- `file.unified_diff`

Core обязан прогнать External Patch через **Schema validation** и **Resolver**.

Пример External (search/replace):

```json
{
  "type":"file.search_replace",
  "path":"internal/foo.go",
  "search":"func Foo() {\n  ...\n}",
  "replace":"func Foo() {\n  // updated\n}",
  "file_hash":"sha256:..."
}
```

### 6.2. Internal Ops v1 (то, что реально применяется)

Базовый op: `file.replace_range`.

```json
{
  "op":"file.replace_range",
  "path":"internal/foo.go",
  "range":{
    "start":{ "line":10, "col":0 },
    "end":  { "line":20, "col":1 }
  },
  "expected":"// old content (exact)",
  "replacement":"// new content",
  "conditions":{
    "file_hash":"sha256:...",
    "allow_fuzzy":true,
    "fuzzy_window":2
  }
}
```

Правила:

- Координаты **0-based line/col**.
- `expected` **обязателен** и проверяется.
- Если `file_hash` не совпал — это сигнал возможного stale.

---

## 7. Resolver Policy (Strict + ограниченный Fuzzy)

Алгоритм применения одного `file.replace_range`:

1. **Strict Match**

- Сверяем срез по `range` и `expected` (1-в-1).
- Если совпало → применяем.

2. **Hash Check**

- Если strict не совпал и `conditions.file_hash` задан:
  - если hash файла **не совпал** → stale‑сценарий.

3. **Fuzzy Fallback** (если `allow_fuzzy=true`)

- Ищем `expected` в окне строк `[start.line - N, start.line + N]`.
- Если найден **ровно один** матч → пересчитываем range и применяем.
- Если 0 или >1 матч → ошибка **StaleContent**.

4. **Ошибка**

- Возвращаем структурированную ошибку, агент должен перечитать файл и построить новые ops.

---

## 8. Tools (vNext)

### 8.1. Минимальный набор

- `fs.list` — список файлов (с exclude правилами)
- `fs.read` — чтение файла (с лимитами)
- `fs.apply_ops` — применить Internal Ops (с dry-run поддержкой)
- `search.text` — текстовый поиск (лексический)
- `code.symbols` — outline (функции/классы) через Tree-sitter (с деградацией)
- `exec.run` — запуск команд (с safety contract)

Запрещено по умолчанию:

- `net.fetch` и любые сетевые инструменты.

---

## 9. Exec Safety Contract

Требования к `exec.run`:

1. **Non-interactive**: stdin закрыт (EOF).
2. **Timeout**: дефолт 30s, принудительное завершение при превышении.
3. **Output limits**: stdout/stderr суммарно ≤ 100KB (обрезка + `truncated=true`).
4. **Workdir**: строго внутри workspace.
5. **Return object**:

```json
{
  "exit_code":0,
  "stdout":"...",
  "stderr":"...",
  "duration_ms":150,
  "truncated":false
}
```

6. **Consent policy (CLI/IDE)**:

- По умолчанию: подтверждение пользователя перед выполнением команд.
- Позже: allowlist/denylist в конфиге.

---

## 10. Индексация и Tree-sitter

### 10.1. Интерфейс

`SymbolProvider` (минимум):

- `GetDocumentSymbols(path) -> []Symbol`

### 10.2. Graceful Degradation

При недоступном/битом синтаксисе:

- Tier 1: Tree-sitter parse ok → точные символы.
- Tier 2: Regex/heuristics (func/class/def) → приблизительные символы.
- Tier 3: пустой результат (но система продолжает работать через fs/search).

Ошибки парсинга: только debug‑лог, без падения.

Языки vNext:

- обязательный: Go
- желательные (через LanguageProvider): TypeScript/JavaScript, Python

---

## 11. LLM протокол (JSON-only) + Schema Enforcement

### 11.1. Правило

- Core принимает от LLM только **валидный JSON** по схеме.

### 11.2. Validator middleware

- Любой LLM output (plan, patches) валидируется JSON Schema.
- При ошибке:
  - Core НЕ применяет правки.
  - Core возвращает модели tool‑ответ: `Invalid JSON format: ...`.
  - Автокоррекция: до 3 попыток.

---

## 12. Agent Loop (минимум)

Цикл:

1. Think (LLM)
2. Tool call
3. Observe
4. Decide next

Политики:

- лимиты контекста (KB/bytes)
- лимиты шагов (max\_steps)
- логирование каждого tool-call и результата (в debug)

---

## 13. Error Model (ядро)

Коды ошибок (минимум):

- `InvalidLLMOutput` (schema / parse)
- `StaleContent` (expected не совпал, fuzzy не помог)
- `AmbiguousMatch` (fuzzy нашел >1)
- `PathTraversal` (выход за workspace)
- `ExecTimeout` / `ExecFailed`

---

## 14. Конфигурация

`.orchestra.yml` (минимум):

- exclude dirs/globs
- limits: context\_kb, max\_files, max\_bytes\_per\_file
- daemon: enabled, scan\_interval
- exec: confirm=true, timeout\_s, output\_limit\_kb
- languages: enabled parsers

---

## 15. Тестирование

- Unit: parser, resolver, applier, tools, config.
- Integration: mock LLM server + full flow.
- Perf: benchmarks context-only (direct / daemon-inproc / daemon-http).

---

## 16. Definition of Done (vNext ядра)

- JSON‑RPC контракт стабилен (protocol\_version фиксирован).
- External Patch → Resolver → Internal Ops применяются безопасно.
- StaleContent корректно ловится и не портит файлы.
- exec.run безопасен (timeouts, non-interactive, output limits).
- Tree-sitter интегрирован с деградацией.
- Все тесты и бенчи проходят.

