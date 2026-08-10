# Field eval и Real LLM E2E

Оба контура **намеренно вне CI** — они требуют живой LLM и/или локальный inference stack. Используются как ongoing KPI и регрессионный smoke перед релизом.

## Field eval (YAML tasks)

**Цель:** измерять качество правок на локальных моделях без ручного прогона `apply`.

```powershell
# из корня репо, с настроенным .orchestra.yml
orchestra eval
orchestra eval tests/eval/tasks --timeout 180
orchestra eval --model qwen2.5-coder-7b
```

- Задачи: `tests/eval/tasks/*.yaml`
- Harness: `tests/eval/` (checks: `file_contains`, `file_exists`, …)
- **Не блокер релиза** — метрики собираются вручную; падение одной задачи не ломает CI.

Рекомендуемый KPI-набор перед bump версии:

1. `ambiguous_match_recovery.yaml` — forgiving resolver
2. `worker_workorder.yaml` — если есть в tasks/
3. Любые новые задачи под вашу локальную модель

## Real LLM E2E (gated)

**Цель:** end-to-end через subprocess `orchestra core --via-core` против реального API (LM Studio / vLLM / OpenAI-compat).

```powershell
$env:ORCH_E2E_LLM = "1"
go test ./tests/e2e_real_llm -v -count=1

# опционально
$env:ORCH_E2E_LLM_API_BASE = "http://127.0.0.1:1234/v1"
$env:ORCH_E2E_LLM_API_KEY = "lm-studio"
$env:ORCH_E2E_LLM_MODEL = "your-model"
```

Ключевые тесты:

| Тест | Что проверяет |
|---|---|
| `TestRealLLMMinimalFlow` | базовый apply через core |
| `TestOrchestraWorker_RealLLM` | Lead → worker tier, WorkOrder |
| `TestWorkerLSPIterations_RealLLM` | LSP validation loop на worker |

`TestMain` в пакете при `ORCH_E2E_LLM=1` пытается preload модель в LM Studio (20k ctx) — на других провайдерах шаг пропускается.

## Orchestra mode smoke (unit, в CI)

Без LLM:

```bash
go test ./internal/tools -run Orchestra
go test ./internal/agent -run 'Orchestra|PlanEnter'
go test ./internal/prompt -run Orchestra
go test ./internal/tasks -run Worker
```

## LSP install (TUI)

Автотест UI: `go test ./ui/tui/view -run TestModal_LSPInstall`.

Ручной чеклист (полный UX): `ui/tui/README.md` § LSP smoke — modal → worker edit → inline diagnostics.

## Связанные документы

- [planner-worker.md](./planner-worker.md) — Lead/Worker контракт
- [modes.md](../modes.md) — `--mode orchestra`
- [tools-status.md](../tools-status.md) — `plan_enter` legacy stub
