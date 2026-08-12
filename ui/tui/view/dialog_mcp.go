package view

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// MCPServerView is one MCP server row for the list dialog (config projection).
type MCPServerView struct {
	Name         string
	Command      []string
	Env          map[string]string
	Disabled     bool
	CallTimeoutS int
	AllowedTools []string
}

// MCPListDialog lists configured MCP servers and an "+ Add" action.
type MCPListDialog struct {
	servers    []MCPServerView
	cursor     int
	confirmDel bool
	workspace  string // project root for filesystem preset
}

// NewMCPListDialog seeds the list from config servers.
func NewMCPListDialog(servers []MCPServerView, workspaceRoot string) *MCPListDialog {
	return &MCPListDialog{servers: servers, workspace: workspaceRoot}
}

// SetServers replaces the list (after save/delete) and clamps the cursor.
func (d *MCPListDialog) SetServers(servers []MCPServerView) {
	d.servers = servers
	d.confirmDel = false
	if d.cursor >= d.rowCount() {
		d.cursor = d.rowCount() - 1
	}
	if d.cursor < 0 {
		d.cursor = 0
	}
}

func (d *MCPListDialog) rowCount() int {
	return len(d.servers) + 1 // trailing "+ Add MCP server"
}

// Update implements Dialog.
func (d *MCPListDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	n := d.rowCount()
	switch km.String() {
	case "up", "ctrl+p":
		if d.cursor > 0 {
			d.cursor--
		}
		d.confirmDel = false
	case "down", "ctrl+n":
		if d.cursor < n-1 {
			d.cursor++
		}
		d.confirmDel = false
	case "esc", "left":
		if d.confirmDel {
			d.confirmDel = false
			return d, nil
		}
		return d, resultCmd(MCPListDialogMsg{Action: MCPListCancel})
	case "enter", "right":
		if d.cursor == len(d.servers) {
			return d, resultCmd(MCPListDialogMsg{Action: MCPListAdd})
		}
		if d.cursor < 0 || d.cursor >= len(d.servers) {
			return d, nil
		}
		if d.confirmDel {
			name := d.servers[d.cursor].Name
			d.confirmDel = false
			return d, resultCmd(MCPListDialogMsg{Action: MCPListDelete, ServerName: name})
		}
		return d, resultCmd(MCPListDialogMsg{Action: MCPListEdit, Server: d.servers[d.cursor]})
	case "a", "n":
		d.confirmDel = false
		return d, resultCmd(MCPListDialogMsg{Action: MCPListAdd})
	case "d":
		// Toggle disabled for the selected server.
		if d.cursor >= 0 && d.cursor < len(d.servers) {
			d.confirmDel = false
			return d, resultCmd(MCPListDialogMsg{Action: MCPListToggle, ServerName: d.servers[d.cursor].Name})
		}
	case "t":
		if d.cursor >= 0 && d.cursor < len(d.servers) {
			d.confirmDel = false
			return d, resultCmd(MCPListDialogMsg{Action: MCPListTest, ServerName: d.servers[d.cursor].Name})
		}
		return d, resultCmd(MCPListDialogMsg{Action: MCPListTest}) // empty = all
	case "ctrl+d":
		if d.cursor >= 0 && d.cursor < len(d.servers) {
			d.confirmDel = true
		}
	}
	return d, nil
}

// Render implements Dialog.
func (d *MCPListDialog) Render(screenW, screenH int) string {
	items := make([]listDialogItem, 0, d.rowCount())
	for _, s := range d.servers {
		cmd := strings.Join(s.Command, " ")
		if cmd == "" {
			cmd = "(no command)"
		}
		status := "enabled"
		if s.Disabled {
			status = "disabled"
		}
		desc := status + "  ·  " + truncRunes(cmd, 48)
		items = append(items, listDialogItem{
			Title:       s.Name,
			Description: desc,
			Ready:       !s.Disabled && len(s.Command) > 0,
			Disabled:    s.Disabled,
			Category:    "Configured",
		})
	}
	items = append(items, listDialogItem{
		Title:       "+ Add MCP server",
		Description: "preset или custom command",
		Category:    "Actions",
	})

	hint := "↑↓ · Enter edit/add · a add · d toggle · t test · Ctrl+D delete · Esc"
	if d.confirmDel {
		hint = "Enter — удалить сервер · Esc — отмена"
	}
	title := "MCP servers"
	if len(d.servers) == 0 {
		title = "MCP servers — none yet"
	}
	return renderListDialog(title, items, d.cursor, "", "", hint, screenW, screenH)
}

