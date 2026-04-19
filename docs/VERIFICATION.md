# Инструкции по проверке готовности Orchestra

Этот документ содержит **конкретные команды** для проверки, что система готова к работе с реальными проектами.

---

## Предварительные требования

1. **Настроен LLM провайдер** в `.orchestra.yml`:
   ```yaml
   llm:
     api_base: "http://localhost:8000/v1"  # или ваш провайдер
     api_key: "your-key"
     model: "your-model"
     timeout_s: 300  # для больших моделей (30B+) может потребоваться 120-300с
   ```

2. **Собран бинарник**:
   ```powershell
   go build -o orchestra ./cmd/orchestra
   ```

---

## Шаг 1: LLM Smoke Test (обязательно)

Проверяет, что провайдер доступен и отвечает корректно.

**Запустите 3-5 раз подряд для проверки стабильности:**

```powershell
orchestra llm-ping
orchestra llm-ping
orchestra llm-ping
```

**Ожидаемый результат:**
```
✅ LLM ping successful
URL: http://localhost:8000/v1
Model: your-model
Duration: 1234 ms
Request size: 123 bytes
Response size: 456 bytes
```

**Проверка артефактов:**
```powershell
# Результат ping
cat .orchestra/llm_ping_result.json
# Должен содержать "success": true и "http_code": 200

# Лог запросов (должен дополняться)
cat .orchestra/llm_log.jsonl

# Последняя ошибка (если была)
cat .orchestra/llm_last_error.json
```

**Если `"messages field is required`:**
- Это **НЕ проблема модели** — это значит, что на endpoint ушёл не тот JSON
- Откройте `.orchestra/llm_last_error.json` — там будет HTTP код и preview тела ошибки
- Проверьте `llm_log.jsonl` — там будет видно `request_preview` (что реально ушло)

**Критерий готовности:** Команда успешно завершается **3-5 раз подряд** без ошибок.

---

## Шаг 2: Minimal Flow Test (главный тест)

Проверяет полный pipeline: LLM → tool_call → final.patches → apply.

```powershell
$env:ORCH_E2E_LLM="1"
go test ./tests/e2e_real_llm -v -run TestRealLLMMinimalFlow -count=1
```

**Ожидаемый результат:**
```
=== RUN   TestRealLLMMinimalFlow
    minimal_flow_test.go:XX: Step 1: Running --plan-only
    minimal_flow_test.go:XX: ✓ Step 1 passed: plan-only created artifacts, file unchanged
    minimal_flow_test.go:XX: Step 2: Running --from-plan --apply
    minimal_flow_test.go:XX: ✓ Step 2 passed: apply succeeded, file modified, backup created
    minimal_flow_test.go:XX: Step 3: Running --from-plan --apply again
    minimal_flow_test.go:XX: ✓ Step 3 passed: StaleContent detected correctly, no side effects
--- PASS: TestRealLLMMinimalFlow (XX.XXs)
PASS
```

**Проверка артефактов после Step 1 (`--plan-only`):**

```powershell
# План должен существовать
Test-Path .orchestra/plan.json  # True

# Diff должен существовать
Test-Path .orchestra/diff.txt  # True

# Файлы проекта НЕ должны измениться
git diff  # Пустой (если git repo)
```

**Проверка артефактов после Step 2 (`--from-plan --apply`):**

```powershell
# Файл должен быть изменён
git diff main.go  # Показывает изменения

# Backup должен существовать
Test-Path main.go.orchestra.bak  # True

# Изменения должны соответствовать diff.txt
cat .orchestra/diff.txt  # Должен совпадать с git diff
```

**Проверка после Step 3 (повторный `--from-plan --apply`):**

```powershell
# Должен быть StaleContent или idempotent
# НЕ должно быть новых backup
# Файл НЕ должен измениться повторно
git diff --name-only  # Не должен меняться между Step 2 и Step 3
```

**Критерий готовности:** Тест проходит **3 раза подряд** без ошибок:
```powershell
go test ./tests/e2e_real_llm -v -run TestRealLLMMinimalFlow -count=3
```

---

## Шаг 3: Проверка LLM Logging

После любого запуска с LLM должны появиться логи:

```powershell
# Лог всех запросов
cat .orchestra/llm_log.jsonl

# Последняя ошибка (если была)
cat .orchestra/llm_last_error.json
```

**Ожидаемое содержимое `llm_log.jsonl`:**

Каждая строка — JSON объект с полями:
- `ts_unix` — timestamp
- `event` — "llm_request", "llm_response", или "llm_error"
- `url`, `model`, `timeout_s`
- `request_bytes`, `response_bytes`
- `duration_ms`
- `http_code` (для ошибок)
- `error_body` (обрезано до 2KB, для ошибок)
- `request_preview` / `response_preview` (обрезано до 2KB, без секретов)

**Критерий готовности:** После любого запуска `apply` в `.orchestra/llm_log.jsonl` есть записи.

---

## Шаг 4: Тест на реальном проекте (опционально, но рекомендуется)

**ВАЖНО:** Используйте только безопасную лестницу:

1. **Сначала `--plan-only`** (смотрим diff):
   ```powershell
   orchestra apply --plan-only "добавь комментарий в main.go"
   ```

2. **Проверяем diff:**
   ```powershell
   cat .orchestra/diff.txt
   ```

3. **Только если diff адекватный, применяем:**
   ```powershell
   orchestra apply --from-plan .orchestra/plan.json --apply
   ```

**Критерий готовности:** Можете безопасно применять изменения на реальных проектах через `--plan-only` → `--from-plan --apply`.

---

## Типичные проблемы и решения

### Проблема: `"messages" field is required`

**Причина:** Неправильный endpoint или прокси меняет body.

**Решение:**
1. Проверьте `llm-ping` — он должен показать проблему сразу
2. Проверьте `api_base` в `.orchestra.yml` (должен быть `/v1` или без него)
3. Проверьте логи в `.orchestra/llm_log.jsonl` — там будет видно, что реально ушло

### Проблема: Таймауты

**Причина:** `timeout_s` слишком мал для модели.

**Решение:**
- Для больших моделей (30B+): увеличьте `timeout_s` до 120-300
- Проверьте latency в `llm_log.jsonl` — если `duration_ms` близок к `timeout_s * 1000`, увеличьте таймаут

### Проблема: StaleContent при первом apply

**Причина:** Файл изменился между `--plan-only` и `--from-plan --apply`.

**Решение:** Это нормально, если файл действительно изменился. Проверьте, что файл не изменяется внешне между шагами.

---

## Итоговый чеклист готовности

- [ ] `orchestra llm-ping` проходит 3-5 раз подряд
- [ ] `TestRealLLMMinimalFlow` проходит 3 раза подряд
- [ ] После любого `apply` есть `.orchestra/llm_log.jsonl` с записями
- [ ] Можете безопасно применять изменения через `--plan-only` → `--from-plan --apply`

**Если все пункты выполнены — система готова к работе с реальными проектами.**
