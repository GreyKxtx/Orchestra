# E2E Tests без LLM

Эти тесты проверяют интеграцию **core (stdio JSON-RPC) + tool.call(fs.apply_ops) + applier** без участия LLM. Это позволяет тестировать реальные файлы и реальные изменения без зависимости от "настроения модели".

## Что покрывается

- **stdio JSON-RPC фрейминг** (Content-Length, чтение ответов)
- **initialize handshake** (через core.health → initialize)
- **fs.apply_ops**: dry-run vs apply
- **backup-политика**
- **StaleContent без побочных эффектов**
- **path traversal denial**

## Запуск

```bash
# Все тесты пакета
go test ./tests/e2e_nollm -v

# Конкретный тест
go test ./tests/e2e_nollm -v -run TestApplyOps_Stdio_DryRun

# Все тесты проекта (включая e2e_nollm)
go test ./... -run TestApplyOps_Stdio
```

## Особенности

- Тесты создают временные проекты в `t.TempDir()`, ничего в репе не портят
- Бинарник `orchestra` собирается один раз и кэшируется между тестами
- Используется реальный stdio JSON-RPC протокол (как в production)
- Все изменения применяются к реальным файлам

## Требования

- Собранный бинарник `orchestra` (собирается автоматически при первом запуске)
- Go 1.21+
