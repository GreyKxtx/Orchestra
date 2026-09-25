package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/memory"
	"github.com/orchestra/orchestra/llm"
)

func testMemoryClient(t *testing.T, dir string) *Client {
	t.Helper()
	return NewClient(dir, func() string { return "" }, func() memory.Config { return memory.DefaultConfig() }, func() config.EmbedConfig { return config.EmbedConfig{} }, nil)
}

func TestMemoryWrite_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	c := testMemoryClient(t, dir)

	resp, err := c.MemoryWrite(context.Background(), MemoryWriteRequest{Content: "remember this"})
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
	c := testMemoryClient(t, dir)

	if _, err := c.MemoryWrite(context.Background(), MemoryWriteRequest{Content: "first entry"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.MemoryWrite(context.Background(), MemoryWriteRequest{Content: "second entry"}); err != nil {
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
	c := testMemoryClient(t, dir)

	_, err := c.MemoryWrite(context.Background(), MemoryWriteRequest{Content: "   "})
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestMemoryWrite_HasTimestamp(t *testing.T) {
	dir := t.TempDir()
	c := testMemoryClient(t, dir)

	if _, err := c.MemoryWrite(context.Background(), MemoryWriteRequest{Content: "ts check"}); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".orchestra", "memory", "agent.md"))
	if !strings.Contains(string(data), "T") || !strings.Contains(string(data), "Z") {
		t.Errorf("expected ISO timestamp in agent.md, got: %q", string(data))
	}
}

// A note says which agent of which run wrote it.
func TestMemoryWrite_RecordsWhoWroteIt(t *testing.T) {
	dir := t.TempDir()
	c := testMemoryClient(t, dir)
	sub := llm.WithTrace(context.Background(), llm.Trace{RunID: "r1", TaskID: "task_2_9", Depth: 1})
	if _, err := c.MemoryWrite(sub, MemoryWriteRequest{Content: "the billing module is legacy"}); err != nil {
		t.Fatal(err)
	}
	root := llm.WithTrace(context.Background(), llm.Trace{RunID: "r1"})
	if _, err := c.MemoryWrite(root, MemoryWriteRequest{Content: "tests use testify only in pkg/api"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".orchestra", "memory", "agent.md"))
	for _, want := range []string{"(by task_2_9, run r1) [project]", "(by main agent, run r1) [project]"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("agent.md lacks %q:\n%s", want, data)
		}
	}
}
