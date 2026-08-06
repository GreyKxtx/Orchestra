package tui

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/sessionstore"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// renderWelcomeView renders the OpenCode-style centered welcome layout.
// Layout (centered both axes, except status bar at bottom):
//
//	{ASCII logo}
//	{empty}
//	┃ {textarea}                            ← grey box, slim (textarea only)
//	{mode · model · provider}               ← below the box, opencode style
//	          {tab agents  ctrl+k commands} ← right-aligned hints
//	{cwd:branch}                {version}   ← thin status bar at very bottom
func (a *App) renderWelcomeView() string {
	t := view.ThemeForApp()

	// Box width — wider than before (~80), clamped to terminal width.
	boxWidth := 80
	if a.width < boxWidth+8 {
		boxWidth = a.width - 8
	}
	if boxWidth < 40 {
		boxWidth = 40
	}

	// Logo — centered.
	logo := view.RenderWelcomeLogo()

	// Reuse the shared boxed-input renderer — input box already contains the
	// mode/model/provider status row inside it.
	inputBox := a.renderInputBox(boxWidth)

	// Right-aligned hints below box.
	hintsMuted := lipgloss.NewStyle().Foreground(t.TextMuted())
	hintsBold := lipgloss.NewStyle().Foreground(t.Text()).Bold(true)
	hintsText := hintsBold.Render("tab") + hintsMuted.Render(" agents  ") +
		hintsBold.Render("ctrl+k") + hintsMuted.Render(" commands")
	hints := lipgloss.NewStyle().Width(boxWidth).Padding(0, 2).Align(lipgloss.Right).Render(hintsText)

	// Slash/mention palette appears ABOVE the input box (when active).
	var paletteView string
	switch {
	case a.paletteActive && len(a.slashPalette.Items) > 0:
		a.slashPalette.SetSize(boxWidth)
		paletteView = a.slashPalette.Render()
	case a.mentionActive && len(a.mentionPalette.Items) > 0:
		a.mentionPalette.SetSize(boxWidth)
		paletteView = a.mentionPalette.Render()
	}
	defer a.slashPalette.SetSize(a.width - 2*chatSidePad)
	defer a.mentionPalette.SetSize(a.width - 2*chatSidePad)

	parts := []string{logo, "", ""}
	if paletteView != "" {
		parts = append(parts, paletteView)
	}
	parts = append(parts, inputBox, hints)
	block := lipgloss.JoinVertical(lipgloss.Center, parts...)

	contentH := a.height - 2
	if contentH < 1 {
		contentH = 1
	}
	centered := lipgloss.Place(a.width, contentH, lipgloss.Center, lipgloss.Center, block)
	bottom := a.welcomeBottomBar()

	out := centered + "\n" + bottom

	// Command modal overlays everything else.
	if a.commandModal != nil && a.commandModal.Active() {
		a.commandModal.SetScreenSize(a.width, a.height)
		return a.commandModal.Render()
	}
	return out
}

// welcomeModeLine renders "<mode> · <model> · <provider>". When bg is non-zero
// every span is filled with that background (so the line sits flush inside a
// panel); when bg is the zero color the line uses the main terminal bg.
func (a *App) welcomeModeLine() string {
	return a.modeProviderLine(view.ThemeForApp().BackgroundSecondary())
}

// modeProviderLine — internal helper for the mode·model·provider line; bg
// applied to every span (pass the zero color to skip background painting).
func (a *App) modeProviderLine(bg lipgloss.Color) string {
	t := view.ThemeForApp()

	mode := a.cfg.Mode
	if mode == "" {
		mode = "build"
	}
	model := a.cfg.Model
	if model == "" {
		model = "no model"
	}
	provider := a.providerDisplayName()

	base := lipgloss.NewStyle()
	if bg != "" {
		base = base.Background(bg)
	}
	modeStyle := base.Foreground(view.ModeColor(mode)).Bold(true)
	modelStyle := base.Foreground(t.Text()).Bold(true)
	muted := base.Foreground(t.TextMuted())
	dot := muted.Render(" · ")

	return modeStyle.Render(mode) + dot + modelStyle.Render(model) + dot + muted.Render(provider) + dot + a.shellPermsSpan(muted)
}

// shellPermsSpan renders shell permission mode for the input-box status row.
// Labels follow the usual CLI wording (ask vs allow), not the internal tool name.
func (a *App) shellPermsSpan(muted lipgloss.Style) string {
	t := view.ThemeForApp()
	if a.allowExec {
		return lipgloss.NewStyle().Foreground(t.Success()).Render("shell · allow")
	}
	return muted.Render("shell · ask")
}

// providerDisplayName returns a human-readable provider name for the welcome
// line, derived from the saved config.
func (a *App) providerDisplayName() string {
	if a.cfg.ConfigPath != "" {
		if cfg, err := config.Load(a.cfg.ConfigPath); err == nil && cfg != nil {
			if p, ok := view.FindProviderByKey(cfg.LLM.Provider); ok {
				return p.Name
			}
		}
	}
	return "LM Studio"
}

// padLinesBg appends bg-styled space padding to each line so the visible
// width is exactly `width` cells.
func padLinesBg(s string, width int, bgColor lipgloss.Color) string {
	pad := lipgloss.NewStyle().Background(bgColor)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		visW := lipgloss.Width(l)
		if visW >= width {
			continue
		}
		lines[i] = l + pad.Render(strings.Repeat(" ", width-visW))
	}
	return strings.Join(lines, "\n")
}

// welcomeBottomBar — minimal status line with side padding:
// "  <project name>                                              v0.6  "
func (a *App) welcomeBottomBar() string {
	t := view.ThemeForApp()
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())

	left := a.cfg.CWD
	if left == "" {
		left = filepath.Base(a.cfg.WorkspaceRoot)
	}
	if left == "" {
		left = "."
	}
	right := view.AppVersion()

	const sidePad = 2
	leftR := muted.Render(left)
	rightR := muted.Render(right)

	inner := a.width - sidePad*2
	mid := inner - lipgloss.Width(leftR) - lipgloss.Width(rightR)
	if mid < 1 {
		mid = 1
	}
	return strings.Repeat(" ", sidePad) + leftR +
		strings.Repeat(" ", mid) +
		rightR + strings.Repeat(" ", sidePad)
}

// buildWelcomeInfo constructs the metadata shown on the welcome screen.
func (a *App) buildWelcomeInfo() view.WelcomeInfo {
	projectPath := a.cfg.WorkspaceRoot
	projectName := a.cfg.CWD
	if projectName == "" && projectPath != "" {
		projectName = filepath.Base(projectPath)
	}
	return view.WelcomeInfo{
		ProjectPath:  projectPath,
		ProjectName:  projectName,
		ModelName:    a.cfg.Model,
		SessionCount: countSessions(projectPath),
	}
}

// countSessions returns the number of saved TUI chat sessions under
// .orchestra/sessions/.
func countSessions(workspaceRoot string) int {
	metas, err := sessionstore.List(workspaceRoot)
	if err != nil {
		return 0
	}
	return len(metas)
}
