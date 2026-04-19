package tools

import "testing"

func TestTodoTypes(t *testing.T) {
	items := []TodoItem{
		{ID: "1", Content: "read files", Status: TodoPending},
		{ID: "2", Content: "write output", Status: TodoInProgress},
		{ID: "3", Content: "verify result", Status: TodoDone},
	}

	req := TodoWriteRequest{Todos: items}
	if len(req.Todos) != 3 {
		t.Fatalf("expected 3 todos, got %d", len(req.Todos))
	}

	resp := TodoWriteResponse{Count: len(items)}
	if resp.Count != 3 {
		t.Fatalf("expected count=3, got %d", resp.Count)
	}

	readResp := TodoReadResponse{Todos: items}
	if len(readResp.Todos) != 3 {
		t.Fatalf("expected 3 todos in read response, got %d", len(readResp.Todos))
	}
}
