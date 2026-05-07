# TUI Phase 1 — Skeleton (no core connection)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to execute this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Создать `ui/tui/` пакет и команду `orchestra tui`, которая открывает интерактивный Bubble Tea-UI с раскладкой как у Claude Code (header, scrollable viewport, multiline textarea, footer hints). Echo-режим: пишешь в input → появляется в ленте. Никакого подключения к ядру (это Фаза 2).

**Architecture:** Стандартная Bubble Tea Elm-architecture: одна корневая `Model`, `Update(msg) → (Model, Cmd)`, `View() → string`. Декомпозиция по экранным зонам — каждая зона своя под-модель в `ui/tui/view/`. Layout рассчитывается в `View()` через Lipgloss. Resize обрабатывается в `Update`.

**Tech Stack:**
- `github.com/charmbracelet/bubbletea v1.3.10` — event loop (уже в go.mod, indirect)
- `github.com/charmbracelet/lipgloss v1.1.0` — стили (уже)
- `github.com/charmbracelet/bubbles v1.0.0` — `textarea`, `viewport` (уже)

**Reference:** [`docs/superpowers/specs/2026-05-07-tui-design.md`](../specs/2026-05-07-tui-design.md), секция «Раскладка экрана».

## Workflow Note

User preference (`memory/user_prefs.md`): сначала вся реализация, потом тесты. Tasks 1-4 — только реализация и `go build ./...`. Task 5 — единый test pass (где `teatest` snapshots для ключевых флоу). Task 6 — polish (README + cleanup).

## Pre-existing context

В `internal/tui/model_picker.go` уже есть `package tui` который использует Bubble Tea + Lipgloss для model-picker (используется из `internal/cli/model.go`). Это **не наш TUI** — другая утилита, другой purpose. Оставляем её на месте, новый TUI живёт в `ui/tui/` (другой import path: `github.com/orchestra/orchestra/ui/tui`). Никакого конфликта пакетов.

`cmd/orchestra/main.go` просто зовёт `cli.Execute()`. Все subcommands регистрируются в `internal/cli/` (cobra). Новый subcommand `tui` добавляется аналогично существующим (`apply.go`, `core.go`, `chat.go`).

---

## File Structure

| Файл | Создаётся | Ответственность |
|---|---|---|
| `ui/README.md` | Create | Зонтичный README: какие клиенты есть, как добавить новый |
| `ui/tui/README.md` | Create | Описание TUI клиента — как запустить, какие клавиши |
| `ui/tui/app.go` | Create | Корневая Bubble Tea Model, Init/Update/View, агрегирует под-модели |
| `ui/tui/state/session.go` | Create | Локальное состояние сессии (history of messages) — пока для echo, в Фазе 2 расширим |
| `ui/tui/view/header.go` | Create | Однострочный header: "Orchestra · model · mode · cwd" |
| `ui/tui/view/chat.go` | Create | Прокручиваемая лента сообщений (Bubbles `viewport`) |
| `ui/tui/view/input.go` | Create | Multiline `textarea` с авто-resize до 6 строк |
| `ui/tui/view/footer.go` | Create | Footer hints — статичный набор: `↑↓ history · Enter send · Shift+Enter newline · Ctrl+C quit` |
| `ui/vscode/README.md` | Create | Заглушка: «реализация в Этапе 2 product roadmap» |
| `ui/desktop/README.md` | Create | Заглушка: «реализация в Этапе 3 product roadmap» |
| `internal/cli/tui.go` | Create | cobra wrapper для команды `orchestra tui` (по аналогии с `apply.go`, `core.go`) |
| `internal/cli/cli.go` или эквивалент | Modify | Зарегистрировать команду `tui` в root cobra command |
| `ui/tui/app_test.go` | Create (Task 5) | teatest snapshot tests |

---

## Task 1: Create ui/ umbrella + placeholder READMEs

**Files (all Create):**
- `ui/README.md`
- `ui/vscode/README.md`
- `ui/desktop/README.md`

This task lays the empty zonts so subsequent tasks have a place to put files. No Go code yet.

- [ ] **Step 1: Create ui/README.md**

