# Руководство по настройке тестов с реальной LLM

## Быстрый старт

### 1. Запустить LLM API сервер

Выберите один из вариантов:

#### Вариант A: LM Studio (рекомендуется для Windows)
1. Скачать и установить [LM Studio](https://lmstudio.ai/)
2. Запустить сервер локально на порту 8000
3. Выбрать модель (например, Llama 3, Mistral)

#### Вариант B: Ollama
```powershell
# Установить Ollama
# Запустить сервер
ollama serve
# В другом терминале запустить модель
ollama run llama3
```

#### Вариант C: OpenAI API (облачный)
```powershell
$env:ORCH_E2E_LLM_API_BASE="https://api.openai.com/v1"
$env:ORCH_E2E_LLM_API_KEY="sk-your-key-here"
```

### 2. Настроить переменные окружения

```powershell
# Обязательно: включить E2E тесты
$env:ORCH_E2E_LLM="1"

# Опционально: настроить API (если не используете localhost:8000)
$env:ORCH_E2E_LLM_API_BASE="http://localhost:8000/v1"
$env:ORCH_E2E_LLM_API_KEY="lm-studio"  # или ваш ключ
$env:ORCH_E2E_LLM_MODEL="gpt-3.5-turbo"  # или ваша модель
```

### 3. Запустить тесты

```powershell
# Все тесты
go test ./tests/e2e_real_llm -v -timeout 30m

# Конкретный тест
go test ./tests/e2e_real_llm -v -run TestSimpleEdit

# Быстрый smoke-тест
go test ./tests/e2e_real_llm -v -run TestSmokeCLI
```

## Проверка подключения

Перед запуском тестов проверьте подключение:

```powershell
.\orchestra.exe llm-ping
```

Должен вернуть успешный ответ.

## Доступные тесты

1. **TestSimpleEdit** - базовое редактирование (переименование функции)
2. **TestSmokeCLI** - быстрый smoke-тест
3. **TestRealLLMMinimalFlow** - полный цикл (plan → apply → repeat)
4. **TestStaleScenario** - проверка обнаружения устаревшего контента
5. **TestWorkspaceEscapeAttempt** - защита от path traversal (✅ работает без LLM)
6. **TestExecBlocked** - блокировка exec (✅ работает без LLM)
7. **TestLLMContractSmoke** - проверка контракта LLM API
8. **TestSmokeCLI_Strict** - строгий smoke-тест

## Ожидаемое время выполнения

- С локальным LLM (LM Studio, Ollama): 30-60 секунд на тест
- С облачным API (OpenAI): 10-30 секунд на тест
- Таймаут на команду: 5 минут

## Устранение проблем

### Ошибка: "connection refused"
- Убедитесь, что LLM сервер запущен
- Проверьте порт (по умолчанию 8000)
- Проверьте URL в `ORCH_E2E_LLM_API_BASE`

### Ошибка: "authentication failed"
- Проверьте `ORCH_E2E_LLM_API_KEY`
- Для LM Studio обычно используется `"lm-studio"`

### Тесты слишком медленные
- Используйте более быструю модель
- Уменьшите `max_tokens` в конфиге
- Используйте `temperature=0.0` для детерминированности

