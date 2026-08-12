package view

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// OrchestraRoleKey identifies planner or a worker tier row.
type OrchestraRoleKey string

const (
	OrchestraRolePlanner OrchestraRoleKey = "planner"
	OrchestraRoleLead    OrchestraRoleKey = "lead"
	OrchestraRoleComplex OrchestraRoleKey = "complex"
	OrchestraRoleFocused OrchestraRoleKey = "focused"
	OrchestraRoleMicro   OrchestraRoleKey = "micro"
	OrchestraRoleEmbed   OrchestraRoleKey = "embed"
)

// OrchestraRoleDraft is one editable role row.
type OrchestraRoleDraft struct {
	Key      OrchestraRoleKey
	Label    string
	Provider string // empty = main llm:; else providers: map key
	Model    string // empty = inherit provider/main model
}

// OrchestraDialogResult is returned on save.
type OrchestraDialogResult struct {
	Roles []OrchestraRoleDraft
	Named map[string]OrchestraNamedProvider // providers: snapshot (URL/key/model)
}

// OrchestraNamedProvider is runtime snapshot of one providers: entry (or catalog default).
type OrchestraNamedProvider struct {
	Key        string
	APIBase    string
	APIKey     string
	Model      string
	NeedsKey   bool
	Label      string // human name from catalog when known
	Configured bool   // true if from providers: YAML or edited this session
}

// OrchestraDialogCtx seeds validation against current .orchestra.yml.
type OrchestraDialogCtx struct {
	MainProvider string
	MainAPIBase  string
	MainAPIKey   string
	MainModel    string
	MainNeedsKey bool
	FastProvider string // llm.router.fast_provider; empty → fast option hidden
	Named        map[string]OrchestraNamedProvider
}

// orchestraProvOpt is one selectable provider source in the dialog.
type orchestraProvOpt struct {
	Key   string // "" = main, else providers key
	Label string
}

// OrchestraDialog edits orchestra.planner + tiers provider/model.
type OrchestraDialog struct {
	roles   []OrchestraRoleDraft
	opts    []orchestraProvOpt
	ctx     OrchestraDialogCtx
	cursor  int
	col     int // 0=role, 1=provider, 2=model
	editBuf string
	editing bool
	errMsg  string
}

// NewOrchestraDialog seeds rows from drafts + provider options built from ctx/catalog.
func NewOrchestraDialog(roles []OrchestraRoleDraft, ctx OrchestraDialogCtx) *OrchestraDialog {
	if len(roles) == 0 {
		roles = defaultOrchestraRoles()
	}
	if ctx.Named == nil {
		ctx.Named = map[string]OrchestraNamedProvider{}
	}
	return &OrchestraDialog{
		roles: roles,
		opts:  buildOrchestraProviderOpts(ctx),
		ctx:   ctx,
	}
}

func defaultOrchestraRoles() []OrchestraRoleDraft {
	return []OrchestraRoleDraft{
		{Key: OrchestraRolePlanner, Label: "L5 · Orchestrator"},
		{Key: OrchestraRoleLead, Label: "L4 · Dept Leads"},
		{Key: OrchestraRoleComplex, Label: "L3 · Worker complex"},
		{Key: OrchestraRoleFocused, Label: "L3 · Worker focused"},
		{Key: OrchestraRoleMicro, Label: "L1 · Worker micro"},
		{Key: OrchestraRoleEmbed, Label: "Embeddings"},
	}
}

