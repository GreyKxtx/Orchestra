package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/search"
	"github.com/orchestra/orchestra/protocol"
)

const defaultGrepMaxMatches = 200

func (c *Client) SearchText(ctx context.Context, req SearchTextRequest) (*SearchTextResponse, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "client is nil", nil)
	}
	_ = ctx

	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "query cannot be empty", nil)
	}

	exclude := c.ExcludeDirs
	if len(req.ExcludeDirs) > 0 {
		exclude = req.ExcludeDirs
	}

	opts := search.DefaultOptions()
	if req.Options.MaxMatchesPerFile > 0 {
		opts.MaxMatchesPerFile = req.Options.MaxMatchesPerFile
	}
	opts.CaseInsensitive = req.Options.CaseInsensitive
	if req.Options.ContextLines >= 0 {
		opts.ContextLines = req.Options.ContextLines
	}

	var matches []search.Match
	if search.HasRipgrep() {
		var scopePaths []string
		for _, p := range req.Paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			abs, _, err := resolveWorkspacePath(c.Root, p)
			if err != nil {
				return nil, err
			}
			scopePaths = append(scopePaths, abs)
		}
		m, err := search.SearchWithRipgrep(c.Root, query, exclude, opts, scopePaths)
		if err != nil {
			return nil, err
		}
		matches = m
	} else if len(req.Paths) == 0 {
		m, err := search.SearchInProject(c.Root, query, exclude, opts)
		if err != nil {
			return nil, err
		}
		matches = append(matches, m...)
	} else {
		queryLower := strings.ToLower(query)
		for _, p := range req.Paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			abs, _, err := resolveWorkspacePath(c.Root, p)
			if err != nil {
				return nil, err
			}
			st, err := os.Stat(abs)
			if err != nil {
				return nil, err
			}
			if st.IsDir() {
				m, err := search.SearchInProject(abs, query, exclude, opts)
				if err != nil {
					return nil, err
				}
				matches = append(matches, m...)
				continue
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return nil, err
			}
			matches = append(matches, searchInSingleFile(abs, string(b), query, queryLower, opts)...)
		}
	}

	out := make([]SearchTextMatch, 0, len(matches))
	for _, m := range matches {
		rel, relErr := filepath.Rel(c.Root, m.FilePath)
		if relErr != nil {
			rel = m.FilePath
		}
		rel = filepath.ToSlash(rel)
		sm := SearchTextMatch{
			Path:          rel,
			Line:          m.Line,
			LineText:      m.LineText,
			ContextBefore: m.ContextBefore,
			ContextAfter:  m.ContextAfter,
		}
		if strings.HasSuffix(rel, ".go") && c.Hooks.SymbolFQNAtLine != nil {
			if fqn := c.Hooks.SymbolFQNAtLine(ctx, rel, m.Line); fqn != "" {
				sm.SymbolFQN = fqn
			}
		}
		out = append(out, sm)
	}

	maxOut := req.MaxMatches
	if maxOut <= 0 {
		maxOut = defaultGrepMaxMatches
	}

	mtimeCache := make(map[string]time.Time)
	for _, m := range out {
		if _, ok := mtimeCache[m.Path]; ok {
			continue
		}
		abs := filepath.Join(c.Root, filepath.FromSlash(m.Path))
		if st, err := os.Stat(abs); err == nil {
			mtimeCache[m.Path] = st.ModTime()
		}
	}

	sort.Slice(out, func(i, j int) bool {
		ti, tj := mtimeCache[out[i].Path], mtimeCache[out[j].Path]
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Line < out[j].Line
	})
	if len(out) > maxOut {
		out = out[:maxOut]
	}

	return &SearchTextResponse{Matches: out}, nil
}

// FormatSearchResults renders grep output as human-readable text.
func FormatSearchResults(query string, resp *SearchTextResponse) string {
	if resp == nil || len(resp.Matches) == 0 {
		return fmt.Sprintf("Совпадений для %q в проекте не найдено (исчерпывающий поиск).", query)
	}

	var sb strings.Builder
	n := len(resp.Matches)
	sb.WriteString(fmt.Sprintf("Найдено %d совпадени", n))
	switch {
	case n == 1:
		sb.WriteString("е")
	case n < 5:
		sb.WriteString("я")
	default:
		sb.WriteString("й")
	}
	sb.WriteString(fmt.Sprintf(" для %q (исчерпывающий поиск по всему проекту):\n", query))

	for _, m := range resp.Matches {
		sb.WriteString("\n")

		kind := classifyMatch(m.Path, m.LineText, query, m.SymbolFQN)

		fqn := m.SymbolFQN
		if idx := strings.LastIndex(fqn, "/"); idx >= 0 {
			fqn = fqn[idx+1:]
		}

		if fqn != "" {
			sb.WriteString(fmt.Sprintf("%s:%d  [в: %s]  %s\n", m.Path, m.Line, fqn, kind))
		} else {
			sb.WriteString(fmt.Sprintf("%s:%d  %s\n", m.Path, m.Line, kind))
		}
		for _, line := range m.ContextBefore {
			sb.WriteString("    " + line + "\n")
		}
		sb.WriteString(">   " + m.LineText + "\n")
		for _, line := range m.ContextAfter {
			sb.WriteString("    " + line + "\n")
		}
	}
	return sb.String()
}

func classifyMatch(filePath, lineText, query, symbolFQN string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".md", ".txt", ".rst", ".adoc", ".json", ".yaml", ".yml", ".toml":
		return "[документация/конфиг]"
	}

	trimmed := strings.TrimSpace(lineText)
	if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
		return "[комментарий]"
	}
	if strings.Contains(trimmed, "func ") && strings.Contains(trimmed, query) {
		return "[определение]"
	}
	if symbolFQN != "" {
		lastSeg := symbolFQN
		if idx := strings.LastIndex(symbolFQN, "/"); idx >= 0 {
			lastSeg = symbolFQN[idx+1:]
		}
		if strings.HasSuffix(lastSeg, "."+query) || lastSeg == query {
			return "[тело определения]"
		}
	}
	return "[вызов]"
}

func searchInSingleFile(filePath string, content string, query string, queryLower string, opts search.Options) []search.Match {
	var matches []search.Match
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		if len(matches) >= opts.MaxMatchesPerFile {
			break
		}

		lineToSearch := line
		searchQuery := query
		if opts.CaseInsensitive {
			lineToSearch = strings.ToLower(line)
			searchQuery = queryLower
		}

		if strings.Contains(lineToSearch, searchQuery) {
			contextBefore := collectContextLines(lines, i, opts.ContextLines, true)
			contextAfter := collectContextLines(lines, i, opts.ContextLines, false)
			matches = append(matches, search.Match{
				FilePath:      filePath,
				Line:          i + 1,
				LineText:      strings.TrimRight(line, "\r\n"),
				ContextBefore: contextBefore,
				ContextAfter:  contextAfter,
			})
		}
	}

	return matches
}

func collectContextLines(lines []string, currentLine int, contextLines int, before bool) []string {
	var ctx []string
	start := currentLine - contextLines
	end := currentLine

	if before {
		if start < 0 {
			start = 0
		}
		for i := start; i < end; i++ {
			ctx = append(ctx, strings.TrimRight(lines[i], "\r\n"))
		}
		return ctx
	}

	start = currentLine + 1
	end = currentLine + 1 + contextLines
	if end > len(lines) {
		end = len(lines)
	}
	for i := start; i < end; i++ {
		ctx = append(ctx, strings.TrimRight(lines[i], "\r\n"))
	}
	return ctx
}