```markdown
# Orchestra UI clients

Все клиенты ядра `orchestra core` живут здесь. Каждый — отдельный subdirectory с собственным README.

| Каталог | Стек | Статус |
|---|---|---|
| `tui/` | Go + Bubble Tea | в разработке (Фаза 1) |
| `vscode/` | TypeScript / Node | планируется (Этап 2 product roadmap) |
| `desktop/` | TBD (Tauri или Electron) | планируется (Этап 3 product roadmap) |

## Принципы

- Каждый клиент общается с ядром через JSON-RPC stdio (subprocess `orchestra core`).
- Не дублируем бизнес-логику ядра в клиентах. Клиент = только UI + транспорт.
- Все клиенты опираются на единый `internal/protocol` для типов.

## Добавление нового клиента

1. Создать `ui/<name>/` с собственным README
2. Если клиент на Go — может импортировать `internal/protocol`, `internal/jsonrpc`
3. Если на другом языке — генерировать DTO из `docs/PROTOCOL.md` и переиспользовать схему версионирования
```

- [ ] **Step 2: Create ui/vscode/README.md**

```markdown
# VS Code Extension (планируется)

Реализация — Этап 2 product roadmap, после стабилизации TUI.

Транспорт: subprocess `orchestra core --workspace-root .` через stdio JSON-RPC (тот же, что использует TUI).

Стек: TypeScript + Node.js. Управление через VS Code Extension API.

Когда начнём: после того как `ui/tui/` пройдёт acceptance и UX-концепции стабилизируются.
```

- [ ] **Step 3: Create ui/desktop/README.md**

```markdown
# Desktop App (планируется)

Реализация — Этап 3 product roadmap, после VS Code extension.

Стек: TBD (рассматриваются Tauri и Electron).

Полный контроль над UX, нативная встройка CKG-визуализации.

Когда начнём: после VS Code extension.
```

- [ ] **Step 4: Verify**

```
ls ui/
```
Expected: `README.md  desktop/  tui/  vscode/` (only `tui/` will be empty until Task 2; the other two contain README.md only).

Wait — `tui/` doesn't exist yet at this point. Only the three READMEs and the parent directory. That's fine.

Actually create `ui/tui/README.md` here too as a stub (will be filled in Task 6):

```markdown
# Orchestra TUI

Terminal UI для Orchestra на Bubble Tea. Запуск: `orchestra tui`.

Подробное описание появится после завершения Фазы 1.
```

- [ ] **Step 5: Commit**

```bash
git add ui/
git commit -m "$(cat <<'EOF'
docs(ui): scaffold ui/ umbrella with placeholder READMEs for tui/vscode/desktop

ui/ is the umbrella for all core clients (TUI, VS Code ext, desktop).
Each subdirectory has its own README; tui/ will be filled in by
subsequent Phase 1 tasks; vscode/ and desktop/ stay as placeholders
until Stages 2 and 3 of the product roadmap.

Part of TUI Phase 1 (skeleton).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Create ui/tui/ skeleton — package compiles, no UI logic yet

**Files (all Create):**
- `ui/tui/app.go`
- `ui/tui/state/session.go`
- `ui/tui/view/header.go`
- `ui/tui/view/chat.go`
- `ui/tui/view/input.go`
- `ui/tui/view/footer.go`

Goal of this task: minimally-implemented Bubble Tea program that compiles, can be instantiated, has all under-views as zero-value structs. Renders empty screen. No interactivity yet (that's Task 3).

- [ ] **Step 1: ui/tui/state/session.go**

```go
// Package state holds local session state for the TUI.
// In Phase 1 this is just a slice of messages for the echo demo;
// in Phase 2 it will be extended with pending ops, tool blocks, etc.
package state

// Role identifies who produced a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// Message is one entry in the chat scroll.
type Message struct {
	Role Role
	Text string
}

// Session is the TUI's local view of the current chat.
type Session struct {
	Messages []Message
}

