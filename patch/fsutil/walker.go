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

// WalkOptions contains options for walking the project
type WalkOptions struct {
	ExcludeDirs []string // Directories to exclude (by name or relative path)
	SkipBackups bool     // Skip .orchestra.bak files (default: true)
}

// DefaultWalkOptions returns default walk options
func DefaultWalkOptions() WalkOptions {
	return WalkOptions{
		ExcludeDirs: []string{".git", "node_modules", "dist", "build", ".orchestra"},
		SkipBackups: true,
	}
}

// FileHandler is called for each file during walk
// Return error to stop walking, return nil to continue
type FileHandler func(info FileInfo) error

// WalkProject walks the project directory and calls handler for each file
func WalkProject(root string, opts WalkOptions, handler FileHandler) error {
	if handler == nil {
		return fmt.Errorf("handler cannot be nil")
	}

	excludeMap := make(map[string]bool)
	for _, dir := range opts.ExcludeDirs {
		excludeMap[dir] = true
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	err = filepath.Walk(rootAbs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't read
		}

		// Skip directories
		if info.IsDir() {
			relPath, _ := filepath.Rel(rootAbs, path)
			dirName := filepath.Base(path)
			if excludeMap[dirName] || excludeMap[relPath] {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip backup files if requested
		if opts.SkipBackups && strings.HasSuffix(path, ".orchestra.bak") {
			return nil
		}

		// Read file content
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip files we can't read
		}

		relPath, _ := filepath.Rel(rootAbs, path)

		fileInfo := FileInfo{
			Path:    path,
			RelPath: relPath,
			Content: string(data),
			Size:    info.Size(),
		}

		return handler(fileInfo)
	})

	if err != nil {
		return fmt.Errorf("failed to walk project: %w", err)
	}

	return nil
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
