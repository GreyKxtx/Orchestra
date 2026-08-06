package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/sessionstore"
	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// handleDialogResult orchestrates the Provider → Model → Settings flow.
// Each dialog emits one of these via DialogResultMsg; we push the next dialog
// onto the stack, persist on save, or pop on cancel.
func (a *App) handleDialogResult(m view.DialogResultMsg) (tea.Model, tea.Cmd) {
	switch m.Source {
	case "provider":
		switch m.Action {
		case "cancel":
			a.popDialog()
			return a, nil
		case "select":
			p, _ := m.Data.(view.ProviderEntry)
			p = a.hydrateProviderEndpoint(p)
			if p.EndpointEditable {
				a.pushDialog(view.NewEndpointDialog(p, p.Endpoint))
				return a, nil
			}
			return a, a.pushModelDialog(p)
		}
	case "endpoint":
		switch m.Action {
		case "cancel":
			a.popDialog()
			return a, nil
		case "save":
			p, _ := m.Data.(view.ProviderEntry)
			return a, a.pushModelDialog(p)
		}
	case "model":
		switch m.Action {
		case "cancel":
			a.popDialog()
			return a, nil
		case "select":
			me, _ := m.Data.(view.ModelEntry)
			// Find the underlying ModelDialog to learn which provider we picked.
			var provider view.ProviderEntry
			if md, ok := a.topDialog().(*view.ModelDialog); ok {
				provider = md.Provider()
			}
			sd := view.NewSettingsDialog(provider, me)
			// Hydrate with an existing preset for this model id, if any.
			if a.cfg.ConfigPath != "" {
				if cfg, err := config.Load(a.cfg.ConfigPath); err == nil && cfg != nil {
					if preset, ok := cfg.LLM.ModelPresets[me.ID]; ok {
						var thinking bool
						if preset.EnableThinking != nil {
							thinking = *preset.EnableThinking
						}
						sd.SetInitial(preset.Temperature, preset.MaxTokens, preset.NumCtx, thinking, cfg.LLM.APIKey)
					} else if cfg.LLM.Model == me.ID {
						sd.SetInitial(cfg.LLM.Temperature, cfg.LLM.MaxTokens, cfg.EffectiveNumCtx(), false, cfg.LLM.APIKey)
					} else {
						sd.SetInitial(0, 0, 0, false, cfg.LLM.APIKey)
					}
				}
			}
			a.pushDialog(sd)
			return a, nil
		}
	case "settings":
		switch m.Action {
		case "cancel":
			a.popDialog()
			return a, nil
		case "save":
			r, _ := m.Data.(view.SettingsDialogResult)
			a.dialogStack = nil
			a.closeCommandModal()
			return a, a.persistSettingsCmd(r)
		}
	case "session":
		switch m.Action {
		case "cancel":
			a.popDialog()
			return a, nil
		case "select":
			id, _ := m.Data.(string)
			a.popDialog()
			a.closeCommandModal()
			return a, a.loadSession(id)
		case "delete":
			id, _ := m.Data.(string)
			_ = sessionstore.Delete(a.cfg.WorkspaceRoot, id)
			// Refresh list in-place.
			metas, _ := sessionstore.List(a.cfg.WorkspaceRoot)
			a.popDialog()
			a.pushDialog(view.NewSessionsDialog(metas))
			return a, nil
		}
	case "message_action":
		switch m.Action {
		case "cancel":
			a.popDialog()
			return a, nil
		case "copy":
			if text, ok := m.Data.(string); ok && text != "" {
				_ = clipboard.WriteAll(text)
				a.showToast("Скопировано")
			}
			a.popDialog()
			return a, nil
		case "edit":
			if text, ok := m.Data.(string); ok && text != "" {
				a.input.SetValue(text)
				a.input.SyncHeight(5)
				a.layout()
			}
			a.popDialog()
			return a, nil
		}
	}
	return a, nil
}

// closeCommandModal hides the Ctrl+K palette modal if it is currently open.
func (a *App) closeCommandModal() {
	if a.commandModal != nil {
		a.commandModal.SetActive(false)
	}
}

