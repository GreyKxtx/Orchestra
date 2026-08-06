package sessionstore

import (
	"testing"

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
