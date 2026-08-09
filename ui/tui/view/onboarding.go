package view

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/llm/lmstudio"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// OnboardingStep identifies which step of the wizard is active.
type OnboardingStep int

const (
	OnboardingProvider OnboardingStep = iota // step 1: choose provider
	OnboardingModel                          // step 2: choose model
	OnboardingSettings                       // step 3: configure settings
)

// OnboardingView renders the onboarding wizard.
type OnboardingView struct {
	Step           OnboardingStep
	Providers      []ProviderEntry
	ProviderCursor int
	EndpointURL    string // editable URL for local/custom providers
	editingURL     bool

	Models        []lmstudio.RemoteModel
	ModelCursor   int
	LoadingModels bool
	ModelError    string

	Settings       ModelSettings
	settingsCursor int
	settingsEdit   string // typed buffer for numeric fields on settings step

	screenW int
	screenH int
}

// ModelSettings holds the user-configured model parameters from step 3.
type ModelSettings struct {
	Temperature    float32
	MaxTokens      int
	NumCtx         int64
	EnableThinking bool
}

// NewOnboardingView creates an onboarding view with defaults.
func NewOnboardingView(screenW, screenH int) *OnboardingView {
	o := &OnboardingView{
		Step:      OnboardingProvider,
		Providers: DialogProviders,
		Settings: ModelSettings{
			Temperature: 0.20,
			MaxTokens:   8192,
			NumCtx:      20480,
		},
		screenW: screenW,
		screenH: screenH,
	}
	o.syncEndpointField()
	return o
}

// SetScreenSize updates known terminal dimensions.
func (o *OnboardingView) SetScreenSize(w, h int) { o.screenW = w; o.screenH = h }

// SelectedProvider returns the currently highlighted provider.
func (o *OnboardingView) SelectedProvider() ProviderEntry {
	if o.ProviderCursor < len(o.Providers) {
		return o.Providers[o.ProviderCursor]
	}
	return ProviderEntry{}
}

// SelectedEndpoint returns the API base for the current provider step.
func (o *OnboardingView) SelectedEndpoint() string {
	p := o.SelectedProvider()
	if p.EndpointEditable && strings.TrimSpace(o.EndpointURL) != "" {
		return NormalizeEndpoint(o.EndpointURL)
	}
	return NormalizeEndpoint(p.Endpoint)
}

// SelectedModel returns the currently highlighted remote model.
func (o *OnboardingView) SelectedModel() lmstudio.RemoteModel {
	if o.ModelCursor < len(o.Models) {
		return o.Models[o.ModelCursor]
	}
	return lmstudio.RemoteModel{}
}

// ProviderCursorUp / Down navigate the provider list.
func (o *OnboardingView) ProviderCursorUp() {
	if o.ProviderCursor > 0 {
		o.ProviderCursor--
		o.syncEndpointField()
	}
}
func (o *OnboardingView) ProviderCursorDown() {
	if o.ProviderCursor < len(o.Providers)-1 {
		o.ProviderCursor++
		o.syncEndpointField()
	}
}

func (o *OnboardingView) syncEndpointField() {
	p := o.SelectedProvider()
	if p.EndpointEditable {
		o.EndpointURL = p.Endpoint
	} else {
		o.EndpointURL = ""
		o.editingURL = false
	}
}

// ModelCursorUp / Down navigate the model list.
func (o *OnboardingView) ModelCursorUp() {
	if o.ModelCursor > 0 {
		o.ModelCursor--
	}
}
func (o *OnboardingView) ModelCursorDown() {
	if o.ModelCursor < len(o.Models)-1 {
		o.ModelCursor++
	}
}

// SettingsCursorUp / Down navigate settings fields.
func (o *OnboardingView) SettingsCursorUp() {
	o.CommitSettingsEdit()
	if o.settingsCursor > 0 {
		o.settingsCursor--
	}
}
func (o *OnboardingView) SettingsCursorDown() {
	o.CommitSettingsEdit()
	if o.settingsCursor < 3 {
		o.settingsCursor++
	}
}

