package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/lessons"
)

func testMemoryRunner(t *testing.T, dir string) *Runner {
	t.Helper()
	r, err := NewRunner(dir, RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestMemoryWrite_DeptScopeLessons(t *testing.T) {
	dir := t.TempDir()
	r := testMemoryRunner(t, dir)

	resp, err := r.MemoryWrite(context.Background(), MemoryWriteRequest{
		Content: "always run go test ./pkg after edit",
		Scope:   "engineering",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Scope != "engineering" {
		t.Fatalf("scope = %q", resp.Scope)
	}
	path := filepath.Join(dir, filepath.FromSlash(lessons.RelDir), "engineering.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "agent_note") || !strings.Contains(string(data), "go test") {
		t.Fatalf("lessons file: %q", data)
	}
}

func TestMemoryWrite_DeptScopeBudget(t *testing.T) {
	dir := t.TempDir()
	r := testMemoryRunner(t, dir)
	for i := 0; i < 3; i++ {
		if _, err := r.MemoryWrite(context.Background(), MemoryWriteRequest{
			Content: "note " + string(rune('a'+i)),
			Scope:   "engineering",
		}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := r.MemoryWrite(context.Background(), MemoryWriteRequest{
		Content: "fourth",
		Scope:   "engineering",
	})
	if err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("expected budget error, got %v", err)
	}
}

func TestMemoryWrite_DeptScopeClip(t *testing.T) {
	dir := t.TempDir()
	r := testMemoryRunner(t, dir)
	long := strings.Repeat("x", lessons.MaxAgentNoteBytes+50)
	resp, err := r.MemoryWrite(context.Background(), MemoryWriteRequest{
		Content: long,
		Scope:   "frontend",
	})
	if err != nil {
		t.Fatal(err)
	}
	clipped := lessons.ClipAgentNote(long)
	if resp.Written != len(clipped) {
		t.Fatalf("written = %d want %d", resp.Written, len(clipped))
	}
}
