package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/config"
)

// chromeMetrics groups status-bar / session gauges owned by App (not chat history).
type chromeMetrics struct {
	sessionTokens     int     // prompt+completion across the whole session
	promptTokensUsed  int     // last LLM step prompt size — drives the ctx bar
	livePromptTokens  int     // current step prompt tokens while a turn is running
	tokensEstimated   bool    // true when last ctx figure is agent estimate, not provider usage
	sessionCostUSD    float64 // accumulated spend (paid providers)
	modelContextLimit int
	lspStatus         string // off | idle | active
	showCost          bool
}

// syncStatusBar pushes App session metrics into the status bar widget.
func (a *App) syncStatusBar() {
	a.statusBar.SetProfile(a.cfg.Profile)
	a.statusBar.SetLSPStatus(a.chrome.lspStatus)
	used := a.chrome.promptTokensUsed
	if a.turn.ShowBusySpinner() && a.chrome.livePromptTokens > 0 {
		used = a.chrome.livePromptTokens
	}
	a.statusBar.SetTokens(used, a.contextLimit())
	a.statusBar.SetTokensEstimated(a.chrome.tokensEstimated)
	a.statusBar.SetCostUSD(a.chrome.sessionCostUSD)
	a.statusBar.SetShowCost(a.chrome.showCost)
}

type lspStatusMsg struct {
	status string
}

// refreshLSPStatusCmd polls core.health once for current lsp_status.
func (a *App) refreshLSPStatusCmd() tea.Cmd {
	rpc := a.rpc
	if rpc == nil {
		return nil
	}
	return func() tea.Msg {
		st, err := rpc.QueryLSPStatus(context.Background())
		if err != nil || st == "" {
			return nil
		}
		return lspStatusMsg{status: st}
	}
}

// awaitLSPWarmupCmd polls health until active/off or ~10s — core WarmupLSP
// starts servers asynchronously after initialize.
func (a *App) awaitLSPWarmupCmd() tea.Cmd {
	rpc := a.rpc
	if rpc == nil {
		return nil
	}
	return func() tea.Msg {
		deadline := time.Now().Add(10 * time.Second)
		last := ""
		for {
			st, err := rpc.QueryLSPStatus(context.Background())
			if err == nil && st != "" {
				last = st
				if st == "active" || st == "off" {
					return lspStatusMsg{status: st}
				}
			}
			if time.Now().After(deadline) {
				if last != "" {
					return lspStatusMsg{status: last}
				}
				return nil
			}
			time.Sleep(400 * time.Millisecond)
		}
	}
}

func (a *App) contextLimit() int {
	if a.chrome.modelContextLimit > 0 {
		return a.chrome.modelContextLimit
	}
	return 0
}

func (a *App) setContextLimitFromConfig(cfg *config.ProjectConfig) {
	if cfg == nil {
		return
	}
	if n := int(cfg.EffectiveNumCtx()); n > 0 {
		a.chrome.modelContextLimit = n
		a.statusBar.SetModelCtx(n)
	}
}

// loadConfigPrefs reads agent profile and pricing hints from .orchestra.yml.
func (a *App) loadConfigPrefs() {
	if a.cfg.ConfigPath == "" {
		return
	}
	cfg, err := config.Load(a.cfg.ConfigPath)
	if err != nil || cfg == nil {
		return
	}
	a.cfg.Profile = strings.TrimSpace(cfg.Agent.Profile)
	a.chrome.showCost = isPaidProvider(cfg.LLM.Provider)
	a.setContextLimitFromConfig(cfg)
	a.syncStatusBar()
}

func isPaidProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "openai", "anthropic", "gemini", "google", "kimi", "moonshot", "openrouter",
		"groq", "together", "mistral", "deepseek", "xai", "fireworks", "cerebras":
		return true
	default:
		return false
	}
}

func isLSPToolName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(n, "lsp.")
}

// recordTurnUsage accumulates turn-total tokens/cost for session accounting.
// It must NOT overwrite chrome.promptTokensUsed — that field is the last per-step
// prompt size (ctx bar). Turn PromptTokens are often a sum across many steps.
func (a *App) recordTurnUsage(prompt, completion int, costUSD float64) {
	a.chrome.sessionTokens += prompt + completion
	if costUSD > 0 {
		a.chrome.sessionCostUSD += costUSD
	}
	a.syncStatusBar()
}
