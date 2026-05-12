package view

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// PanelOpts controls how panelBlock styles its output. Empty fields fall back
// to sensible defaults (panel bg + text fg + 1×2 padding).
type PanelOpts struct {
	Width    int             // 0 = auto-fit
	Padding  [2]int          // [vertical, horizontal]; default [1,2]
	Accent   lipgloss.Color  // border color; "" = theme.Background()
	TextFG   lipgloss.Color  // body text fg; "" = theme.Text()
	NoBorder bool            // drop the thick left bar entirely
}

// panelBlock wraps content in the panel style shared by user-message and
// block-tool: BackgroundSecondary fill, optional thick ┃ left border in the
// accent color, padded interior. All callers that used to inline a
// `BorderLeft + ThickBorder + Background + Padding` chain should route through
// here.
func panelBlock(content string, opts PanelOpts) string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	textFG := opts.TextFG
	if textFG == "" {
		textFG = t.Text()
	}
	accent := opts.Accent
	if accent == "" {
		accent = t.Background()
	}
	padV, padH := opts.Padding[0], opts.Padding[1]
	if padV == 0 && padH == 0 {
		padV, padH = 1, 2
	}

	style := lipgloss.NewStyle().
		Background(bg).
		Foreground(textFG).
		Padding(padV, padH)
	if !opts.NoBorder {
		style = style.
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(accent).
			BorderBackground(bg)
	}
	if opts.Width > 0 {
		style = style.Width(opts.Width)
	}
	return style.Render(content)
}
