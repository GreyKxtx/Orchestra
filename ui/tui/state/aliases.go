// Package state holds TUI-local session behavior. Chat message types live in
// internal/uimodel; this package re-exports them and adds streaming/session helpers.
package state

import "github.com/orchestra/orchestra/internal/uimodel"

// Re-exported chat DTOs (shared with sessionstore / sessionfile via uimodel).
type (
	Role            = uimodel.Role
	SystemKind      = uimodel.SystemKind
	SystemNotice    = uimodel.SystemNotice
	Message         = uimodel.Message
	Attachment      = uimodel.Attachment
	DiffFile        = uimodel.DiffFile
	SegmentKind     = uimodel.SegmentKind
	Segment         = uimodel.Segment
	TodoItem        = uimodel.TodoItem
	ToolBlockStatus = uimodel.ToolBlockStatus
	ToolBlock       = uimodel.ToolBlock
	ToolDiagnostic  = uimodel.ToolDiagnostic
)

const (
	RoleUser      = uimodel.RoleUser
	RoleAssistant = uimodel.RoleAssistant
	RoleSystem    = uimodel.RoleSystem
	RoleDiff      = uimodel.RoleDiff

	SystemKindInfo    = uimodel.SystemKindInfo
	SystemKindRetry   = uimodel.SystemKindRetry
	SystemKindError   = uimodel.SystemKindError
	SystemKindSuccess = uimodel.SystemKindSuccess

	SegmentReasoning = uimodel.SegmentReasoning
	SegmentText      = uimodel.SegmentText
	SegmentTools     = uimodel.SegmentTools
	SegmentNotice    = uimodel.SegmentNotice
	SegmentTodos     = uimodel.SegmentTodos

	ToolBlockRunning   = uimodel.ToolBlockRunning
	ToolBlockCompleted = uimodel.ToolBlockCompleted
	ToolBlockFailed    = uimodel.ToolBlockFailed
	ToolBlockSkipped   = uimodel.ToolBlockSkipped

	DiffReviewAccepted = uimodel.DiffReviewAccepted
	DiffReviewRejected = uimodel.DiffReviewRejected
)