// AppendMessage adds a message to the session history.
func (s *Session) AppendMessage(m Message) {
	s.Messages = append(s.Messages, m)
}
```

- [ ] **Step 2: ui/tui/view/header.go**

```go
// Package view holds the per-region Bubble Tea views for the TUI.
package view

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Header is the top-of-screen status line.
// Shows: Orchestra · model · mode · cwd
type Header struct {
	Model string // e.g. "qwen3.6-27b"
	Mode  string // e.g. "code"
	CWD   string // current working directory (truncated for display)

	width int
}

// SetSize updates the header's known width for layout.
func (h *Header) SetSize(width int) {
	h.width = width
}

// Render returns the styled header line.
func (h Header) Render() string {
	style := lipgloss.NewStyle().
		Background(lipgloss.Color("#3a3a3a")).
		Foreground(lipgloss.Color("#ffffff")).
		Padding(0, 1).
		Width(h.width)

	parts := fmt.Sprintf("Orchestra · %s · %s · %s", h.Model, h.Mode, h.CWD)
	return style.Render(parts)
}
```

- [ ] **Step 3: ui/tui/view/footer.go**

```go
package view

import "github.com/charmbracelet/lipgloss"

// Footer is the bottom-of-screen hints line.
type Footer struct {
	width int
}

// SetSize updates the footer's known width.
func (f *Footer) SetSize(width int) {
	f.width = width
}

// Render returns the styled footer hints.
func (f Footer) Render() string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Width(f.width).
		Padding(0, 1)
	return style.Render("↑↓ history · Enter send · Shift+Enter newline · Ctrl+C quit")
}
```

- [ ] **Step 4: ui/tui/view/chat.go**

```go
package view

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
)

// Chat renders the scrollable history of messages.
type Chat struct {
	vp viewport.Model
}

// NewChat creates an empty chat view sized to width × height.
func NewChat(width, height int) Chat {
	return Chat{vp: viewport.New(width, height)}
}

// SetSize resizes the chat viewport.
func (c *Chat) SetSize(width, height int) {
	c.vp.Width = width
	c.vp.Height = height
}

// SetMessages re-renders the viewport content from the session messages.
func (c *Chat) SetMessages(msgs []state.Message) {
	var b strings.Builder
	userStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7")).Bold(true)
	asstStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9ece6a"))
	for i, m := range msgs {
		switch m.Role {
		case state.RoleUser:
			b.WriteString(userStyle.Render("> ") + m.Text)
		case state.RoleAssistant:
			b.WriteString(asstStyle.Render(m.Text))
		default:
			b.WriteString(m.Text)
		}
		if i < len(msgs)-1 {
			b.WriteString("\n\n")
		}
	}
	c.vp.SetContent(b.String())
	c.vp.GotoBottom()
}

// Render returns the viewport's current view.
func (c Chat) Render() string {
	return c.vp.View()
}
```

- [ ] **Step 5: ui/tui/view/input.go**

```go
package view

import (
	"github.com/charmbracelet/bubbles/textarea"
)

// Input is the multiline editor at the bottom of the screen.
type Input struct {
	ta textarea.Model
}

// NewInput creates a sized textarea.
func NewInput(width int) Input {
	ta := textarea.New()
	ta.Placeholder = "Спроси Orchestra…"
	ta.SetWidth(width)
	ta.SetHeight(3) // grows up to ~6 visually via Bubble Tea wrapping
	ta.ShowLineNumbers = false
	ta.Focus()
	return Input{ta: ta}
}

// SetSize resizes the textarea width.
func (in *Input) SetSize(width int) {
	in.ta.SetWidth(width)
}

// Value returns the current input text.
func (in Input) Value() string { return in.ta.Value() }

// Reset clears the input.
func (in *Input) Reset() { in.ta.Reset() }

// Render returns the textarea view.
func (in Input) Render() string { return in.ta.View() }

// Inner returns the underlying textarea model so app.go can route Bubble Tea
// messages to it (key events, etc.). Phase 1 only.
func (in *Input) Inner() *textarea.Model { return &in.ta }
```

- [ ] **Step 6: ui/tui/app.go (skeleton; interactivity in Task 3)**

```go
// Package tui implements the Orchestra terminal UI client.
// In Phase 1 it provides an echo-only skeleton; Phase 2 connects it
// to orchestra core via JSON-RPC stdio.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// Config carries one-time settings into the App.
type Config struct {
	Model string // for the header
	Mode  string // for the header
	CWD   string // for the header
}

