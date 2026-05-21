package tui

import (
	"fmt"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/ui/tui/rpcclient"
)

func (a *App) agentRunOptions() rpcclient.AgentRunOptions {
	return rpcclient.AgentRunOptions{
		Apply:     a.autoApply,
		AllowExec: a.allowExec,
	}
}

func (a *App) workflowRunOptions() rpcclient.WorkflowRunOptions {
	return rpcclient.WorkflowRunOptions{
		Apply:     a.autoApply,
		AllowExec: a.allowExec,
	}
}

func (a *App) writeModeLabel() string {
	if a.autoApply {
		return "LIVE"
	}
	return "PREVIEW"
}

func (a *App) toggleLiveMode(on bool) {
	a.autoApply = on
	a.updateStatusHints()
}

func (a *App) toggleAllowExec(on bool) {
	a.allowExec = on
	a.updateStatusHints()
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
	cfg.UI.AutoApply = a.autoApply
	cfg.UI.AllowExec = a.allowExec
	return config.Save(a.cfg.ConfigPath, cfg)
}
