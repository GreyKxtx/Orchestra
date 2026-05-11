package tui

import (
	"context"
	"fmt"

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
			// If the picked provider matches the user's saved one, prefer
			// the saved endpoint over the hardcoded default so model
			// listing hits the user's actual server.
			if a.cfg.ConfigPath != "" {
				if cfg, err := config.Load(a.cfg.ConfigPath); err == nil && cfg != nil {
					if cfg.LLM.Provider == p.Key && cfg.LLM.APIBase != "" {
						p.Endpoint = cfg.LLM.APIBase
					}
				}
			}
			a.pushDialog(view.NewModelDialog(p))
			if p.Local {
				return a, view.FetchModelsCmd(p.Key, p.Endpoint)
			}
			return a, nil
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
			a.loadSession(id)
			return a, nil
		case "delete":
			id, _ := m.Data.(string)
			_ = sessionstore.Delete(a.cfg.WorkspaceRoot, id)
			// Refresh list in-place.
			metas, _ := sessionstore.List(a.cfg.WorkspaceRoot)
			a.popDialog()
			a.pushDialog(view.NewSessionsDialog(metas))
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

// openModelDialogForCurrentProvider is the /model entry point: push a
// ModelDialog using the currently configured provider (read from disk so
// the most recent /provider choice wins).
func (a *App) openModelDialogForCurrentProvider() tea.Cmd {
	provider := a.currentProvider()
	a.pushDialog(view.NewModelDialog(provider))
	if provider.Local {
		return view.FetchModelsCmd(provider.Key, provider.Endpoint)
	}
	return nil
}

// currentProvider determines the active provider from .orchestra.yml.
// Falls back to LM Studio defaults when nothing is configured.
func (a *App) currentProvider() view.ProviderEntry {
	defaultProvider := view.DialogProviders[0] // LM Studio
	if a.cfg.ConfigPath == "" {
		return defaultProvider
	}
	cfg, err := config.Load(a.cfg.ConfigPath)
	if err != nil || cfg == nil {
		return defaultProvider
	}
	for _, p := range view.DialogProviders {
		if p.Key == cfg.LLM.Provider {
			if cfg.LLM.APIBase != "" {
				p.Endpoint = cfg.LLM.APIBase
			}
			return p
		}
	}
	// No exact match — guess from the endpoint.
	if cfg.LLM.APIBase != "" {
		for _, p := range view.DialogProviders {
			if p.Endpoint == cfg.LLM.APIBase {
				return p
			}
		}
	}
	return defaultProvider
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
		cfg.LLM.APIBase = r.Provider.Endpoint
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
			APIBase:        r.Provider.Endpoint,
			Temperature:    r.Temperature,
			MaxTokens:      r.MaxTokens,
			NumCtx:         r.NumCtx,
			EnableThinking: &thinkingVal,
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			return settingsSavedMsg{err: err}
		}
		return settingsSavedMsg{provider: r.Provider, model: r.Model}
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
	a.statusBar.SetModelCtx(int(m.model.MaxContextLength))
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
	// In-flight agent activity is killed by the close; reset UI state so the
	// header/status bar don't spin forever after respawn.
	if a.agentBusy {
		a.agentBusy = false
		a.statusBar.SetAgentBusy(false)
		a.chat.SetAgentBusy(false)
		a.chat.SetStreamCursor(false)
		a.session.FinishAssistant()
	}
	a.pendingOps = nil
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
