package daemon

import "time"

const (
	ProtocolVersion = 1
	DaemonVersion   = "0.3.0"
)

const (
	DefaultAddress = "127.0.0.1"
	DefaultPort    = 8080

	DefaultScanInterval      = 10 * time.Second
	DefaultMaxCacheFileBytes = int64(64 * 1024)
	DefaultMaxBytesPerFile   = int64(200 * 1024)
)

type HealthResponse struct {
	Status          string `json:"status"`
	DaemonVersion   string `json:"daemon_version"`
	ProtocolVersion int    `json:"protocol_version"`
	ProjectRoot     string `json:"project_root"`
	ProjectID       string `json:"project_id"`
}

type FileMeta struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"` // unix nanos
	Hash  string `json:"hash,omitempty"`
}

type ListFilesResponse struct {
	Files []FileMeta `json:"files"`
}

type ReadFileRequest struct {
	Path string `json:"path"`
}

type ReadFileResponse struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	MTime   int64  `json:"mtime"` // unix nanos
	Content string `json:"content"`
}

type SearchOptions struct {
	MaxMatchesPerFile int  `json:"max_matches_per_file"`
	CaseInsensitive   bool `json:"case_insensitive"`
	ContextLines      int  `json:"context_lines"`
}

type SearchRequest struct {
	Query   string        `json:"query"`
	Options SearchOptions `json:"options"`
}

type SearchMatch struct {
	Path          string   `json:"path"`
	Line          int      `json:"line"`
	LineText      string   `json:"line_text"`
	ContextBefore []string `json:"context_before"`
	ContextAfter  []string `json:"context_after"`
}

type SearchResponse struct {
	Matches []SearchMatch `json:"matches"`
}

type ContextLimits struct {
	MaxFiles        int   `json:"max_files"`
	MaxTotalBytes   int64 `json:"max_total_bytes"`
	MaxBytesPerFile int64 `json:"max_bytes_per_file"`
}

type ContextRequest struct {
	Query       string         `json:"query"`
	LimitKB     int            `json:"limit_kb"`
	ExcludeDirs []string       `json:"exclude_dirs,omitempty"`
	Limits      *ContextLimits `json:"limits,omitempty"`
}

type ContextFile struct {
	Path         string `json:"path"`
	Content      string `json:"content"`
	Truncated    bool   `json:"truncated,omitempty"`
	OriginalSize int    `json:"original_size,omitempty"`
}

type ContextResponse struct {
	Files   []ContextFile `json:"files"`
	Metrics *Metrics      `json:"metrics,omitempty"`
}

type Metrics struct {
	ScanMS        int64 `json:"scan_ms,omitempty"`         // Last scan time
	CacheLoadMS   int64 `json:"cache_load_ms,omitempty"`   // Cache load time
	CacheSaveMS   int64 `json:"cache_save_ms,omitempty"`   // Cache save time
	CacheFastOK   int   `json:"cache_fast_ok,omitempty"`   // Files validated via mtime+size
	CacheHashed   int   `json:"cache_hashed,omitempty"`    // Files that needed hash computation
	ScanFilesSeen int   `json:"scan_files_seen,omitempty"` // Files seen in last scan
}

type RefreshResponse struct {
	Status       string   `json:"status"`
	ScannedFiles int      `json:"scanned_files"`
	ChangedFiles int      `json:"changed_files"`
	Metrics      *Metrics `json:"metrics,omitempty"`
}

type DiscoveryInfo struct {
	ProtocolVersion int    `json:"protocol_version"`
	ProjectRoot     string `json:"project_root"`
	ProjectID       string `json:"project_id"`
	URL             string `json:"url"`
	Token           string `json:"token,omitempty"`
	PID             int    `json:"pid"`
	StartedAt       int64  `json:"started_at"` // unix seconds
}
