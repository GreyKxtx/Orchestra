package search

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearchInProject_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create test files
	testFile1 := filepath.Join(tmpDir, "file1.go")
	testFile2 := filepath.Join(tmpDir, "file2.go")
	
	content1 := "package main\n\nfunc hello() {\n\tfmt.Println(\"hello\")\n}\n"
	content2 := "package main\n\nfunc world() {\n\tfmt.Println(\"world\")\n}\n"
	
	if err := os.WriteFile(testFile1, []byte(content1), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := os.WriteFile(testFile2, []byte(content2), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	opts := DefaultOptions()
	matches, err := SearchInProject(tmpDir, "hello", []string{}, opts)
	if err != nil {
		t.Fatalf("SearchInProject failed: %v", err)
	}

	if len(matches) == 0 {
		t.Fatal("Expected at least one match, got 0")
	}

	found := false
	for _, match := range matches {
		if match.LineText == "func hello() {" {
			found = true
			if match.Line != 3 {
				t.Errorf("Expected line 3, got %d", match.Line)
			}
		}
	}

	if !found {
		t.Error("Expected to find 'func hello() {'")
	}
}

func TestSearchInProject_ExcludeDirs(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create file in root
	rootFile := filepath.Join(tmpDir, "root.go")
	if err := os.WriteFile(rootFile, []byte("package main\nfunc root() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create excluded directory
	excludedDir := filepath.Join(tmpDir, "excluded")
	if err := os.MkdirAll(excludedDir, 0755); err != nil {
		t.Fatalf("Failed to create excluded dir: %v", err)
	}

	excludedFile := filepath.Join(excludedDir, "excluded.go")
	if err := os.WriteFile(excludedFile, []byte("package main\nfunc excluded() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create excluded file: %v", err)
	}

	opts := DefaultOptions()
	matches, err := SearchInProject(tmpDir, "func", []string{"excluded"}, opts)
	if err != nil {
		t.Fatalf("SearchInProject failed: %v", err)
	}

	// Should find root but not excluded
	foundRoot := false
	foundExcluded := false
	for _, match := range matches {
		if filepath.Base(match.FilePath) == "root.go" {
			foundRoot = true
		}
		if filepath.Base(match.FilePath) == "excluded.go" {
			foundExcluded = true
		}
	}

	if !foundRoot {
		t.Error("Expected to find root.go")
	}
	if foundExcluded {
		t.Error("Should not find excluded.go")
	}
}

func TestSearchInProject_CaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()
	
	testFile := filepath.Join(tmpDir, "test.go")
	content := "package main\n\nfunc Hello() {}\nfunc hello() {}\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	opts := DefaultOptions()
	opts.CaseInsensitive = true
	
	matches, err := SearchInProject(tmpDir, "HELLO", []string{}, opts)
	if err != nil {
		t.Fatalf("SearchInProject failed: %v", err)
	}

	if len(matches) != 2 {
		t.Errorf("Expected 2 matches (case-insensitive), got %d", len(matches))
	}
}

func TestSearchInProject_MaxMatchesPerFile(t *testing.T) {
	tmpDir := t.TempDir()
	
	testFile := filepath.Join(tmpDir, "test.go")
	content := "func a() {}\nfunc b() {}\nfunc c() {}\nfunc d() {}\nfunc e() {}\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	opts := DefaultOptions()
	opts.MaxMatchesPerFile = 3
	
	matches, err := SearchInProject(tmpDir, "func", []string{}, opts)
	if err != nil {
		t.Fatalf("SearchInProject failed: %v", err)
	}

	if len(matches) > 3 {
		t.Errorf("Expected max 3 matches, got %d", len(matches))
	}
}

func TestSearchInProject_EmptyQuery(t *testing.T) {
	tmpDir := t.TempDir()
	
	opts := DefaultOptions()
	_, err := SearchInProject(tmpDir, "", []string{}, opts)
	if err == nil {
		t.Error("Expected error for empty query, got nil")
	}
}

