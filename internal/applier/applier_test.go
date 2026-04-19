package applier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/parser"
)

func TestApplyChanges_ReplaceBlock_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	originalContent := "package main\n\nfunc a() {}\nfunc b() { old }\nfunc c() {}\n"
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	changes := []parser.FileChange{
		{
			Path: "test.go",
			Operations: []parser.Operation{
				{
					Type:     parser.OpReplaceBlock,
					OldBlock: "func b() { old }",
					NewBlock: "func b() { new }",
				},
			},
		},
	}

	opts := ApplyOptions{
		DryRun:       true,
		Backup:       false,
		BackupSuffix: ".orchestra.bak",
	}

	result, err := ApplyChanges(tmpDir, changes, opts)
	if err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	if len(result.Diffs) != 1 {
		t.Fatalf("Expected 1 diff, got %d", len(result.Diffs))
	}

	diff := result.Diffs[0]
	if diff.Before != originalContent {
		t.Errorf("Before content mismatch")
	}

	expectedAfter := "package main\n\nfunc a() {}\nfunc b() { new }\nfunc c() {}\n"
	if diff.After != expectedAfter {
		t.Errorf("After content mismatch. Expected:\n%s\nGot:\n%s", expectedAfter, diff.After)
	}

	// Verify file was not changed
	data, _ := os.ReadFile(testFile)
	if string(data) != originalContent {
		t.Error("File was modified in dry-run mode")
	}
}

func TestApplyChanges_ReplaceBlock_Apply(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	originalContent := "package main\n\nfunc a() {}\nfunc b() { old }\nfunc c() {}\n"
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	changes := []parser.FileChange{
		{
			Path: "test.go",
			Operations: []parser.Operation{
				{
					Type:     parser.OpReplaceBlock,
					OldBlock: "func b() { old }",
					NewBlock: "func b() { new }",
				},
			},
		},
	}

	opts := ApplyOptions{
		DryRun:       false,
		Backup:       true,
		BackupSuffix: ".orchestra.bak",
	}

	result, err := ApplyChanges(tmpDir, changes, opts)
	if err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	// Verify file was changed
	data, _ := os.ReadFile(testFile)
	expectedAfter := "package main\n\nfunc a() {}\nfunc b() { new }\nfunc c() {}\n"
	if string(data) != expectedAfter {
		t.Errorf("File was not updated correctly. Expected:\n%s\nGot:\n%s", expectedAfter, string(data))
	}

	// Verify backup was created
	backupFile := testFile + ".orchestra.bak"
	if _, err := os.Stat(backupFile); err != nil {
		t.Errorf("Backup file was not created: %v", err)
	}

	backupData, _ := os.ReadFile(backupFile)
	if string(backupData) != originalContent {
		t.Error("Backup content mismatch")
	}

	_ = result
}

func TestApplyChanges_ReplaceFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	originalContent := "old content"
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	changes := []parser.FileChange{
		{
			Path: "test.go",
			Operations: []parser.Operation{
				{
					Type:           parser.OpReplaceFile,
					NewFileContent: "new content",
				},
			},
		},
	}

	opts := ApplyOptions{
		DryRun: false,
		Backup: false,
	}

	result, err := ApplyChanges(tmpDir, changes, opts)
	if err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	// Verify file was replaced (should end with newline)
	data, _ := os.ReadFile(testFile)
	expected := "new content\n"
	if string(data) != expected {
		t.Errorf("File was not replaced. Expected %q, got %q", expected, string(data))
	}

	_ = result
}

func TestApplyChanges_ReplaceFile_NewFile(t *testing.T) {
	tmpDir := t.TempDir()

	changes := []parser.FileChange{
		{
			Path: "new.go",
			Operations: []parser.Operation{
				{
					Type:           parser.OpReplaceFile,
					NewFileContent: "package main\n",
				},
			},
		},
	}

	opts := ApplyOptions{
		DryRun: false,
		Backup: false,
	}

	_, err := ApplyChanges(tmpDir, changes, opts)
	if err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	// Verify file was created
	testFile := filepath.Join(tmpDir, "new.go")
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("File was not created: %v", err)
	}

	if string(data) != "package main\n" {
		t.Errorf("File content mismatch. Expected 'package main\\n', got '%s'", string(data))
	}
}