// App is the root Bubble Tea Model.
type App struct {
	cfg     Config
	session state.Session
	header  view.Header
	chat    view.Chat
	input   view.Input
	footer  view.Footer

	width  int
	height int
}

// NewApp constructs an App with the given config.
func NewApp(cfg Config) *App {
	return &App{
		cfg:    cfg,
		header: view.Header{Model: cfg.Model, Mode: cfg.Mode, CWD: cfg.CWD},
		// chat and input are sized when WindowSizeMsg arrives.
		footer: view.Footer{},
	}
}

// Init satisfies tea.Model.
func (a *App) Init() tea.Cmd {
	return nil
}

// Update is implemented in Task 3.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return a, nil
}

// View renders the full screen layout.
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return ""
	}
	return a.header.Render() + "\n" + a.chat.Render() + "\n" + a.input.Render() + "\n" + a.footer.Render()
}

// Run starts the tea program. Blocks until quit.
func Run(cfg Config) error {
	app := NewApp(cfg)
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
```

- [ ] **Step 7: Build**

```
go build ./...
```
Expected: clean build. The `tea` and `lipgloss` deps will switch from `// indirect` to direct in `go.mod` after `go mod tidy`. Run that:

```
go mod tidy
```

Then re-run `go build ./...`.

- [ ] **Step 8: Commit**

```bash
git add ui/tui/ go.mod go.sum
git commit -m "$(cat <<'EOF'
feat(ui/tui): scaffold Bubble Tea TUI skeleton

ui/tui/app.go: root Model/Init/View, Update is a no-op (interactivity
in next commit). state/session.go holds local message history.
view/{header,chat,input,footer}.go are per-zone views built on
Bubbles textarea/viewport and Lipgloss.

Phase 1 — empty skeleton; Phase 2 connects to orchestra core via
JSON-RPC stdio.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Wire interactivity — echo loop, resize, Ctrl+C, Esc

**Files (Modify):**
- `ui/tui/app.go`

This makes the TUI actually do something: typing in input + Enter echoes the text into the chat lane.

- [ ] **Step 1: Implement App.Init**

```go
func (a *App) Init() tea.Cmd {
	return textarea.Blink
}
```

(Add `"github.com/charmbracelet/bubbles/textarea"` to imports.)

- [ ] **Step 2: Implement App.Update**

Replace the no-op `Update` with:

```go
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.layout()
		return a, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "esc":
			a.input.Reset()
			return a, nil
		case "enter":
			text := strings.TrimSpace(a.input.Value())
			if text == "" {
				return a, nil
			}
			a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: text})
			// Echo back (Phase 1 behavior: no real agent yet).
			a.session.AppendMessage(state.Message{Role: state.RoleAssistant, Text: "echo: " + text})
			a.chat.SetMessages(a.session.Messages)
			a.input.Reset()
			return a, nil
		}
	}

	// Forward all other messages (mostly KeyMsg for typing) to the textarea.
	var cmd tea.Cmd
	innerTA := a.input.Inner()
	updatedTA, cmd := innerTA.Update(msg)
	*innerTA = updatedTA
	return a, cmd
}
```

(Add `"strings"` import. The textarea import was already added in Step 1.)

- [ ] **Step 3: Add layout helper**

Below `Update`, add:

```go
// layout recomputes child sizes based on current width/height.
func (a *App) layout() {
	a.header.SetSize(a.width)
	a.footer.SetSize(a.width)
	a.input.SetSize(a.width)

	// Reserve: 1 line header, 1 line footer, 4 lines for input area (textarea + padding).
	chatHeight := a.height - 1 - 1 - 4
	if chatHeight < 1 {
		chatHeight = 1
	}
	if a.chat.Inner() == nil {
		a.chat = view.NewChat(a.width, chatHeight)
	} else {
		a.chat.SetSize(a.width, chatHeight)
	}
	if a.input.Inner() == nil {
		a.input = view.NewInput(a.width)
	}
}
```

(Note: `a.chat.Inner()` and `a.input.Inner()` need to exist — extend `Chat` similarly to `Input.Inner()` from Task 2 if needed. Or use a different sentinel like a `bool initialized` flag.)

**Simpler alternative:** initialize `chat` and `input` once, lazily, on first WindowSizeMsg:

```go
func (a *App) layout() {
	a.header.SetSize(a.width)
	a.footer.SetSize(a.width)

	chatHeight := a.height - 6
	if chatHeight < 1 {
		chatHeight = 1
	}

	// Initialize on first call; resize on subsequent.
	if !a.initialized {
		a.chat = view.NewChat(a.width, chatHeight)
		a.input = view.NewInput(a.width)
		a.initialized = true
	} else {
		a.chat.SetSize(a.width, chatHeight)
		a.input.SetSize(a.width)
	}
}
```

Add `initialized bool` field to App. Use this simpler version.

- [ ] **Step 4: Build**

```
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add ui/tui/app.go
git commit -m "$(cat <<'EOF'
feat(ui/tui): wire echo loop, resize, and key handling

