package lessons

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func TestAppendDedupAndInject(t *testing.T) {
	root := t.TempDir()
	e := Entry{
		Dept:   "engineering",
		Kind:   KindPattern,
		Task:   "add retry helper",
		Files:  []string{"internal/foo.go"},
		Tools:  "read×2 edit×1",
		Verify: "passed",
	}
	if err := Append(root, e); err != nil {
		t.Fatal(err)
	}
	if err := Append(root, e); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(RelDir), "engineering.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "## ") != 1 {
		t.Fatalf("dedup failed: want 1 entry, got %q", data)
	}

	inject := FormatInject(root, "engineering")
	if inject == "" || !strings.Contains(inject, "<dept_lessons") || !strings.Contains(inject, "add retry helper") {
		t.Fatalf("FormatInject = %q", inject)
	}
}

func TestNormalizeDept(t *testing.T) {
	if got := NormalizeDept(""); got != "engineering" {
		t.Fatalf("empty = %q", got)
	}
	if got := NormalizeDept("QA@prod"); got != "qa@prod" {
		t.Fatalf("valid = %q", got)
	}
	if got := NormalizeDept("!!!"); got != "engineering" {
		t.Fatalf("invalid = %q", got)
	}
}

func TestTrimFileKeepsTail(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x.md")
	for i := 0; i < maxStoredEntries+5; i++ {
		if err := Append(root, Entry{
			Dept: "eng",
			Kind: KindAgentNote,
			Note: strings.Repeat("n", i+1),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Append writes to eng.md via NormalizeDept("eng") -> eng
	path = filepath.Join(root, filepath.FromSlash(RelDir), "eng.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "## ") > maxStoredEntries {
		t.Fatalf("trim failed: %d entries", strings.Count(string(data), "## "))
	}
}

func TestIsDeptScope(t *testing.T) {
	if IsDeptScope("project") || IsDeptScope("session") || !IsDeptScope("engineering") {
		t.Fatal("IsDeptScope mismatch")
	}
}

func TestAppendAgentNote(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("n", MaxAgentNoteBytes+20)
	if err := AppendAgentNote(root, "eng", long); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(RelDir), "eng.md")
	data, _ := os.ReadFile(path)
	if len(data) > MaxAgentNoteBytes+200 {
		t.Fatalf("note too long in file")
	}
}

func TestToolCountsFromHistory(t *testing.T) {
	hist := []llm.Message{
		{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				{Function: llm.ToolCallFunc{Name: "read"}},
				{Function: llm.ToolCallFunc{Name: "read"}},
				{Function: llm.ToolCallFunc{Name: "edit"}},
			},
		},
	}
	got := ToolCountsFromHistory(hist)
	if got != "edit×1 read×2" {
		t.Fatalf("ToolCountsFromHistory = %q", got)
	}
}
