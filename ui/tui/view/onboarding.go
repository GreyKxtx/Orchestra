package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/internal/lmstudio"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// OnboardingStep identifies which step of the wizard is active.
type OnboardingStep int

const (
	OnboardingProvider OnboardingStep = iota // step 1: choose provider
	OnboardingModel                          // step 2: choose model
	OnboardingSettings                       // step 3: configure settings
)

// Provider is a selectable LLM provider in the onboarding flow.
type Provider struct {
	Name     string
	Endpoint string
}

// DefaultProviders is the list shown in step 1.
var DefaultProviders = []Provider{
	{"LM Studio", "http://localhost:1234"},
	{"Ollama", "http://localhost:11434"},
	{"OpenAI", "https://api.openai.com"},
	{"Custom", ""},
}

// ModelSettings holds the user-configured model parameters from step 3.
type ModelSettings struct {
	Temperature    float32
	MaxTokens      int
	NumCtx         int64
	EnableThinking bool
}

// OnboardingView renders the onboarding wizard.
type OnboardingView struct {
	Step           OnboardingStep
	Providers      []Provider
	ProviderCursor int
	CustomEndpoint string
	editingCustom  bool

	Models        []lmstudio.RemoteModel
	ModelCursor   int
	LoadingModels bool
	ModelError    string

	Settings       ModelSettings
	settingsCursor int // 0=temp 1=maxTokens 2=numCtx 3=thinking

	screenW int
	screenH int
}

// NewOnboardingView creates an onboarding view with defaults.
func NewOnboardingView(screenW, screenH int) *OnboardingView {
	return &OnboardingView{
		Step:      OnboardingProvider,
		Providers: DefaultProviders,
		Settings: ModelSettings{
			Temperature: 0.20,
			MaxTokens:   8192,
			NumCtx:      20480,
		},
		screenW: screenW,
		screenH: screenH,
	}
}

// SetScreenSize updates known terminal dimensions.
func (o *OnboardingView) SetScreenSize(w, h int) { o.screenW = w; o.screenH = h }

// SelectedProvider returns the currently highlighted provider.
func (o *OnboardingView) SelectedProvider() Provider {
	if o.ProviderCursor < len(o.Providers) {
		return o.Providers[o.ProviderCursor]
	}
	return Provider{}
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
	}
}
func (o *OnboardingView) ProviderCursorDown() {
	if o.ProviderCursor < len(o.Providers)-1 {
		o.ProviderCursor++
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
	if o.settingsCursor > 0 {
		o.settingsCursor--
	}
}
func (o *OnboardingView) SettingsCursorDown() {
	if o.settingsCursor < 3 {
		o.settingsCursor++
	}
}

// AdjustSetting changes the currently focused setting value.
// delta is +1 or -1.
func (o *OnboardingView) AdjustSetting(delta int) {
	switch o.settingsCursor {
	case 0: // temperature
		o.Settings.Temperature += float32(delta) * 0.05
		if o.Settings.Temperature < 0 {
			o.Settings.Temperature = 0
		}
		if o.Settings.Temperature > 2 {
			o.Settings.Temperature = 2
		}
	case 1: // max_tokens
		o.Settings.MaxTokens += delta * 256
		if o.Settings.MaxTokens < 256 {
			o.Settings.MaxTokens = 256
		}
	case 2: // num_ctx
		o.Settings.NumCtx += int64(delta) * 1024
		if o.Settings.NumCtx < 1024 {
			o.Settings.NumCtx = 1024
		}
	case 3: // thinking
		o.Settings.EnableThinking = !o.Settings.EnableThinking
	}
}

// TypeCustomEndpoint appends a rune when editing a custom endpoint.
func (o *OnboardingView) TypeCustomEndpoint(r rune) {
	if o.editingCustom {
		o.CustomEndpoint += string(r)
	}
}

// BackspaceCustomEndpoint removes last rune from custom endpoint.
func (o *OnboardingView) BackspaceCustomEndpoint() {
	if o.editingCustom && len(o.CustomEndpoint) > 0 {
		o.CustomEndpoint = o.CustomEndpoint[:len(o.CustomEndpoint)-1]
	}
}

// IsEditingCustom reports whether the user is typing a custom URL.
func (o *OnboardingView) IsEditingCustom() bool { return o.editingCustom }

// ToggleCustomEdit starts or stops custom endpoint editing.
func (o *OnboardingView) ToggleCustomEdit() { o.editingCustom = !o.editingCustom }

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
		isCustom := p.Name == "Custom"
		line := fmt.Sprintf("  %s", p.Name)
		if !isCustom {
			line += fmt.Sprintf("  (%s)", p.Endpoint)
		}
		if i == o.ProviderCursor {
			sb.WriteString(selectedStyle.Width(w - 4).Render(line))
		} else {
			sb.WriteString(normalStyle.Width(w - 4).Render(line))
		}
		sb.WriteString("\n")
		if isCustom && i == o.ProviderCursor {
			url := o.CustomEndpoint
			if o.editingCustom {
				url += "▋"
			}
			sb.WriteString(muted.Render("    URL: " + url))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(muted.Render("↑↓ выбор · Enter продолжить"))
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
		var val string
		if i == o.settingsCursor {
			val = selectedValueStyle.Render("[ " + f.value + " ]")
		} else {
			val = valueStyle.Render("[ " + f.value + " ]")
		}
		sb.WriteString(label + val)
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(muted.Render("↑↓ поля · ←→ значение · Enter сохранить"))
	return sb.String()
}
