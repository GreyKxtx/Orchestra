package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// SettingsDialogResult is the payload of DialogResultMsg{Source:"settings"}.
type SettingsDialogResult struct {
	Provider       ProviderEntry
	Model          ModelEntry
	APIKey         string
	Temperature    float32
	MaxTokens      int
	NumCtx         int64
	EnableThinking bool
}

// SettingsDialog edits the per-model settings preset. Field cursor cycles
// through the editable fields; ←/→ adjust numeric/bool values; for the
// API-key field, plain typing edits the buffer.
type SettingsDialog struct {
	provider ProviderEntry
	model    ModelEntry

	apiKey         string
	temperature    float32
	maxTokens      int
	numCtx         int64
	enableThinking bool

	cursor int // index into fields()
}

// NewSettingsDialog returns a SettingsDialog seeded with reasonable defaults
// (or values from an existing preset, if applicable). Caller can override
// initial values via SetInitial.
func NewSettingsDialog(provider ProviderEntry, model ModelEntry) *SettingsDialog {
	d := &SettingsDialog{
		provider:    provider,
		model:       model,
		temperature: 0.20,
		maxTokens:   8192,
		numCtx:      20480,
	}
	if model.MaxContextLength > 0 && model.MaxContextLength < d.numCtx {
		d.numCtx = model.MaxContextLength
	}
	return d
}

// SetInitial seeds settings from an existing preset.
func (d *SettingsDialog) SetInitial(temperature float32, maxTokens int, numCtx int64, enableThinking bool, apiKey string) {
	if temperature > 0 {
		d.temperature = temperature
	}
	if maxTokens > 0 {
		d.maxTokens = maxTokens
	}
	if numCtx > 0 {
		d.numCtx = numCtx
	}
	d.enableThinking = enableThinking
	if apiKey != "" {
		d.apiKey = apiKey
	}
}

type settingsField struct {
	label string
	kind  string // "temp" | "tokens" | "ctx" | "thinking" | "apikey"
}

func (d *SettingsDialog) fields() []settingsField {
	out := []settingsField{
		{label: "Temperature", kind: "temp"},
		{label: "Max tokens", kind: "tokens"},
		{label: "Context length", kind: "ctx"},
		{label: "Enable thinking", kind: "thinking"},
	}
	if d.provider.NeedsKey {
		out = append(out, settingsField{label: "API key", kind: "apikey"})
	}
	return out
}

// Update implements Dialog.
func (d *SettingsDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	flds := d.fields()
	switch km.String() {
	case "up", "ctrl+p":
		if d.cursor > 0 {
			d.cursor--
		}
	case "down", "ctrl+n":
		if d.cursor < len(flds)-1 {
			d.cursor++
		}
	case "left":
		d.adjustField(flds[d.cursor].kind, -1)
	case "right":
		d.adjustField(flds[d.cursor].kind, +1)
	case "esc":
		return d, dialogResultCmd("settings", "cancel", nil)
	case "enter":
		return d, dialogResultCmd("settings", "save", SettingsDialogResult{
			Provider:       d.provider,
			Model:          d.model,
			APIKey:         d.apiKey,
			Temperature:    d.temperature,
			MaxTokens:      d.maxTokens,
			NumCtx:         d.numCtx,
			EnableThinking: d.enableThinking,
		})
	case "backspace":
		if flds[d.cursor].kind == "apikey" && len(d.apiKey) > 0 {
			d.apiKey = d.apiKey[:len(d.apiKey)-1]
		}
	default:
		if flds[d.cursor].kind == "apikey" && len(km.Runes) == 1 {
			d.apiKey += string(km.Runes[0])
		}
	}
	return d, nil
}

