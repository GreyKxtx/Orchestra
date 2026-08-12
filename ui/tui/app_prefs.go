package tui

import (
	"strings"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/theme"
	"github.com/orchestra/orchestra/ui/tui/view"
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

// cycleShellPerms toggles shell ask ↔ allow (bypass) and persists UI prefs.
// Bound to Shift+Tab so Tab keeps cycling agent modes.
func (a *App) cycleShellPerms() {
	a.toggleAllowExec(!a.allowExec)
	_ = a.persistUIPrefs()
	if a.allowExec {
		a.showToast("shell · allow · Shift+Tab")
	} else {
		a.showToast("shell · ask · Shift+Tab")
	}
	a.layout()
}

// respondShellPermission closes the permission modal and answers the core.
// sessionAllow flips shell · allow for the rest of the session (+ prefs),
// or for lsp.install sets lsp.auto_install=true.
// toolAlways remembers this tool so ask-mode skips the modal next time.
func (a *App) respondShellPermission(approved, sessionAllow, toolAlways bool) {
	req, _ := a.perms.Answer()
	tool := req.Tool
	kind := req.Kind
	isLSP := strings.EqualFold(kind, "lsp.install") || tool == "lsp.install"
	reqID := req.ReqID
	a.permModal = nil
	if approved && sessionAllow {
		if isLSP {
			_ = a.persistLSPAutoInstall(true)
			a.showToast("lsp · auto_install — всегда")
		} else {
			a.toggleAllowExec(true)
			_ = a.persistUIPrefs()
			a.showToast("shell · allow — на сессию")
		}
	}
	if approved && toolAlways && tool != "" {
		if a.sessionToolAllow == nil {
			a.sessionToolAllow = map[string]bool{}
		}
		a.sessionToolAllow[strings.ToLower(strings.TrimSpace(tool))] = true
		a.showToast("tool · always: " + tool)
	}
	if approved && isLSP {
		a.chrome.lspStatus = "installing"
		a.showToast("ставлю language server…")
		a.syncStatusBar()
	}
	a.updateStatusHints()
	a.layout()
	if a.rpc != nil {
		a.rpc.RespondPermissionDecision(reqID, rpcclient.PermissionDecision{
			Approved: approved,
			Always:   approved && sessionAllow && isLSP,
		})
	}
	// Show next queued permission modal (FIFO: shell ↔ lsp.install).
	a.showNextPermissionModal()
}

func (a *App) showNextPermissionModal() {
	if a.permModal != nil {
		return
	}
	for {
		next, ok := a.perms.Promote()
		if !ok {
			return
		}
		if a.toolAllowedThisSession(next.Tool) {
			// Auto-approve without showing a modal, then keep draining.
			if a.rpc != nil {
				a.rpc.RespondPermission(next.ReqID, true)
			}
			a.perms.Answer()
			continue
		}
		a.permModal = view.NewPermissionModal(next.Tool, next.Description, next.Kind)
		a.layout()
		return
	}
}

func (a *App) persistLSPAutoInstall(on bool) error {
	return a.cfgStore.Mutate(func(cfg *config.ProjectConfig) error {
		if on {
			cfg.LSP.AutoInstall = "true"
		} else {
			cfg.LSP.AutoInstall = "ask"
		}
		return nil
	})
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
	allowExec := a.allowExec
	themeName := a.cfg.Theme
	return a.cfgStore.Mutate(func(cfg *config.ProjectConfig) error {
		cfg.UI.AllowExec = allowExec
		if themeName != "" {
			cfg.UI.Theme = themeName
		}
		return nil
	})
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
