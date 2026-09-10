// Package trajectory records what an agent turn did, as an append-only log.
//
// One file per session, one JSON object per line, beside the session snapshot
// it belongs to. The snapshot is a single document rewritten whole on every
// save, so a log kept inside it would rewrite the entire history on every
// event; and a torn write there loses the session, where a torn line here
// loses one event.
package trajectory

import "encoding/json"

// Event is one recorded thing. The field names are the ones a derived
// projection will need later — see the spec's "One event shape, defined once".
type Event struct {
	// Seq is monotonic and contiguous from 1 within a session.
	Seq int64 `json:"seq"`
	// TimeMS is epoch milliseconds, stamped by the core when the event
	// happened. Never a client's clock: durations are measured where the work
	// runs, not where it is observed.
	TimeMS int64 `json:"time_ms"`
	// Type is the notification method that carried this event —
	// "agent/event", "exec/output_chunk", and so on.
	Type string `json:"type"`
	// Source names what produced the event. Always "core" today. It exists so
	// that a plugin layer can fill it in without a format migration; see the
	// spec's "Where this meets extensibility".
	Source string `json:"source"`
	// Data is the notification's params, stored as they were sent.
	Data json.RawMessage `json:"data,omitempty"`
}

// SourceCore is the only source this build emits.
const SourceCore = "core"
