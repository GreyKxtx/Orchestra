# Input Selection & Cursor — Phase 2 Design

**Date:** 2026-05-12
**Topic:** chat input — фиксы багов, OpenCode-набор шорткатов, mouse extras, multi-line через Shift+Enter
**Status:** approved (brainstorming), pending plan
**Related:** [`docs/superpowers/plans/2026-05-12-input-selection-cursor.md`](../plans/2026-05-12-input-selection-cursor.md) (Phase 1, частично исполнен в коммитах f4376be → 2617082)

---

## 1. Goal

Привести редактирование chat-инпута в TUI к уровню «обычный десктопный редактор»: тип-замена выделения, корректное позиционирование курсора при удалении, полный набор клавишных и mouse-шорткатов как у OpenCode/Cursor, и переход к multi-line вводу через `Shift+Enter`.

## 2. Non-Goals

- Undo / Redo (нет history stack)
- Soft-wrap навигация внутри одной логической строки (визуальные «верх/низ» на wrap-границе)
- Drag-selection в чате (рендере сообщений) — только инпут
- IME composition / dead keys
- Кастомный editor вместо `bubbles/textarea` (Approach C из брейнсторма) — отдельный проект

## 3. Bugs Fixed

| # | Где | Симптом | Источник |
|---|---|---|---|
| B1 | `view/input.go:151 DeleteSelection` | После Backspace на выделении в середине строки курсор прыгает в конец value | `ta.SetValue` сбрасывает курсор в конец, далее нет восстановления позиции |
| B2 | `app_update.go:209-219` fall-through в textarea | Печать символа при активном выделении — выделение **не заменяется**, символ дописывается рядом | `ClearSelection()` зовётся уже **после** того как textarea получил KeyMsg |
| B3 | `app_update.go` (отсутствует) | `Delete` (forward) при активном выделении удаляет 1 символ справа, выделение остаётся | Нет специального case для `delete` |
| B4 | `app_update.go` (отсутствует) | `Ctrl+V` при активном выделении вклеивает текст рядом с выделением | Нет case для `ctrl+v` |
| B5 | `app_update.go:170` mouse motion | Drag за пределы первой визуальной строки игнорируется при multi-line | Жёсткое `m.Y == a.inputRowY` |

## 4. New Features

### Клавиатура

- `Ctrl+A` — select all
- `Ctrl+X` — cut (copy selection в clipboard + delete) + toast «Вырезано»
- `Ctrl+V` — paste (с заменой активного выделения)
- `Shift+Home` / `Shift+End` — выделить до начала / конца текущей логической строки
- `Ctrl+Shift+Home` / `Ctrl+Shift+End` — выделить до начала / конца документа
- `Ctrl+Home` / `Ctrl+End` — переместить курсор в начало / конец документа
- `Shift+Up` / `Shift+Down` — vertical selection (для multi-line)
- `Shift+Enter` — вставить `\n` (multi-line input)
- **Type-to-replace** — печать любой printable runes при активном выделении заменяет выделение

### Mouse

- **Double-click** — выделение слова под курсором (граница: `unicode.IsLetter` || `IsDigit` || `_`)
- **Triple-click** — выделение логической строки (между ближайшими `\n`)
- **Shift+click** — расширение текущего выделения до позиции клика (anchor сохраняется)
- **Multi-line drag** — drag работает в диапазоне `inputRowY .. inputRowY + ta.Height()`

### Multi-line input

- `Shift+Enter` вставляет `\n` в позицию курсора (через `ta.InsertRune('\n')`)
- `Enter` всегда submit (как сейчас)
- Высота textarea автоадаптивна: `ta.SetHeight(min(LineCount, 5))` после каждого изменения value
- `inputBoxHeight` в `app_view.go:207` вычисляется из `ta.Height()`, не хардкод 5
- `WelcomeRender` обрабатывает multi-line: разбивает value по `\n`, каждая строка рендерится с overlay (cursor / selection) тем же алгоритмом

## 5. Architecture — Approach B (централизация в `view.Input`)

### Принцип

Все операции редактирования становятся методами `*Input`. Логика покидает `app_update.go`. `routeKey` превращается в тонкий dispatcher.

```
routeKey (app_update.go)        view.Input              bubbles/textarea
─────────────────────────       ──────────────         ─────────────────
case "ctrl+a":           ─►     SelectAll()       ─►   SetCursor / value access
  return
case "ctrl+v":           ─►     Paste(clip)       ─►   InsertString
case printable + sel:    ─►     ReplaceSelection  ─►   SetValue + InsertString + SetCursor
case "delete":           ─►     DeleteForward     ─►   deleteAfterCursor / SetValue+SetCursor
```

