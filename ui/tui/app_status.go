package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// chromeMetrics groups status-bar / session gauges owned by App (not chat history).
type chromeMetrics struct {
	sessionTokens     int     // prompt+completion across the whole session
	promptTokensUsed  int     // last LLM step prompt size — drives the ctx bar
	livePromptTokens  int     // current step prompt tokens while a turn is running
	tokensEstimated   bool    // true when last ctx figure is agent estimate, not provider usage
	sessionCostUSD    float64 // accumulated spend (paid providers)
	modelContextLimit int     // full model window (num_ctx / max_model_len)
	promptBudgetTokens int    // prompt room after max_tokens reserve — ctx bar denominator
	lspStatus         string  // off | idle | installing | active
	lspInstallPercent int
	lspInstallID      string
	showCost          bool
}

// syncStatusBar pushes App session metrics into the status bar widget.
func (a *App) syncStatusBar() {
	a.statusBar.SetProfile(a.cfg.Profile)
	a.statusBar.SetLSPStatus(a.chrome.lspStatus)
	a.statusBar.SetLSPProgress(a.chrome.lspInstallPercent, a.chrome.lspInstallID)
	used := a.chrome.promptTokensUsed
	if a.turn.ShowBusySpinner() && a.chrome.livePromptTokens > 0 {
		used = a.chrome.livePromptTokens
	}
	a.statusBar.SetTokens(used, a.contextLimit())
	a.statusBar.SetTokensEstimated(a.chrome.tokensEstimated)
	a.statusBar.SetCostUSD(a.chrome.sessionCostUSD)
	a.statusBar.SetShowCost(a.chrome.showCost)
}

// refreshOrchestraPhase reads the SDLC phase from .orchestra/state.md for the
// status-bar badge. Missing or unreadable state clears the badge (the phase
// machine is inactive for pre-vNext projects).
func (a *App) refreshOrchestraPhase() {
	root := strings.TrimSpace(a.cfg.WorkspaceRoot)
	if root == "" {
		root = strings.TrimSpace(a.cfg.CWD)
	}
	if root == "" {
		a.statusBar.SetPhase("")
		return
	}
	st, found, err := orchestrastate.Load(root)
	if err != nil || !found || st == nil {
		a.statusBar.SetPhase("")
		return
	}
	a.statusBar.SetPhase(string(st.Phase))
}

type lspStatusMsg struct {
	status  string
	percent int
	id      string
}

// refreshLSPStatusCmd polls core.health once for current lsp_status.
func (a *App) refreshLSPStatusCmd() tea.Cmd {
	rpc := a.rpc
	if rpc == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		st, pct, id, err := rpc.QueryLSPStatusDetail(ctx)
		if err != nil {
			return nil
		}
		return lspStatusMsg{status: st, percent: pct, id: id}
	}
}

// awaitLSPWarmupCmd polls health until active/off or ~10s — core WarmupLSP
// starts servers asynchronously after initialize. While installing, keeps
// polling so progress % can update the status bar.
func (a *App) awaitLSPWarmupCmd() tea.Cmd {
	rpc := a.rpc
	if rpc == nil {
		return nil
	}
	return func() tea.Msg {
		deadline := time.Now().Add(10 * time.Second)
		last := lspStatusMsg{}
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			st, pct, id, err := rpc.QueryLSPStatusDetail(ctx)
			cancel()
			if err == nil && st != "" {
				last = lspStatusMsg{status: st, percent: pct, id: id}
				if st == "active" || st == "off" {
					return last
				}
			}
			if time.Now().After(deadline) {
				if last.status != "" {
					return last
				}
				return nil
			}
			time.Sleep(400 * time.Millisecond)
		}
	}
}

func (a *App) contextLimit() int {
	// Ctx bar % must match compaction pressure (PromptBudgetTokens), not the
	// raw model window — otherwise 24k/122k looks like 19% while max_tokens
	// has already reserved most of the window for completion.
	if a.chrome.promptBudgetTokens > 0 {
		return a.chrome.promptBudgetTokens
	}
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
		a.chrome.promptBudgetTokens = llm.PromptBudgetTokens(n, cfg.LLM.MaxTokens)
		a.statusBar.SetModelCtx(a.chrome.promptBudgetTokens)
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
	if p, ok := view.FindProviderByKey(cfg.LLM.Provider); ok {
		a.providerName = p.Name
	} else if key := strings.TrimSpace(cfg.LLM.Provider); key != "" {
		a.providerName = key
	}
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
	// The agent may have advanced the SDLC phase during the turn.
	a.refreshOrchestraPhase()
	a.syncStatusBar()
}
