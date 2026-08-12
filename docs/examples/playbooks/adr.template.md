# ADR template (L0 default)

**Audience:** Docs Lead L4, stage 6b (`task_type: docs_content_update`).
**Target path:** `docs/architecture/adr/NNNN-short-slug.md` (NNNN — next free number, zero-padded).

Каждая запись `.orchestra/decisions.md` с архитектурным следствием (waiver, contract change, accepted_risk,
выбор технологии, отклонённая альтернатива) на этапе 6b конвертируется в один ADR по этой форме.
Чисто операционные ответы (например «да, продолжай») ADR не требуют.

---

```markdown
# NNNN. <Заголовок решения — глагол + объект>

- **Status:** accepted | superseded by NNNN | deprecated
- **Date:** YYYY-MM-DD
- **Source:** .orchestra/decisions.md <id/дата записи> · epic: <epic id>
- **Deciders:** User | Orchestrator L5 | <Dept> Lead

## Context

2–5 предложений: какая проблема или вилка возникла, какие ограничения действовали
(contract epoch, NFR, project_profile). Только факты из decisions.md и репозитория.

## Decision

Одно решение, сформулированное в настоящем времени («Используем X», «Запрещаем Y»).
Если решение — waiver или accepted_risk: указать, какой гейт/правило обходится и на какой срок.

## Consequences

- Положительные: что упростилось.
- Отрицательные / принятые риски: что стало дороже или опаснее.
- Follow-up: doc_debt, задачи, повторный аудит (если есть).
```

---

## Правила

1. Один ADR = одно решение. Пакет связанных решений → несколько ADR со взаимными ссылками.
2. ADR append-only: изменение решения — новый ADR со `Status: accepted` + пометка `superseded by` в старом.
3. Заголовок и Decision должны быть проверяемыми: без «улучшить», «оптимизировать» без критерия.
4. Ссылки на контрактные артефакты — по пути + epoch-хешу (`.orchestra/contracts/EPOCH.yaml`).
5. Индекс: `docs/architecture/adr/README.md` — таблица `NNNN | title | status | date`; Docs Lead обновляет её в том же проходе 6b.
