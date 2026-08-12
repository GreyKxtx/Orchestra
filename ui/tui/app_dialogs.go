package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
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
			if a.orchFlow {
				a.orchPending = p.Key
				a.orchPendingP = p
			}
			// Credentials step for every provider (API key; URL if editable).
			a.pushDialog(a.newEndpointDialog(p))
			return a, nil
		}
	case "endpoint":
		switch m.Action {
		case "cancel":
			a.pendingAPIKey = ""
			a.popDialog()
			return a, nil
		case "save":
			r, ok := m.Data.(view.EndpointDialogResult)
			if !ok {
				if p, ok2 := m.Data.(view.ProviderEntry); ok2 {
					r = view.EndpointDialogResult{Provider: p}
				}
			}
			p := r.Provider
			a.pendingAPIKey = r.APIKey
			if a.orchFlow {
				a.orchPendingP = p
				a.orchPending = p.Key
			}
			if ed, ok := a.topDialog().(*view.EndpointDialog); ok {
				ed.ClearError()
			}
			a.showToast("Проверяю endpoint / API key…")
			return a, a.probeLLMCmd("endpoint", p, r.APIKey, "")
		}
	case "model":
		switch m.Action {
		case "cancel":
			a.popDialog()
			if a.orchFlow {
				a.popUntilOrchestra()
			}
			return a, nil
		case "select":
			me, _ := m.Data.(view.ModelEntry)
			if a.orchFlow {
				a.applyOrchestraRoleChoice(a.orchPending, a.orchPendingP, me.ID, a.pendingAPIKey)
				a.popUntilOrchestra()
				a.orchPending = ""
				a.pendingAPIKey = ""
				return a, nil
			}
			// Find the underlying ModelDialog to learn which provider we picked.
			var provider view.ProviderEntry
			if md, ok := a.topDialog().(*view.ModelDialog); ok {
				provider = md.Provider()
			}
			sd := view.NewSettingsDialog(provider, me)
			apiKey := a.pendingAPIKey
			if apiKey == "" {
				apiKey = a.loadProviderAPIKey(provider.Key)
			}
			// Hydrate with an existing preset for this model id, if any.
			if a.cfg.ConfigPath != "" {
				if cfg, err := config.Load(a.cfg.ConfigPath); err == nil && cfg != nil {
					timeoutS := cfg.LLM.TimeoutS
					if preset, ok := cfg.LLM.ModelPresets[me.ID]; ok {
						var thinking bool
						if preset.EnableThinking != nil {
							thinking = *preset.EnableThinking
						}
						if apiKey == "" {
							apiKey = cfg.LLM.APIKey
						}
						sd.SetInitial(preset.Temperature, preset.MaxTokens, preset.NumCtx, timeoutS, thinking, apiKey)
					} else if cfg.LLM.Model == me.ID {
						if apiKey == "" {
							apiKey = cfg.LLM.APIKey
						}
						sd.SetInitial(cfg.LLM.Temperature, cfg.LLM.MaxTokens, cfg.EffectiveNumCtx(), timeoutS, false, apiKey)
					} else {
						if apiKey == "" {
							apiKey = cfg.LLM.APIKey
						}
						sd.SetInitial(0, 0, 0, timeoutS, false, apiKey)
					}
				} else {
					sd.SetInitial(0, 0, 0, 0, false, apiKey)
				}
			} else {
				sd.SetInitial(0, 0, 0, 0, false, apiKey)
			}
			// Keep pendingAPIKey until settings save — merge if Settings field empty.
			a.pushDialog(sd)
			return a, nil
		}
	case "settings":
		switch m.Action {
		case "cancel":
			a.pendingAPIKey = ""
			a.popDialog()
			return a, nil
		case "save":
			r, _ := m.Data.(view.SettingsDialogResult)
			if strings.TrimSpace(r.APIKey) == "" {
				r.APIKey = strings.TrimSpace(a.pendingAPIKey)
			}
			a.pendingAPIKey = ""
			a.dialogStack = nil
			a.closeCommandModal()
			return a, a.persistSettingsCmd(r)
		}
	case "orchestra":
		switch m.Action {
		case "cancel":
			a.clearOrchFlow()
			a.popDialog()
			return a, nil
		case "ok":
			r, _ := m.Data.(view.OrchestraDialogResult)
			a.clearOrchFlow()
			a.dialogStack = nil
			a.closeCommandModal()
			return a, a.persistOrchestraCmd(r)
		case "pick_provider":
			idx, _ := m.Data.(int)
			od := a.findOrchestraDialog()
			if od == nil {
				return a, nil
			}
			a.orchFlow = true
			a.orchRoleIdx = idx
			a.pushDialog(view.NewOrchestraSourceDialog(od.Ctx()))
			return a, nil
		case "pick_model":
			idx, _ := m.Data.(int)
			od := a.findOrchestraDialog()
			if od == nil || idx < 0 || idx >= len(od.RolesSnapshot()) {
				return a, nil
			}
			a.orchFlow = true
			a.orchRoleIdx = idx
			role := od.RolesSnapshot()[idx]
			a.orchPending = role.Provider
			p := a.providerEntryForOrchestra(role.Provider, od.Ctx())
			a.orchPendingP = p
			return a, a.pushModelDialog(p)
		}
	case "orchestra_source":
		switch m.Action {
		case "cancel":
			a.popDialog()
			a.orchPending = ""
			return a, nil
		case "select":
			pick, _ := m.Data.(view.OrchestraSourcePick)
			if pick.IsMain {
				a.applyOrchestraRoleMain()
				a.popDialog() // source
				return a, nil
			}
			p := a.hydrateProviderEndpoint(pick.Provider)
			if p.Key == "" {
				p.Key = pick.Key
			}
			a.orchPending = pick.Key
			a.orchPendingP = p
			a.popDialog() // source
			a.pushDialog(a.newEndpointDialog(p))
			return a, nil
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
	case "rewind":
		switch m.Action {
		case "cancel":
			a.popDialog()
			return a, nil
		case "select":
			cp, _ := m.Data.(view.RewindCheckpoint)
			a.popDialog()
			a.handleRewindSelect(cp)
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
	case "mcp", "mcp_preset", "mcp_edit":
		return a.handleMCPDialogResult(m)
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

// providerReadyMap returns keys that have usable credentials on disk.
func (a *App) providerReadyMap() map[string]bool {
	ready := map[string]bool{}
	if a.cfg.ConfigPath == "" {
		return ready
	}
	cfg, err := config.Load(a.cfg.ConfigPath)
	if err != nil || cfg == nil {
		return ready
	}
	mark := func(key, apiBase, apiKey string, needsKey bool) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		if strings.TrimSpace(apiBase) == "" {
			return
		}
		if needsKey && strings.TrimSpace(apiKey) == "" {
			return
		}
		ready[key] = true
	}
	if entry, ok := view.FindProviderByKey(cfg.LLM.Provider); ok {
		base := cfg.LLM.APIBase
		if base == "" {
			base = entry.Endpoint
		}
		mark(cfg.LLM.Provider, base, cfg.LLM.APIKey, entry.NeedsKey)
	} else if cfg.LLM.Provider != "" && strings.TrimSpace(cfg.LLM.APIBase) != "" {
		ready[cfg.LLM.Provider] = true
	}
	for name, pcfg := range cfg.Providers {
		needs := false
		if cat, ok := view.FindProviderByKey(name); ok {
			needs = cat.NeedsKey
		} else if pcfg.Provider != "" {
			if cat, ok := view.FindProviderByKey(pcfg.Provider); ok {
				needs = cat.NeedsKey
			}
		}
		base := pcfg.APIBase
		if base == "" {
			if cat, ok := view.FindProviderByKey(name); ok {
				base = cat.Endpoint
			}
		}
		mark(name, base, pcfg.APIKey, needs)
	}
	return ready
}

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
	if pc, ok := cfg.Providers[p.Key]; ok && pc.APIBase != "" {
		p.Endpoint = view.NormalizeEndpoint(pc.APIBase)
	}
	return p
}

