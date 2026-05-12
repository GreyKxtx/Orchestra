# Input Selection & Cursor — Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Привести редактирование chat-инпута к уровню «обычный десктопный редактор»: type-to-replace, корректное позиционирование курсора при удалении, OpenCode-набор клавишных и mouse-шорткатов, multi-line через Shift+Enter.

**Architecture:** Approach B — централизация edit-операций в `view.Input`. Все изменения значения и позиции курсора идут через методы `*Input`, использующие нативный API bubbles textarea (`SetCursor`, `InsertString`, `CursorUp/Down`). `routeKey` становится тонким диспатчером. Никаких прямых `ta.SetValue` без последующего `moveCursorAbs`.

**Tech Stack:** Go 1.21, `charmbracelet/bubbletea` v1.3.10, `charmbracelet/bubbles` v1.0.0, `charmbracelet/lipgloss` v1.1.x, `atotto/clipboard`.

**Source spec:** [`docs/superpowers/specs/2026-05-12-input-selection-cursor-phase2-design.md`](../specs/2026-05-12-input-selection-cursor-phase2-design.md)

---

## File Structure

| Файл | Изменение | Ответственность |
|---|---|---|
| `ui/tui/view/input.go` | Heavy modify (~+250 строк) | Все edit-методы + multi-line WelcomeRender |
| `ui/tui/app_update.go` | Modify (routeKey + Update + mouse) | Тонкий dispatcher шорткатов, type-to-replace, mouse multi-click |
| `ui/tui/app_view.go:206-217` | Modify (динамический inputBoxHeight) | Высота input box зависит от `ta.Height()` |
| `ui/tui/app.go` | Modify (поля mouse tracker) | `mouseLastClickAt`, `mouseLastClickPos`, `mouseClickCount` |
| `ui/tui/view/input_test.go` | Create | Unit-тесты на API `Input` |

User preference: implementation before tests ([[user_prefs]]). Unit-тесты — Task 18, в самом конце.

---

## Task 1: Add internal helpers + rename `AbsolutePos` → `CursorPos`

**Files:** `ui/tui/view/input.go`, `ui/tui/app_update.go`

Подготовка: добавляем внутренние конвертеры абс./row+col и `moveCursorAbs` — фундамент для всех будущих методов.

- [ ] **Step 1: Добавить unicode import в `view/input.go`**

В файле `ui/tui/view/input.go` найти блок import:

```go
import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)
```

Заменить на:

```go
import (
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)
```

- [ ] **Step 2: Переименовать `AbsolutePos` → `CursorPos`**

В `ui/tui/view/input.go` найти:

```go
// AbsolutePos returns the absolute rune index of the textarea cursor across all lines.
func (in Input) AbsolutePos() int { return absolutePos(in.ta) }
```

Заменить на:

```go
// CursorPos returns the absolute rune index of the textarea cursor across all lines.
func (in Input) CursorPos() int { return absolutePos(in.ta) }
```

- [ ] **Step 3: Обновить вызовы `AbsolutePos` в `app_update.go`**

В `ui/tui/app_update.go` заменить **все** `a.input.AbsolutePos()` на `a.input.CursorPos()`. На момент написания плана это строки 327, 333, 339, 345, 351, 357 (внутри case `shift+left`, `shift+right`, `ctrl+shift+left`, `ctrl+shift+right`, `alt+shift+left`, `alt+shift+right`).

Сделать через `Edit` с `replace_all: true`:

```
old_string: a.input.AbsolutePos()
new_string: a.input.CursorPos()
```

- [ ] **Step 4: Добавить helpers в `view/input.go`**

В `ui/tui/view/input.go` найти функцию `absolutePos`:

```go
// absolutePos computes the absolute rune index of the textarea cursor,
// accounting for multiple lines (each separated by '\n').
func absolutePos(ta textarea.Model) int {
	info := ta.LineInfo()
	if info.RowOffset == 0 {
		return info.CharOffset
	}
	lines := strings.Split(ta.Value(), "\n")
	pos := 0
	for i := 0; i < info.RowOffset && i < len(lines); i++ {
		pos += len([]rune(lines[i])) + 1 // +1 for '\n'
	}
	return pos + info.CharOffset
}
```

Сразу **после** неё добавить:

```go
// absoluteToRowCol converts an absolute rune index to (logical row, col on row).
// Lines are split by '\n'. Out-of-range pos is clamped to [0, len].
func (in Input) absoluteToRowCol(pos int) (row, col int) {
	runes := []rune(in.ta.Value())
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}
	row = 0
	last := 0
	for i := 0; i < pos; i++ {
		if runes[i] == '\n' {
			row++
			last = i + 1
		}
	}
	col = pos - last
	return row, col
}

// moveCursorAbs positions the textarea cursor at the given absolute rune index.
// Strategy: navigate from the current row to the target row via CursorUp/Down,
// then SetCursor(col) for column-within-line. Clamped to [0, len(value)].
func (in *Input) moveCursorAbs(pos int) {
	runes := []rune(in.ta.Value())
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}
	row, col := in.absoluteToRowCol(pos)
	currentRow := in.ta.Line()
	delta := row - currentRow
	if delta > 0 {
		for i := 0; i < delta; i++ {
			in.ta.CursorDown()
		}
	} else if delta < 0 {
		for i := 0; i < -delta; i++ {
			in.ta.CursorUp()
		}
	}
	in.ta.SetCursor(col)
}

// currentLineRange returns the absolute [lo, hi) bounds of the logical line
// containing pos. Lines are split by '\n'.
func (in Input) currentLineRange(pos int) (lo, hi int) {
	runes := []rune(in.ta.Value())
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}
	lo = pos
	for lo > 0 && runes[lo-1] != '\n' {
		lo--
	}
	hi = pos
	for hi < len(runes) && runes[hi] != '\n' {
		hi++
	}
	return lo, hi
}

// isWordChar — letter, digit, or underscore.
func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}
```

- [ ] **Step 5: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```powershell
git add ui/tui/view/input.go ui/tui/app_update.go
git commit -m "refactor(input): rename AbsolutePos to CursorPos; add row/col helpers and moveCursorAbs"
```

---

## Task 2: Fix `DeleteSelection` (B1) + add `ReplaceSelection`, `InsertText`, `DeleteForward`, `DeleteBackward`

**Files:** `ui/tui/view/input.go`

- [ ] **Step 1: Переписать `DeleteSelection` и добавить остальные методы**

В `ui/tui/view/input.go` найти существующий `DeleteSelection`:

