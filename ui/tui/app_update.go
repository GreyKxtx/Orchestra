package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// Update routes incoming messages to the appropriate sub-handler.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok && isNoiseKey(km) {
		return a, nil
	}

	if cmd, handled := a.handleMCPPromptMsg(msg); handled {
		return a, cmd
	}
	if m, ok := msg.(skillsLoadedMsg); ok {
		a.handleSkillsLoadedMsg(m)
		return a, nil
	}
	// Each message belongs to one family; what none claims — a key no
	// router took, the textarea's own messages — reaches the textarea.
	for _, update := range []func(tea.Msg) (tea.Model, tea.Cmd, bool){
		a.updateDialogMsg,
		a.updateChromeMsg,
		a.updateCoreMsg,
		a.updateInputMsg,
	} {
		if next, cmd, handled := update(msg); handled {
			return next, cmd
		}
	}
	return a.updateTextarea(msg)
}

// updateDialogMsg is the dialogs' own messages.
func (a *App) updateDialogMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	var next tea.Model
	var cmd tea.Cmd
	switch m := msg.(type) {
	case view.ProviderDialogMsg:
		next, cmd = a.handleProviderDialog(m)
	case view.EndpointDialogMsg:
		next, cmd = a.handleEndpointDialog(m)
	case view.ModelDialogMsg:
		next, cmd = a.handleModelDialog(m)
	case view.SettingsDialogMsg:
		next, cmd = a.handleSettingsDialog(m)
	case view.OrchestraDialogMsg:
		next, cmd = a.handleOrchestraDialog(m)
	case view.OrchestraSourceDialogMsg:
		next, cmd = a.handleOrchestraSourceDialog(m)
	case view.SessionsDialogMsg:
		next, cmd = a.handleSessionsDialog(m)
	case view.RewindDialogMsg:
		next, cmd = a.handleRewindDialog(m)
	case view.MessageActionDialogMsg:
		next, cmd = a.handleMessageActionDialog(m)
	case view.MCPListDialogMsg:
		next, cmd = a.handleMCPListDialog(m)
	case view.MCPPresetDialogMsg:
		next, cmd = a.handleMCPPresetDialog(m)
	case view.MCPEditDialogMsg:
		next, cmd = a.handleMCPEditDialog(m)
	case view.ModelsLoadedMsg:
		a.handleModelsLoaded(m)
		return a, nil, true
	default:
		return a, nil, false
	}
	return next, cmd, true
}

// handleModelsLoaded hands the model dialog on top of the stack the models
// it asked for — the provider's known list when the endpoint would not say.
func (a *App) handleModelsLoaded(m view.ModelsLoadedMsg) {
	if len(a.dialogStack) == 0 {
		return
	}
	md, ok := a.dialogStack[len(a.dialogStack)-1].(*view.ModelDialog)
	if !ok {
		return
	}
	if m.Err == "" {
		md.SetModels(m.Models)
		return
	}
	if fallback := view.CloudModels[md.Provider().Key]; len(fallback) > 0 {
		md.SetModels(fallback)
	} else {
		md.SetLoadError(m.Err)
	}
}

// updateChromeMsg is the chrome's messages: the window, the ticker, the
// status bar's sources, the settings and onboarding flows.
func (a *App) updateChromeMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
		a.layout()
		if a.initialized {
			a.flushChat(true)
		}
		return a, nil, true
	case tickMsg:
		return a, a.handleTick(m), true
	case lspStatusMsg:
		if m.status != "" {
			a.chrome.lspStatus = m.status
			a.chrome.lspInstallPercent = m.percent
			a.chrome.lspInstallID = m.id
			a.syncStatusBar()
		}
		return a, nil, true
	case sessionCompactDoneMsg:
		a.handleSessionCompactDone(m)
		return a, nil, true
	case modelsLoadedMsg:
		if a.onboarding != nil {
			a.onboarding.LoadingModels = false
			if m.err != nil {
				a.onboarding.ModelError = "LM Studio недоступен: " + m.err.Error()
			} else {
				a.onboarding.Models = m.models
				a.onboarding.ModelError = ""
			}
		}
		return a, nil, true
	case onboardingDoneMsg:
		return a, a.handleOnboardingDone(m), true
	case settingsSavedMsg:
		return a, a.applySavedSettings(m), true
	case llmProbeMsg:
		return a, a.handleLLMProbe(m), true
	case limitsAppliedMsg:
		a.handleLimitsApplied(m)
		return a, nil, true
	case orchestraSavedMsg:
		if m.err != nil {
			a.session.AppendMessage(state.Message{
				Role: state.RoleSystem,
				Text: "[error] save orchestra: " + m.err.Error(),
			})
			a.chat.SetMessages(a.session.Messages)
			return a, nil, true
		}
		a.showToast("orchestra · roles saved")
		return a, a.respawnRPCCmd(), true
	case mcpTestMsg:
		a.handleMCPTestMsg(m)
		return a, nil, true
	}
	return a, nil, false
}