func buildOrchestraProviderOpts(ctx OrchestraDialogCtx) []orchestraProvOpt {
	out := []orchestraProvOpt{
		{Key: "", Label: "Main"},
	}
	if fp := strings.TrimSpace(ctx.FastProvider); fp != "" {
		label := "Fast · " + fp
		if n, ok := ctx.Named[fp]; ok && n.Label != "" {
			label = "Fast · " + n.Label
		} else if p, ok := FindProviderByKey(fp); ok {
			label = "Fast · " + p.Name
		}
		out = append(out, orchestraProvOpt{Key: fp, Label: label})
	}
	seen := map[string]bool{"": true}
	if fp := strings.TrimSpace(ctx.FastProvider); fp != "" {
		seen[fp] = true
	}
	for _, p := range DialogProviders {
		if seen[p.Key] {
			continue
		}
		seen[p.Key] = true
		out = append(out, orchestraProvOpt{Key: p.Key, Label: p.Name})
	}
	// Extra named providers from YAML not in catalog.
	extras := make([]string, 0)
	for k := range ctx.Named {
		if seen[k] {
			continue
		}
		extras = append(extras, k)
	}
	for i := 0; i < len(extras); i++ {
		for j := i + 1; j < len(extras); j++ {
			if extras[j] < extras[i] {
				extras[i], extras[j] = extras[j], extras[i]
			}
		}
	}
	for _, k := range extras {
		label := k
		if n := ctx.Named[k]; n.Label != "" {
			label = n.Label
		}
		out = append(out, orchestraProvOpt{Key: k, Label: label})
	}
	return out
}

// Update implements Dialog.
func (d *OrchestraDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	d.errMsg = ""
	if d.editing {
		switch km.String() {
		case "esc":
			d.editing = false
			d.editBuf = ""
		case "enter":
			d.roles[d.cursor].Model = strings.TrimSpace(d.editBuf)
			d.editing = false
			d.editBuf = ""
		case "backspace":
			if len(d.editBuf) > 0 {
				r := []rune(d.editBuf)
				d.editBuf = string(r[:len(r)-1])
			}
		default:
			if len(km.Runes) == 1 {
				d.editBuf += string(km.Runes[0])
			}
		}
		return d, nil
	}
	switch km.String() {
	case "up", "ctrl+p":
		if d.cursor > 0 {
			d.cursor--
		}
	case "down", "ctrl+n":
		if d.cursor < len(d.roles)-1 {
			d.cursor++
		}
	case "left":
		if d.col > 0 {
			d.col--
		}
	case "right", "tab":
		d.col = (d.col + 1) % 3
	case "enter":
		switch d.col {
		case 1:
			return d, resultCmd(OrchestraDialogMsg{Action: OrchestraPickProvider, RoleIdx: d.cursor})
		case 2:
			return d, resultCmd(OrchestraDialogMsg{Action: OrchestraPickModel, RoleIdx: d.cursor})
		default:
			return d, d.trySave()
		}
	case "/":
		// Manual model id when list is wrong / offline.
		d.col = 2
		d.editing = true
		d.editBuf = d.roles[d.cursor].Model
	case "ctrl+s":
		return d, d.trySave()
	case "esc":
		return d, resultCmd(OrchestraDialogMsg{Action: OrchestraCancel})
	}
	return d, nil
}

func (d *OrchestraDialog) trySave() tea.Cmd {
	if err := d.validateAll(); err != "" {
		d.errMsg = err
		return nil
	}
	named := make(map[string]OrchestraNamedProvider, len(d.ctx.Named))
	for k, v := range d.ctx.Named {
		named[k] = v
	}
	return resultCmd(OrchestraDialogMsg{Action: OrchestraSave, Result: OrchestraDialogResult{
		Roles: append([]OrchestraRoleDraft(nil), d.roles...),
		Named: named,
	}})
}

// SetRole updates one role's provider/model (used after nested pick dialogs).
func (d *OrchestraDialog) SetRole(idx int, provider, model string) {
	if idx < 0 || idx >= len(d.roles) {
		return
	}
	d.roles[idx].Provider = strings.TrimSpace(provider)
	if model != "" {
		d.roles[idx].Model = strings.TrimSpace(model)
	}
	d.errMsg = ""
	d.col = 0
}

