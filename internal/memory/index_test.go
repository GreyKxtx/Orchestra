package memory

import (
	"strings"
	"testing"
)

func TestEntryIndexLine(t *testing.T) {
	line := entryIndexLine("*2026-09-05T10:00:00Z* [feedback]\n\nDo not reformat files\nyou did not edit")
	if !strings.Contains(line, "feedback") {
		t.Errorf("line = %q, want the type", line)
	}
	if !strings.Contains(line, "Do not reformat files") {
		t.Errorf("line = %q, want the first line of the body", line)
	}
	if strings.Contains(line, "\n") {
		t.Errorf("line = %q, want a single line", line)
	}
}

func TestEntryIndexLine_Truncates(t *testing.T) {
	long := strings.Repeat("word ", 100)
	line := entryIndexLine("*t* [project]\n\n" + long)
	if len(line) > indexLineMax+32 {
		t.Errorf("line is %d bytes, want it capped near %d", len(line), indexLineMax)
	}
}

func TestSliceEntriesWithIndex_FullTextWhileItFits(t *testing.T) {
	entries := []string{
		"*t1* [feedback]\n\nnever reformat untouched files",
		"*t2* [project]\n\nbuild runs via make",
	}
	got := sliceEntriesWithIndex(entries, 8192)
	if !strings.Contains(got, "never reformat untouched files") ||
		!strings.Contains(got, "build runs via make") {
		t.Fatalf("both entries should be in full:\n%s", got)
	}
	if strings.Contains(got, indexHeader) {
		t.Errorf("no index is needed when everything fits:\n%s", got)
	}
}

func TestSliceEntriesWithIndex_OverflowBecomesAnIndexNotSilence(t *testing.T) {
	// What used to happen: entries past the budget were cut off and the model
	// had no idea they existed, so it could not even ask for them.
	var entries []string
	entries = append(entries, "*t0* [feedback]\n\nKEEP-ME never reformat untouched files")
	for i := 0; i < 40; i++ {
		entries = append(entries, "*t* [project]\n\nOVERFLOW-"+strings.Repeat("x", 200)+string(rune('a'+i%26)))
	}
	got := sliceEntriesWithIndex(entries, 2048)

	if !strings.Contains(got, "KEEP-ME") {
		t.Fatalf("the highest-priority entry must survive in full:\n%s", got)
	}
	if !strings.Contains(got, indexHeader) {
		t.Fatalf("the overflow must be indexed:\n%s", got)
	}
	if !strings.Contains(got, "OVERFLOW-") {
		t.Errorf("the index must name what did not fit:\n%s", got)
	}
	if len(got) > 2048+len(indexHeader)+256 {
		t.Errorf("result is %d bytes, well past the %d budget", len(got), 2048)
	}
}

func TestSliceEntriesWithIndex_Empty(t *testing.T) {
	if got := sliceEntriesWithIndex(nil, 1024); got != "" {
		t.Errorf("got %q", got)
	}
	if got := sliceEntriesWithIndex([]string{"*t* [project]\n\nx"}, 0); got != "" {
		t.Errorf("got %q with no budget", got)
	}
}
