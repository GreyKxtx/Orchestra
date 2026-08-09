package fs

import (
	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/patch/applier"
	"github.com/orchestra/orchestra/patch/ops"
)

// ToolDiagnostic is re-exported for hooks/responses without requiring importers to use internal/lsp.
type ToolDiagnostic = lsp.ToolDiagnostic

// FSFileMeta describes one file in list/glob results.
type FSFileMeta struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	MTime    int64  `json:"mtime"`
	FileHash string `json:"file_hash,omitempty"`
}

type FSListRequest struct {
	Path        string `json:"path,omitempty"`
	Recursive   *bool  `json:"recursive,omitempty"`
	MaxEntries  int    `json:"max_entries,omitempty"`
	ExcludeDirs []string `json:"exclude_dirs,omitempty"`
	IncludeHash bool   `json:"include_hash,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	SkipBackups *bool  `json:"skip_backups,omitempty"`
}

type FSListResponse struct {
	Files []FSFileMeta `json:"files"`
}

type FSReadRequest struct {
	Path     string `json:"path"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

type FSReadResponse struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	SHA256    string `json:"sha256"`
	FileHash  string `json:"file_hash"`
	MTimeUnix int64  `json:"mtime_unix"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
}

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

type FSGlobRequest struct {
	Pattern     string   `json:"pattern"`
	Limit       int      `json:"limit,omitempty"`
	IncludeHash bool     `json:"include_hash,omitempty"`
	ExcludeDirs []string `json:"exclude_dirs,omitempty"`
}

type FSGlobResponse struct {
	Files   []FSFileMeta `json:"files"`
	Pattern string       `json:"pattern"`
}

type FSWriteRequest struct {
	Path         string `json:"path"`
	Content      string `json:"content"`
	FileHash     string `json:"file_hash,omitempty"`
	MustNotExist bool   `json:"must_not_exist,omitempty"`
	Backup       bool   `json:"backup,omitempty"`
}

type FSWriteResponse struct {
	Path               string          `json:"path"`
	FileHash           string          `json:"file_hash"`
	BytesWritten       int             `json:"bytes_written"`
	Diagnostics        []ToolDiagnostic `json:"diagnostics,omitempty"`
	DiagnosticsPending bool            `json:"diagnostics_pending,omitempty"`
}

type FSEditRequest struct {
	Path         string `json:"path"`
	Search       string `json:"search"`
	Replace      string `json:"replace"`
	FileHash     string `json:"file_hash,omitempty"`
	TargetSymbol string `json:"target_symbol,omitempty"`
	Backup       bool   `json:"backup,omitempty"`
}

type FSEditResponse struct {
	Path               string          `json:"path"`
	FileHash           string          `json:"file_hash"`
	Diagnostics        []ToolDiagnostic `json:"diagnostics,omitempty"`
	DiagnosticsPending bool            `json:"diagnostics_pending,omitempty"`
}

type FSDeleteRequest struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive,omitempty"`
}

type FSDeleteResponse struct {
	Path string `json:"path"`
}

type FSRenameRequest struct {
	Path    string `json:"path"`
	NewPath string `json:"new_path"`
}

type FSRenameResponse struct {
	Path    string `json:"path"`
	NewPath string `json:"new_path"`
}

type FSPreviewRequest struct {
	Path    string `json:"path"`
	Search  string `json:"search"`
	Replace string `json:"replace"`
}

type FSPreviewResponse struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
}

type ASTRenameRequest struct {
	Path    string `json:"path"`
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
}

type ASTRenameResponse struct {
	Path         string `json:"path"`
	Count        int    `json:"count"`
	Sites        []int  `json:"sites,omitempty"`
	SkippedSites []int  `json:"skipped_sites,omitempty"`
	Wrote        bool   `json:"wrote"`
	NewHash      string `json:"new_file_hash,omitempty"`
}

type SearchTextRequest struct {
	Query       string            `json:"query"`
	Paths       []string          `json:"paths,omitempty"`
	MaxMatches  int               `json:"max_matches,omitempty"`
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
	SymbolFQN     string   `json:"symbol_fqn,omitempty"`
}

type SearchTextResponse struct {
	Matches []SearchTextMatch `json:"matches"`
}
