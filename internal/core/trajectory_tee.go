package core

import "github.com/orchestra/orchestra/internal/trajectory"

// teeToTrajectory returns a notify function that records every notification
// into the session's event log before forwarding it.
//
// This is the one place every agent notification passes through, which is why
// the log hooks here instead of inside emitAgentStreamEvent: one wrapper
// records agent/event, exec/output_chunk and anything added later, and it
// records exactly what the client is told.
//
// Recording is best-effort. A log that cannot be written must never stop a
// turn from streaming — the user's work matters more than our observability —
// so a write error is dropped rather than propagated. There is no error path
// to propagate it down: notify returns nothing.
func teeToTrajectory(notify func(method string, params any), w *trajectory.Writer) func(string, any) {
	if notify == nil {
		notify = func(string, any) {}
	}
	if w == nil {
		return notify
	}
	return func(method string, params any) {
		_ = w.Append(method, params)
		notify(method, params)
	}
}