// ── Presets ──────────────────────────────────────────────────────────────────

// MCPPreset is a one-click starter for a common MCP server.
type MCPPreset struct {
	Key     string
	Title   string
	Desc    string
	Name    string
	Command []string
	EnvHint string // shown in edit dialog, not auto-filled secrets
}

// MCPPresets returns catalog entries; workspaceRoot fills filesystem path.
func MCPPresets(workspaceRoot string) []MCPPreset {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		root = "."
	}
	return []MCPPreset{
		{
			Key:     "filesystem",
			Title:   "Filesystem",
			Desc:    "чтение/запись файлов в project root",
			Name:    "filesystem",
			Command: []string{"npx", "-y", "@modelcontextprotocol/server-filesystem", root},
		},
		{
			Key:     "github",
			Title:   "GitHub",
			Desc:    "issues / PRs · нужен GITHUB_PERSONAL_ACCESS_TOKEN",
			Name:    "github",
			Command: []string{"npx", "-y", "@modelcontextprotocol/server-github"},
			EnvHint: "GITHUB_PERSONAL_ACCESS_TOKEN",
		},
		{
			Key:     "memory",
			Title:   "Memory (knowledge graph)",
			Desc:    "локальный memory MCP",
			Name:    "memory",
			Command: []string{"npx", "-y", "@modelcontextprotocol/server-memory"},
		},
		{
			Key:   "custom",
			Title: "Custom…",
			Desc:  "свой command / npx пакет",
			Name:  "",
		},
	}
}

// MCPPresetDialog picks a starter template before the edit form.
type MCPPresetDialog struct {
	presets []MCPPreset
	cursor  int
}

// NewMCPPresetDialog builds the preset picker.
func NewMCPPresetDialog(workspaceRoot string) *MCPPresetDialog {
	return &MCPPresetDialog{presets: MCPPresets(workspaceRoot)}
}

// Update implements Dialog.
func (d *MCPPresetDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
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
		if d.cursor < len(d.presets)-1 {
			d.cursor++
		}
	case "esc", "left":
		return d, resultCmd(MCPPresetDialogMsg{Cancel: true})
	case "enter", "right":
		if d.cursor >= 0 && d.cursor < len(d.presets) {
			return d, resultCmd(MCPPresetDialogMsg{Preset: d.presets[d.cursor]})
		}
	}
	return d, nil
}

// Render implements Dialog.
func (d *MCPPresetDialog) Render(screenW, screenH int) string {
	items := make([]listDialogItem, 0, len(d.presets))
	for _, p := range d.presets {
		items = append(items, listDialogItem{
			Title:       p.Title,
			Description: p.Desc,
			Category:    "Presets",
		})
	}
	return renderListDialog(
		"Add MCP server",
		items,
		d.cursor,
		"",
		"",
		"↑↓ · Enter выбрать · Esc назад",
		screenW, screenH,
	)
}

// ── Edit form ────────────────────────────────────────────────────────────────

// MCPEditDialogResult is emitted on save.
type MCPEditDialogResult struct {
	OriginalName string // empty when adding; used to rename/replace
	Server       MCPServerView
}

// MCPEditDialog edits one MCP server entry.
type MCPEditDialog struct {
	originalName string
	name         string
	command      string // space/quote-joined argv
	env          string // KEY=val; KEY2=val2
	timeoutS     string
	allowed      string // comma-separated
	disabled     bool
	field        int
	errMsg       string
	envHint      string
}

const (
	mcpFieldName = iota
	mcpFieldCommand
	mcpFieldEnv
	mcpFieldTimeout
	mcpFieldAllowed
	mcpFieldDisabled
	mcpFieldCount
)

// NewMCPEditDialog creates an empty add form.
func NewMCPEditDialog() *MCPEditDialog {
	return &MCPEditDialog{timeoutS: "0"}
}

// NewMCPEditDialogFromView edits an existing server.
func NewMCPEditDialogFromView(s MCPServerView) *MCPEditDialog {
	return &MCPEditDialog{
		originalName: s.Name,
		name:         s.Name,
		command:      joinCommand(s.Command),
		env:          joinEnv(s.Env),
		timeoutS:     strconv.Itoa(s.CallTimeoutS),
		allowed:      strings.Join(s.AllowedTools, ", "),
		disabled:     s.Disabled,
	}
}

// NewMCPEditDialogFromPreset seeds from a catalog preset.
func NewMCPEditDialogFromPreset(p MCPPreset) *MCPEditDialog {
	d := NewMCPEditDialog()
	d.name = p.Name
	d.command = joinCommand(p.Command)
	d.envHint = p.EnvHint
	return d
}