```go
// DeleteSelection removes the selected rune range from the textarea value
// and clears the selection. Returns true if anything was deleted.
func (in *Input) DeleteSelection() bool {
	lo, hi, ok := in.SelectionRange()
	if !ok || lo == hi {
		return false
	}
	runes := []rune(in.ta.Value())
	newVal := string(runes[:lo]) + string(runes[hi:])
	in.ta.SetValue(newVal)
	in.selAnchor = -1
	return true
}
```

Заменить на блок:

```go
// DeleteSelection removes the selected rune range, clears the selection,
// and positions the cursor at the start of the deleted range. Returns true
// if anything was deleted.
func (in *Input) DeleteSelection() bool {
	lo, hi, ok := in.SelectionRange()
	if !ok || lo == hi {
		return false
	}
	runes := []rune(in.ta.Value())
	newVal := string(runes[:lo]) + string(runes[hi:])
	in.ta.SetValue(newVal)
	in.selAnchor = -1
	in.moveCursorAbs(lo)
	return true
}

// InsertText inserts s at the current cursor position via the native
// textarea InsertString (preserves cursor positioning).
func (in *Input) InsertText(s string) {
	if s == "" {
		return
	}
	in.ta.InsertString(s)
}

// ReplaceSelection removes the selected range, inserts s in its place,
// and leaves the cursor just after the inserted text. If there is no
// selection, behaves like InsertText. Returns true if anything was
// replaced (selection existed).
func (in *Input) ReplaceSelection(s string) bool {
	if !in.HasSelection() {
		in.InsertText(s)
		return false
	}
	lo, hi, _ := in.SelectionRange()
	runes := []rune(in.ta.Value())
	newVal := string(runes[:lo]) + s + string(runes[hi:])
	in.ta.SetValue(newVal)
	in.selAnchor = -1
	in.moveCursorAbs(lo + len([]rune(s)))
	return true
}

// DeleteForward deletes the active selection if any, otherwise deletes
// one rune to the right of the cursor.
func (in *Input) DeleteForward() {
	if in.DeleteSelection() {
		return
	}
	pos := in.CursorPos()
	runes := []rune(in.ta.Value())
	if pos >= len(runes) {
		return
	}
	newVal := string(runes[:pos]) + string(runes[pos+1:])
	in.ta.SetValue(newVal)
	in.moveCursorAbs(pos)
}

// DeleteBackward deletes the active selection if any, otherwise deletes
// one rune to the left of the cursor.
func (in *Input) DeleteBackward() {
	if in.DeleteSelection() {
		return
	}
	pos := in.CursorPos()
	if pos <= 0 {
		return
	}
	runes := []rune(in.ta.Value())
	newVal := string(runes[:pos-1]) + string(runes[pos:])
	in.ta.SetValue(newVal)
	in.moveCursorAbs(pos - 1)
}
```

- [ ] **Step 2: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```powershell
git add ui/tui/view/input.go
git commit -m "feat(input): add ReplaceSelection/InsertText/DeleteForward/DeleteBackward; fix DeleteSelection cursor pos (B1)"
```

---

## Task 3: Add selection-extending methods

**Files:** `ui/tui/view/input.go`

- [ ] **Step 1: Добавить методы выделения**

В `ui/tui/view/input.go` найти существующий `ClearSelection`:

```go
// ClearSelection removes any active selection.
func (in *Input) ClearSelection() { in.selAnchor = -1 }
```

Сразу **после** него добавить:

```go
// SelectAll selects the entire value: anchor at 0, cursor at end.
// No-op if the value is empty.
func (in *Input) SelectAll() {
	runes := []rune(in.ta.Value())
	if len(runes) == 0 {
		return
	}
	in.selAnchor = 0
	in.moveCursorAbs(len(runes))
}

// SelectToLineStart extends the selection (or starts one at the current
// cursor) to the start of the current logical line.
func (in *Input) SelectToLineStart() {
	pos := in.CursorPos()
	if !in.HasSelection() {
		in.selAnchor = pos
	}
	lo, _ := in.currentLineRange(pos)
	in.moveCursorAbs(lo)
}

// SelectToLineEnd extends the selection to the end of the current logical line.
func (in *Input) SelectToLineEnd() {
	pos := in.CursorPos()
	if !in.HasSelection() {
		in.selAnchor = pos
	}
	_, hi := in.currentLineRange(pos)
	in.moveCursorAbs(hi)
}

// SelectToDocStart extends the selection to the start of the entire value.
func (in *Input) SelectToDocStart() {
	pos := in.CursorPos()
	if !in.HasSelection() {
		in.selAnchor = pos
	}
	in.moveCursorAbs(0)
}

// SelectToDocEnd extends the selection to the end of the entire value.
func (in *Input) SelectToDocEnd() {
	pos := in.CursorPos()
	if !in.HasSelection() {
		in.selAnchor = pos
	}
	runes := []rune(in.ta.Value())
	in.moveCursorAbs(len(runes))
}

// SelectToPos extends the selection to absolute pos, anchoring at the
// current cursor if no selection is active yet.
func (in *Input) SelectToPos(pos int) {
	if !in.HasSelection() {
		in.selAnchor = in.CursorPos()
	}
	in.moveCursorAbs(pos)
}

// ExtendSelectionTo keeps the existing anchor and moves the cursor to pos.
// If no anchor exists yet, sets it to the current cursor first. Used by
// Shift+click and similar gestures.
func (in *Input) ExtendSelectionTo(pos int) {
	if !in.HasSelection() {
		in.selAnchor = in.CursorPos()
	}
	in.moveCursorAbs(pos)
}
```

- [ ] **Step 2: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```powershell
git add ui/tui/view/input.go
git commit -m "feat(input): add SelectAll/SelectToLineStart/SelectToLineEnd/SelectToDocStart/SelectToDocEnd/SelectToPos/ExtendSelectionTo"
```

---

## Task 4: Add `WordRange`, `LineRange`, `Cut`, `Paste`, `SelectedText`, `InsertNewline`, `SyncHeight`

**Files:** `ui/tui/view/input.go`

- [ ] **Step 1: Добавить публичные методы**

В `ui/tui/view/input.go` в конец файла (после `clampPos`) добавить:

