# Clipboard Copy, Toast, Mouse Selection — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ctrl+C копирует выделение в буфер (не выходит), тост "Скопировано", клик мышью позиционирует курсор, drag мышью выделяет текст, alt+left/right как фоллбек для слово-прыжков при выделении.

**Architecture:** Все изменения в `app.go` (struct + Init), `app_update.go` (mouse/keyboard handlers), `app_view.go` (layout position tracking), `app_welcome.go` (toast render helper). Новая прямая зависимость `github.com/atotto/clipboard`.

**Tech Stack:** Go, charmbracelet/bubbletea v1.3.10, github.com/atotto/clipboard v0.1.4

---

## File Map

| Файл | Изменение |
|------|-----------|
| `go.mod` / `go.sum` | clipboard — из indirect в direct |
| `ui/tui/app.go` | toastText, toastTicks, mouseDown, inputRowY, inputColX в App struct; Init → AllMotion |
| `ui/tui/app_update.go` | Ctrl+C handler, mouse handler, alt+shift+left/right fallback |
| `ui/tui/app_view.go` | layout() вычисляет inputRowY, inputColX |
| `ui/tui/view/app_view.go` | toast render в View() |

---

## Task 1: Clipboard dependency + Ctrl+C → copy or quit

**Files:** `go.mod`, `ui/tui/app_update.go`

- [ ] **Шаг 1: Сделать clipboard прямой зависимостью**

```powershell
cd D:\CursorProjects\Orchestra
go get github.com/atotto/clipboard@v0.1.4
```

- [ ] **Шаг 2: Изменить Ctrl+C handler в `app_update.go`**

Найти в `routeKey`:
```go
// Ctrl+C always quits, regardless of which overlay is on top.
if m.String() == "ctrl+c" {
    return a, tea.Quit, true
}
```

Заменить на:
```go
// Ctrl+C: copy selection to clipboard if active, otherwise quit.
if m.String() == "ctrl+c" {
    if a.input.HasSelection() {
        lo, hi, _ := a.input.SelectionRange()
        runes := []rune(a.input.Value())
        if hi > len(runes) {
            hi = len(runes)
        }
        selected := string(runes[lo:hi])
        _ = clipboard.WriteAll(selected)
        a.input.ClearSelection()
        a.showToast("Скопировано")
        return a, nil, true
    }
    return a, tea.Quit, true
}
```

- [ ] **Шаг 3: Добавить import clipboard в `app_update.go`**

В блок импортов добавить:
```go
"github.com/atotto/clipboard"
```

- [ ] **Шаг 4: Добавить `showToast` метод в `app_update.go`** (или `app.go`)

После функции `sendKeyToTA` добавить:
```go
// showToast displays a temporary notification for ~1.5 seconds (15 ticks at 10fps).
func (a *App) showToast(text string) {
    a.toastText = text
    a.toastTick = 15
}
```

- [ ] **Шаг 5: Добавить поля в App struct (`app.go`)**

В struct `App` добавить (после `chatDirty bool`):
```go
toastText string // non-empty while toast is visible
toastTick int    // countdown ticks until toast clears
```

- [ ] **Шаг 6: Декрементировать toast в tickMsg handler (`app_update.go`)**

В `case tickMsg:` в конце блока перед `return a, tickCmd()` добавить:
```go
if a.toastTick > 0 {
    a.toastTick--
    if a.toastTick == 0 {
        a.toastText = ""
    }
}
```

- [ ] **Шаг 7: Компиляция**

```powershell
go build ./ui/tui/...
```

- [ ] **Шаг 8: Коммит**

```powershell
git add go.mod go.sum ui/tui/app.go ui/tui/app_update.go
git commit -m "feat(input): ctrl+c copies selection to clipboard; toast system"
```

---

## Task 2: Toast render в View()

**Files:** `ui/tui/app_view.go`

Toast — полупрозрачный overlay в верхней части экрана, по центру. Формат:
```
  ╭──────────────╮
  │  Скопировано │
  ╰──────────────╯
```

- [ ] **Шаг 1: Добавить renderToast helper в `app_view.go`**

