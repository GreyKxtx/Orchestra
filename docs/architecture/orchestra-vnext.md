# Orchestra vNext — автономия Lead–Worker

**Статус:** roadmap (2026-08)  
**Базовая архитектура:** [planner-worker.md](./planner-worker.md) (MVP ✅)  
**Связано:** [semantic-dry-run-tz.md](./semantic-dry-run-tz.md), [modes.md](../modes.md)

---

## Контекст

Режим `orchestra` уже реализует паттерн **Actor–Delegator**: Lead (Planner) держит диалог и декомпозицию; Worker получает изолированный **WorkOrder JSON** без истории чата. Это снимает **Context Rot** — главную проблему длинных сессий с локальными моделями.

Документ фиксирует **6 архитектурных улучшений** для следующего уровня автономии и надёжности.

---

## Матрица статуса

| # | Улучшение | Статус | Где в коде / docs |
|---|-----------|--------|-------------------|
| 1 | **Verifier / Critic** — post-worker проверка до Lead | ✅ Done (V2) | `internal/tasks/worker_verify.go` — LSP + go build, retry, Lead JSON |
| 2 | **CKG Injection** — подграф в WorkOrder | ✅ Done | schema + `EditScopePaths` runtime guard (`worker_scope.go`) |
| 3 | **Parallel Workers** — независимые `task_spawn` | 🟡 Partial | `task_spawn` + `task_wait` ✅; orchestra prompt обновлён |
| 4 | **Spec Invariants** — жёсткие `constraints[]` | 🟡 Partial | поле ✅; worker prompt + runtime guard — усилено |
| 5 | **Lead Scratchpad** — сжатый working state | ✅ Done | `.orchestra/state.md`, `update_working_state`, compact worker results |
| 6 | **Forced LSP on task_result** — блок успеха при ошибках | ✅ Done | `internal/agent/worker_task_result.go`, `guard/diag_tracker.go` |

**Легенда:** ✅ Done · 🟡 Partial · 🔲 TODO

---

## 1. Агент-Верификатор (Critic)

### Сейчас
Worker → `task_result` → Lead читает дифф и решает.

### Цель
Между Worker и Lead — **автоматическая верификация** без токенов Lead:

```
Worker task_result (staging)
  → go build / go test (subset) / LSP diagnostics
  → OK → Lead видит «success»
  → FAIL → retry Worker (≤2) или debug child
  → exhausted → Lead видит «blocked» + suggestion
```

### Реализация (V2 — deterministic hook ✅)

После успешного `task_result` Worker:

1. **LSP** — `lsp.diagnostics` на каждый изменённый файл (staging-aware)
2. **go build** — `./pkg` для каждого затронутого `.go` (только когда **не** dry-run; иначе skip)
3. При fail — **retry Worker** (default 1 раз) с verification hint
4. Lead получает компактный JSON: `verified_success` или `verification_failed`

Конфиг (`.orchestra.yml`):

```yaml
orchestra:
  worker_verify_enabled: true   # default
  max_worker_verify_retries: 1  # re-run worker after verify fail
  worker_llm_verify_enabled: false  # optional LLM verifier after deterministic pass
```

| Файл | Роль |
|------|------|
| `internal/tasks/worker_verify.go` | checks + retry loop |
| `internal/tasks/tasks.go` | `runWorkerWithVerification` |

### V1 LLM verifier ✅

