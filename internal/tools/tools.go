package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/applier"
	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/search"
)

// MCPCaller routes mcp:<server>:<tool> calls to the appropriate MCP server.
type MCPCaller interface {
	Call(ctx context.Context, prefixedName string, input json.RawMessage) (json.RawMessage, error)
}

// Runner executes vNext tools inside a workspace (no network tools).
type Runner struct {
	workspaceRoot string
	excludeDirs   []string

	// Defaults for exec.run safety contract.
	execTimeout     time.Duration
	execOutputLimit int // bytes, combined stdout+stderr

	mcpCaller MCPCaller

	ckgStore    *ckg.Store
	ckgProvider *ckg.Provider
}

type RunnerOptions struct {
	ExcludeDirs []string

	ExecTimeout     time.Duration
	ExecOutputLimit int // bytes, combined stdout+stderr
}

func NewRunner(workspaceRoot string, opts RunnerOptions) (*Runner, error) {
	if strings.TrimSpace(workspaceRoot) == "" {
		return nil, fmt.Errorf("workspaceRoot is empty")
	}
	rootAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("abs workspaceRoot: %w", err)
	}

	exclude := append([]string(nil), opts.ExcludeDirs...)
	if len(exclude) == 0 {
		exclude = []string{".git", "node_modules", "dist", "build", ".orchestra"}
	}

	timeout := opts.ExecTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	limit := opts.ExecOutputLimit
	if limit <= 0 {
		limit = 100 * 1024
	}

	orchDir := filepath.Join(rootAbs, ".orchestra")
	if err := os.MkdirAll(orchDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir .orchestra: %w", err)
	}
	dbPath := filepath.Join(orchDir, "ckg.db")
	store, err := ckg.NewStore("file:" + dbPath + "?cache=shared")
	if err != nil {
		return nil, fmt.Errorf("open ckg store: %w", err)
	}
	provider := ckg.NewProvider(store, rootAbs)

	return &Runner{
		workspaceRoot:   rootAbs,
		excludeDirs:     exclude,
		execTimeout:     timeout,
		execOutputLimit: limit,
		ckgStore:        store,
		ckgProvider:     provider,
	}, nil
}

func (r *Runner) WorkspaceRoot() string { return r.workspaceRoot }

// FetchCKGContext returns a <ckg_context> block of up to 12 nodes relevant to
// the query, or an empty string if the CKG store is unavailable or has no matches.
func (r *Runner) FetchCKGContext(ctx context.Context, query string) string {
	if r.ckgStore == nil {
		return ""
	}
	nodes, err := r.ckgStore.FindRelevantNodes(ctx, query, 12)
	if err != nil || len(nodes) == 0 {
		return ""
	}
	return ckg.FormatNodesForPrompt(nodes, 800)
}

// Close releases resources held by the Runner (CKG store, etc).
// Safe to call multiple times.
func (r *Runner) Close() error {
	if r.ckgStore != nil {
		err := r.ckgStore.Close()
		r.ckgStore = nil
		r.ckgProvider = nil
		return err
	}
	return nil
}

// SetMCPCaller registers an MCP manager for routing mcp:* tool calls.
func (r *Runner) SetMCPCaller(caller MCPCaller) { r.mcpCaller = caller }

// --- fs.list ---

type FSListRequest struct {
	// Path is a relative path inside the workspace to list from (default: ".").
	// Kept for vNext MVP compatibility with docs/task_v4.md.
	Path string `json:"path,omitempty"`
	// Recursive controls whether listing is recursive (default: true).
	Recursive *bool `json:"recursive,omitempty"`
	// MaxEntries is an alias for Limit (default: unlimited).
	MaxEntries int `json:"max_entries,omitempty"`

	ExcludeDirs []string `json:"exclude_dirs,omitempty"`
	IncludeHash bool     `json:"include_hash,omitempty"`
	Limit       int      `json:"limit,omitempty"`
	SkipBackups *bool    `json:"skip_backups,omitempty"`
}

