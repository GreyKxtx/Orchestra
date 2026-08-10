package sessionfile

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// ExportFormat identifies portable session export bundles written by
// orchestra session export / read by session import.
const ExportFormat = "orchestra.session.v1"

// ExportBundle is the portable on-disk shape for session transfer between
// workspaces or machines. The embedded Snapshot uses schema Version.
type ExportBundle struct {
	Format     string    `json:"format"`
	ExportedAt time.Time `json:"exported_at"`
	SourceRoot string    `json:"source_root,omitempty"`
	Snapshot   Snapshot  `json:"snapshot"`
}

// ImportOptions controls session import behaviour.
type ImportOptions struct {
	// ID overrides the snapshot id (generates NewID() when empty and id collides).
	ID string
	// Force overwrites an existing session file with the same id.
	Force bool
}

// ValidateSessionID rejects path traversal in session ids.
func ValidateSessionID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("sessionfile: empty session id")
	}
	if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return fmt.Errorf("sessionfile: invalid session id %q", id)
	}
	return nil
}

// Export loads a session and returns a portable JSON bundle.
func Export(workspaceRoot, id string) ([]byte, error) {
	if err := ValidateSessionID(id); err != nil {
		return nil, err
	}
	snap, err := Load(workspaceRoot, id)
	if err != nil {
		return nil, err
	}
	bundle := ExportBundle{
		Format:     ExportFormat,
		ExportedAt: time.Now().UTC(),
		SourceRoot: workspaceRoot,
		Snapshot:   *snap,
	}
	return json.MarshalIndent(bundle, "", "  ")
}

// ParseExport reads export bundle JSON or a raw .orchestra/sessions/*.json snapshot.
// Returns snapshot, optional source_root hint, error.
func ParseExport(data []byte) (*Snapshot, string, error) {
	if len(data) == 0 {
		return nil, "", fmt.Errorf("sessionfile: empty export")
	}
	var probe struct {
		Format   string          `json:"format"`
		Snapshot json.RawMessage `json:"snapshot"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, "", fmt.Errorf("sessionfile: parse export: %w", err)
	}
	if probe.Format == ExportFormat && len(probe.Snapshot) > 0 {
		var bundle ExportBundle
		if err := json.Unmarshal(data, &bundle); err != nil {
			return nil, "", fmt.Errorf("sessionfile: parse bundle: %w", err)
		}
		snap := bundle.Snapshot
		normalizeSnapshot(&snap, snap.ID)
		if err := ValidateSessionID(snap.ID); err != nil {
			return nil, "", err
		}
		return &snap, bundle.SourceRoot, nil
	}
	snap, err := ParseSnapshot(data, "")
	if err != nil {
		return nil, "", err
	}
	if err := ValidateSessionID(snap.ID); err != nil {
		return nil, "", err
	}
	return snap, "", nil
}

// Import writes a parsed snapshot into workspaceRoot/.orchestra/sessions/.
// Returns the session id used on disk.
func Import(workspaceRoot string, data []byte, opts ImportOptions) (string, error) {
	snap, _, err := ParseExport(data)
	if err != nil {
		return "", err
	}
	targetID := strings.TrimSpace(opts.ID)
	if targetID == "" {
		targetID = snap.ID
	}
	if err := ValidateSessionID(targetID); err != nil {
		return "", err
	}

	dest := snapshotPath(workspaceRoot, targetID)
	if _, statErr := os.Stat(dest); statErr == nil && !opts.Force {
		return "", fmt.Errorf("sessionfile: session %q already exists (use --force or --id)", targetID)
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return "", statErr
	}

	snap.ID = targetID
	normalizeSnapshot(snap, targetID)
	if err := Save(workspaceRoot, snap); err != nil {
		return "", err
	}
	return targetID, nil
}

// SessionExists reports whether a session file is present.
func SessionExists(workspaceRoot, id string) bool {
	if err := ValidateSessionID(id); err != nil {
		return false
	}
	_, err := os.Stat(snapshotPath(workspaceRoot, id))
	return err == nil
}
