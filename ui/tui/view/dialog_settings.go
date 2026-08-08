package view

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	TimeoutS       int
	EnableThinking bool
}

// SettingsDialog edits the per-model settings preset.
// Type digits to enter values manually; ←/→ step the committed value.
type SettingsDialog struct {
	provider ProviderEntry
	model    ModelEntry

	apiKey         string
	temperature    float32
	maxTokens      int
	numCtx         int64
	timeoutS       int
	enableThinking bool

	cursor  int
	editBuf string // in-progress typed value for the focused field
}

// NewSettingsDialog returns a SettingsDialog seeded with reasonable defaults.
func NewSettingsDialog(provider ProviderEntry, model ModelEntry) *SettingsDialog {
	d := &SettingsDialog{
		provider:    provider,
		model:       model,
		temperature: 0.20,
		maxTokens:   8192,
		numCtx:      20480,
		timeoutS:    600, // match Claude Code-class default (10m) for local/ngrok
	}
	if model.MaxContextLength > 0 && model.MaxContextLength < d.numCtx {
		d.numCtx = model.MaxContextLength
	}
	d.syncAutoAnswer()
	return d
}

// SetInitial seeds settings from an existing preset / config.
// timeoutS <= 0 leaves the dialog default.
func (d *SettingsDialog) SetInitial(temperature float32, maxTokens int, numCtx int64, timeoutS int, enableThinking bool, apiKey string) {
	if temperature > 0 {
		d.temperature = temperature
	}
	if maxTokens > 0 {
		d.maxTokens = maxTokens
	}
	if numCtx > 0 {
		d.numCtx = numCtx
	}
	if timeoutS > 0 {
		d.timeoutS = clampTimeoutS(timeoutS)
	}
	d.enableThinking = enableThinking
	if apiKey != "" {
		d.apiKey = apiKey
	}
	d.syncAutoAnswer()
}

type settingsField struct {
	label string
	kind  string // "temp" | "tokens" | "ctx" | "timeout" | "thinking" | "apikey"
}

func (d *SettingsDialog) fields() []settingsField {
	// One Tokens knob (= window). Answer budget is always derived (~20%).
	return []settingsField{
		{label: "Temperature", kind: "temp"},
		{label: "Tokens", kind: "ctx"},
		{label: "LLM timeout (s)", kind: "timeout"},
		{label: "Enable thinking", kind: "thinking"},
		{label: "API key", kind: "apikey"},
	}
}

// syncAutoAnswer sets max_tokens from the window when the provider auto-splits.
func (d *SettingsDialog) syncAutoAnswer() {
	if d.provider.AutoAnswerBudget() {
		d.maxTokens = autoAnswerBudget(d.numCtx)
	}
}

// autoAnswerBudget is the derived completion size for a context window (~20%).
func autoAnswerBudget(numCtx int64) int {
	return clampTokensForCtx(1<<30, numCtx)
}

