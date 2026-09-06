package sessionfile

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/llm"
)

// ForkSnapshot returns a new snapshot holding everything strictly before
// uiIndex, leaving snap untouched. This is the non-destructive counterpart to
// session.rewind: the original session keeps every message it had.
//
// uiIndex must point at a user message — the same checkpoint rule rewind
// enforces — and the message it points at is NOT carried into the branch. The
// branch therefore ends with the assistant's answer to the previous turn, so
// the next thing written into it is a fresh prompt rather than a second user
// message in a row.
func ForkSnapshot(snap *Snapshot, uiIndex int, newID string) (*Snapshot, error) {
	if snap == nil {
		return nil, errors.New("fork: snapshot is nil")
	}
	if newID == "" {
		return nil, errors.New("fork: a new session id is required")
	}
	if uiIndex < 0 || uiIndex >= len(snap.UIMessages) {
		return nil, fmt.Errorf("fork: ui_message_index %d is out of range (session has %d messages)",
			uiIndex, len(snap.UIMessages))
	}
	if role := snap.UIMessages[uiIndex].Role; role != "user" {
		return nil, fmt.Errorf("fork: ui_message_index %d points at a %q message; a fork point must be a user message",
			uiIndex, role)
	}
	if uiIndex == 0 {
		return nil, errors.New("fork: cannot fork at the first message — the branch would be empty")
	}

	prefix := append([]UIMessage(nil), snap.UIMessages[:uiIndex]...)

	// uiIndex is a user message, so it opens the (k+1)-th turn where k is the
	// number of user messages before it. The branch keeps every completed turn
	// before that one, which is exactly History[:TurnStarts[k]].
	//
	// This cannot be derived by counting role=user entries in History: the
	// agent never appends the user's prompt there (agent_step.go builds a
	// fresh system+user+history slice per request) and it does inject
	// synthetic role=user messages mid-run. Only a recorded boundary maps the
	// two arrays onto each other.
	k := CountUserMessages(prefix)
	cut, err := TurnStartAt(snap.TurnStarts, k, len(snap.History))
	if err != nil {
		return nil, fmt.Errorf("fork: cannot branch at message %d: %w", uiIndex, err)
	}

	out := *snap
	out.ID = newID
	out.UIMessages = prefix
	out.History = append([]llm.Message(nil), snap.History[:cut]...)
	out.TurnStarts = append([]int(nil), snap.TurnStarts[:k]...)
	out.MsgCount = len(prefix)
	out.ParentID = snap.ID
	out.ForkedFromIndex = uiIndex
	out.Title = forkTitle(snap.Title)
	// Save only stamps CreatedAt when it is zero, so inheriting the parent's
	// would make every branch report its parent's creation time.
	out.CreatedAt = time.Time{}

	// Everything below describes the path the branch is abandoning: pending ops
	// and todos are what rewind clears too, spend belongs to the parent's
	// session (counting it twice would inflate the project total), and apply
	// output refers to work the branch does not contain.
	out.PendingOps = nil
	out.Todos = nil
	out.CostUSD = 0
	out.ApplyOutput = ""

	return &out, nil
}

// forkTitle marks a branch so the session picker does not show two identical
// rows: titles are derived from the first user message, which a branch shares
// with its parent verbatim. Forking a fork does not stack the suffix — one
// marker already says "this is not the original".
func forkTitle(parent string) string {
	if parent == "" {
		return forkSuffix
	}
	if strings.HasSuffix(parent, " "+forkSuffix) || parent == forkSuffix {
		return parent
	}
	return parent + " " + forkSuffix
}

const forkSuffix = "(fork)"
