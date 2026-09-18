package tui

import "testing"

// The TUI hardcoded Apply: true on every turn, so a turn's edits were written
// and then shown: the user reviewed and reverted rather than approving. The
// pre-approval path was already built — core withholds the ops when Apply is
// false, and applyPendingOpsEvent arms them for review — but nothing could
// reach it, because agentRunOptions never asked for it.
//
// ui.auto_apply was the setting a user would reach for, it was documented as
// read from .orchestra.yml, and it had no reader anywhere in Go, TypeScript or
// JavaScript. These two are the same defect from opposite ends.

func TestAgentRunOptions_AppliesByDefault(t *testing.T) {
	a := &App{cfg: Config{}}
	if !a.agentRunOptions().Apply {
		t.Fatal("a config that says nothing must keep committing the turn's edits; " +
			"flipping the default would switch every existing user into review mode")
	}
}

func TestAgentRunOptions_HoldsEditsWhenReviewIsAskedFor(t *testing.T) {
	a := &App{cfg: Config{ReviewBeforeApply: true}}
	if a.agentRunOptions().Apply {
		t.Fatal("ui.auto_apply: false asks for review before the write, " +
			"but the turn still ran with apply on")
	}
}

// The other switches must not move when the new one does.
func TestAgentRunOptions_CarriesTheOtherSwitchesUnchanged(t *testing.T) {
	a := &App{cfg: Config{ReviewBeforeApply: true, Profile: "precision"}, allowExec: true, allowBrowser: true}
	opts := a.agentRunOptions()
	if !opts.AllowExec || !opts.AllowBrowser || opts.Profile != "precision" {
		t.Fatalf("unrelated options changed: %+v", opts)
	}
}
