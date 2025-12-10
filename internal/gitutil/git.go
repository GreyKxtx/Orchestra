package gitutil

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsRepo checks if the given directory is a git repository
func IsRepo(root string) bool {
	cmd := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree")
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

// IsClean checks if git repository is clean (no uncommitted changes)
// Returns: (isClean, statusOutput, error)
func IsClean(root string) (bool, string, error) {
	cmd := exec.Command("git", "-C", root, "status", "--porcelain")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return false, "", fmt.Errorf("failed to check git status: %w", err)
	}

	status := strings.TrimSpace(out.String())
	return status == "", status, nil
}

// CommitAll stages all changes and creates a commit
func CommitAll(root, message string) error {
	// Stage all changes
	cmd := exec.Command("git", "-C", root, "add", "-A")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stage changes: %w", err)
	}

	// Create commit
	cmd = exec.Command("git", "-C", root, "commit", "-m", message)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Check if there's nothing to commit
		if strings.Contains(stderr.String(), "nothing to commit") {
			return fmt.Errorf("nothing to commit")
		}
		return fmt.Errorf("failed to create commit: %w", err)
	}

	return nil
}

// FindRepoRoot finds the git repository root starting from the given directory
func FindRepoRoot(startDir string) (string, error) {
	dir := filepath.Clean(startDir)

	for {
		if _, err := exec.Command("git", "-C", dir, "rev-parse", "--git-dir").Output(); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			return "", fmt.Errorf("not a git repository")
		}
		dir = parent
	}
}
