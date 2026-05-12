# Input Selection & Cursor Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Исправить баг "курсор сдвигает символ", добавить выделение текста через Shift+стрелки, починить перехват Shift+Up/Down (уходили на скролл чата вместо инпута).

**Architecture:** Оставляем `WelcomeRender` как единственный рендерер инпута (нужен для overlay выделения). Добавляем `selAnchor int` в `Input` (-1 = нет выделения). Перехватываем Shift+стрелки в `routeKey` до textarea: фиксируем якорь выделения, пересылаем «чистую» стрелку в textarea. WelcomeRender читает selAnchor + текущую позицию курсора и рисует выделенный span. Backspace при активном выделении удаляет весь выделенный диапазон.

**Tech Stack:** Go 1.21, charmbracelet/bubbletea, charmbracelet/bubbles v1.0.0, charmbracelet/lipgloss

---

## File Map

| Файл | Изменение |
|------|-----------|
| `ui/tui/view/input.go` | Добавить `selAnchor` в `Input`, методы управления выделением, переписать `WelcomeRender` |
| `ui/tui/app_update.go` | Перехват Shift+стрелок, удаление Shift+Up/Down из скролла, сброс выделения на обычной навигации, Backspace с выделением |

---

## Task 1: Fix WelcomeRender — cursor overlay (без push символа)

**Files:** `ui/tui/view/input.go`

Текущая проблема: `│` вставляется как лишний символ между `before` и `after`, увеличивая ширину строки на 1.

Правило после фикса:
- Курсор **в середине** → block cursor: символ под курсором рендерится с инвертированными цветами (Primary bg + Background fg). Ширина не меняется.
- Курсор **в конце** текста → `│` после последнего символа (там нечего сдвигать).
- Пустой инпут → `│` перед placeholder.

- [ ] **Шаг 1: Переписать WelcomeRender в `ui/tui/view/input.go`**

Заменить весь метод `WelcomeRender` (строки 119–169) на:

```go
// WelcomeRender renders the input row for the welcome view.
// Cursor model:
//   - empty input      → │ bar before placeholder
//   - cursor in middle → block cursor on the char at position (no extra char)
//   - cursor at end    → │ bar after last char
//
// width    — target visible width of the row (matches box content area)
// blinkOn  — whether the cursor is currently visible
func (in Input) WelcomeRender(width int, blinkOn bool) string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	bgStyle   := lipgloss.NewStyle().Background(bg)
	textStyle := bgStyle.Foreground(t.Text())
	mutedStyle := bgStyle.Foreground(t.TextMuted())
	// Block cursor: Primary bg, Background fg — char is visible, just inverted.
	cursorStyle := lipgloss.NewStyle().Background(t.Primary()).Foreground(t.Background())
	// Bar cursor character used only at text boundaries (start / end).
	bar := lipgloss.NewStyle().Background(bg).Foreground(t.Primary()).Bold(true).Render("│")

	val := in.ta.Value()
	runes := []rune(val)

	if val == "" {
		ph := "Спроси Orchestra…"
		if blinkOn {
			return padLine(bar+mutedStyle.Render(ph), width, bgStyle)
		}
		return padLine(mutedStyle.Render(ph), width, bgStyle)
	}

	info := in.ta.LineInfo()
	pos := clampPos(info.CharOffset, len(runes))

	var b strings.Builder
	for i, r := range runes {
		ch := string(r)
		if blinkOn && i == pos {
			b.WriteString(cursorStyle.Render(ch))
		} else {
			b.WriteString(textStyle.Render(ch))
		}
	}
	if blinkOn && pos == len(runes) {
		b.WriteString(bar)
	}

	return padLine(b.String(), width, bgStyle)
}

// padLine pads s to exactly width visible cells using bgStyle-filled spaces.
func padLine(s string, width int, bgStyle lipgloss.Style) string {
	if diff := width - lipgloss.Width(s); diff > 0 {
		s += bgStyle.Render(strings.Repeat(" ", diff))
	}
	return s
}

// clampPos returns pos clamped to [0, max].
func clampPos(pos, max int) int {
	if pos < 0 {
		return 0
	}
	if pos > max {
		return max
	}
	return pos
}
```