### Ключевое правило

Ни один edit-метод **не использует `ta.SetValue` без последующего восстановления позиции курсора**. Все методы используют либо нативный `ta.InsertString`/`ta.InsertRune` (вставляют на текущей позиции), либо хелпер `moveCursorAbs(pos)` (`SetCursor(0)` + `CursorDown × row` + `SetCursor(col)`).

Это убирает класс багов категории B1 на уровне дизайна.

## 6. `Input` API

### Поля

```go
type Input struct {
    ta               textarea.Model
    width            int
    mode             string
    selAnchor        int       // -1 = no selection; абс. rune-позиция
    mouseCaret       int
    mouseCaretActive bool
}
```

### Внутренние хелперы

| Метод | Назначение |
|---|---|
| `absoluteToRowCol(pos int) (row, col int)` | Конвертирует абс. rune-позицию в (row, colOnRow) по `\n` |
| `rowColToAbsolute(row, col int) int` | Обратное преобразование |
| `moveCursorAbs(pos int)` | `SetCursor(0)` → `CursorDown × row` → `SetCursor(col)`; clamp к границам value |
| `currentLineRange() (lo, hi int)` | Границы (абс.) текущей логической строки |
| `isWordChar(r rune) bool` | `unicode.IsLetter(r) \|\| IsDigit(r) \|\| r == '_'` |

### Публичный API

| Метод | Сигнатура | Описание |
|---|---|---|
| Cursor | `CursorPos() int` | абс. позиция курсора |
| | `MoveCursorAbs(pos int)` | поставить курсор в абс. pos |
| Insert | `InsertText(s string)` | `ta.InsertString(s)` (с учётом `\n`) |
| | `ReplaceSelection(s string) bool` | удалить selection, вставить s, курсор после s; если selection нет — InsertText |
| Delete | `DeleteSelection() bool` | удалить выделение, курсор на `lo` |
| | `DeleteForward()` | если selection — DeleteSelection; иначе удалить 1 руну справа |
| | `DeleteBackward()` | selection → DeleteSelection; иначе 1 руна слева |
| Select | `SelectAll()` | `selAnchor = 0`, курсор → конец value (только если value не пуст) |
| | `SelectToLineStart()` | anchor (если нет) + курсор → начало логической строки |
| | `SelectToLineEnd()` | anchor + курсор → конец логической строки |
| | `SelectToDocStart()` | anchor + курсор → 0 |
| | `SelectToDocEnd()` | anchor + курсор → len(runes) |
| | `SelectToPos(pos int)` | anchor (если нет) + MoveCursorAbs(pos) |
| | `ExtendSelectionTo(pos int)` | anchor сохраняется, MoveCursorAbs(pos) (для Shift+click) |
| Ranges | `WordRange(pos int) (lo, hi int)` | границы слова вокруг pos |
| | `LineRange(pos int) (lo, hi int)` | границы логической строки |
| Clipboard ops | `SelectedText() string` | runes\[lo:hi\] |
| | `Cut() string` | возвращает selected, удаляет выделение |
| | `Paste(text string) bool` | ReplaceSelection(text); no-op если text пуст |
| Multi-line | `InsertNewline()` | `ta.InsertRune('\n')` (для Shift+Enter) |
| | `SyncHeight(max int)` | `ta.SetHeight(clamp(LineCount, 1, max))` |

### Существующие методы (судьба)

- `SetMouseCaret` / `MouseCaret` / `ClearMouseCaret` — **остаются** (используются для drag visual cursor)
- `SetAnchor` / `ClearSelection` / `HasSelection` / `SelectionRange` — **остаются** (используются routeKey и mouse handlers)
- `AbsolutePos` — **переименовать в `CursorPos`** для единообразия; обновить все call sites (на момент написания спеки: `app_update.go:327, 333, 339, 345, 351, 357`)
- `DeleteSelection` — **переписать** так, чтобы курсор оставался на `lo` после удаления (фикс B1)

## 7. Shortcut Table (порядок в `routeKey` switch)

