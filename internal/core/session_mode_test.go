package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// LLM-10: agent.run refused a subagent-only or unknown mode; session.message
// — what the TUI and VS Code send — took any string: a worker or compaction
// ran as a whole turn, and a typo silently became build. Both refuse now,
// before the turn touches the session.
func TestSessionMessage_RefusesAModeATurnCannotRunIn(t *testing.T) {
	// An open gate: a turn the check lets through finishes at once, and
	// fails the test by succeeding rather than by hanging.
	open := make(chan struct{})
	close(open)
	_, h := setupInitializedCore(t, t.TempDir(), &gateLLM{release: open})
	startP, _ := json.Marshal(SessionStartParams{})
	res, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatal(err)
	}
	id := res.(*SessionStartResult).SessionID
	for mode, want := range map[string]string{
		"worker":     "runs only as a subagent",
		"compaction": "runs only as a subagent",
		"biuld":      "unknown agent mode",
	} {
		p, _ := json.Marshal(SessionMessageParams{SessionID: id, Content: "hello", Mode: mode})
		_, err := h.Handle(context.Background(), "session.message", p)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("session.message mode %q: %v, want %q", mode, err, want)
		}
	}
}