// pushDialog appends d to the stack.
func (a *App) pushDialog(d view.Dialog) { a.dialogStack = append(a.dialogStack, d) }

// popDialog removes the top dialog if any.
func (a *App) popDialog() {
	if n := len(a.dialogStack); n > 0 {
		a.dialogStack = a.dialogStack[:n-1]
	}
}

// topDialog returns the dialog at the top of the stack, or nil.
func (a *App) topDialog() view.Dialog {
	if n := len(a.dialogStack); n > 0 {
		return a.dialogStack[n-1]
	}
	return nil
}

// hydrateProviderEndpoint loads saved api_base when re-selecting the same provider.
func (a *App) hydrateProviderEndpoint(p view.ProviderEntry) view.ProviderEntry {
	if a.cfg.ConfigPath == "" {
		return p
	}
	cfg, err := config.Load(a.cfg.ConfigPath)
	if err != nil || cfg == nil {
		return p
	}
	if cfg.LLM.Provider == p.Key && cfg.LLM.APIBase != "" {
		p.Endpoint = view.NormalizeEndpoint(cfg.LLM.APIBase)
	}
	return p
}

// pushModelDialog opens model selection and fetches model lists from the API.
func (a *App) pushModelDialog(p view.ProviderEntry) tea.Cmd {
	apiKey := a.loadLLMAPIKey()
	fetchRemote := p.Local || p.Key == "custom" || (p.NeedsKey && apiKey != "")
	a.pushDialog(view.NewModelDialog(p, fetchRemote))
	if fetchRemote {
		return view.FetchModelsCmd(p.Key, p.Endpoint, apiKey)
	}
	return nil
}

func (a *App) loadLLMAPIKey() string {
	if a.cfg.ConfigPath == "" {
		return ""
	}
	cfg, err := config.Load(a.cfg.ConfigPath)
	if err != nil || cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.LLM.APIKey)
}

// openModelDialogForCurrentProvider is the /model entry point: push a
// ModelDialog using the currently configured provider (read from disk so
// the most recent /provider choice wins).
func (a *App) openModelDialogForCurrentProvider() tea.Cmd {
	provider := a.currentProvider()
	return a.pushModelDialog(provider)
}

// currentProvider determines the active provider from .orchestra.yml.
func (a *App) currentProvider() view.ProviderEntry {
	savedBase := ""
	savedKey := ""
	if a.cfg.ConfigPath != "" {
		if cfg, err := config.Load(a.cfg.ConfigPath); err == nil && cfg != nil {
			savedKey = cfg.LLM.Provider
			savedBase = cfg.LLM.APIBase
		}
	}
	if savedKey != "" {
		return view.ProviderWithSavedEndpoint(savedKey, savedBase)
	}
	if savedBase != "" {
		for _, p := range view.DialogProviders {
			if view.NormalizeEndpoint(p.Endpoint) == view.NormalizeEndpoint(savedBase) {
				p.Endpoint = view.NormalizeEndpoint(savedBase)
				return p
			}
		}
		return view.ProviderEntry{
			Key:              "custom",
			Name:             "Custom (OpenAI-compatible)",
			Category:         "Other",
			Endpoint:         view.NormalizeEndpoint(savedBase),
			Local:            true,
			EndpointEditable: true,
		}
	}
	return view.DialogProviders[0]
}

