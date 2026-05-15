package tui

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"

	"github.com/orchestra/orchestra/internal/sessionstore"
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

// syncMention checks if the input ends with an @-mention and updates the
// mention palette.
func (a *App) syncMention() {
	if !strings.Contains(a.input.Value(), "@") {
		a.mentionActive = false
		return
	}
	// Lazy-load workspace files.
	if a.workspaceFiles == nil && a.cfg.WorkspaceRoot != "" {
		a.workspaceFiles = listWorkspaceFiles(a.cfg.WorkspaceRoot, 4)
	}

	q := mentionQuery(a.input.Value())
	lastAt := strings.LastIndex(a.input.Value(), "@")
	if lastAt < 0 {
		a.mentionActive = false
		return
	}

	var items []string
	if q == "" {
		// Show first 10 files when just "@" is typed.
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

// mentionQuery returns the text after @ in the last @word of the input,
// or "" if the last word is not an @-mention or has no text after @.
func mentionQuery(text string) string {
	lastAt := strings.LastIndex(text, "@")
	if lastAt < 0 {
		return ""
	}
	word := text[lastAt+1:]
	if strings.IndexByte(word, ' ') >= 0 {
		return ""
	}
	return word
}

// replaceLastMention replaces the last @word in text with replacement.
func replaceLastMention(text, replacement string) string {
	lastAt := strings.LastIndex(text, "@")
	if lastAt < 0 {
		return text
	}
	rest := text[lastAt+1:]
	spaceIdx := strings.IndexByte(rest, ' ')
	if spaceIdx < 0 {
		return text[:lastAt] + replacement
	}
	return text[:lastAt] + replacement + rest[spaceIdx:]
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

const helpText = `Orchestra TUI — key bindings:
  Enter         send message
  Shift+Enter   newline in input
  Tab           cycle agent mode (build / ask / plan)
  Ctrl+T        expand/collapse last tool block
  ↑ / ↓         input history (single-line mode)
  @             file mention (fuzzy)
  Ctrl+K        command palette
  Ctrl+O        change model (onboarding)
  a             apply pending ops
  d             toggle diff
  x             discard pending ops
  y / n / Esc   allow / deny exec.run permission
  Ctrl+C        quit`

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
	case "/model":
		return a.openModelDialogForCurrentProvider()
	case "/provider":
		a.dialogStack = append(a.dialogStack, view.NewProviderDialog())
		return nil
	case "/sessions":
		metas, _ := sessionstore.List(a.cfg.WorkspaceRoot)
		a.dialogStack = append(a.dialogStack, view.NewSessionsDialog(metas))
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
		if a.pendingOps != nil {
			if a.diffShown {
				a.session.RemoveDiff()
				a.diffShown = false
			} else {
				content := a.buildDiffContent()
				a.session.AddDiff(content)
				a.diffShown = true
			}
			a.chat.SetMessages(a.session.Messages)
		}
	case "/apply":
		if a.pendingOps != nil && a.rpc != nil {
			rawOps := a.pendingOps.Ops
			count := len(a.pendingOps.Ops)
			a.pendingOps = nil
			if a.diffShown {
				a.session.RemoveDiff()
				a.diffShown = false
			}
			a.chat.SetMessages(a.session.Messages)
			rpc := a.rpc
			return func() tea.Msg {
				return applyResultMsg{err: rpc.ApplyOps(context.Background(), rawOps), count: count}
			}
		}
	case "/discard":
		if a.pendingOps != nil {
			a.pendingOps = nil
			if a.diffShown {
				a.session.RemoveDiff()
				a.diffShown = false
			}
			a.chat.SetMessages(a.session.Messages)
		}
	case "/quit":
		return tea.Quit
	}
	return nil
}