// ApplyProviderChoice updates role + Named snapshot (API URL/key flags) after a pick.
// apiKey non-empty replaces any prior key; empty keeps the previous value.
func (d *OrchestraDialog) ApplyProviderChoice(idx int, key string, entry ProviderEntry, model, apiKey string) {
	d.SetRole(idx, key, model)
	if key == "" {
		return
	}
	if d.ctx.Named == nil {
		d.ctx.Named = map[string]OrchestraNamedProvider{}
	}
	label := entry.Name
	if label == "" {
		label = key
	}
	prev := d.ctx.Named[key]
	keyVal := prev.APIKey
	if strings.TrimSpace(apiKey) != "" {
		keyVal = strings.TrimSpace(apiKey)
	}
	base := entry.Endpoint
	if base == "" {
		base = prev.APIBase
	}
	d.ctx.Named[key] = OrchestraNamedProvider{
		Key:        key,
		APIBase:    base,
		APIKey:     keyVal,
		Model:      model,
		NeedsKey:   entry.NeedsKey,
		Label:      label,
		Configured: true,
	}
	// Refresh display labels for Main/etc.
	d.opts = buildOrchestraProviderOpts(d.ctx)
}

// Ctx returns the validation snapshot (for nested pickers).
func (d *OrchestraDialog) Ctx() OrchestraDialogCtx { return d.ctx }

// RolesSnapshot returns a copy of current drafts.
func (d *OrchestraDialog) RolesSnapshot() []OrchestraRoleDraft {
	return append([]OrchestraRoleDraft(nil), d.roles...)
}

func (d *OrchestraDialog) cycleProvider(delta int) {
	if len(d.opts) == 0 {
		return
	}
	cur := d.roles[d.cursor].Provider
	idx := 0
	for i, o := range d.opts {
		if o.Key == cur {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(d.opts)) % len(d.opts)
	d.roles[d.cursor].Provider = d.opts[idx].Key
}

func (d *OrchestraDialog) providerLabel(key string) string {
	for _, o := range d.opts {
		if o.Key == key {
			return o.Label
		}
	}
	if key == "" {
		return "Main"
	}
	return key
}

func (d *OrchestraDialog) roleStatus(r OrchestraRoleDraft) (ok bool, detail string) {
	if r.Key == OrchestraRoleEmbed && strings.TrimSpace(r.Model) == "" {
		return true, "○ optional"
	}
	model := strings.TrimSpace(r.Model)
	if r.Provider == "" {
		if strings.TrimSpace(d.ctx.MainAPIBase) == "" {
			return false, "main: нет API URL"
		}
		if d.ctx.MainNeedsKey && strings.TrimSpace(d.ctx.MainAPIKey) == "" {
			return false, "main: нужен API key"
		}
		if model == "" && strings.TrimSpace(d.ctx.MainModel) == "" {
			return false, "main: нет model"
		}
		return true, "● ok"
	}
	n, ok := d.ctx.Named[r.Provider]
	if !ok {
		// Catalog default — not yet in providers: map.
		if p, found := FindProviderByKey(r.Provider); found {
			n = OrchestraNamedProvider{
				Key:      p.Key,
				APIBase:  p.Endpoint,
				NeedsKey: p.NeedsKey,
				Label:    p.Name,
			}
		} else {
			return false, "нет в providers:"
		}
	}
	if strings.TrimSpace(n.APIBase) == "" {
		return false, "нет API URL"
	}
	if n.NeedsKey && strings.TrimSpace(n.APIKey) == "" {
		return false, "нужен API key"
	}
	if model == "" && strings.TrimSpace(n.Model) == "" && strings.TrimSpace(d.ctx.MainModel) == "" {
		return false, "укажи model"
	}
	return true, "● ok"
}

func (d *OrchestraDialog) validateAll() string {
	var problems []string
	for _, r := range d.roles {
		ok, detail := d.roleStatus(r)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: %s", r.Label, detail))
		}
	}
	if len(problems) == 0 {
		return ""
	}
	if len(problems) > 2 {
		return problems[0] + "; … — заполни URL/key (/provider) и model"
	}
	return strings.Join(problems, "; ")
}