| Шаг | Статус |
|-----|--------|
| `subagent_type=verifier` — read-only + `lsp.diagnostics` + optional `bash` (allowExec) | ✅ |
| Prompt `internal/prompt/files/verifier.txt` (goal-backward, ## VERIFICATION PASSED/FAILED) | ✅ |
| Lead может spawn через `task` / `task_spawn` | ✅ |
| Optional auto после deterministic pass: `orchestra.worker_llm_verify_enabled: true` | ✅ (default false) |

| Файл | Роль |
|------|------|
| `internal/tools/registry.go` | `listToolsVerifier` |
| `internal/prompt/files/verifier.txt` | system prompt |
| `internal/tasks/worker_verify.go` | `runInlineLLMVerifier`, markers |

### Будущее

| Шаг | Задача |
|-----|--------|
| V3 | Skill `verifier` как альтернатива для workflow pipeline |

---

## 2. CKG Injection в WorkOrder

Lead собирает контекст через `explore` / `symbols` / `repo_map` и передаёт Worker **структурированный подграф**, а не prose.

### Расширенный WorkOrder (schema ✅)

```json
{
  "task_id": "add_jwt_check",
  "tier": "focused",
  "target_file": "internal/api/handler.go",
  "target_files": ["internal/api/handler.go"],
  "target_symbol": "GetUser",
  "intent": "Добавить проверку JWT",
  "readonly_references": ["internal/core/models.go", "pkg/auth/jwt.go"],
  "allowed_symbols": ["auth.ValidateToken", "logger.Errorf"],
  "context": {
    "ast_skeleton": "func (h *Handler) GetUser(...)",
    "lsp_types": "type Claims struct { … }",
    "exact_snippet_to_replace": "…"
  },
  "instructions": ["…"],
  "constraints": ["Не добавлять новые зависимости", "Публичный API без breaking change"],
  "acceptance_criteria": ["JWT invalid → 401", "LSP clean on target_file"]
}
```

| Поле | Назначение |
|------|------------|
| `target_files[]` | явный scope записи (мульти-file tier) |
| `readonly_references[]` | файлы только для `read` / `lsp.*` |
| `allowed_symbols[]` | CKG-инъекция: снижает галлюцинации API |

### TODO
- Lead prompt: обязать заполнение `readonly_references` после `explore` (best-effort ✅ in prompt)
- ~~Optional runtime: warn Worker при `edit` вне `target_files`~~ ✅ `WorkerEditPaths` deny gate

---

## 3. Параллельное исполнение

Независимые подзадачи (разные файлы, нет shared state) — **параллельные Workers**:

```text
task_spawn worker #1 (model DB)
task_spawn worker #2 (DTO API)
task_spawn worker #3 (tests)
task_wait  # все три
```

### Уже есть
- `task_spawn` / `task_wait` / `task_cancel` в orchestra Lead surface
- `errgroup` внутри `TaskRunner` для parallel children

### Правила для Lead (prompt)
- Параллель только при **непересекающихся** `target_file`(s)
- После `task_wait` — сверка конфликтов (git diff / read)
- Sequential: зависимые шаги через sync `task`

---

## 4. Spec-Driven Constraints (Invariants)

Worker не видит архитектуру проекта → Lead **обязан** передать инварианты в `constraints[]`:

- «Не добавлять внешние зависимости»
- «Сохранить обратную совместимость публичного API»
- «Не менять логирование / error wrapping style»

Worker system prompt: **сверяться с constraints перед каждым edit**.

Runtime (future): deny `edit` если path ∉ `target_files` когда поле задано.

---

## 5. Lead Working State (Scratchpad)

### Сейчас (Partial)
`<working_state>` inject каждый ход Lead — rule-based digest (`internal/agent/working/state.go`): файлы, tools, errors, todos.

### Цель
Lead ведёт **`.orchestra/state.md`** (или `update_working_state` tool):

```markdown
## Goal
Refactor auth middleware

## Done
- [x] explore JWT usage
- [x] worker: handler.go GetUser

## Next
- [ ] worker: middleware chain
- [ ] integration test

## Notes
- Uses jwt/v5, not v4
```

Старые ответы Workers **обрезаются** из history; Lead получает только scratchpad + последний digest.

### Реализация ✅

- `update_working_state` tool (orchestra Lead only) → atomic write `.orchestra/state.md`
- `<orchestra_scratchpad>` inject each turn (`working_wire.go`)
- Lead may `write` to `.orchestra/state.md` (path guard extended)
- Worker results compacted in `task` / `task_wait` history; auto-append one-liner to `## Done`
- Retroactive collapse of older worker `task`/`task_wait` tool atoms (`history/prune.go` + `agent_run.go`)

| Файл | Роль |
|------|------|
| `internal/agent/orchestra_scratchpad.go` | read/write, compact, auto-append |
| `internal/plan/orchestra.go` | path constants + write guard |
| `internal/tools/session/scratchpad.go` | tool def |
| `internal/agent/history/prune.go` | `CollapseOrchestraWorkerTaskOutputs` |

---

## 6. Forced LSP Resolution ✅

Worker **не может** вызвать `task_result` со `status=success`, пока на изменённых файлах висят LSP errors.

### Реализация

```
write/edit → staging → LSP diagnostics
  → fingerprint + hint в DiagTracker
Worker → task_result(success)
  → blockWorkerTaskResult() если PathsWithErrors() ≠ ∅
  → tool error + hint обратно Worker (Lead не видит)
Worker → task_result(error) — пропускается (эскалация Lead)
```

| Файл | Роль |
|------|------|
| `internal/agent/guard/diag_tracker.go` | `PathsWithErrors`, `ErrorHint` |
| `internal/agent/worker_task_result.go` | gate logic |
| `internal/agent/tool_dispatch.go` | hook на `task_result` |

---

## Порядок внедрения

| Приоритет | Item | Effort | Impact |
|-----------|------|--------|--------|
| P0 | #6 Forced LSP | S | ✅ High — без битого кода у Lead |
| P1 | #2 CKG fields + Lead prompt | M | High — меньше галлюцинаций |
| P1 | #4 Constraints in worker prompt | S | Medium |
| P2 | #3 Parallel guidance + eval | S | High latency win |
| P2 | #5 Scratchpad file | M | Lead context economy |
| P3 | #1 Verifier subagent / hook | L | Full autonomy |

---

## Критерии приёмки vNext

- [x] Worker `task_result(success)` blocked при LSP errors на staged file
- [x] Post-worker deterministic verify (LSP + go build) before Lead
- [ ] WorkOrder с `readonly_references` + `allowed_symbols` в E2E eval
- [ ] Lead spawn 2+ parallel workers, `task_wait`, merge без conflict
- [ ] Verifier (build/test) между Worker и Lead без участия Lead LLM
- [ ] `.orchestra/state.md` persists across Lead turns; worker blobs pruned

---

## Ссылки

| Компонент | Путь |
|-----------|------|
| WorkOrder schema | `internal/tasks/workorder_validate.go` |
| Lead prompt | `internal/prompt/files/orchestra.txt` |
| Worker prompt | `internal/prompt/files/worker.txt` |
| LSP gate | `internal/agent/worker_task_result.go` |
| Working state | `internal/agent/working/state.go` |
| Subagent runner | `internal/tasks/tasks.go` |