// handleTick advances the spinner and the cursor blink, ages the toast and
// re-arms the ticker; while a language server installs it polls the
// progress for the status bar.
func (a *App) handleTick(m tickMsg) tea.Cmd {
	a.spinFrame++
	// Cursor blink is wall-clock based (~500ms) so the cadence stays the
	// same whether the adaptive ticker runs at 100ms or 500ms.
	now := time.Time(m)
	if now.Sub(a.lastBlinkAt) >= 450*time.Millisecond {
		a.cursorBlink = !a.cursorBlink
		a.lastBlinkAt = now
	}
	a.statusBar.AdvanceSpin()
	a.chat.SetSpinFrame(a.spinFrame)
	a.flushChat(false)
	if a.toastTick > 0 {
		a.toastTick--
		if a.toastTick == 0 {
			a.toastText = ""
		}
	}
	cmds := []tea.Cmd{a.nextTickCmd()}
	if a.chrome.lspStatus == "installing" && a.spinFrame%8 == 0 {
		if c := a.refreshLSPStatusCmd(); c != nil {
			cmds = append(cmds, c)
		}
	}
	return tea.Batch(cmds...)
}

// handleOnboardingDone takes the configuration onboarding wrote and spawns
// the core against it.
func (a *App) handleOnboardingDone(m onboardingDoneMsg) tea.Cmd {
	a.showOnboarding = false
	cfg, err := config.Load(m.configPath)
	if err != nil {
		a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: "[error] failed to load config: " + err.Error()})
		a.chat.SetMessages(a.session.Messages)
		return nil
	}
	a.cfg.Model = cfg.LLM.Model
	if p, ok := view.FindProviderByKey(cfg.LLM.Provider); ok {
		a.providerName = p.Name
	}
	a.statusBar.SetModel(cfg.LLM.Model)
	a.setContextLimitFromConfig(cfg)
	a.chat.SetMeta(a.cfg.Mode, a.cfg.Model)
	a.chat.SetWelcomeInfo(a.buildWelcomeInfo())
	binary := a.cfg.Binary
	workspaceRoot := a.cfg.WorkspaceRoot
	projectID := a.cfg.ProjectID
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		client, err := rpcclient.Spawn(ctx, rpcclient.Config{
			Binary:        binary,
			WorkspaceRoot: workspaceRoot,
			ProjectID:     projectID,
		})
		return rpcSpawnedMsg{client: client, cancel: cancel, err: err}
	}
}

// handleLimitsApplied shows the context window the model was given.
func (a *App) handleLimitsApplied(m limitsAppliedMsg) {
	if m.err != nil {
		a.showToast("ctx sync ✗ · " + m.err.Error())
		return
	}
	if m.contextTokens > 0 {
		a.chrome.modelContextLimit = m.contextTokens
		budget := llm.PromptBudgetTokens(m.contextTokens, m.maxTokens)
		if budget <= 0 {
			budget = m.contextTokens
		}
		a.chrome.promptBudgetTokens = budget
		a.statusBar.SetModelCtx(budget)
		a.syncStatusBar()
	}
	switch {
	case m.ctxClamped && m.serverMax > 0:
		a.showToast(fmt.Sprintf("ctx урезан до %d (сервер max_model_len)", m.contextTokens))
	case m.clamped:
		a.showToast(fmt.Sprintf("окно %d · ответ auto %d", m.contextTokens, m.maxTokens))
	}
}

