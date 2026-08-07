package view

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestRenderAssistantMessage_NoticeInChronologicalOrder(t *testing.T) {
	c := NewChat(80, 24)
	m := state.Message{
		Role: state.RoleAssistant,
		Segments: []state.Segment{
			{Kind: state.SegmentText, Text: "AAA_BEFORE"},
			{Kind: state.SegmentNotice, Text: "BBB_COMPACT", NoticeKind: state.SystemKindInfo},
			{Kind: state.SegmentText, Text: "CCC_AFTER"},
		},
	}
	out := c.renderAssistantMessage(m, 80, false, "")
	// Markdown may style tokens; match stable substrings.
	idxBefore := strings.Index(out, "AAA_")
	idxInfo := strings.Index(out, "BBB_COMPACT")
	idxAfter := strings.Index(out, "CCC_")
	if idxBefore < 0 || idxInfo < 0 || idxAfter < 0 {
		t.Fatalf("missing pieces: %q", out)
	}
	if !(idxBefore < idxInfo && idxInfo < idxAfter) {
		t.Fatalf("notice not chronological: before=%d info=%d after=%d\n%s", idxBefore, idxInfo, idxAfter, out)
	}
}

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
	if !strings.Contains(out, "Done") {
		t.Fatalf("want English Done label, got %q", out)
	}
}

func TestRenderSystemMessage_errorLabelEnglish(t *testing.T) {
	out := RenderSystemMessage(state.Message{
		Role:       state.RoleSystem,
		SystemKind: state.SystemKindError,
		Text:       "stream request failed",
	}, 80)
	if !strings.Contains(out, "Error") || !strings.Contains(out, "stream request failed") {
		t.Fatalf("want English Error label, got %q", out)
	}
	if strings.Contains(out, "Ошибка") {
		t.Fatalf("Russian label should be gone: %q", out)
	}
}

func TestRenderSystemMessage_longErrorWraps(t *testing.T) {
	long := "request failed (status 400): This model's maximum context length is 51200 tokens. However, you requested 12054 output tokens and your prompt contains at least 40000 input tokens, for a total of at least 52054 tokens."
	out := RenderSystemMessage(state.Message{
		Role:       state.RoleSystem,
		SystemKind: state.SystemKindError,
		Text:       long,
	}, 80)
	if !strings.Contains(out, "52054") {
		t.Fatalf("truncated error body, got %q", out)
	}
	if !strings.Contains(out, "\n") {
		t.Fatalf("expected wrapped lines, got single line: %q", out)
	}
}
