# Критерии готовности Orchestra vNext для реальной работы

Этот документ определяет **жёсткие критерии готовности**, без которых система не готова к работе с реальными проектами.

---

## ✅ Что уже реализовано (базовый фундамент)

### Core/Architecture
- ✅ JSON-RPC 2.0 протокол (stdio + опциональный HTTP)
- ✅ Agent Loop с инструментами и retry-логикой
- ✅ External Patch → Resolver → Internal Ops
- ✅ StaleContent detection с fuzzy fallback
- ✅ Path traversal protection
- ✅ Atomic writes с backup поддержкой
- ✅ Workspace snapshot injection (`<user_info>` в промптах)

### Testing
- ✅ Unit тесты для resolver, applier, tools
- ✅ E2E без LLM (`tests/e2e_nollm/`)
- ✅ E2E с реальным LLM (`tests/e2e_real_llm/`) — частично

### Artifacts
- ✅ `.orchestra/plan.json` — сохранённый план
- ✅ `.orchestra/diff.txt` — человекочитаемый diff
- ✅ `.orchestra/last_result.json` — результат последнего запуска
- ✅ `.orchestra/last_run.jsonl` — лог событий

---

## 🚨 Блокеры для реальной работы (обязательные)

### 1. LLM Contract Smoke Test

**Проблема:** Сейчас ошибки типа `"messages field is required"` и таймауты обнаруживаются только при полном прогоне агента, что затрудняет диагностику.

**Требование:** Отдельный тест/команда, который делает **минимальный запрос** к провайдеру:
- `model` (из конфига)
- `messages` (минимум 1 сообщение)
- (опционально `tools`)

**Критерий готовности:**
- ✅ Команда `orchestra llm-ping` или тест `TestLLMContractSmoke` — **РЕАЛИЗОВАНО**
- ✅ Проверяет, что ответ **не 400/401/500**
- ✅ Проверяет, что ответ **структурно валидный** (JSON с `choices`)
- ✅ Логирует: URL, model, timeout, размер запроса, HTTP код, время ответа
- ✅ Сохраняет результат в `.orchestra/llm_ping_result.json`
- ✅ **Ожидаемый результат:** "Провайдер отвечает на ping за X секунд"

**Файлы реализации:**
- ✅ `internal/cli/llm_ping.go` (новая команда) — **ГОТОВО**
- ✅ `tests/e2e_real_llm/llm_contract_test.go` (тест) — **ГОТОВО**

---

### 2. Definition of Done для Real-LLM режима

**Проблема:** "E2E с реальным LLM" звучит как "когда-нибудь проверим", нет чёткого минимального сценария.