```go
// WordRange returns the absolute [lo, hi) bounds of the word containing
// or adjacent to pos. Word chars are letters, digits, and underscore.
// If pos is on a non-word char with no adjacent word, returns (pos, pos).
func (in Input) WordRange(pos int) (lo, hi int) {
	runes := []rune(in.ta.Value())
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}
	// pos at end-of-value: check the rune to the left.
	if pos == len(runes) {
		if pos > 0 && isWordChar(runes[pos-1]) {
			lo = pos - 1
			for lo > 0 && isWordChar(runes[lo-1]) {
				lo--
			}
			return lo, pos
		}
		return pos, pos
	}
	// pos on a non-word char: try the rune to the left as a fallback.
	if !isWordChar(runes[pos]) {
		if pos > 0 && isWordChar(runes[pos-1]) {
			lo = pos - 1
			for lo > 0 && isWordChar(runes[lo-1]) {
				lo--
			}
			return lo, pos
		}
		return pos, pos
	}
	// pos on a word char: expand both directions.
	lo = pos
	for lo > 0 && isWordChar(runes[lo-1]) {
		lo--
	}
	hi = pos + 1
	for hi < len(runes) && isWordChar(runes[hi]) {
		hi++
	}
	return lo, hi
}

// LineRange returns the absolute [lo, hi) bounds of the logical line
// containing pos. Wrapper around currentLineRange for the public API.
func (in Input) LineRange(pos int) (lo, hi int) {
	return in.currentLineRange(pos)
}

// SelectedText returns the currently selected runes as a string, or ""
// if there is no selection.
func (in Input) SelectedText() string {
	lo, hi, ok := in.SelectionRange()
	if !ok {
		return ""
	}
	runes := []rune(in.ta.Value())
	if hi > len(runes) {
		hi = len(runes)
	}
	if lo < 0 {
		lo = 0
	}
	return string(runes[lo:hi])
}

// Cut returns the selected text and removes it from the value. Returns
// "" if there is no selection.
func (in *Input) Cut() string {
	if !in.HasSelection() {
		return ""
	}
	s := in.SelectedText()
	in.DeleteSelection()
	return s
}

// Paste inserts text at the cursor, replacing any active selection.
// Returns false if text is empty.
func (in *Input) Paste(text string) bool {
	if text == "" {
		return false
	}
	in.ReplaceSelection(text)
	return true
}

// InsertNewline inserts '\n' at the cursor, replacing any active selection.
func (in *Input) InsertNewline() {
	if in.HasSelection() {
		in.ReplaceSelection("\n")
		return
	}
	in.ta.InsertRune('\n')
}

// SyncHeight caps the textarea height to LineCount, clamped to [1, max].
// Call after any value mutation that may add or remove '\n' so the visible
// rows match the actual content (up to max).
func (in *Input) SyncHeight(max int) {
	h := in.ta.LineCount()
	if h < 1 {
		h = 1
	}
	if h > max {
		h = max
	}
	in.ta.SetHeight(h)
}
```

- [ ] **Step 2: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```powershell
git add ui/tui/view/input.go
git commit -m "feat(input): add WordRange/LineRange/SelectedText/Cut/Paste/InsertNewline/SyncHeight"
```

---

## Task 5: Type-to-replace (fix B2)

**Files:** `ui/tui/app_update.go`

- [ ] **Step 1: Добавить helper `isPrintableKey` в конец файла**

В `ui/tui/app_update.go`, в конце файла (после `handleEnter`) добавить:

```go
// isPrintableKey reports whether a key message represents printable input
// (regular typing or a bracketed paste from the terminal).
func isPrintableKey(km tea.KeyMsg) bool {
	return km.Type == tea.KeyRunes && len(km.Runes) > 0
}
```

- [ ] **Step 2: Перехватить ввод при активном выделении в `Update()`**

В `ui/tui/app_update.go` найти fall-through блок в конце `Update`:

```go
	// Forward all messages to textarea (default fall-through for unhandled keys).
	innerTA := a.input.Inner()
	updatedTA, taCmd := innerTA.Update(msg)
	*innerTA = updatedTA
	if _, isKey := msg.(tea.KeyMsg); isKey {
		// Any key that reaches textarea clears selection (user typed a character).
		a.input.ClearSelection()
		a.syncPalette()
		a.updateStatusHints()
		a.layout()
	}
	return a, taCmd
}
```

Заменить на:

```go
	// Type-to-replace: printable input while a selection is active replaces
	// the selection rather than appending alongside it.
	if km, ok := msg.(tea.KeyMsg); ok && a.input.HasSelection() && isPrintableKey(km) {
		a.input.ReplaceSelection(string(km.Runes))
		a.syncPalette()
		a.updateStatusHints()
		a.layout()
		return a, nil
	}
	// Forward all messages to textarea (default fall-through for unhandled keys).
	innerTA := a.input.Inner()
	updatedTA, taCmd := innerTA.Update(msg)
	*innerTA = updatedTA
	if _, isKey := msg.(tea.KeyMsg); isKey {
		// Any key that reaches textarea clears selection (user typed a character).
		a.input.ClearSelection()
		a.syncPalette()
		a.updateStatusHints()
		a.layout()
	}
	return a, taCmd
}
```

- [ ] **Step 3: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```powershell
git add ui/tui/app_update.go
git commit -m "fix(input): type-to-replace — printable keys replace active selection (B2)"
```

---

## Task 6: Backspace / Delete via new API (fix B3)

**Files:** `ui/tui/app_update.go`

- [ ] **Step 1: Заменить `case "backspace"` в `routeKey`**

В `ui/tui/app_update.go` найти:

```go
	case "backspace":
		if a.input.HasSelection() {
			a.input.DeleteSelection()
			return a, nil, true
		}
		return a, nil, false
```

Заменить на:

```go
	case "backspace":
		a.input.DeleteBackward()
		return a, nil, true
	case "delete":
		a.input.DeleteForward()
		return a, nil, true
```

- [ ] **Step 2: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```powershell
git add ui/tui/app_update.go
git commit -m "feat(input): Backspace/Delete via DeleteBackward/DeleteForward (selection-aware, fixes B3)"
```

---

## Task 7: `Ctrl+A`, `Ctrl+X`, `Ctrl+V` (fix B4)

**Files:** `ui/tui/app_update.go`

- [ ] **Step 1: Добавить case-блоки в `routeKey` switch**

В `ui/tui/app_update.go` найти `routeKey` switch. Сразу **после** существующего `case "shift+left":` ... `case "alt+shift+right":` блока (закрывается перед `case "up":`) добавить:

```go
	case "ctrl+a":
		a.input.SelectAll()
		return a, nil, true
	case "ctrl+x":
		if a.input.HasSelection() {
			s := a.input.Cut()
			_ = clipboard.WriteAll(s)
			a.showToast("Вырезано")
		}
		return a, nil, true
	case "ctrl+v":
		txt, err := clipboard.ReadAll()
		if err == nil && txt != "" {
			a.input.Paste(txt)
		}
		return a, nil, true
```

- [ ] **Step 2: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```powershell
git add ui/tui/app_update.go
git commit -m "feat(input): Ctrl+A select-all, Ctrl+X cut, Ctrl+V paste (selection-aware, fixes B4)"
```

---

## Task 8: `Shift+Home`, `Shift+End`, `Ctrl+Shift+Home`, `Ctrl+Shift+End`

**Files:** `ui/tui/app_update.go`

