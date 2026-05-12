package view

import (
	tea "github.com/charmbracelet/bubbletea"
)

// ProviderEntry describes one selectable LLM provider.
type ProviderEntry struct {
	Key      string // "lmstudio", "ollama", "openai", "anthropic", "custom"
	Name     string // display name
	Endpoint string // default endpoint
	NeedsKey bool   // requires API key (cloud)
	Local    bool   // can fetch model list via /v1/models without key
}

// DialogProviders is the canonical provider list shown in ProviderDialog.
var DialogProviders = []ProviderEntry{
	{Key: "lmstudio", Name: "LM Studio", Endpoint: "http://localhost:1234", NeedsKey: false, Local: true},
	{Key: "ollama", Name: "Ollama", Endpoint: "http://localhost:11434", NeedsKey: false, Local: true},
	{Key: "openai", Name: "OpenAI", Endpoint: "https://api.openai.com/v1", NeedsKey: true, Local: false},
	{Key: "anthropic", Name: "Anthropic", Endpoint: "https://api.anthropic.com", NeedsKey: true, Local: false},
}

// ProviderDialog is the first dialog in the /provider flow: picks a provider.
type ProviderDialog struct {
	cursor int
}

// NewProviderDialog constructs a ProviderDialog with cursor at 0.
func NewProviderDialog() *ProviderDialog { return &ProviderDialog{} }

// Update implements Dialog.
func (d *ProviderDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	switch km.String() {
	case "up", "ctrl+p":
		if d.cursor > 0 {
			d.cursor--
		}
	case "down", "ctrl+n":
		if d.cursor < len(DialogProviders)-1 {
			d.cursor++
		}
	case "esc", "left":
		return d, dialogResultCmd("provider", "cancel", nil)
	case "enter", "right":
		return d, dialogResultCmd("provider", "select", DialogProviders[d.cursor])
	}
	return d, nil
}

// Render implements Dialog.
func (d *ProviderDialog) Render(screenW, screenH int) string {
	items := make([]listDialogItem, 0, len(DialogProviders))
	for _, p := range DialogProviders {
		desc := p.Endpoint
		if p.NeedsKey {
			desc += "  (needs API key)"
		}
		items = append(items, listDialogItem{
			Title:       p.Name,
			Description: desc,
		})
	}
	return renderListDialog(
		"Select provider",
		items,
		d.cursor,
		"",
		"",
		"↑↓ navigate · Enter/→ select · Esc/← back",
		screenW, screenH,
	)
}
