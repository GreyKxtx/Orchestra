# Быстрая проверка готовности

**Цель:** Убедиться, что система работает с реальным LLM провайдером.

---

## Подготовка

1. Убедитесь, что `.orchestra.yml` настроен с рабочим провайдером
2. Собран бинарник: `go build -o orchestra.exe ./cmd/orchestra`

---

## Шаг 1: LLM Ping (3-5 раз подряд)

```powershell
orchestra llm-ping
orchestra llm-ping
orchestra llm-ping
```

**Проверка:**
- ✅ Команда не падает
- ✅ Появляется/дополняется `.orchestra/llm_log.jsonl`
- ✅ При ошибках появляется `.orchestra/llm_last_error.json` с HTTP кодом и текстом ошибки

**Если `"messages field is required"`:**
- Открой `.orchestra/llm_last_error.json` — там будет видно URL, HTTP status, и preview тела ошибки
- Проверьте `api_base` в `.orchestra.yml` (должен быть `/v1` или без него)
- Проверьте `llm_log.jsonl` — там будет видно, что реально ушло

---

## Шаг 2: Minimal Flow Test

**Один раз:**
```powershell
$env:ORCH_E2E_LLM="1"
go test ./tests/e2e_real_llm -v -run TestRealLLMMinimalFlow -count=1
```

**Три раза подряд (для стабильности):**
```powershell
go test ./tests/e2e_real_llm -v -run TestRealLLMMinimalFlow -count=3
```

**Ожидаемое:**
- ✅ `--plan-only` создаёт артефакты и **не трогает файлы**
- ✅ `--from-plan --apply` применяет ровно то, что в `diff.txt`
- ✅ Повтор `--from-plan --apply` даёт **StaleContent** или "ничего не меняется", но **без побочек** (новых backup/изменений)

**Проверка артефактов:**
```powershell
# После --plan-only
Test-Path .orchestra/plan.json  # True
Test-Path .orchestra/diff.txt   # True
git diff  # Пустой (файлы не изменены)

# После --from-plan --apply
git diff main.go  # Показывает изменения
Test-Path main.go.orchestra.bak  # True (backup создан)
cat .orchestra/diff.txt  # Должен совпадать с git diff
```

---

## Шаг 3: Проверка логов

После любого запуска `apply`:

```powershell
# Лог всех запросов
cat .orchestra/llm_log.jsonl

# Последняя ошибка (если была)
cat .orchestra/llm_last_error.json
```

**Ожидаемое содержимое `llm_log.jsonl`:**
- Строки с `"event": "llm_request"` (URL, model, request_bytes)
- Строки с `"event": "llm_response"` (response_bytes, duration_ms)
- При ошибках: строки с `"event": "llm_error"` (http_code, error_body)

**Если провайдер "умер":**
- Открой `.orchestra/llm_last_error.json` — там будет код, текст ошибки, размер запроса и latency
- Это ваш "почему" в одном файле

---

## Шаг 4: Готовность к реальному проекту

**Можно использовать на живом репе, если:**
- ✅ `llm-ping` проходит 3-5 раз подряд
- ✅ `TestRealLLMMinimalFlow` проходит 3 раза подряд
- ✅ После любого `apply` есть `.orchestra/llm_log.jsonl`

**Правильная лестница на живой репе:**

1. **Сначала план (безопасно):**
   ```powershell
   orchestra apply "добавь комментарий в main.go" --plan-only
   ```

2. **Смотрим diff:**
   ```powershell
   cat .orchestra/diff.txt
   # Проверяем список changed files в выводе
   ```

3. **Только если diff адекватный, применяем:**
   ```powershell
   orchestra apply --from-plan .orchestra/plan.json --apply
   ```

**Если что-то пошло не так:**
- Открой `.orchestra/llm_last_error.json` — там будет причина
- Проверьте `.orchestra/llm_log.jsonl` — там будет видно размер запроса и latency

---

## Типичные проблемы

### `"messages field is required"`

**Это НЕ проблема модели.** Это значит, что на endpoint ушёл не тот JSON.

**Диагностика:**
1. Открой `.orchestra/llm_last_error.json` — там будет HTTP код и тело ошибки
2. Открой `.orchestra/llm_log.jsonl` — там будет видно `request_preview` (что реально ушло)
3. Проверьте `api_base` в `.orchestra.yml` (должен быть `/v1` или без него)

**Возможные причины:**
- Неправильный endpoint (`/completions` вместо `/chat/completions`)
- Прокси/обвязка меняет body
- `Content-Type` не `application/json`

### Таймауты

**Диагностика:**
- Проверьте `duration_ms` в `.orchestra/llm_log.jsonl`
- Если `duration_ms` близок к `timeout_s * 1000`, увеличьте `timeout_s` в `.orchestra.yml`

**Для больших моделей (30B+):**
- Увеличьте `timeout_s` до 120-300

---

## Итоговый чеклист

- [ ] `orchestra llm-ping` проходит 3-5 раз подряд
- [ ] `TestRealLLMMinimalFlow` проходит 3 раза подряд
- [ ] После любого `apply` есть `.orchestra/llm_log.jsonl` с записями
- [ ] Можете безопасно применять изменения через `--plan-only` → `--from-plan --apply`

**Если все пункты выполнены — система готова к работе с реальными проектами.**