type FSFileMeta struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	MTime    int64  `json:"mtime"` // unix seconds (not nanoseconds) for better JSON compatibility
	FileHash string `json:"file_hash,omitempty"`
}

type FSListResponse struct {
	Files []FSFileMeta `json:"files"`
}

func (r *Runner) FSList(ctx context.Context, req FSListRequest) (*FSListResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
	}
	_ = ctx // no cancellation support in filepath.WalkDir yet; keep API consistent

	exclude := r.excludeDirs
	if len(req.ExcludeDirs) > 0 {
		exclude = req.ExcludeDirs
	}
	skipBackups := true
	if req.SkipBackups != nil {
		skipBackups = *req.SkipBackups
	}

	listPath := strings.TrimSpace(req.Path)
	if listPath == "" {
		listPath = "."
	}
	startAbs, _, err := resolveWorkspacePath(r.workspaceRoot, listPath)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(startAbs)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is not a directory", map[string]any{
			"path": listPath,
		})
	}

	recursive := true
	if req.Recursive != nil {
		recursive = *req.Recursive
	}
	limit := req.Limit
	if req.MaxEntries > 0 {
		limit = req.MaxEntries
	}

	files, err := listFiles(r.workspaceRoot, startAbs, exclude, skipBackups, req.IncludeHash, recursive, limit)
	if err != nil {
		return nil, err
	}
	return &FSListResponse{Files: files}, nil
}

// --- fs.read ---

