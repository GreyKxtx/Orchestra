package wire

// The session methods: session.start … session.close.

type SessionForkParams struct {
	SessionID      string `json:"session_id"`
	UIMessageIndex int    `json:"ui_message_index"` // exclusive; must point at role=user
}

type SessionForkResult struct {
	SessionID       string `json:"session_id"` // the new branch
	ParentID        string `json:"parent_id"`
	UIMessages      int    `json:"ui_messages"`
	HistoryMessages int    `json:"history_messages"`
}

type SessionRewindParams struct {
	SessionID      string `json:"session_id"`
	UIMessageIndex int    `json:"ui_message_index"` // inclusive; must point at role=user
}

type SessionRewindResult struct {
	SessionID       string `json:"session_id"`
	UIMessages      int    `json:"ui_messages"`
	HistoryMessages int    `json:"history_messages"`
}

type SessionStartParams struct {
	// SessionID optionally reopens an existing on-disk session (v2 snapshot).
	// When empty, core allocates a new sortable id.
	SessionID string `json:"session_id,omitempty"`
}

type SessionStartResult struct {
	SessionID string `json:"session_id"`
	Restored  bool   `json:"restored,omitempty"`
	// ResumableTurnID names the session's most recent turn its core did not
	// finish — a crash, a kill — which session.message{resume} continues.
	// Empty when there is none. (ProtocolVersion 25.)
	ResumableTurnID string `json:"resumable_turn_id,omitempty"`
}

type SessionGetParams struct {
	SessionID string `json:"session_id"`
}

type SessionListParams struct{}

type SessionUISyncResult struct {
	SessionID string `json:"session_id"`
	Saved     bool   `json:"saved"`
}

type SessionApplyPendingParams struct {
	SessionID string   `json:"session_id"`
	Backup    bool     `json:"backup,omitempty"`
	Paths     []string `json:"paths,omitempty"` // optional: apply only ops whose path matches one of these (workspace-relative)
}

type SessionDiscardPendingParams struct {
	SessionID string   `json:"session_id"`
	Paths     []string `json:"paths,omitempty"` // optional: discard only ops whose path matches one of these (workspace-relative)
}

type SessionHistoryParams struct {
	SessionID string `json:"session_id"`
}

type SessionCompactParams struct {
	SessionID string `json:"session_id"`
	Query     string `json:"query,omitempty"` // optional goal hint for the summary
}

type SessionCompactResult struct {
	SessionID   string `json:"session_id"`
	BeforeMsgs  int    `json:"before_msgs"`
	AfterMsgs   int    `json:"after_msgs"`
	BeforeBytes int    `json:"before_bytes,omitempty"`
	AfterBytes  int    `json:"after_bytes,omitempty"`
}

type SessionCancelParams struct {
	SessionID string `json:"session_id"`
}

type SessionCloseParams struct {
	SessionID string `json:"session_id"`
}

type SessionSearchParams struct {
	Query       string `json:"query"`
	Insensitive bool   `json:"insensitive,omitempty"`
	IncludeAll  bool   `json:"include_all,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// SessionTrajectoryParams selects the session whose log to read.
type SessionTrajectoryParams struct {
	SessionID string `json:"session_id"`
}