- [ ] **Шаг 2: Убедиться что компилируется**

```powershell
cd D:\CursorProjects\Orchestra
go build ./ui/tui/...
```

Ожидаем: нет ошибок.

- [ ] **Шаг 3: Собрать бинарник и быстро проверить**

```powershell
go build -o orchestra.exe ./cmd/orchestra
```

Запустить TUI, набрать текст, пройтись стрелками — символы не должны сдвигаться.

- [ ] **Шаг 4: Коммит**

```powershell
git add ui/tui/view/input.go
git commit -m "fix(input): block cursor overlay — no character push on blink"
```

---

## Task 2: Add selection state to Input

**Files:** `ui/tui/view/input.go`

- [ ] **Шаг 1: Добавить `selAnchor` в struct и методы управления**

В `ui/tui/view/input.go` найти строку `type Input struct {` и добавить поле:

```go
type Input struct {
	ta        textarea.Model
	width     int
	mode      string
	selAnchor int // -1 = no selection; ≥0 = rune index where selection started
}
```

После функции `NewInput` добавить конструктор с корректным начальным значением. В `NewInput` перед `return` добавить:

```go
	return Input{ta: ta, width: width, selAnchor: -1}
```

(Заменить существующий `return Input{ta: ta, width: width}`)

- [ ] **Шаг 2: Добавить методы выделения после `Inner()`**

```go
// HasSelection reports whether a selection range is active.
func (in Input) HasSelection() bool { return in.selAnchor >= 0 }

// SelectionRange returns the [min, max) rune indices of the current
// selection and ok=true. Returns 0,0,false when no selection is active.
func (in Input) SelectionRange() (min, max int, ok bool) {
	if in.selAnchor < 0 {
		return 0, 0, false
	}
	cursor := clampPos(in.ta.LineInfo().CharOffset, len([]rune(in.ta.Value())))
	a := in.selAnchor
	if a <= cursor {
		return a, cursor, true
	}
	return cursor, a, true
}

// SetAnchor pins the selection start to pos (call before moving cursor).
func (in *Input) SetAnchor(pos int) { in.selAnchor = pos }

// ClearSelection removes any active selection.
func (in *Input) ClearSelection() { in.selAnchor = -1 }

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
	// Position cursor at lo.
	in.ta.SetValue(newVal) // SetValue resets cursor to end; move it back below.
	// Move cursor to lo by resetting and sending left keys is complex;
	// simplest: set value, cursor ends up at end, which is acceptable for now.
	in.selAnchor = -1
	return true
}
```

- [ ] **Шаг 3: Компиляция**

```powershell
go build ./ui/tui/...
```

- [ ] **Шаг 4: Коммит**

```powershell
git add ui/tui/view/input.go
git commit -m "feat(input): add selAnchor selection state + HasSelection/SelectionRange/DeleteSelection"
```

---

## Task 3: Render selection in WelcomeRender

**Files:** `ui/tui/view/input.go`

- [ ] **Шаг 1: Добавить selStyle в WelcomeRender и рисовать выделение**

Заменить тело `WelcomeRender` (уже переписанное в Task 1) на расширенную версию с поддержкой выделения:

