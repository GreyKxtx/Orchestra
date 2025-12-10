package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsRepo(t *testing.T) {
	tmpDir := t.TempDir()

	// Not a repo initially
	if IsRepo(tmpDir) {
		t.Error("Expected false for non-repo directory")
	}

	// Initialize git repo
	cmd := exec.Command("git", "-C", tmpDir, "init")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Now it should be a repo
	if !IsRepo(tmpDir) {
		t.Error("Expected true for git repo")
	}
}

func TestIsClean(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "-C", tmpDir, "init")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Configure git (needed for tests)
	exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", tmpDir, "config", "user.name", "Test User").Run()

	// Should be clean initially (empty repo)
	clean, status, err := IsClean(tmpDir)
	if err != nil {
		t.Fatalf("IsClean failed: %v", err)
	}
	if !clean {
		t.Errorf("Expected clean repo, got status: %s", status)
	}

	// Create a file
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Should be dirty now
	clean, status, err = IsClean(tmpDir)
	if err != nil {
		t.Fatalf("IsClean failed: %v", err)
	}
	if clean {
		t.Error("Expected dirty repo after creating file")
	}
	if status == "" {
		t.Error("Expected non-empty status output")
	}
}

func TestCommitAll(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "-C", tmpDir, "init")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Configure git
	exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", tmpDir, "config", "user.name", "Test User").Run()

	// Create a file
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Commit
	if err := CommitAll(tmpDir, "test commit"); err != nil {
		t.Fatalf("CommitAll failed: %v", err)
	}

	// Should be clean now
	clean, _, err := IsClean(tmpDir)
	if err != nil {
		t.Fatalf("IsClean failed: %v", err)
	}
	if !clean {
		t.Error("Expected clean repo after commit")
	}
}