- [ ] **Step 1: Добавить case-блоки**

В `ui/tui/app_update.go` найти `routeKey` switch. Сразу **после** `case "ctrl+v":` (добавленного в Task 7) вставить:

```go
	case "shift+home":
		a.input.SelectToLineStart()
		return a, nil, true
	case "shift+end":
		a.input.SelectToLineEnd()
		return a, nil, true
	case "ctrl+shift+home":
		a.input.SelectToDocStart()
		return a, nil, true
	case "ctrl+shift+end":
		a.input.SelectToDocEnd()
		return a, nil, true
```

- [ ] **Step 2: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```powershell
git add ui/tui/app_update.go
git commit -m "feat(input): Shift+Home/End and Ctrl+Shift+Home/End line/doc selection"
```

---

## Task 9: `Ctrl+Home`, `Ctrl+End` (no selection — bare nav to doc start/end)

**Files:** `ui/tui/view/input.go`, `ui/tui/app_update.go`

- [ ] **Step 1: Добавить публичный `MoveCursorAbs` в `Input`**

В `ui/tui/view/input.go` найти существующий `ClearSelection`:

```go
// ClearSelection removes any active selection.
func (in *Input) ClearSelection() { in.selAnchor = -1 }
```

Сразу **после** него добавить:

```go
// MoveCursorAbs positions the cursor at the absolute rune index.
func (in *Input) MoveCursorAbs(pos int) { in.moveCursorAbs(pos) }
```

- [ ] **Step 2: Добавить case-блоки в `routeKey`**

В `ui/tui/app_update.go` в `routeKey` switch сразу **после** `case "ctrl+shift+end":` (Task 8) вставить:

```go
	case "ctrl+home":
		a.input.ClearSelection()
		a.input.MoveCursorAbs(0)
		return a, nil, true
	case "ctrl+end":
		a.input.ClearSelection()
		runes := []rune(a.input.Value())
		a.input.MoveCursorAbs(len(runes))
		return a, nil, true
```

- [ ] **Step 3: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```powershell
git add ui/tui/view/input.go ui/tui/app_update.go
git commit -m "feat(input): Ctrl+Home/End jump to doc start/end (clears selection)"
```

---

## Task 10: `Shift+Up`, `Shift+Down` (vertical selection)

**Files:** `ui/tui/app_update.go`

- [ ] **Step 1: Добавить case-блоки**

В `routeKey` switch сразу **после** `case "ctrl+end":` (Task 9) вставить:

```go
	case "shift+up":
		if !a.input.HasSelection() {
			a.input.SetAnchor(a.input.CursorPos())
		}
		return a, a.sendKeyToTA(tea.KeyUp), true
	case "shift+down":
		if !a.input.HasSelection() {
			a.input.SetAnchor(a.input.CursorPos())
		}
		return a, a.sendKeyToTA(tea.KeyDown), true
```

- [ ] **Step 2: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```powershell
git add ui/tui/app_update.go
git commit -m "feat(input): Shift+Up/Down vertical selection (for multi-line)"
```

---

## Task 11: Esc clears selection first

**Files:** `ui/tui/app_update.go`

- [ ] **Step 1: Добавить selection clear в начало `case "esc"`**

В `ui/tui/app_update.go` найти:

```go
	case "esc":
		if a.mentionActive {
			a.mentionActive = false
			a.layout()
			a.updateStatusHints()
			return a, nil, true
		}
		if a.paletteActive {
```

Заменить начало case на:

```go
	case "esc":
		if a.input.HasSelection() {
			a.input.ClearSelection()
			return a, nil, true
		}
		if a.mentionActive {
			a.mentionActive = false
			a.layout()
			a.updateStatusHints()
			return a, nil, true
		}
		if a.paletteActive {
```

- [ ] **Step 2: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```powershell
git add ui/tui/app_update.go
git commit -m "feat(input): Esc clears selection first (before palette/mention/reset)"
```

---

## Task 12: Mouse tracker fields + double/triple-click

**Files:** `ui/tui/app.go`, `ui/tui/app_update.go`

- [ ] **Step 1: Добавить поля в `App` struct**

В `ui/tui/app.go` найти существующее объявление `mouseDown bool` (используется в drag-обработчике):

```powershell
grep -n "mouseDown" ui/tui/app.go
```

В блоке с `mouseDown`, рядом, добавить:

```go
	mouseLastClickAt  time.Time
	mouseLastClickPos int
	mouseClickCount   int
```

Убедиться, что в импортах `app.go` есть `"time"`:

```powershell
grep -n "\"time\"" ui/tui/app.go
```

Если нет — добавить.

- [ ] **Step 2: Заменить mouse press handler в `app_update.go`**

В `ui/tui/app_update.go` найти существующий блок:

```go
		case m.Button == tea.MouseButtonLeft && m.Action == tea.MouseActionPress:
			if m.Y == a.inputRowY {
				charPos := a.mouseXToAbsolutePos(m.X)
				a.input.ClearSelection()
				a.input.SetAnchor(charPos)
				a.input.SetMouseCaret(charPos)
				a.mouseDown = true
			}
			return a, nil
```

Заменить на:

```go
		case m.Button == tea.MouseButtonLeft && m.Action == tea.MouseActionPress:
			if m.Y == a.inputRowY {
				charPos := a.mouseXToAbsolutePos(m.X)
				now := time.Now()
				absDiff := charPos - a.mouseLastClickPos
				if absDiff < 0 {
					absDiff = -absDiff
				}
				if now.Sub(a.mouseLastClickAt) <= 400*time.Millisecond && absDiff <= 2 {
					a.mouseClickCount++
				} else {
					a.mouseClickCount = 1
				}
				a.mouseLastClickAt = now
				a.mouseLastClickPos = charPos

				switch a.mouseClickCount {
				case 1:
					a.input.ClearSelection()
					a.input.SetAnchor(charPos)
					a.input.SetMouseCaret(charPos)
					a.mouseDown = true
				case 2:
					lo, hi := a.input.WordRange(charPos)
					if lo != hi {
						a.input.SetAnchor(lo)
						a.input.MoveCursorAbs(hi)
					}
				case 3:
					lo, hi := a.input.LineRange(charPos)
					if lo != hi {
						a.input.SetAnchor(lo)
						a.input.MoveCursorAbs(hi)
					}
				}
			}
			return a, nil
```

Убедиться что в импортах `app_update.go` есть `"time"`:

```powershell
grep -n "\"time\"" ui/tui/app_update.go
```

Если нет — добавить в блок import.

- [ ] **Step 3: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```powershell
git add ui/tui/app.go ui/tui/app_update.go
git commit -m "feat(input): mouse double-click word selection, triple-click line selection (400ms window)"
```

