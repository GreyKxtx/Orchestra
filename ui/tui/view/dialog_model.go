package view

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"

	"github.com/orchestra/orchestra/internal/lmstudio"
)

// ModelEntry is a single selectable model.
type ModelEntry struct {
	ID               string
	MaxContextLength int64 // 0 if unknown
	IsLoaded         bool  // applies to local providers
}

// CloudModels lists known cloud model ids per provider key. Used when the
// provider is non-local (no /v1/models endpoint without an API key).
var CloudModels = map[string][]ModelEntry{
	"openai": {
		{ID: "gpt-4.1"},
		{ID: "gpt-4.1-mini"},
		{ID: "gpt-4.1-nano"},
		{ID: "gpt-4o"},
		{ID: "gpt-4o-mini"},
		{ID: "o3"},
		{ID: "o3-mini"},
		{ID: "o4-mini"},
	},
	"anthropic": {
		{ID: "claude-opus-4-7"},
		{ID: "claude-sonnet-4-6"},
		{ID: "claude-haiku-4-5-20251001"},
		{ID: "claude-opus-4"},
		{ID: "claude-sonnet-4"},
		{ID: "claude-3-7-sonnet-latest"},
		{ID: "claude-3-5-sonnet-latest"},
	},
}

// ModelsLoadedMsg is dispatched when async fetching of models completes.
type ModelsLoadedMsg struct {
	ProviderKey string
	Models      []ModelEntry
	Err         string
}

// FetchModelsCmd fetches models from a local OpenAI-compatible endpoint.
// Used for LM Studio and Ollama. The result lands as ModelsLoadedMsg.
func FetchModelsCmd(providerKey, endpoint string) tea.Cmd {
	return func() tea.Msg {
		client := lmstudio.NewClient(endpoint)
		raw, err := client.ListModels()
		if err != nil {
			return ModelsLoadedMsg{ProviderKey: providerKey, Err: err.Error()}
		}
		models := make([]ModelEntry, 0, len(raw))
		for _, m := range raw {
			models = append(models, ModelEntry{
				ID:               m.ID,
				MaxContextLength: m.MaxContextLength,
				IsLoaded:         m.IsLoaded,
			})
		}
		return ModelsLoadedMsg{ProviderKey: providerKey, Models: models}
	}
}

// ModelDialog is the second step of /provider or root of /model: pick a model.
type ModelDialog struct {
	provider ProviderEntry
	models   []ModelEntry
	loading  bool
	loadErr  string
	cursor   int
	filter   string
}

// NewModelDialog returns a ModelDialog for the given provider. For local
// providers (LM Studio, Ollama, Custom) it starts in loading state; the
// caller is expected to also dispatch FetchModelsCmd. For cloud providers
// it pre-populates from CloudModels.
func NewModelDialog(p ProviderEntry) *ModelDialog {
	d := &ModelDialog{provider: p}
	if p.Local {
		d.loading = true
	} else {
		d.models = CloudModels[p.Key]
	}
	return d
}

// Provider returns the provider this dialog was opened for.
func (d *ModelDialog) Provider() ProviderEntry { return d.provider }

// SetModels populates the dialog with fetched models (used for local
// providers). Stops the loading spinner.
func (d *ModelDialog) SetModels(models []ModelEntry) {
	d.models = models
	d.loading = false
	d.loadErr = ""
	d.cursor = 0
}

// SetLoadError records a failed fetch so the user sees the reason.
func (d *ModelDialog) SetLoadError(err string) {
	d.loadErr = err
	d.loading = false
}

// Update implements Dialog.
func (d *ModelDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
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
		filtered := d.visibleModels()
		if d.cursor < len(filtered)-1 {
			d.cursor++
		}
	case "esc", "left":
		return d, dialogResultCmd("model", "cancel", nil)
	case "enter", "right":
		filtered := d.visibleModels()
		if d.cursor >= 0 && d.cursor < len(filtered) {
			return d, dialogResultCmd("model", "select", filtered[d.cursor])
		}
	case "backspace":
		if len(d.filter) > 0 {
			d.filter = d.filter[:len(d.filter)-1]
			d.cursor = 0
		}
	default:
		if len(km.Runes) == 1 {
			d.filter += string(km.Runes[0])
			d.cursor = 0
		}
	}
	return d, nil
}

func (d *ModelDialog) visibleModels() []ModelEntry {
	if d.filter == "" {
		return d.models
	}
	names := make([]string, len(d.models))
	for i, m := range d.models {
		names[i] = m.ID
	}
	matches := fuzzy.Find(d.filter, names)
	out := make([]ModelEntry, 0, len(matches))
	for _, m := range matches {
		out = append(out, d.models[m.Index])
	}
	return out
}

// Render implements Dialog.
func (d *ModelDialog) Render(screenW, screenH int) string {
	title := "Select model — " + d.provider.Name

	var items []listDialogItem
	switch {
	case d.loading:
		items = []listDialogItem{{Title: "loading…", Description: d.provider.Endpoint, Disabled: true}}
	case d.loadErr != "":
		items = []listDialogItem{{Title: "error", Description: d.loadErr, Disabled: true}}
	default:
		filtered := d.visibleModels()
		items = make([]listDialogItem, 0, len(filtered))
		for _, m := range filtered {
			desc := ""
			if m.MaxContextLength > 0 {
				desc = formatCtxLen(m.MaxContextLength)
			}
			if m.IsLoaded {
				if desc != "" {
					desc += "  "
				}
				desc += "● loaded"
			}
			items = append(items, listDialogItem{
				Title:       m.ID,
				Description: desc,
			})
		}
	}

	return renderListDialog(
		title,
		items,
		d.cursor,
		d.filter,
		"Search models",
		"↑↓ navigate · Enter/→ select · Esc/← back",
		screenW, screenH,
	)
}

// formatCtxLen renders a token count as "16k ctx" / "128k ctx" / "1.0M ctx".
func formatCtxLen(n int64) string {
	switch {
	case n < 1024:
		return ""
	case n < 1_048_576:
		return fmt.Sprintf("%dk ctx", (n+512)/1024)
	default:
		return fmt.Sprintf("%.1fM ctx", float64(n)/1_048_576.0)
	}
}
