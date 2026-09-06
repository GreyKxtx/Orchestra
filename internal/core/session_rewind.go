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
	turnStarts := sess.TurnStarts()
	truncHist := truncateHistoryForUIPrefix(sess.CopyHistory(), truncUI, turnStarts)

	sess.SetUIMessages(truncUI)
	sess.ReplaceHistory(truncHist)
	sess.SetTurnStarts(truncateTurnStartsForUIPrefix(turnStarts, truncUI))
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

// truncateHistoryForUIPrefix cuts LLM history to match a UI prefix ending at a
// user message.
//
// With recorded turn boundaries it makes exactly the cut fork makes: history
// keeps every completed turn before the one the retained prompt opens. Rewind
// and fork differ only in the UI prefix — rewind keeps the prompt so the user
// can edit and resend it, fork drops it — and the prompt itself never lives in
// history anyway (agent_step.go passes it separately), so the same history cut
// serves both.
//
// Without boundaries — a session written before they were recorded, or one
// whose history /compact rewrote — it falls back to the exact behaviour it had
// before: count user messages, and keep the FULL history when that count does
// not resolve, rather than truncate too far. That fallback is conservative for
// rewind, which is destructive; fork refuses instead, because for a fork the
// same fallback would produce a branch still containing everything it was
// meant to branch away from.
func truncateHistoryForUIPrefix(hist []llm.Message, ui []sessionfile.UIMessage, turnStarts []int) []llm.Message {
	userTarget := sessionfile.CountUserMessages(ui)
	if userTarget == 0 {
		return nil
	}
	if cut, err := sessionfile.TurnStartAt(turnStarts, userTarget-1, len(hist)); err == nil {
		out := make([]llm.Message, cut)
		copy(out, hist[:cut])
		return out
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

// truncateTurnStartsForUIPrefix drops boundaries for turns the rewind threw
// away. The retained prefix ends with the user prompt opening turn N, whose
// own boundary is kept and now equals the new history length: that turn is
// present but has produced nothing yet, which is exactly true after a rewind.
func truncateTurnStartsForUIPrefix(turnStarts []int, ui []sessionfile.UIMessage) []int {
	userTarget := sessionfile.CountUserMessages(ui)
	if userTarget == 0 || len(turnStarts) == 0 {
		return nil
	}
	if len(turnStarts) <= userTarget {
		return turnStarts
	}
	return turnStarts[:userTarget]
}
