# Forgiving edit — стратегии поиска `search` в resolver

Когда LLM вызывает `edit` (или `final.patches` типа `file.search_replace`), **resolver** (`patch/resolver/external_patches.go`) должен найти фрагмент `search` в текущем файле и вычислить byte-range для `file.replace_range`.

Orchestra и OpenCode решают одну проблему: **модель редко копирует файл байт-в-байт** — лишние пробелы, `\r\n`, табы, escape-последовательности в JSON.

Разница: OpenCode прогоняет **до 9 стратегий** подряд (включая fuzzy/Levenshtein); Orchestra — **каскад с hard stop** на ambiguous match и обязательным **`file_hash`**.

---

## Invariants Orchestra (не ослабляем)

1. **Unique match** — 0 вхождений → `StaleContent`; >1 → `AmbiguousMatch`.
2. **Offsets в original file** — после нормализации маппим обратно в байты on-disk файла.
3. **`file_hash`** — applier проверяет hash перед записью (anti-stale).
4. **No fuzzy-by-default** — Phase 11a добавляет только **детерминированные** нормализации, не «похожесть строк».

---

## Orchestra сегодня (9-pass)

```
exact → lineTrimmed → indentFlexible → whitespaceNormalized → escapeNormalized → trimmedBoundary → blockAnchor → fuzzyBlock → doubleAnchor → FAIL
```

| # | Имя (OpenCode analog) | Что делает | Пример |
|---|------------------------|------------|--------|
| 1 | **Simple** / `findUnique` | Подстрока `search` должна встретиться в файле **ровно один раз** как есть | Идеальный copy-paste из `read` |
| 2 | **LineTrimmed** / `lineTrimmedFind` | CRLF→LF; на каждой строке убираем trailing spaces/tabs; сравниваем нормализованные строки; offsets → original | LLM добавил пробелы в конце строк |
| 3 | **IndentationFlexible** / `indentFlexibleFind` | Pass 2 + leading tabs→4 spaces; прощает разницу indent между needle и файлом | LLM сбил отступ блока на 2 spaces |

Код: `findUnique`, `lineTrimmedFind`, `indentFlexibleFind`, `normalizeTrailingWS`, `normalizeLeadingAndTrailingWS`.

---

## OpenCode (~9 strategies) — что ещё бывает

OpenCode (`tool/edit.ts`) пробует цепочку **до записи на диск**. Имена и порядок могут чуть отличаться; смысл такой:

| # | Стратегия | Суть | Есть у нас? |
|---|-----------|------|-------------|
| 1 | Simple | exact substring | ✅ pass 1 |
| 2 | LineTrimmed | trim line endings | ✅ pass 2 |
| 3 | BlockAnchor | первая+последняя строка блока как якоря; середина verbatim из файла | ✅ Phase 11 A2 |
| 4 | WhitespaceNormalized | все runs пробелов/tab → один пробел (или collapse internal WS) | ✅ Phase 11 A1 |
| 5 | IndentationFlexible | tabs/spaces в начале строк | ✅ pass 3 |
| 6 | EscapeNormalized | `\"` ↔ `"`, `\\n` ↔ newline, unicode escapes | ✅ Phase 11 A1 |
| 7 | TrimmedBoundary | trim только **начало и конец** всего блока search (не каждой строки) | ✅ Phase 11 A1 |
| 8 | ContextAware / **FuzzyBlock** | якоря strict; середина — bounded Levenshtein ≥0.85 per line | ✅ E4 pass 8 |
| 9 | MultiOccurrence / **DoubleAnchor** | first+last two lines as anchors | ✅ E4 pass 9 (unique-only) |

---

## Phase 11a — passes 4–6 ✅ (A1)

### Pass 4: WhitespaceNormalized

**Проблема:** внутри строки LLM пишет `foo    bar`, в файле `foo bar`.

**Идея:** нормализовать **внутренние** whitespace runs (не только trailing per line) в canonical form, искать там, маппить offsets обратно.

**Риск:** два похожих блока могут слиться → **AmbiguousMatch** (это OK, invariant).

### Pass 5: EscapeNormalized

**Проблема:** модель в tool JSON экранирует: `\"hello\\nworld\"` vs реальный многострочный текст в файле.

**Идея:** unescape needle (JSON/string escapes) и сравнить с файлом; только если unique match.

### Pass 6: TrimmedBoundary

**Проблема:** лишний `\n` или пробел **в начале/конце** всего блока search, строки внутри верные.

**Иdea:** `strings.TrimSpace` на needle и на sliding windows файла **с сохранением line structure** — уже после pass 2/3.

**Отличие от LineTrimmed:** trim per-line trailing vs trim whole-block edges.

---

## Phase 11a-ext: BlockAnchor (strict)

**Проблема:** блок из 20 строк, съехали отступы **внутри**, но первая и последняя строки совпадают с файлом.

**OpenCode:** якоря + fuzzy middle (Levenshtein).

**Orchestra (strict):**

1. Разбить needle на lines; взять **first non-empty** и **last non-empty** line.
2. Найти в файле пары (startLine, endLine) с exact/trimmed match якорей.
3. Если **ровно одна** пара — заменить диапазон строк между якорями.
4. Если 0 или >1 — `StaleContent` / `AmbiguousMatch`.

**Без Levenshtein** — не матчим «похожую» середину; только exact bytes между якорями (или pass 2/3 на этом диапазоне).

---

## Что сознательно не добавляем в 11a

| Стратегия | Почему |
|-----------|--------|
| Levenshtein / fuzzy ratio 0.3 | Может попасть в соседний блок; ломает eval determinism |
| MultiOccurrence replace-all | Нарушает unique-match invariant |
| ContextAware | Сложно, дорого; нужны метрики что pass 4–6 не хватило |

---

## Связь с agent loop

```
LLM edit/search_replace
  → resolver cascade (passes 1..N)
  → ops.replace_range + file_hash
  → applier (atomic write)
```

При fail resolver возвращает `StaleContent` / `AmbiguousMatch` → agent inject hint → **ещё один LLM step**. Forgiving passes **уменьшают число таких шагов** без ослабления applier.

---

## Тесты

- `patch/resolver/external_patches_test.go` — unit на каждый pass.
- `go test ./patch/resolver/...`
- Baseline / after: `orchestra eval` + count `resolve_failed` in `llm_log.jsonl`.

---

## Ссылки

- Resolver: `patch/resolver/external_patches.go`
- OpenCode reference: `_opencode/packages/opencode/src/tool/edit.ts`
- Phase plan: `docs/ROADMAP.md` § Фаза 11
