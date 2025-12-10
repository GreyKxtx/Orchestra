package search

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Match represents a search match in a file
type Match struct {
	FilePath      string
	Line          int
	LineText      string
	ContextBefore []string
	ContextAfter  []string
}

// Options contains search options
type Options struct {
	MaxMatchesPerFile int
	CaseInsensitive   bool
	ContextLines      int // Number of context lines before/after
}

// DefaultOptions returns default search options
func DefaultOptions() Options {
	return Options{
		MaxMatchesPerFile: 10,
		CaseInsensitive:   false,
		ContextLines:      2,
	}
}

// SearchInProject searches for query in project files
func SearchInProject(root string, query string, excludeDirs []string, opts Options) ([]Match, error) {
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	excludeMap := make(map[string]bool)
	for _, dir := range excludeDirs {
		excludeMap[dir] = true
	}

	var matches []Match
	queryLower := strings.ToLower(query)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't read
		}

		// Skip directories
		if info.IsDir() {
			relPath, _ := filepath.Rel(root, path)
			dirName := filepath.Base(path)
			if excludeMap[dirName] || excludeMap[relPath] {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip backup files
		if strings.HasSuffix(path, ".orchestra.bak") {
			return nil
		}

		// Read file
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip files we can't read
		}

		// Search in file
		fileMatches := searchInFile(path, string(data), query, queryLower, opts)
		matches = append(matches, fileMatches...)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk project: %w", err)
	}

	return matches, nil
}

func searchInFile(filePath string, content string, query string, queryLower string, opts Options) []Match {
	var matches []Match
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		// Check if we've exceeded max matches per file
		if len(matches) >= opts.MaxMatchesPerFile {
			break
		}

		// Check if line contains query
		lineToSearch := line
		if opts.CaseInsensitive {
			lineToSearch = strings.ToLower(line)
		}

		var searchQuery string
		if opts.CaseInsensitive {
			searchQuery = queryLower
		} else {
			searchQuery = query
		}

		if strings.Contains(lineToSearch, searchQuery) {
			// Collect context
			contextBefore := collectContext(lines, i, opts.ContextLines, true)
			contextAfter := collectContext(lines, i, opts.ContextLines, false)

			matches = append(matches, Match{
				FilePath:      filePath,
				Line:          i + 1, // 1-indexed
				LineText:      strings.TrimRight(line, "\r\n"),
				ContextBefore: contextBefore,
				ContextAfter:  contextAfter,
			})
		}
	}

	return matches
}

func collectContext(lines []string, currentLine int, contextLines int, before bool) []string {
	var context []string
	start := currentLine - contextLines
	end := currentLine

	if before {
		if start < 0 {
			start = 0
		}
		for i := start; i < end; i++ {
			context = append(context, strings.TrimRight(lines[i], "\r\n"))
		}
	} else {
		start = currentLine + 1
		end = currentLine + 1 + contextLines
		if end > len(lines) {
			end = len(lines)
		}
		for i := start; i < end; i++ {
			context = append(context, strings.TrimRight(lines[i], "\r\n"))
		}
	}

	return context
}
