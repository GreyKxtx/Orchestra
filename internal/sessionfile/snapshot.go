package sessionfile

import (
	"time"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/ops"
)

// TodoItem mirrors tools.TodoItem for disk schema without importing tools/.
type TodoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

// Snapshot is the canonical session file shape stored at
// .orchestra/sessions/<id>.json (v3: chronological ui_messages.segments).
type Snapshot struct {
	Version     int           `json:"version"`
	ID          string        `json:"id"`
	Title       string        `json:"title,omitempty"`
	Model       string        `json:"model,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	History     []llm.Message `json:"history"`
	PendingOps  []ops.AnyOp   `json:"pending_ops,omitempty"`
	Todos       []TodoItem    `json:"todos,omitempty"`
	PlanPath    string        `json:"plan_path,omitempty"`
	UIMessages  []UIMessage   `json:"ui_messages"`
	Profile     string        `json:"profile,omitempty"`
	ApplyOutput string        `json:"apply_output,omitempty"`
	CostUSD     float64       `json:"cost_usd,omitempty"` // session spend (paid providers)
	// MsgCount supports legacy list UIs; equals len(UIMessages) when unset.
	MsgCount int `json:"msg_count,omitempty"`
}

// Meta is list-picker metadata without loading full history/ui_messages.
type Meta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Model     string    `json:"model,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	MsgCount  int       `json:"msg_count"`
}
