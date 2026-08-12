# Security L1 playbook template

**Target path:** `.orchestra/playbooks/security.md` — пишет Docs Lead на этапе 1, сужает Security Lead (L2) на этапе 3.
**L0 base:** [security_methodology.md](security_methodology.md) — алгоритм аудита; этот файл только параметризует его под проект.

Правило слоёв: L1 может только **сужать** L0 (добавлять запреты, повышать уровень). Ослабление
(waive bucket, понижение ASVS) — только через `accepted_risks` с approve Пользователя в `decisions.md`.

---

```markdown
---
# --- машиночитаемая часть (читает runtime и Security Lead) ---
asvs_level: 1            # 1 | 2 | 3 — см. таблицу выбора ниже
forbidden_patterns: []   # проектные запреты, например: ["fmt.Sprintf в SQL", "eval(", "http:// в prod-конфигах"]
pentest_allowed_hosts: []  # пусто = DAST/pentest (Step 4) запрещён целиком
accepted_risks: []       # только ID решений из decisions.md, одобренных Пользователем
waive_buckets: []        # injection/authn нельзя waive без записи в accepted_risks
---

# Security — project overrides

## ASVS level rationale

<1–2 предложения: почему выбран этот уровень; ссылка на project_profile из PRD.>

## Project-specific invariants

<Список инвариантов, которых нет в L0: доверенные зоны, секреты, специфичные API.>
```

---

## Выбор `asvs_level` (по `project_profile` из PRD)

| project_profile | asvs_level | Дополнительно |
|---|---|---|
| hobby / prototype | 1 | Step 4 (pentest) обычно пропускается |
| startup / business | 2 | Step 4 по staging-хостам из `pentest_allowed_hosts` |
| enterprise / regulated | 3 | Human gate G5 перед релизом; Security L1 не подлежит waive (spec §L) |

## Маппинг ASVS ↔ buckets методологии (Step 3)

Уровень определяет глубину проверки каждого bucket, а не их состав — состав фиксирован в L0.

| Bucket (L0 Step 3) | ASVS v4 chapters | L1 | L2 | L3 |
|---|---|---|---|---|
| Injection | V5 Validation, V8 Data | искомые паттерны | + все параметры OpenAPI | + fuzz-пробы (Step 4) |
| AuthN/AuthZ | V2 AuthN, V4 Access Control | наличие middleware | + матрица ролей по эндпоинтам | + тесты обхода (IDOR) |
| Crypto | V6 Cryptography | hardcoded secrets, TLS skip | + алгоритмы/ключи по списку | + ротация и хранение ключей |
| Data exposure | V7 Errors & Logging, V9 Comms | утечки в логах/ошибках | + PII-карта из Domain_Model | + retention-политики |
| Input validation | V5 Validation | bounds, upload, SSRF | + схемы JSON Schema на входах | + канонизация путей/URL |
| Dependencies | V14 Config | CVE-скан прямых зависимостей | + транзитивные | + SBOM + лицензии |

## Выходные артефакты (без изменений против L0)

- `Threat_Model.md`, `Security_Findings.md` — в `.orchestra/specs/security/`
- Каждый finding → ID; фиксы идут отдельными WorkOrder (Step 5), Security сам код не патчит.
