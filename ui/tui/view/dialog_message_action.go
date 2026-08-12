package view

import (
	tea "github.com/charmbracelet/bubbletea"
)

// MessageActionDialog opens when the user clicks a message in the chat viewport.
// User messages offer Copy + Edit; assistant messages offer only Copy.
type MessageActionDialog struct {
	text   string
	isUser bool
	cursor int
}

// NewMessageActionDialog creates a dialog for the given message text.
// isUser=true shows "Редактировать" alongside "Копировать".
func NewMessageActionDialog(text string, isUser bool) *MessageActionDialog {
	return &MessageActionDialog{text: text, isUser: isUser}
}

type msgAction struct {
	title  string
	action MessageActionKind
}

func (d *MessageActionDialog) actions() []msgAction {
	if d.isUser {
		return []msgAction{
			{"Копировать", MessageActionCopy},
			{"Редактировать", MessageActionEdit},
		}
	}
	return []msgAction{
		{"Копировать", MessageActionCopy},
	}
}

// Update implements Dialog.
func (d *MessageActionDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	acts := d.actions()
	switch km.String() {
	case "up", "ctrl+p":
		if d.cursor > 0 {
			d.cursor--
		}
	case "down", "ctrl+n":
		if d.cursor < len(acts)-1 {
			d.cursor++
		}
	case "esc":
		return d, resultCmd(MessageActionDialogMsg{Action: MessageActionCancel})
	case "enter":
		if d.cursor < len(acts) {
			return d, resultCmd(MessageActionDialogMsg{Action: acts[d.cursor].action, Text: d.text})
		}
	}
	return d, nil
}

// Render implements Dialog.
func (d *MessageActionDialog) Render(screenW, screenH int) string {
	acts := d.actions()
	items := make([]listDialogItem, len(acts))
	for i, a := range acts {
		items[i] = listDialogItem{Title: a.title}
	}
	title := "Сообщение"
	if d.isUser {
		title = "Ваше сообщение"
	}
	return renderListDialog(
		title,
		items,
		d.cursor,
		"", "",
		"↑↓ навигация · Enter выбрать · Esc закрыть",
		screenW, screenH,
	)
}