В конце файла `app_view.go` добавить:
```go
// renderToast renders a centered floating notification box.
func (a *App) renderToast() string {
    if a.toastText == "" {
        return ""
    }
    t := view.ThemeForApp()
    inner := lipgloss.NewStyle().
        Foreground(t.Text()).
        Background(t.BackgroundSecondary()).
        Padding(0, 2).
        Render(a.toastText)
    box := lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(t.Primary()).
        Background(t.BackgroundSecondary()).
        Render(inner)
    return lipgloss.Place(a.width, 3, lipgloss.Center, lipgloss.Top, box)
}
```

- [ ] **Шаг 2: Вставить toast поверх финального вывода в `View()`**

В `app_view.go` найти метод `View()` (он в `app_view.go` или `app.go`). Найти строку где возвращается финальный контент. Перед каждым `return` (кроме тех что возвращают модальные окна) добавить overlay toast.

Найти паттерн `return out` или аналогичный в конце View(). Заменить:
```go
return out
```
на:
```go
if toast := a.renderToast(); toast != "" {
    // Overlay toast at top — replace first 3 lines.
    lines := strings.Split(out, "\n")
    toastLines := strings.Split(toast, "\n")
    for i, tl := range toastLines {
        if i < len(lines) {
            lines[i] = tl
        }
    }
    return strings.Join(lines, "\n")
}
return out
```

Добавить `"strings"` в импорты `app_view.go` если отсутствует.

- [ ] **Шаг 3: Компиляция**

```powershell
go build ./ui/tui/...
```

- [ ] **Шаг 4: Коммит**

```powershell
git add ui/tui/app_view.go
git commit -m "feat(ui): toast overlay render (Скопировано notification)"
```

---

## Task 3: Alt+Shift+←/→ как fallback для word-selection

**Files:** `ui/tui/app_update.go`

Ctrl+Shift+←/→ перехватывается Windows Terminal. Alt+Shift+←/→ работает стабильно.

- [ ] **Шаг 1: Добавить alt+shift+left и alt+shift+right в switch routeKey**

Найти в switch m.String() блок `case "ctrl+shift+left":`. После него добавить:

```go
case "alt+shift+left":
    if !a.input.HasSelection() {
        pos := a.input.Inner().LineInfo().CharOffset
        runes := []rune(a.input.Value())
        if pos < 0 {
            pos = 0
        }
        if pos > len(runes) {
            pos = len(runes)
        }
        a.input.SetAnchor(pos)
    }
    return a, a.sendKeyToTA(tea.KeyCtrlLeft), true

case "alt+shift+right":
    if !a.input.HasSelection() {
        pos := a.input.Inner().LineInfo().CharOffset
        runes := []rune(a.input.Value())
        if pos < 0 {
            pos = 0
        }
        if pos > len(runes) {
            pos = len(runes)
        }
        a.input.SetAnchor(pos)
    }
    return a, a.sendKeyToTA(tea.KeyCtrlRight), true
```

Также добавить `"alt+left"` и `"alt+right"` в список ключей что сбрасывают выделение (inner switch перед `return a, nil, false`):
```go
case "left", "right", "ctrl+left", "ctrl+right", "alt+left", "alt+right", "home", "end", "up", "down":
    a.input.ClearSelection()
```

- [ ] **Шаг 2: Компиляция и коммит**

```powershell
go build ./ui/tui/...
git add ui/tui/app_update.go
git commit -m "feat(input): alt+shift+left/right word-selection fallback for Windows Terminal"
```

---

## Task 4: Mouse click-to-cursor + drag selection

**Files:** `ui/tui/app.go`, `ui/tui/app_view.go`, `ui/tui/app_update.go`

### 4a: Переключить на AllMotion + добавить mouse state в App

- [ ] **Шаг 1: В `app.go` добавить поля в struct**

После `toastTick int` добавить:
```go
// Mouse state for click-to-cursor and drag selection.
inputRowY  int  // absolute screen row of textarea content
inputColX  int  // absolute screen column where textarea content starts
mouseDown  bool // true while left button held in input area
```

- [ ] **Шаг 2: В `app.go` найти `tea.WithMouseCellMotion()` и заменить на `tea.WithMouseAllMotion()`**

Найти:
```go
p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
```
Заменить на:
```go
p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseAllMotion())
```

### 4b: Вычислять inputRowY и inputColX в layout()

- [ ] **Шаг 3: В `app_view.go` в функции `layout()` добавить расчёт позиции**

