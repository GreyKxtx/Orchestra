package tui

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

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

// syncMention checks if the input ends with an in-progress @-mention and
// updates the mention palette. Completed mentions (@path followed by space)
// and mid-token '@' (emails) do not open the palette.
func (a *App) syncMention() {
	q, active := activeMentionQuery(a.input.Value())
	if !active {
		a.mentionActive = false
		return
	}
	// Lazy-load workspace files.
	if a.workspaceFiles == nil && a.cfg.WorkspaceRoot != "" {
		a.workspaceFiles = listWorkspaceFiles(a.cfg.WorkspaceRoot, 4)
	}

	var items []string
	if q == "" {
		// Bare "@" — show first 10 files.
		items = a.workspaceFiles
		if len(items) > 10 {
			items = items[:10]
		}
	} else {
		matches := fuzzy.Find(filepath.ToSlash(q), a.workspaceFiles)
		items = make([]string, 0, len(matches))
		for _, m := range matches {
			items = append(items, m.Str)
			if len(items) >= 10 {
				break
			}
		}
	}
	a.mentionPalette.SetItems(items)
	a.mentionActive = len(items) > 0
}

// listWorkspaceFiles walks root up to maxDepth levels, skipping hidden dirs,
// and returns relative POSIX paths. Result is capped at 500 entries.
func listWorkspaceFiles(root string, maxDepth int) []string {
	if root == "" {
		return nil
	}
	var files []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(root, path)
			depth := len(strings.Split(rel, string(filepath.Separator)))
			if depth > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		files = append(files, filepath.ToSlash(rel))
		if len(files) >= 500 {
			return filepath.SkipDir
		}
		return nil
	})
	return files
}

// mentionQuery returns the text after @ in the last in-progress @-mention,
// or "" if none / completed. Prefer activeMentionQuery when you need to know
// whether the palette should open (bare "@" is active with empty query).
func mentionQuery(text string) string {
	q, active := activeMentionQuery(text)
	if !active {
		return ""
	}
	return q
}

// activeMentionQuery reports an in-progress @-mention at the end of text.
// Active only when '@' is at start-of-token and nothing after it contains
// whitespace (so "@path " / normal typing after a finished mention stay closed).
func activeMentionQuery(text string) (query string, active bool) {
	lastAt := strings.LastIndex(text, "@")
	if lastAt < 0 {
		return "", false
	}
	if lastAt > 0 {
		prev := text[lastAt-1]
		if prev != ' ' && prev != '\n' && prev != '\t' {
			return "", false // mid-token (e.g. user@host)
		}
	}
	after := text[lastAt+1:]
	if strings.ContainsAny(after, " \t\n") {
		return "", false // mention already finished
	}
	return after, true
}

// replaceLastMention replaces the last @word in text with "@"+replacement
// plus a trailing space so the mention is completed and the palette closes.
func replaceLastMention(text, replacement string) string {
	lastAt := strings.LastIndex(text, "@")
	if lastAt < 0 {
		return text
	}
	replacement = strings.TrimSpace(replacement)
	if replacement == "" {
		return text
	}
	if !strings.HasPrefix(replacement, "@") {
		replacement = "@" + replacement
	}
	rest := text[lastAt+1:]
	spaceIdx := strings.IndexByte(rest, ' ')
	if spaceIdx < 0 {
		return text[:lastAt] + replacement + " "
	}
	return text[:lastAt] + replacement + " " + strings.TrimLeft(rest[spaceIdx:], " ")
}

// updateCommandModal handles keyboard input while the Ctrl+K modal is open.
func (a *App) updateCommandModal(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	cm := a.commandModal
	switch m.String() {
	case "ctrl+c":
		return a, tea.Quit
	case "esc", "left":
		cm.SetActive(false)
	case "up":
		cm.CursorUp()
	case "down":
		cm.CursorDown()
	case "enter", "right":
		selected := cm.Selected()
		if selected == "" {
			return a, nil
		}
		// Run the command; if it pushed a dialog onto the stack, leave the
		// command modal active so Esc/← from that dialog returns here
		// rather than dropping the user back into the chat.
		stackBefore := len(a.dialogStack)
		cmd := a.executePaletteCmd(selected)
		if len(a.dialogStack) == stackBefore {
			cm.SetActive(false)
		}
		return a, cmd
	case "backspace":
		cm.Backspace()
	default:
		if len(m.Runes) == 1 {
			cm.TypeRune(m.Runes[0])
		}
	}
	return a, nil
}