| Клавиша | Действие | Реализация |
|---|---|---|
| `ctrl+c` | copy / quit | как сейчас |
| `ctrl+a` | select all | `input.SelectAll()` |
| `ctrl+x` | cut | `s := input.Cut(); clipboard.WriteAll(s); toast("Вырезано")` |
| `ctrl+v` | paste | `txt, _ := clipboard.ReadAll(); input.Paste(txt)` |
| `shift+left` | selection ← | anchor (если нет) + `sendKeyToTA(KeyLeft)` |
| `shift+right` | selection → | anchor + `sendKeyToTA(KeyRight)` |
| `ctrl+shift+left` | word selection ← | anchor + `sendKeyToTA(KeyCtrlLeft)` |
| `ctrl+shift+right` | word selection → | anchor + `sendKeyToTA(KeyCtrlRight)` |
| `alt+shift+left/right` | WT fallback word selection | как сейчас |
| `shift+up` | vert selection ↑ | anchor + `sendKeyToTA(KeyUp)` |
| `shift+down` | vert selection ↓ | anchor + `sendKeyToTA(KeyDown)` |
| `shift+home` | sel → line start | `input.SelectToLineStart()` |
| `shift+end` | sel → line end | `input.SelectToLineEnd()` |
| `ctrl+shift+home` | sel → doc start | `input.SelectToDocStart()` |
| `ctrl+shift+end` | sel → doc end | `input.SelectToDocEnd()` |
| `home` | clear sel + line start | `input.ClearSelection(); ta.CursorStart()` |
| `end` | clear sel + line end | `input.ClearSelection(); ta.CursorEnd()` |
| `ctrl+home` | clear sel + doc start | `input.ClearSelection(); input.MoveCursorAbs(0)` |
| `ctrl+end` | clear sel + doc end | `input.ClearSelection(); input.MoveCursorAbs(len)` |
| `delete` | forward-delete sel-aware | `input.DeleteForward()` |
| `backspace` | back-delete sel-aware | `input.DeleteBackward()` |
| `shift+enter` | insert newline | `input.InsertNewline()` |
| `enter` | submit | `handleEnter()` |
| printable + sel active | replace selection | в Update до textarea: `input.ReplaceSelection(km.Runes); return` |

## 8. Mouse Model

### Tracker (поля в `App`)

```go
mouseLastClickAt    time.Time
mouseLastClickPos   int    // абс. rune-позиция
mouseClickCount     int    // 1 / 2 / 3
```

### Логика на `MouseButtonLeft + Press`

```
shiftHeld := m.Shift  // bubbletea Mouse Shift modifier

if shiftHeld && input.HasSelection() {
    input.ExtendSelectionTo(clickPos)
    return
}

now := time.Now()
if now.Sub(mouseLastClickAt) <= 400ms && abs(clickPos - mouseLastClickPos) <= 2 {
    mouseClickCount++
} else {
    mouseClickCount = 1
}
mouseLastClickAt = now
mouseLastClickPos = clickPos

switch mouseClickCount {
case 1:
    input.ClearSelection()
    input.SetAnchor(clickPos)
    input.SetMouseCaret(clickPos)
    mouseDown = true
case 2:
    lo, hi := input.WordRange(clickPos)
    input.SetAnchor(lo)
    input.MoveCursorAbs(hi)
case 3:
    lo, hi := input.LineRange(clickPos)
    input.SetAnchor(lo)
    input.MoveCursorAbs(hi)
}
```

### Логика на `Motion`

```
if mouseDown && m.Y >= inputRowY && m.Y < inputRowY + ta.Height() {
    pos := mouseXYToAbsolutePos(m.X, m.Y - inputRowY)
    input.SetMouseCaret(pos)
}
```

### Логика на `Release`

как сейчас + сброс mouseDown.

### Constants

- Double-click window: **400 ms**
- Position tolerance: **2 руны** (учитывает мелкий drift курсора между кликами)

## 9. Edge Cases

| Случай | Решение |
|---|---|
| Backspace без выделения, курсор в начале | textarea-no-op |
| Delete без выделения, курсор в конце | textarea-no-op |
| Ctrl+V с пустым clipboard | `Paste("")` → no-op, без toast |
| Ctrl+V с многострочным текстом | InsertString сохраняет `\n` — multi-line появляется автоматом, SyncHeight адаптирует высоту |
| Ctrl+A на пустом инпуте | `SelectAll` проверяет `len(value) > 0`, иначе no-op |
| Shift+arrow когда selection уже есть | anchor НЕ перезаписывается (защищено `if !HasSelection()`) |
| Type-to-replace с пустыми km.Runes | `isPrintableKey` фильтрует только `km.Type == KeyRunes && len(km.Runes) > 0` |
| Mouse drag за пределы input строк | clamp m.Y до диапазона; ниже → pos = end of last visible line; выше → pos = start |
| Shift+click без активного selection | `SetAnchor(CursorPos())` затем `MoveCursorAbs(clickPos)` |
| Shift+Enter с selection | `ReplaceSelection("\n")` |
| Palette / mention / modal активны | новые shortcut case-блоки идут **после** palette/onboarding/modal проверок в `routeKey` |
| Esc с активным selection | `ClearSelection()` — добавить в начало `case "esc"` |
| Triple-click на одной логической строке мульти-визуально | LineRange по `\n`, не по визуальным wrap-строкам |
| Double-click на пробеле | возвращается `(pos, pos)` — нет выделения, эффективно no-op |
| Mouse drag из welcome view в chat view | welcome dismisses на первом submit; смена view невозможна во время drag |

