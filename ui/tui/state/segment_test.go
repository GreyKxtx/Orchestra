package state_test

import (
	"testing"

	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestSession_ChronologicalSegments(t *testing.T) {
	s := state.NewSession()
	s.StartAssistant("build", "local")
	s.AppendAssistantReasoningDelta("plan A")
	s.AppendAssistantDelta("I'll read the file.")
	s.AppendToolBlock(state.ToolBlock{ID: "t1", Name: "read", Status: state.ToolBlockRunning})
	s.UpdateToolBlock("t1", state.ToolBlockCompleted, "ok", nil)
	s.AppendAssistantDelta(" Now editing.")
	s.AppendToolBlock(state.ToolBlock{ID: "t2", Name: "edit", Status: state.ToolBlockRunning})
	s.UpdateToolBlock("t2", state.ToolBlockCompleted, "ok", nil)
	s.AppendAssistantDelta(" Done.")

	m := s.Messages[0]
	if len(m.Segments) < 5 {
		t.Fatalf("want ≥5 segments, got %d: %+v", len(m.Segments), m.Segments)
	}
	kinds := make([]state.SegmentKind, len(m.Segments))
	for i, seg := range m.Segments {
		kinds[i] = seg.Kind
	}
	wantPrefix := []state.SegmentKind{
		state.SegmentReasoning,
		state.SegmentText,
		state.SegmentTools,
		state.SegmentText,
		state.SegmentTools,
		state.SegmentText,
	}
	for i, w := range wantPrefix {
		if i >= len(kinds) || kinds[i] != w {
			t.Fatalf("kinds=%v want prefix %v", kinds, wantPrefix)
		}
	}
	if m.Text == "" || m.Reasoning == "" || len(m.ToolBlocks) != 2 {
		t.Fatalf("projections unset: text=%q reasoning=%q tools=%d", m.Text, m.Reasoning, len(m.ToolBlocks))
	}
	// Truncate must not wipe mid-step narration.
	before := m.Text
	s.TruncateAssistantText(0)
	if s.Messages[0].Text != before {
		t.Fatal("TruncateAssistantText should be a no-op with segments")
	}
}

func TestNormalizeSegments_FromFlat(t *testing.T) {
	m := state.Message{
		Role:      state.RoleAssistant,
		Reasoning: "think",
		Text:      "answer",
		ToolBlocks: []state.ToolBlock{
			{ID: "1", Name: "read", Status: state.ToolBlockCompleted},
		},
	}
	m.NormalizeSegments()
	if len(m.Segments) != 3 {
		t.Fatalf("want 3 synthetic segments, got %+v", m.Segments)
	}
	if m.Segments[0].Kind != state.SegmentReasoning || m.Segments[1].Kind != state.SegmentTools || m.Segments[2].Kind != state.SegmentText {
		t.Fatalf("legacy order mismatch: %+v", m.Segments)
	}
}
