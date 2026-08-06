package view

import (
	"testing"

	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestLocalizeRetryHint_emptyResponse(t *testing.T) {
	got := LocalizeRetryHint("Model returned an empty response. Call a tool (read, grep, ls) or answer in plain text.")
	if got == "" || got == "Model returned an empty response. Call a tool (read, grep, ls) or answer in plain text." {
		t.Fatalf("expected Russian localization, got %q", got)
	}
}

func TestRenderSystemMessage_committed(t *testing.T) {
	out := RenderSystemMessage(state.Message{
		Role:       state.RoleSystem,
		SystemKind: state.SystemKindSuccess,
		Text:       "записано на диск: 2 ops",
	}, 80)
	if out == "" {
		t.Fatal("expected rendered system message")
	}
}