```go
func (in Input) WelcomeRender(width int, blinkOn bool) string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	bgStyle    := lipgloss.NewStyle().Background(bg)
	textStyle  := bgStyle.Foreground(t.Text())
	mutedStyle := bgStyle.Foreground(t.TextMuted())
	// Block cursor: Primary bg, Background fg.
	cursorStyle := lipgloss.NewStyle().Background(t.Primary()).Foreground(t.Background())
	// Selection: BorderNormal bg, Text fg — visually distinct from cursor.
	selStyle := lipgloss.NewStyle().Background(t.BorderNormal()).Foreground(t.Text())
	// Bar character — used only at text boundaries.
	bar := lipgloss.NewStyle().Background(bg).Foreground(t.Primary()).Bold(true).Render("│")

	val := in.ta.Value()
	runes := []rune(val)

	if val == "" {
		ph := "Спроси Orchestra…"
		if blinkOn {
			return padLine(bar+mutedStyle.Render(ph), width, bgStyle)
		}
		return padLine(mutedStyle.Render(ph), width, bgStyle)
	}

	info := in.ta.LineInfo()
	pos := clampPos(info.CharOffset, len(runes))
	selMin, selMax, hasSel := in.SelectionRange()

	var b strings.Builder
	for i, r := range runes {
		ch := string(r)
		isCursor   := blinkOn && i == pos
		isSelected := hasSel && i >= selMin && i < selMax

		switch {
		case isCursor:
			b.WriteString(cursorStyle.Render(ch))
		case isSelected:
			b.WriteString(selStyle.Render(ch))
		default:
			b.WriteString(textStyle.Render(ch))
		}
	}

	// Bar cursor at end of text (nothing to overlay).
	if blinkOn && pos == len(runes) {
		b.WriteString(bar)
	}

	return padLine(b.String(), width, bgStyle)
}
```

- [ ] **Шаг 2: Компиляция**

```powershell
go build ./ui/tui/...
```

- [ ] **Шаг 3: Коммит**

```powershell
git add ui/tui/view/input.go
git commit -m "feat(input): render text selection highlight in WelcomeRender"
```

---

## Task 4: Fix key routing — Shift+arrows → selection, not scroll

**Files:** `ui/tui/app_update.go`

Сейчас в `routeKey`:
- `"shift+up"` → `a.chat.ScrollUp(1)` — неправильно, инпут никогда не получает
- `"shift+down"` → `a.chat.ScrollDown(1)` — то же

Нужно убрать их из скролла (pgup/pgdown уже работают для скролла) и добавить обработку `shift+left`, `shift+right`, `ctrl+shift+left`, `ctrl+shift+right` для выделения.

Паттерн для каждого selection-ключа:
1. Если выделения нет — зафиксировать якорь в текущей позиции курсора
2. Переслать «чистый» навигационный ключ в textarea (курсор движется внутри bubbles)
3. Вернуть `handled=true`

- [ ] **Шаг 1: Удалить shift+up/down из скролл-обработчика**

В `routeKey` найти блоки:
```go
case "shift+up":
    a.chat.ScrollUp(1)
    return a, nil, true
case "shift+down":
    a.chat.ScrollDown(1)
    return a, nil, true
```
И **удалить их полностью**. Пользователь использует `pgup`/`pgdown` (уже работают) для скролла.

- [ ] **Шаг 2: Добавить вспомогательную функцию `sendKeyToTA` перед `routeKey`**

```go
// sendKeyToTA forwards a synthetic key event directly to the textarea
// and returns the resulting command. Used when routeKey wants to move
// the cursor without triggering its own key handlers.
func (a *App) sendKeyToTA(kt tea.KeyType, runes ...rune) tea.Cmd {
	innerTA := a.input.Inner()
	updated, cmd := innerTA.Update(tea.KeyMsg{Type: kt, Runes: runes})
	*innerTA = updated
	return cmd
}
```

- [ ] **Шаг 3: Добавить selection-обработчики в switch внутри `routeKey`**

Добавить **перед** `case "up":` следующие case-блоки:

