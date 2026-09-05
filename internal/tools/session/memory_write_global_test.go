package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryWrite_ScopeGlobal_WritesToHomeMemoryFile(t *testing.T) {
	home := t.TempDir()
	setHomeForTest(t, home)
	c := testMemoryClient(t, t.TempDir())

	resp, err := c.MemoryWrite(context.Background(), MemoryWriteRequest{
		Content: "prefers TDD, red before green",
		Scope:   "global",
	})
	if err != nil {
		t.Fatalf("MemoryWrite: %v", err)
	}
	if resp.Scope != "global" {
		t.Errorf("Scope = %q, want global", resp.Scope)
	}

	data, err := os.ReadFile(filepath.Join(home, ".orchestra", "memory.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "prefers TDD") {
		t.Errorf("global memory file missing content: %q", data)
	}
}

func TestToolMemoryWrite_DescribesGlobalScope(t *testing.T) {
	def := ToolMemoryWrite()
	if !strings.Contains(def.Function.Description, "scope=global") {
		t.Errorf("memory_write description must document scope=global, got: %s", def.Function.Description)
	}
	if !strings.Contains(string(def.Function.Parameters), "global") {
		t.Errorf("memory_write scope parameter must mention global, got: %s", def.Function.Parameters)
	}
}

func setHomeForTest(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}
