package tui

import (
	"strings"

	"github.com/orchestra/orchestra/internal/config"
)

// syncStatusBar pushes App session metrics into the status bar widget.
func (a *App) syncStatusBar() {
	a.statusBar.SetProfile(a.cfg.Profile)
	a.statusBar.SetLSPStatus(a.lspStatus)
	used := a.promptTokensUsed
	if a.turn.ShowBusySpinner() && a.livePromptTokens > 0 {
		used = a.livePromptTokens
	}
	a.statusBar.SetTokens(used, a.contextLimit())
	a.statusBar.SetCostUSD(a.sessionCostUSD)
	a.statusBar.SetShowCost(a.showCost)
}

func (a *App) contextLimit() int {
	if a.modelContextLimit > 0 {
		return a.modelContextLimit
	}
	return 0
}

func (a *App) setContextLimitFromConfig(cfg *config.ProjectConfig) {
	if cfg == nil {
		return
	}
	if n := int(cfg.EffectiveNumCtx()); n > 0 {
		a.modelContextLimit = n
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
	a.showCost = isPaidProvider(cfg.LLM.Provider)
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

// recordTurnUsage accumulates turn-total tokens/cost for session accounting.
// It must NOT overwrite promptTokensUsed — that field is the last per-step
// prompt size (ctx bar). Turn PromptTokens are often a sum across many steps.
func (a *App) recordTurnUsage(prompt, completion int, costUSD float64) {
	a.sessionTokens += prompt + completion
	if costUSD > 0 {
		a.sessionCostUSD += costUSD
	}
	a.syncStatusBar()
}