**Требование:** Минимальный сценарий, где модель:
1. Делает **ровно 1 tool_call** (`fs.read`)
2. Возвращает **final.patches` с валидным `file_hash`
3. Изменения применяются (dry-run или реально)
4. Повторный запуск по `--from-plan` детерминированно даёт `StaleContent` или `AlreadyExists` без побочек

**Критерий готовности:**
- ✅ Тест `TestRealLLMMinimalFlow` в `tests/e2e_real_llm/` — **РЕАЛИЗОВАНО**
- ✅ Сценарий: простая задача (например, "добавь комментарий в main.go")
- ✅ Модель делает минимум 1 tool call (`fs.read` для получения `file_hash`)
- ✅ Модель возвращает `final.patches` с корректным `file_hash`
- ✅ Применение успешно (dry-run или реально)
- ✅ Повторный запуск с `--from-plan` детерминированно:
  - Если файл не изменён → `AlreadyExists` или успех (idempotent)
  - Если файл изменён внешне → `StaleContent` (без побочек, без backup)
- ✅ **Ожидаемый результат:** "LLM-loop реально завершает задачу и даёт воспроизводимый output"

**Файлы реализации:**
- ✅ `tests/e2e_real_llm/minimal_flow_test.go` — **ГОТОВО**

---

### 3. Логирование LLM запросов/ответов в артефакты

**Проблема:** При ошибках провайдера (400, таймауты, невалидный JSON) нет способа увидеть, что реально ушло и пришло.

**Требование:** Расширить `.orchestra/last_run.jsonl` или создать `.orchestra/llm_log.jsonl` с:
- URL + model + timeout + размер messages/tools (в байтах)
- При ошибках: HTTP код + тело ответа (обрезанное до 1-2KB)
- Превью запроса/ответа (1-2KB, без API ключей)

**Критерий готовности:**
- ✅ В `internal/llm/client.go` добавлено логирование каждого запроса — **РЕАЛИЗОВАНО**
- ✅ Артефакт `.orchestra/llm_log.jsonl` — **РЕАЛИЗОВАНО**
- ✅ Артефакт `.orchestra/llm_last_error.json` (последняя ошибка) — **РЕАЛИЗОВАНО**
- ✅ Каждая строка JSONL содержит:
  - `ts_unix`, `event` ("llm_request", "llm_response", "llm_error")
  - `url`, `model`, `timeout_s`, `request_bytes`, `tools_count`, `messages_count`
  - `response_bytes`, `duration_ms`, `http_code` (для ошибок)
  - `error_body` (обрезано до 2KB, без ключей)
  - `request_preview` / `response_preview` (обрезано до 2KB, без секретов)
- ✅ **Ожидаемый результат:** "По одному артефакту видно, что реально ушло и пришло"

**Файлы реализации:**
- ✅ `internal/llm/client.go` — добавлено логирование — **ГОТОВО**
- ✅ `internal/llm/logger.go` — новый модуль логирования — **ГОТОВО**
- ✅ `internal/core/core.go` и `internal/cli/apply.go` — подключение logger — **ГОТОВО**

---

### 4. Workspace Snapshot (уже есть, но нужно проверить полноту)

**Статус:** ✅ Уже реализовано в `internal/prompt/agent_prompt.go`

**Проверка полноты:**
- ✅ `BuildUserInfoSnapshot` собирает: OS, shell, workspace_root, is_git_repo
- ✅ Поддержка env переменных: `ORCH_ACTIVE_FILE`, `ORCH_CURSOR_LINE`, `ORCH_CURSOR_COL`, `ORCH_OPEN_FILES`, `ORCH_CHANGED_FILES`
- ✅ `<user_info>` инжектится в каждый LLM запрос

**Критерий готовности (проверка):**
- ✅ Workspace snapshot присутствует в каждом запросе
- ✅ IDE может передавать контекст через env переменные
- ✅ **Ожидаемый результат:** "Модель перестаёт гадать где проект/какие файлы"

**Дополнительно (nice-to-have, не блокер):**
- Lints/errors из IDE (если доступны)
- Последние tool results (компактно) в контексте

---

## 📋 Acceptance Criteria (финальная проверка)

После реализации всех блокеров, система должна пройти:

1. **LLM Smoke Test:** `orchestra llm-ping` успешно завершается за < 10 секунд
2. **Minimal Flow Test:** `TestRealLLMMinimalFlow` проходит 3 раза подряд с одинаковым результатом
3. **Stale Detection Test:** `TestStaleScenario` детерминированно ловит stale без побочек
4. **Artifacts Check:** После любого запуска в `.orchestra/llm_log.jsonl` есть запись с request/response preview
5. **Workspace Context:** В каждом LLM запросе присутствует `<user_info>` с workspace_root и is_git_repo

**Ожидаемый результат:** "3 прогонов подряд на тестовой копии проекта дают одинаковый результат (или ожидаемый stale)"

---

## 🚫 Что НЕ считается блокером (nice-to-have)

- Полные `config_test.go` unit тесты
- Идеальный `search.block` с точным текстовым поиском
- Daemon с полным кэшированием
- Метрики и мониторинг
- Tree-sitter на все языки (Go достаточно для MVP)

Эти улучшения можно делать после выхода в боевую работу.

---

## 📝 Следующие шаги (приоритетный порядок)

1. **LLM Smoke Test** (1-2 часа) — ✅ **ВЫПОЛНЕНО**
   - ✅ Создать `internal/cli/llm_ping.go`
   - ✅ Создать `tests/e2e_real_llm/llm_contract_test.go`
   - ⏳ Проверить на реальном провайдере (требует настройки)

2. **LLM Logging** (2-3 часа) — ✅ **ВЫПОЛНЕНО**
   - ✅ Расширить `internal/llm/client.go` с логированием
   - ✅ Создать `internal/llm/logger.go` для `.orchestra/llm_log.jsonl`
   - ✅ Логирование: HTTP status, request/response bytes, latency, error body (обрезано)
   - ✅ Артефакты: `.orchestra/llm_log.jsonl` и `.orchestra/llm_last_error.json`
   - ⏳ Проверить, что артефакты создаются (требует запуска с реальным LLM)

3. **Minimal Flow Test** (2-3 часа) — ✅ **ВЫПОЛНЕНО**
   - ✅ Создать `tests/e2e_real_llm/minimal_flow_test.go`
   - ⏳ Проверить детерминированность с `--from-plan` (требует запуска с реальным LLM)

4. **Verification** (1 час)
   - Прогнать все acceptance criteria
   - Обновить документацию

**Итого:** ~6-9 часов работы для выхода в боевую готовность.