func TestApplyChanges_BlockNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	if err := os.WriteFile(testFile, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	changes := []parser.FileChange{
		{
			Path: "test.go",
			Operations: []parser.Operation{
				{
					Type:     parser.OpReplaceBlock,
					OldBlock: "func b() { ... }",
					NewBlock: "func b() { new }",
				},
			},
		},
	}

	opts := ApplyOptions{
		DryRun: false,
		Backup: false,
	}

	_, err := ApplyChanges(tmpDir, changes, opts)
	if err == nil {
		t.Fatal("Expected error for block not found, got nil")
	}

	if !strings.Contains(err.Error(), "old block not found") {
		t.Errorf("Expected 'old block not found' error, got: %v", err)
	}
}

func TestApplyChanges_EmptyOldBlock_Appends(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	originalContent := "package main\n\nfunc a() {}\n"
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	changes := []parser.FileChange{
		{
			Path: "test.go",
			Operations: []parser.Operation{
				{
					Type:     parser.OpReplaceBlock,
					OldBlock: "", // Empty old block = explicit append signal
					NewBlock: "func b() {}\n",
				},
			},
		},
	}

	opts := ApplyOptions{
		DryRun: false,
		Backup: false,
	}

	result, err := ApplyChanges(tmpDir, changes, opts)
	if err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	// Verify file was updated
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	expected := "package main\n\nfunc a() {}\nfunc b() {}\n"
	if string(data) != expected {
		t.Errorf("File was not updated correctly.\nExpected:\n%q\nGot:\n%q", expected, string(data))
	}

	// Verify diff
	if len(result.Diffs) != 1 {
		t.Fatalf("Expected 1 diff, got %d", len(result.Diffs))
	}

	diff := result.Diffs[0]
	if diff.Before != originalContent {
		t.Errorf("Before content mismatch")
	}
	if diff.After != expected {
		t.Errorf("After content mismatch")
	}
}

func TestApplyChanges_MultipleOperations_SameFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	originalContent := "package main\n\nfunc a() { old }\nfunc b() {}\n"
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	changes := []parser.FileChange{
		{
			Path: "test.go",
			Operations: []parser.Operation{
				{
					Type:     parser.OpReplaceBlock,
					OldBlock: "func a() { old }",
					NewBlock: "func a() { new }",
				},
				{
					Type:     parser.OpReplaceBlock,
					OldBlock: "", // Append new function
					NewBlock: "func c() {}\n",
				},
			},
		},
	}

	opts := ApplyOptions{
		DryRun: false,
		Backup: false,
	}

	result, err := ApplyChanges(tmpDir, changes, opts)
	if err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	// Verify file was updated correctly
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	expected := "package main\n\nfunc a() { new }\nfunc b() {}\nfunc c() {}\n"
	if string(data) != expected {
		t.Errorf("File was not updated correctly.\nExpected:\n%q\nGot:\n%q", expected, string(data))
	}

	_ = result
}

func TestApplyChanges_ReplaceFile_TrailingNewline(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	originalContent := "old content"
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	changes := []parser.FileChange{
		{
			Path: "test.go",
			Operations: []parser.Operation{
				{
					Type:           parser.OpReplaceFile,
					NewFileContent: "new content", // No trailing newline
				},
			},
		},
	}

	opts := ApplyOptions{
		DryRun: false,
		Backup: false,
	}

	_, err := ApplyChanges(tmpDir, changes, opts)
	if err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	// Verify file ends with newline
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if !strings.HasSuffix(string(data), "\n") {
		t.Error("File should end with newline after OpReplaceFile")
	}

	expected := "new content\n"
	if string(data) != expected {
		t.Errorf("File content mismatch. Expected %q, got %q", expected, string(data))
	}
}