func formatTokenCount(n int) string {
	if n >= 1000 {
		if n%1000 == 0 {
			return fmt.Sprintf("%dk", n/1000)
		}
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return strconv.Itoa(n)
}


// Update implements Dialog.
func (d *SettingsDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	flds := d.fields()
	kind := flds[d.cursor].kind

	switch km.String() {
	case "up", "ctrl+p":
		d.commitEdit()
		if d.cursor > 0 {
			d.cursor--
		}
	case "down", "ctrl+n":
		d.commitEdit()
		if d.cursor < len(flds)-1 {
			d.cursor++
		}
	case "left":
		d.commitEdit()
		d.adjustField(kind, -1)
	case "right":
		d.commitEdit()
		d.adjustField(kind, +1)
	case "esc":
		return d, dialogResultCmd("settings", "cancel", nil)
	case "enter":
		d.commitEdit()
		d.syncAutoAnswer()
		if !d.provider.AutoAnswerBudget() {
			d.maxTokens = clampTokensForCtx(d.maxTokens, d.numCtx)
		}
		d.timeoutS = clampTimeoutS(d.timeoutS)
		return d, dialogResultCmd("settings", "save", SettingsDialogResult{
			Provider:       d.provider,
			Model:          d.model,
			APIKey:         d.apiKey,
			Temperature:    d.temperature,
			MaxTokens:      d.maxTokens,
			NumCtx:         d.numCtx,
			TimeoutS:       d.timeoutS,
			EnableThinking: d.enableThinking,
		})
	case "backspace":
		if len(d.editBuf) > 0 {
			d.editBuf = d.editBuf[:len(d.editBuf)-1]
		} else if kind == "apikey" && len(d.apiKey) > 0 {
			d.apiKey = d.apiKey[:len(d.apiKey)-1]
		}
	default:
		if len(km.Runes) == 0 {
			break
		}
		// Paste may arrive as many runes in one KeyMsg (Windows Terminal).
		chunk := string(km.Runes)
		switch kind {
		case "temp":
			for _, r := range km.Runes {
				if isTempRune(r) {
					d.editBuf += string(r)
				}
			}
		case "tokens", "ctx", "timeout":
			for _, r := range km.Runes {
				if unicode.IsDigit(r) {
					d.editBuf += string(r)
				}
			}
		case "apikey":
			d.editBuf += chunk
		}
	}
	return d, nil
}

func isTempRune(r rune) bool {
	return unicode.IsDigit(r) || r == '.'
}

func (d *SettingsDialog) commitEdit() {
	if d.editBuf == "" {
		return
	}
	kind := d.fields()[d.cursor].kind
	switch kind {
	case "temp":
		if v, err := strconv.ParseFloat(d.editBuf, 32); err == nil {
			d.temperature = clampTemp(float32(v))
		}
	case "tokens":
		if v, err := strconv.Atoi(d.editBuf); err == nil {
			d.maxTokens = clampTokensForCtx(v, d.numCtx)
		}
	case "ctx":
		if v, err := strconv.ParseInt(d.editBuf, 10, 64); err == nil {
			d.numCtx = clampCtx(v, d.model.MaxContextLength)
			d.syncAutoAnswer()
			if !d.provider.AutoAnswerBudget() {
				d.maxTokens = clampTokensForCtx(d.maxTokens, d.numCtx)
			}
		}
	case "timeout":
		if v, err := strconv.Atoi(d.editBuf); err == nil {
			d.timeoutS = clampTimeoutS(v)
		}
	case "apikey":
		d.apiKey = d.editBuf
	}
	d.editBuf = ""
}

func clampTemp(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 2 {
		return 2
	}
	return v
}

func clampTokens(v int) int {
	if v < 256 {
		return 256
	}
	if v > 200_000 {
		return 200_000
	}
	return v
}

// clampTokensForCtx keeps max_tokens ≤ ~20% of the context window so most of
// num_ctx stays for the prompt (LM Studio-friendly). Hard floor 256.
func clampTokensForCtx(tokens int, numCtx int64) int {
	tokens = clampTokens(tokens)
	if numCtx <= 0 {
		return tokens
	}
	capTok := numCtx / 5 // ~20% for the answer
	if capTok < 256 {
		capTok = 256
	}
	if int64(tokens) > capTok {
		return int(capTok)
	}
	return tokens
}

func clampCtx(v, maxModel int64) int64 {
	if v < 1024 {
		return 1024
	}
	if maxModel > 0 && v > maxModel {
		return maxModel
	}
	if v > 1_048_576 {
		return 1_048_576
	}
	return v
}

// clampTimeoutS bounds a single LLM step wait. Floor 30s (local probe),
// ceiling 2h (Claude Code allows effectively unbounded; we keep a sane cap).
func clampTimeoutS(v int) int {
	if v < 30 {
		return 30
	}
	if v > 7200 {
		return 7200
	}
	return v
}

func (d *SettingsDialog) adjustField(kind string, dir int) {
	switch kind {
	case "temp":
		d.temperature = clampTemp(d.temperature + float32(dir)*0.05)
	case "tokens":
		step := 1024
		if d.maxTokens >= 16384 {
			step = 4096
		}
		d.maxTokens = clampTokensForCtx(d.maxTokens+dir*step, d.numCtx)
	case "ctx":
		step := int64(2048)
		if d.numCtx >= 32768 {
			step = 8192
		}
		if d.numCtx >= 131_072 {
			step = 32_768
		}
		d.numCtx = clampCtx(d.numCtx+int64(dir)*step, d.model.MaxContextLength)
		d.syncAutoAnswer()
		if !d.provider.AutoAnswerBudget() {
			d.maxTokens = clampTokensForCtx(d.maxTokens, d.numCtx)
		}
	case "timeout":
		step := 30
		if d.timeoutS >= 600 {
			step = 60
		}
		d.timeoutS = clampTimeoutS(d.timeoutS + dir*step)
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
	if d.provider.AutoAnswerBudget() {
		ans := autoAnswerBudget(d.numCtx)
		modelLine = fitInner(mutedStyle.Render(fmt.Sprintf(
			"  %s · %s · answer auto %s",
			d.provider.Name, d.model.ID, formatTokenCount(ans),
		)))
	}

	flds := d.fields()
	const labelCol = 18
	const inset = "  "

	var rows []string
	for i, f := range flds {
		active := i == d.cursor
		valueText := d.fieldDisplay(f.kind, active)
		hint := d.fieldHint(f.kind, active)

		row := inset + fmt.Sprintf("%-*s", labelCol, f.label)
		if active {
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

	hint := fitInner(mutedStyle.Render("↑↓ field · type value · ←→ step · Enter save · Esc back"))
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

func (d *SettingsDialog) fieldDisplay(kind string, active bool) string {
	if active && d.editBuf != "" {
		return d.editBuf + "▋"
	}
	if active && kind == "apikey" {
		if d.apiKey == "" && d.editBuf == "" {
			return "▋"
		}
	}
	switch kind {
	case "temp":
		return fmt.Sprintf("%.2f", d.temperature)
	case "ctx":
		return fmt.Sprintf("%d", d.numCtx)
	case "tokens":
		return fmt.Sprintf("%d", d.maxTokens)
	case "timeout":
		return fmt.Sprintf("%d", d.timeoutS)
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
	case "temp", "tokens", "timeout", "apikey":
		return "type · ←→ step"
	case "ctx":
		if d.provider.AutoAnswerBudget() {
			return "window · answer auto · ←→"
		}
		return "type · ←→ step"
	case "thinking":
		return "←→ toggle"
	}
	return ""
}
