package agent

import (
	"strings"
	"testing"
)

func TestAgent_ModeReminder_Orchestra(t *testing.T) {
	ag := &Agent{opts: Options{Mode: ModeOrchestra}}
	got := ag.modeReminder()
	if !strings.Contains(got, "complex|focused|micro") {
		t.Fatalf("orchestra reminder must list tiers, got: %q", got)
	}
	if !strings.Contains(got, "Do not edit production code") {
		t.Fatalf("orchestra reminder must forbid production edits, got: %q", got)
	}
}
