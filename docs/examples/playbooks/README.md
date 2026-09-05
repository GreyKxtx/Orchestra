# Orchestra — Dept Playbooks (L0 defaults)

**Слой L0** в [orchestra-routing.md § G](../../architecture/orchestra-routing.md#g-dept-playbooks--implementation-briefs).

| Файл | Отдел | Назначение |
|------|-------|------------|
| `security_methodology.md` | Security | Алгоритм audit / pentest (OWASP-based) |
| `security_L1_playbook.template.md` | Security | L1 override: `asvs_level` по `project_profile`, ASVS ↔ bucket маппинг |
| `frontend_implementation_brief.template.md` | Frontend | Brief-анкета до ТЗ и workers |
| `backend_implementation_brief.template.md` | Backend | Brief-анкета до OpenAPI и workers |
| `project_docs_MANIFEST.template.md` | Documentation | Карта project docs → `.orchestra/docs/MANIFEST.md` |
| `adr.template.md` | Documentation | ADR из `decisions.md` на этапе 6b → `docs/architecture/adr/` |
| `go_engineering.md` | Engineering (Go) | Error handling, тесты, `go vet`/`-race`, типичные анти-паттерны |
| `typescript_engineering.md` | Engineering (TS/JS) | Типизация, error handling, тесты, типичные анти-паттерны |
| `python_engineering.md` | Engineering (Python) | Типизация, error handling, тесты, типичные анти-паттерны |

**L1 (project):** Docs Lead копирует и адаптирует в `.orchestra/playbooks/{dept}.md` и **создаёт каркас `docs/`** после approve PRD.

**L2 (epic):** Dept Lead заполняет brief/TZ в `.orchestra/specs/{dept}/`.

**Доступность в целевом проекте:** этот каталог существует только в репозитории Orchestra — при работе над чужим проектом его не видно через `fs.read`. `orchestra init` встраивает все файлы отсюда (через `docs/examples/embed.go`) в `.orchestra/playbooks/l0/` целевого проекта, откуда их и читает Docs Lead. Редактировать `.orchestra/playbooks/l0/` бессмысленно — `orchestra init` перезаписывает его версией, встроенной в бинарник.
