package tui

import (
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

func (a *App) agentRunOptions() rpcclient.AgentRunOptions {
	return rpcclient.AgentRunOptions{
		Apply:     true,
		AllowExec: a.allowExec,
		Profile:   a.cfg.Profile,
	}
}

func (a *App) workflowRunOptions() rpcclient.WorkflowRunOptions {
	return rpcclient.WorkflowRunOptions{
		Apply:     true,
		AllowExec: a.allowExec,
	}
}

func (a *App) toggleAllowExec(on bool) {
	a.allowExec = on
	a.updateStatusHints()
}

// respondShellPermission closes the permission modal and answers the core.
// sessionAllow flips shell · allow for the rest of the session (+ prefs).
// toolAlways remembers this tool so ask-mode skips the modal next time.
func (a *App) respondShellPermission(approved, sessionAllow, toolAlways bool) {
	tool := ""
	if a.permModal != nil {
		tool = a.permModal.Tool
	}
	a.permModal = nil
	if approved && sessionAllow {
		a.toggleAllowExec(true)
		_ = a.persistUIPrefs()
		a.showToast("shell · allow — на сессию")
	}
	if approved && toolAlways && tool != "" {
		if a.sessionToolAllow == nil {
			a.sessionToolAllow = map[string]bool{}
		}
		a.sessionToolAllow[strings.ToLower(strings.TrimSpace(tool))] = true
		a.showToast("tool · always: " + tool)
	}
	a.updateStatusHints()
	a.layout()
	if a.rpc != nil {
		a.rpc.RespondPermission(approved)
	}
}

func (a *App) toolAllowedThisSession(tool string) bool {
	if a.allowExec {
		return true
	}
	if a.sessionToolAllow == nil {
		return false
	}
	return a.sessionToolAllow[strings.ToLower(strings.TrimSpace(tool))]
}

func (a *App) persistUIPrefs() error {
	if a.cfg.ConfigPath == "" {
		return fmt.Errorf("no .orchestra.yml path configured")
	}
	cfg, err := config.Load(a.cfg.ConfigPath)
	if err != nil || cfg == nil {
		cfg = config.DefaultConfig(a.cfg.WorkspaceRoot)
		cfg.ProjectRoot = a.cfg.WorkspaceRoot
	}
	cfg.UI.AllowExec = a.allowExec
	if a.cfg.Theme != "" {
		cfg.UI.Theme = a.cfg.Theme
	}
	return config.Save(a.cfg.ConfigPath, cfg)
}

// cycleTheme toggles between registered themes and persists the choice.
func (a *App) cycleTheme() string {
	cur := strings.ToLower(strings.TrimSpace(a.cfg.Theme))
	if cur == "" {
		cur = theme.DefaultTheme
	}
	next := "neutral"
	if cur == "neutral" {
		next = "orchestra"
	}
	a.cfg.Theme = next
	theme.SetTheme(theme.ByName(next))
	_ = a.persistUIPrefs()
	return next
}
