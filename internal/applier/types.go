package applier

// ApplyOptions contains options for applying changes.
type ApplyOptions struct {
	DryRun       bool
	Backup       bool
	BackupSuffix string // ".orchestra.bak"
}

// FileDiff represents a diff for a single file.
type FileDiff struct {
	Path   string `json:"path"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// ApplyResult contains the result of applying changes.
type ApplyResult struct {
	Diffs []FileDiff `json:"diffs"`
	// ChangedFiles are relative file paths that were changed (or would change in dry-run).
	ChangedFiles []string `json:"changed_files,omitempty"`
}
