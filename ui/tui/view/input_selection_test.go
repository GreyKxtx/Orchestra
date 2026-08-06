package view

import "testing"

func TestMentionSpans(t *testing.T) {
	runes := []rune("look at @src/main.go and @README.md please")
	spans := mentionSpans(runes)
	if len(spans) != 2 {
		t.Fatalf("want 2 mentions, got %d: %v", len(spans), spans)
	}
	got0 := string(runes[spans[0][0]:spans[0][1]])
	got1 := string(runes[spans[1][0]:spans[1][1]])
	if got0 != "@src/main.go" || got1 != "@README.md" {
		t.Fatalf("got %q %q", got0, got1)
	}
	if inMention(spans, 0) || !inMention(spans, spans[0][0]) {
		t.Fatal("inMention bounds wrong")
	}
}

func TestMentionSpans_IgnoresEmailish(t *testing.T) {
	// '@' mid-token is not a mention start
	runes := []rune("user@host path")
	if spans := mentionSpans(runes); len(spans) != 0 {
		t.Fatalf("expected no mention, got %v", spans)
	}
}

func TestCollapseSelection_SelectAllLeft(t *testing.T) {
	in := NewInput(40)
	in.SetValue("hello world")
	in.SelectAll()
	if !in.HasSelection() {
		t.Fatal("expected selection after SelectAll")
	}
	if !in.CollapseSelectionToStart() {
		t.Fatal("collapse start failed")
	}
	if in.HasSelection() {
		t.Fatal("selection should be cleared")
	}
	if in.CursorPos() != 0 {
		t.Fatalf("cursor want 0, got %d", in.CursorPos())
	}

	in.SelectAll()
	if !in.CollapseSelectionToEnd() {
		t.Fatal("collapse end failed")
	}
	if in.CursorPos() != len([]rune("hello world")) {
		t.Fatalf("cursor want end, got %d", in.CursorPos())
	}
}
