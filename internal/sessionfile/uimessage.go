package sessionfile

import "time"

// UIToolDiagnostic is one LSP diagnostic attached to a write/edit tool result.
type UIToolDiagnostic struct {
	StartLine int    `json:"start_line"`
	StartCol  int    `json:"start_col"`
	EndLine   int    `json:"end_line,omitempty"`
	EndCol    int    `json:"end_col,omitempty"`
	Severity  string `json:"severity"`
	Source    string `json:"source,omitempty"`
	Message   string `json:"message"`
}

// UIToolBlock is the persisted form of a tool call shown in the TUI chat.
type UIToolBlock struct {
	ID          string             `json:"id,omitempty"`
	Name        string             `json:"name"`
	ArgsPreview string             `json:"args_preview,omitempty"`
	ArgsRaw     string             `json:"args_raw,omitempty"`
	Status      string             `json:"status"`
	Result      string             `json:"result,omitempty"`
	Diagnostics []UIToolDiagnostic `json:"diagnostics,omitempty"`
	Expanded    bool               `json:"expanded,omitempty"`
	DurationMS  int64              `json:"duration_ms,omitempty"`
}

// UIDiffFile is one file change in a persisted diff message.
type UIDiffFile struct {
	Path   string `json:"path"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// UINotice is a persisted inline assistant notice.
type UINotice struct {
	Kind string `json:"kind,omitempty"`
	Text string `json:"text"`
}

// UISegment is one chronological part of a persisted assistant message.
type UISegment struct {
	Kind       string        `json:"kind"` // reasoning | text | tools | notice
	Text       string        `json:"text,omitempty"`
	Tools      []UIToolBlock `json:"tools,omitempty"`
	NoticeKind string        `json:"notice_kind,omitempty"` // info|retry|error|success
}

// UIMessage is the persisted projection of one chat viewport message.
type UIMessage struct {
	Role              string        `json:"role"`
	Text              string        `json:"text,omitempty"`
	ToolBlocks        []UIToolBlock `json:"tool_blocks,omitempty"`
	Segments          []UISegment   `json:"segments,omitempty"`
	Reasoning         string        `json:"reasoning,omitempty"`
	Notices           []UINotice    `json:"notices,omitempty"`
	SystemKind        string        `json:"system_kind,omitempty"`
	DiffFiles         []UIDiffFile  `json:"diff_files,omitempty"`
	DiffExpanded      bool          `json:"diff_expanded,omitempty"`
	ToolsExpanded     bool          `json:"tools_expanded,omitempty"`
	ReasoningExpanded bool          `json:"reasoning_expanded,omitempty"`
	StartedAt         time.Time     `json:"started_at,omitempty"`
	DurationMS        int64         `json:"duration_ms,omitempty"`
	TokensIn          int           `json:"tokens_in,omitempty"`
	TokensOut         int           `json:"tokens_out,omitempty"`
	PromptCtx         int           `json:"prompt_ctx,omitempty"` // last step prompt size for ctx bar
	Mode              string        `json:"mode,omitempty"`
	Model             string        `json:"model,omitempty"`
}
