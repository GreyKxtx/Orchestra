# Project Documentation MANIFEST

> Копируется Docs Lead в `.orchestra/docs/MANIFEST.md` на этапе 1–2.
> Одна строка = один doc-файл проекта. Orchestrator проверяет `doc_debt` по этой таблице.
>
> **Trigger globs:** любой шаблон в бэктиках внутри колонки *Update trigger*
> рантайм трактует как path-glob (`*` — внутри сегмента, `**` — сквозь сегменты).
> Успешно верифицированная правка воркера, совпавшая с глобом, кладёт doc-файл
> строки в `doc_debt` (`.orchestra/state.md`). Триггеры без глобов — для L4/людей.

| Path | Owner dept | Update trigger | Status |
|------|------------|----------------|--------|
| `docs/README.md` | Documentation | PRD approve, new top-level section | draft |
| `docs/architecture/overview.md` | Documentation | PRD approve, major epic | draft |
| `docs/architecture/adr/ADR-0001-template.md` | *(per ADR author)* | Architectural decision | draft |
| `docs/development/setup.md` | Documentation | Stack / CI / docker change (`Dockerfile`, `docker-compose*`, `.github/workflows/**`) | draft |
| `docs/development/contributing.md` | Documentation | PR workflow change | draft |
| `docs/api/README.md` | Backend | OpenAPI change (`.orchestra/contract/OpenAPI*`, `api/**`) | draft |
| `docs/operations/runbooks/deploy.md` | Platform | Deploy path change (6b) (`deploy/**`, `helm/**`) | draft |
| `README.md` | Documentation + Platform | Every release (6b sync) | draft |
| `CHANGELOG.md` | Platform | Every release | draft |

## Rules

1. **Public surface change** (CLI, HTTP API, config schema) → touch row above or set `doc_debt` in `.orchestra/state.md`.
2. **`.orchestra/`** — Orchestra internal; optional excerpt may link from `docs/README.md`.
3. Brownfield: add rows for existing docs; do not delete without User approve.
4. Правка самого doc-файла в том же батче снимает триггер (долг не пишется).