// Render implements Dialog.
func (d *OrchestraDialog) Render(screenW, screenH int) string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	modalW := screenW - 4
	if modalW > 92 {
		modalW = 92
	}
	if modalW < 36 {
		modalW = 36
	}
	// Prefer ~70% width on wide terminals, but never exceed available.
	if prefer := screenW * 70 / 100; prefer < modalW && prefer >= 36 {
		modalW = prefer
	}
	inner := modalW - 8
	if inner < 28 {
		inner = 28
	}

	base := lipgloss.NewStyle().Background(bg)
	titleStyle := base.Foreground(t.Text()).Bold(true)
	muted := base.Foreground(t.TextMuted())
	textStyle := base.Foreground(t.Text())
	okStyle := base.Foreground(t.Success())
	warnStyle := base.Foreground(t.Warning())
	errStyle := base.Foreground(t.Error())
	selStyle := lipgloss.NewStyle().
		Background(t.Primary()).
		Foreground(t.Background()).
		Bold(true)

	line := func(style lipgloss.Style, s string) string {
		return style.Width(inner).MaxWidth(inner).Render(truncRunes(s, inner))
	}
	blank := base.Width(inner).Render("")

	sub := "Enter на Provider/Model → список.  Main = текущий llm:."
	if inner >= 70 {
		sub = "Enter: список провайдеров/моделей.  Main = глобальный llm: из статус-бара."
	}

	var lines []string
	lines = append(lines,
		blank,
		line(titleStyle, "Orchestra roles"),
		line(muted, sub),
		blank,
	)

	compact := inner < 58
	if compact {
		lines = append(lines, d.renderCompactRows(inner, base, textStyle, muted, okStyle, warnStyle, selStyle)...)
	} else {
		lines = append(lines, d.renderTableRows(inner, base, textStyle, muted, okStyle, warnStyle, selStyle)...)
	}

	hint := "↑↓  Tab  Enter=список  /=model вручную  Ctrl+S  Esc"
	if inner >= 64 {
		hint = "↑↓ роль  Tab поле  Enter открыть список  / model вручную  Ctrl+S  Esc"
	}
	lines = append(lines, blank, line(muted, hint))
	if d.errMsg != "" {
		lines = append(lines,
			line(errStyle, "⚠ "+d.errMsg),
			line(muted, "URL/ключ: /provider, потом снова /orchestra"),
		)
	}
	lines = append(lines, blank)

	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	box := lipgloss.NewStyle().
		Background(bg).
		Padding(0, 2).
		Width(modalW).
		MaxWidth(modalW).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocused()).
		BorderBackground(bg).
		Render(body)
	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, box)
}

