package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/memory"
)

func writeAndRead(t *testing.T, req MemoryWriteRequest) string {
	t.Helper()
	dir := t.TempDir()
	c := NewClient(dir, func() string { return "" }, func() memory.Config { return memory.DefaultConfig() }, func() config.EmbedConfig { return config.EmbedConfig{} }, nil)
	if _, err := c.MemoryWrite(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".orchestra", "memory", "agent.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestMemoryWrite_CarriesTheTypeThrough(t *testing.T) {
	body := writeAndRead(t, MemoryWriteRequest{
		Content: "Do not reformat untouched files.",
		Type:    "feedback",
	})
	if !strings.Contains(body, "[feedback]") {
		t.Errorf("agent.md = %q", body)
	}
}

func TestMemoryWrite_NoTypeIsAProjectFact(t *testing.T) {
	body := writeAndRead(t, MemoryWriteRequest{Content: "Build runs via make."})
	if !strings.Contains(body, "[project]") {
		t.Errorf("agent.md = %q", body)
	}
}

func TestMemoryWrite_AModelInventingATypeDoesNotBreakTheFile(t *testing.T) {
	body := writeAndRead(t, MemoryWriteRequest{Content: "x", Type: "IMPORTANT!!"})
	if !strings.Contains(body, "[project]") {
		t.Errorf("agent.md = %q", body)
	}
}
