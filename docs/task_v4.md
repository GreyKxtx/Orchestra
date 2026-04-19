Ладно, ещё раз, максимально приземлённо, без философии и без “а давай ещё улучшений”. Тебе нужно **перестать тестировать двигатель на стенде** и наконец поставить его в машину. Вот список “что доделать”, “какие тесты”, “какой ожидаемый результат”, чтобы можно было нормально гонять реальную модель на реальных проектах.

---

## 0) Где мы сейчас

✅ Core по stdio JSON-RPC работает.
✅ `fs.apply_ops` и базовые safety есть.
✅ e2e_nollm через `orchestra core` уже проверяют apply/dry-run/stale/path traversal.

❌ Нет **живого UX-пути пользователя**: “запрос → план → diff → применить → откат/повтор”.
❌ Нет **детерминированного режима** для повторного применения плана без модели (обязателен для регрессий).
❌ Нет “демо/примеров” чтобы ты сам видел результат глазами, а не только “PASS”.

---

# 1) Что доделать (минимум MVP для реальных кейсов)

## A. Артефакты и понятный вывод CLI (обязательное)

`orchestra apply` должен всегда создавать в `.orchestra/`:

* `plan.json` (то, что модель предложила или что ты применяешь)
* `diff.txt` (человекочитаемый diff)
* `last_result.json` (структурированный результат: changed_files, applied, diffs)
* `last_run.jsonl` (лог шагов: tool_call, tool_ok, tool_error, resolve, apply)

Ожидаемый stdout:

* `Changed files: ...`
* `Dry-run: true/false`
* `Plan saved to: .orchestra/plan.json`
* `Diff saved to: .orchestra/diff.txt`
* при ошибке: `error_code=StaleContent` и причина

**Зачем:** чтобы ты *видел результат*, а не гадал “что произошло”.

---

## B. Режим `--from-plan` (детерминированный apply без LLM)

Добавить команду/флаг:

```bash
orchestra apply --from-plan .orchestra/plan.json --apply
```

Он:

* не вызывает LLM
* читает plan.json
* применяет ops/patches через тот же pipeline
* пишет diff/result как обычно

**Зачем:** чтобы тесты и отладка не зависели от “настроения модели”.

---

## C. Ops для “реальных проектов”: создание файлов и директорий

Убедись, что в `fs.apply_ops` реально поддержано и покрыто тестами:

* `file.mkdir_all`
* `file.write_atomic` (dry-run, must_not_exist, атомарность)

**Зачем:** иначе “создай новый файл” в реальных задачах просто не работает.

---

## D. “Demo path” (1 команда, которая показывает реальную работу)

Добавить:

```bash
orchestra demo tiny-go --apply
```

Она в temp/примерной папке реально:

* создаёт директории
* создаёт новый .go файл
* правит существующие файлы
* печатает diff
* показывает changed_files

**Зачем:** это твой “Cursor moment”: нажал кнопку и увидел живой проект, а не “PASS”.

---

# 2) Какие тесты нужны (чёткая матрица)

## A. E2E NOLLM (обязательно, детерминированно)

Папка: `tests/e2e` или `tests/e2e_nollm`

Добавить/дожать:

1. **CLI артефакты**

* `orchestra apply --plan-only` → создаёт `plan.json` и `diff.txt` и `last_result.json`
* формат файлов валиден JSON, diff не пустой (best-effort)

2. **--from-plan**

* сначала сгенерили plan.json (можно вручную в тесте)
* затем `orchestra apply --from-plan plan.json --apply`
* файл реально изменился
* changed_files корректный

3. **mkdir_all + write_atomic**

* `mkdir_all internal/x`
* `write_atomic internal/x/new.go`
* файл реально появляется, содержимое совпадает

4. **write_atomic must_not_exist**

* первый раз create ok
* второй раз create с `must_not_exist=true` → `AlreadyExists`
* никаких побочных эффектов

5. **multi-op all-or-nothing**

* op1 валиден (пишет файл)
* op2 специально stale/invalid
* итог: **ничего не записано**, backups нет, changed_files пусто

Команда прогонки:

```bash
go test -count=20 ./tests/e2e -v
```

**Ожидаемый результат:** 20/20 PASS стабильно.

---

## B. Unit tests (минимум, но важные)

1. applier: atomic write + backup logic
2. applier: stale не пишет вообще
3. tools: path traversal, symlink escape

**Ожидаемый результат:** `go test ./...` PASS.

---

## C. Real-LLM тесты (потом, когда всё выше зелёное)

Тут правило такое:

* Real-LLM тесты НЕ должны ломать CI по “модель тупит”.
* Модель тупит → SKIP
* Инфра умерла → FAIL
* Система нарушила safety → FAIL

Запуск:

```bash
ORCH_E2E_LLM=1 go test ./tests/e2e_real_llm -v
```

---

# 3) Какой результат считается “готово для реальных кейсов”

Ты считаешь систему готовой, когда выполняются **все**:

✅ `go test ./...` PASS
✅ `go test -count=20 ./tests/e2e` PASS
✅ `orchestra demo tiny-go --apply` создаёт/правит файлы и показывает diff
✅ `orchestra apply "..." --plan-only` оставляет артефакты, даже если модель ошиблась
✅ `orchestra apply --from-plan .orchestra/plan.json --apply` детерминированно применяет правки

После этого можно нормально запускать реальную модель на реальных проектах, потому что:

* даже если модель бредит, ты получаешь plan+diff и можешь применить/проверить
* у тебя есть deterministic replay
* у тебя есть инструментальная база (create/mkdir/write/replace)
* у тебя есть понятный UX

Вот это и есть твой “Cursor-like” MVP, а не бесконечное “PASS”.
