package view

import (
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestCollapseOldTurnsForView(t *testing.T) {
	var msgs []state.Message
	for i := 0; i < 30; i++ {
		msgs = append(msgs, state.Message{
			Role:      state.RoleUser,
			Text:      "u",
			StartedAt: time.Now(),
		})
	}
	out := CollapseOldTurnsForView(msgs)
	if len(out) != collapseOlderThan+1 {
		t.Fatalf("len=%d want %d", len(out), collapseOlderThan+1)
	}
	if out[0].Role != state.RoleSystem || !strings.Contains(out[0].Text, "collapsed") {
		t.Fatalf("expected collapse notice, got %+v", out[0])
	}
	short := CollapseOldTurnsForView(msgs[:10])
	if len(short) != 10 {
		t.Fatalf("short list should not collapse, got %d", len(short))
	}
}
