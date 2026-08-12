package view

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// EndpointDialogResult is emitted on successful save.
type EndpointDialogResult struct {
	Provider ProviderEntry
	APIKey   string
}

// EndpointDialog edits credentials: API key for every provider, plus URL when
// EndpointEditable (local / custom). Cloud providers get a read-only URL + key.
type EndpointDialog struct {
	provider    ProviderEntry
	url         string
	apiKey      string
	field       int // 0=url, 1=api key
	urlEditable bool
	errMsg      string
}

// NewEndpointDialog pre-fills URL/key from saved config or catalog defaults.
func NewEndpointDialog(provider ProviderEntry, savedURL, savedAPIKey string) *EndpointDialog {
	url := provider.Endpoint
	if savedURL != "" {
		url = savedURL
	}
	if provider.Key == "custom" && url == "" {
		url = "http://localhost:11434"
	}
	if provider.Key == "vllm" && url == "" {
		url = "http://localhost:8000/v1"
	}
	d := &EndpointDialog{
		provider:    provider,
		url:         url,
		apiKey:      savedAPIKey,
		urlEditable: provider.EndpointEditable,
	}
	// Cloud / gateway: jump straight to API key.
	if !d.urlEditable {
		d.field = 1
	}
	return d
}

// SetError shows a validation failure inside the modal.
func (d *EndpointDialog) SetError(msg string) {
	d.errMsg = msg
	if !d.urlEditable {
		d.field = 1
	}
}

// ClearError clears the last validation message.
func (d *EndpointDialog) ClearError() {
	d.errMsg = ""
}

// Update implements Dialog.
func (d *EndpointDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	switch km.String() {
	case "esc":
		return d, resultCmd(EndpointDialogMsg{Cancel: true})
	case "tab", "shift+tab", "up", "down":
		if !d.urlEditable {
			d.field = 1
			break
		}
		if d.field == 0 {
			d.field = 1
		} else {
			d.field = 0
		}
		d.errMsg = ""
	case "enter":
		p := d.provider
		p.Endpoint = NormalizeEndpoint(d.url)
		if p.Endpoint == "" {
			d.errMsg = "нужен URL"
			return d, nil
		}
		key := strings.TrimSpace(d.apiKey)
		if p.NeedsKey && key == "" {
			d.field = 1
			d.errMsg = "нужен API key"
			return d, nil
		}
		return d, resultCmd(EndpointDialogMsg{Result: EndpointDialogResult{
			Provider: p,
			APIKey:   key,
		}})
	case "backspace":
		d.errMsg = ""
		if d.field == 0 && d.urlEditable {
			if len(d.url) > 0 {
				r := []rune(d.url)
				d.url = string(r[:len(r)-1])
			}
		} else if d.field == 1 && len(d.apiKey) > 0 {
			r := []rune(d.apiKey)
			d.apiKey = string(r[:len(r)-1])
		}
	case "ctrl+u":
		d.errMsg = ""
		if d.field == 0 && d.urlEditable {
			d.url = ""
		} else if d.field == 1 {
			d.apiKey = ""
		}
	default:
		if len(km.Runes) == 0 {
			break
		}
		d.errMsg = ""
		chunk := string(km.Runes)
		if d.field == 0 && d.urlEditable {
			d.url += chunk
		} else if d.field == 1 {
			d.apiKey += chunk
		}
	}
	return d, nil
}

