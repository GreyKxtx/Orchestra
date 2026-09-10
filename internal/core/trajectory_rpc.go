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
// A missing sidecar is not one answer but two. A session this core knows — in
// memory or on disk — whose history and UI messages are both empty has had no
// turn, and the first turn is what creates the sidecar: nothing has happened, and
// nothing was missed, so that is Recorded true with no events. A session with
// history and no sidecar was recorded by a core that predates the log, and a
// session id that names nothing is treated the same way: Recorded false. Only a
// blank id is rejected, because that is a malformed request rather than a
// question about a session.
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
	if !recorded {
		// No sidecar yet. If the session exists and is empty, the log is
		// simply not born: the first agent launch creates it. Saying
		// "predates the log" here would be untrue of every new chat.
		if sess, lookErr := c.sessions.GetOrLoad(c.workspaceRoot, id); lookErr == nil && sess != nil {
			sess.Lock()
			// A session with an in-flight turn and no sidecar is mid-launch of
			// its first turn — the sidecar is created inside
			// prepareAgentLaunch, after SessionMessage has already appended
			// the user's UI message and marked the session busy. That window
			// must read the same as "nothing recorded yet", not "predates the
			// log": nothing was missed, the log just has not been born yet.
			empty := (len(sess.History) == 0 && len(sess.UIMessages()) == 0) || sess.IsBusy()
			sess.Unlock()
			if empty {
				recorded = true
			}
		}
	}
	// Never nil: `events` marshals to `null` when nil, and a client that reads
	// `events.length` would fault on it. An empty log is `[]`.
	if events == nil {
		events = []trajectory.Event{}
	}
	return &SessionTrajectoryResult{Recorded: recorded, Events: events}, nil
}