Update() now handles WindowSizeMsg (resizes children), Ctrl+C (quit),
Esc (clear input), Enter (echo input back as assistant message). All
other key events route to the textarea so typing works.

Phase 1 echo behavior; Phase 2 replaces the synthesized echo with a
real agent.run JSON-RPC call to orchestra core.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Wire `orchestra tui` CLI command

**Files:**
- Create: `internal/cli/tui.go`
- Modify: cobra root command file (likely `internal/cli/cli.go` or `internal/cli/root.go` — find by `grep -l "rootCmd" internal/cli/`)

- [ ] **Step 1: Locate the cobra root**

```
grep -n "rootCmd\|Execute" internal/cli/*.go | head -10
```

Find the file that has:
```go
var rootCmd = &cobra.Command{...}
```
Likely `cli.go` or `root.go`. Note the file path.

- [ ] **Step 2: Look at an existing simple subcommand for the pattern**

Read `internal/cli/chat.go` or `internal/cli/core.go` — the small ones. Note:
- How they declare a `*cobra.Command` variable
- How they register it via `rootCmd.AddCommand(...)` (probably in `init()`)
- How they read flags
- How they import config / set up workspace

Mimic the pattern.

- [ ] **Step 3: Create internal/cli/tui.go**

```go
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/ui/tui"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Open the interactive Orchestra terminal UI",
	Long: `Open the Orchestra terminal UI.

Phase 1: echo-only skeleton (no core connection yet). Use Ctrl+C to quit.

Configure model and project_root via .orchestra.yml in the current directory
(create with 'orchestra init').`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load(".") // best-effort; nil-safe below
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}

		model := "(none)"
		if cfg != nil {
			model = cfg.LLM.Model
		}

		return tui.Run(tui.Config{
			Model: model,
			Mode:  "code",
			CWD:   filepath.Base(cwd),
		})
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
```

**Adjustments to make based on actual codebase:**
- The `config.Load` signature might differ (e.g., return only `*Config`, or take a different argument). Adapt the call.
- `cfg.LLM.Model` might be different field — verify against `internal/config/config.go`.
- If the existing cobra root variable isn't called `rootCmd`, adapt.

- [ ] **Step 4: Build + smoke run**

```
go build -o orchestra.exe ./cmd/orchestra
.\orchestra.exe tui
```

Expected: empty Orchestra TUI opens with header showing "Orchestra · qwen3.6-27b · code · Orchestra" (or whatever your cwd basename is). Type something + Enter → it echoes. Ctrl+C exits.

If you can't run interactively in the subagent environment, just verify build is clean.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/tui.go
git commit -m "$(cat <<'EOF'
feat(cli): add 'orchestra tui' subcommand

Wires ui/tui.Run into a cobra subcommand. Reads model name from
.orchestra.yml (or shows '(none)' if no config). cwd basename shown
in header. Phase 1: echo-only.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Test pass — teatest snapshots for key flows

**Files:**
- Create: `ui/tui/app_test.go`
- Maybe: `ui/tui/state/session_test.go`

`teatest` is in `github.com/charmbracelet/x/exp/teatest`. May need to add to `go.mod`. Check first:

