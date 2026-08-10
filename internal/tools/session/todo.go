package session

import (
	"fmt"
	"strings"
)

type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoDone       TodoStatus = "done"
	TodoCancelled  TodoStatus = "cancelled"
)

type TodoItem struct {
	ID      string     `json:"id"`
	Content string     `json:"content"`
	Status  TodoStatus `json:"status"`
}

type TodoWriteRequest struct {
	Todos []TodoItem `json:"todos"`
}

type TodoWriteResponse struct {
	Count int `json:"count"`
}

type TodoReadResponse struct {
	Todos []TodoItem `json:"todos"`
}

func ValidateTodos(items []TodoItem) ([]TodoItem, error) {
	normalized := make([]TodoItem, len(items))
	seen := make(map[string]bool, len(items))
	inProgress := 0
	for i, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			return nil, fmt.Errorf("todo item %d: id is required", i)
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate todo id %q", id)
		}
		seen[id] = true
		st := item.Status
		if st == "completed" {
			st = TodoDone
		}
		switch st {
		case TodoPending, TodoInProgress, TodoDone, TodoCancelled:
		default:
			return nil, fmt.Errorf("todo %q: invalid status %q", id, item.Status)
		}
		if st == TodoInProgress {
			inProgress++
		}
		normalized[i] = TodoItem{ID: id, Content: strings.TrimSpace(item.Content), Status: st}
	}
	if inProgress > 1 {
		return nil, fmt.Errorf("at most one todo may be in_progress")
	}
	return normalized, nil
}
