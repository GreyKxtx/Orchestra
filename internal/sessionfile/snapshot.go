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
	// ParentID and ForkedFromIndex record where a forked session branched from.
	// Additive with omitempty on purpose: LoadFromDisk rejects any snapshot
	// whose Version differs from the binary's own
	// (internal/core/session/persist.go:101-103), so bumping the schema would
	// make files written here unreadable by an older binary, while a field an
	// older binary does not know is simply ignored by json.Unmarshal.
	ParentID        string `json:"parent_id,omitempty"`
	ForkedFromIndex int    `json:"forked_from_index,omitempty"`
	// TurnStarts[k] is the index into History at which the (k+1)-th user
	// turn's agent output begins. It exists because History and UIMessages
	// cannot be mapped onto each other any other way: the agent builds a
	// fresh `system + user + history` slice per request
	// (internal/agent/agent_step.go:52-68), so the user's prompt is never
	// appended to History, while the agent does inject synthetic role=user
	// messages mid-run (LSP hints, retries, image carriers). Counting user
	// messages in History therefore maps onto nothing the UI can see.
	//
	// For a session recorded from its first turn, TurnStarts[0] == 0. The
	// field is absent on sessions written before it existed, and fork refuses
	// rather than guess wherever it has no entry.
	//
	// The array is POSITIONAL and every writer must keep it that way: exactly
	// one slot per user turn, because that is how fork and rewind reach into
	// it (they count user messages in UIMessages). A turn whose History was
	// rewritten underneath it — /compact, or a turn interrupted after the
	// agent compacted mid-run — keeps its slot and carries TurnStartUnknown
	// rather than being removed; removing it would shift every later turn and
	// silently break the mapping for the rest of the session.
	//
	// Additive with omitempty for the same reason as ParentID above — the
	// schema stays at version 4. Sessions written by earlier builds carry
	// shorter, sentinel-free arrays and keep working: they refuse for the
	// turns they have no entry for.
	TurnStarts []int `json:"turn_starts,omitempty"`
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
