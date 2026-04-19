# testdata/medium

Проект среднего размера для бенчмарков (без сети).

## Примечание

Изначально в плане предполагался снапшот реального OSS-проекта в архиве. В текущей реализации используется **синтетический, но детерминированный** проект, чтобы:

- не зависеть от сети в тестах/CI;
- иметь воспроизводимый размер/структуру.

## Как пересоздать

```bash
go run ./testdata/large/synthetic_project/generator --seed 42 --files 300 --packages 40 --max-depth 4 --size-profile medium --out testdata/medium/synthetic_project --clean
```