// persistSettingsCmd writes the chosen provider/model/settings to
// .orchestra.yml and stores them as a preset keyed by model id, so future
// returns to this model auto-restore tuning.
func (a *App) persistSettingsCmd(r view.SettingsDialogResult) tea.Cmd {
	cfgPath := a.cfg.ConfigPath
	workspaceRoot := a.cfg.WorkspaceRoot
	return func() tea.Msg {
		cfg, err := config.Load(cfgPath)
		if err != nil || cfg == nil {
			cfg = config.DefaultConfig(workspaceRoot)
			cfg.ProjectRoot = workspaceRoot
		}
		cfg.LLM.Provider = r.Provider.Key
		cfg.LLM.APIBase = view.NormalizeEndpoint(r.Provider.Endpoint)
		if r.Provider.NeedsKey {
			cfg.LLM.APIKey = r.APIKey
		}
		cfg.LLM.Model = r.Model.ID
		cfg.LLM.Temperature = r.Temperature
		cfg.LLM.MaxTokens = r.MaxTokens
		if cfg.LLM.ExtraBody == nil {
			cfg.LLM.ExtraBody = map[string]any{}
		}
		cfg.LLM.ExtraBody["num_ctx"] = r.NumCtx
		if r.EnableThinking {
			cfg.LLM.ExtraBody["chat_template_kwargs"] = map[string]any{"enable_thinking": true}
		} else {
			delete(cfg.LLM.ExtraBody, "chat_template_kwargs")
		}
		if cfg.LLM.ModelPresets == nil {
			cfg.LLM.ModelPresets = map[string]config.ModelPreset{}
		}
		thinkingVal := r.EnableThinking
		cfg.LLM.ModelPresets[r.Model.ID] = config.ModelPreset{
			Provider:       r.Provider.Key,
			APIBase:        view.NormalizeEndpoint(r.Provider.Endpoint),
			Temperature:    r.Temperature,
			MaxTokens:      r.MaxTokens,
			NumCtx:         r.NumCtx,
			EnableThinking: &thinkingVal,
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			return settingsSavedMsg{err: err}
		}
		return settingsSavedMsg{provider: r.Provider, model: r.Model, numCtx: r.NumCtx}
	}
}

// applySavedSettings updates in-memory state after a successful settings save
// and returns a tea.Cmd that respawns the core subprocess so the new model
// applies immediately. Returns nil for echo mode (no Binary) or on save error.
func (a *App) applySavedSettings(m settingsSavedMsg) tea.Cmd {
	if m.err != nil {
		a.session.AppendMessage(state.Message{
			Role: state.RoleSystem,
			Text: "[error] save config: " + m.err.Error(),
		})
		a.chat.SetMessages(a.session.Messages)
		return nil
	}
	a.cfg.Model = m.model.ID
	a.statusBar.SetModel(m.model.ID)
	limit := int(m.numCtx)
	if limit <= 0 {
		limit = int(m.model.MaxContextLength)
	}
	a.modelContextLimit = limit
	a.statusBar.SetModelCtx(limit)
	a.syncStatusBar()
	a.chat.SetMeta(a.cfg.Mode, a.cfg.Model)
	msg := fmt.Sprintf("[saved] %s · %s", m.provider.Name, m.model.ID)
	if a.cfg.Binary != "" {
		msg += " — reloading core…"
	}
	a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: msg})
	a.chat.SetMessages(a.session.Messages)
	return a.respawnRPCCmd()
}

// respawnRPCCmd shuts down the active core subprocess and spawns a fresh one
// against the just-saved config. Visible chat history stays; the agent run
// loop is reset and any in-flight tool calls are abandoned.
func (a *App) respawnRPCCmd() tea.Cmd {
	if a.cfg.Binary == "" {
		return nil
	}
	oldRPC := a.rpc
	oldCancel := a.rpcCancel
	a.rpc = nil
	a.rpcCancel = nil
	a.coreSessionID = ""
	// In-flight agent activity is killed by the close; reset UI state so the
	// header/status bar don't spin forever after respawn.
	if a.turn.IsRunning() {
		a.resetTurnFSM()
		a.chat.SetStreamCursor(false)
		a.session.FinishAssistant()
	}
	a.lastCommitDiff = nil
	a.permModal = nil

	binary := a.cfg.Binary
	workspaceRoot := a.cfg.WorkspaceRoot
	projectID := a.cfg.ProjectID
	return func() tea.Msg {
		if oldRPC != nil {
			_ = oldRPC.Close()
		}
		if oldCancel != nil {
			oldCancel()
		}
		ctx, cancel := context.WithCancel(context.Background())
		client, err := rpcclient.Spawn(ctx, rpcclient.Config{
			Binary:        binary,
			WorkspaceRoot: workspaceRoot,
			ProjectID:     projectID,
		})
		return rpcSpawnedMsg{client: client, cancel: cancel, err: err}
	}
}
