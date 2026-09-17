package tui

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/ui/tui/view"
)

// Every /command typed in the TUI is executed by the slash palette: the
// palette opens on "/" and Enter runs the highlighted entry, so a command the
// palette's dispatcher does not know silently does nothing. /skills and
// /workflows were in the menu and in /help, and both were dead — selecting
// them left the screen exactly as it was.
//
// The guard is the menu itself: a command offered to the user has to do
// something visible when chosen.
func TestSlashPalette_EveryOfferedCommandDoesSomething(t *testing.T) {
	offered := make([]string, 0, len(view.AllSlashCmds)+len(view.DefaultModalCommands))
	for _, item := range view.AllSlashCmds {
		offered = append(offered, item.Cmd)
	}
	// Ctrl+K offers its own list through the same dispatcher.
	for _, item := range view.DefaultModalCommands {
		offered = append(offered, item.Name)
	}
	for _, name := range offered {
		switch name {
		case "/quit": // ends the program; nothing to observe
			continue
		case "/attach": // needs a path argument; the bare form is a usage toast
		}
		t.Run(name, func(t *testing.T) {
			a, _ := testCoreApp(t)
			a.currentSessionID = "sess-1"
			before := struct {
				msgs    int
				dialogs int
				toast   string
				welcome bool
			}{len(a.session.Messages), len(a.dialogStack), a.toastText, a.showWelcome}

			cmd := a.executePaletteCmd(name)

			changed := cmd != nil ||
				len(a.session.Messages) != before.msgs ||
				len(a.dialogStack) != before.dialogs ||
				a.toastText != before.toast ||
				a.showWelcome != before.welcome
			if !changed {
				t.Errorf("%s is offered in the menu and does nothing when chosen", name)
			}
		})
	}
}

// The two that were dead, by name, so the reason does not get lost.
func TestSlashPalette_SkillsAndWorkflowsList(t *testing.T) {
	for _, name := range []string{"/skills", "/workflows"} {
		a, _ := testCoreApp(t)
		a.currentSessionID = "sess-1"
		if cmd := a.executePaletteCmd(name); cmd == nil {
			t.Errorf("%s did not ask the core for the list", name)
		}
	}
}

func TestSlashPalette_HelpListsOnlyWorkingCommands(t *testing.T) {
	known := map[string]bool{}
	for _, item := range view.AllSlashCmds {
		known[item.Cmd] = true
	}
	for _, line := range strings.Split(helpText, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
			continue
		}
		if name := fields[0]; !known[name] && !strings.Contains(name, ":") {
			t.Errorf("/help offers %s, which the command menu does not list", name)
		}
	}
}
