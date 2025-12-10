package parser

import (
	"fmt"
	"strings"
)

// OperationType represents the type of file operation
type OperationType string

const (
	OpReplaceBlock OperationType = "replace_block"
	OpReplaceFile  OperationType = "replace_file"
)

// Operation represents a single operation on a file
type Operation struct {
	Type OperationType

	// For replace_block
	OldBlock string
	NewBlock string

	// For replace_file
	NewFileContent string
}

// FileChange represents a single file change operation
type FileChange struct {
	Path       string
	Operations []Operation
}

// ParsedResponse represents the parsed LLM response
type ParsedResponse struct {
	Files []FileChange
}

// Parse parses LLM response in Orchestra format
func Parse(raw string) (*ParsedResponse, error) {
	if raw == "" {
		return nil, fmt.Errorf("empty response")
	}

	var files []FileChange
	lines := strings.Split(raw, "\n")

	i := 0
	for i < len(lines) {
		// Look for ---FILE: marker
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "---FILE:") {
			i++
			continue
		}

		// Extract file path
		fileLine := strings.TrimSpace(lines[i])
		if len(fileLine) < 9 {
			return nil, fmt.Errorf("invalid ---FILE: marker at line %d", i+1)
		}
		filePath := strings.TrimSpace(fileLine[8:])
		if filePath == "" {
			return nil, fmt.Errorf("empty file path at line %d", i+1)
		}

		i++

		// Look for <<<BLOCK or content
		var oldBlock, newBlock string
		var hasOldBlock bool

		// Check if next line is <<<BLOCK
		if i < len(lines) && strings.TrimSpace(lines[i]) == "<<<BLOCK" {
			hasOldBlock = true
			i++

			// Collect old block until >>>BLOCK
			var oldLines []string
			for i < len(lines) && strings.TrimSpace(lines[i]) != ">>>BLOCK" {
				oldLines = append(oldLines, lines[i])
				i++
			}

			if i >= len(lines) {
				return nil, fmt.Errorf("unclosed <<<BLOCK for file %s", filePath)
			}

			oldBlock = strings.Join(oldLines, "\n")
			i++ // skip >>>BLOCK
		}

		// Collect new block until ---END
		var newLines []string
		for i < len(lines) && strings.TrimSpace(lines[i]) != "---END" {
			newLines = append(newLines, lines[i])
			i++
		}

		if i >= len(lines) {
			return nil, fmt.Errorf("unclosed ---FILE block for file %s", filePath)
		}

		newBlock = strings.Join(newLines, "\n")
		i++ // skip ---END

		// Determine operation type
		var op Operation
		if !hasOldBlock || oldBlock == "" {
			// No old block or empty -> replace_file
			op = Operation{
				Type:           OpReplaceFile,
				NewFileContent: strings.Trim(newBlock, "\n"),
			}
		} else {
			// Has old block -> replace_block
			op = Operation{
				Type:     OpReplaceBlock,
				OldBlock: strings.Trim(oldBlock, "\n"),
				NewBlock: strings.Trim(newBlock, "\n"),
			}
		}

		files = append(files, FileChange{
			Path:       filePath,
			Operations: []Operation{op},
		})
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no file changes found in response")
	}

	return &ParsedResponse{Files: files}, nil
}
