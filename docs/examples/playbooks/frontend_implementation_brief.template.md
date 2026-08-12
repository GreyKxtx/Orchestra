---
dept: frontend
epic_id: EPIC-XXX
status: draft   # draft | complete | waived
required_fields: [routes, state_management, api_contract_ref, design_tokens_ref]
---

# Frontend Implementation Brief — {{EPIC_TITLE}}

> Заполняет **Frontend Lead** на этапе 3. Workers **не** стартуют, пока `status: complete` или User явно waived gaps через Orchestrator.

## 1. Routes / screens *(required)*

| Route | Screen | Auth | Notes |
|-------|--------|:----:|-------|
| `/…` | … | yes/no | … |

## 2. State management *(required)*

- Library: (e.g. TanStack Query + Zustand)
- Server state vs UI state split:
- Cache invalidation rules:

## 3. API contract ref *(required)*

- OpenAPI / mock path: `.orchestra/specs/backend/OpenAPI_Contract.yaml`
- Endpoints used by this epic:
- Error shape expected:

## 4. Design tokens ref *(required)*

- Path: `.orchestra/specs/design/UI_Tokens.json`
- Components from design system:
- Responsive breakpoints:

## 5. a11y / i18n *(optional — default from L1 playbook)*

- WCAG target:
- i18n keys namespace:

## 6. Error / loading UX *(optional)*

- Global vs local error boundaries:
- Skeleton / spinner policy:

## 7. Out of scope

- …

## 8. open_questions

<!-- Lead: собери ВСЕ неясности сюда перед task_result. Orchestrator задаст User одним batch. -->

| id | question | options | answer |
|----|----------|---------|--------|
| q1 | … | … | *(filled by Orchestrator relay)* |

## Completeness

- [ ] All `required_fields` non-empty
- [ ] All `open_questions` have `answer` or marked `waived_by_user`
