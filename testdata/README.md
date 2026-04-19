# testdata

Эта папка содержит фиксированные (или детерминированно генерируемые) проекты для тестов и бенчмарков Orchestra.

## Структура

Разделение на:
- **real_project/** — реальные проекты (локально, **не коммитятся в git**)
- **synthetic_project/** — синтетические проекты (детерминированно генерируемые, могут коммититься)

**Важно**: Содержимое `real_project/**` игнорируется в `.gitignore`. В git коммитятся только `README.md` файлы в `real_project/`.

## small

- **Назначение**: быстрые unit/integration проверки.
- **Размер**: ~несколько файлов.
- **Структура**:
  - `testdata/small/real_project/` — реальный маленький проект (локально, не в git)
  - `testdata/small/synthetic_project/` — синтетический маленький проект (опционально)
  - `testdata/small/test_real_project/` — старый тестовый Go-проект (в git)

**Бенчмарки**: по умолчанию используют `real_project/` если существует, иначе `test_real_project/`.

## medium

- **Назначение**: бенчмарки на проекте среднего размера без зависимости от сети.
- **Структура**:
  - `testdata/medium/real_project/` — реальный проект среднего размера (локально, не в git)
  - `testdata/medium/synthetic_project/` — синтетический снапшот (детерминированная генерация, в git)

**Бенчмарки**: по умолчанию используют `real_project/` если существует, иначе `synthetic_project/`, иначе генерируют в temp.

## large

- **Назначение**: ручные/локальные бенчмарки на большом проекте.
- **Структура**:
  - `testdata/large/real_project/` — реальный большой проект (локально, не в git)
  - `testdata/large/synthetic_project/` — синтетический проект + генератор

**Генератор**: `testdata/large/synthetic_project/generator`.

Пример генерации:

```bash
go run ./testdata/large/synthetic_project/generator --seed 42 --files 2500 --packages 200 --max-depth 6 --size-profile small --clean --out testdata/large/synthetic_project
```

**Бенчмарки**: по умолчанию используют `synthetic_project/` если сгенерирован, иначе генерируют в temp. `real_project/` используется только если существует локально.

**Git**: Сгенерированные директории (`cmd/`, `internal/`, `pkg/`, `api/`, `docs/`, `configs/`, `scripts/`) в `synthetic_project/` намеренно игнорируются в git.
