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
		ContextLines:      3,
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

		// Skip backup files and known binary/generated extensions.
		if strings.HasSuffix(path, ".orchestra.bak") || isBinaryExt(filepath.Ext(path)) {
			return nil
		}

		// Skip large files (> 1 MB) — binaries and generated assets.
		if info.Size() > 1<<20 {
			return nil
		}

		// Read file
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip files we can't read
		}

		// Skip files whose first 512 bytes contain a null byte — binary content.
		if hasBinaryContent(data) {
			return nil
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

// isBinaryExt reports whether the file extension belongs to a binary or
// generated file that should never be text-searched.
func isBinaryExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".exe", ".dll", ".so", ".dylib", ".a", ".o", ".obj",
		".bin", ".dat", ".db", ".sqlite", ".sqlite3",
		".zip", ".tar", ".gz", ".bz2", ".xz", ".7z", ".rar",
		".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".webp",
		".pdf", ".doc", ".docx", ".xls", ".xlsx",
		".wasm", ".class", ".pyc", ".pyo":
		return true
	}
	return false
}

// hasBinaryContent reports whether the first 512 bytes of data contain a null byte,
// which is a reliable indicator of binary (non-text) content.
func hasBinaryContent(data []byte) bool {
	check := data
	if len(check) > 512 {
		check = check[:512]
	}
	for _, b := range check {
		if b == 0 {
			return true
		}
	}
	return false
}