// AdjustSetting steps the focused setting with ←/→ only.
func (o *OnboardingView) AdjustSetting(delta int) {
	o.CommitSettingsEdit()
	switch o.settingsCursor {
	case 0:
		o.Settings.Temperature += float32(delta) * 0.05
		if o.Settings.Temperature < 0 {
			o.Settings.Temperature = 0
		}
		if o.Settings.Temperature > 2 {
			o.Settings.Temperature = 2
		}
	case 1:
		o.Settings.MaxTokens += delta * 256
		if o.Settings.MaxTokens < 256 {
			o.Settings.MaxTokens = 256
		}
	case 2:
		o.Settings.NumCtx += int64(delta) * 1024
		if o.Settings.NumCtx < 1024 {
			o.Settings.NumCtx = 1024
		}
	case 3:
		o.Settings.EnableThinking = !o.Settings.EnableThinking
	}
}

func (o *OnboardingView) CommitSettingsEdit() {
	if o.settingsEdit == "" {
		return
	}
	switch o.settingsCursor {
	case 0:
		if v, err := strconv.ParseFloat(o.settingsEdit, 32); err == nil {
			o.Settings.Temperature = clampTemp(float32(v))
		}
	case 1:
		if v, err := strconv.Atoi(o.settingsEdit); err == nil {
			if v >= 256 {
				o.Settings.MaxTokens = v
			}
		}
	case 2:
		if v, err := strconv.ParseInt(o.settingsEdit, 10, 64); err == nil {
			if v >= 1024 {
				o.Settings.NumCtx = v
			}
		}
	}
	o.settingsEdit = ""
}

func (o *OnboardingView) TypeSettingsEdit(r rune) {
	switch o.settingsCursor {
	case 0:
		if unicode.IsDigit(r) || r == '.' {
			o.settingsEdit += string(r)
		}
	case 1, 2:
		if unicode.IsDigit(r) {
			o.settingsEdit += string(r)
		}
	}
}

func (o *OnboardingView) BackspaceSettingsEdit() {
	if len(o.settingsEdit) > 0 {
		o.settingsEdit = o.settingsEdit[:len(o.settingsEdit)-1]
	}
}

func (o *OnboardingView) settingsFieldDisplay(idx int, committed string) string {
	if idx == o.settingsCursor && o.settingsEdit != "" {
		return o.settingsEdit + "▋"
	}
	return committed
}

// TypeEndpoint appends a rune when editing server URL.
func (o *OnboardingView) TypeEndpoint(r rune) {
	if o.editingURL {
		o.EndpointURL += string(r)
	}
}

// BackspaceEndpoint removes last rune from server URL.
func (o *OnboardingView) BackspaceEndpoint() {
	if o.editingURL && len(o.EndpointURL) > 0 {
		o.EndpointURL = o.EndpointURL[:len(o.EndpointURL)-1]
	}
}

func (o *OnboardingView) IsEditingURL() bool { return o.editingURL }

// ToggleURLEdit starts or stops server URL editing on the provider step.
func (o *OnboardingView) ToggleURLEdit() {
	p := o.SelectedProvider()
	if p.EndpointEditable {
		o.editingURL = !o.editingURL
	}
}

// Render returns the onboarding screen string (full screen overlay).
func (o *OnboardingView) Render() string {
	t := theme.CurrentTheme()
	const boxWidth = 48

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary()).
		Background(t.BackgroundSecondary()).
		Padding(1, 2).
		Width(boxWidth)

	var content string
	switch o.Step {
	case OnboardingProvider:
		content = o.renderProvider(t, boxWidth)
	case OnboardingModel:
		content = o.renderModel(t, boxWidth)
	case OnboardingSettings:
		content = o.renderSettings(t, boxWidth)
	}

	box := border.Render(content)
	return lipgloss.Place(o.screenW, o.screenH, lipgloss.Center, lipgloss.Center, box)
}

