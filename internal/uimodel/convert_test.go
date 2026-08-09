package uimodel_test

import (
	"testing"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/internal/uimodel"
)

func TestToSessionfile_promptCtxAndDiagsRoundTrip(t *testing.T) {
	msgs := []uimodel.Message{
		{
			Role:              uimodel.RoleAssistant,
			Text:              "ok",
			TokensIn:          120000,
			PromptCtx:         18234,
			ToolsExpanded:     true,
			ReasoningExpanded: true,
			ToolBlocks: []uimodel.ToolBlock{{
				Name:   "edit",
				Status: uimodel.ToolBlockCompleted,
				Diagnostics: []uimodel.ToolDiagnostic{
					{StartLine: 1, StartCol: 2, Severity: "error", Message: "bad"},
				},
			}},
		},
	}
	ui := uimodel.ToSessionfile(msgs)
	if len(ui) != 1 || ui[0].PromptCtx != 18234 || !ui[0].ToolsExpanded || len(ui[0].ToolBlocks[0].Diagnostics) != 1 {
		t.Fatalf("ui=%+v", ui[0])
	}
	back := uimodel.FromSessionfile(ui)
	if len(back) != 1 || back[0].PromptCtx != 18234 || !back[0].ToolsExpanded ||
		len(back[0].ToolBlocks[0].Diagnostics) != 1 || back[0].ToolBlocks[0].Diagnostics[0].Message != "bad" {
		t.Fatalf("back=%+v", back[0])
	}
}

func TestToSessionfile_segmentsRoundTrip(t *testing.T) {
	msgs := []uimodel.Message{{
		Role: uimodel.RoleAssistant,
		Segments: []uimodel.Segment{
			{Kind: uimodel.SegmentReasoning, Text: "think"},
			{Kind: uimodel.SegmentText, Text: "I'll edit"},
			{Kind: uimodel.SegmentTools, Tools: []uimodel.ToolBlock{{Name: "edit", Status: uimodel.ToolBlockCompleted}}},
			{Kind: uimodel.SegmentText, Text: "done"},
		},
	}}
	ui := uimodel.ToSessionfile(msgs)
	if len(ui) != 1 || len(ui[0].Segments) != 4 {
		t.Fatalf("ui segments=%+v", ui)
	}
	back := uimodel.FromSessionfile(ui)
	if len(back) != 1 || len(back[0].Segments) != 4 {
		t.Fatalf("back=%+v", back)
	}
	if back[0].Segments[1].Text != "I'll edit" || back[0].Text == "" {
		t.Fatalf("text lost: %+v", back[0])
	}
}

func TestToSessionfile_noticeSegmentRoundTrip(t *testing.T) {
	msgs := []uimodel.Message{{
		Role: uimodel.RoleAssistant,
		Segments: []uimodel.Segment{
			{Kind: uimodel.SegmentText, Text: "before"},
			{Kind: uimodel.SegmentNotice, Text: "Контекст сжат", NoticeKind: uimodel.SystemKindInfo},
			{Kind: uimodel.SegmentText, Text: "after"},
		},
	}}
	ui := uimodel.ToSessionfile(msgs)
	if len(ui) != 1 || len(ui[0].Segments) != 3 {
		t.Fatalf("ui=%+v", ui)
	}
	if ui[0].Segments[1].Kind != "notice" || ui[0].Segments[1].NoticeKind != "info" {
		t.Fatalf("notice segment=%+v", ui[0].Segments[1])
	}
	back := uimodel.FromSessionfile(ui)
	if len(back) != 1 || len(back[0].Segments) != 3 {
		t.Fatalf("back=%+v", back)
	}
	if back[0].Segments[1].Kind != uimodel.SegmentNotice || back[0].Segments[1].NoticeKind != uimodel.SystemKindInfo {
		t.Fatalf("notice lost: %+v", back[0].Segments[1])
	}
	if len(back[0].Notices) != 1 {
		t.Fatalf("Notices projection=%+v", back[0].Notices)
	}
}

func TestFromSessionfile_synthesizesSegmentsFromFlat(t *testing.T) {
	ui := []sessionfile.UIMessage{{
		Role:       "assistant",
		Reasoning:  "r",
		Text:       "t",
		ToolBlocks: []sessionfile.UIToolBlock{{Name: "read", Status: "completed"}},
	}}
	back := uimodel.FromSessionfile(ui)
	if len(back) != 1 || len(back[0].Segments) != 3 {
		t.Fatalf("want synthesized segments, got %+v", back)
	}
}

func TestToSessionfile_diffRoundTrip(t *testing.T) {
	msgs := []uimodel.Message{
		{
			Role: uimodel.RoleDiff,
			DiffFiles: []uimodel.DiffFile{
				{Path: "a.go", Before: "old", After: "new"},
			},
			DiffExpanded: true,
		},
	}
	ui := uimodel.ToSessionfile(msgs)
	if len(ui) != 1 || len(ui[0].DiffFiles) != 1 {
		t.Fatalf("ui=%+v", ui)
	}
	back := uimodel.FromSessionfile(ui)
	if len(back) != 1 || len(back[0].DiffFiles) != 1 || back[0].DiffFiles[0].After != "new" {
		t.Fatalf("back=%+v", back)
	}
}
