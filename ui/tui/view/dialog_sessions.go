package view

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"

	"github.com/orchestra/orchestra/internal/sessionstore"
)

// SessionsDialog lists past saved sessions with fuzzy filter. Enter selects;
// 'd' deletes the highlighted session after a confirm step.
type SessionsDialog struct {
	all        []sessionstore.SessionMeta
	cursor     int
	scroll     int
	filter     string
	confirmDel bool
	screenH    int // updated every Render; used by Update to keep cursor in view
}

// NewSessionsDialog seeds with already-loaded session metadata.
func NewSessionsDialog(metas []sessionstore.SessionMeta) *SessionsDialog {
	return &SessionsDialog{all: metas, screenH: 24}
}

// maxVisible computes how many list rows fit on screen given fixed overhead.
// Fixed overhead in renderListDialog (with filter row): 8 rows; add 4 safety.
func (d *SessionsDialog) maxVisible() int {
	n := d.screenH - 12
	if n < 3 {
		n = 3
	}
	return n
}

// clampScroll ensures scroll puts cursor inside the visible window.
func (d *SessionsDialog) clampScroll(totalVisible int) {
	mv := d.maxVisible()
	// Scroll down if cursor is below the window.
	if d.cursor >= d.scroll+mv {
		d.scroll = d.cursor - mv + 1
	}
	// Scroll up if cursor is above the window.
	if d.cursor < d.scroll {
		d.scroll = d.cursor
	}
	// Clamp scroll so we don't show empty rows at the bottom.
	maxScroll := totalVisible - mv
	if maxScroll < 0 {
		maxScroll = 0
	}
	if d.scroll > maxScroll {
		d.scroll = maxScroll
	}
	if d.scroll < 0 {
		d.scroll = 0
	}
}

// Update implements Dialog.
func (d *SessionsDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	visible := d.visible()
	switch km.String() {
	case "up", "ctrl+p":
		if d.cursor > 0 {
			d.cursor--
			d.clampScroll(len(visible))
		}
		d.confirmDel = false
	case "down", "ctrl+n":
		if d.cursor < len(visible)-1 {
			d.cursor++
			d.clampScroll(len(visible))
		}
		d.confirmDel = false
	case "esc", "left":
		if d.confirmDel {
			d.confirmDel = false
			return d, nil
		}
		return d, dialogResultCmd("session", "cancel", nil)
	case "enter", "right":
		if d.cursor >= 0 && d.cursor < len(visible) {
			if d.confirmDel {
				id := visible[d.cursor].ID
				d.confirmDel = false
				return d, dialogResultCmd("session", "delete", id)
			}
			return d, dialogResultCmd("session", "select", visible[d.cursor].ID)
		}
	case "ctrl+d":
		if d.cursor >= 0 && d.cursor < len(visible) {
			d.confirmDel = true
		}
	case "backspace":
		if len(d.filter) > 0 {
			d.filter = d.filter[:len(d.filter)-1]
			d.cursor = 0
			d.scroll = 0
		}
		d.confirmDel = false
	default:
		if len(km.Runes) == 1 {
			d.filter += string(km.Runes[0])
			d.cursor = 0
			d.scroll = 0
			d.confirmDel = false
		}
	}
	return d, nil
}

func (d *SessionsDialog) visible() []sessionstore.SessionMeta {
	if d.filter == "" {
		return d.all
	}
	titles := make([]string, len(d.all))
	for i, m := range d.all {
		titles[i] = m.Title
	}
	matches := fuzzy.Find(d.filter, titles)
	out := make([]sessionstore.SessionMeta, 0, len(matches))
	for _, m := range matches {
		out = append(out, d.all[m.Index])
	}
	return out
}

// Render implements Dialog.
func (d *SessionsDialog) Render(screenW, screenH int) string {
	d.screenH = screenH

	visible := d.visible()
	mv := d.maxVisible()
	d.clampScroll(len(visible))

	// Slice to the scroll window.
	start := d.scroll
	end := start + mv
	if end > len(visible) {
		end = len(visible)
	}
	window := visible[start:end]

	items := make([]listDialogItem, 0, len(window))
	for _, m := range window {
		desc := relativeTime(m.UpdatedAt)
		if m.MsgCount > 0 {
			desc += fmt.Sprintf("  ·  %d msgs", m.MsgCount)
		}
		if m.Model != "" {
			desc += "  ·  " + m.Model
		}
		items = append(items, listDialogItem{
			Title:       m.Title,
			Description: desc,
		})
	}

	hint := "↑↓ navigate · Enter/→ open · Ctrl+D delete · Esc/← back"
	if d.confirmDel {
		hint = "Press Enter again to confirm delete · Esc cancel"
	}

	title := "Sessions"
	if len(d.all) == 0 {
		title = "Sessions — none yet"
	} else if len(visible) > mv {
		title = fmt.Sprintf("Sessions  %d/%d", d.cursor+1, len(visible))
	}

	return renderListDialog(
		title,
		items,
		d.cursor-d.scroll,
		d.filter,
		"Search sessions",
		hint,
		screenW, screenH,
	)
}

// relativeTime renders a coarse human-readable delta like "5m ago" / "2h ago".
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Local().Format("2006-01-02")
	}
}