```
grep teatest go.mod
```

If absent, run `go get github.com/charmbracelet/x/exp/teatest` (will be added once a test imports it and we run `go mod tidy`).

- [ ] **Step 1: Write app_test.go with three scenarios**

```go
package tui_test

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/orchestra/orchestra/ui/tui"
)

func TestApp_EchoesUserInput(t *testing.T) {
	app := tui.NewApp(tui.Config{Model: "test-model", Mode: "code", CWD: "test"})
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Type "hello" + Enter.
	tm.Type("hello")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})

	out, err := teatest.WaitFor(
		t, tm.FinalOutput(),
		func(b []byte) bool {
			return strings.Contains(string(b), "echo: hello")
		},
		teatest.WithCheckInterval(50*time.Millisecond),
		teatest.WithDuration(2*time.Second),
	)
	if err != nil {
		t.Fatalf("waiting for echo: %v\n\noutput:\n%s", err, out)
	}
}

func TestApp_CtrlCQuits(t *testing.T) {
	app := tui.NewApp(tui.Config{})
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})

	if err := tm.WaitForExit(); err != nil {
		t.Fatalf("expected clean exit, got %v", err)
	}
}

func TestApp_EscClearsInput(t *testing.T) {
	app := tui.NewApp(tui.Config{})
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	tm.Type("garbage")
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})

	out, _ := teatest.WaitFor(
		t, tm.FinalOutput(),
		func(b []byte) bool { return true }, // capture whatever
		teatest.WithDuration(500*time.Millisecond),
	)

	if strings.Contains(string(out), "garbage") {
		t.Errorf("expected 'garbage' to be cleared from input area, but it appeared in output:\n%s", string(out))
	}
}
```

**Note on teatest API:** the exact function names (`NewTestModel`, `Send`, `Type`, `FinalOutput`, `WaitForExit`, `WaitFor`, options) may differ slightly by version. Check `_opencode/` or run `go doc github.com/charmbracelet/x/exp/teatest` for the actual API. Adapt accordingly.

- [ ] **Step 2: Add session_test.go (small, optional)**

```go
package state_test

import (
	"testing"

	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestSession_AppendMessage(t *testing.T) {
	var s state.Session
	s.AppendMessage(state.Message{Role: state.RoleUser, Text: "hi"})
	s.AppendMessage(state.Message{Role: state.RoleAssistant, Text: "hello"})

	if len(s.Messages) != 2 {
		t.Fatalf("want 2 messages, got %d", len(s.Messages))
	}
	if s.Messages[0].Role != state.RoleUser || s.Messages[1].Role != state.RoleAssistant {
		t.Errorf("roles in wrong order: %+v", s.Messages)
	}
}
```

- [ ] **Step 3: Run tests**

```
go mod tidy
go test ./ui/tui/... -count=1
```

Expected: pass.

- [ ] **Step 4: Run full suite to ensure no regression**

```
go test ./... -count=1
```

- [ ] **Step 5: Commit**

```bash
git add ui/tui/app_test.go ui/tui/state/session_test.go go.mod go.sum
git commit -m "$(cat <<'EOF'
test(ui/tui): teatest scenarios for echo, Ctrl+C, Esc

Three flows covered via teatest:
  - typing 'hello' + Enter results in 'echo: hello' in the chat
  - Ctrl+C exits cleanly
  - Esc clears input (no garbage leaks into next render)

Plus a tiny test for state.Session message append ordering.

Part of TUI Phase 1 (skeleton).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

If teatest API mismatch makes tests fail to compile, **report DONE_WITH_CONCERNS** noting which APIs need adaptation. Do not commit failing tests.

---

## Task 6: Polish — fill in ui/tui/README.md and update memory

**Files:**
- Modify: `ui/tui/README.md`
- Memory: append to `C:\Users\andre\.claude\projects\D--CursorProjects-Orchestra\memory\` (controller will do this, not the implementer)

- [ ] **Step 1: Replace ui/tui/README.md stub**

```markdown
# Orchestra TUI

Terminal UI для Orchestra ядра.

## Запуск

```bash
orchestra tui
```

