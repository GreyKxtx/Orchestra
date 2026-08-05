package sessionfile

import "time"

// UIToolBlock is the persisted form of a tool call shown in the TUI chat.
type UIToolBlock struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	ArgsPreview string `json:"args_preview,omitempty"`
	ArgsRaw     string `json:"args_raw,omitempty"`
	Status      string `json:"status"`
	Result      string `json:"result,omitempty"`
	DurationMS  int64  `json:"duration_ms,omitempty"`
}

// UIMessage is the persisted projection of one chat viewport message.
type UIMessage struct {
	Role       string        `json:"role"`
	Text       string        `json:"text,omitempty"`
	ToolBlocks []UIToolBlock `json:"tool_blocks,omitempty"`
	Reasoning  string        `json:"reasoning,omitempty"`
	StartedAt  time.Time     `json:"started_at,omitempty"`
	DurationMS int64         `json:"duration_ms,omitempty"`
	TokensIn   int           `json:"tokens_in,omitempty"`
	TokensOut  int           `json:"tokens_out,omitempty"`
	Mode       string        `json:"mode,omitempty"`
	Model      string        `json:"model,omitempty"`
}