// newEndpointDialog opens URL+API-key editor with values from disk / orchestra Named.
func (a *App) newEndpointDialog(p view.ProviderEntry) *view.EndpointDialog {
	key := a.loadProviderAPIKey(p.Key)
	if a.orchFlow {
		if od := a.findOrchestraDialog(); od != nil {
			if n, ok := od.Ctx().Named[p.Key]; ok {
				if strings.TrimSpace(n.APIBase) != "" {
					p.Endpoint = view.NormalizeEndpoint(n.APIBase)
				}
				if strings.TrimSpace(n.APIKey) != "" {
					key = strings.TrimSpace(n.APIKey)
				}
			}
		}
	}
	return view.NewEndpointDialog(p, p.Endpoint, key)
}

// pushModelDialog opens model selection and fetches model lists from the API.
func (a *App) pushModelDialog(p view.ProviderEntry) tea.Cmd {
	apiKey := strings.TrimSpace(a.pendingAPIKey)
	if apiKey == "" {
		apiKey = a.loadProviderAPIKey(p.Key)
	}
	fetchRemote := p.Local || p.Key == "custom" || p.EndpointEditable || (p.NeedsKey && apiKey != "")
	a.pushDialog(view.NewModelDialog(p, fetchRemote))
	if fetchRemote {
		return view.FetchModelsCmd(p.Key, p.Endpoint, apiKey)
	}
	return nil
}