(Требует, чтобы в cwd был `.orchestra.yml` для отображения модели в header'e — иначе будет "(none)".)

## Раскладка

```
┌─────────────────────────────────────────────────────────────┐
│  Orchestra · qwen3.6-27b · code · <project>                 │  header
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  > пользовательский ввод                                    │
│                                                             │
│  ответ агента                                               │  ← (Фаза 1: echo)
│                                                             │
├─────────────────────────────────────────────────────────────┤
│ > _                                                         │  multiline textarea
├─────────────────────────────────────────────────────────────┤
│ ↑↓ history · Enter send · Shift+Enter newline · Ctrl+C quit │  footer
└─────────────────────────────────────────────────────────────┘
```

## Клавиши

| Клавиша | Действие |
|---|---|
| Enter | отправить ввод |
| Shift+Enter | новая строка в инпуте |
| Esc | очистить инпут |
| Ctrl+C | выйти |

## Статус по фазам

- [x] **Фаза 1 — скелет** (текущая): раскладка, echo, базовая навигация
- [ ] Фаза 2 — подключение к `orchestra core` через JSON-RPC stdio, streaming событий
- [ ] Фаза 3 — collapsible tool blocks, inline-diff, pending ops action bar
- [ ] Фаза 4 — slash-команды, @-mention, динамические footer hints
- [ ] Фаза 5 — polish, snapshot tests расширенные

## Архитектура

`ui/tui/app.go` — корневая Bubble Tea модель. Делегирует рендеринг в `view/{header,chat,input,footer}.go`. Состояние сессии (history) живёт в `state/session.go`. Phase 2 добавит `rpcclient/` для stdio JSON-RPC.

См. также: `docs/superpowers/specs/2026-05-07-tui-design.md` (общий дизайн TUI), `docs/PROTOCOL.md` (контракт ядра).
```

- [ ] **Step 2: Final test pass**

```
go build ./... && go test ./... -count=1
```

Expected: all pass.

- [ ] **Step 3: Commit**

```bash
git add ui/tui/README.md
git commit -m "$(cat <<'EOF'
docs(ui/tui): expand README with layout, keybindings, phase roadmap

Closes TUI Phase 1 (skeleton). Echo demo runs; Phase 2 will connect
to orchestra core via JSON-RPC stdio.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Phase 1 Completion Criteria

After all 6 tasks:

- [ ] `orchestra tui` запускается, показывает корректную раскладку
- [ ] Можно ввести текст + Enter → видно "> текст" + "echo: текст" в ленте
- [ ] Ctrl+C корректно завершает программу
- [ ] Esc очищает инпут
- [ ] Resize (изменить размер терминала) перерасчитывает раскладку
- [ ] `go test ./... -count=1` зелёный
- [ ] `go build -o orchestra.exe ./cmd/orchestra` чистый

Ручная проверка после всех задач:
```
go build -o orchestra.exe ./cmd/orchestra
.\orchestra.exe tui
```

Поиграться руками, убедиться что всё работает.

---

## Notes for the implementing engineer

1. **API дрифт.** Bubble Tea v1.3.10, Bubbles v1.0.0, Lipgloss v1.1.0 — версии зафиксированы. Если документации в `_opencode/` или модели подсказывают другие имена методов — проверьте сначала по `go doc <package>` чтобы поймать version drift.

2. **Существующий `internal/tui/model_picker.go`.** Не трогать. Это другой пакет (`package tui` в `internal/tui/`), используется в `internal/cli/model.go` для выбора модели. Наш TUI = `ui/tui/` (другой import path).

3. **teatest.** Это experimental пакет. Если API окажется заметно другим — адаптировать тесты или пропустить Task 5 step 1 с DONE_WITH_CONCERNS, оставив только session_test.go.

4. **Workflow rule.** Tasks 1-4 — только реализация + `go build ./...`. Task 5 — тесты. Task 6 — README.

5. **Если задача 4 (cobra wiring) обнаруживает что текущая cobra-структура не совпадает с моими предположениями** (например, нет `rootCmd` или используется другой паттерн) — изучите как сделаны существующие subcommands и точно так же добавьте `tui`.