---

## Task 13: Shift+click extends selection

**Files:** `ui/tui/app_update.go`

- [ ] **Step 1: Перехватить Shift в mouse press**

В `ui/tui/app_update.go` найти начало case-блока:

```go
		case m.Button == tea.MouseButtonLeft && m.Action == tea.MouseActionPress:
			if m.Y == a.inputRowY {
				charPos := a.mouseXToAbsolutePos(m.X)
				now := time.Now()
```

Заменить начало (до `now := time.Now()`) на:

```go
		case m.Button == tea.MouseButtonLeft && m.Action == tea.MouseActionPress:
			if m.Y == a.inputRowY {
				charPos := a.mouseXToAbsolutePos(m.X)
				if m.Shift {
					a.input.ExtendSelectionTo(charPos)
					a.mouseLastClickAt = time.Time{} // reset double-click chain
					a.mouseClickCount = 0
					return a, nil
				}
				now := time.Now()
```

- [ ] **Step 2: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```powershell
git add ui/tui/app_update.go
git commit -m "feat(input): Shift+click extends current selection to click position"
```

---

## Task 14: Multi-row mouse drag (fix B5)

**Files:** `ui/tui/app_update.go`

- [ ] **Step 1: Расширить `mouseXToAbsolutePos` до multi-row**

В `ui/tui/app_update.go` найти существующую функцию:

```go
// mouseXToAbsolutePos converts a screen X coordinate to an absolute rune index in the input.
// For multi-line text, adds the lengths of lines before the current visible line.
func (a *App) mouseXToAbsolutePos(screenX int) int {
	colOffset := screenX - a.inputColX
	if colOffset < 0 {
		colOffset = 0
	}
	// Account for multi-line: current row offset from LineInfo
	info := a.input.Inner().LineInfo()
	lines := strings.Split(a.input.Value(), "\n")
	absPos := 0
	for i := 0; i < info.RowOffset && i < len(lines); i++ {
		absPos += len([]rune(lines[i])) + 1 // +1 for '\n'
	}
	// Clamp colOffset to current line length
	if info.RowOffset < len(lines) {
		lineLen := len([]rune(lines[info.RowOffset]))
		if colOffset > lineLen {
			colOffset = lineLen
		}
	} else {
		// last line or beyond
		total := len([]rune(a.input.Value()))
		if absPos+colOffset > total {
			colOffset = total - absPos
		}
	}
	if colOffset < 0 {
		colOffset = 0
	}
	return absPos + colOffset
}
```

Заменить на:

```go
// mouseXToAbsolutePos converts a screen X coordinate to an absolute rune
// index in the input, assuming the click is on the first visible row of
// the input box. Kept as a thin wrapper around mouseXYToAbsolutePos for
// existing single-row call sites.
func (a *App) mouseXToAbsolutePos(screenX int) int {
	return a.mouseXYToAbsolutePos(screenX, 0)
}

// mouseXYToAbsolutePos converts (screenX, rowOffset) to an absolute rune
// index. rowOffset is 0 for the topmost input row, 1 for the second, etc.
// Clamps to the bounds of the logical line at that row.
func (a *App) mouseXYToAbsolutePos(screenX, rowOffset int) int {
	colOffset := screenX - a.inputColX
	if colOffset < 0 {
		colOffset = 0
	}
	lines := strings.Split(a.input.Value(), "\n")
	if rowOffset < 0 {
		rowOffset = 0
	}
	if rowOffset >= len(lines) {
		// Beyond last line — clamp to end of value.
		total := len([]rune(a.input.Value()))
		return total
	}
	absPos := 0
	for i := 0; i < rowOffset; i++ {
		absPos += len([]rune(lines[i])) + 1
	}
	lineLen := len([]rune(lines[rowOffset]))
	if colOffset > lineLen {
		colOffset = lineLen
	}
	return absPos + colOffset
}
```

- [ ] **Step 2: Обновить motion handler — учитывать высоту**

В `ui/tui/app_update.go` найти существующий блок motion:

```go
		case m.Action == tea.MouseActionMotion:
			if a.mouseDown && m.Y == a.inputRowY {
				charPos := a.mouseXToAbsolutePos(m.X)
				a.input.SetMouseCaret(charPos)
			}
			return a, nil
```

Заменить на:

```go
		case m.Action == tea.MouseActionMotion:
			if a.mouseDown {
				inputH := a.input.Inner().Height()
				if inputH < 1 {
					inputH = 1
				}
				if m.Y >= a.inputRowY && m.Y < a.inputRowY+inputH {
					rowOff := m.Y - a.inputRowY
					charPos := a.mouseXYToAbsolutePos(m.X, rowOff)
					a.input.SetMouseCaret(charPos)
				} else if m.Y < a.inputRowY {
					// Above input — clamp to position 0 of first row.
					a.input.SetMouseCaret(0)
				} else {
					// Below input — clamp to end of value.
					runes := []rune(a.input.Value())
					a.input.SetMouseCaret(len(runes))
				}
			}
			return a, nil
```

- [ ] **Step 3: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```powershell
git add ui/tui/app_update.go
git commit -m "fix(input): mouse drag works across multi-line input rows (B5)"
```

---

## Task 15: `Shift+Enter` inserts newline

**Files:** `ui/tui/app_update.go`

- [ ] **Step 1: Добавить case-блок**

В `ui/tui/app_update.go` найти в `routeKey` switch:

```go
	case "enter":
		return a.handleEnter()
```

**Перед** этим блоком вставить:

```go
	case "shift+enter":
		a.input.InsertNewline()
		a.input.SyncHeight(5)
		a.layout()
		return a, nil, true
```

- [ ] **Step 2: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```powershell
git add ui/tui/app_update.go
git commit -m "feat(input): Shift+Enter inserts newline (multi-line input)"
```

---

## Task 16: `SyncHeight` after value-changing operations

**Files:** `ui/tui/app_update.go`

Multi-line растёт когда InsertNewline / Paste с `\n` / ReplaceSelection с `\n`. Нужно вызывать `SyncHeight` после каждого такого действия.

- [ ] **Step 1: Добавить SyncHeight вызовы**

В `ui/tui/app_update.go` найти `case "ctrl+v":` (Task 7):

```go
	case "ctrl+v":
		txt, err := clipboard.ReadAll()
		if err == nil && txt != "" {
			a.input.Paste(txt)
		}
		return a, nil, true
```

Заменить на:

```go
	case "ctrl+v":
		txt, err := clipboard.ReadAll()
		if err == nil && txt != "" {
			a.input.Paste(txt)
			a.input.SyncHeight(5)
			a.layout()
		}
		return a, nil, true
```

