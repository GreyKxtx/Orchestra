package applier

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/parser"
)

// Protocol for OpReplaceBlock:
//
// 1. Empty oldBlock (after trimming whitespace):
//    - Explicit signal from LLM to append new code to end of file
//    - This is NOT a fallback, but an intentional operation type
//    - Behavior: append newBlock to the end of file
//
// 2. Non-empty oldBlock:
//    - Must find exact match in file (or normalized match for full file replacement)
//    - If not found: return error (don't try to guess LLM's intent)
//    - Behavior: replace first occurrence of oldBlock with newBlock
//
// This protocol is documented in the prompt sent to LLM (see context/builder.go)

// ApplyOptions contains options for applying changes
type ApplyOptions struct {
	DryRun       bool
	Backup       bool
	BackupSuffix string // ".orchestra.bak"
}

// FileDiff represents a diff for a single file
type FileDiff struct {
	Path   string
	Before string
	After  string
}

// ApplyResult contains the result of applying changes
type ApplyResult struct {
	Diffs []FileDiff
}

// ApplyChanges applies file changes to the filesystem
func ApplyChanges(root string, changes []parser.FileChange, opts ApplyOptions) (*ApplyResult, error) {
	result := &ApplyResult{
		Diffs: make([]FileDiff, 0, len(changes)),
	}

	for _, change := range changes {
		diff, err := applyFileChange(root, change, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to apply change to %s: %w", change.Path, err)
		}
		result.Diffs = append(result.Diffs, *diff)
	}

	return result, nil
}

func applyFileChange(root string, change parser.FileChange, opts ApplyOptions) (*FileDiff, error) {
	filePath := filepath.Join(root, change.Path)

	// Normalize path separators
	filePath = filepath.Clean(filePath)

	// Security: ensure path is within root
	relPath, err := filepath.Rel(root, filePath)
	if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return nil, fmt.Errorf("invalid file path: %s", change.Path)
	}

	var before, after string

	// Read existing file if it exists
	if _, err := os.Stat(filePath); err == nil {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}
		before = string(data)
	}

	// Apply operations
	after = before
	for _, op := range change.Operations {
		switch op.Type {
		case parser.OpReplaceBlock:
			// Protocol: empty oldBlock means "append new code to end of file"
			// This is an explicit signal from LLM, not a fallback
			if strings.TrimSpace(op.OldBlock) == "" {
				// Append mode: add new code to the end of file
				if after == "" {
					// File doesn't exist, just set to new block
					after = op.NewBlock
				} else {
					// Append to existing content
					after = strings.TrimRight(after, "\n\r") + "\n" + op.NewBlock
				}
				// Ensure result ends with newline
				if !strings.HasSuffix(after, "\n") {
					after += "\n"
				}
				continue
			}

			// Normalize whitespace for comparison (to handle trailing newlines)
			oldBlockNormalized := strings.TrimRight(op.OldBlock, " \t\n\r")
			afterNormalized := strings.TrimRight(after, " \t\n\r")

			// Try exact match first
			idx := strings.Index(after, op.OldBlock)
			if idx == -1 {
				// Check if old block matches entire file (normalized)
				// This handles cases where model returns full file with trailing whitespace differences
				if strings.TrimSpace(afterNormalized) == strings.TrimSpace(oldBlockNormalized) && oldBlockNormalized != "" {
					// Old block is the entire file, replace with new block
					after = op.NewBlock
					if !strings.HasSuffix(after, "\n") {
						after += "\n"
					}
				} else {
					// Old block not found - this is an error
					// Truncate large files/blocks in error message to avoid console spam
					oldBlockPreview := op.OldBlock
					fileContentPreview := after
					maxPreviewLen := 500

					if len(oldBlockPreview) > maxPreviewLen {
						oldBlockPreview = oldBlockPreview[:maxPreviewLen] + "\n... (truncated, " + fmt.Sprintf("%d", len(op.OldBlock)-maxPreviewLen) + " more chars)"
					}
					if len(fileContentPreview) > maxPreviewLen {
						fileContentPreview = fileContentPreview[:maxPreviewLen] + "\n... (truncated, " + fmt.Sprintf("%d", len(after)-maxPreviewLen) + " more chars)"
					}

					return nil, fmt.Errorf("old block not found in file.\nSearched for:\n%s\n\nFile content:\n%s", oldBlockPreview, fileContentPreview)
				}
			} else {
				// Replace first occurrence
				after = after[:idx] + op.NewBlock + after[idx+len(op.OldBlock):]
			}

		case parser.OpReplaceFile:
			after = op.NewFileContent
			// Ensure file ends with newline for consistency
			if !strings.HasSuffix(after, "\n") {
				after += "\n"
			}
		}
	}

	diff := &FileDiff{
		Path:   change.Path,
		Before: before,
		After:  after,
	}

	// Apply changes if not dry-run
	if !opts.DryRun {
		// Create backup if needed
		if opts.Backup && before != "" {
			backupPath := filePath + opts.BackupSuffix
			if err := os.WriteFile(backupPath, []byte(before), 0644); err != nil {
				return nil, fmt.Errorf("failed to create backup: %w", err)
			}
		}

		// Ensure directory exists
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory: %w", err)
		}

		// Write file
		if err := os.WriteFile(filePath, []byte(after), 0644); err != nil {
			return nil, fmt.Errorf("failed to write file: %w", err)
		}
	}

	return diff, nil
}
