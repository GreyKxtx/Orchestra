package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectMemory_OrchestraMD(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ORCHESTRA.md"), []byte("# Memory\nRemember this."), 0644); err != nil {
		t.Fatal(err)
	}

	result := LoadProjectMemory(dir, 2048)
	if !strings.Contains(result, "Remember this.") {
		t.Fatalf("expected content in result, got: %q", result)
	}
	if !strings.HasPrefix(result, "<project_memory>") {
		t.Fatalf("expected <project_memory> wrapper, got: %q", result)
	}
	if !strings.HasSuffix(result, "</project_memory>") {
		t.Fatalf("expected </project_memory> suffix, got: %q", result)
	}
}

func TestLoadProjectMemory_MemoryDir(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, ".orchestra", "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "a.md"), []byte("alpha"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "b.md"), []byte("beta"), 0644); err != nil {
		t.Fatal(err)
	}

	result := LoadProjectMemory(dir, 2048)
	if !strings.Contains(result, "alpha") || !strings.Contains(result, "beta") {
		t.Fatalf("expected both files in result, got: %q", result)
	}
	// Sorted order: a.md before b.md
	if strings.Index(result, "alpha") > strings.Index(result, "beta") {
		t.Fatal("expected a.md (alpha) before b.md (beta)")
	}
}

func TestLoadProjectMemory_NoSources(t *testing.T) {
	dir := t.TempDir()
	result := LoadProjectMemory(dir, 2048)
	if result != "" {
		t.Fatalf("expected empty string when no sources, got: %q", result)
	}
}

func TestLoadProjectMemory_OrchestraMDTakesPriority(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ORCHESTRA.md"), []byte("from ORCHESTRA.md"), 0644); err != nil {
		t.Fatal(err)
	}
	memDir := filepath.Join(dir, ".orchestra", "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "other.md"), []byte("from memory dir"), 0644); err != nil {
		t.Fatal(err)
	}

	result := LoadProjectMemory(dir, 2048)
	if !strings.Contains(result, "from ORCHESTRA.md") {
		t.Fatalf("expected ORCHESTRA.md priority, got: %q", result)
	}
	if strings.Contains(result, "from memory dir") {
		t.Fatalf("should not include memory dir when ORCHESTRA.md present, got: %q", result)
	}
}

func TestLoadProjectMemory_Truncation(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("x", 200)
	if err := os.WriteFile(filepath.Join(dir, "ORCHESTRA.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result := LoadProjectMemory(dir, 100)
	if !strings.Contains(result, "...(truncated)") {
		t.Fatalf("expected truncation marker, got: %q", result)
	}
}

func TestLoadProjectMemory_MemoryDirIgnoresNonMD(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, ".orchestra", "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "notes.txt"), []byte("should not appear"), 0644); err != nil {
		t.Fatal(err)
	}

	result := LoadProjectMemory(dir, 2048)
	if result != "" {
		t.Fatalf("expected empty result when only non-.md files, got: %q", result)
	}
}

func TestLoadProjectMemory_DefaultCapApplied(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("a", defaultMemoryCap+100)
	if err := os.WriteFile(filepath.Join(dir, "ORCHESTRA.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result := LoadProjectMemory(dir, 0) // 0 = use default cap
	if !strings.Contains(result, "...(truncated)") {
		t.Fatalf("expected truncation with default cap, got: %q", result)
	}
}
