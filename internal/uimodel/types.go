// Package uimodel holds client-neutral chat UI data types shared by TUI, VS Code,
// and session persistence. Clients may wrap these types with view-specific behavior;
// core and sessionstore must not import ui/*.
package uimodel

import "time"

// Role identifies who produced a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleDiff      Role = "diff"
)

// SystemKind classifies standalone system messages and inline assistant notices.
type SystemKind string

const (
	SystemKindInfo    SystemKind = "info"
	SystemKindRetry   SystemKind = "retry"
	SystemKindError   SystemKind = "error"
	SystemKindSuccess SystemKind = "success"
)

// SystemNotice is a compact status line shown inside an assistant turn.
type SystemNotice struct {
	Kind SystemKind
	Text string
}

// Message is one entry in the chat scroll.
type Message struct {
	Role        Role
	Text        string
	Attachments []Attachment
	ToolBlocks  []ToolBlock
	Streaming   bool

	Segments []Segment

	Reasoning string

	StartedAt time.Time
	Duration  time.Duration

	TokensIn  int
	TokensOut int
	PromptCtx int

	Mode  string
	Model string

	DiffFiles    []DiffFile
	DiffExpanded bool

	SystemKind SystemKind

	Notices []SystemNotice

	ToolsExpanded     bool
	ReasoningExpanded bool
}

// Attachment is a chat file/image staged for a user turn.
type Attachment struct {
	Path string
	Name string
	Kind string // image | file
	MIME string
	Ext  string
}

// DiffFile is one file change shown inline in the chat diff panel.
type DiffFile struct {
	Path         string
	Before       string
	After        string
	ReviewStatus string
}

// Diff review status values persisted in session UI projection.
const (
	DiffReviewAccepted = "accepted"
	DiffReviewRejected = "rejected"
)
