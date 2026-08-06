package view

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// EndpointDialog lets the user edit the server URL for local/custom providers.
type EndpointDialog struct {
	provider ProviderEntry
	url      string
}

// NewEndpointDialog pre-fills the URL from saved config or catalog default.
func NewEndpointDialog(provider ProviderEntry, savedURL string) *EndpointDialog {
	url := provider.Endpoint
	if savedURL != "" {
		url = savedURL
	}
	if provider.Key == "custom" && url == "" {
		url = "http://localhost:11434"
	}
	return &EndpointDialog{provider: provider, url: url}
}

// Update implements Dialog.
func (d *EndpointDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	switch km.String() {
	case "esc", "left":
		return d, dialogResultCmd("endpoint", "cancel", nil)
	case "enter", "right":
		p := d.provider
		p.Endpoint = NormalizeEndpoint(d.url)
		if p.Endpoint == "" {
			return d, nil
		}
		return d, dialogResultCmd("endpoint", "save", p)
	case "backspace":
		if len(d.url) > 0 {
			d.url = d.url[:len(d.url)-1]
		}
	default:
		if len(km.Runes) == 1 {
			d.url += string(km.Runes[0])
		}
	}
	return d, nil
}

// Render implements Dialog.
func (d *EndpointDialog) Render(screenW, screenH int) string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	modalW := screenW * 70 / 100
	if modalW < 56 {
		modalW = 56
	}
	if modalW > 90 {
		modalW = 90
	}
	if maxW := screenW - 4; modalW > maxW {
		modalW = maxW
	}
	inner := modalW - 8

	base := lipgloss.NewStyle().Background(bg)
	titleStyle := base.Foreground(t.Text()).Bold(true)
	muted := base.Foreground(t.TextMuted())
	inputStyle := base.Foreground(t.Primary())
	cursor := inputStyle.Render("▋")

	hint := "Examples: http://localhost:1234 · http://192.168.1.10:11434"
	if d.provider.Key == "ollama" {
		hint = "Default: http://localhost:11434  (or remote Ollama host:port)"
	}
	if d.provider.Key == "lmstudio" {
		hint = "Default: http://localhost:1234  (LM Studio → Developer → Server)"
	}

	urlLine := d.url + cursor
	if d.url == "" {
		urlLine = muted.Render("http://") + cursor
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		base.Render(strings.Repeat(" ", inner)),
		titleStyle.Render("Server URL — "+d.provider.Name),
		base.Render(strings.Repeat(" ", inner)),
		muted.Render("  Paste or type the OpenAI-compatible base URL:"),
		fitDialogLine(inputStyle.Render("  "+urlLine), inner, bg),
		base.Render(strings.Repeat(" ", inner)),
		fitDialogLine(muted.Render("  "+hint), inner, bg),
		base.Render(strings.Repeat(" ", inner)),
		fitDialogLine(muted.Render("  Enter save · Esc back"), inner, bg),
		base.Render(strings.Repeat(" ", inner)),
	)

	box := lipgloss.NewStyle().Background(bg).Padding(0, 4).Width(modalW).Render(body)
	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, box)
}

func fitDialogLine(s string, inner int, bg lipgloss.Color) string {
	pad := inner - lipgloss.Width(s)
	if pad > 0 {
		s += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", pad))
	}
	return s
}
