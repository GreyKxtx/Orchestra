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

	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
		a.layout()
		if a.initialized {
			a.flushChat(true)
		}
		return a, nil

	case tickMsg:
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
		var cmds []tea.Cmd
		cmds = append(cmds, a.nextTickCmd())
		// Poll install progress while LSP is installing (status bar %).
		if a.chrome.lspStatus == "installing" && a.spinFrame%8 == 0 {
			if c := a.refreshLSPStatusCmd(); c != nil {
				cmds = append(cmds, c)
			}
		}
		return a, tea.Batch(cmds...)

	case lspStatusMsg:
		if m.status != "" {
			a.chrome.lspStatus = m.status
			a.chrome.lspInstallPercent = m.percent
			a.chrome.lspInstallID = m.id
			a.syncStatusBar()
		}
		return a, nil

	case sessionCompactDoneMsg:
		a.handleSessionCompactDone(m)
		return a, nil

	case modelsLoadedMsg:
		if a.onboarding != nil {
			a.onboarding.LoadingModels = false
			if m.err != nil {
				a.onboarding.ModelError = "LM Studio РЅРµРґРѕСЃС‚СѓРїРµРЅ: " + m.err.Error()
			} else {
				a.onboarding.Models = m.models
				a.onboarding.ModelError = ""
			}
		}
		return a, nil

	case onboardingDoneMsg:
		a.showOnboarding = false
		cfg, err := config.Load(m.configPath)
		if err != nil {
			a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: "[error] failed to load config: " + err.Error()})
			a.chat.SetMessages(a.session.Messages)
			return a, nil
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
		return a, func() tea.Msg {
			ctx, cancel := context.WithCancel(context.Background())
			client, err := rpcclient.Spawn(ctx, rpcclient.Config{
				Binary:        binary,
				WorkspaceRoot: workspaceRoot,
				ProjectID:     projectID,
			})
			return rpcSpawnedMsg{client: client, cancel: cancel, err: err}
		}

	case rpcSpawnedMsg:
		if m.err != nil {
			if m.cancel != nil {
				m.cancel() // release the spawn context — otherwise it leaks
			}
			a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: "[error] failed to connect to core: " + m.err.Error()})
			a.chat.SetMessages(a.session.Messages)
			return a, nil
		}
		a.rpc = m.client
		a.rpcCancel = m.cancel
		a.rpcGen++ // invalidate any listener still attached to the old client
		a.coreSessionID = ""
		return a, tea.Batch(a.listenForEvents(), a.startCoreSession())

	case coreSessionStartedMsg:
		a.handleCoreSessionStarted(m)
		var cmds []tea.Cmd
		if a.cfg.Model != "" && a.cfg.ConfigPath != "" {
			cmds = append(cmds, a.probeStartupCmd())
		}
		if len(cmds) == 0 {
			return a, nil
		}
		return a, tea.Batch(cmds...)

	case view.ProviderDialogMsg:
		return a.handleProviderDialog(m)
	case view.EndpointDialogMsg:
		return a.handleEndpointDialog(m)
	case view.ModelDialogMsg:
		return a.handleModelDialog(m)
	case view.SettingsDialogMsg:
		return a.handleSettingsDialog(m)
	case view.OrchestraDialogMsg:
		return a.handleOrchestraDialog(m)
	case view.OrchestraSourceDialogMsg:
		return a.handleOrchestraSourceDialog(m)
	case view.SessionsDialogMsg:
		return a.handleSessionsDialog(m)
	case view.RewindDialogMsg:
		return a.handleRewindDialog(m)
	case view.MessageActionDialogMsg:
		return a.handleMessageActionDialog(m)
	case view.MCPListDialogMsg:
		return a.handleMCPListDialog(m)
	case view.MCPPresetDialogMsg:
		return a.handleMCPPresetDialog(m)
	case view.MCPEditDialogMsg:
		return a.handleMCPEditDialog(m)

	case view.ModelsLoadedMsg:
		if len(a.dialogStack) > 0 {
			if md, ok := a.dialogStack[len(a.dialogStack)-1].(*view.ModelDialog); ok {
				if m.Err != "" {
					if fallback := view.CloudModels[md.Provider().Key]; len(fallback) > 0 {
						md.SetModels(fallback)
					} else {
						md.SetLoadError(m.Err)
					}
				} else {
					md.SetModels(m.Models)
				}
			}
		}
		return a, nil

	case settingsSavedMsg:
		return a, a.applySavedSettings(m)
	case llmProbeMsg:
		return a, a.handleLLMProbe(m)
	case limitsAppliedMsg:
		if m.err != nil {
			a.showToast("ctx sync ✗ · " + m.err.Error())
			return a, nil
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
		return a, nil
	case orchestraSavedMsg:
		if m.err != nil {
			a.session.AppendMessage(state.Message{
				Role: state.RoleSystem,
				Text: "[error] save orchestra: " + m.err.Error(),
			})
			a.chat.SetMessages(a.session.Messages)
			return a, nil
		}
		a.showToast("orchestra · roles saved")
		return a, a.respawnRPCCmd()

	case mcpTestMsg:
		a.handleMCPTestMsg(m)
		return a, nil

	case tea.MouseMsg:
		return a.handleMouseMsg(m)

	case tea.KeyMsg:
		if next, cmd, handled := a.routeKey(m); handled {
			return next, cmd
		}

	case rpcEventMsg:
		if m.gen != a.rpcGen {
			return a, nil // stale listener from a pre-respawn client — drop, don't re-arm
		}
		saveCmd := a.handleRPCEvent(m.ev)
		listenCmd := a.listenForEvents()
		return a, tea.Batch(saveCmd, listenCmd)

	case rpcBatchMsg:
		if m.gen != a.rpcGen {
			return a, nil
		}
		var saveCmds []tea.Cmd
		for _, ev := range m.evs {
			if cmd := a.handleRPCEvent(ev); cmd != nil {
				saveCmds = append(saveCmds, cmd)
			}
		}
		saveCmds = append(saveCmds, a.listenForEvents())
		return a, tea.Batch(saveCmds...)

	case systemMsgMsg:
		return a, a.handleSystemMsg(m)

	case workflowResultMsg:
		return a, a.handleWorkflowResult(m)

	case skillResultMsg:
		return a, a.handleSkillResult(m)

	case memoryOpenDoneMsg:
		a.handleMemoryOpenDone(m)
		return a, nil

	case diffRevertResultMsg:
		return a, a.handleDiffRevertResult(m)

	case diffApplyResultMsg:
		return a, a.handleDiffApplyResult(m)

	case attachResultMsg:
		return a, a.handleAttachResult(m)

	case sessionRewindResultMsg:
		return a, a.handleSessionRewindResult(m)
	}

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