func (d *MCPEditDialog) fieldLabels() []string {
	return []string{
		"Name",
		"Command",
		"Env (KEY=val; …)",
		"Call timeout (s)",
		"Allowed tools",
		"Disabled",
	}
}

func (d *MCPEditDialog) fieldValue(i int) string {
	switch i {
	case mcpFieldName:
		return d.name
	case mcpFieldCommand:
		return d.command
	case mcpFieldEnv:
		return d.env
	case mcpFieldTimeout:
		return d.timeoutS
	case mcpFieldAllowed:
		return d.allowed
	case mcpFieldDisabled:
		if d.disabled {
			return "yes"
		}
		return "no"
	default:
		return ""
	}
}

func (d *MCPEditDialog) setFieldValue(i int, v string) {
	switch i {
	case mcpFieldName:
		d.name = v
	case mcpFieldCommand:
		d.command = v
	case mcpFieldEnv:
		d.env = v
	case mcpFieldTimeout:
		d.timeoutS = v
	case mcpFieldAllowed:
		d.allowed = v
	}
}

// Update implements Dialog.
func (d *MCPEditDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	switch km.String() {
	case "esc":
		return d, resultCmd(MCPEditDialogMsg{Cancel: true})
	case "tab", "down", "ctrl+n":
		d.field = (d.field + 1) % mcpFieldCount
		d.errMsg = ""
	case "shift+tab", "up", "ctrl+p":
		d.field = (d.field - 1 + mcpFieldCount) % mcpFieldCount
		d.errMsg = ""
	case "enter":
		srv, err := d.buildServer()
		if err != nil {
			d.errMsg = err.Error()
			return d, nil
		}
		return d, resultCmd(MCPEditDialogMsg{Result: MCPEditDialogResult{
			OriginalName: d.originalName,
			Server:       srv,
		}})
	case " ":
		if d.field == mcpFieldDisabled {
			d.disabled = !d.disabled
			d.errMsg = ""
			return d, nil
		}
		fallthrough
	case "backspace":
		d.errMsg = ""
		if d.field == mcpFieldDisabled {
			return d, nil
		}
		v := []rune(d.fieldValue(d.field))
		if len(v) > 0 {
			d.setFieldValue(d.field, string(v[:len(v)-1]))
		}
	case "ctrl+u":
		d.errMsg = ""
		if d.field != mcpFieldDisabled {
			d.setFieldValue(d.field, "")
		}
	default:
		if d.field == mcpFieldDisabled {
			switch km.String() {
			case "y", "1":
				d.disabled = true
			case "n", "0":
				d.disabled = false
			}
			return d, nil
		}
		if len(km.Runes) == 0 {
			break
		}
		d.errMsg = ""
		d.setFieldValue(d.field, d.fieldValue(d.field)+string(km.Runes))
	}
	return d, nil
}

func (d *MCPEditDialog) buildServer() (MCPServerView, error) {
	name := strings.TrimSpace(d.name)
	if name == "" {
		d.field = mcpFieldName
		return MCPServerView{}, fmt.Errorf("нужно имя сервера")
	}
	if strings.Contains(name, ":") {
		d.field = mcpFieldName
		return MCPServerView{}, fmt.Errorf("имя не должно содержать ':'")
	}
	cmd := splitCommand(strings.TrimSpace(d.command))
	if len(cmd) == 0 {
		d.field = mcpFieldCommand
		return MCPServerView{}, fmt.Errorf("нужна команда запуска")
	}
	timeout := 0
	if ts := strings.TrimSpace(d.timeoutS); ts != "" {
		n, err := strconv.Atoi(ts)
		if err != nil || n < 0 {
			d.field = mcpFieldTimeout
			return MCPServerView{}, fmt.Errorf("timeout: целое ≥ 0")
		}
		timeout = n
	}
	return MCPServerView{
		Name:         name,
		Command:      cmd,
		Env:          parseEnv(d.env),
		Disabled:     d.disabled,
		CallTimeoutS: timeout,
		AllowedTools: parseCSV(d.allowed),
	}, nil
}

