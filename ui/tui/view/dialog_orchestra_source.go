package view

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// OrchestraSourcePick is emitted when the user picks a provider source for a role.
type OrchestraSourcePick struct {
	IsMain   bool
	Provider ProviderEntry // valid when !IsMain
	Key      string        // providers: / catalog key; empty when IsMain
}

// OrchestraSourceDialog lists Main (current TUI llm) + catalog providers with params.
type OrchestraSourceDialog struct {
	items  []orchestraSourceItem
	cursor int
	filter string
}

type orchestraSourceItem struct {
	pick  OrchestraSourcePick
	title string
	desc  string
	cat   string
	ready bool
}

// NewOrchestraSourceDialog builds the picker from dialog context + catalog.
// Cursor starts on the first catalog provider (not Main) so the short list
// focus isn't stuck on the Current section.
func NewOrchestraSourceDialog(ctx OrchestraDialogCtx) *OrchestraSourceDialog {
	items := make([]orchestraSourceItem, 0, len(DialogProviders)+2)

	mainDesc := "llm: из статус-бара"
	if m := strings.TrimSpace(ctx.MainModel); m != "" {
		mainDesc = truncRunes(m, 36)
	}
	parts := []string{}
	if b := strings.TrimSpace(ctx.MainAPIBase); b != "" {
		parts = append(parts, truncRunes(b, 40))
	}
	if ctx.MainNeedsKey {
		if strings.TrimSpace(ctx.MainAPIKey) != "" {
			parts = append(parts, "key ✓")
		} else {
			parts = append(parts, "нужен key")
		}
	}
	if len(parts) > 0 {
		mainDesc = strings.Join(parts, " · ")
	}
	items = append(items, orchestraSourceItem{
		pick:  OrchestraSourcePick{IsMain: true},
		title: "Main",
		desc:  mainDesc,
		cat:   "Current",
		ready: credentialsReady(ctx.MainAPIBase, ctx.MainAPIKey, ctx.MainNeedsKey),
	})

	if fp := strings.TrimSpace(ctx.FastProvider); fp != "" {
		entry, _ := FindProviderByKey(fp)
		apiBase, apiKey := entry.Endpoint, ""
		needs := entry.NeedsKey
		configured := false
		if n, ok := ctx.Named[fp]; ok {
			if entry.Key == "" {
				entry = ProviderEntry{Key: fp, Name: n.Label, Endpoint: n.APIBase, NeedsKey: n.NeedsKey, Local: true, EndpointEditable: true}
			} else if n.APIBase != "" {
				entry.Endpoint = n.APIBase
			}
			apiBase = n.APIBase
			if apiBase == "" {
				apiBase = entry.Endpoint
			}
			apiKey = n.APIKey
			needs = n.NeedsKey
			configured = n.Configured
		}
		title := "Fast · " + fp
		if entry.Name != "" {
			title = "Fast · " + entry.Name
		}
		desc := providerEndpointDesc(entry)
		if desc == "" {
			desc = "llm.router.fast_provider"
		}
		items = append(items, orchestraSourceItem{
			pick:  OrchestraSourcePick{Key: fp, Provider: entry},
			title: title,
			desc:  desc,
			cat:   "Current",
			ready: configured && credentialsReady(apiBase, apiKey, needs),
		})
	}

	firstCatalog := -1
	for _, p := range DialogProviders {
		ep := p
		apiBase, apiKey := p.Endpoint, ""
		needs := p.NeedsKey
		configured := false
		if n, ok := ctx.Named[p.Key]; ok {
			if n.APIBase != "" {
				ep.Endpoint = n.APIBase
				apiBase = n.APIBase
			}
			apiKey = n.APIKey
			needs = n.NeedsKey
			configured = n.Configured
		}
		if firstCatalog < 0 {
			firstCatalog = len(items)
		}
		items = append(items, orchestraSourceItem{
			pick:  OrchestraSourcePick{Key: p.Key, Provider: ep},
			title: p.Name,
			desc:  providerEndpointDesc(ep),
			cat:   p.Category,
			ready: configured && credentialsReady(apiBase, apiKey, needs),
		})
	}

	// YAML-only extras.
	for k, n := range ctx.Named {
		if _, ok := FindProviderByKey(k); ok {
			continue
		}
		if strings.EqualFold(k, ctx.FastProvider) {
			continue
		}
		entry := ProviderEntry{
			Key:              k,
			Name:             n.Label,
			Endpoint:         n.APIBase,
			NeedsKey:         n.NeedsKey,
			Local:            true,
			EndpointEditable: true,
			Category:         "Configured",
		}
		if entry.Name == "" {
			entry.Name = k
		}
		items = append(items, orchestraSourceItem{
			pick:  OrchestraSourcePick{Key: k, Provider: entry},
			title: entry.Name,
			desc:  fmt.Sprintf("%s · из providers:", n.APIBase),
			cat:   "Configured",
			ready: n.Configured && credentialsReady(n.APIBase, n.APIKey, n.NeedsKey),
		})
	}

	cursor := 0
	if firstCatalog >= 0 {
		cursor = firstCatalog
	}
	return &OrchestraSourceDialog{items: items, cursor: cursor}
}

// credentialsReady reports whether URL (+ API key when required) are filled.
func credentialsReady(apiBase, apiKey string, needsKey bool) bool {
	if strings.TrimSpace(apiBase) == "" {
		return false
	}
	if needsKey && strings.TrimSpace(apiKey) == "" {
		return false
	}
	return true
}

// Update implements Dialog.
func (d *OrchestraSourceDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	vis := d.visible()
	switch km.String() {
	case "up", "ctrl+p":
		if d.cursor > 0 {
			d.cursor--
		}
	case "down", "ctrl+n":
		if d.cursor < len(vis)-1 {
			d.cursor++
		}
	case "esc", "left":
		return d, dialogResultCmd("orchestra_source", "cancel", nil)
	case "enter", "right":
		if d.cursor >= 0 && d.cursor < len(vis) {
			return d, dialogResultCmd("orchestra_source", "select", vis[d.cursor].pick)
		}
	case "backspace":
		if len(d.filter) > 0 {
			r := []rune(d.filter)
			d.filter = string(r[:len(r)-1])
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

func (d *OrchestraSourceDialog) visible() []orchestraSourceItem {
	f := strings.ToLower(strings.TrimSpace(d.filter))
	if f == "" {
		return d.items
	}
	out := make([]orchestraSourceItem, 0, len(d.items))
	for _, it := range d.items {
		if strings.Contains(strings.ToLower(it.title), f) ||
			strings.Contains(strings.ToLower(it.desc), f) ||
			strings.Contains(strings.ToLower(it.pick.Key), f) {
			out = append(out, it)
		}
	}
	return out
}

// Render implements Dialog.
func (d *OrchestraSourceDialog) Render(screenW, screenH int) string {
	vis := d.visible()
	items := make([]listDialogItem, 0, len(vis))
	for _, it := range vis {
		items = append(items, listDialogItem{
			Title:       it.title,
			Description: it.desc,
			Category:    it.cat,
			Ready:       it.ready,
		})
	}
	return renderListDialog(
		"Провайдер для роли Orchestra",
		items,
		d.cursor,
		d.filter,
		"фильтр…",
		"● зелёный = подключено  ↑↓ Enter  Esc",
		screenW, screenH,
	)
}