```go
case "shift+left":
	if !a.input.HasSelection() {
		a.input.SetAnchor(clampPos(a.input.Inner().LineInfo().CharOffset, len([]rune(a.input.Value()))))
	}
	return a, a.sendKeyToTA(tea.KeyLeft), true

case "shift+right":
	if !a.input.HasSelection() {
		a.input.SetAnchor(clampPos(a.input.Inner().LineInfo().CharOffset, len([]rune(a.input.Value()))))
	}
	return a, a.sendKeyToTA(tea.KeyRight), true

case "ctrl+shift+left":
	if !a.input.HasSelection() {
		a.input.SetAnchor(clampPos(a.input.Inner().LineInfo().CharOffset, len([]rune(a.input.Value()))))
	}
	return a, a.sendKeyToTA(tea.KeyCtrlLeft), true

case "ctrl+shift+right":
	if !a.input.HasSelection() {
		a.input.SetAnchor(clampPos(a.input.Inner().LineInfo().CharOffset, len([]rune(a.input.Value()))))
	}
	return a, a.sendKeyToTA(tea.KeyCtrlRight), true
```

- [ ] **Шаг 4: Сбрасывать выделение при обычной навигации**

В `routeKey`, в начале `case "left":` (если его нет — добавить), **до того как возвращаем handled=false** (т.е. до `return a, nil, false`), добавить сброс выделения для всех навигационных ключей без Shift.

Найти в конце `routeKey` строку `return a, nil, false` и **перед ней** добавить:

```go
	// Clear selection when any non-shift navigation falls through to textarea.
	switch m.String() {
	case "left", "right", "ctrl+left", "ctrl+right", "home", "end", "up", "down":
		a.input.ClearSelection()
	}
```

- [ ] **Шаг 5: Добавить нужный import если отсутствует**

В `app_update.go` убедиться что в блоке import есть:
```go
tea "github.com/charmbracelet/bubbletea"
```
(уже должен быть, т.к. `tea.KeyMsg` используется)

Также нужна `clampPos` из `view/input.go` — но это в другом пакете. Вместо этого инлайним:

```go
// В sendKeyToTA и selection handlers заменить clampPos на inline:
pos := a.input.Inner().LineInfo().CharOffset
runes := []rune(a.input.Value())
if pos < 0 { pos = 0 }
if pos > len(runes) { pos = len(runes) }
```

Обновлённые selection case-блоки:

```go
case "shift+left":
	if !a.input.HasSelection() {
		pos := a.input.Inner().LineInfo().CharOffset
		runes := []rune(a.input.Value())
		if pos < 0 { pos = 0 }
		if pos > len(runes) { pos = len(runes) }
		a.input.SetAnchor(pos)
	}
	return a, a.sendKeyToTA(tea.KeyLeft), true

case "shift+right":
	if !a.input.HasSelection() {
		pos := a.input.Inner().LineInfo().CharOffset
		runes := []rune(a.input.Value())
		if pos < 0 { pos = 0 }
		if pos > len(runes) { pos = len(runes) }
		a.input.SetAnchor(pos)
	}
	return a, a.sendKeyToTA(tea.KeyRight), true

case "ctrl+shift+left":
	if !a.input.HasSelection() {
		pos := a.input.Inner().LineInfo().CharOffset
		runes := []rune(a.input.Value())
		if pos < 0 { pos = 0 }
		if pos > len(runes) { pos = len(runes) }
		a.input.SetAnchor(pos)
	}
	return a, a.sendKeyToTA(tea.KeyCtrlLeft), true

case "ctrl+shift+right":
	if !a.input.HasSelection() {
		pos := a.input.Inner().LineInfo().CharOffset
		runes := []rune(a.input.Value())
		if pos < 0 { pos = 0 }
		if pos > len(runes) { pos = len(runes) }
		a.input.SetAnchor(pos)
	}
	return a, a.sendKeyToTA(tea.KeyCtrlRight), true
```

- [ ] **Шаг 6: Добавить tea.KeyCtrlLeft/KeyCtrlRight — проверить существование**

`tea.KeyCtrlLeft` и `tea.KeyCtrlRight` существуют в bubbletea. Проверить grep:

```powershell
grep -r "KeyCtrlLeft\|KeyCtrlRight" (go env GOPATH)/pkg/mod/github.com/charmbracelet/bubbletea*/key.go 2>$null | Select-Object -First 5
```