func TestApplyChanges_MultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()
	fileA := filepath.Join(tmpDir, "a.go")
	fileB := filepath.Join(tmpDir, "b.go")

	contentA := "package a\n\nfunc a() {}\n"
	contentB := "package b\n\nfunc b() {}\n"

	if err := os.WriteFile(fileA, []byte(contentA), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := os.WriteFile(fileB, []byte(contentB), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	changes := []parser.FileChange{
		{
			Path: "a.go",
			Operations: []parser.Operation{
				{
					Type:     parser.OpReplaceBlock,
					OldBlock: "func a() {}",
					NewBlock: "func a() { updated }",
				},
			},
		},
		{
			Path: "b.go",
			Operations: []parser.Operation{
				{
					Type:     parser.OpReplaceBlock,
					OldBlock: "", // Append
					NewBlock: "func c() {}\n",
				},
			},
		},
	}

	opts := ApplyOptions{
		DryRun: false,
		Backup: false,
	}

	result, err := ApplyChanges(tmpDir, changes, opts)
	if err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	// Verify both files were updated
	dataA, _ := os.ReadFile(fileA)
	expectedA := "package a\n\nfunc a() { updated }\n"
	if string(dataA) != expectedA {
		t.Errorf("File A was not updated correctly. Expected %q, got %q", expectedA, string(dataA))
	}

	dataB, _ := os.ReadFile(fileB)
	expectedB := "package b\n\nfunc b() {}\nfunc c() {}\n"
	if string(dataB) != expectedB {
		t.Errorf("File B was not updated correctly. Expected %q, got %q", expectedB, string(dataB))
	}

	// Verify result contains both diffs
	if len(result.Diffs) != 2 {
		t.Fatalf("Expected 2 diffs, got %d", len(result.Diffs))
	}

	if result.Diffs[0].Path != "a.go" {
		t.Errorf("First diff should be for a.go, got %s", result.Diffs[0].Path)
	}
	if result.Diffs[1].Path != "b.go" {
		t.Errorf("Second diff should be for b.go, got %s", result.Diffs[1].Path)
	}
}

func TestApplyChanges_PathTraversal_Rejected(t *testing.T) {
	tmpDir := t.TempDir()

	changes := []parser.FileChange{
		{
			Path: "../evil.go", // Path traversal attempt
			Operations: []parser.Operation{
				{
					Type:           parser.OpReplaceFile,
					NewFileContent: "evil content",
				},
			},
		},
	}

	opts := ApplyOptions{
		DryRun: false,
		Backup: false,
	}

	_, err := ApplyChanges(tmpDir, changes, opts)
	if err == nil {
		t.Fatal("Expected error for path traversal, got nil")
	}

	if !strings.Contains(err.Error(), "invalid file path") {
		t.Errorf("Expected 'invalid file path' error, got: %v", err)
	}
}

func TestApplyChanges_ReplaceFile_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	originalContent := "old content"
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	changes := []parser.FileChange{
		{
			Path: "test.go",
			Operations: []parser.Operation{
				{
					Type:           parser.OpReplaceFile,
					NewFileContent: "new content",
				},
			},
		},
	}

	opts := ApplyOptions{
		DryRun: true,
		Backup: false,
	}

	result, err := ApplyChanges(tmpDir, changes, opts)
	if err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	// Verify file was NOT changed
	data, _ := os.ReadFile(testFile)
	if string(data) != originalContent {
		t.Error("File was modified in dry-run mode")
	}

	// Verify diff shows the change
	if len(result.Diffs) != 1 {
		t.Fatalf("Expected 1 diff, got %d", len(result.Diffs))
	}

	diff := result.Diffs[0]
	if diff.Before != originalContent {
		t.Errorf("Before content mismatch")
	}

	expectedAfter := "new content\n"
	if diff.After != expectedAfter {
		t.Errorf("After content mismatch. Expected %q, got %q", expectedAfter, diff.After)
	}
}