func (d *SettingsDialog) adjustField(kind string, dir int) {
	switch kind {
	case "temp":
		d.temperature += float32(dir) * 0.05
		if d.temperature < 0 {
			d.temperature = 0
		}
		if d.temperature > 2 {
			d.temperature = 2
		}
	case "tokens":
		step := 1024
		if d.maxTokens >= 16384 {
			step = 4096
		}
		d.maxTokens += dir * step
		if d.maxTokens < 256 {
			d.maxTokens = 256
		}
		if d.maxTokens > 200_000 {
			d.maxTokens = 200_000
		}
	case "ctx":
		step := int64(2048)
		if d.numCtx >= 32768 {
			step = 8192
		}
		if d.numCtx >= 131_072 {
			step = 32_768
		}
		d.numCtx += int64(dir) * step
		if d.numCtx < 1024 {
			d.numCtx = 1024
		}
		if d.model.MaxContextLength > 0 && d.numCtx > d.model.MaxContextLength {
			d.numCtx = d.model.MaxContextLength
		}
		if d.numCtx > 1_048_576 {
			d.numCtx = 1_048_576
		}
	case "thinking":
		d.enableThinking = !d.enableThinking
	}
}

// Render implements Dialog.
func (d *SettingsDialog) Render(screenW, screenH int) string {
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
	if modalW < 30 {
		modalW = 30
	}
	inner := modalW - 8

	base := lipgloss.NewStyle().Background(bg)
	titleStyle := base.Foreground(t.Text()).Bold(true)
	mutedStyle := base.Foreground(t.TextMuted())
	textStyle := base.Foreground(t.Text())
	primStyle := base.Foreground(t.Primary())
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

	titleR := titleStyle.Render("Model settings — " + d.model.ID)
	esc := mutedStyle.Render("esc")
	gap := inner - lipgloss.Width(titleR) - lipgloss.Width(esc)
	if gap < 1 {
		gap = 1
	}
	header := titleR + padBg(gap) + esc

	modelLine := fitInner(mutedStyle.Render("  " + d.provider.Name + " · " + d.model.ID))

	flds := d.fields()
	const labelCol = 18
	const inset = "  "

	var rows []string
	for i, f := range flds {
		valueText := d.fieldValue(f.kind)
		hint := d.fieldHint(f.kind, i == d.cursor)

		row := inset + fmt.Sprintf("%-*s", labelCol, f.label)
		if i == d.cursor {
			rendered := selStyle.Render(row + valueText + "  " + hint)
			rows = append(rows, rendered)
		} else {
			line := textStyle.Render(row) + primStyle.Render(valueText)
			if hint != "" {
				line += mutedStyle.Render("  " + hint)
			}
			rows = append(rows, fitInner(line))
		}
	}

	hint := fitInner(mutedStyle.Render("↑↓ field · ←→ adjust · Enter save · Esc back"))
	blank := padBg(inner)

	sections := []string{blank, header, modelLine, blank}
	sections = append(sections, rows...)
	sections = append(sections, blank, hint, blank)
	body := lipgloss.JoinVertical(lipgloss.Left, sections...)

	box := lipgloss.NewStyle().
		Background(bg).
		Padding(0, 4).
		Width(modalW).
		Render(body)

	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, box)
}

func (d *SettingsDialog) fieldValue(kind string) string {
	switch kind {
	case "temp":
		return fmt.Sprintf("%.2f", d.temperature)
	case "tokens":
		return fmt.Sprintf("%d", d.maxTokens)
	case "ctx":
		return fmt.Sprintf("%d", d.numCtx)
	case "thinking":
		if d.enableThinking {
			return "on"
		}
		return "off"
	case "apikey":
		if d.apiKey == "" {
			return "(empty)"
		}
		return strings.Repeat("•", len(d.apiKey))
	}
	return ""
}

func (d *SettingsDialog) fieldHint(kind string, active bool) string {
	if !active {
		return ""
	}
	switch kind {
	case "temp", "tokens", "ctx":
		return "← →"
	case "thinking":
		return "← → toggle"
	case "apikey":
		return "type to edit"
	}
	return ""
}
