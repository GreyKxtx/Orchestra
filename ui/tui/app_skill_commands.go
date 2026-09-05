package tui

import (
	"context"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// syncSkillCommands loads the project's skills so each one shows up as its
// own /<name> command, the same way MCP server prompts do. Best effort: a
// project with no skills, or a core without RPC, simply leaves the palette
// as it was.
func (a *App) syncSkillCommands() tea.Cmd {
	if a.rpc == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		skills, err := a.rpc.SkillList(ctx)
		if err != nil {
			return nil
		}
		return skillsLoadedMsg{skills: skills}
	}
}

// skillsLoadedMsg carries a skill.list result back to the update loop, which
// owns both the palette and the name list parseSkillSlashCommand matches
// against.
type skillsLoadedMsg struct {
	skills []rpcclient.SkillSummary
}

// handleSkillsLoadedMsg records the loaded skill names and offers them in
// the palette. A skill whose name collides with a built-in command (e.g. one
// named "skill" or "help") is left out here — it stays reachable only via
// `/skill <name> <args>`, since as a bare "/<name>" it would either be
// unreachable behind the built-in or, worse, silently steal that built-in's
// input.
func (a *App) handleSkillsLoadedMsg(m skillsLoadedMsg) {
	names := make([]string, 0, len(m.skills))
	rows := make([]view.SlashCmd, 0, len(m.skills))
	for _, s := range m.skills {
		name := strings.TrimSpace(s.Name)
		if name == "" || isBuiltinSlashCmd(name) {
			continue
		}
		names = append(names, name)
		rows = append(rows, view.SlashCmd{Cmd: "/" + name, Desc: s.Description})
	}
	sort.Strings(names)
	a.skillNames = names
	a.slashPalette.SetExtraSkills(rows)
}

// isBuiltinSlashCmd reports whether name (without the leading "/") is
// already a built-in command.
func isBuiltinSlashCmd(name string) bool {
	for _, c := range view.AllSlashCmds {
		if strings.TrimPrefix(c.Cmd, "/") == name {
			return true
		}
	}
	return false
}

// parseSkillSlashCommand matches "/<name> <args>" against skillNames. ok is
// false for anything else, including a name not in skillNames and a skill
// invoked without arguments — skill.invoke requires them, same as the
// existing "/skill <name> <args>" form.
func parseSkillSlashCommand(text string, skillNames []string) (name, args string, ok bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") {
		return "", "", false
	}
	rest := trimmed[1:]
	candidate := rest
	if i := strings.IndexAny(rest, " \t"); i >= 0 {
		candidate = rest[:i]
		args = strings.TrimSpace(rest[i+1:])
	}
	if candidate == "" || args == "" {
		return "", "", false
	}
	for _, n := range skillNames {
		if n == candidate {
			return candidate, args, true
		}
	}
	return "", "", false
}