func (d *OrchestraDialog) renderTableRows(
	inner int,
	base, textStyle, muted, okStyle, warnStyle, selStyle lipgloss.Style,
) []string {
	// 3 gutters between 4 columns.
	stW := 10
	if inner < 70 {
		stW = 8
	}
	remain := inner - stW - 3
	if remain < 24 {
		remain = 24
		stW = inner - remain - 3
		if stW < 4 {
			stW = 4
			remain = inner - stW - 3
		}
	}
	roleW := remain * 32 / 100
	provW := remain * 30 / 100
	modelW := remain - roleW - provW
	if roleW < 8 {
		roleW = 8
	}
	if provW < 8 {
		provW = 8
	}
	if modelW < 8 {
		modelW = 8
		provW = remain - roleW - modelW
	}

	hdr := fmt.Sprintf("%s %s %s %s",
		truncatePad("Role", roleW),
		truncatePad("Provider", provW),
		truncatePad("Model", modelW),
		truncatePad("Status", stW),
	)
	rule := strings.Repeat("─", inner)
	out := []string{
		muted.Width(inner).MaxWidth(inner).Render(truncRunes(hdr, inner)),
		muted.Width(inner).MaxWidth(inner).Render(rule),
	}

	for i, r := range d.roles {
		prov := d.providerLabel(r.Provider)
		model := r.Model
		if model == "" {
			model = "(inherit)"
		}
		if d.editing && i == d.cursor {
			model = d.editBuf + "▌"
		}
		ok, st := d.roleStatus(r)
		st = truncatePad(st, stW)

		roleCell := r.Label
		provCell := prov
		modelCell := model
		if i == d.cursor {
			switch d.col {
			case 1:
				provCell = "[" + prov + "]"
			case 2:
				modelCell = "[" + model + "]"
			default:
				roleCell = "> " + r.Label
			}
		}
		stCell := truncatePad(st, stW)
		body := fmt.Sprintf("%s %s %s",
			truncatePad(roleCell, roleW),
			truncatePad(provCell, provW),
			truncatePad(modelCell, modelW),
		)
		if i == d.cursor {
			plain := truncRunes(body+" "+stCell, inner)
			out = append(out, selStyle.Width(inner).MaxWidth(inner).Render(plain))
			continue
		}
		if !ok {
			plain := truncRunes(body+" "+stCell, inner)
			out = append(out, warnStyle.Width(inner).MaxWidth(inner).Render(plain))
			continue
		}
		bodyW := inner - stW - 1
		if bodyW < 8 {
			bodyW = 8
		}
		left := textStyle.Width(bodyW).MaxWidth(bodyW).Render(truncRunes(body, bodyW))
		status := okStyle.Width(stW).MaxWidth(stW).Render(stCell)
		joined := lipgloss.JoinHorizontal(lipgloss.Top, left, base.Render(" "), status)
		if pad := inner - lipgloss.Width(joined); pad > 0 {
			joined += base.Render(strings.Repeat(" ", pad))
		}
		out = append(out, joined)
	}
	return out
}

func (d *OrchestraDialog) renderCompactRows(
	inner int,
	base, textStyle, muted, okStyle, warnStyle, selStyle lipgloss.Style,
) []string {
	_ = base
	out := []string{}
	for i, r := range d.roles {
		prov := d.providerLabel(r.Provider)
		model := r.Model
		if model == "" {
			model = "(inherit)"
		}
		if d.editing && i == d.cursor {
			model = d.editBuf + "▌"
		}
		ok, st := d.roleStatus(r)
		mark := "  "
		if i == d.cursor {
			mark = "> "
		}
		title := mark + r.Label
		if i == d.cursor {
			switch d.col {
			case 1:
				title = mark + r.Label + "  [" + prov + "]"
			case 2:
				title = mark + r.Label + "  [" + model + "]"
			}
		}
		detail := fmt.Sprintf("  %s · %s · %s", prov, model, st)
		title = truncRunes(title, inner)
		detail = truncRunes(detail, inner)

		if i == d.cursor {
			out = append(out,
				selStyle.Width(inner).MaxWidth(inner).Render(title),
				selStyle.Width(inner).MaxWidth(inner).Render(detail),
			)
			continue
		}
		topStyle := textStyle
		if !ok {
			topStyle = warnStyle
		}
		// Detail line: mute prefix, green status when ok.
		var bot string
		if ok {
			prefix := truncRunes(fmt.Sprintf("  %s · %s · ", prov, model), inner-6)
			bot = muted.Render(prefix) + okStyle.Render(st)
			if pad := inner - lipgloss.Width(bot); pad > 0 {
				bot += base.Render(strings.Repeat(" ", pad))
			}
		} else {
			bot = warnStyle.Width(inner).MaxWidth(inner).Render(detail)
		}
		out = append(out,
			topStyle.Width(inner).MaxWidth(inner).Render(title),
			bot,
		)
	}
	return out
}

func truncatePad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > width {
		if width == 1 {
			return "…"
		}
		return string(r[:width-1]) + "…"
	}
	for len(r) < width {
		r = append(r, ' ')
	}
	return string(r)
}
