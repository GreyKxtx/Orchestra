package tui

import (
	"encoding/json"
	"strings"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

func todosToView(items []rpcclient.TodoItem) []view.TodoView {
	if len(items) == 0 {
		return nil
	}
	out := make([]view.TodoView, len(items))
	for i, it := range items {
		out[i] = view.TodoView{
			ID:      it.ID,
			Content: it.Content,
			Status:  it.Status,
		}
	}
	return out
}

func (a *App) setTodos(items []rpcclient.TodoItem) {
	a.todos = append([]rpcclient.TodoItem(nil), items...)
	if a.taskPanel != nil {
		a.taskPanel.SetItems(todosToView(items))
	}
	// Sticky checklist above input is the live view; also keep SegmentTodos
	// in the transcript for scrollback history.
	if segs := todosToState(items); len(segs) > 0 {
		a.session.UpsertTodosChecklist(segs)
		a.chat.SetMessages(a.session.Messages)
		a.chatDirty = true
	}
	a.layout()
}

// stickyTaskRows is the height reserved above the input for the pinned checklist.
// lead + up to 5 open task rows + footer.
func (a *App) stickyTaskRows() int {
	open := 0
	any := false
	for _, it := range a.todos {
		st := strings.ToLower(strings.TrimSpace(it.Status))
		if st == "cancelled" {
			continue
		}
		any = true
		if st != "done" {
			open++
		}
	}
	if !any {
		return 0
	}
	if open > 5 {
		open = 5
	}
	if open == 0 {
		return 0 // all done — hide sticky strip
	}
	return 2 + open
}

func todosToState(items []rpcclient.TodoItem) []state.TodoItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]state.TodoItem, len(items))
	for i, it := range items {
		out[i] = state.TodoItem{ID: it.ID, Content: it.Content, Status: it.Status}
	}
	return out
}

func (a *App) pendingTodoCount() int {
	n := 0
	for _, it := range a.todos {
		switch strings.ToLower(strings.TrimSpace(it.Status)) {
		case "pending", "in_progress":
			n++
		}
	}
	return n
}

func isTodoWriteTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "todowrite", "todo.write", "todo_write":
		return true
	default:
		return false
	}
}

func parseTodosFromTool(name, argsRaw string) []rpcclient.TodoItem {
	if !isTodoWriteTool(name) {
		return nil
	}
	raw := strings.TrimSpace(argsRaw)
	if raw == "" {
		return nil
	}
	var req struct {
		Todos []rpcclient.TodoItem `json:"todos"`
	}
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		return nil
	}
	return req.Todos
}
