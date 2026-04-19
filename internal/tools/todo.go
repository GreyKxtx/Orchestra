package tools

// TodoStatus represents the lifecycle state of a todo item.
type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoDone       TodoStatus = "done"
	TodoCancelled  TodoStatus = "cancelled"
)

// TodoItem is a single task in the model's working checklist.
type TodoItem struct {
	ID      string     `json:"id"`
	Content string     `json:"content"`
	Status  TodoStatus `json:"status"`
}

// TodoWriteRequest is the input for todo.write.
type TodoWriteRequest struct {
	Todos []TodoItem `json:"todos"`
}

// TodoWriteResponse is returned by todo.write.
type TodoWriteResponse struct {
	Count int `json:"count"`
}

// TodoReadResponse is returned by todo.read.
type TodoReadResponse struct {
	Todos []TodoItem `json:"todos"`
}
