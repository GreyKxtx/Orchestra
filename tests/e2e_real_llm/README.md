# E2E Tests with Real LLM

Эти тесты проверяют интеграцию Orchestra с реальным LLM API. Они запускаются только при явном указании через переменную окружения.

## Требования

- Собранный бинарник `orchestra` (или `orchestra.exe` на Windows) в корне проекта или в PATH
- Доступный LLM API (OpenAI-совместимый)
- Переменные окружения для настройки LLM (опционально)

## Запуск

### Базовый запуск

```bash
# Установить флаг для включения E2E тестов
export ORCH_E2E_LLM=1

# Опционально: настроить LLM API
export ORCH_E2E_LLM_API_BASE=http://localhost:8000/v1
export ORCH_E2E_LLM_API_KEY=lm-studio
export ORCH_E2E_LLM_MODEL=gpt-3.5-turbo

# Запустить все E2E тесты
go test ./tests/e2e_real_llm -v

# Запустить конкретный тест
go test ./tests/e2e_real_llm -v -run TestSimpleEdit
```

### Windows (PowerShell)

```powershell
$env:ORCH_E2E_LLM="1"
$env:ORCH_E2E_LLM_API_BASE="http://localhost:8000/v1"
$env:ORCH_E2E_LLM_MODEL="gpt-3.5-turbo"

go test ./tests/e2e_real_llm -v
```

## Тесты

### 1. TestSimpleEdit
Проверяет базовое редактирование: переименование функции.
- ✅ Генерируются ops
- ✅ Есть diff в dry-run режиме
- ✅ Файл не изменяется в dry-run

### 2. TestStaleScenario
Проверяет обнаружение stale content (файл изменён после планирования).

**⚠ Ограничение**: Тест имеет ограничение - если LLM перегенерирует план на изменённом файле, stale detection может не сработать. Для правильного теста нужен флаг `--from-plan` для применения сохранённого плана без участия LLM.

- ✅ Возвращается ошибка `StaleContent` (если план не был перегенерирован)
- ✅ Нет backup при ошибке
- ✅ Файл не записывается при stale

### 3. TestWorkspaceEscapeAttempt
Проверяет защиту от path traversal.
- ✅ Блокируются попытки чтения файлов вне workspace
- ✅ Возвращается ошибка `PathTraversal` или отказ

### 4. TestExecBlocked
Проверяет блокировку exec.run без `--allow-exec`.
- ✅ Возвращается ошибка `ExecDenied`
- ✅ Команда не выполняется

### 5. TestSmokeCLI
Быстрый smoke-тест интеграции CLI.
- ✅ Команда завершается успешно
- ✅ Есть вывод (plan/diff)

## Настройка

По умолчанию тесты используют:
- API Base: `http://localhost:8000/v1`
- API Key: `lm-studio`
- Model: `gpt-3.5-turbo`
- Temperature: `0.0` (детерминированность)

Все параметры можно переопределить через переменные окружения (см. `e2e_test.go`).

## Время выполнения

Тесты используют таймаут 5 минут на команду. При использовании локального LLM (lm-studio, ollama) тесты обычно выполняются за 30-60 секунд.

## Примечания

- Эти тесты **не запускаются** в обычном CI (требуют `ORCH_E2E_LLM=1`)
- Тесты создают временные проекты через `t.TempDir()`
- Все тесты используют `--via-core` для изоляции через subprocess
