package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileInfo represents information about a project file
type FileInfo struct {
	Path    string // Absolute path
	RelPath string // Relative path from project root
	Content string // File content (if read)
	Size    int64  // File size in bytes
}

// ReadFile reads a single file from the project
func ReadFile(root string, relPath string) (FileInfo, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return FileInfo{}, fmt.Errorf("failed to get absolute path: %w", err)
	}

	absPath := filepath.Join(rootAbs, relPath)
	absPath = filepath.Clean(absPath)

	// Security: ensure path is within root
	rel, err := filepath.Rel(rootAbs, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return FileInfo{}, fmt.Errorf("invalid file path: %s", relPath)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return FileInfo{}, fmt.Errorf("failed to stat file: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return FileInfo{}, fmt.Errorf("failed to read file: %w", err)
	}

	return FileInfo{
		Path:    absPath,
		RelPath: relPath,
		Content: string(data),
		Size:    info.Size(),
	}, nil
}
