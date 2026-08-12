---
dept: backend
epic_id: EPIC-XXX
status: draft
required_fields: [domain_model, api_style, auth_model, migrations, openapi_output]
---

# Backend Implementation Brief — {{EPIC_TITLE}}

> Заполняет **Backend Lead** на этапе 3. OpenAPI и WorkOrders — только после `status: complete`.

## 1. Domain model *(required)*

| Entity | Key fields | Relations |
|--------|------------|-----------|
| … | … | … |

## 2. API style *(required)*

- Style: REST / GraphQL / gRPC
- Versioning:
- Pagination / filtering conventions:

## 3. AuthN / AuthZ *(required)*

- Mechanism: (JWT / session / API key)
- Roles / permissions matrix:
- Public vs protected routes:

## 4. Migrations / schema *(required)*

- DB: (postgres / sqlite / …)
- Migration tool:
- Backward compatibility rules:

## 5. Observability *(optional)*

- Structured log fields:
- Metrics / traces:

## 6. OpenAPI output *(required)*

- Target path: `.orchestra/specs/backend/OpenAPI_Contract.yaml`
- Must cover endpoints for epic:

## 7. Non-functional

- Rate limits:
- Idempotency keys:
- Transaction boundaries:

## 8. open_questions

| id | question | options | answer |
|----|----------|---------|--------|
| q1 | … | … | |

## Completeness

- [ ] Domain + API + auth + migrations documented
- [ ] open_questions resolved or waived