func (a *App) loadLLMAPIKey() string {
	return a.loadProviderAPIKey("")
}

// loadProviderAPIKey returns providers[key].api_key, or llm.api_key when key
// matches the active provider / is empty.
func (a *App) loadProviderAPIKey(providerKey string) string {
	if a.cfg.ConfigPath == "" {
		return ""
	}
	cfg, err := config.Load(a.cfg.ConfigPath)
	if err != nil || cfg == nil {
		return ""
	}
	providerKey = strings.TrimSpace(providerKey)
	if providerKey != "" {
		if pc, ok := cfg.Providers[providerKey]; ok {
			if k := strings.TrimSpace(pc.APIKey); k != "" {
				return k
			}
		}
		if cfg.LLM.Provider == providerKey {
			return strings.TrimSpace(cfg.LLM.APIKey)
		}
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
		// Always persist non-empty API key (incl. local/vLLM/ngrok). Never wipe an
		// existing key with an empty field from the settings dialog.
		if k := strings.TrimSpace(r.APIKey); k != "" {
			cfg.LLM.APIKey = k
		}
		cfg.LLM.Model = r.Model.ID
		cfg.LLM.Temperature = r.Temperature
		cfg.LLM.MaxTokens = r.MaxTokens
		if r.TimeoutS > 0 {
			cfg.LLM.TimeoutS = r.TimeoutS
		}
		if cfg.LLM.ExtraBody == nil {
			cfg.LLM.ExtraBody = map[string]any{}
		}
		cfg.LLM.ExtraBody["num_ctx"] = r.NumCtx
		// Always persist an explicit thinking flag. Qwen3 defaults to thinking-on;
		// deleting the key leaves empty assistant content with no tool calls.
		cfg.LLM.ExtraBody["chat_template_kwargs"] = map[string]any{"enable_thinking": r.EnableThinking}
		// Mirror into providers: so named lookups (orchestra / router) keep the key.
		if key := strings.TrimSpace(r.Provider.Key); key != "" {
			if cfg.Providers == nil {
				cfg.Providers = map[string]config.LLMConfig{}
			}
			pc := cfg.Providers[key]
			if pc.Provider == "" {
				pc.Provider = key
			}
			pc.APIBase = cfg.LLM.APIBase
			if k := strings.TrimSpace(cfg.LLM.APIKey); k != "" {
				pc.APIKey = k
			}
			pc.Model = cfg.LLM.Model
			pc.MaxTokens = cfg.LLM.MaxTokens
			pc.Temperature = cfg.LLM.Temperature
			pc.ToolChoice = cfg.LLM.ToolChoice
			if r.TimeoutS > 0 {
				pc.TimeoutS = r.TimeoutS
			}
			if pc.ExtraBody == nil {
				pc.ExtraBody = map[string]any{}
			}
			pc.ExtraBody["num_ctx"] = r.NumCtx
			pc.ExtraBody["chat_template_kwargs"] = map[string]any{"enable_thinking": r.EnableThinking}
			cfg.Providers[key] = pc
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
		return settingsSavedMsg{provider: r.Provider, model: r.Model, numCtx: r.NumCtx, maxTokens: r.MaxTokens}
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
	a.chrome.modelContextLimit = limit
	budget := llm.PromptBudgetTokens(limit, m.maxTokens)
	if budget <= 0 {
		budget = limit
	}
	a.chrome.promptBudgetTokens = budget
	a.statusBar.SetModelCtx(budget)
	a.syncStatusBar()
	a.chat.SetMeta(a.cfg.Mode, a.cfg.Model)
	msg := fmt.Sprintf("[saved] %s · %s", m.provider.Name, m.model.ID)
	if a.cfg.Binary != "" {
		msg += " — reloading core…"
	}
	a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: msg})
	a.chat.SetMessages(a.session.Messages)
	return tea.Batch(a.respawnRPCCmd(), a.probeLLMCmd("settings", m.provider, a.loadProviderAPIKey(m.provider.Key), m.model.ID))
}

