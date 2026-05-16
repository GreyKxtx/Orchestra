package search

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	rgOnce  sync.Once
	rgFound bool
	rgBin   string
)

// HasRipgrep reports whether rg is available in PATH.
// The result is cached after the first call.
func HasRipgrep() bool {
	rgOnce.Do(func() {
		p, err := exec.LookPath("rg")
		if err == nil {
			rgBin = p
			rgFound = true
		}
	})
	return rgFound
}

// rgJSONLine is one NDJSON line from `rg --json`.
type rgJSONLine struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// rgJSONData is the `data` field for `match` and `context` types.
type rgJSONData struct {
	Path struct {
		Text string `json:"text"`
	} `json:"path"`
	LineNumber int `json:"line_number"`
	Lines      struct {
		Text string `json:"text"`
	} `json:"lines"`
}

// rawEntry is an intermediate representation while parsing rg JSON output.
type rawEntry struct {
	path    string
	lineNum int
	text    string
	isMatch bool
}

// SearchWithRipgrep runs rg and returns Match results compatible with SearchInProject.
// scopePaths are absolute paths to scope the search to (files or dirs).
// Pass nil to search the entire root.
func SearchWithRipgrep(root, query string, excludeDirs []string, opts Options, scopePaths []string) ([]Match, error) {
	if !HasRipgrep() {
		return nil, fmt.Errorf("ripgrep not available")
	}
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	args := []string{"--json"}
	if opts.CaseInsensitive {
		args = append(args, "-i")
	}
	if opts.ContextLines > 0 {
		args = append(args, fmt.Sprintf("-C%d", opts.ContextLines))
	}
	if opts.MaxMatchesPerFile > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", opts.MaxMatchesPerFile))
	}
	for _, dir := range excludeDirs {
		args = append(args, "--glob=!**/"+dir+"/**")
		args = append(args, "--glob=!"+dir)
	}
	args = append(args, "--", query)
	if len(scopePaths) > 0 {
		args = append(args, scopePaths...)
	} else {
		args = append(args, root)
	}

	cmd := exec.Command(rgBin, args...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil // exit 1 = no matches found, not an error
		}
		return nil, fmt.Errorf("ripgrep: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return parseRipgrepJSON(root, stdout.Bytes(), opts.ContextLines), nil
}

// parseRipgrepJSON parses the NDJSON stream from `rg --json` and converts it
// into Match values. It is a pure function (no I/O) so it is easy to test.
func parseRipgrepJSON(root string, data []byte, contextLines int) []Match {
	var entries []rawEntry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 2<<20), 2<<20)

	for scanner.Scan() {
		var line rgJSONLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Type != "match" && line.Type != "context" {
			continue
		}
		var d rgJSONData
		if err := json.Unmarshal(line.Data, &d); err != nil {
			continue
		}
		p := d.Path.Text
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, filepath.FromSlash(p))
		}
		entries = append(entries, rawEntry{
			path:    p,
			lineNum: d.LineNumber,
			text:    strings.TrimRight(d.Lines.Text, "\r\n"),
			isMatch: line.Type == "match",
		})
	}

	var matches []Match
	for i, e := range entries {
		if !e.isMatch {
			continue
		}

		var before []string
		for j := i - 1; j >= 0 && len(before) < contextLines; j-- {
			if entries[j].path != e.path {
				break
			}
			before = append([]string{entries[j].text}, before...)
		}

		var after []string
		for j := i + 1; j < len(entries) && len(after) < contextLines; j++ {
			if entries[j].path != e.path {
				break
			}
			after = append(after, entries[j].text)
		}

		matches = append(matches, Match{
			FilePath:      e.path,
			Line:          e.lineNum,
			LineText:      e.text,
			ContextBefore: before,
			ContextAfter:  after,
		})
	}

	return matches
}