const helpText = `Orchestra TUI — клавиши:
  Enter         отправить
  Shift+Enter   новая строка
  Tab           цикл mode (build / plan / explore / ask / debug / architecture / agent / orchestra)
  Ctrl+T / t    tools → diff (d / Ctrl+D — diff)
  /compact      сжать LLM-контекст сессии
  /rewind       checkpoint rewind (скелет)
  /memory       слои памяти + pinned facts
  /mcp          MCP servers (добавить / edit / test)
  ↑ / ↓         история ввода
  @             mention файла
  Ctrl+K        command palette
  Ctrl+S        sessions (если есть в проекте)
  /shell        ask ↔ allow
  /theme        тема orchestra ↔ neutral
  d             diff последнего commit
  y / a / t / n shell: раз / сессия / этот tool / нет
  Ctrl+C        выход

Pipeline: staging + LSP → auto-commit. Chrome: docs/architecture/tui-chrome.md`

// executePaletteCmd carries out the chosen slash command and returns a tea.Cmd
// if the action requires async work, or nil. Commands that don't start a chat
// session (/provider, /model, /quit, /clear) leave the welcome screen up so
// the user can return to it after Esc; commands that print into the chat
// (/help, /mode) dismiss welcome since their output lives in the chat view.
func (a *App) executePaletteCmd(cmd string) tea.Cmd {
	dismissWelcome := func() {
		a.showWelcome = false
		a.chat.SetForceWelcome(false)
	}
	switch cmd {
	case "/help":
		dismissWelcome()
		a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: helpText})
		a.chat.SetMessages(a.session.Messages)
	case "/clear":
		a.session.Clear()
		a.chat.SetMessages(a.session.Messages)
		a.currentSessionID = ""
		a.coreSessionID = ""
		return a.startCoreSession()
	case "/compact":
		dismissWelcome()
		return a.cmdSessionCompact()
	case "/memory":
		dismissWelcome()
		return a.cmdShowMemory()
	case "/model":
		return a.openModelDialogForCurrentProvider()
	case "/orchestra":
		a.openOrchestraDialog()
		return nil
	case "/provider":
		a.dialogStack = append(a.dialogStack, view.NewProviderDialog(a.providerReadyMap()))
		return nil
	case "/sessions":
		a.openSessionsDialog()
		return nil
	case "/rewind":
		dismissWelcome()
		a.openRewindDialog()
		return nil
	case "/mcp":
		a.openMCPDialog()
		return nil
	case "/mode":
		dismissWelcome()
		mode := a.cfg.Mode
		if mode == "" {
			mode = "(not configured)"
		}
		a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: "Mode: " + mode})
		a.chat.SetMessages(a.session.Messages)
	case "/diff":
		if len(a.lastCommitDiff) > 0 {
			a.showCommitDiff()
		} else {
			dismissWelcome()
			a.session.AppendMessage(state.Message{
				Role: state.RoleSystem,
				Text: "Diff появится после хода агента с изменениями ([committed] в чате).",
			})
			a.chat.SetMessages(a.session.Messages)
		}
	case "/shell", "/exec":
		dismissWelcome()
		a.toggleAllowExec(!a.allowExec)
		if err := a.persistUIPrefs(); err != nil {
			a.showToast(fmt.Sprintf("shell: %v", a.allowExec))
		} else {
			if a.allowExec {
				a.showToast("shell · allow — команды без спроса")
			} else {
				a.showToast("shell · ask — спрашивать перед командой")
			}
		}
		a.layout()
	case "/theme":
		dismissWelcome()
		next := a.cycleTheme()
		a.showToast("тема: " + next)
		a.layout()
	case "/quit":
		return tea.Quit
	}
	return nil
}
