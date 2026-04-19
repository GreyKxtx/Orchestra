# testdata/large/synthetic_project

Большой синтетический проект для ручных/локальных бенчмарков.

## Генерация

Генератор: `testdata/large/synthetic_project/generator`.

```bash
go run ./testdata/large/synthetic_project/generator --seed 42 --files 2500 --packages 200 --max-depth 6 --size-profile small --clean --out testdata/large/synthetic_project
```

- `--seed` обязателен (детерминированность).
- `--out` по умолчанию можно не указывать, если генерируем прямо в эту директорию.

## Git

Сгенерированные директории (`cmd/`, `internal/`, `pkg/`, `api/`, `docs/`, `configs/`, `scripts/`) **не коммитятся** и игнорируются в `.gitignore`.