func (o *OnboardingView) renderProvider(t theme.Theme, w int) string {
	titleStyle := lipgloss.NewStyle().Background(t.BackgroundSecondary()).Foreground(t.Primary()).Bold(true)
	normalStyle := lipgloss.NewStyle().Background(t.BackgroundSecondary()).Foreground(t.Text())
	selectedStyle := lipgloss.NewStyle().Background(t.Primary()).Foreground(t.Background()).Bold(true)
	muted := lipgloss.NewStyle().Background(t.BackgroundSecondary()).Foreground(t.TextMuted())

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Выбери провайдера"))
	sb.WriteString("\n\n")

	for i, p := range o.Providers {
		line := fmt.Sprintf("  %s", p.Name)
		if !p.EndpointEditable && p.Endpoint != "" {
			line += fmt.Sprintf("  (%s)", p.Endpoint)
		} else if p.EndpointEditable && p.Endpoint != "" {
			line += fmt.Sprintf("  (%s)", p.Endpoint)
		}
		if i == o.ProviderCursor {
			sb.WriteString(selectedStyle.Width(w - 4).Render(line))
		} else {
			sb.WriteString(normalStyle.Width(w - 4).Render(line))
		}
		sb.WriteString("\n")
		if p.EndpointEditable && i == o.ProviderCursor {
			url := o.EndpointURL
			if o.editingURL {
				url += "▋"
			}
			if url == "" {
				url = "(enter URL)"
			}
			sb.WriteString(muted.Render("    URL: " + url))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	hint := "↑↓ выбор · Enter продолжить"
	if o.SelectedProvider().EndpointEditable {
		hint += " · Tab редактировать URL"
	}
	sb.WriteString(muted.Render(hint))
	return sb.String()
}

func (o *OnboardingView) renderModel(t theme.Theme, w int) string {
	titleStyle := lipgloss.NewStyle().Background(t.BackgroundSecondary()).Foreground(t.Primary()).Bold(true)
	normalStyle := lipgloss.NewStyle().Background(t.BackgroundSecondary()).Foreground(t.Text())
	selectedStyle := lipgloss.NewStyle().Background(t.Primary()).Foreground(t.Background()).Bold(true)
	muted := lipgloss.NewStyle().Background(t.BackgroundSecondary()).Foreground(t.TextMuted())
	errStyle := lipgloss.NewStyle().Background(t.BackgroundSecondary()).Foreground(t.Error())

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Выбери модель"))
	sb.WriteString("\n\n")

	switch {
	case o.LoadingModels:
		sb.WriteString(muted.Render("  ⠋ загрузка моделей…"))
	case o.ModelError != "":
		sb.WriteString(errStyle.Render("  ✗ " + o.ModelError))
		sb.WriteString("\n")
		sb.WriteString(muted.Render("  Убедись что LM Studio запущен"))
	case len(o.Models) == 0:
		sb.WriteString(muted.Render("  Нет доступных моделей"))
	default:
		for i, m := range o.Models {
			ctx := ""
			if m.MaxContextLength > 0 {
				ctx = fmt.Sprintf("  ctx: %d", m.MaxContextLength)
			}
			loaded := ""
			if m.IsLoaded {
				loaded = " ✓"
			}
			line := fmt.Sprintf("  %-28s%s%s", m.ID, ctx, loaded)
			if i == o.ModelCursor {
				sb.WriteString(selectedStyle.Width(w - 4).Render(line))
			} else {
				sb.WriteString(normalStyle.Width(w - 4).Render(line))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(muted.Render("↑↓ выбор · Enter настройки"))
	return sb.String()
}

func (o *OnboardingView) renderSettings(t theme.Theme, w int) string {
	titleStyle := lipgloss.NewStyle().Background(t.BackgroundSecondary()).Foreground(t.Primary()).Bold(true)
	labelStyle := lipgloss.NewStyle().Background(t.BackgroundSecondary()).Foreground(t.Text())
	valueStyle := lipgloss.NewStyle().Background(t.BackgroundSecondary()).Foreground(t.Secondary())
	selectedValueStyle := lipgloss.NewStyle().Background(t.Primary()).Foreground(t.Background()).Bold(true)
	muted := lipgloss.NewStyle().Background(t.BackgroundSecondary()).Foreground(t.TextMuted())

	model := o.SelectedModel()

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Настройки: " + model.ID))
	sb.WriteString("\n\n")

	fields := []struct {
		label string
		value string
	}{
		{"temperature  ", fmt.Sprintf("%.2f", o.Settings.Temperature)},
		{"max_tokens   ", fmt.Sprintf("%d", o.Settings.MaxTokens)},
		{"num_ctx      ", fmt.Sprintf("%d", o.Settings.NumCtx)},
		{"thinking mode", map[bool]string{true: "on", false: "off"}[o.Settings.EnableThinking]},
	}

	for i, f := range fields {
		label := labelStyle.Render("  " + f.label + "  ")
		display := o.settingsFieldDisplay(i, f.value)
		var val string
		if i == o.settingsCursor {
			val = selectedValueStyle.Render("[ " + display + " ]")
		} else {
			val = valueStyle.Render("[ " + display + " ]")
		}
		sb.WriteString(label + val)
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(muted.Render("↑↓ поля · ввод цифрами · ←→ шаг · Enter сохранить"))
	return sb.String()
}