Также в `case "backspace":` и `case "delete":` (Task 6) — после удаления может пропасть `\n`. Заменить:

```go
	case "backspace":
		a.input.DeleteBackward()
		return a, nil, true
	case "delete":
		a.input.DeleteForward()
		return a, nil, true
```

На:

```go
	case "backspace":
		a.input.DeleteBackward()
		a.input.SyncHeight(5)
		a.layout()
		return a, nil, true
	case "delete":
		a.input.DeleteForward()
		a.input.SyncHeight(5)
		a.layout()
		return a, nil, true
```

И в type-to-replace ветке (Task 5) — replacement может содержать или удалить `\n`. Найти:

```go
	// Type-to-replace: printable input while a selection is active replaces
	// the selection rather than appending alongside it.
	if km, ok := msg.(tea.KeyMsg); ok && a.input.HasSelection() && isPrintableKey(km) {
		a.input.ReplaceSelection(string(km.Runes))
		a.syncPalette()
		a.updateStatusHints()
		a.layout()
		return a, nil
	}
```

Заменить на:

```go
	// Type-to-replace: printable input while a selection is active replaces
	// the selection rather than appending alongside it.
	if km, ok := msg.(tea.KeyMsg); ok && a.input.HasSelection() && isPrintableKey(km) {
		a.input.ReplaceSelection(string(km.Runes))
		a.input.SyncHeight(5)
		a.syncPalette()
		a.updateStatusHints()
		a.layout()
		return a, nil
	}
```

- [ ] **Step 2: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```powershell
git add ui/tui/app_update.go
git commit -m "feat(input): SyncHeight after paste/backspace/delete/type-to-replace (multi-line height adapts)"
```

---

## Task 17: Dynamic `inputBoxHeight` in `app_view.go`

**Files:** `ui/tui/app_view.go`

- [ ] **Step 1: Заменить хардкод 5 на расчёт от ta.Height()**

В `ui/tui/app_view.go` найти:

```go
	// Track input box position for mouse click-to-cursor.
	if !a.showWelcome {
		const inputBoxHeight = 5
		a.inputRowY = a.height - 1 - inputBoxHeight + 1 // textarea content row
		a.inputColX = chatSidePad + 1 + 2               // sidePad + border(▌) + leftPad
	} else {
```

Заменить на:

```go
	// Track input box position for mouse click-to-cursor.
	if !a.showWelcome {
		// Box layout: 1 top border + 1 top pad + ta.Height() + 1 gap + 1 modeLine + 1 bottom pad = 5 + (ta.Height()-1).
		taH := a.input.Inner().Height()
		if taH < 1 {
			taH = 1
		}
		inputBoxHeight := 4 + taH
		a.inputRowY = a.height - 1 - inputBoxHeight + 2 // textarea content row (skip top border + top pad)
		a.inputColX = chatSidePad + 1 + 2                // sidePad + border(▌) + leftPad
	} else {
```

- [ ] **Step 2: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 3: Smoke**

```powershell
go build -o orchestra.exe ./cmd/orchestra
```

Запустить, ввести текст с Shift+Enter — инпут должен вырасти. Скриншот не нужен, проверить визуально.

- [ ] **Step 4: Commit**

```powershell
git add ui/tui/app_view.go
git commit -m "feat(input): inputBoxHeight derives from ta.Height() (multi-line aware)"
```

---

## Task 18: Multi-line `WelcomeRender`

**Files:** `ui/tui/view/input.go`

`WelcomeRender` сейчас рендерит **одну** строку. Для multi-line надо разбить value по `\n` и каждую строку прогнать через тот же overlay-алгоритм (cursor / selection).

- [ ] **Step 1: Переписать `WelcomeRender`**

В `ui/tui/view/input.go` найти существующий `WelcomeRender` (от `func (in Input) WelcomeRender(width int, blinkOn bool) string {` до закрывающей `}`). Заменить **весь** метод на:

```go
// WelcomeRender renders the input rows for the welcome view and chat box
// ourselves, bypassing bubbles textarea.View(). Renders each logical line
// (split by '\n') on its own row with consistent overlay (cursor + selection).
//
//	width    — target visible width of each row (matches box content area)
//	blinkOn  — whether the cursor is currently visible (animation)
func (in Input) WelcomeRender(width int, blinkOn bool) string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	bgStyle := lipgloss.NewStyle().Background(bg)
	textStyle := bgStyle.Foreground(t.Text())
	mutedStyle := bgStyle.Foreground(t.TextMuted())
	cursorStyle := lipgloss.NewStyle().Background(t.Primary()).Foreground(t.Background())
	selStyle := lipgloss.NewStyle().Background(t.BorderNormal()).Foreground(t.Text())
	bar := lipgloss.NewStyle().Background(bg).Foreground(t.Primary()).Bold(true).Render("│")

	val := in.ta.Value()
	totalRunes := len([]rune(val))

	// Empty input — single-line placeholder.
	if val == "" {
		if blinkOn {
			return padLine(bar, width, bgStyle)
		}
		return padLine(mutedStyle.Render("Спроси Orchestra…"), width, bgStyle)
	}

	// Resolve cursor absolute position (mouse-caret override during drag).
	var cursorPos int
	if in.mouseCaretActive {
		cursorPos = clampPos(in.mouseCaret, totalRunes)
	} else {
		cursorPos = clampPos(absolutePos(in.ta), totalRunes)
	}
	selMin, selMax, hasSel := in.SelectionRange()

	lines := strings.Split(val, "\n")
	rendered := make([]string, 0, len(lines))
	absOffset := 0
	for li, line := range lines {
		runes := []rune(line)
		var b strings.Builder
		b.Grow(len(runes) * 20)
		for i, r := range runes {
			absIdx := absOffset + i
			ch := string(r)
			isCursor := blinkOn && absIdx == cursorPos
			isSelected := hasSel && absIdx >= selMin && absIdx < selMax
			switch {
			case isCursor:
				b.WriteString(cursorStyle.Render(ch))
			case isSelected:
				b.WriteString(selStyle.Render(ch))
			default:
				b.WriteString(textStyle.Render(ch))
			}
		}
		// Bar cursor at end of THIS line iff overall cursor sits there.
		endOfLineAbs := absOffset + len(runes)
		if blinkOn && cursorPos == endOfLineAbs && li == len(lines)-1 {
			b.WriteString(bar)
		}
		rendered = append(rendered, padLine(b.String(), width, bgStyle))
		absOffset += len(runes) + 1 // +1 for '\n'
	}

	return strings.Join(rendered, "\n")
}
```

- [ ] **Step 2: Compile**

```powershell
go build ./ui/tui/...
```

Expected: no errors.

- [ ] **Step 3: Smoke**

```powershell
go build -o orchestra.exe ./cmd/orchestra
```

