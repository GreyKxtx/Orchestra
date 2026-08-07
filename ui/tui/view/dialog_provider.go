package view

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ProviderEntry describes one selectable LLM provider in the TUI.
type ProviderEntry struct {
	Key              string // config llm.provider value
	Name             string // display name
	Category         string // list grouping: Local | Cloud | Gateway
	Endpoint         string // default api_base
	NeedsKey         bool   // cloud — API key in settings
	Local            bool   // OpenAI-compatible /v1/models without key
	EndpointEditable bool   // user can set custom URL (LM Studio, Ollama, Custom)
}

// DialogProviders is the canonical provider catalog for /provider and onboarding.
var DialogProviders = []ProviderEntry{
	// Local — OpenAI-compatible servers on the machine.
	{Key: "lmstudio", Name: "LM Studio", Category: "Local", Endpoint: "http://localhost:1234", Local: true, EndpointEditable: true},
	{Key: "ollama", Name: "Ollama", Category: "Local", Endpoint: "http://localhost:11434", Local: true, EndpointEditable: true},
	{Key: "vllm", Name: "vLLM", Category: "Local", Endpoint: "http://localhost:8000/v1", Local: true, EndpointEditable: true},

	// Major cloud APIs.
	{Key: "openai", Name: "OpenAI", Category: "Cloud", Endpoint: "https://api.openai.com/v1", NeedsKey: true},
	{Key: "anthropic", Name: "Anthropic", Category: "Cloud", Endpoint: "https://api.anthropic.com", NeedsKey: true},
	{Key: "google", Name: "Google Gemini", Category: "Cloud", Endpoint: "https://generativelanguage.googleapis.com/v1beta/openai", NeedsKey: true},
	{Key: "mistral", Name: "Mistral AI", Category: "Cloud", Endpoint: "https://api.mistral.ai/v1", NeedsKey: true},
	{Key: "deepseek", Name: "DeepSeek", Category: "Cloud", Endpoint: "https://api.deepseek.com/v1", NeedsKey: true},
	{Key: "xai", Name: "xAI (Grok)", Category: "Cloud", Endpoint: "https://api.x.ai/v1", NeedsKey: true},
	{Key: "moonshot", Name: "Moonshot (Kimi)", Category: "Cloud", Endpoint: "https://api.moonshot.cn/v1", NeedsKey: true},

	// Gateways / fast inference hosts (OpenAI-compatible).
	{Key: "openrouter", Name: "OpenRouter", Category: "Gateway", Endpoint: "https://openrouter.ai/api/v1", NeedsKey: true},
	{Key: "groq", Name: "Groq", Category: "Gateway", Endpoint: "https://api.groq.com/openai/v1", NeedsKey: true},
	{Key: "together", Name: "Together AI", Category: "Gateway", Endpoint: "https://api.together.xyz/v1", NeedsKey: true},
	{Key: "fireworks", Name: "Fireworks AI", Category: "Gateway", Endpoint: "https://api.fireworks.ai/inference/v1", NeedsKey: true},
	{Key: "cerebras", Name: "Cerebras", Category: "Gateway", Endpoint: "https://api.cerebras.ai/v1", NeedsKey: true},

	// Any OpenAI-compatible endpoint (vLLM, LiteLLM proxy, corporate gateway, …).
	{Key: "custom", Name: "Custom (OpenAI-compatible)", Category: "Other", Endpoint: "", Local: true, EndpointEditable: true},
}

// FindProviderByKey returns a catalog entry by key.
func FindProviderByKey(key string) (ProviderEntry, bool) {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, p := range DialogProviders {
		if p.Key == key {
			return p, true
		}
	}
	return ProviderEntry{}, false
}

// ProviderWithSavedEndpoint merges catalog defaults with a saved api_base from config.
func ProviderWithSavedEndpoint(key, savedAPIBase string) ProviderEntry {
	p, ok := FindProviderByKey(key)
	if !ok {
		if savedAPIBase != "" {
			return ProviderEntry{
				Key:              "custom",
				Name:             "Custom (OpenAI-compatible)",
				Category:         "Other",
				Endpoint:         savedAPIBase,
				Local:            true,
				EndpointEditable: true,
			}
		}
		return DialogProviders[0]
	}
	if savedAPIBase != "" {
		p.Endpoint = strings.TrimRight(strings.TrimSpace(savedAPIBase), "/")
	}
	return p
}

// NormalizeEndpoint trims trailing slashes from api_base URLs.
func NormalizeEndpoint(url string) string {
	return strings.TrimRight(strings.TrimSpace(url), "/")
}

// ProviderDialog is the first dialog in the /provider flow: picks a provider.
type ProviderDialog struct {
	cursor int
	ready  map[string]bool // provider key → credentials configured
}

// NewProviderDialog constructs a ProviderDialog.
// ready marks providers that already have URL (+ API key when required).
func NewProviderDialog(ready map[string]bool) *ProviderDialog {
	if ready == nil {
		ready = map[string]bool{}
	}
	return &ProviderDialog{ready: ready}
}

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
		items = append(items, listDialogItem{
			Title:       p.Name,
			Description: providerEndpointDesc(p),
			Category:    p.Category,
			Ready:       d.ready[p.Key],
		})
	}
	return renderListDialog(
		"Select provider",
		items,
		d.cursor,
		"",
		"",
		"● зелёный = подключено  ↑↓ Enter  Esc",
		screenW, screenH,
	)
}

func providerEndpointDesc(p ProviderEntry) string {
	switch {
	case p.EndpointEditable && p.Endpoint != "":
		return p.Endpoint + "  · edit URL"
	case p.EndpointEditable:
		return "enter server URL"
	case p.NeedsKey:
		return p.Endpoint + "  · API key"
	default:
		return p.Endpoint
	}
}
