package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

const appVersion = "v0.7"

// AppVersion returns the TUI display version string.
func AppVersion() string { return appVersion }

// WelcomeInfo contains project metadata shown on the empty-state welcome screen.
type WelcomeInfo struct {
	ProjectPath  string // full path to workspace root
	ProjectName  string // display name (base of path)
	ModelName    string // configured model, or "" if none
	SessionCount int    // number of past sessions in this project
}

// LogoStyle selects which ASCII art is rendered for the welcome screen logo.
// "opencode" = block-letters (█▀▄) in the opencode aesthetic, the new default.
// "calvin"   = the original Calvin-S box-drawing style we shipped first.
const LogoStyle = "opencode"

// --- Calvin-S box-drawing variant (original) ---

const orchArtCalvin = "╔═╗ ╦═╗ ╔═╗ ╦ ╦ ╔═╗ ╔═╗ ╔╦╗ ╦═╗ ╔═╗\n" +
	"║ ║ ╠╦╝ ║   ╠═╣ ╠═  ╚═╗  ║  ╠╦╝ ╠═╣\n" +
	"╚═╝ ╩╚═ ╚═╝ ╩ ╩ ╚═╝ ╚═╝  ╩  ╩╚═ ╚ ╝"

const codeArtCalvin = "╔═╗ ╔═╗ ╔╦╗ ╔═╗\n" +
	"║   ║ ║ ║ ║ ╠═ \n" +
	"╚═╝ ╚═╝ ╚╩╝ ╚═╝"

// --- opencode block-letter variant ---

const orchArtOpenCode = "█▀▀█ █▀▀█ █▀▀▀ █  █ █▀▀▀ █▀▀▀ ▀█▀▀ █▀▀█ █▀▀█\n" +
	"█  █ █▀▀▄ █    █▀▀█ █▀▀  ▀▀▀▄  █   █▀▀▄ █▀▀█\n" +
	"▀▀▀▀ █  ▀ ▀▀▀▀ █  █ ▀▀▀▀ ▀▀▀▀  ▀   █  ▀ █  █"

const codeArtOpenCode = "█▀▀▀ █▀▀█ █▀▀▄ █▀▀▀\n" +
	"█    █  █ █  █ █▀▀ \n" +
	"▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀"

var (
	orchArt = pickArt(orchArtOpenCode, orchArtCalvin)
	codeArt = pickArt(codeArtOpenCode, codeArtCalvin)
)

func pickArt(opencode, calvin string) string {
	if LogoStyle == "calvin" {
		return calvin
	}
	return opencode
}

// ThemeForApp exposes the current theme to app.go callers.
func ThemeForApp() theme.Theme { return theme.CurrentTheme() }

// gradientShades is the discrete grey palette stepping from darkest (left)
// to lightest (right) across ORCHESTRA.
var gradientShades = []lipgloss.Color{
	"#5a5a5a",
	"#6a6a6a",
	"#7a7a7a",
	"#8a8a8a",
	"#9a9a9a",
	"#aaaaaa",
	"#bababa",
	"#cacaca",
	"#dadada",
}

// RenderWelcomeLogo returns the ORCHESTRA + CODE block on a single line.
func RenderWelcomeLogo() string {
	orch := renderGradientLogo(orchArt)
	code := renderBrightLogo(codeArt)
	return lipgloss.JoinHorizontal(lipgloss.Top, orch, "    ", code)
}

// renderGradientLogo paints each letter with a different shade from gradientShades.
func renderGradientLogo(art string) string {
	const letterWidth = 5 // 4 cols + 1 space gap
	lines := strings.Split(art, "\n")

	out := make([]string, len(lines))
	for li, line := range lines {
		runes := []rune(line)
		var b strings.Builder
		for i := 0; i < len(runes); i += letterWidth {
			end := i + letterWidth
			if end > len(runes) {
				end = len(runes)
			}
			letterIdx := i / letterWidth
			shade := gradientShades[letterIdx%len(gradientShades)]
			style := lipgloss.NewStyle().Foreground(shade)
			b.WriteString(style.Render(string(runes[i:end])))
		}
		out[li] = b.String()
	}
	return strings.Join(out, "\n")
}

// renderBrightLogo paints all rows in solid bright text, bold.
func renderBrightLogo(art string) string {
	t := theme.CurrentTheme()
	return lipgloss.NewStyle().Foreground(t.Text()).Bold(true).Render(art)
}

// welcomeScreen — port of OpenCode initialScreen+header+lspsConfigured.
func (c Chat) welcomeScreen() string {
	w := c.width
	if w == 0 {
		w = c.vp.Width
	}
	return lipgloss.NewStyle().Width(w).Render(
		lipgloss.JoinVertical(
			lipgloss.Top,
			c.welcomeLogo(w),
			c.welcomeVersion(w),
			"",
			c.welcomeCwd(w),
			"",
			c.welcomeProjectInfo(w),
		),
	)
}

func (c Chat) welcomeLogo(width int) string {
	return lipgloss.NewStyle().Width(width).Render(RenderWelcomeLogo())
}

func (c Chat) welcomeVersion(width int) string {
	t := theme.CurrentTheme()
	return lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Width(width).
		Render(appVersion + " · AI coding assistant")
}

func (c Chat) welcomeCwd(width int) string {
	t := theme.CurrentTheme()
	path := c.welcome.ProjectPath
	if path == "" {
		path = "."
	}
	return lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Width(width).
		Render("cwd: " + path)
}

func (c Chat) welcomeProjectInfo(width int) string {
	t := theme.CurrentTheme()
	base := lipgloss.NewStyle()

	title := base.Width(width).Foreground(t.Primary()).Bold(true).Render("Project")

	name := c.welcome.ProjectName
	if name == "" {
		name = "(unknown)"
	}
	nameLine := base.Width(width).Render(
		base.Foreground(t.Text()).Render("• ") +
			base.Foreground(t.Text()).Render(name),
	)

	var modelLine string
	if c.welcome.ModelName == "" {
		modelLine = base.Width(width).Render(
			base.Foreground(t.Text()).Render("• model: ") +
				base.Foreground(t.Warning()).Render("не выбрана ") +
				base.Foreground(t.TextMuted()).Render("(ctrl+o)"),
		)
	} else {
		modelLine = base.Width(width).Render(
			base.Foreground(t.Text()).Render("• model: ") +
				base.Foreground(t.Text()).Render(c.welcome.ModelName),
		)
	}

	var sessLine string
	if c.welcome.SessionCount > 0 {
		sessLine = base.Width(width).Render(
			base.Foreground(t.Text()).Render("• ") +
				base.Foreground(t.Text()).Render(fmt.Sprintf("sessions: %d ", c.welcome.SessionCount)) +
				base.Foreground(t.TextMuted()).Render("(ctrl+s)"),
		)
	}

	rows := []string{title, nameLine, modelLine}
	if sessLine != "" {
		rows = append(rows, sessLine)
	}
	return base.Width(width).Render(
		lipgloss.JoinVertical(lipgloss.Left, rows...),
	)
}