// updateCoreMsg is what comes back from the core: its connection, its
// events and the results of the calls the app made.
func (a *App) updateCoreMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch m := msg.(type) {
	case rpcSpawnedMsg:
		if m.err != nil {
			if m.cancel != nil {
				m.cancel() // release the spawn context — otherwise it leaks
			}
			a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: "[error] failed to connect to core: " + m.err.Error()})
			a.chat.SetMessages(a.session.Messages)
			return a, nil, true
		}
		a.rpc = m.client
		a.rpcCancel = m.cancel
		a.rpcGen++ // invalidate any listener still attached to the old client
		a.coreSessionID = ""
		return a, tea.Batch(a.listenForEvents(), a.startCoreSession()), true
	case coreSessionStartedMsg:
		a.handleCoreSessionStarted(m)
		if a.cfg.Model != "" && a.cfg.ConfigPath != "" {
			return a, a.probeStartupCmd(), true
		}
		return a, nil, true
	case rpcEventMsg:
		if m.gen != a.rpcGen {
			return a, nil, true // stale listener from a pre-respawn client — drop, don't re-arm
		}
		saveCmd := a.handleRPCEvent(m.ev)
		listenCmd := a.listenForEvents()
		return a, tea.Batch(saveCmd, listenCmd), true
	case rpcBatchMsg:
		if m.gen != a.rpcGen {
			return a, nil, true
		}
		var saveCmds []tea.Cmd
		for _, ev := range m.evs {
			if cmd := a.handleRPCEvent(ev); cmd != nil {
				saveCmds = append(saveCmds, cmd)
			}
		}
		saveCmds = append(saveCmds, a.listenForEvents())
		return a, tea.Batch(saveCmds...), true
	case systemMsgMsg:
		return a, a.handleSystemMsg(m), true
	case workflowResultMsg:
		return a, a.handleWorkflowResult(m), true
	case skillResultMsg:
		return a, a.handleSkillResult(m), true
	case memoryOpenDoneMsg:
		a.handleMemoryOpenDone(m)
		return a, nil, true
	case diffRevertResultMsg:
		return a, a.handleDiffRevertResult(m), true
	case diffApplyResultMsg:
		return a, a.handleDiffApplyResult(m), true
	case attachResultMsg:
		return a, a.handleAttachResult(m), true
	case sessionRewindResultMsg:
		return a, a.handleSessionRewindResult(m), true
	case sessionForkResultMsg:
		return a, a.handleSessionForkResult(m), true
	}
	return a, nil, false
}

// updateInputMsg is the mouse and the keys the router claims.
func (a *App) updateInputMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch m := msg.(type) {
	case tea.MouseMsg:
		next, cmd := a.handleMouseMsg(m)
		return next, cmd, true
	case tea.KeyMsg:
		if next, cmd, handled := a.routeKey(m); handled {
			return next, cmd, true
		}
	}
	return a, nil, false
}

// updateTextarea hands the message to the textarea: a printable key with a
// selection active replaces the selection (a paste as one chunk), anything
// else is the textarea's to handle.
func (a *App) updateTextarea(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok && a.input.HasSelection() && isPrintableKey(km) {
		// Bracketed / multi-rune paste replaces selection as one chunk.
		if km.Paste || len(km.Runes) > 1 {
			if next, cmd, handled := a.ingestPasteChunk(string(km.Runes)); handled {
				return next, cmd
			}
		}
		a.input.ReplaceSelection(string(km.Runes))
		a.input.SyncHeight(5)
		a.syncPalette()
		a.syncTurnComposing()
		a.updateStatusHints()
		a.layout()
		return a, nil
	}
	innerTA := a.input.Inner()
	updatedTA, taCmd := innerTA.Update(msg)
	*innerTA = updatedTA
	if _, isKey := msg.(tea.KeyMsg); isKey {
		a.input.ClearSelection()
		a.input.SyncHeight(5)
		a.syncPalette()
		a.syncTurnComposing()
		a.updateStatusHints()
		a.layout()
	}
	return a, taCmd
}

func (a *App) sendKeyToTA(kt tea.KeyType) tea.Cmd {
	innerTA := a.input.Inner()
	updated, cmd := innerTA.Update(tea.KeyMsg{Type: kt})
	*innerTA = updated
	return cmd
}

func (a *App) showToast(text string) {
	a.toastText = text
	a.toastTick = 15
}