В конце функции `layout()` добавить:
```go
// Track input box position for mouse click-to-cursor.
// Chat view: input box is at bottom, textarea row = height - statusBar(1) - boxHeight(5) + topPad(1)
if !a.showWelcome {
    const inputBoxHeight = 5
    a.inputRowY = a.height - 1 - inputBoxHeight + 1 // textarea content row
    a.inputColX = chatSidePad + 1 + 2               // sidePad + border(▌) + leftPad
} else {
    // Welcome view: rough estimate — center of screen
    a.inputRowY = a.height / 2
    a.inputColX = (a.width - 80) / 2 + 3 // assumes boxWidth≈80
    if a.inputColX < 3 {
        a.inputColX = 3
    }
}
```

### 4c: Handle mouse events

- [ ] **Шаг 4: Заменить mouse handler в `app_update.go`**

Найти текущий обработчик:
```go
case tea.MouseMsg:
    // Mouse wheel scrolls the chat viewport. Clicks/motion are ignored.
    switch m.Type {
    case tea.MouseWheelUp:
        a.chat.ScrollUp(3)
        return a, nil
    case tea.MouseWheelDown:
        a.chat.ScrollDown(3)
        return a, nil
    }
    return a, nil
```

Заменить на:
```go
case tea.MouseMsg:
    switch m.Type {
    case tea.MouseWheelUp:
        a.chat.ScrollUp(3)
        return a, nil
    case tea.MouseWheelDown:
        a.chat.ScrollDown(3)
        return a, nil
    case tea.MouseLeft:
        // Click in input row → position cursor + start potential drag selection.
        if m.Y == a.inputRowY {
            charPos := a.mouseXToCharPos(m.X)
            a.moveCursorToPos(charPos)
            a.mouseDown = true
            a.input.ClearSelection()
            a.input.SetAnchor(charPos)
        }
        return a, nil
    case tea.MouseRelease:
        if a.mouseDown {
            a.mouseDown = false
            // If anchor == cursor pos, it was just a click (no drag) — clear selection.
            if lo, hi, ok := a.input.SelectionRange(); ok && lo == hi {
                a.input.ClearSelection()
            }
        }
        return a, nil
    case tea.MouseMotion:
        if a.mouseDown && m.Y == a.inputRowY {
            charPos := a.mouseXToCharPos(m.X)
            a.moveCursorToPos(charPos)
        }
        return a, nil
    }
    return a, nil
```

- [ ] **Шаг 5: Добавить helper методы в `app_update.go`**

После `sendKeyToTA` добавить:

```go
// mouseXToCharPos converts a screen X coordinate to a rune index in the input.
func (a *App) mouseXToCharPos(screenX int) int {
    charPos := screenX - a.inputColX
    runes := []rune(a.input.Value())
    if charPos < 0 {
        charPos = 0
    }
    if charPos > len(runes) {
        charPos = len(runes)
    }
    return charPos
}

// moveCursorToPos moves the textarea cursor to the given rune index.
// Strategy: reset value (preserves text) then send Right keys to advance cursor.
// For large texts this is slow; acceptable for typical prompt lengths (<200 chars).
func (a *App) moveCursorToPos(targetPos int) {
    val := a.input.Value()
    a.input.SetValue(val) // resets cursor to end
    // Move left from end to targetPos.
    runes := []rune(val)
    endPos := len(runes)
    steps := endPos - targetPos
    innerTA := a.input.Inner()
    for i := 0; i < steps; i++ {
        updated, _ := innerTA.Update(tea.KeyMsg{Type: tea.KeyLeft})
        *innerTA = updated
    }
}
```

- [ ] **Шаг 6: Компиляция**

```powershell
go build ./ui/tui/...
go build -o orchestra.exe ./cmd/orchestra
```

Если ошибки с `tea.MouseRelease` или `tea.MouseMotion` — проверить правильные константы в bubbletea v1.3.10:
```powershell
grep -r "MouseRelease\|MouseMotion\|MouseLeft" "$(go env GOPATH)\pkg\mod\github.com\charmbracelet\bubbletea*\mouse.go" | Select-Object -First 10
```
И подставить правильные имена констант.

- [ ] **Шаг 7: go vet**

```powershell
go vet ./...
```

- [ ] **Шаг 8: Коммит**

```powershell
git add ui/tui/app.go ui/tui/app_view.go ui/tui/app_update.go
git commit -m "feat(input): mouse click-to-cursor + drag selection; switch to AllMotion"
```