type FSReadRequest struct {
	Path     string `json:"path"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

type FSReadResponse struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	SHA256   string `json:"sha256"`
	FileHash string `json:"file_hash"` // legacy alias (same as sha256)

	MTimeUnix int64 `json:"mtime_unix"`
	Size      int64 `json:"size"`
	Truncated bool  `json:"truncated"`
}

func (r *Runner) FSRead(ctx context.Context, req FSReadRequest) (*FSReadResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
	}
	_ = ctx

	path := strings.TrimSpace(req.Path)
	if path == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	// projectfs.ReadFile enforces traversal protection, but reads whole file. We need streaming + hash.
	absPath, relSlash, err := resolveWorkspacePath(r.workspaceRoot, path)
	if err != nil {
		return nil, err
	}

	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 200 * 1024
	}

	content, size, mtimeUnix, hash, truncated, err := readFileWithHash(absPath, maxBytes)
	if err != nil {
		return nil, err
	}

	return &FSReadResponse{
		Path:      relSlash,
		Content:   content,
		SHA256:    hash,
		FileHash:  hash,
		MTimeUnix: mtimeUnix,
		Size:      size,
		Truncated: truncated,
	}, nil
}

// --- fs.apply_ops ---

type FSApplyOpsRequest struct {
	Ops    []ops.AnyOp `json:"ops"`
	DryRun bool        `json:"dry_run,omitempty"`
	Backup bool        `json:"backup,omitempty"`
}

type FSApplyOpsResponse struct {
	Diffs        []applier.FileDiff `json:"diffs"`
	ChangedFiles []string           `json:"changed_files"`
	Applied      bool               `json:"applied"`
}

func (r *Runner) FSApplyOps(ctx context.Context, req FSApplyOpsRequest) (*FSApplyOpsResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
	}
	_ = ctx

	res, err := applier.ApplyAnyOps(r.workspaceRoot, req.Ops, applier.ApplyOptions{
		DryRun:       req.DryRun,
		Backup:       req.Backup,
		BackupSuffix: ".orchestra.bak",
	})
	if err != nil {
		return nil, err
	}
	return &FSApplyOpsResponse{
		Diffs:        res.Diffs,
		ChangedFiles: res.ChangedFiles,
		Applied:      !req.DryRun,
	}, nil
}

// --- search.text ---

type SearchTextRequest struct {
	Query string `json:"query"`
	// Paths optionally scopes search to these relative paths (files or directories) within the workspace.
	Paths []string `json:"paths,omitempty"`
	// MaxMatches optionally caps total matches returned across all files.
	MaxMatches int `json:"max_matches,omitempty"`

	ExcludeDirs []string          `json:"exclude_dirs,omitempty"`
	Options     SearchTextOptions `json:"options,omitempty"`
}

type SearchTextOptions struct {
	MaxMatchesPerFile int  `json:"max_matches_per_file,omitempty"`
	CaseInsensitive   bool `json:"case_insensitive,omitempty"`
	ContextLines      int  `json:"context_lines,omitempty"`
}

type SearchTextMatch struct {
	Path          string   `json:"path"`
	Line          int      `json:"line"`
	LineText      string   `json:"line_text"`
	ContextBefore []string `json:"context_before"`
	ContextAfter  []string `json:"context_after"`
}

type SearchTextResponse struct {
	Matches []SearchTextMatch `json:"matches"`
}

func (r *Runner) SearchText(ctx context.Context, req SearchTextRequest) (*SearchTextResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
	}
	_ = ctx

	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "query cannot be empty", nil)
	}

	exclude := r.excludeDirs
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
	if len(req.Paths) == 0 {
		m, err := search.SearchInProject(r.workspaceRoot, query, exclude, opts)
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
			abs, _, err := resolveWorkspacePath(r.workspaceRoot, p)
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
			// File scope: search only within this file.
			b, err := os.ReadFile(abs)
			if err != nil {
				return nil, err
			}
			matches = append(matches, searchInSingleFile(abs, string(b), query, queryLower, opts)...)
		}
	}

	out := make([]SearchTextMatch, 0, len(matches))
	for _, m := range matches {
		rel, relErr := filepath.Rel(r.workspaceRoot, m.FilePath)
		if relErr != nil {
			rel = m.FilePath
		}
		rel = filepath.ToSlash(rel)
		out = append(out, SearchTextMatch{
			Path:          rel,
			Line:          m.Line,
			LineText:      m.LineText,
			ContextBefore: m.ContextBefore,
			ContextAfter:  m.ContextAfter,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Line < out[j].Line
	})
	if req.MaxMatches > 0 && len(out) > req.MaxMatches {
		out = out[:req.MaxMatches]
	}

	return &SearchTextResponse{Matches: out}, nil
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
				Line:          i + 1, // 1-indexed
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

// --- code.symbols ---
// Implemented in todo #5 (Tree-sitter + graceful degradation).
// For now: Tier 3 (empty result).

type CodeSymbolsRequest struct {
	Path string `json:"path"`
}

type Symbol struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Optional location in file.
	Range *ops.Range `json:"range,omitempty"`
}

type CodeSymbolsResponse struct {
	Symbols []Symbol `json:"symbols"`
}

// --- exec.run ---

type ExecRunRequest struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Workdir string   `json:"workdir,omitempty"` // relative to workspace root

	TimeoutMS     int `json:"timeout_ms,omitempty"`
	OutputLimitKB int `json:"output_limit_kb,omitempty"`
}

type ExecRunResponse struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMS int64  `json:"duration_ms"`
	Truncated  bool   `json:"truncated"`
}

func (r *Runner) ExecRun(ctx context.Context, req ExecRunRequest) (*ExecRunResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
	}
	// Implementation in exec.go
	return runExec(ctx, r.workspaceRoot, r.execTimeout, r.execOutputLimit, req)
}

// --- helpers ---

// resolveWorkspacePath resolves a relative path within workspace with realpath-based security checks.
// Returns absolute path, relative path (with forward slashes), and error.
func resolveWorkspacePath(workspaceRoot, p string) (abs string, relSlash string, _ error) {
	// Normalize input path
	p = strings.TrimSpace(p)
	if p == "" {
		return "", "", protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}

	// Build absolute path from workspace root
	abs = filepath.Join(workspaceRoot, filepath.FromSlash(p))
	abs = filepath.Clean(abs)

	// Step 1: Lexical check - ensure path doesn't escape via ".."
	rel, err := filepath.Rel(workspaceRoot, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", "", protocol.NewError(protocol.PathTraversal, "path escapes workspace", map[string]any{
			"path": p,
		})
	}

	// Step 2: Get realpath of workspace root (resolves junctions/symlinks)
	rootAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", "", fmt.Errorf("failed to get absolute workspace root: %w", err)
	}
	rootReal := rootAbs
	if rp, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootReal = rp
	} else {
		// If workspace root itself is a broken symlink, that's a configuration error
		// But we continue with rootAbs as fallback
	}

	// Step 3: Get absolute path and resolve symlinks/junctions
	absAbs, err := filepath.Abs(abs)
	if err != nil {
		return "", "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Step 4: Check if target exists and resolve realpath
	if realAbs, err := filepath.EvalSymlinks(absAbs); err == nil {
		// Target exists - check if realpath is within workspace realpath
		if !isWithinRoot(rootReal, realAbs) {
			return "", "", protocol.NewError(protocol.PathTraversal, "path escapes workspace (via symlink/junction)", map[string]any{
				"path":           p,
				"abs_path":       absAbs,
				"real_path":      realAbs,
				"workspace":      rootAbs,
				"workspace_real": rootReal,
			})
		}
	} else if os.IsNotExist(err) {
		// Target doesn't exist - validate the deepest existing parent directory
		dir := absAbs
		for {
			parentDir := filepath.Dir(dir)
			if parentDir == dir || parentDir == "." || parentDir == string(os.PathSeparator) {
				// Reached filesystem root - allow (path is valid, just doesn't exist yet)
				break
			}
			if samePath(parentDir, rootAbs) {
				// Reached workspace root - allow
				break
			}
			// Check if this directory exists
			if st, statErr := os.Stat(parentDir); statErr == nil && st.IsDir() {
				// Found existing parent - check its realpath
				if realDir, eerr := filepath.EvalSymlinks(parentDir); eerr == nil {
					if !isWithinRoot(rootReal, realDir) {
						return "", "", protocol.NewError(protocol.PathTraversal, "path escapes workspace (via symlink/junction in parent)", map[string]any{
							"path":      p,
							"parent":    parentDir,
							"real_path": realDir,
						})
					}
				}
				// Found valid parent - allow
				break
			}
			dir = parentDir
		}
	} else {
		// Error resolving symlinks (not "not exists") - be conservative and reject
		return "", "", protocol.NewError(protocol.PathTraversal, "cannot resolve path (symlink/junction)", map[string]any{
			"path":  p,
			"error": err.Error(),
		})
	}

	relSlash = filepath.ToSlash(rel)
	return absAbs, relSlash, nil
}

// isWithinRoot checks if targetAbs is within rootAbs using realpath comparison.
// On Windows, handles case-insensitive comparison and extended paths (\\?\ prefix).
func isWithinRoot(rootAbs, targetAbs string) bool {
	// Normalize both paths
	r := filepath.Clean(rootAbs)
	t := filepath.Clean(targetAbs)

	// Handle Windows extended paths (\\?\ prefix)
	if runtime.GOOS == "windows" {
		// Remove \\?\ prefix if present for comparison
		r = strings.TrimPrefix(r, `\\?\`)
		t = strings.TrimPrefix(t, `\\?\`)
		// Case-insensitive comparison on Windows
		r = strings.ToLower(r)
		t = strings.ToLower(t)
	}

	// Exact match
	if r == t {
		return true
	}

	// Ensure root ends with separator for prefix check
	sep := string(os.PathSeparator)
	if !strings.HasSuffix(r, sep) {
		r += sep
	}

	// Check if target starts with root + separator
	// This prevents false positives like "C:\repo2" matching "C:\repo"
	return strings.HasPrefix(t, r)
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
