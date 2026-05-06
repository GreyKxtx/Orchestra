package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryWrite_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunner(dir, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	resp, err := r.MemoryWrite(context.Background(), MemoryWriteRequest{Content: "remember this"})
	if err != nil {
		t.Fatalf("MemoryWrite error: %v", err)
	}
	if resp.Written <= 0 {
		t.Errorf("expected Written > 0, got %d", resp.Written)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".orchestra", "memory", "agent.md"))
	if err != nil {
		t.Fatalf("agent.md not created: %v", err)
	}
	if !strings.Contains(string(data), "remember this") {
		t.Errorf("agent.md does not contain written content: %q", string(data))
	}
}

func TestMemoryWrite_Appends(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunner(dir, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	if _, err := r.MemoryWrite(context.Background(), MemoryWriteRequest{Content: "first entry"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.MemoryWrite(context.Background(), MemoryWriteRequest{Content: "second entry"}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".orchestra", "memory", "agent.md"))
	if err != nil {
		t.Fatalf("agent.md not found: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "first entry") || !strings.Contains(content, "second entry") {
		t.Errorf("agent.md missing entries: %q", content)
	}
}

func TestMemoryWrite_EmptyContentError(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunner(dir, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	_, err = r.MemoryWrite(context.Background(), MemoryWriteRequest{Content: "   "})
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestMemoryWrite_HasTimestamp(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunner(dir, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	if _, err := r.MemoryWrite(context.Background(), MemoryWriteRequest{Content: "ts check"}); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".orchestra", "memory", "agent.md"))
	if !strings.Contains(string(data), "T") || !strings.Contains(string(data), "Z") {
		t.Errorf("expected ISO timestamp in agent.md, got: %q", string(data))
	}
}
