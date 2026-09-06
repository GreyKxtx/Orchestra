package core

import (
	"strings"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/protocol"
)

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

// SessionFork copies a session's history up to (but not including) a user
// checkpoint into a new session, leaving the original untouched. This is the
// non-destructive counterpart to SessionRewind.
//
// It reads the parent from memory rather than from disk: the Manager holds the
// authoritative copy and the snapshot on disk lags a live session by up to the
// mid-turn snapshot interval.
func (c *Core) SessionFork(params SessionForkParams) (*SessionForkResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	sid := strings.TrimSpace(params.SessionID)
	if sid == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "session_id is empty", nil)
	}
	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, sid)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": sid})
	}

	sess.Lock()
	defer sess.Unlock()

	if sess.IsBusy() {
		return nil, protocol.NewError(protocol.ExecFailed, "session is busy", map[string]any{"session_id": sid})
	}

	// Built from live accessors rather than sessionfile.Load: todos, pending
	// ops, spend and apply output are all cleared by ForkSnapshot anyway, so
	// only the fields the branch actually inherits are carried over.
	src := &sessionfile.Snapshot{
		Version:    sessionfile.Version,
		ID:         sid,
		Title:      sess.Title(),
		Model:      sess.Model(),
		Profile:    sess.Profile(),
		PlanPath:   sess.PlanPath(),
		UIMessages: sess.UIMessages(),
		History:    sess.CopyHistory(),
	}

	branch, err := sessionfile.ForkSnapshot(src, params.UIMessageIndex, sessionfile.NewID())
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, err.Error(), map[string]any{
			"session_id": sid,
			"index":      params.UIMessageIndex,
		})
	}
	if err := sessionfile.Save(c.workspaceRoot, branch); err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{
			"session_id": branch.ID,
		})
	}

	// Deliberately not registered with the Manager: the client's following
	// session.start loads it from disk through LoadOrCreate, so the branch
	// never has two owners.
	return &SessionForkResult{
		SessionID:       branch.ID,
		ParentID:        sid,
		UIMessages:      len(branch.UIMessages),
		HistoryMessages: len(branch.History),
	}, nil
}