Запустить, набрать «abc», `Shift+Enter`, «def». В инпуте должно появиться 2 строки, курсор на конце второй, выделение `Shift+←` должно работать в пределах строки.

- [ ] **Step 4: Commit**

```powershell
git add ui/tui/view/input.go
git commit -m "feat(input): multi-line WelcomeRender — splits by '\\n' and renders each row with overlay"
```

---

## Task 19: Unit tests `view/input_test.go`

**Files:** `ui/tui/view/input_test.go` (create)

User pref: tests after implementation. Сейчас — закрепляем корректность через unit-тесты.

- [ ] **Step 1: Создать файл с тестами**

Создать `ui/tui/view/input_test.go`:

```go
package view

import (
	"strings"
	"testing"
)

func newTestInput(t *testing.T, value string) *Input {
	t.Helper()
	in := NewInput(80)
	in.SetValue(value)
	// SetValue places cursor at end of value; bring back to start for predictable tests.
	in.MoveCursorAbs(0)
	return &in
}

func TestInput_CursorPos_AfterMoveCursorAbs(t *testing.T) {
	in := newTestInput(t, "hello\nworld")
	in.MoveCursorAbs(7) // 'o' in "world" — pos 7 absolute
	if got := in.CursorPos(); got != 7 {
		t.Fatalf("CursorPos: want 7, got %d", got)
	}
}

func TestInput_DeleteSelection_PositionsCursorOnLo(t *testing.T) {
	in := newTestInput(t, "abcdef")
	in.MoveCursorAbs(1)
	in.SetAnchor(1)
	in.MoveCursorAbs(4) // selection [1, 4) → "bcd"
	if !in.DeleteSelection() {
		t.Fatal("DeleteSelection returned false")
	}
	if got := in.Value(); got != "aef" {
		t.Fatalf("Value: want %q, got %q", "aef", got)
	}
	if got := in.CursorPos(); got != 1 {
		t.Fatalf("CursorPos: want 1, got %d", got)
	}
	if in.HasSelection() {
		t.Fatal("selection should be cleared")
	}
}

func TestInput_ReplaceSelection_PositionsCursorAfterInsert(t *testing.T) {
	in := newTestInput(t, "abcdef")
	in.SetAnchor(1)
	in.MoveCursorAbs(4)
	if !in.ReplaceSelection("XYZ") {
		t.Fatal("ReplaceSelection returned false")
	}
	if got := in.Value(); got != "aXYZef" {
		t.Fatalf("Value: want %q, got %q", "aXYZef", got)
	}
	if got := in.CursorPos(); got != 4 {
		t.Fatalf("CursorPos: want 4 (after 'aXYZ'), got %d", got)
	}
	if in.HasSelection() {
		t.Fatal("selection should be cleared")
	}
}

func TestInput_ReplaceSelection_NoSelection_InsertsAtCursor(t *testing.T) {
	in := newTestInput(t, "abc")
	in.MoveCursorAbs(2)
	if in.ReplaceSelection("X") {
		t.Fatal("ReplaceSelection should return false when no selection")
	}
	if got := in.Value(); got != "abXc" {
		t.Fatalf("Value: want %q, got %q", "abXc", got)
	}
}

func TestInput_SelectAll_Empty_NoOp(t *testing.T) {
	in := newTestInput(t, "")
	in.SelectAll()
	if in.HasSelection() {
		t.Fatal("SelectAll on empty input should not create a selection")
	}
}

func TestInput_SelectAll_NonEmpty(t *testing.T) {
	in := newTestInput(t, "hello")
	in.SelectAll()
	lo, hi, ok := in.SelectionRange()
	if !ok || lo != 0 || hi != 5 {
		t.Fatalf("SelectAll: want [0,5), got [%d,%d) ok=%v", lo, hi, ok)
	}
}

func TestInput_WordRange_Middle(t *testing.T) {
	in := newTestInput(t, "hello world")
	lo, hi := in.WordRange(2)
	if lo != 0 || hi != 5 {
		t.Fatalf("WordRange(2): want [0,5), got [%d,%d)", lo, hi)
	}
}

func TestInput_WordRange_OnSpace(t *testing.T) {
	in := newTestInput(t, "hello world")
	lo, hi := in.WordRange(5) // pos on the space — fallback to left word
	if lo != 0 || hi != 5 {
		t.Fatalf("WordRange(5): want [0,5), got [%d,%d)", lo, hi)
	}
}

func TestInput_WordRange_AtEnd(t *testing.T) {
	in := newTestInput(t, "hello")
	lo, hi := in.WordRange(5)
	if lo != 0 || hi != 5 {
		t.Fatalf("WordRange(5): want [0,5), got [%d,%d)", lo, hi)
	}
}

func TestInput_LineRange_MultiLine(t *testing.T) {
	in := newTestInput(t, "a\nbcd\ne")
	// pos 3 is 'c' in "bcd" — line range [2, 5)
	lo, hi := in.LineRange(3)
	if lo != 2 || hi != 5 {
		t.Fatalf("LineRange(3): want [2,5), got [%d,%d)", lo, hi)
	}
}

func TestInput_Paste_WithSelection_ReplacesIt(t *testing.T) {
	in := newTestInput(t, "abc")
	in.SetAnchor(0)
	in.MoveCursorAbs(3)
	if !in.Paste("XY") {
		t.Fatal("Paste returned false")
	}
	if got := in.Value(); got != "XY" {
		t.Fatalf("Value: want %q, got %q", "XY", got)
	}
}

func TestInput_DeleteForward_NoSelection(t *testing.T) {
	in := newTestInput(t, "abc")
	in.MoveCursorAbs(1)
	in.DeleteForward()
	if got := in.Value(); got != "ac" {
		t.Fatalf("Value: want %q, got %q", "ac", got)
	}
	if got := in.CursorPos(); got != 1 {
		t.Fatalf("CursorPos: want 1, got %d", got)
	}
}

func TestInput_DeleteForward_WithSelection(t *testing.T) {
	in := newTestInput(t, "abcdef")
	in.SetAnchor(1)
	in.MoveCursorAbs(4)
	in.DeleteForward()
	if got := in.Value(); got != "aef" {
		t.Fatalf("Value: want %q, got %q", "aef", got)
	}
}

func TestInput_DeleteBackward_NoSelection(t *testing.T) {
	in := newTestInput(t, "abc")
	in.MoveCursorAbs(2)
	in.DeleteBackward()
	if got := in.Value(); got != "ac" {
		t.Fatalf("Value: want %q, got %q", "ac", got)
	}
	if got := in.CursorPos(); got != 1 {
		t.Fatalf("CursorPos: want 1, got %d", got)
	}
}

func TestInput_InsertNewline_SplitsLine(t *testing.T) {
	in := newTestInput(t, "abc")
	in.MoveCursorAbs(2)
	in.InsertNewline()
	if got := in.Value(); got != "ab\nc" {
		t.Fatalf("Value: want %q, got %q", "ab\nc", got)
	}
}

func TestInput_SyncHeight_Caps(t *testing.T) {
	in := newTestInput(t, "a\nb\nc\nd\ne\nf\ng")
	in.SyncHeight(5)
	if got := in.Inner().Height(); got != 5 {
		t.Fatalf("Height: want 5 (capped), got %d", got)
	}
}

func TestInput_SyncHeight_GrowsToLineCount(t *testing.T) {
	in := newTestInput(t, "a\nb\nc")
	in.SyncHeight(5)
	if got := in.Inner().Height(); got != 3 {
		t.Fatalf("Height: want 3, got %d", got)
	}
}

func TestInput_SelectToLineEnd_FromMiddle(t *testing.T) {
	in := newTestInput(t, "hello\nworld")
	in.MoveCursorAbs(2) // 'l' in "hello"
	in.SelectToLineEnd()
	lo, hi, ok := in.SelectionRange()
	if !ok || lo != 2 || hi != 5 {
		t.Fatalf("SelectionRange: want [2,5), got [%d,%d) ok=%v", lo, hi, ok)
	}
}

func TestInput_SelectToDocStart(t *testing.T) {
	in := newTestInput(t, "hello\nworld")
	in.MoveCursorAbs(8)
	in.SelectToDocStart()
	lo, hi, ok := in.SelectionRange()
	if !ok || lo != 0 || hi != 8 {
		t.Fatalf("SelectionRange: want [0,8), got [%d,%d) ok=%v", lo, hi, ok)
	}
}

func TestInput_Cut_RemovesAndReturns(t *testing.T) {
	in := newTestInput(t, "hello world")
	in.SetAnchor(0)
	in.MoveCursorAbs(5)
	got := in.Cut()
	if got != "hello" {
		t.Fatalf("Cut: want %q, got %q", "hello", got)
	}
	if v := in.Value(); v != " world" {
		t.Fatalf("Value after Cut: want %q, got %q", " world", v)
	}
}

func TestInput_isWordChar(t *testing.T) {
	// isWordChar (used by WordRange) handles unicode letters / digits / underscore.
	if !isWordChar('я') {
		t.Fatal("isWordChar should return true for Cyrillic letter")
	}
	if !isWordChar('_') {
		t.Fatal("isWordChar should return true for underscore")
	}
	if !isWordChar('7') {
		t.Fatal("isWordChar should return true for digit")
	}
	if isWordChar(' ') {
		t.Fatal("isWordChar should return false for space")
	}
}

// Sanity: WelcomeRender with mid-cursor still returns the same number of
// visible rows as logical lines (no extra row from cursor overlay).
func TestInput_WelcomeRender_RowCountMatchesLineCount(t *testing.T) {
	in := newTestInput(t, "ab\ncd\nef")
	out := in.WelcomeRender(20, true)
	rows := strings.Count(out, "\n") + 1
	if rows != 3 {
		t.Fatalf("WelcomeRender rows: want 3, got %d", rows)
	}
}
```

