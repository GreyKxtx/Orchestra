package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// updateOnboarding routes keyboard input through the 3-step provider/model/
// settings wizard shown on first launch (or after the user wipes their
// config). Esc walks back a step, Enter advances; ↑/↓ select within the
// current step, ←/→ adjusts sliders on the Settings step.
func (a *App) updateOnboarding(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	ob := a.onboarding
	switch m.String() {
	case "ctrl+c":
		return a, tea.Quit
	case "esc":
		if ob.Step == view.OnboardingProvider {
			return a, nil // can't go back from first step
		}
		ob.Step--
		return a, nil
	case "up":
		switch ob.Step {
		case view.OnboardingProvider:
			ob.ProviderCursorUp()
		case view.OnboardingModel:
			ob.ModelCursorUp()
		case view.OnboardingSettings:
			ob.SettingsCursorUp()
		}
	case "down":
		switch ob.Step {
		case view.OnboardingProvider:
			ob.ProviderCursorDown()
		case view.OnboardingModel:
			ob.ModelCursorDown()
		case view.OnboardingSettings:
			ob.SettingsCursorDown()
		}
	case "left":
		if ob.Step == view.OnboardingSettings {
			ob.AdjustSetting(-1)
		}
	case "right":
		if ob.Step == view.OnboardingSettings {
			ob.AdjustSetting(+1)
		}
	case "enter":
		switch ob.Step {
		case view.OnboardingProvider:
			p := ob.SelectedProvider()
			endpoint := p.Endpoint
			if p.Name == "Custom" {
				endpoint = ob.CustomEndpoint
			}
			ob.Step = view.OnboardingModel
			ob.LoadingModels = true
			ob.ModelError = ""
			return a, fetchModelsCmd(endpoint)
		case view.OnboardingModel:
			if len(ob.Models) > 0 {
				sel := ob.SelectedModel()
				if sel.MaxContextLength > 0 {
					ob.Settings.NumCtx = sel.MaxContextLength
				}
				ob.Step = view.OnboardingSettings
			}
		case view.OnboardingSettings:
			return a, a.saveOnboardingConfig()
		}
	default:
		// Custom URL typing when editing custom endpoint.
		if ob.Step == view.OnboardingProvider && ob.IsEditingCustom() {
			if m.String() == "backspace" {
				ob.BackspaceCustomEndpoint()
			} else if len(m.Runes) == 1 {
				ob.TypeCustomEndpoint(m.Runes[0])
			}
		}
	}
	return a, nil
}

// saveOnboardingConfig writes the selected model and settings to .orchestra.yml.
func (a *App) saveOnboardingConfig() tea.Cmd {
	ob := a.onboarding
	sel := ob.SelectedModel()
	p := ob.SelectedProvider()
	endpoint := p.Endpoint
	if p.Name == "Custom" {
		endpoint = ob.CustomEndpoint
	}
	cfgPath := a.cfg.ConfigPath
	workspaceRoot := a.cfg.WorkspaceRoot

	return func() tea.Msg {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			cfg = config.DefaultConfig(workspaceRoot)
			cfg.ProjectRoot = workspaceRoot
		}
		cfg.LLM.APIBase = endpoint
		cfg.LLM.Model = sel.ID
		cfg.LLM.Temperature = ob.Settings.Temperature
		cfg.LLM.MaxTokens = ob.Settings.MaxTokens
		if cfg.LLM.ExtraBody == nil {
			cfg.LLM.ExtraBody = map[string]any{}
		}
		cfg.LLM.ExtraBody["num_ctx"] = ob.Settings.NumCtx
		if ob.Settings.EnableThinking {
			cfg.LLM.ExtraBody["chat_template_kwargs"] = map[string]any{"enable_thinking": true}
		} else {
			delete(cfg.LLM.ExtraBody, "chat_template_kwargs")
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			return modelsLoadedMsg{err: fmt.Errorf("save config: %w", err)}
		}
		return onboardingDoneMsg{configPath: cfgPath}
	}
}