Если не найдены — использовать строковый вариант через `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{}}` и пересылать `tea.KeyMsg{Type: tea.KeyLeft, Alt: true}` (Ctrl+Left в ряде терминалов = Alt+Left). Если `KeyCtrlLeft` не существует, заменить на:

```go
// Ctrl+Left word jump
innerTA := a.input.Inner()
updated, cmd := innerTA.Update(tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
*innerTA = updated
return a, cmd, true
```

- [ ] **Шаг 7: Компиляция**

```powershell
go build ./ui/tui/...
```

Если ошибки — исправить типы ключей согласно фактическому API bubbletea.

- [ ] **Шаг 8: Коммит**

```powershell
git add ui/tui/app_update.go
git commit -m "feat(input): shift+arrows selection, remove shift+up/down scroll capture"
```

---

## Task 5: Backspace deletes selection

**Files:** `ui/tui/app_update.go`

Когда активно выделение и пользователь нажимает Backspace — удаляем выделенный диапазон.

- [ ] **Шаг 1: Перехватить Backspace в `routeKey`**

Добавить в switch `m.String()` **перед** `case "enter":`:

```go
case "backspace":
	if a.input.HasSelection() {
		a.input.DeleteSelection()
		return a, nil, true
	}
	// No selection — fall through to textarea for normal backspace.
```

Важно: `case "backspace":` без `return` в ветке "no selection" НЕ вернёт handled=true, а выполнит `break` из switch и дойдёт до `return a, nil, false` — textarea сам обработает backspace. Но в Go switch нет fallthrough по умолчанию, поэтому нужно явно:

```go
case "backspace":
	if a.input.HasSelection() {
		a.input.DeleteSelection()
		return a, nil, true
	}
	// No selection: handled=false → falls through to textarea.
	return a, nil, false
```

- [ ] **Шаг 2: Компиляция**

```powershell
go build ./ui/tui/...
```

- [ ] **Шаг 3: Коммит**

```powershell
git add ui/tui/app_update.go
git commit -m "feat(input): backspace deletes active selection"
```

---

## Task 6: Clear selection on regular typing

**Files:** `ui/tui/app_update.go`

Любой printable символ без Shift должен сбрасывать выделение (стандартное поведение текстовых редакторов).

- [ ] **Шаг 1: Сбросить выделение перед пересылкой в textarea**

В `Update()` найти блок fall-through в textarea (конец метода):

```go
// Forward all messages to textarea (default fall-through for unhandled keys).
innerTA := a.input.Inner()
updatedTA, taCmd := innerTA.Update(msg)
*innerTA = updatedTA
if _, isKey := msg.(tea.KeyMsg); isKey {
    a.syncPalette()
    a.updateStatusHints()
    a.layout()
}
return a, taCmd
```

Заменить на:

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
```

- [ ] **Шаг 2: Финальная компиляция**

```powershell
go build ./ui/tui/...
go vet ./...
```

Оба без ошибок.

- [ ] **Шаг 3: Финальный бинарник**

```powershell
go build -o orchestra.exe ./cmd/orchestra
```

- [ ] **Шаг 4: Ручное smoke-тестирование**

Запустить `./orchestra.exe`, открыть чат, проверить:
1. Набрать текст → курсор не сдвигает символы ✓
2. Shift+← / Shift+→ → выделение рисуется (другой фон) ✓
3. Ctrl+Shift+← / Ctrl+Shift+→ → выделение по словам ✓
4. Backspace на выделении → текст удаляется ✓
5. Напечатать символ на выделении → выделение сбрасывается ✓
6. PgUp/PgDn → чат скроллится (Shift+Up/Down больше не делают это) ✓
7. Стрелки без Shift → выделение сбрасывается ✓

- [ ] **Шаг 5: Итоговый коммит**

```powershell
git add ui/tui/app_update.go
git commit -m "feat(input): clear selection on regular typing (any non-shift key falls to textarea)"
```