- [ ] **Step 2: Run tests**

```powershell
go test ./ui/tui/view/... -run TestInput -v
```

Expected: all tests pass.

- [ ] **Step 3: Vet**

```powershell
go vet ./...
```

Expected: clean.

- [ ] **Step 4: Commit**

```powershell
git add ui/tui/view/input_test.go
git commit -m "test(input): unit tests for selection/deletion/cursor positioning API"
```

---

## Task 20: Manual smoke checklist + final build

**Files:** none (manual)

- [ ] **Step 1: Final build**

```powershell
go build -o orchestra.exe ./cmd/orchestra
go vet ./...
go test ./ui/tui/...
```

Expected: build OK, vet clean, all tests pass.

- [ ] **Step 2: Запустить TUI и проверить smoke checklist**

Run `./orchestra.exe` and verify:

1. Набрать «abc», выделить Shift+←×3, нажать `x` → текст = «x» (B2 fix)
2. Скопировать в системный clipboard «yyy» (вручную), набрать «abc», выделить всё через Ctrl+A, Ctrl+V → текст = «yyy» (Ctrl+A + Ctrl+V paste-replace)
3. Набрать «abc», Ctrl+A, нажать любую букву → текст заменён одной буквой (Ctrl+A + type-to-replace)
4. Набрать «abc», Ctrl+A, Ctrl+X → инпут пуст, toast «Вырезано», в clipboard «abc»
5. Набрать «abcde», курсор в середине (Home + →×2), выделить Shift+→×2, нажать Delete → удалено выделение, курсор на lo
6. Набрать «abcde», курсор посередине, выделить Shift+→×2, Backspace → удалено выделение, курсор на lo (фикс B1)
7. Набрать «one two», нажать End, потом Shift+Home → выделено всё
8. Набрать «one\ntwo» (через Shift+Enter), курсор в «two», Ctrl+Shift+Home → выделение от курсора до самого начала
9. Набрать «abc», Shift+Enter, «def» → инпут вырос до 2 строк, видно обе
10. Набрать «abc\ndef», Shift+Up при курсоре на «def» → выделение в верх по вертикали
11. Набрать «hello world», двойной клик на «hello» → выделено «hello»
12. Тройной клик на любом слове → выделена логическая строка
13. Кликнуть в начале «hello», Shift+клик в конце «world» → выделение от клика до клика
14. Набрать «abc\ndef», нажать Esc когда есть выделение → выделение пропало, инпут не очистился; повторный Esc → инпут очистился
15. Ctrl+End → курсор в конце value без выделения; Ctrl+Home → в начале

- [ ] **Step 3: Зафиксировать чек-лист в коммите (если smoke прошёл чисто)**

Если все 15 пунктов работают:

```powershell
git commit --allow-empty -m "chore(input): manual smoke checklist passed for phase 2 features"
```

Если что-то не работает — НЕ коммитить пустой коммит, а вернуться в соответствующий task, исправить, перепройти smoke.

---

## Summary

После исполнения плана:
- 5 багов фиксированы (B1, B2, B3, B4, B5)
- 11 новых клавишных шорткатов
- 3 mouse extras (double-click, triple-click, Shift+click)
- Multi-line input через Shift+Enter
- Чистый API `view.Input` (Approach B), все edit-операции в одном месте
- 20+ unit-тестов
- Smoke checklist пройден

Phase 1 plan: [`docs/superpowers/plans/2026-05-12-input-selection-cursor.md`](2026-05-12-input-selection-cursor.md) (можно считать завершённым).
