package core

import (
	"strings"

	"github.com/orchestra/orchestra/internal/trajectory"
	"github.com/orchestra/orchestra/protocol"
)

// SessionTrajectoryParams selects the session whose log to read.
type SessionTrajectoryParams struct {
	SessionID string `json:"session_id"`
}

// SessionTrajectoryResult carries a session's recorded events.
//
// Recorded distinguishes "this session has no log" from "this session's log is
// empty". They look identical in the events array and mean different things to
// a reader: the first is a session that predates the feature, the second a
// session where nothing has happened yet. The view says which, in words.
type SessionTrajectoryResult struct {
	Recorded bool               `json:"recorded"`
	Events   []trajectory.Event `json:"events"`
}

// SessionTrajectory returns the append-only event log for a session.
//
// A session id that names nothing yields Recorded false rather than an error:
// the log is a sidecar, so "no log here" is the same answer for a session that
// predates the feature and for one that never existed, and this method has no
// business deciding which. Only a blank id is rejected, because that is a
// malformed request rather than a question about a session.
func (c *Core) SessionTrajectory(p SessionTrajectoryParams) (*SessionTrajectoryResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	id := strings.TrimSpace(p.SessionID)
	if id == "" {
		return nil, protocol.NewError(protocol.InvalidParams, "session_id is empty", nil)
	}
	events, recorded, err := trajectory.Read(c.workspaceRoot, id)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": id})
	}
	// Never nil: `events` marshals to `null` when nil, and a client that reads
	// `events.length` would fault on it. An empty log is `[]`.
	if events == nil {
		events = []trajectory.Event{}
	}
	return &SessionTrajectoryResult{Recorded: recorded, Events: events}, nil
}
