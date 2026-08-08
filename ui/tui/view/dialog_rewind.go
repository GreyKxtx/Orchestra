package view

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// RewindCheckpoint is one user turn the UI can rewind to (skeleton).
type RewindCheckpoint struct {
	MsgIndex int
	Label    string
	At       time.Time
}

// RewindDialog lists user-message checkpoints for future history rewind.
type RewindDialog struct {
	items   []RewindCheckpoint
	cursor  int
	scroll  int
	screenH int
}

func NewRewindDialog(items []RewindCheckpoint) *RewindDialog {
	return &RewindDialog{items: items, screenH: 24}
}

func (d *RewindDialog) maxVisible() int {
	n := d.screenH - 8
	if n < 4 {
		return 4
	}
	return n
}

func (d *RewindDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			return d, dialogResultCmd("rewind", "cancel", nil)
		case "up", "shift+up":
			if d.cursor > 0 {
				d.cursor--
				d.clampScroll()
			}
		case "down", "shift+down":
			if d.cursor < len(d.items)-1 {
				d.cursor++
				d.clampScroll()
			}
		case "enter":
			if len(d.items) == 0 {
				return d, dialogResultCmd("rewind", "cancel", nil)
			}
			cp := d.items[d.cursor]
			return d, dialogResultCmd("rewind", "select", cp)
		}
	}
	return d, nil
}

func (d *RewindDialog) clampScroll() {
	max := d.maxVisible()
	if d.cursor < d.scroll {
		d.scroll = d.cursor
	}
	if d.cursor >= d.scroll+max {
		d.scroll = d.cursor - max + 1
	}
}

func (d *RewindDialog) Render(screenW, screenH int) string {
	d.screenH = screenH
	mv := d.maxVisible()
	d.clampScroll()

	start := d.scroll
	end := start + mv
	if end > len(d.items) {
		end = len(d.items)
	}
	window := d.items[start:end]

	items := make([]listDialogItem, 0, len(window))
	for i, cp := range window {
		desc := ""
		if !cp.At.IsZero() {
			desc = cp.At.Format("15:04:05")
		}
		items = append(items, listDialogItem{
			Title:       fmt.Sprintf("#%d  %s", start+i+1, cp.Label),
			Description: desc,
		})
	}

	title := "Rewind checkpoint"
		hint := "Enter — rewind · Esc — отмена"
	if len(d.items) == 0 {
		title = "Rewind — нет сообщений"
		hint = "Esc — закрыть"
	} else if len(d.items) > mv {
		title = fmt.Sprintf("Rewind  %d/%d", d.cursor+1, len(d.items))
	}

	return renderListDialog(
		title,
		items,
		d.cursor-d.scroll,
		"",
		"",
		hint,
		screenW, screenH,
	)
}
