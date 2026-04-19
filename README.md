# Orchestra

Сервис-оркестратор для работы LLM с кодовой базой проекта.

## Статус

✅ **vNext** — core JSON-RPC протокол + безопасный apply

[Changelog](docs/CHANGELOG.md) | [Protocol](docs/PROTOCOL.md) | [Проектные заметки](docs/Task_project.md)

## Описание

Orchestra — консольный инструмент, который умеет читать проект, формировать контекст для LLM и применять предлагаемые моделью изменения в файлах (с возможностью dry-run).

## Быстрый старт

```bash
# 1) Инициализация проекта (в корне репозитория)
orchestra init

# 2) Запуск core (JSON-RPC over stdio) — нужно для интеграций / --via-core
orchestra core --workspace-root .

# (debug) HTTP JSON-RPC для отладки (только localhost; токен обязателен)
# ⚠️ НЕ ОСНОВНОЙ РЕЖИМ: HTTP = debug-only, loopback + token, может не поддерживать все фичи
orchestra core --workspace-root . --http
# Токен автоматически генерируется (или задайте через --http-token)
# Discovery файл: .orchestra/core.http.json (содержит URL + токен, права 0600, в .gitignore)
# Пример подключения: curl -H "Authorization: Bearer <token>" http://127.0.0.1:<port>/rpc

# 3) Поиск по коду
orchestra search "function main"

# 4) Просмотр плана изменений (без генерации кода)
orchestra apply --plan-only "добавь логирование в main.go"

# 5) Dry-run apply (по умолчанию)
orchestra apply "добавь логирование в main.go"

# 6) Реальное применение изменений (с backup по умолчанию)
orchestra apply --apply "добавь логирование в main.go"

# 7) Разрешить exec.run (опасно; включайте осознанно)
orchestra apply --apply --allow-exec "запусти go test и исправь ошибки"

# 8) Изолированно через subprocess core (stdio)
orchestra apply --via-core "добавь функцию Sum"

# 9) Проверка подключения к LLM провайдеру (smoke test)
orchestra llm-ping
```

## Daemon-режим (v0.3)

Daemon — локальный процесс, который обслуживает **один** `project_root`, хранит метаданные файлов в памяти и предоставляет HTTP API для ускорения `apply/search`.

- **Discovery**: daemon пишет `.orchestra/daemon.json` (в `.gitignore`), CLI читает его и подключается автоматически.
- **Cache**: daemon хранит снапшот метаданных в `.orchestra/cache.json` (в `.gitignore`).
- **Fallback**: если daemon недоступен/не тот проект/несовместимый протокол — CLI автоматически падает обратно в прямой режим (v0.2).

Переменная окружения (опционально):

- `ORCHESTRA_DAEMON_URL` — принудительно указать URL daemon (например `http://127.0.0.1:8080`).

## Требования

- Go 1.22+
- LLM API (OpenAI-совместимый, локальный vLLM или внешний провайдер)

## Установка

```bash
go build -o orchestra ./cmd/orchestra
```

## Тесты

```bash
go test ./...
go test -race ./... # на Linux/macOS или Windows с cgo/gcc
go test -race -count=50 ./internal/jsonrpc ./internal/core
```

## Документация

- [Контракт протокола core](docs/PROTOCOL.md)
- [Changelog](docs/CHANGELOG.md)
- [Проектные заметки](docs/Task_project.md)
- [Критерии готовности](docs/READINESS_CRITERIA.md)
- [Инструкции по проверке](docs/VERIFICATION.md)
- [Быстрая проверка](docs/QUICK_CHECK.md)

## Лицензия

MIT