// Render implements Dialog.
func (d *EndpointDialog) Render(screenW, screenH int) string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	modalW := screenW * 70 / 100
	if modalW < 48 {
		modalW = 48
	}
	if modalW > 90 {
		modalW = 90
	}
	if maxW := screenW - 4; modalW > maxW {
		modalW = maxW
	}
	if modalW < 36 {
		modalW = 36
	}
	inner := modalW - 8
	if inner < 28 {
		inner = 28
	}

	base := lipgloss.NewStyle().Background(bg)
	titleStyle := base.Foreground(t.Text()).Bold(true)
	muted := base.Foreground(t.TextMuted())
	labelStyle := base.Foreground(t.Text())
	primStyle := base.Foreground(t.Primary())
	errStyle := base.Foreground(t.Error())
	selStyle := lipgloss.NewStyle().
		Background(t.Primary()).
		Foreground(t.Background()).
		Bold(true).
		Width(inner)

	padBg := func(n int) string {
		if n <= 0 {
			return ""
		}
		return base.Render(strings.Repeat(" ", n))
	}
	fitInner := func(s string) string {
		if visW := lipgloss.Width(s); visW < inner {
			return s + padBg(inner-visW)
		}
		return s
	}

	sub := "API key + base URL"
	hint := "Tab: URL ↔ Key · Enter далее · Esc назад"
	if d.urlEditable {
		sub = "OpenAI-compatible base URL + API key (ключ опционален)"
		switch d.provider.Key {
		case "ollama":
			hint = "Default: http://localhost:11434"
		case "lmstudio":
			hint = "Default: http://localhost:1234 (LM Studio → Server)"
		case "vllm":
			hint = "Default: http://localhost:8000/v1 · ngrok/remote тоже ок"
		default:
			hint = "Пример: http://localhost:8000/v1"
		}
	} else if d.provider.NeedsKey {
		sub = "Вставь API key для " + d.provider.Name
		hint = "URL фиксированный · ключ обязателен · Enter далее"
	} else {
		sub = "API key (опционально) для " + d.provider.Name
		hint = "URL фиксированный · Enter далее · Esc назад"
	}

	urlVal := d.url
	if urlVal == "" {
		urlVal = "http://"
	}
	keyVal := d.apiKey
	keyHint := false
	if d.field != 1 {
		if keyVal == "" {
			if d.provider.NeedsKey {
				keyVal = "(обязательно — Tab/↓ чтобы ввести)"
			} else {
				keyVal = "(optional — Tab чтобы ввести)"
			}
			keyHint = true
		} else {
			keyVal = maskSecret(keyVal)
		}
	}

	var urlLine, keyLine string
	if d.field == 0 && d.urlEditable {
		raw := "▶ URL   " + d.url + "▋"
		urlLine = selStyle.Render(truncRunes(raw, inner))
		keyDisp := "  Key   " + keyVal
		if keyHint {
			keyLine = fitInner(muted.Render(keyDisp))
		} else {
			keyLine = fitInner(labelStyle.Render("  Key   ") + primStyle.Render(keyVal))
		}
	} else {
		urlPrefix := "  URL   "
		if !d.urlEditable {
			urlPrefix = "  URL   " // read-only
		}
		urlLine = fitInner(labelStyle.Render(urlPrefix) + primStyle.Render(truncRunes(urlVal, inner-8)))
		raw := "▶ Key   " + d.apiKey + "▋"
		keyLine = selStyle.Render(truncRunes(raw, inner))
	}

	blank := padBg(inner)
	title := titleStyle.Render("Credentials — " + d.provider.Name)
	esc := muted.Render("esc")
	gap := inner - lipgloss.Width(title) - lipgloss.Width(esc)
	if gap < 1 {
		gap = 1
	}
	header := title + padBg(gap) + esc

	lines := []string{
		blank,
		header,
		blank,
		fitInner(muted.Render(sub)),
		blank,
		urlLine,
		keyLine,
		blank,
		fitInner(muted.Render(hint)),
	}
	if d.errMsg != "" {
		lines = append(lines, fitInner(errStyle.Render("⚠ "+d.errMsg)))
	}
	lines = append(lines, blank)

	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	box := lipgloss.NewStyle().
		Background(bg).
		Padding(0, 4).
		Width(modalW).
		Render(body)
	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, box)
}

func maskSecret(s string) string {
	r := []rune(s)
	if len(r) <= 4 {
		return strings.Repeat("•", len(r))
	}
	return strings.Repeat("•", len(r)-4) + string(r[len(r)-4:])
}
