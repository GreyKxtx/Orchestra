# Slash Palette Restore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Восстановить инлайн слэш-палитру: при вводе `/` в строке ввода TUI показывается список команд над полем ввода, фильтруется по мере набора.

**Architecture:** Единственное изменение — метод `syncPalette()` в `ui/tui/app_palette.go`. Он сейчас принудительно выставляет `paletteActive = false`; нужно добавить проверку на `/`-префикс и вызов `slashPalette.Filter()`. Весь остальной стек (рендер, навигация, Enter/Esc, Ctrl+K модаль) уже работает и не трогается.

**Tech Stack:** Go, charmbracelet/bubbletea, charmbracelet/x/exp/teatest

---

## Files

| Действие | Файл | Что меняется |
|----------|------|-------------|
| Modify | `ui/tui/app_palette.go:20-23` | Логика `syncPalette()` |
| Modify | `ui/tui/app_test.go` | +2 teatest-теста на palette |

---

### Task 1: Написать падающие тесты

**Files:**
- Modify: `ui/tui/app_test.go`

- [ ] **Step 1.1: Добавить два теста в конец `app_test.go`** (перед закрывающей `}` файла нет, просто добавляем функции)

  Добавить ПОСЛЕ последней функции `readAll` в `ui/tui/app_test.go`:

  ```go
  func TestApp_SlashPaletteOpensOnSlash(t *testing.T) {
  	app, err := tui.NewApp(tui.Config{})
  	if err != nil {
  		t.Fatal(err)
  	}
  	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

  	// Typing "/" should open the inline slash palette.
  	tm.Type("/")

  	// The palette renders command names above the input box.
  	// At minimum /help and /clear must appear in the rendered output.
  	teatest.WaitFor(
  		t, tm.Output(),
  		func(b []byte) bool {
  			s := string(b)
  			return strings.Contains(s, "/help") && strings.Contains(s, "/clear")
  		},
  		teatest.WithCheckInterval(50*time.Millisecond),
  		teatest.WithDuration(2*time.Second),
  	)

  	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
  	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
  }

  func TestApp_SlashPaletteFiltersOnType(t *testing.T) {
  	app, err := tui.NewApp(tui.Config{})
  	if err != nil {
  		t.Fatal(err)
  	}
  	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

  	// Typing "/cl" should filter to /clear only (not /quit, /help, etc.)
  	tm.Type("/cl")

  	teatest.WaitFor(
  		t, tm.Output(),
  		func(b []byte) bool {
  			return bytes.Contains(b, []byte("/clear"))
  		},
  		teatest.WithCheckInterval(50*time.Millisecond),
  		teatest.WithDuration(2*time.Second),
  	)

  	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
  	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
  }
  ```

  func TestApp_SlashPaletteClosesOnSpace(t *testing.T) {
  	app, err := tui.NewApp(tui.Config{Model: "test-model", Mode: "build", CWD: "test"})
  	if err != nil {
  		t.Fatal(err)
  	}
  	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

  	// Type "/clear " (with trailing space) — palette should close.
  	// Then Enter should send "/clear" as a regular message (not execute the command).
  	// Echo mode returns "echo: /clear" proving the palette was NOT active on Enter.
  	tm.Type("/clear ")
  	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

  	teatest.WaitFor(
  		t, tm.Output(),
  		func(b []byte) bool {
  			return bytes.Contains(b, []byte("echo: /clear"))
  		},
  		teatest.WithCheckInterval(50*time.Millisecond),
  		teatest.WithDuration(2*time.Second),
  	)

  	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
  	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
  }
  ```

  Все три теста используют импорты `bytes`, `strings`, `time`, `tea`, `teatest`, `tui` — они уже присутствуют в файле.

- [ ] **Step 1.2: Убедиться, что тесты падают**

  ```powershell
  go test ./ui/tui/... -run "TestApp_SlashPaletteOpensOnSlash|TestApp_SlashPaletteFiltersOnType|TestApp_SlashPaletteClosesOnSpace" -v -timeout 30s
  ```

  Ожидаемый результат: первые два теста **FAIL** с `WaitFor timeout`; третий может **PASS** или **FAIL** (в зависимости от текущего поведения Enter с пустым value). Важно: именно первые два падают — это и есть баг.

---

### Task 2: Реализовать фикс

**Files:**
- Modify: `ui/tui/app_palette.go:20-23`

- [ ] **Step 2.1: Заменить тело `syncPalette()`**

  Найти в `ui/tui/app_palette.go` строки 20–23:

  ```go
  // syncPalette refreshes the slash-palette and mention-palette state to match
  // the current input value. The legacy inline slash palette is now disabled
  // (Ctrl+K is the canonical command surface); only mention completion remains.
  func (a *App) syncPalette() {
  	a.paletteActive = false
  	a.syncMention()
  }
  ```

  Заменить на:

  ```go
  // syncPalette refreshes the slash-palette and mention-palette state to match
  // the current input value. When the input starts with "/" and contains no
  // space, the slash palette is shown above the input box (filtered in real
  // time). Typing "/" alone shows all commands; "/cl" narrows to /clear, etc.
  // A space after the slash prefix closes the palette (user is typing a message).
  func (a *App) syncPalette() {
  	val := a.input.Value()
  	if strings.HasPrefix(val, "/") && !strings.Contains(val, " ") {
  		query := val[1:] // text after the leading "/"
  		a.slashPalette.Filter(query)
  		a.paletteActive = len(a.slashPalette.Items) > 0
  	} else {
  		a.paletteActive = false
  	}
  	a.syncMention()
  }
  ```

  Убедиться, что `"strings"` уже импортирован в файле (он уже есть — строка 7 `import "strings"`).

---

### Task 3: Прогнать тесты и закоммитить

- [ ] **Step 3.1: Прогнать только новые тесты — убедиться что PASS**

  ```powershell
  go test ./ui/tui/... -run "TestApp_SlashPaletteOpensOnSlash|TestApp_SlashPaletteFiltersOnType|TestApp_SlashPaletteClosesOnSpace" -v -timeout 30s
  ```

  Ожидаемый результат: все три **PASS**.

- [ ] **Step 3.2: Прогнать весь пакет tui — убедиться, что ничего не сломалось**

  ```powershell
  go test ./ui/tui/... -v -timeout 60s
  ```

  Ожидаемый результат: все тесты **PASS**.

- [ ] **Step 3.3: Прогнать весь репозиторий**

  ```powershell
  go test ./... -timeout 120s
  ```

  Ожидаемый результат: все тесты **PASS**.

- [ ] **Step 3.4: Закоммитить**

  ```powershell
  git add ui/tui/app_palette.go ui/tui/app_test.go
  git commit -m "feat(tui): restore inline slash palette on / input"
  ```