// Render implements Dialog.
func (d *MCPEditDialog) Render(screenW, screenH int) string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	modalW := screenW * 70 / 100
	if modalW < 52 {
		modalW = 52
	}
	if modalW > 96 {
		modalW = 96
	}
	if maxW := screenW - 4; modalW > maxW {
		modalW = maxW
	}
	inner := modalW - 8

	base := lipgloss.NewStyle().Background(bg)
	titleStyle := base.Foreground(t.Text()).Bold(true)
	muted := base.Foreground(t.TextMuted())
	text := base.Foreground(t.Text())
	errStyle := base.Foreground(t.Error())
	sel := lipgloss.NewStyle().Background(t.Primary()).Foreground(t.Background()).Bold(true).Width(inner)

	padBg := func(n int) string {
		if n <= 0 {
			return ""
		}
		return base.Render(strings.Repeat(" ", n))
	}
	fit := func(s string) string {
		w := lipgloss.Width(s)
		if w < inner {
			return s + padBg(inner-w)
		}
		return s
	}

	title := "Edit MCP server"
	if d.originalName == "" {
		title = "Add MCP server"
	}
	titleR := titleStyle.Render(title)
	esc := muted.Render("esc")
	gap := inner - lipgloss.Width(titleR) - lipgloss.Width(esc)
	if gap < 1 {
		gap = 1
	}
	header := titleR + padBg(gap) + esc
	blank := padBg(inner)

	labels := d.fieldLabels()
	var rows []string
	for i, label := range labels {
		val := d.fieldValue(i)
		display := val
		if i == mcpFieldDisabled {
			if d.disabled {
				display = "yes  (Space toggle)"
			} else {
				display = "no   (Space toggle)"
			}
		} else if i == d.field {
			display = val + "▋"
		}
		line := fmt.Sprintf("%-18s %s", label, truncRunes(display, inner-20))
		if i == d.field {
			rows = append(rows, sel.Render("  "+line))
		} else {
			pad := 18 - len(label)
			if pad < 1 {
				pad = 1
			}
			rows = append(rows, fit(text.Render("  "+label)+muted.Render(strings.Repeat(" ", pad)+truncRunes(display, inner-20))))
		}
	}

	hint := fit(muted.Render("Tab поля · Enter сохранить · Esc отмена"))
	if d.envHint != "" {
		hint = fit(muted.Render("Env hint: " + d.envHint + " · Tab · Enter · Esc"))
	}
	errLine := blank
	if d.errMsg != "" {
		errLine = fit(errStyle.Render("✗  " + d.errMsg))
	}

	sections := []string{blank, header, blank}
	sections = append(sections, rows...)
	sections = append(sections, blank, errLine, hint, blank)
	body := lipgloss.JoinVertical(lipgloss.Left, sections...)
	box := lipgloss.NewStyle().Background(bg).Padding(0, 4).Width(modalW).Render(body)
	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, box)
}

// ── helpers ──────────────────────────────────────────────────────────────────

func joinCommand(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	parts := make([]string, len(argv))
	for i, a := range argv {
		needsQuote := strings.ContainsAny(a, " \t\"'")
		switch {
		case !needsQuote:
			parts[i] = a
		case !strings.ContainsRune(a, '\''):
			parts[i] = "'" + a + "'"
		case !strings.ContainsRune(a, '"'):
			parts[i] = `"` + a + `"`
		default:
			// Both quote styles present — fall back to Go quoting.
			parts[i] = strconv.Quote(a)
		}
	}
	return strings.Join(parts, " ")
}

// splitCommand parses a shell-ish argv line with "double" and 'single' quotes.
func splitCommand(s string) []string {
	var out []string
	var b strings.Builder
	var quote rune // 0 | '"' | '\''
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
		case unicode.IsSpace(r):
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}

func joinEnv(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	parts := make([]string, 0, len(env))
	for k, v := range env {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}

func parseEnv(s string) map[string]string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	out := map[string]string{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ';' || r == '\n'
	}) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// MCPServerViewFromConfig converts config → dialog view.
func MCPServerViewFromConfig(s config.MCPServerConfig) MCPServerView {
	return MCPServerView{
		Name:         s.Name,
		Command:      append([]string(nil), s.Command...),
		Env:          cloneStringMap(s.Env),
		Disabled:     s.Disabled,
		CallTimeoutS: s.CallTimeoutS,
		AllowedTools: append([]string(nil), s.AllowedTools...),
	}
}

// ToConfig converts dialog view → config.
func (s MCPServerView) ToConfig() config.MCPServerConfig {
	return config.MCPServerConfig{
		Name:         s.Name,
		Command:      append([]string(nil), s.Command...),
		Env:          cloneStringMap(s.Env),
		Disabled:     s.Disabled,
		CallTimeoutS: s.CallTimeoutS,
		AllowedTools: append([]string(nil), s.AllowedTools...),
	}
}

func cloneStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
