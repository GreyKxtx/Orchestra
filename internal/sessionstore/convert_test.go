package sessionstore

import (
	"testing"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestStateMessagesToUI_promptCtxAndDiagsRoundTrip(t *testing.T) {
	msgs := []state.Message{
		{
			Role:              state.RoleAssistant,
			Text:              "ok",
			TokensIn:          120000,
			PromptCtx:         18234,
			ToolsExpanded:     true,
			ReasoningExpanded: true,
			ToolBlocks: []state.ToolBlock{{
				Name:   "edit",
				Status: state.ToolBlockCompleted,
				Diagnostics: []state.ToolDiagnostic{
					{StartLine: 1, StartCol: 2, Severity: "error", Message: "bad"},
				},
			}},
		},
	}
	ui := StateMessagesToUI(msgs)
	if len(ui) != 1 || ui[0].PromptCtx != 18234 || !ui[0].ToolsExpanded || len(ui[0].ToolBlocks[0].Diagnostics) != 1 {
		t.Fatalf("ui=%+v", ui[0])
	}
	back := UIMessagesToState(ui)
	if len(back) != 1 || back[0].PromptCtx != 18234 || !back[0].ToolsExpanded ||
		len(back[0].ToolBlocks[0].Diagnostics) != 1 || back[0].ToolBlocks[0].Diagnostics[0].Message != "bad" {
		t.Fatalf("back=%+v", back[0])
	}
}

func TestStateMessagesToUI_segmentsRoundTrip(t *testing.T) {
	msgs := []state.Message{{
		Role: state.RoleAssistant,
		Segments: []state.Segment{
			{Kind: state.SegmentReasoning, Text: "think"},
			{Kind: state.SegmentText, Text: "I'll edit"},
			{Kind: state.SegmentTools, Tools: []state.ToolBlock{{Name: "edit", Status: state.ToolBlockCompleted}}},
			{Kind: state.SegmentText, Text: "done"},
		},
	}}
	ui := StateMessagesToUI(msgs)
	if len(ui) != 1 || len(ui[0].Segments) != 4 {
		t.Fatalf("ui segments=%+v", ui)
	}
	back := UIMessagesToState(ui)
	if len(back) != 1 || len(back[0].Segments) != 4 {
		t.Fatalf("back=%+v", back)
	}
	if back[0].Segments[1].Text != "I'll edit" || back[0].Text == "" {
		t.Fatalf("text lost: %+v", back[0])
	}
}

func TestStateMessagesToUI_noticeSegmentRoundTrip(t *testing.T) {
	msgs := []state.Message{{
		Role: state.RoleAssistant,
		Segments: []state.Segment{
			{Kind: state.SegmentText, Text: "before"},
			{Kind: state.SegmentNotice, Text: "Контекст сжат", NoticeKind: state.SystemKindInfo},
			{Kind: state.SegmentText, Text: "after"},
		},
	}}
	ui := StateMessagesToUI(msgs)
	if len(ui) != 1 || len(ui[0].Segments) != 3 {
		t.Fatalf("ui=%+v", ui)
	}
	if ui[0].Segments[1].Kind != "notice" || ui[0].Segments[1].NoticeKind != "info" {
		t.Fatalf("notice segment=%+v", ui[0].Segments[1])
	}
	back := UIMessagesToState(ui)
	if len(back) != 1 || len(back[0].Segments) != 3 {
		t.Fatalf("back=%+v", back)
	}
	if back[0].Segments[1].Kind != state.SegmentNotice || back[0].Segments[1].NoticeKind != state.SystemKindInfo {
		t.Fatalf("notice lost: %+v", back[0].Segments[1])
	}
	if len(back[0].Notices) != 1 {
		t.Fatalf("Notices projection=%+v", back[0].Notices)
	}
}

func TestUIMessagesToState_synthesizesSegmentsFromFlat(t *testing.T) {
	ui := []sessionfile.UIMessage{{
		Role:       "assistant",
		Reasoning:  "r",
		Text:       "t",
		ToolBlocks: []sessionfile.UIToolBlock{{Name: "read", Status: "completed"}},
	}}
	back := UIMessagesToState(ui)
	if len(back) != 1 || len(back[0].Segments) != 3 {
		t.Fatalf("want synthesized segments, got %+v", back)
	}
}

func TestStateMessagesToUI_diffRoundTrip(t *testing.T) {
	msgs := []state.Message{
		{
			Role: state.RoleDiff,
			DiffFiles: []state.DiffFile{
				{Path: "a.go", Before: "old", After: "new"},
			},
			DiffExpanded: true,
		},
	}
	ui := StateMessagesToUI(msgs)
	if len(ui) != 1 || len(ui[0].DiffFiles) != 1 {
		t.Fatalf("ui=%+v", ui)
	}
	back := UIMessagesToState(ui)
	if len(back) != 1 || len(back[0].DiffFiles) != 1 || back[0].DiffFiles[0].After != "new" {
		t.Fatalf("back=%+v", back)
	}
}
