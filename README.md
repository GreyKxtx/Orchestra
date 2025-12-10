# Orchestra

Сервис-оркестратор для работы LLM с кодовой базой проекта.

## Статус

✅ **v0.2.0 Released** — Search, Planning, Git Safety

[Changelog](CHANGELOG.md) | [v0.2 Checklist](V0.2_CHECKLIST.md)

## Описание

Orchestra — консольный инструмент, который умеет читать проект, формировать контекст для LLM и применять предлагаемые моделью изменения в файлах (с возможностью dry-run).

## Быстрый старт

```bash
# Инициализация проекта
orchestra init

# Поиск по коду
orchestra search "function main"

# Просмотр плана изменений
orchestra apply --plan-only "добавь логирование в main.go"

# Применение изменений (dry-run)
orchestra apply "добавь логирование в main.go"

# Применение изменений с записью и git-коммитом
orchestra apply --apply --git-commit "добавь функцию Sum"
```

## Требования

- Go 1.22+
- LLM API (OpenAI-совместимый, локальный vLLM или внешний провайдер)

## Установка

```bash
go build -o orchestra ./cmd/orchestra
```

## Документация

- [ТЗ и план MVP](task.md)
- [Стратегия тестирования](TESTING.md)

## Лицензия

MIT