## 10. Testing Strategy

User preference: implementation first, tests after ([[user_prefs]]).

### Unit-тесты `ui/tui/view/input_test.go` (создаётся после имплементации)

- `TestInput_ReplaceSelection_PositionsCursor` — после ReplaceSelection cursor pos == lo + len(replacement)
- `TestInput_DeleteSelection_PositionsCursor` — фикс B1
- `TestInput_SelectAll_Empty` — no-op на пустом value
- `TestInput_SelectAll_NonEmpty` — anchor=0, cursor=len
- `TestInput_WordRange_Middle` — на «hello world», pos=2 → (0,5)
- `TestInput_WordRange_OnSpace` — pos на пробеле → (pos, pos)
- `TestInput_WordRange_AtEnd` — pos = len → (wordStart, len)
- `TestInput_LineRange_MultiLine` — на «a\nbcd\ne», pos=3 → (2, 5)
- `TestInput_MoveCursorAbs_MultiLine` — после move CursorPos() возвращает исходное
- `TestInput_Paste_WithSelection` — заменяет выделение
- `TestInput_DeleteForward_NoSelection`
- `TestInput_DeleteForward_WithSelection`
- `TestInput_InsertNewline_SplitsLine`
- `TestInput_SyncHeight_Caps`

### Smoke checklist (ручной, после `go build -o orchestra.exe ./cmd/orchestra`)

1. Type-to-replace: выделить «abc», нажать `x` → «x»
2. Paste-with-replace: выделить «abc», в clipboard «yyy», Ctrl+V → «yyy»
3. Ctrl+A → весь текст подсвечен; нажать любую букву → текст заменён ею
4. Ctrl+X → текст в clipboard, инпут пуст, toast «Вырезано»
5. Delete на выделении → выделение удалено, курсор на lo
6. Backspace в середине строки на выделении → курсор на lo (фикс B1)
7. Shift+Home → выделение от курсора до начала текущей строки
8. Ctrl+Shift+Home → выделение до начала всего value
9. Shift+Enter → newline вставлен, инпут вырос
10. Shift+Up / Shift+Down в multi-line → vert selection
11. Double-click на «hello world» → подсвечено «hello»
12. Triple-click → подсвечена логическая строка
13. Shift+click → текущее выделение расширяется до клика
14. Mouse drag через 2 строки multi-line → selection растягивается
15. Ctrl+Home / Ctrl+End → курсор в начало / конец без выделения

### Out of scope для тестов в этой фазе

- teatest integration tests (mouse multi-click и terminal-specific shortcuts — слишком хрупко)
- Cross-terminal compatibility (WT, ConEmu, VSCode, Alacritty) — проверяется smoke в WT, остальные — best effort

## 11. Open Risks

- **`bubbles/textarea` v1.0.0** — `SetCursor(col)` работает только на текущей строке. Multi-line cursor positioning требует комбинации `SetCursor(0) + CursorDown × N + SetCursor(col)`. Поведение `CursorDown` на последней строке может не двигать курсор (зависит от bubbles) — лечится явным clamp в `moveCursorAbs`.
- **Mouse Shift detection** — `tea.MouseMsg` имеет поле `Shift` (bubbletea v1.3.10 — есть). Проверить флаг доступности до имплементации.
- **`shift+enter` в Windows Terminal** — может не отправляться отдельно от `enter` в некоторых терминалах. Fallback: `alt+enter` или `ctrl+j` как альтернативные шорткаты. Решается smoke-тестом.

## 12. References

- Phase 1 plan: `docs/superpowers/plans/2026-05-12-input-selection-cursor.md`
- bubbles textarea API: `C:\Users\andre\go\pkg\mod\github.com\charmbracelet\bubbles@v1.0.0\textarea\textarea.go`
- Recent commits: `871490e..2617082` (Phase 1 implementation)
- TUI architecture memory: [[tui_visual_refactor]]
