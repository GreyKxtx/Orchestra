package core

import (
	"strings"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/internal/sessionfile"
)

type SessionRewindParams struct {
	SessionID      string `json:"session_id"`
	UIMessageIndex int    `json:"ui_message_index"` // inclusive; must point at role=user
}

type SessionRewindResult struct {
	SessionID       string `json:"session_id"`
	UIMessages      int    `json:"ui_messages"`
	HistoryMessages int    `json:"history_messages"`
}

// SessionRewind truncates UI projection and LLM history to a user-message checkpoint.
func (c *Core) SessionRewind(params SessionRewindParams) (*SessionRewindResult, error) {
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

	ui := sess.UIMessages()
	idx := params.UIMessageIndex
	if idx < 0 || idx >= len(ui) {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "ui_message_index out of range", map[string]any{
			"session_id": sid,
			"index":      idx,
			"ui_len":     len(ui),
		})
	}
	if ui[idx].Role != "user" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "ui_message_index must point at a user message", map[string]any{
			"session_id": sid,
			"index":      idx,
			"role":       ui[idx].Role,
		})
	}

	truncUI := append([]sessionfile.UIMessage(nil), ui[:idx+1]...)
	truncHist := truncateHistoryForUIPrefix(sess.CopyHistory(), truncUI)

	sess.SetUIMessages(truncUI)
	sess.ReplaceHistory(truncHist)
	sess.SetPending(nil)
	sess.SetTodos(nil)

	if snapErr := sess.Snapshot(c.workspaceRoot); snapErr != nil {
		return nil, protocol.NewError(protocol.ExecFailed, snapErr.Error(), map[string]any{"session_id": sid})
	}

	return &SessionRewindResult{
		SessionID:       sid,
		UIMessages:      len(truncUI),
		HistoryMessages: len(truncHist),
	}, nil
}

// truncateHistoryForUIPrefix keeps LLM history through the last user message that
// corresponds to the user-message count in the UI prefix.
func truncateHistoryForUIPrefix(hist []llm.Message, ui []sessionfile.UIMessage) []llm.Message {
	userTarget := sessionfile.CountUserMessages(ui)
	if userTarget == 0 {
		return nil
	}
	if i := sessionfile.IndexOfNthUserMessage(hist, userTarget); i >= 0 {
		out := make([]llm.Message, i+1)
		copy(out, hist[:i+1])
		return out
	}
	// Compaction or partial sync — keep full history rather than truncate too far.
	out := make([]llm.Message, len(hist))
	copy(out, hist)
	return out
}
