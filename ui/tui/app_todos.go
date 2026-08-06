package tui

import (
	"encoding/json"
	"strings"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
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
	views := todosToView(items)
	a.taskPanel.SetItems(views)
	if len(items) == 0 {
		a.taskPanelOpen = false
		a.taskPanel.SetOpen(false)
	} else {
		a.taskPanel.SetOpen(a.taskPanelOpen)
	}
	a.layout()
}

func (a *App) toggleTaskPanel() {
	if len(a.todos) == 0 {
		return
	}
	a.taskPanelOpen = !a.taskPanelOpen
	a.taskPanel.SetOpen(a.taskPanelOpen)
	a.layout()
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