// probeStartupCmd soft-checks the configured LLM after TUI connects to core.
func (a *App) probeStartupCmd() tea.Cmd {
	p := a.currentProvider()
	key := a.loadProviderAPIKey(p.Key)
	model := a.cfg.Model
	return a.probeLLMCmd("startup", p, key, model)
}

func (a *App) probeLLMCmd(phase string, p view.ProviderEntry, apiKey, model string) tea.Cmd {
	return func() tea.Msg {
		cfg := config.LLMConfig{
			Provider:  p.Key,
			APIBase:   view.NormalizeEndpoint(p.Endpoint),
			APIKey:    strings.TrimSpace(apiKey),
			Model:     strings.TrimSpace(model),
			MaxTokens: 32,
			TimeoutS:  25,
		}
		if cfg.APIKey == "" {
			cfg.APIKey = a.loadProviderAPIKey(p.Key)
		}
		kind := llm.ProbeModels
		if phase == "settings" || phase == "startup" {
			kind = llm.ProbeChat
			if cfg.Model == "" {
				cfg.Model = a.cfg.Model
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res := llm.Probe(ctx, cfg, kind)
		return llmProbeMsg{phase: phase, provider: p, apiKey: apiKey, result: res}
	}
}

// handleLLMProbe reacts to connectivity probe results.
func (a *App) handleLLMProbe(m llmProbeMsg) tea.Cmd {
	var after tea.Cmd
	if m.result.ContextTokens > 0 {
		// Persist may keep a lower user num_ctx; chrome updates via limitsAppliedMsg
		// with the *effective* window, not raw server max_model_len.
		after = a.persistDiscoveredLimitsCmd(m.result)
	}
	switch m.phase {
	case "endpoint":
		if !m.result.OK {
			msg := m.result.Err
			if m.result.Hint != "" {
				msg = m.result.Err + " · " + m.result.Hint
			}
			if ed, ok := a.topDialog().(*view.EndpointDialog); ok {
				ed.SetError(msg)
			}
			a.showToast("LLM ✗ · не пускает — исправь URL/ключ")
			return after
		}
		a.showToast(m.result.Summary())
		if a.orchFlow {
			a.popDialog() // endpoint
			return tea.Batch(after, a.pushModelDialog(m.provider))
		}
		return tea.Batch(after, a.pushModelDialog(m.provider))
	case "settings":
		if m.result.OK {
			a.showToast(m.result.Summary())
		} else {
			a.session.AppendMessage(state.Message{
				Role: state.RoleSystem,
				Text: "[warn] LLM probe: " + m.result.Summary(),
			})
			a.chat.SetMessages(a.session.Messages)
			a.showToast("Сохранено, но LLM не отвечает — проверь ключ/сервер")
		}
		return after
	case "startup":
		if m.result.OK {
			a.showToast(m.result.Summary())
			return after
		}
		a.session.AppendMessage(state.Message{
			Role: state.RoleSystem,
			Text: "[warn] LLM недоступен при старте: " + m.result.Summary() +
				"\nИсправь через /provider или orchestra llm-ping",
		})
		a.chat.SetMessages(a.session.Messages)
		a.showToast("LLM ✗ при старте — см. предупреждение")
		return after
	}
	return after
}

// persistDiscoveredLimitsCmd reconciles server max_model_len with user num_ctx
// (fill if unset, clamp if oversize, keep intentional lower windows) and
// clamps max_tokens when needed.
func (a *App) persistDiscoveredLimitsCmd(res llm.ProbeResult) tea.Cmd {
	if res.ContextTokens <= 0 {
		return nil
	}
	cfgPath := a.cfg.ConfigPath
	workspaceRoot := a.cfg.WorkspaceRoot
	ctxTok := res.ContextTokens
	return func() tea.Msg {
		cfg, err := config.Load(cfgPath)
		if err != nil || cfg == nil {
			cfg = config.DefaultConfig(workspaceRoot)
			cfg.ProjectRoot = workspaceRoot
		}
		lim := llm.ModelLimits{ContextTokens: ctxTok, MaxTokensCap: res.MaxTokensCap}
		beforeTok := cfg.LLM.MaxTokens
		beforeCtx := int(cfg.EffectiveNumCtx())
		if !llm.ApplyDiscoveredLimits(&cfg.LLM, lim) {
			return nil
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			return limitsAppliedMsg{err: err}
		}
		applied := int(cfg.EffectiveNumCtx())
		if applied <= 0 {
			applied = contextLenFromCfgExtra(cfg)
		}
		return limitsAppliedMsg{
			contextTokens: applied,
			serverMax:     ctxTok,
			maxTokens:     cfg.LLM.MaxTokens,
			clamped:       beforeTok > 0 && cfg.LLM.MaxTokens < beforeTok,
			ctxClamped:    beforeCtx > 0 && applied > 0 && beforeCtx > applied,
		}
	}
}

func contextLenFromCfgExtra(cfg *config.ProjectConfig) int {
	if cfg == nil || cfg.LLM.ExtraBody == nil {
		return 0
	}
	switch n := cfg.LLM.ExtraBody["num_ctx"].(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
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

func (a *App) openOrchestraDialog() {
	drafts := []view.OrchestraRoleDraft{
		{Key: view.OrchestraRolePlanner, Label: "L5 · Orchestrator"},
		{Key: view.OrchestraRoleComplex, Label: "L3 · Worker complex"},
		{Key: view.OrchestraRoleFocused, Label: "L3 · Worker focused"},
		{Key: view.OrchestraRoleMicro, Label: "L1 · Worker micro"},
	}
	ctx := view.OrchestraDialogCtx{Named: map[string]view.OrchestraNamedProvider{}}
	if a.cfg.ConfigPath != "" {
		if cfg, err := config.Load(a.cfg.ConfigPath); err == nil && cfg != nil {
			mainEntry, _ := view.FindProviderByKey(cfg.LLM.Provider)
			ctx.MainProvider = cfg.LLM.Provider
			ctx.MainAPIBase = cfg.LLM.APIBase
			ctx.MainAPIKey = cfg.LLM.APIKey
			ctx.MainModel = cfg.LLM.Model
			ctx.MainNeedsKey = mainEntry.NeedsKey
			ctx.FastProvider = strings.TrimSpace(cfg.LLM.Router.FastProvider)

			for name, pcfg := range cfg.Providers {
				needs := false
				label := name
				if cat, ok := view.FindProviderByKey(name); ok {
					needs = cat.NeedsKey
					label = cat.Name
				} else if pcfg.Provider != "" {
					if cat, ok := view.FindProviderByKey(pcfg.Provider); ok {
						needs = cat.NeedsKey
						label = cat.Name
					}
				}
				ctx.Named[name] = view.OrchestraNamedProvider{
					Key:        name,
					APIBase:    pcfg.APIBase,
					APIKey:     pcfg.APIKey,
					Model:      pcfg.Model,
					NeedsKey:   needs,
					Label:      label,
					Configured: true,
				}
			}
			// Seed catalog defaults into Named for status when not yet in YAML.
			for _, cat := range view.DialogProviders {
				if _, ok := ctx.Named[cat.Key]; ok {
					continue
				}
				n := view.OrchestraNamedProvider{
					Key:      cat.Key,
					APIBase:  cat.Endpoint,
					NeedsKey: cat.NeedsKey,
					Label:    cat.Name,
				}
				// Active llm: counts as configured for the green indicator.
				if cat.Key == cfg.LLM.Provider {
					n.Configured = true
					if cfg.LLM.APIBase != "" {
						n.APIBase = cfg.LLM.APIBase
					}
					n.APIKey = cfg.LLM.APIKey
					n.Model = cfg.LLM.Model
				}
				ctx.Named[cat.Key] = n
			}

			drafts[0].Provider = cfg.Orchestra.Planner.Provider
			drafts[0].Model = cfg.Orchestra.Planner.Model
			tierMap := map[string]config.OrchestraTier{}
			for _, t := range cfg.Orchestra.Tiers {
				tierMap[strings.ToLower(t.Name)] = t
			}
			for i, key := range []view.OrchestraRoleKey{view.OrchestraRoleComplex, view.OrchestraRoleFocused, view.OrchestraRoleMicro} {
				if t, ok := tierMap[string(key)]; ok {
					drafts[i+1].Provider = t.Provider
					drafts[i+1].Model = t.Model
				}
			}
		}
	}
	a.pushDialog(view.NewOrchestraDialog(drafts, ctx))
}

type orchestraSavedMsg struct {
	err error
}

func (a *App) persistOrchestraCmd(r view.OrchestraDialogResult) tea.Cmd {
	cfgPath := a.cfg.ConfigPath
	workspaceRoot := a.cfg.WorkspaceRoot
	return func() tea.Msg {
		cfg, err := config.Load(cfgPath)
		if err != nil || cfg == nil {
			cfg = config.DefaultConfig(workspaceRoot)
			cfg.ProjectRoot = workspaceRoot
		}
		if cfg.Providers == nil {
			cfg.Providers = map[string]config.LLMConfig{}
		}
		// Persist Named snapshots (URL + API key) for providers used by roles.
		for name, n := range r.Named {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			used := false
			for _, role := range r.Roles {
				if strings.TrimSpace(role.Provider) == name {
					used = true
					break
				}
			}
			if !used {
				continue
			}
			pc := cfg.Providers[name]
			if pc.Provider == "" {
				pc.Provider = name
			}
			if base := strings.TrimSpace(n.APIBase); base != "" {
				pc.APIBase = view.NormalizeEndpoint(base)
			}
			if k := strings.TrimSpace(n.APIKey); k != "" {
				pc.APIKey = k
			}
			if m := strings.TrimSpace(n.Model); m != "" {
				pc.Model = m
			}
			cfg.Providers[name] = pc
		}
		tiers := make([]config.OrchestraTier, 0, 3)
		for _, role := range r.Roles {
			prov := strings.TrimSpace(role.Provider)
			model := strings.TrimSpace(role.Model)
			// Ensure named provider exists in providers: (seed from catalog).
			if prov != "" {
				if _, ok := cfg.Providers[prov]; !ok {
					entry := config.LLMConfig{Provider: prov, Model: model}
					if cat, ok := view.FindProviderByKey(prov); ok {
						entry.APIBase = cat.Endpoint
						if cat.Key != "" && entry.Provider == "" {
							entry.Provider = cat.Key
						}
					}
					cfg.Providers[prov] = entry
				} else if model != "" {
					pc := cfg.Providers[prov]
					pc.Model = model
					cfg.Providers[prov] = pc
				}
			}
			switch role.Key {
			case view.OrchestraRolePlanner:
				cfg.Orchestra.Planner.Provider = prov
				cfg.Orchestra.Planner.Model = model
			case view.OrchestraRoleComplex, view.OrchestraRoleFocused, view.OrchestraRoleMicro:
				tiers = append(tiers, config.OrchestraTier{
					Name:     string(role.Key),
					Provider: prov,
					Model:    model,
				})
			}
		}
		if len(tiers) > 0 {
			cfg.Orchestra.Tiers = tiers
			if cfg.Orchestra.DefaultTier == "" {
				cfg.Orchestra.DefaultTier = "focused"
			}
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			return orchestraSavedMsg{err: err}
		}
		return orchestraSavedMsg{}
	}
}

func (a *App) clearOrchFlow() {
	a.orchFlow = false
	a.orchRoleIdx = -1
	a.orchPending = ""
	a.orchPendingP = view.ProviderEntry{}
	a.pendingAPIKey = ""
}

func (a *App) findOrchestraDialog() *view.OrchestraDialog {
	for i := len(a.dialogStack) - 1; i >= 0; i-- {
		if d, ok := a.dialogStack[i].(*view.OrchestraDialog); ok {
			return d
		}
	}
	return nil
}

func (a *App) popUntilOrchestra() {
	for len(a.dialogStack) > 0 {
		if _, ok := a.topDialog().(*view.OrchestraDialog); ok {
			return
		}
		a.popDialog()
	}
	a.orchFlow = false
}

func (a *App) applyOrchestraRoleMain() {
	od := a.findOrchestraDialog()
	if od == nil || a.orchRoleIdx < 0 {
		return
	}
	od.SetRole(a.orchRoleIdx, "", "")
}

func (a *App) applyOrchestraRoleChoice(key string, entry view.ProviderEntry, model, apiKey string) {
	od := a.findOrchestraDialog()
	if od == nil || a.orchRoleIdx < 0 {
		return
	}
	od.ApplyProviderChoice(a.orchRoleIdx, key, entry, model, apiKey)
}

func (a *App) providerEntryForOrchestra(key string, ctx view.OrchestraDialogCtx) view.ProviderEntry {
	key = strings.TrimSpace(key)
	if key == "" {
		return a.currentProvider()
	}
	if n, ok := ctx.Named[key]; ok {
		p, found := view.FindProviderByKey(key)
		if !found {
			p = view.ProviderEntry{
				Key:              key,
				Name:             n.Label,
				Endpoint:         n.APIBase,
				NeedsKey:         n.NeedsKey,
				Local:            true,
				EndpointEditable: true,
			}
		} else if n.APIBase != "" {
			p.Endpoint = n.APIBase
		}
		return a.hydrateProviderEndpoint(p)
	}
	if p, ok := view.FindProviderByKey(key); ok {
		return a.hydrateProviderEndpoint(p)
	}
	return a.currentProvider()
}
